package process

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 000001 is frozen (red line: no migration is edited to fit new code), and it enforces
// three enumerations with CHECK constraints. My Go constants are a second list of the
// same names, so the two can drift - and the drift would surface as an SQLite
// constraint error in production, in the one path (Recovery reading process_instances)
// that is already hard to debug. This test reads the migration itself rather than a
// pasted copy, so the schema stays the only source of truth.

var (
	processInstances = regexp.MustCompile(`(?s)CREATE TABLE process_instances \((.*?)\n\) STRICT;`)
	checkList        = regexp.MustCompile(`(?s)CHECK \((\w+) IN \((.*?)\)\)`)
	stringLit        = regexp.MustCompile(`'([A-Z_]+)'`)
	columnName       = regexp.MustCompile(`(?m)^\s{4}(\w+)\s+\w+`)
)

// migrationFile walks up from the test's directory to the repo root the same way the
// contract cross-check does: a hardcoded ../.. breaks when the package moves, and an
// unreadable file must skip loudly rather than pass.
func migrationFile(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "migrations", "000001_core_schema.sql")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("000001 not reachable from this working tree; the cross-check is skipped, not passed")
	return ""
}

// instanceTable returns the CHECK-constrained columns and the plain column names of
// process_instances, failing loudly if either parse comes back empty: an empty set
// would make every subset assertion pass by accident.
func instanceTable(t *testing.T) (map[string][]string, map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile(migrationFile(t))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	body := strings.ReplaceAll(string(raw), "\r", "") // CRLF would defeat the \n anchors
	block := processInstances.FindStringSubmatch(body)
	if block == nil {
		t.Fatal("no CREATE TABLE process_instances ... STRICT; block found - the anchor is stale, not the schema")
	}
	checks := map[string][]string{}
	for _, m := range checkList.FindAllStringSubmatch(block[1], -1) {
		var vals []string
		for _, lit := range stringLit.FindAllStringSubmatch(m[2], -1) {
			vals = append(vals, lit[1])
		}
		checks[m[1]] = vals
	}
	cols := map[string]bool{}
	for _, m := range columnName.FindAllStringSubmatch(block[1], -1) {
		cols[m[1]] = true
	}
	for _, name := range []string{"process_type", "status", "restart_policy"} {
		if len(checks[name]) == 0 {
			t.Fatalf("CHECK list for %s parsed to 0 values; fix the parser, do not judge green", name)
		}
	}
	if len(cols) < 10 {
		t.Fatalf("process_instances columns parsed to %d, want the whole table; the parse is wrong", len(cols))
	}
	return checks, cols
}

func TestEnumsFitTheFrozenProcessInstancesTable(t *testing.T) {
	checks, cols := instanceTable(t)

	groups := []struct {
		name   string
		column string
		values []string
	}{
		{"Kind", "process_type", []string{string(KindAdapter), string(KindWorker)}},
		{"Status", "status", []string{string(StatusStarting), string(StatusRunning),
			string(StatusStopping), string(StatusExited), string(StatusFailed), string(StatusUnknown)}},
		{"RestartPolicy", "restart_policy", []string{string(RestartNever),
			string(RestartOnFailure), string(RestartAlways)}},
	}
	for _, g := range groups {
		allowed := map[string]bool{}
		for _, v := range checks[g.column] {
			allowed[v] = true
		}
		for _, v := range g.values {
			if !allowed[v] {
				t.Errorf("%s %q is not in 000001's CHECK (%s IN %v): the row would be refused by SQLite, "+
					"and 000001 is frozen so the Go value is the one that has to move",
					g.name, v, g.column, checks[g.column])
			}
		}
	}

	// The three lifecycle methods carry no row handle (T-62 ruled the interface final),
	// which is only workable because these columns are filled at assembly time rather
	// than per event. If one of them ever disappears from the table, the assumption in
	// Recorder's documentation is no longer true and that comment has to change with it.
	for _, name := range []string{"id", "owner_type", "owner_id", "executable", "metadata_json"} {
		if !cols[name] {
			t.Errorf("process_instances has no %s column any more: Recorder's assembly-time note is stale", name)
		}
	}
	// There is deliberately no crashes column, so the running count in Event.Crashes
	// has exactly one home. If a future migration adds one, this assertion is the trip
	// wire that says so rather than leaving the counter silently dropped.
	if cols["crashes"] {
		t.Error("process_instances gained a crashes column: Event.Crashes no longer has to ride in metadata_json, " +
			"and the Recorder doc must be updated")
	}
}
