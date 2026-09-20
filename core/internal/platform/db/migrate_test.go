package db

import (
	"os"
	"path/filepath"
	"testing"
)

func repoMigrations(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	for _, name := range repoMigrationNames(t) {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("migrations dir %s: %v", dir, err)
		}
	}
	return dir
}

// repoMigrationNames lists every tracked .sql in core/migrations, so a new
// migration never leaves these fixtures half-updated.
func repoMigrationNames(t *testing.T) []string {
	t.Helper()
	return []string{
		"000001_core_schema.sql",
		"000002_aggregate_version.sql",
		"000003_grant_policy_version.sql",
	}
}

// copyMigrations copies the whole real migration set into dir, so synthetic
// scripts can always be numbered after the last real one.
func copyMigrations(t *testing.T, dir string) {
	t.Helper()
	src := repoMigrations(t)
	for _, name := range repoMigrationNames(t) {
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		writeFile(t, filepath.Join(dir, name), string(b))
	}
}

func ledgerVersions(t *testing.T, d *DB) []int {
	t.Helper()
	rows, err := d.Read.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan ledger: %v", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate ledger: %v", err)
	}
	return out
}

func eq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func backupsOf(t *testing.T, dbPath string) []string {
	t.Helper()
	m, err := filepath.Glob(dbPath + ".pre-migration-*")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	return m
}

// The real 000001 + 000002 pair must apply to an empty file, and re-running must
// be a no-op that neither errors nor double-books.
func TestMigrateAppliesRealMigrationsThenIsIdempotent(t *testing.T) {
	dir := repoMigrations(t)
	d := openTemp(t)

	if err := d.Migrate(dir); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if got := ledgerVersions(t, d); !eq(got, []int{1, 2, 3}) {
		t.Fatalf("ledger after migrate = %v; want [1 2 3]", got)
	}
	if mode, err := d.JournalMode(); err != nil || mode != "wal" {
		t.Fatalf("journal_mode after migrate = %q, %v; want wal (000001 must not have broken it)", mode, err)
	}

	// ADR-017 + 000003: a NOT NULL, default-1 version column on every aggregate table.
	for _, tbl := range []string{"projects", "adapter_instances", "models", "sessions", "tasks",
		"executions", "approvals", "quota_accounts", "notifications", "handoffs",
		"temporary_grants", "policy_rules", "adapter_capabilities"} {
		var name, dflt string
		var notNull int
		err := d.Read.QueryRow(`SELECT name, "notnull", dflt_value FROM pragma_table_info(?) WHERE name='version'`, tbl).
			Scan(&name, &notNull, &dflt)
		if err != nil {
			t.Fatalf("%s.version missing: %v", tbl, err)
		}
		if notNull != 1 || dflt != "1" {
			t.Fatalf("%s.version: notnull=%d default=%s; want 1 and 1", tbl, notNull, dflt)
		}
	}

	if err := d.Migrate(dir); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if got := ledgerVersions(t, d); !eq(got, []int{1, 2, 3}) {
		t.Fatalf("ledger after rerun = %v; want [1 2 3] with no double booking", got)
	}
}

// An empty database has nothing to back up; a migrated one must be snapshotted
// before the next schema change.
func TestMigrateBacksUpOnlyWhenThereIsASchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lucero.db")
	d := openAt(t, path)
	if err := d.Migrate(repoMigrations(t)); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if backs := backupsOf(t, path); len(backs) != 0 {
		t.Fatalf("empty database was backed up: %v", backs)
	}
	d.Close()

	d = openAt(t, path)
	dir := t.TempDir()
	copyMigrations(t, dir)
	writeFile(t, filepath.Join(dir, "000004_extra.sql"), `BEGIN IMMEDIATE;
CREATE TABLE extra (id TEXT PRIMARY KEY) STRICT;
INSERT INTO schema_migrations(version, name, applied_at) VALUES (4, 'extra', 1);
COMMIT;`)
	if err := d.Migrate(dir); err != nil {
		t.Fatalf("Migrate with new script: %v", err)
	}
	if backs := backupsOf(t, path); len(backs) != 1 {
		t.Fatalf("backups before migrating a populated database = %v; want exactly 1", backs)
	}
	if got := ledgerVersions(t, d); !eq(got, []int{1, 2, 3, 4}) {
		t.Fatalf("ledger = %v; want [1 2 3 4]", got)
	}
}

// A script that dies half way must leave the previous schema intact, the ledger
// clean, and integrity_check ok.
func TestMigrateFailureLeavesDatabaseIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lucero.db")
	d := openAt(t, path)
	dir := t.TempDir()
	copyMigrations(t, dir)
	writeFile(t, filepath.Join(dir, "000004_broken.sql"), `BEGIN IMMEDIATE;
CREATE TABLE half_baked (id TEXT PRIMARY KEY) STRICT;
INSERT INTO no_such_table VALUES (1);
COMMIT;`)

	if err := d.Migrate(dir); err == nil {
		t.Fatal("Migrate accepted a broken script")
	} else {
		t.Logf("expected failure: %v", err)
	}

	var check string
	if err := d.Read.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if check != "ok" {
		t.Fatalf("integrity_check = %q; want ok", check)
	}
	if got := ledgerVersions(t, d); !eq(got, []int{1, 2, 3}) {
		t.Fatalf("ledger after failed migration = %v; want [1 2 3]", got)
	}
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='half_baked'`).Scan(&n); err != nil {
		t.Fatalf("look for half_baked: %v", err)
	}
	if n != 0 {
		t.Fatal("the failed transaction's table survived the rollback")
	}

	// A good script still applies afterwards.
	writeFile(t, filepath.Join(dir, "000004_broken.sql"), `BEGIN IMMEDIATE;
CREATE TABLE fixed_up (id TEXT PRIMARY KEY) STRICT;
INSERT INTO schema_migrations(version, name, applied_at) VALUES (4, 'fixed_up', 1);
COMMIT;`)
	if err := d.Migrate(dir); err != nil {
		t.Fatalf("Migrate after fixing the script: %v", err)
	}
	if got := ledgerVersions(t, d); !eq(got, []int{1, 2, 3, 4}) {
		t.Fatalf("ledger = %v; want [1 2 3 4]", got)
	}
}

// A migration file must record itself; the migrator never writes the ledger.
func TestMigrateRejectsSilentMigration(t *testing.T) {
	d := openTemp(t)
	dir := t.TempDir()
	copyMigrations(t, dir)
	writeFile(t, filepath.Join(dir, "000004_unbooked.sql"), `BEGIN IMMEDIATE;
CREATE TABLE unbooked (id TEXT PRIMARY KEY) STRICT;
COMMIT;`)
	if err := d.Migrate(dir); err == nil {
		t.Fatal("Migrate accepted a script that recorded nothing")
	} else {
		t.Logf("expected failure: %v", err)
	}
}
