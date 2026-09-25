package process

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// The whole point of this file is the pairing: one observation in, one row plus exactly
// one event out. A fake Tx cannot show that - a stub happily "records" a row while writing
// no event, which is the exact T-168 accident - so every case here runs against real SQLite
// over the frozen migrations.
func openProcessStore(t *testing.T, row ProcessRow, logf func(string, ...any)) (*LifecycleStore, *db.DB, *[]string) {
	t.Helper()
	r := audit.Rules()
	if err := db.InitRedaction(r.DenyKeys, r.Patterns, r.Marker); err != nil {
		t.Fatalf("InitRedaction: %v", err)
	}
	d, err := db.Open(filepath.Join(t.TempDir(), "lifecycle.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ids := []string{}
	n := 0
	stamp := func() int64 { return 1789900000000 }
	s, err := NewLifecycleStore(row, func() (string, error) {
		n++
		id := "proc-" + string(rune('a'+n-1))
		ids = append(ids, id)
		return id, nil
	}, stamp, logf)
	if err != nil {
		t.Fatalf("NewLifecycleStore: %v", err)
	}
	return s, d, &ids
}

func testRow() ProcessRow {
	return ProcessRow{
		Kind: KindAdapter, OwnerType: "adapter_instance", OwnerID: "ai-1",
		Executable: "fake-adapter", RestartPolicy: RestartOnFailure,
	}
}

func queryRow(t *testing.T, d *db.DB, id string) map[string]any {
	t.Helper()
	rows, err := d.Read.Query(`SELECT process_type, owner_type, owner_id, pid, executable, status,
		restart_policy, started_at, exited_at, exit_code, metadata_json, created_at, updated_at
		FROM process_instances WHERE id = ?`, id)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no process_instances row for %s", id)
	}
	cols, _ := rows.Columns()
	raw := make([]any, len(cols))
	for i := range raw {
		raw[i] = new(sqlNullish{})
	}
	out := map[string]any{}
	if err := rows.Scan(raw...); err != nil {
		t.Fatalf("scan row: %v", err)
	}
	for i, c := range cols {
		out[c] = deref(raw[i])
	}
	return out
}

// sqlNullish/Scan below keep the test free of sql.Null* boilerplate for 13 columns.
type sqlNullish struct{ v any }

func (s *sqlNullish) Scan(src any) error { s.v = src; return nil }

func deref(a any) any {
	if s, ok := a.(*sqlNullish); ok {
		return s.v
	}
	return a
}

func eventRows(t *testing.T, d *db.DB, id string) []struct {
	Type    string
	Version int64
} {
	t.Helper()
	rows, err := d.Read.Query(`SELECT event_type, aggregate_version FROM event_journal
		WHERE aggregate_type = 'process' AND aggregate_id = ? ORDER BY aggregate_version`, id)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	defer rows.Close()
	out := []struct {
		Type    string
		Version int64
	}{}
	for rows.Next() {
		var e struct {
			Type    string
			Version int64
		}
		if err := rows.Scan(&e.Type, &e.Version); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func write(t *testing.T, d *db.DB, fn func(*db.Tx) error) {
	t.Helper()
	if err := d.Write(context.Background(), fn); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func shoutRecorder() (func(string, ...any), *[]string) {
	seen := []string{}
	return func(format string, args ...any) {
		seen = append(seen, strings.TrimSpace(fmt.Sprintf(format, args...)))
	}, &seen
}

func stringOf(a any) string {
	if b, ok := a.([]byte); ok {
		return string(b)
	}
	if s, ok := a.(string); ok {
		return s
	}
	if s, ok := a.(*sqlNullish); ok {
		return stringOf(s.v)
	}
	return fmt.Sprint(a)
}

func asInt64(a any) int64 {
	if s, ok := a.(*sqlNullish); ok {
		a = s.v
	}
	switch v := a.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return -1
	}
}

// importsOf reads a file's import paths, so the guard below judges the real dependency
// rather than a comment about it (the mistake CO-43 is about).
func importsOf(t *testing.T, file string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	out := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("unquote import in %s: %v", file, err)
		}
		out = append(out, p)
	}
	return out
}

func TestStartLandsOneRowAndExactlyOneEvent(t *testing.T) {
	logf, shouted := shoutRecorder()
	s, d, ids := openProcessStore(t, testRow(), logf)
	write(t, d, func(tx *db.Tx) error {
		_, err := s.RecordProcessStart(tx, Event{Kind: EventStarted, PID: 4242, Status: StatusStarting, Timestamp: 1700000000000})
		return err
	})
	row := queryRow(t, d, (*ids)[0])
	// The status is the one the Supervisor observed (supervisor.go emits STARTING with the
	// real pid); the write cell never fudges it to RUNNING, and Recovery's live-process
	// reading in 契约① 第 20 条 counts STARTING as alive, so nothing hangs off the fudge.
	for col, want := range map[string]any{
		"process_type": "ADAPTER", "owner_type": "adapter_instance", "owner_id": "ai-1",
		"executable": "fake-adapter", "status": "STARTING", "restart_policy": "ON_FAILURE",
	} {
		if got := row[col]; got != want {
			t.Errorf("%s = %v, want %v", col, got, want)
		}
	}
	if got := asInt64(row["pid"]); got != 4242 {
		t.Errorf("pid = %d, want the real child pid 4242", got)
	}
	if got := asInt64(row["started_at"]); got != 1700000000000 {
		t.Errorf("started_at = %d, want the event's own ms (§14 stores ms)", got)
	}
	if got := asInt64(row["created_at"]); got != 1789900000000 {
		t.Errorf("created_at = %d, want Now()'s ms (the row is stamped, not defaulted)", got)
	}
	if meta := stringOf(row["metadata_json"]); !strings.Contains(meta, `"attempt":1`) {
		t.Errorf("metadata_json = %s, want attempt 1 for the first row", meta)
	}
	ev := eventRows(t, d, (*ids)[0])
	if len(ev) != 1 || ev[0].Type != "process_started" || ev[0].Version != 1 {
		t.Fatalf("events = %+v, want exactly one process_started at version 1", ev)
	}
	if len(*shouted) != 0 {
		t.Errorf("a clean start should not shout, got %q", *shouted)
	}
}

func TestExitMovesTheRowAndBooksTheSecondEvent(t *testing.T) {
	logf, shouted := shoutRecorder()
	s, d, ids := openProcessStore(t, testRow(), logf)
	write(t, d, func(tx *db.Tx) error {
		if _, err := s.RecordProcessStart(tx, Event{Kind: EventStarted, PID: 7, Status: StatusStarting}); err != nil {
			return err
		}
		return s.RecordProcessExit(tx, Event{Kind: EventExited, PID: 7, Status: StatusExited, ExitCode: 0, Timestamp: 1700000009000})
	})
	id := (*ids)[0]
	row := queryRow(t, d, id)
	if got := stringOf(row["status"]); got != "EXITED" {
		t.Errorf("status = %s, want EXITED", got)
	}
	if got := asInt64(row["exited_at"]); got != 1700000009000 {
		t.Errorf("exited_at = %d, want the event's ms", got)
	}
	if got := asInt64(row["exit_code"]); got != 0 {
		t.Errorf("exit_code = %d, want 0", got)
	}
	if got := asInt64(row["created_at"]); got != 1789900000000 {
		t.Errorf("created_at = %d, an edge must not rewrite the row's birth", got)
	}
	ev := eventRows(t, d, id)
	if len(ev) != 2 || ev[1].Type != "process_exited" || ev[1].Version != 2 {
		t.Fatalf("events = %+v, want process_started then process_exited at version 2", ev)
	}
	if len(*shouted) != 0 {
		t.Errorf("a real edge should not shout, got %q", *shouted)
	}
}

func TestCrashKeepsTheBudgetInTheRow(t *testing.T) {
	logf, _ := shoutRecorder()
	s, d, ids := openProcessStore(t, testRow(), logf)
	write(t, d, func(tx *db.Tx) error {
		if _, err := s.RecordProcessStart(tx, Event{Kind: EventStarted, PID: 8, Status: StatusStarting}); err != nil {
			return err
		}
		return s.RecordProcessCrash(tx, Event{Kind: EventCrashed, PID: 8, Status: StatusFailed,
			ExitCode: 3, Crashes: 2, Timestamp: 1700000010000})
	})
	id := (*ids)[0]
	row := queryRow(t, d, id)
	if got := stringOf(row["status"]); got != "FAILED" {
		t.Errorf("status = %s, want FAILED for an abnormal exit", got)
	}
	meta := stringOf(row["metadata_json"])
	if !strings.Contains(meta, `"crashes":2`) {
		t.Errorf("metadata_json = %s, want §8's consecutive-crash count kept (no column for it)", meta)
	}
	ev := eventRows(t, d, id)
	if len(ev) != 2 || ev[1].Type != "process_crashed" {
		t.Fatalf("events = %+v, want a process_crashed edge second", ev)
	}
}

func TestSecondAttemptGetsItsOwnRow(t *testing.T) {
	logf, _ := shoutRecorder()
	s, d, ids := openProcessStore(t, testRow(), logf)
	write(t, d, func(tx *db.Tx) error {
		if _, err := s.RecordProcessStart(tx, Event{Kind: EventStarted, PID: 11, Status: StatusStarting}); err != nil {
			return err
		}
		if err := s.RecordProcessCrash(tx, Event{Kind: EventCrashed, PID: 11, Status: StatusFailed, ExitCode: 1, Crashes: 1}); err != nil {
			return err
		}
		_, err := s.RecordProcessStart(tx, Event{Kind: EventStarted, PID: 22, Status: StatusStarting})
		return err
	})
	if len(*ids) != 2 || (*ids)[0] == (*ids)[1] {
		t.Fatalf("ids = %v, want a fresh id per attempt (CO-45 (a))", *ids)
	}
	first, second := queryRow(t, d, (*ids)[0]), queryRow(t, d, (*ids)[1])
	if got := stringOf(first["status"]); got != "FAILED" {
		t.Errorf("attempt 1 status = %s, want FAILED - a restart must not overwrite pid history", got)
	}
	if got := asInt64(first["pid"]); got != 11 || asInt64(second["pid"]) != 22 {
		t.Errorf("pids = %d/%d, want one row per live instance (11 then 22)", got, asInt64(second["pid"]))
	}
	meta := stringOf(second["metadata_json"])
	if !strings.Contains(meta, `"attempt":2`) || !strings.Contains(meta, `"`+(*ids)[0]+`"`) {
		t.Errorf("metadata_json = %s, want attempt 2 and previous_instance_id %q (000001 is frozen, so the chain lives here)",
			meta, (*ids)[0])
	}
	// Each attempt's row carries its own chain and nothing from its sibling: attempt 1 was
	// started then crashed, attempt 2 is still live and its own first event is version 1.
	if ev := eventRows(t, d, (*ids)[0]); len(ev) != 2 || ev[0].Type != "process_started" || ev[1].Type != "process_crashed" {
		t.Errorf("attempt 1 events = %+v, want its own started then crashed", ev)
	}
	if ev := eventRows(t, d, (*ids)[1]); len(ev) != 1 || ev[0].Type != "process_started" || ev[0].Version != 1 {
		t.Errorf("attempt 2 events = %+v, want exactly one process_started at version 1", ev)
	}
}

// CO-44 ①+③ together: a second edge on a closed row books nothing, and is not quiet about it.
func TestReplayedEdgeBooksNoEventAndSaysSo(t *testing.T) {
	logf, shouted := shoutRecorder()
	s, d, _ := openProcessStore(t, testRow(), logf)
	first := ""
	write(t, d, func(tx *db.Tx) error {
		id, err := s.RecordProcessStart(tx, Event{Kind: EventStarted, PID: 5, Status: StatusStarting})
		if err != nil {
			return err
		}
		first = id
		if err := s.RecordProcessExit(tx, Event{Kind: EventExited, PID: 5, Status: StatusExited, ExitCode: 0}); err != nil {
			return err
		}
		return s.RecordProcessExit(tx, Event{Kind: EventExited, PID: 5, Status: StatusExited, ExitCode: 0})
	})
	id := first
	if ev := eventRows(t, d, id); len(ev) != 2 {
		t.Fatalf("events = %+v, want the replay to book no third event", ev)
	}
	if len(*shouted) != 1 || !strings.Contains((*shouted)[0], "moved no row") {
		t.Fatalf("shout = %q, want exactly one 0-row line (幂等 and 从来没成功过 must stay separable)", *shouted)
	}
	if !strings.Contains((*shouted)[0], "STARTING") || !strings.Contains((*shouted)[0], id) {
		t.Errorf("shout = %q, want the expected from-state and the row key", (*shouted)[0])
	}
}

// The other half of CO-44: an edge whose row was never landed has no subject for an
// aggregate event, so it must not write one - and it must hand the caller something
// distinguishable from a write failure, because that something is an audit fact.
func TestEdgeWithoutARowWritesNothingAndReturnsAuditSentinel(t *testing.T) {
	logf, shouted := shoutRecorder()
	s, d, ids := openProcessStore(t, testRow(), logf)
	err := s.RecordProcessExit(nil, Event{Kind: EventExited, PID: 99, Status: StatusExited, ExitCode: 1})
	if !errors.Is(err, ErrNoLifecycleRow) {
		t.Fatalf("err = %v, want ErrNoLifecycleRow so the assembler can route it to audit", err)
	}
	if len(*shouted) != 1 {
		t.Errorf("shout = %q, want the missing row shouted, not swallowed", *shouted)
	}
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM process_instances`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(*ids) != 0 {
		t.Fatalf("rows = %d ids = %v, want no row and no id minted for an edge", n, *ids)
	}
}

func TestWriteCellRefusesUnlandableRowFacts(t *testing.T) {
	mint := func() (string, error) { return "x", nil }
	stamp := func() int64 { return 1 }
	cases := []struct {
		name string
		row  ProcessRow
		idf  func() (string, error)
		nowf func() int64
		want string
	}{
		{"kind outside what we supervise", ProcessRow{Kind: Kind("BROWSER"), RestartPolicy: RestartNever}, mint, stamp, "process_type"},
		{"unregistered owner domain", ProcessRow{Kind: KindWorker, OwnerType: "project", OwnerID: "p-1", RestartPolicy: RestartNever}, mint, stamp, "owner_type"},
		{"half-known owner", ProcessRow{Kind: KindWorker, OwnerType: "adapter_instance", RestartPolicy: RestartNever}, mint, stamp, "owner_id"},
		{"restart policy off the CHECK list", ProcessRow{Kind: KindAdapter, RestartPolicy: RestartPolicy("MAYBE")}, mint, stamp, "restart_policy"},
		{"no id source", testRow(), nil, stamp, "id source"},
		{"no clock", testRow(), mint, nil, "clock"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewLifecycleStore(c.row, c.idf, c.nowf, nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one naming %q", err, c.want)
			}
		})
	}
	if _, err := NewLifecycleStore(testRow(), mint, stamp, nil); err != nil {
		t.Fatalf("a legal row must construct: %v", err)
	}
}

// The write cell is the reason this package imports the platform at all, so the boundary
// that used to live in a comment is enforced here: pump and Supervisor stay persistence-free,
// and the only door is this file.
func TestOnlyTheWriteCellTouchesThePlatform(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"lifecycle.go": true}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		checked++
		for _, imp := range importsOf(t, f) {
			if !strings.Contains(imp, "/internal/platform/db") && !strings.Contains(imp, "/internal/eventjournal") {
				continue
			}
			if !allowed[f] {
				t.Errorf("%s imports %s - the persistence surface is lifecycle.go only (T-211/CO-43)", f, imp)
			}
		}
	}
	if checked < 2 {
		t.Fatalf("scanned %d non-test files; the guard is looking at the wrong directory", checked)
	}
}
