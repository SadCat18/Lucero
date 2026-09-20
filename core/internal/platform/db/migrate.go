package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var migrationFile = regexp.MustCompile(`^(\d+)_(.+)\.sql$`)

type migration struct {
	version int
	name    string
	path    string
}

// Migrate applies pending .sql migrations from a directory on disk, in version
// order.
//
// Each script carries its own BEGIN IMMEDIATE/COMMIT and its own
// schema_migrations INSERT, so this function must neither wrap them in another
// transaction nor write ledger rows itself (契约 §1.5). A script that fails
// leaves the ledger untouched, and the caller must not enter Ready.
func (d *DB) Migrate(dir string) error {
	return d.migrate(os.DirFS(dir), ".", dir)
}

// migrateFS applies pending .sql scripts read only from fsys, never from disk.
// v0.1 ships them through `cmd/core -migrations <dir>` (see Migrate); this entry
// exists so tests can drive the real migrator — ordering, bookkeeping and both
// deviation checks — from memory with fstest.MapFS. It shares one implementation
// with Migrate, so the two cannot drift apart.
func (d *DB) migrateFS(fsys fs.FS, dir string) error {
	return d.migrate(fsys, dir, fmt.Sprintf("fs.FS(%s)", dir))
}

func (d *DB) migrate(fsys fs.FS, dir, label string) error {
	ctx := context.Background()
	present, err := readMigrations(fsys, dir, label)
	if err != nil {
		return err
	}
	applied, err := d.appliedVersions()
	if err != nil {
		return err
	}
	// Deviation one: a ledger row with no script. A committed migration went
	// missing, and applying what is left cannot reconstruct that schema.
	for _, v := range sortedVersions(applied) {
		if !hasVersion(present, v) {
			return fmt.Errorf("schema_migrations records version %d but no migration in %s provides it", v, label)
		}
	}

	c, err := d.write.Conn(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	backupDone := false
	for _, m := range present {
		if applied[m.version] {
			continue
		}
		if !backupDone {
			if err := d.backupBefore(c, m.version); err != nil {
				return err
			}
			backupDone = true
		}
		script, err := fs.ReadFile(fsys, m.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.name, err)
		}
		if _, err := c.ExecContext(ctx, string(script)); err != nil {
			// The script's own BEGIN may still be open, and this is the only write
			// connection — leaving it parked would block every later migration.
			if _, rbErr := c.ExecContext(ctx, "ROLLBACK"); rbErr != nil {
				return fmt.Errorf("apply migration %s: %w (rollback: %v)", m.name, err, rbErr)
			}
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		var recorded int
		err = c.QueryRowContext(ctx,
			"SELECT count(*) FROM schema_migrations WHERE version = ?", m.version).Scan(&recorded)
		if err != nil {
			return fmt.Errorf("check ledger for migration %d: %w", m.version, err)
		}
		// Deviation two: a script that is present but never booked, including one
		// that books a version other than its own file name.
		if recorded != 1 {
			return fmt.Errorf("migration %s changed the schema but recorded nothing (want exactly 1 ledger row, got %d)", m.name, recorded)
		}
	}
	return nil
}

func sortedVersions(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func hasVersion(list []migration, v int) bool {
	for _, m := range list {
		if m.version == v {
			return true
		}
	}
	return false
}

func readMigrations(fsys fs.FS, dir, label string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %s: %w", label, err)
	}
	var out []migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		f := migrationFile.FindStringSubmatch(e.Name())
		if f == nil {
			continue
		}
		v, err := strconv.Atoi(f[1])
		if err != nil {
			return nil, fmt.Errorf("migration %s: bad version %q", e.Name(), f[1])
		}
		if dup, ok := seen[v]; ok {
			return nil, fmt.Errorf("duplicate migration version %d: %s and %s", v, dup, e.Name())
		}
		seen[v] = e.Name()
		out = append(out, migration{version: v, name: e.Name(), path: path.Join(dir, e.Name())})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no .sql migrations in %s", label)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func (d *DB) appliedVersions() (map[int]bool, error) {
	ctx := context.Background()
	c, err := d.Read.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	var n int
	if err := c.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&n); err != nil {
		return nil, fmt.Errorf("look for schema_migrations: %w", err)
	}
	applied := map[int]bool{}
	if n == 0 {
		return applied, nil
	}
	rows, err := c.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// backupBefore snapshots the database file before the schema changes. VACUUM INTO
// gives a consistent single-file copy without a WAL checkpoint dance; it cannot
// take a bound parameter, so the generated name is escaped as a literal.
func (d *DB) backupBefore(c *sql.Conn, nextVersion int) error {
	ctx := context.Background()
	var tables int
	if err := c.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil {
		return fmt.Errorf("count tables: %w", err)
	}
	if tables == 0 {
		return nil
	}
	dst := fmt.Sprintf("%s.pre-migration-%d-%s", d.path, nextVersion,
		time.Now().UTC().Format("20060102T150405.000Z"))
	q := strings.ReplaceAll(dst, "'", "''")
	if _, err := c.ExecContext(ctx, "VACUUM INTO '"+q+"'"); err != nil {
		return fmt.Errorf("backup before migration %d: %w", nextVersion, err)
	}
	return nil
}
