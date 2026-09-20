package db

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	return openAt(t, filepath.Join(t.TempDir(), "lucero.db"))
}

func openAt(t *testing.T, path string) *DB {
	t.Helper()
	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// setJournalMode returns what SQLite reports after the attempt; a no-op inside a
// transaction reports the unchanged mode instead of erroring.
func setJournalMode(t *testing.T, d *DB, mode string) string {
	t.Helper()
	var got string
	if err := d.Read.QueryRow("PRAGMA journal_mode=" + mode).Scan(&got); err != nil {
		t.Fatalf("PRAGMA journal_mode=%s: %v", mode, err)
	}
	return got
}

func TestOpenAppliesPragmasAndSupportsStrict(t *testing.T) {
	ctx := context.Background()
	d := openTemp(t)

	if mode, err := d.JournalMode(); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v; want wal", mode, err)
	}

	// Connection-level PRAGMA must hold on a freshly created read connection too.
	for _, tc := range []struct {
		pragma string
		want   int
	}{
		{"PRAGMA foreign_keys", 1},
		{"PRAGMA busy_timeout", 5000},
		{"PRAGMA synchronous", 2},
	} {
		var got int
		if err := d.Read.QueryRowContext(ctx, tc.pragma).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tc.pragma, err)
		}
		if got != tc.want {
			t.Fatalf("%s = %d; want %d", tc.pragma, got, tc.want)
		}
	}

	var ver string
	if err := d.Read.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&ver); err != nil {
		t.Fatalf("sqlite_version: %v", err)
	}
	t.Logf("linked SQLite = %s", ver)
	var major, minor int
	if _, err := fmt.Sscanf(ver, "%d.%d", &major, &minor); err != nil {
		t.Fatalf("parse version %q: %v", ver, err)
	}
	if major < 3 || (major == 3 && minor < 37) {
		t.Fatalf("SQLite %s < 3.37, STRICT/FTS5 not guaranteed (ADR-016)", ver)
	}

	if _, err := d.Read.ExecContext(ctx,
		`CREATE TABLE probe (id TEXT PRIMARY KEY, n INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatalf("create STRICT: %v", err)
	}
	if _, err := d.Read.ExecContext(ctx, `INSERT INTO probe(id, n) VALUES('a', 'not-an-int')`); err == nil {
		t.Fatal("STRICT table accepted a string in an INTEGER column")
	}
	if _, err := d.Read.ExecContext(ctx, `INSERT INTO probe(id, n) VALUES('a', 1)`); err != nil {
		t.Fatalf("STRICT insert: %v", err)
	}
	if _, err := d.Read.ExecContext(ctx,
		`CREATE VIRTUAL TABLE probe_fts USING fts5(body, content='probe', content_rowid='rowid')`); err != nil {
		t.Fatalf("create FTS5: %v", err)
	}
}

// ADR-007's "journal_mode inside a transaction is a silent no-op" is stale on
// the pinned engine (modernc SQLite 3.53.4 errors with "cannot change into wal
// mode from within a transaction"). Either way the invariant that matters holds:
// the statement cannot enable WAL from inside a transaction, so Open must run it
// once outside one.
func TestJournalModeInsideTransactionDoesNotTakeEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lucero.db")
	d := openAt(t, path)

	// Start from a non-WAL database so the attempt has something to change.
	if got := setJournalMode(t, d, "DELETE"); got != "delete" {
		t.Fatalf("prep journal_mode = %q; want delete", got)
	}

	tx, err := d.Read.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	var got string
	if err := tx.QueryRow("PRAGMA journal_mode=WAL").Scan(&got); err != nil {
		t.Logf("in-tx PRAGMA journal_mode=WAL rejected: %v", err)
	} else if got == "wal" {
		t.Fatal("journal_mode=WAL took effect inside a transaction")
	} else {
		t.Logf("in-tx PRAGMA journal_mode=WAL silently reported %q", got)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if mode, err := d.JournalMode(); err != nil || mode != "delete" {
		t.Fatalf("journal_mode = %q, %v; the in-tx attempt must have left it delete", mode, err)
	}

	// The same statement outside a transaction does take effect, and persists.
	if got := setJournalMode(t, d, "WAL"); got != "wal" {
		t.Fatalf("outside tx journal_mode = %q; want wal", got)
	}
	if mode, err := d.JournalMode(); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v; want wal", mode, err)
	}
}
