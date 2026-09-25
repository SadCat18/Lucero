package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// REQ-DB-2 (契约 §1.1 items 17-18): process_instances and the Core process have
// no version column, so their events are numbered by the events already stored
// for the aggregate, and the row and the event live or die together.

// startProcess runs entry ①: the row and its process_started event are one
// transaction, which is the only legal way to create this table's rows.
// The status here is this fixture's own choice; what the real write cell stores
// is ev.Status (STARTING in v0.1, 契约 §1.1 item 20 clause ⑤) — this package
// never reads or writes a lifecycle status value, so no test here pins one.
func startProcess(t *testing.T, d *DB, id string) {
	t.Helper()
	startProcessAt(t, d, id, "RUNNING", "ps-"+id)
}

func startProcessAt(t *testing.T, d *DB, id, status, evID string) {
	t.Helper()
	now := int64(1700000000000)
	err := d.Write(context.Background(), func(tx *Tx) error {
		return tx.CreateVersionless(AggProcess, id,
			[]string{"process_type", "status", "restart_policy", "created_at", "updated_at"},
			[]any{"ADAPTER", status, "ON_FAILURE", now, now},
			EventDraft{EventID: evID, Type: "process_started", AggregateID: id})
	})
	if err != nil {
		t.Fatalf("CreateVersionless %s: %v", id, err)
	}
}

func processColumn(t *testing.T, d *DB, id, column string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow("SELECT "+column+" FROM process_instances WHERE id = ?", id).Scan(&s); err != nil {
		t.Fatalf("read %s for %s: %v", column, id, err)
	}
	return s
}

func countProcessRows(t *testing.T, d *DB) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM process_instances`).Scan(&n); err != nil {
		t.Fatalf("count process_instances: %v", err)
	}
	return n
}

func processStatus(t *testing.T, d *DB, id string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(`SELECT status FROM process_instances WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatalf("read status for %s: %v", id, err)
	}
	return s
}

// eventLog renders an aggregate's events as "version:type" in commit order.
func eventLog(t *testing.T, d *DB, aggType, aggID string) []string {
	t.Helper()
	rows, err := d.Read.Query(`SELECT aggregate_version, event_type FROM event_journal
		WHERE aggregate_type = ? AND aggregate_id = ? ORDER BY seq`, aggType, aggID)
	if err != nil {
		t.Fatalf("read events for %s %s: %v", aggType, aggID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v int64
		var typ string
		if err := rows.Scan(&v, &typ); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, fmt.Sprintf("%d:%s", v, typ))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	return out
}

// transition runs entry ② the way the lifecycle write cells do: the edge moves
// both the guard column and a fact column, so a SET that silently drops a column
// cannot pass.
func transition(t *testing.T, d *DB, id, from, to, policy, evType, evID string) int64 {
	t.Helper()
	var changed int64
	err := d.Write(context.Background(), func(tx *Tx) error {
		var err error
		changed, err = tx.TouchTransition(AggProcess, id, "status", from,
			"status = ?, restart_policy = ?, updated_at = ?",
			[]any{to, policy, int64(1700000000001)},
			EventDraft{EventID: evID, Type: evType, AggregateID: id})
		return err
	})
	if err != nil {
		t.Fatalf("TouchTransition %s %s→%s: %v", id, from, to, err)
	}
	return changed
}

// Dispatch item 4: the registry shape is what keeps RecordStart's door open, so a
// future "just add the mapping" edit must be caught here rather than as a
// build-red or a silent write failure in another package.
func TestVersionlessAggregatesStayOutOfAggregateRegistry(t *testing.T) {
	d := newMigrated(t)

	for _, tc := range []struct {
		kind  AggregateKind
		table string
	}{
		{AggProcess, "process_instances"},
		{AggCore, ""},
	} {
		if table, ok := aggTable[tc.kind]; ok {
			t.Errorf("aggTable[%q] = %q: Touch would look for a version column that table has never had", tc.kind, table)
		}
		spec, ok := versionlessAgg[tc.kind]
		if !ok {
			t.Fatalf("versionlessAgg is missing %q", tc.kind)
		}
		if spec.table != tc.table {
			t.Errorf("versionlessAgg[%q].table = %q; want %q", tc.kind, spec.table, tc.table)
		}
	}
	if aggTables["process_instances"] {
		t.Error("process_instances is in aggTables: it is a versionless aggregate, not a Touch one")
	}
	if !versionlessTables["process_instances"] {
		t.Error("versionlessTables is missing process_instances, so Append would accept its rows")
	}

	cols, err := d.columns("process_instances")
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	if cols["version"] {
		t.Error("process_instances has a version column: move AggProcess into aggTable and out of versionlessAgg")
	}
	// v0.1 guard whitelist is exactly process_instances:status, and core has no row.
	if got := len(versionlessAgg[AggProcess].guards); got != 1 || !versionlessAgg[AggProcess].guards["status"] {
		t.Errorf("process guards = %v; want exactly {status}", versionlessAgg[AggProcess].guards)
	}
	if got := len(versionlessAgg[AggCore].guards); got != 0 {
		t.Errorf("core guards = %v; want none (core has no table)", versionlessAgg[AggCore].guards)
	}

	// "Not in aggTables" is a shape fact, not a permission: Append must still be
	// closed here, while the row-less tables that legitimately have no event stay open.
	err = d.Write(context.Background(), func(tx *Tx) error {
		_, e := tx.Append("process_instances",
			[]string{"id", "process_type", "created_at", "updated_at"},
			[]any{"proc-orphan", "ADAPTER", int64(1), int64(1)})
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "CreateVersionless") {
		t.Errorf("Append(process_instances) = %v; want a refusal naming CreateVersionless", err)
	}
	t.Logf("refused as expected: %v", err)
	if n := countProcessRows(t, d); n != 0 {
		t.Errorf("process_instances rows after the refusal = %d; want 0", n)
	}
	if err := d.Write(context.Background(), func(tx *Tx) error {
		_, e := tx.Append("principals",
			[]string{"id", "principal_type", "display_name", "created_at", "updated_at"},
			[]any{"local-user", "USER", "me", int64(1), int64(1)})
		return e
	}); err != nil {
		t.Errorf("Append(principals) refused: %v; that table is legitimately event-less", err)
	}

	// Touch/Create must say which entry does work for these kinds.
	for _, a := range []AggregateKind{AggProcess, AggCore} {
		err := d.Write(context.Background(), func(tx *Tx) error {
			_, err := tx.Touch(a, "x", "status = ?", []any{"RUNNING"},
				EventDraft{EventID: "t-1", Type: "process_started", AggregateID: "x"})
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "TouchTransition") {
			t.Errorf("Touch(%q) = %v; want an error naming TouchTransition", a, err)
		}
		err = d.Write(context.Background(), func(tx *Tx) error {
			return tx.Create(a, "x", []string{"created_at", "updated_at"}, []any{int64(1), int64(1)},
				EventDraft{EventID: "t-2", Type: "process_started", AggregateID: "x"})
		})
		if err == nil || !strings.Contains(err.Error(), "AppendEventOnly") {
			t.Errorf("Create(%q) = %v; want an error naming AppendEventOnly", a, err)
		}
	}
}

// Entry ① : the row and its first event are one unit, so every refusal below has
// to leave the table empty — that is the half Append could not guarantee.
func TestCreateVersionlessBooksRowAndEventTogether(t *testing.T) {
	d := newMigrated(t)
	startProcess(t, d, "proc-cv")

	if got := processStatus(t, d, "proc-cv"); got != "RUNNING" {
		t.Fatalf("status = %q; want RUNNING", got)
	}
	if got := eventLog(t, d, "process", "proc-cv"); len(got) != 1 || got[0] != "1:process_started" {
		t.Fatalf("events = %v; want [1:process_started]", got)
	}

	cases := []struct {
		name    string
		kind    AggregateKind
		id      string
		columns []string
		values  []any
		evType  string
		want    string
	}{
		{"versioned aggregate", AggTask, "proc-x", []string{"title"}, []any{"t"}, "task_created", "not a versionless aggregate"},
		{"core has no table", AggCore, "core-x", []string{"pid"}, []any{int64(1)}, "core_ready", "use AppendEventOnly"},
		{"empty id", AggProcess, "", []string{"pid"}, []any{int64(1)}, "process_started", "empty id"},
		{"no columns", AggProcess, "proc-x", nil, nil, "process_started", "no columns"},
		{"values mismatch", AggProcess, "proc-x", []string{"pid", "status"}, []any{int64(1)}, "process_started", "2 columns but 1 values"},
		{"id is an argument", AggProcess, "proc-x", []string{"id"}, []any{"proc-x"}, "process_started", "id is a Create argument"},
		{"unknown column", AggProcess, "proc-x", []string{"nope"}, []any{1}, "process_started", "no such column process_instances.nope"},
		// These two get a schema-valid row, so the refusal can only come from the
		// event leg — and the row must then disappear with it.
		{"unregistered event type", AggProcess, "proc-x",
			[]string{"process_type", "status", "created_at", "updated_at"},
			[]any{"ADAPTER", "RUNNING", int64(1), int64(1)}, "process_restarting", "not in the event journal catalogue"},
		{"event about another row", AggProcess, "proc-x",
			[]string{"process_type", "status", "created_at", "updated_at"},
			[]any{"ADAPTER", "RUNNING", int64(1), int64(1)}, "process_started", "but the event is about"},
	}
	for _, tc := range cases {
		err := d.Write(context.Background(), func(tx *Tx) error {
			ev := EventDraft{EventID: "cv-" + tc.name, Type: tc.evType, AggregateID: tc.id}
			if tc.name == "event about another row" {
				ev.AggregateID = "someone-else"
			}
			return tx.CreateVersionless(tc.kind, tc.id, tc.columns, tc.values, ev)
		})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v; want one containing %q", tc.name, err, tc.want)
		}
		t.Logf("refused as expected: %v", err)
	}
	if n := countProcessRows(t, d); n != 1 {
		t.Errorf("process_instances rows = %d; want only the one good create", n)
	}
	if n := eventCount(t, d); n != 1 {
		t.Errorf("event_journal rows = %d; want only the one good event", n)
	}

	// A retry with the same pre-generated id cannot grow a second row or event.
	err := d.Write(context.Background(), func(tx *Tx) error {
		return tx.CreateVersionless(AggProcess, "proc-cv",
			[]string{"process_type", "status", "created_at", "updated_at"},
			[]any{"ADAPTER", "RUNNING", int64(2), int64(2)},
			EventDraft{EventID: "dup-start", Type: "process_started", AggregateID: "proc-cv"})
	})
	if err == nil || !strings.Contains(err.Error(), "process_instances.id") {
		t.Errorf("duplicate create = %v; want the primary key refusal", err)
	}
	t.Logf("duplicate create refused: %v", err)
	if got := eventLog(t, d, "process", "proc-cv"); len(got) != 1 {
		t.Errorf("events after the refused retry = %v; want the original single event", got)
	}
}

func TestTouchTransitionBooksOneEventPerEdge(t *testing.T) {
	d := newMigrated(t)
	startProcess(t, d, "proc-1")

	steps := []struct{ from, to, policy, evType, evID string }{
		{"RUNNING", "FAILED", "ON_FAILURE", "process_crashed", "pt-2"},
		{"FAILED", "RUNNING", "ALWAYS", "process_started", "pt-3"},
		{"RUNNING", "EXITED", "NEVER", "process_exited", "pt-4"},
	}
	for i, s := range steps {
		if got := transition(t, d, "proc-1", s.from, s.to, s.policy, s.evType, s.evID); got != 1 {
			t.Fatalf("edge %d changed %d rows; want 1", i+1, got)
		}
	}
	if got := processStatus(t, d, "proc-1"); got != "EXITED" {
		t.Fatalf("status = %q; want EXITED", got)
	}
	// restart_policy is in the same SET list as the guard, so a dropped column
	// shows up here rather than as a silently stale row.
	var policy string
	if err := d.Read.QueryRow(`SELECT restart_policy FROM process_instances WHERE id='proc-1'`).Scan(&policy); err != nil {
		t.Fatalf("read restart_policy: %v", err)
	}
	if policy != "NEVER" {
		t.Fatalf("restart_policy = %q; want NEVER", policy)
	}
	want := []string{"1:process_started", "2:process_crashed", "3:process_started", "4:process_exited"}
	if got := eventLog(t, d, "process", "proc-1"); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("events = %v; want %v", got, want)
	}
}

// Zero rows changed means the edge was already taken: the replay must not grow a
// second, ghost transition in the chain.
func TestTouchTransitionZeroRowsBooksNoEvent(t *testing.T) {
	d := newMigrated(t)
	startProcess(t, d, "proc-2")
	transition(t, d, "proc-2", "RUNNING", "EXITED", "NEVER", "process_exited", "rp-1")

	if got := transition(t, d, "proc-2", "RUNNING", "EXITED", "NEVER", "process_exited", "rp-replay"); got != 0 {
		t.Fatalf("replay changed %d rows; want 0", got)
	}
	if got := processStatus(t, d, "proc-2"); got != "EXITED" {
		t.Fatalf("replay moved the row to %q", got)
	}
	if got := eventLog(t, d, "process", "proc-2"); len(got) != 2 || got[1] != "2:process_exited" {
		t.Fatalf("events after replay = %v; want the create event plus one exit", got)
	}

	// A row that does not exist is the same answer, and no event either.
	if got := transition(t, d, "proc-missing", "RUNNING", "EXITED", "NEVER", "process_exited", "rp-ghost"); got != 0 {
		t.Fatalf("transition on a missing row changed %d; want 0", got)
	}
	if got := eventLog(t, d, "process", "proc-missing"); len(got) != 0 {
		t.Fatalf("events for a missing row = %v; want none", got)
	}
}

func TestTouchTransitionRefusesOutsideItsWhitelist(t *testing.T) {
	d := newMigrated(t)
	startProcess(t, d, "proc-3")

	cases := []struct {
		name  string
		kind  AggregateKind
		id    string
		guard string
		set   string
		args  []any
		ev    EventDraft
		want  string
	}{
		{"guard not whitelisted", AggProcess, "proc-3", "restart_policy", "status = ?", []any{"RUNNING"},
			EventDraft{EventID: "r-1", Type: "process_started", AggregateID: "proc-3"}, "not a guardable"},
		{"not versionless", AggTask, "proc-3", "status", "status = ?", []any{"RUNNING"},
			EventDraft{EventID: "r-2", Type: "task_running", AggregateID: "proc-3"}, "not a versionless aggregate"},
		{"core has no table", AggCore, "core-1", "status", "status = ?", []any{"RUNNING"},
			EventDraft{EventID: "r-3", Type: "core_ready", AggregateID: "core-1"}, "use AppendEventOnly"},
		{"unknown set column", AggProcess, "proc-3", "status", "nope = ?", []any{"x"},
			EventDraft{EventID: "r-4", Type: "process_started", AggregateID: "proc-3"}, "nope"},
		{"id is not settable", AggProcess, "proc-3", "status", "id = ?", []any{"other"},
			EventDraft{EventID: "r-5", Type: "process_started", AggregateID: "proc-3"}, "id is the row key"},
		{"version is not a column here", AggProcess, "proc-3", "status", "version = ?", []any{int64(2)},
			EventDraft{EventID: "r-8", Type: "process_started", AggregateID: "proc-3"}, "belongs to the platform"},
		{"args mismatch", AggProcess, "proc-3", "status", "status = ?, pid = ?", []any{"RUNNING"},
			EventDraft{EventID: "r-9", Type: "process_started", AggregateID: "proc-3"}, "2 columns but 1 args"},
		{"unregistered event type", AggProcess, "proc-3", "status", "status = ?", []any{"RUNNING"},
			EventDraft{EventID: "r-6", Type: "process_restarting", AggregateID: "proc-3"}, "not in the event journal catalogue"},
		{"empty id", AggProcess, "", "status", "status = ?", []any{"RUNNING"},
			EventDraft{EventID: "r-7", Type: "process_started"}, "empty id"},
	}
	for _, tc := range cases {
		err := d.Write(context.Background(), func(tx *Tx) error {
			_, err := tx.TouchTransition(tc.kind, tc.id, tc.guard, "RUNNING", tc.set, tc.args, tc.ev)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v; want one containing %q", tc.name, err, tc.want)
		}
		t.Logf("refused as expected: %v", err)
	}
	// None of those calls may have left a mark beyond the create event.
	if got := processStatus(t, d, "proc-3"); got != "RUNNING" {
		t.Errorf("status = %q after refused calls; want RUNNING", got)
	}
	if got := eventLog(t, d, "process", "proc-3"); len(got) != 1 {
		t.Errorf("events after refused calls = %v; want only the create event", got)
	}
}

func TestAppendEventOnlyNumbersCoreEvents(t *testing.T) {
	d := newMigrated(t)

	add := func(evID, evType, aggID string, kind AggregateKind) error {
		return d.Write(context.Background(), func(tx *Tx) error {
			return tx.AppendEventOnly(kind, aggID, EventDraft{EventID: evID, Type: evType, AggregateID: aggID})
		})
	}
	if err := add("core-1", "core_ready", "core-a", AggCore); err != nil {
		t.Fatalf("core_ready: %v", err)
	}
	if err := add("core-2", "core_shutdown", "core-a", AggCore); err != nil {
		t.Fatalf("core_shutdown: %v", err)
	}
	want := []string{"1:core_ready", "2:core_shutdown"}
	if got := eventLog(t, d, "core", "core-a"); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("core events = %v; want %v", got, want)
	}
	// Numbering is per aggregate, so a second core starts at 1 again.
	if err := add("core-3", "core_ready", "core-b", AggCore); err != nil {
		t.Fatalf("second core: %v", err)
	}
	if got := eventLog(t, d, "core", "core-b"); len(got) != 1 || got[0] != "1:core_ready" {
		t.Fatalf("core-b events = %v; want [1:core_ready]", got)
	}

	for _, tc := range []struct {
		name, evID, evType, aggID string
		kind                      AggregateKind
		want                      string
	}{
		{"versioned aggregate", "core-x", "task_running", "core-a", AggTask, "not a versionless aggregate"},
		{"empty id", "core-y", "core_ready", "", AggCore, "empty id"},
		{"unregistered type", "core-z", "core_restarted", "core-a", AggCore, "not in the event journal catalogue"},
	} {
		err := add(tc.evID, tc.evType, tc.aggID, tc.kind)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v; want one containing %q", tc.name, err, tc.want)
		}
		t.Logf("refused as expected: %v", err)
	}
	if got := eventLog(t, d, "core", "core-a"); len(got) != 2 {
		t.Errorf("core-a events after refusals = %v; want the original two", got)
	}
}

// Entry ③ is a gate, not a convenience: it exists because `core` has no row. Both
// sides are asserted here because "only the real data can go red" is half a check.
func TestAppendEventOnlyRefusesAggregatesThatOwnRows(t *testing.T) {
	d := newMigrated(t)
	startProcess(t, d, "proc-gate")

	err := d.Write(context.Background(), func(tx *Tx) error {
		return tx.AppendEventOnly(AggProcess, "proc-gate",
			EventDraft{EventID: "gate-1", Type: "process_exited", AggregateID: "proc-gate"})
	})
	if err == nil || !strings.Contains(err.Error(), "TouchTransition") {
		t.Errorf("AppendEventOnly(process) = %v; want a refusal naming TouchTransition/CreateVersionless", err)
	}
	t.Logf("refused as expected: %v", err)
	// The row and its event count must be exactly what they were before the call.
	if got := processStatus(t, d, "proc-gate"); got != "RUNNING" {
		t.Errorf("status = %q after the refusal; want RUNNING", got)
	}
	if got := eventLog(t, d, "process", "proc-gate"); len(got) != 1 {
		t.Errorf("events = %v; want only the create event", got)
	}

	if err := d.Write(context.Background(), func(tx *Tx) error {
		return tx.AppendEventOnly(AggCore, "core-gate",
			EventDraft{EventID: "gate-2", Type: "core_ready", AggregateID: "core-gate"})
	}); err != nil {
		t.Errorf("AppendEventOnly(core) = %v; want it to succeed — core is what this entry is for", err)
	}
	if got := eventLog(t, d, "core", "core-gate"); len(got) != 1 || got[0] != "1:core_ready" {
		t.Errorf("core-gate events = %v; want [1:core_ready]", got)
	}
}

// A UNIQUE collision on the counted version is the "another writer got there
// first" signal: the whole transaction has to go back, row change included, and
// the caller must not retry it into a second event.
func TestVersionlessEventConflictRollsBackTheWholeWrite(t *testing.T) {
	d := newMigrated(t)
	startProcess(t, d, "proc-4")

	// The gap that makes count+1 collide: a version already stored out of order,
	// which is what a concurrent commit looks like from here.
	err := d.Write(context.Background(), func(tx *Tx) error {
		if _, e := tx.conn.ExecContext(tx.ctx, insertEventSQL,
			"pre-1", "process_exited", "process", "proc-4", 3,
			nil, nil, nil, `{"schema":1}`, int64(1700000000000)); e != nil {
			return e
		}
		_, e := tx.TouchTransition(AggProcess, "proc-4", "status", "RUNNING",
			"status = ?", []any{"EXITED"},
			EventDraft{EventID: "clash-1", Type: "process_exited", AggregateID: "proc-4"})
		return e
	})
	if err == nil {
		t.Fatal("a UNIQUE(aggregate_type, aggregate_id, aggregate_version) collision was accepted")
	}
	if !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("expected the ledger's own conflict, got: %v", err)
	}
	t.Logf("counted version collided: %v", err)
	if got := processStatus(t, d, "proc-4"); got != "RUNNING" {
		t.Fatalf("status = %q after the rollback; want RUNNING — the row change survived", got)
	}
	// The pre-seeded row was in the same transaction, so it is gone too: only the
	// create event is left.
	if got := eventLog(t, d, "process", "proc-4"); len(got) != 1 || got[0] != "1:process_started" {
		t.Fatalf("events after the rollback = %v; want only the create event", got)
	}

	// The single writer is not parked: the same edge still applies afterwards.
	if got := transition(t, d, "proc-4", "RUNNING", "EXITED", "NEVER", "process_exited", "retry-1"); got != 1 {
		t.Fatalf("transition after the rollback changed %d rows; want 1", got)
	}
	if got := eventLog(t, d, "process", "proc-4"); len(got) != 2 || got[1] != "2:process_exited" {
		t.Fatalf("events after the clean retry = %v; want the create event then 2:process_exited", got)
	}
}

// Redaction runs on every leg, before the event is sealed into payload_json: the
// row values on entries ① and ②, and payload_json on all three.
func TestTouchTransitionRedactsBeforeSeal(t *testing.T) {
	d := newMigrated(t)
	const secret = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

	startErr := d.Write(context.Background(), func(tx *Tx) error {
		return tx.CreateVersionless(AggProcess, "proc-5",
			[]string{"process_type", "status", "executable", "created_at", "updated_at"},
			[]any{"ADAPTER", "RUNNING", "runner " + secret, int64(1), int64(1)},
			EventDraft{EventID: "sec-0", Type: "process_started", AggregateID: "proc-5",
				Payload: map[string]any{"argv": "run " + secret}})
	})
	if startErr != nil {
		t.Fatalf("CreateVersionless: %v", startErr)
	}
	if exec := processColumn(t, d, "proc-5", "executable"); strings.Contains(exec, secret) {
		t.Fatalf("entry ① leaked the secret into executable: %s", exec)
	}
	if p := payloadOf(t, d, "sec-0"); strings.Contains(p, secret) {
		t.Fatalf("entry ① leaked the secret into payload_json: %s", p)
	}

	var hits []RedactionHit
	err := d.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.TouchTransition(AggProcess, "proc-5", "status", "RUNNING",
			"metadata_json = ?, status = ?", []any{
				map[string]any{"note": "keep", "access_token": secret},
				"EXITED",
			},
			EventDraft{EventID: "sec-1", Type: "process_exited", AggregateID: "proc-5",
				Payload: map[string]any{"cmd": "run " + secret}})
		if err != nil {
			return err
		}
		hits = tx.RedactionHits()
		return nil
	})
	if err != nil {
		t.Fatalf("TouchTransition: %v", err)
	}

	var meta string
	if err := d.Read.QueryRow(`SELECT metadata_json FROM process_instances WHERE id='proc-5'`).Scan(&meta); err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if strings.Contains(meta, secret) {
		t.Fatalf("row column leaked the secret: %s", meta)
	}
	var payload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE event_id='sec-1'`).Scan(&payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if strings.Contains(payload, secret) {
		t.Fatalf("payload_json leaked the secret: %s", payload)
	}
	if !strings.Contains(payload, "[REDACTED]") || !strings.Contains(meta, "[REDACTED]") {
		t.Fatalf("expected redaction markers, got row=%s event=%s", meta, payload)
	}
	found := false
	for _, h := range hits {
		if h.Column == "process_instances.metadata_json" && h.Kind == HitKey && h.Match == "accesstoken" {
			found = true
		}
	}
	if !found {
		t.Fatalf("redaction hits = %+v; want the access_token deny-key on metadata_json", hits)
	}
}
