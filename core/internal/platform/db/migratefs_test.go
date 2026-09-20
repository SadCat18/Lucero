package db

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// v0.1 ships migrations through `cmd/core -migrations <dir>`; this entry exists
// for testability, so the ordering and both deviation checks run against an
// in-memory fs.FS rather than a temporary directory on disk.
const (
	probeOne = `BEGIN IMMEDIATE;
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT;
CREATE TABLE probe (id TEXT PRIMARY KEY, n INTEGER NOT NULL DEFAULT 0) STRICT;
INSERT INTO schema_migrations(version, name, applied_at) VALUES (1, 'probe_one', 1);
COMMIT;`
	probeTwo = `BEGIN IMMEDIATE;
ALTER TABLE probe ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
INSERT INTO schema_migrations(version, name, applied_at) VALUES (2, 'probe_two', 2);
COMMIT;`
)

// probeFS serves the two-step set from memory, in the given order/selection.
func probeFS(names ...string) fstest.MapFS {
	src := map[string]string{
		"000001_probe_one.sql": probeOne,
		"000002_probe_two.sql": probeTwo,
	}
	m := fstest.MapFS{}
	for _, n := range names {
		m[n] = &fstest.MapFile{Data: []byte(src[n])}
	}
	return m
}

func TestMigrateFSAppliesInOrderAndIsIdempotent(t *testing.T) {
	d := openTemp(t)
	both := []string{"000001_probe_one.sql", "000002_probe_two.sql"}

	if err := d.migrateFS(probeFS(both...), "."); err != nil {
		t.Fatalf("MigrateFS: %v", err)
	}
	if got := ledgerVersions(t, d); !eq(got, []int{1, 2}) {
		t.Fatalf("ledger = %v; want [1 2]", got)
	}
	var hasVersionCol int
	if err := d.Read.QueryRow(`SELECT count(*) FROM pragma_table_info('probe') WHERE name='version'`).
		Scan(&hasVersionCol); err != nil || hasVersionCol != 1 {
		t.Fatalf("step 2's ALTER did not land: count=%d err=%v", hasVersionCol, err)
	}
	if err := d.migrateFS(probeFS(both...), "."); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if got := ledgerVersions(t, d); !eq(got, []int{1, 2}) {
		t.Fatalf("ledger after rerun = %v; want [1 2]", got)
	}
}

// The production scripts must apply through this entry too, so the in-memory
// fixtures above cannot hide a break in the real set.
func TestMigrateFSOverRealMigrations(t *testing.T) {
	d := openTemp(t)
	if err := d.migrateFS(os.DirFS(repoMigrations(t)), "."); err != nil {
		t.Fatalf("MigrateFS(real migrations): %v", err)
	}
	if got := ledgerVersions(t, d); !eq(got, []int{1, 2, 3}) {
		t.Fatalf("ledger = %v; want [1 2 3]", got)
	}
}

// migrateFS reads only the fs.FS. An empty FS must fail even though the same
// scripts sit on disk a few directories up.
func TestMigrateFSNeverFallsBackToDisk(t *testing.T) {
	d := openTemp(t)
	err := d.migrateFS(fstest.MapFS{}, ".")
	if err == nil {
		t.Fatal("MigrateFS accepted an empty FS while core/migrations exists on disk")
	}
	if !strings.Contains(err.Error(), "no .sql migrations") {
		t.Fatalf("unexpected failure: %v", err)
	}
	t.Logf("%v", err)
}

// Deviation one: the ledger records a version no provided script supplies.
func TestMigrateFSReportsLostMigration(t *testing.T) {
	d := openTemp(t)
	both := []string{"000001_probe_one.sql", "000002_probe_two.sql"}
	if err := d.migrateFS(probeFS(both...), "."); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := d.migrateFS(probeFS("000001_probe_one.sql"), ".")
	if err == nil {
		t.Fatal("a lost migration file was accepted")
	}
	if !strings.Contains(err.Error(), "records version 2 but no migration") {
		t.Fatalf("unexpected failure: %v", err)
	}
	t.Logf("%v", err)
}

// Deviation two: a script that runs but never books itself.
func TestMigrateFSReportsUnbookedMigration(t *testing.T) {
	d := openTemp(t)
	silent := fstest.MapFS{
		"000001_silent.sql": &fstest.MapFile{Data: []byte(`BEGIN IMMEDIATE;
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT;
CREATE TABLE quiet (id TEXT PRIMARY KEY) STRICT;
COMMIT;`)},
	}
	err := d.migrateFS(silent, ".")
	if err == nil {
		t.Fatal("a script that recorded nothing was accepted")
	}
	if !strings.Contains(err.Error(), "recorded nothing") {
		t.Fatalf("unexpected failure: %v", err)
	}
	t.Logf("%v", err)
}
