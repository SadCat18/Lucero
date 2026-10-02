package main

// store.go is the only place in this adapter that touches the observed product's
// files. Two rules shape it and both are easy to lose in a later "cleanup":
//
//   - read-only, structurally. The DSN carries mode=ro, so a write is refused by SQLite
//     itself rather than by our good intentions (§16.7 第 1 条: 只观察).
//   - column names are discovered, not assumed. The profile records the `event` table's
//     columns exactly and the `session`/`message` ones only partially (CO-64 asks survey
//     for the DDL). A reader that hard-codes the half-known list would fail with a
//    _SQL_ error and hide which fact is missing; a reader that asks the file which
//     columns exist can say "this field has no source here" - which is the same
//     discipline §7 demands of a quota value.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// sqlStore is an open, read-only handle on the provider's database.
type sqlStore struct {
	d    *sql.DB
	path string
	// columns caches pragma table_info per table: the tolerant readers below ask once
	// per table per process, and a schema that changes mid-read is not something this
	// tier may chase (the product owns its own migrations, 38 rows of them per profile §④).
	columns map[string]map[string]bool

	// log is this process's stderr, threaded in so a read can report what it saw.
	// §1: nothing that is not a protocol frame may reach stdout, and these lines are exactly
	// the "0 rows vs no such table" distinction a caller cannot see from a reply alone.
	log io.Writer
}

// openReadOnlyStore refuses anything that is not a readable database file. It never
// creates one: mode=ro plus a pre-flight stat means a typo'd path says "no store"
// instead of silently producing an empty one that a panel could read as "no sessions".
func openReadOnlyStore(path string) (*sqlStore, error) {
	if path == "" {
		return nil, fmt.Errorf("no store path configured")
	}
	info, err := statStore(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("open %s: is a directory, not a database file", path)
	}
	// file:// + an absolute Windows path goes through url so a space or a '#' in the
	// path cannot turn into a DSN parameter. The path never comes from a request:
	// session ids travel as bound SQL parameters, so no user input reaches this string.
	// The leading slash is not cosmetic. filepath.ToSlash("C:\x\y") is "C:/x/y", and
	// url renders that as `file:C:/x/y`, which modernc reads as the authority "C:" and
	// rejects ("invalid uri authority"). A drive-qualified path has to be given the slash
	// that makes it the RFC 8089 form `file:///C:/x/y`.
	slash := filepath.ToSlash(path)
	if len(slash) > 1 && slash[1] == ':' {
		slash = "/" + slash
	}
	uri := (&url.URL{Scheme: "file", Path: slash}).String() +
		"?mode=ro&_pragma=busy_timeout(2000)"
	d, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One connection, deliberately: SQLite's read-only snapshot is per connection, and
	// a pool would let two pages of the same read see different revisions.
	d.SetMaxOpenConns(1)
	s := &sqlStore{d: d, path: path, columns: map[string]map[string]bool{}, log: io.Discard}
	if err := d.PingContext(context.Background()); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("open %s read-only: %w", path, err)
	}
	return s, nil
}

func (s *sqlStore) ping(ctx context.Context) error { return s.d.PingContext(ctx) }

func (s *sqlStore) close() error { return s.d.Close() }

// hasColumn answers "does this table actually carry that column", which is the only
// honest way to read a schema this process does not own.
func (s *sqlStore) hasColumn(ctx context.Context, table, column string) (bool, error) {
	cols, err := s.tableColumns(ctx, table)
	if err != nil {
		return false, err
	}
	return cols[column], nil
}

func (s *sqlStore) tableColumns(ctx context.Context, table string) (map[string]bool, error) {
	if c, ok := s.columns[table]; ok {
		return c, nil
	}
	rows, err := s.d.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, fmt.Errorf("table_info %s: %w", table, err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan table_info %s: %w", table, err)
		}
		cols[n] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		// A table with no columns is not a table: either it does not exist (a store
		// from a different OpenCode build) or the name moved. Saying so beats reading
		// it as "nothing in there".
		return nil, fmt.Errorf("%s has no columns: not this product's schema", table)
	}
	s.columns[table] = cols
	return cols, nil
}

// statStore is split out so a test can drive the two "the store is not there" shapes -
// a missing path and a directory - without touching the real machine's files.
func statStore(path string) (os.FileInfo, error) { return os.Stat(path) }

// firstColumn picks the first of several candidate names that the table actually has.
// Its callers are the places where the profile recorded a fact under two spellings
// (`time_updated` vs `updated_at`); each candidate list is ordered most-documented
// first, and the empty result is reported as "no source", never guessed.
func (s *sqlStore) firstColumn(ctx context.Context, table string, candidates ...string) (string, error) {
	for _, c := range candidates {
		ok, err := s.hasColumn(ctx, table, c)
		if err != nil {
			return "", err
		}
		if ok {
			return c, nil
		}
	}
	return "", nil
}

// cell is one scanned column, keeping "SQL NULL" apart from "empty string" apart from
// "a number". That distinction is the whole content of §7 (UNKNOWN 与 0 是两回事) and
// of §16.4's three tiers, so it is preserved at the driver boundary rather than being
// flattened into Go's zero values on the way in.
type cell struct {
	null     bool
	num      float64
	isNum    bool
	str      string
	hasKey   bool // the column was not in the SELECT at all (no source), vs present
	missing  bool
	byteVal  []byte
	hasBytes bool
}

// bytes returns the column as bytes whether SQLite handed back a BLOB or a TEXT. A
// reader that only looked at byteVal would unmarshal an empty slice from every text
// column and report "unexpected end of JSON input" for a payload that is plainly there.
func (c cell) bytes() []byte {
	if c.hasBytes {
		return c.byteVal
	}
	return []byte(c.str)
}

// bytes returns the column as bytes whether SQLite gave back a BLOB or a TEXT. Reading
// byteVal alone would unmarshal an empty slice from every text column and report
// "unexpected end of JSON input" about a payload that is plainly in the row - which is
// the kind of error that gets misread as "the provider wrote junk" instead of "we looked
// in the wrong field".
func (c cell) text() string {
	if c.null || c.missing {
		return ""
	}
	if c.hasBytes {
		return string(c.byteVal)
	}
	return c.str
}

// number reports the value and whether there was one: `ok` false means NULL or absent,
// which callers must not read as 0.
func (c cell) number() (float64, bool) {
	if c.null || c.missing || !c.isNum {
		return 0, false
	}
	return c.num, true
}

func classify(v any) cell {
	switch x := v.(type) {
	case nil:
		return cell{null: true, hasKey: true}
	case int64:
		return cell{num: float64(x), isNum: true, str: fmt.Sprint(x), hasKey: true}
	case int:
		return cell{num: float64(x), isNum: true, str: fmt.Sprint(x), hasKey: true}
	case float64:
		return cell{num: x, isNum: true, hasKey: true}
	case bool:
		return cell{num: b2f(x), isNum: true, str: fmt.Sprint(x), hasKey: true}
	case []byte:
		return cell{byteVal: x, hasBytes: true, str: string(x), hasKey: true}
	case string:
		return cell{str: x, hasKey: true}
	default:
		return cell{str: fmt.Sprint(x), hasKey: true}
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// query reads a page of rows into name→cell maps. `cols` is the explicit list this
// caller settled on with firstColumn, so the SELECT never carries a guessed name, and
// the returned maps only carry what the database really has.
func (s *sqlStore) query(ctx context.Context, query string, args ...any) ([]map[string]cell, error) {
	rows, err := s.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}
	var out []map[string]cell
	for rows.Next() {
		holds := make([]any, len(names))
		for i := range holds {
			holds[i] = new(any)
		}
		if err := rows.Scan(holds...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		row := make(map[string]cell, len(names))
		for i, n := range names {
			v := *(holds[i].(*any))
			c := classify(v)
			// database/sql copies the buffer for []byte only for the lifetime of the
			// row, so the bytes are copied out here or they change under a later page.
			if b, ok := v.([]byte); ok {
				c.byteVal = append([]byte(nil), b...)
			}
			row[n] = c
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// quoteIdent guards the handful of places where a column or table name had to be
// composed rather than bound: the names come from pragma_table_info (so they are the
// schema's own), never from a request, and double quotes are SQLite's identifier
// quoting. Values always travel as `?` parameters.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
