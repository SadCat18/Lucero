package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/eventjournal"
	"github.com/SadCat18/lucero/core/internal/permission"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/google/uuid"
)

const (
	itNow  int64 = 1_700_000_000_000
	itHour int64 = 3600 * 1000
	itTask       = "task-1"
	itExec       = "exec-1"
)

// openMigrated stands up a real SQLite file through the platform layer, with
// redaction initialised straight from audit.Rules(). That call site is the
// evidence for REQ-A1: whatever shape InitRedaction keeps, the rules must be the
// ones audit exports, not a second hand-written list.
func openMigrated(t *testing.T) *db.DB {
	t.Helper()
	return openMigratedAt(t, filepath.Join(t.TempDir(), "lucero.db"))
}

// openMigratedAt exists so a test can open the same file twice and prove an
// answer came from disk rather than from anything cached in a live handle.
func openMigratedAt(t *testing.T, path string) *db.DB {
	t.Helper()
	r := audit.Rules()
	if err := db.InitRedaction(r.DenyKeys, r.Patterns, r.Marker); err != nil {
		t.Fatalf("InitRedaction: %v", err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return d
}

// seed creates the FK parents an approval needs. principals has no aggregate
// kind, so there is no Create path for it and the fixture inserts directly.
func seed(t *testing.T, d *db.DB) {
	t.Helper()
	ctx := context.Background()
	for _, p := range [][2]string{{"local-user", "USER"}, {"system", "SYSTEM"}} {
		if _, err := d.Read.ExecContext(ctx, `INSERT INTO principals
			(id, principal_type, display_name, created_at, updated_at) VALUES (?,?,?,?,?)`,
			p[0], p[1], p[0], itNow, itNow); err != nil {
			t.Fatalf("seed principal %s: %v", p[0], err)
		}
	}
	err := d.Write(ctx, func(tx *db.Tx) error {
		if err := tx.Create(db.AggAdapter, "ad-1",
			[]string{"adapter_key", "display_name", "created_at", "updated_at"},
			[]any{"fake", "Fake", itNow, itNow},
			db.EventDraft{EventID: newID(t), Type: eventjournal.AdapterRegistered, AggregateID: "ad-1"}); err != nil {
			return err
		}
		if err := tx.Create(db.AggTask, itTask,
			[]string{"title", "created_at", "updated_at"},
			[]any{"t", itNow, itNow},
			db.EventDraft{EventID: newID(t), Type: eventjournal.TaskCreated, AggregateID: itTask}); err != nil {
			return err
		}
		for _, id := range []string{itExec, "exec-2"} {
			if err := tx.Create(db.AggExecution, id,
				[]string{"task_id", "adapter_instance_id", "created_at", "updated_at"},
				[]any{itTask, "ad-1", itNow, itNow},
				db.EventDraft{EventID: newID(t), Type: eventjournal.ExecutionCreated, AggregateID: id}); err != nil {
				return err
			}
		}
		// approvals.session_id references sessions, and sessions needs an
		// adapter link, so the slice's FK chain ends here.
		return tx.Create(db.AggSession, "s-1",
			[]string{"adapter_instance_id", "remote_session_id", "created_at", "updated_at"},
			[]any{"ad-1", "remote-1", itNow, itNow},
			db.EventDraft{EventID: newID(t), Type: eventjournal.SessionDiscovered, AggregateID: "s-1"})
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func newID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func countRows(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", q, err)
	}
	return n
}

func strRows(t *testing.T, d *db.DB, q string, args ...any) []string {
	t.Helper()
	rows, err := d.Read.Query(q, args...)
	if err != nil {
		t.Fatalf("query %s: %v", q, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// readAudit reloads the whole chain in seq order, mapping NULL back to the empty
// string the writer used, so VerifyChain re-derives exactly what was sealed.
func readAudit(t *testing.T, d *db.DB) []audit.Entry {
	t.Helper()
	rows, err := d.Read.Query(`SELECT seq, audit_id, COALESCE(principal_id,''), action,
		COALESCE(resource_type,''), COALESCE(resource_id,''), COALESCE(project_id,''),
		COALESCE(task_id,''), COALESCE(execution_id,''), COALESCE(correlation_id,''),
		outcome, details_json, COALESCE(previous_hash,''), entry_hash, COALESCE(signature,''),
		created_at FROM audit_entries ORDER BY seq`)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	defer rows.Close()
	var out []audit.Entry
	for rows.Next() {
		var e audit.Entry
		if err := rows.Scan(&e.Seq, &e.AuditID, &e.PrincipalID, &e.Action, &e.ResourceType,
			&e.ResourceID, &e.ProjectID, &e.TaskID, &e.ExecutionID, &e.CorrelationID,
			&e.Outcome, &e.DetailsJSON, &e.PreviousHash, &e.EntryHash, &e.Signature, &e.CreatedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func draft() Draft {
	return Draft{
		PrincipalID: "local-user", TaskID: itTask, ExecutionID: itExec,
		Type: "EXECUTION_APPROVAL", RiskLevel: "HIGH",
		Action: "filesystem.write", Resource: "repo:src/main.go",
	}
}

func TestStoreWritesAggregateEventsAndChain(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	st := NewSQLiteStore(d, func() int64 { return itNow })
	svc := NewService(st, func() int64 { return itNow })

	a, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE aggregate_type='approval' AND event_type=?`, eventjournal.ApprovalRequested); got != 1 {
		t.Fatalf("%d approval_requested events, want 1", got)
	}
	stored, err := st.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.State != PENDING || stored.Version != 1 || stored.ExecutionID != itExec {
		t.Errorf("stored row = %+v", stored)
	}

	res := Resolution{
		State: APPROVED, By: "local-user",
		Temporary: &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: itTask, Duration: 3 * itHour},
	}
	if err := svc.Resolve(ctx, a.ID, res); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// The second click, in whatever direction, must be inert.
	if err := svc.Resolve(ctx, a.ID, Resolution{State: REJECTED, By: "local-user"}); err != nil {
		t.Fatalf("second Resolve: %v", err)
	}

	after, err := st.Get(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != APPROVED || after.Version != 2 || after.ResolvedBy != "local-user" {
		t.Errorf("after resolve = %+v", after)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE aggregate_type='approval' AND event_type=?`, eventjournal.ApprovalResolved); got != 1 {
		t.Errorf("%d approval_resolved events, want exactly 1 for two Resolve calls", got)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE aggregate_type='approval'`); got != 2 {
		t.Errorf("%d approval events in total, want 2", got)
	}
	var v int64
	if err := d.Read.QueryRow(`SELECT aggregate_version FROM event_journal WHERE event_type=?`, eventjournal.ApprovalResolved).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 2 {
		t.Errorf("resolved event aggregate_version = %d, want 2", v)
	}

	var grantID, scopeType, scopeID, principal, action, effect string
	var startsAt, expiresAt, createdAt int64
	err = d.Read.QueryRow(`SELECT id, principal_id, scope_type, COALESCE(scope_id,''), action, effect,
		starts_at, COALESCE(expires_at,0), created_at FROM temporary_grants`).
		Scan(&grantID, &principal, &scopeType, &scopeID, &action, &effect, &startsAt, &expiresAt, &createdAt)
	if err != nil {
		t.Fatalf("the ALLOW_TEMPORARY decision wrote no grant row: %v", err)
	}
	if scopeType != "TASK" || scopeID != itTask || expiresAt != itNow+3*itHour || effect != "ALLOW" || principal != "local-user" {
		t.Errorf("grant row = %s/%s expires=%d effect=%s principal=%s", scopeType, scopeID, expiresAt, effect, principal)
	}
	if !strings.Contains(after.ResolutionJSON, grantID) {
		t.Errorf("approval_resolved payload/resolution lost the grant link: %s", after.ResolutionJSON)
	}
	// T-13: the grant is its own aggregate, keyed by the grant row's own id.
	var grantAggID, grantAggType string
	err = d.Read.QueryRow(`SELECT aggregate_type, aggregate_id FROM event_journal WHERE event_type=?`,
		eventjournal.GrantIssued).Scan(&grantAggType, &grantAggID)
	if err != nil {
		t.Fatalf("no grant_issued event: %v", err)
	}
	if grantAggType != "grant" || grantAggID != grantID {
		t.Errorf("grant_issued aggregate = %s/%s, want grant/%s", grantAggType, grantAggID, grantID)
	}
	var grantPayload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE event_type=?`, eventjournal.GrantIssued).Scan(&grantPayload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"scope_type":"TASK"`, `"scope_id":"` + itTask + `"`, `"action":"filesystem.write"`} {
		if !strings.Contains(grantPayload, want) {
			t.Errorf("grant_issued payload missing %s: %s", want, grantPayload)
		}
	}

	entries := readAudit(t, d)
	// One row per state change here: nothing was scrubbed, so no redaction row.
	if len(entries) != 2 {
		t.Fatalf("%d audit rows, want 2 (request + resolve): %+v", len(entries), entries)
	}
	for i, want := range []string{eventjournal.ApprovalRequested, eventjournal.ApprovalResolved} {
		if entries[i].Action != want {
			t.Errorf("audit row %d action = %s, want %s", i, entries[i].Action, want)
		}
	}
	head, err := audit.Checkpoint(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.VerifyChain(entries, head); err != nil {
		t.Fatalf("the stored chain does not verify: %v", err)
	}
	tampered := append([]audit.Entry{}, entries...)
	tampered[1].Outcome = audit.OutcomeDenied
	if err := audit.VerifyChain(tampered, head); err == nil {
		t.Error("rewriting a stored audit row passed verification")
	}
	if err := audit.VerifyChain(entries[:len(entries)-1], head); err == nil {
		t.Error("deleting the newest stored audit row passed verification")
	}

	// A fresh service over the same rows must agree the execution is unblocked.
	if state, err := NewService(NewSQLiteStore(d, func() int64 { return itNow }), func() int64 { return itNow }).BlockingState(ctx, itExec); err != nil || state != APPROVED {
		t.Errorf("BlockingState = %s, %v", state, err)
	}
}

func TestStoreRedactsSecretsBeforeTheyLand(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	svc := NewService(NewSQLiteStore(d, func() int64 { return itNow }), func() int64 { return itNow })

	const (
		openAI  = "sk-Zx9QZx9QZx9QZx9QZx9QZx9Q"
		gitHub  = "ghp_A1bCA1bCA1bCA1bCA1bCA1bCA1bCA1bC"
		vaulted = "vault://openai/prod"
	)
	detail, err := json.Marshal(map[string]any{
		"api_key":    openAI,
		"note":       "the adapter offered " + gitHub + " instead",
		"secret_ref": vaulted,
	})
	if err != nil {
		t.Fatal(err)
	}
	dd := draft()
	dd.Detail = detail
	dd.Resource = vaulted
	if _, err := svc.Request(ctx, dd); err != nil {
		t.Fatalf("Request: %v", err)
	}

	stored := strings.Join(strRows(t, d, `SELECT request_json FROM approvals`), "\n")
	for _, s := range []string{openAI, gitHub} {
		if strings.Contains(stored, s) {
			t.Errorf("secret reached the database in the clear: %s in %s", s, stored)
		}
	}
	if !strings.Contains(stored, audit.DefaultMarker) {
		t.Errorf("no marker in stored request_json: %s", stored)
	}
	if !strings.Contains(stored, vaulted) {
		t.Errorf("vault reference was destroyed: %s", stored)
	}

	hits := strRows(t, d, `SELECT details_json FROM audit_entries WHERE action=?`, "redaction."+eventjournal.ApprovalRequested)
	if len(hits) != 1 {
		t.Fatalf("%d redaction audit rows, want 1", len(hits))
	}
	for _, s := range []string{openAI, gitHub} {
		if strings.Contains(hits[0], s) {
			t.Errorf("the audit row carries the value: %s", hits[0])
		}
	}
	// The exact column spelling is db's formatting choice, so only "column" is
	// asserted. kind+match ARE asserted: mis-attributing a deny-key hit as a
	// regex hit is the silent, unfixable-after-the-fact error REQ-T3 exists for.
	for _, want := range []string{`"count"`, `"column"`, `"kind":"key"`, `"match":"apikey"`} {
		if !strings.Contains(hits[0], want) {
			t.Errorf("audit redaction row missing %s: %s", want, hits[0])
		}
	}
	// The rule (audit.IsVaultRef) and the enforcement point (db's own copy of it)
	// must agree, or the two definitions drift. Q6's shape: a key wearing a
	// reference form is not a reference.
	t.Run("key wearing a vault reference is redacted end to end", func(t *testing.T) {
		d := openMigrated(t)
		seed(t, d)
		svc := NewService(NewSQLiteStore(d, func() int64 { return itNow }), func() int64 { return itNow })
		const smuggled = "vault://sk-Zx9QZx9QZx9QZx9QZx9Q"
		if audit.IsVaultRef(smuggled) {
			t.Fatal("audit's rule exempts a key wearing a reference")
		}
		dd := draft()
		dd.Detail = json.RawMessage(`{"api_key":"` + smuggled + `"}`)
		if _, err := svc.Request(ctx, dd); err != nil {
			t.Fatal(err)
		}
		stored := strRows(t, d, `SELECT request_json FROM approvals`)[0]
		if strings.Contains(stored, smuggled) {
			t.Errorf("the enforcement point exempted it while the rule refused: %s", stored)
		}
		if !strings.Contains(stored, audit.DefaultMarker) {
			t.Errorf("nothing was replaced: %s", stored)
		}
	})
	// The audit row is itself a TEXT column, so it goes through the redactor too;
	// if a rule name ever looked like a secret it would be rewritten here.
	var ruleNames string
	if err := d.Read.QueryRow(`SELECT details_json FROM audit_entries WHERE action=?`, "redaction."+eventjournal.ApprovalRequested).Scan(&ruleNames); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ruleNames, "api_key") && !strings.Contains(ruleNames, "apikey") {
		t.Errorf("redaction record does not name the rule that fired: %s", ruleNames)
	}
}

// P0 "Approval 应用于错误 Task/Execution": nothing may read as approved unless the
// execution ids match exactly.
func TestStoreNeverBindsAnUnrelatedApproval(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	clock := func() int64 { return itNow }
	svc := NewService(NewSQLiteStore(d, clock), clock)

	unbound, err := svc.Request(ctx, func() Draft { dd := draft(); dd.ExecutionID = ""; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.Request(ctx, func() Draft { dd := draft(); dd.ExecutionID = "exec-2"; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{unbound.ID, other.ID} {
		if err := svc.Resolve(ctx, id, Resolution{State: APPROVED, By: "local-user"}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := svc.BlockingState(ctx, itExec)
	if err != nil {
		t.Fatal(err)
	}
	if state != StateNone {
		t.Errorf("BlockingState(exec-1) = %s, want NONE: approvals exist but none is bound to this execution", state)
	}
	if list, err := svc.ForExecution(ctx, itExec); err != nil || len(list) != 0 {
		t.Errorf("ForExecution(exec-1) = %d rows, %v", len(list), err)
	}
	if state, err := svc.BlockingState(ctx, "exec-2"); err != nil || state != APPROVED {
		t.Errorf("BlockingState(exec-2) = %s, %v", state, err)
	}
}

func TestStoreExpirePending(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	st := NewSQLiteStore(d, func() int64 { return itNow })
	svc := NewService(st, func() int64 { return itNow })

	due, err := svc.Request(ctx, func() Draft { dd := draft(); dd.ExpiresAt = itNow + 10; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	deadlineless, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	decided, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Resolve(ctx, decided.ID, Resolution{State: REJECTED, By: "local-user"}); err != nil {
		t.Fatal(err)
	}

	if err := st.ExpirePending(ctx, itNow+11); err != nil {
		t.Fatalf("ExpirePending: %v", err)
	}
	if a, _ := st.Get(ctx, due.ID); a.State != EXPIRED || a.Version != 2 {
		t.Errorf("due approval = %+v, want EXPIRED v2", a)
	}
	if a, _ := st.Get(ctx, deadlineless.ID); a.State != PENDING {
		t.Errorf("an approval with no deadline became %s", a.State)
	}
	if a, _ := st.Get(ctx, decided.ID); a.State != REJECTED {
		t.Errorf("a decided approval became %s — sweeps must not rewrite history", a.State)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.ApprovalExpired); got != 1 {
		t.Errorf("%d approval_expired events, want 1", got)
	}
	// Sweeping again must be inert: no second event for an already EXPIRED row.
	if err := st.ExpirePending(ctx, itNow+12); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.ApprovalExpired); got != 1 {
		t.Errorf("%d approval_expired events after a second sweep", got)
	}

	// Grant回收 is now real (AggGrant landed in 000003), so the sweep must not
	// report a failure it no longer has.
	if err := svc.SweepExpired(ctx); err != nil {
		t.Errorf("SweepExpired: %v", err)
	}
}

// Six simultaneous clicks is the double-click bug at the scale a real database
// can show it. Exactly one of them may take a version.
func TestConcurrentResolveSingleEvent(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	clock := func() int64 { return itNow }
	st := NewSQLiteStore(d, clock)
	svc := NewService(st, clock)
	a, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = svc.Resolve(ctx, a.ID, Resolution{State: APPROVED, By: "local-user"})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Resolve %d failed: %v", i, err)
		}
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.ApprovalResolved); got != 1 {
		t.Errorf("%d approval_resolved events from 6 concurrent calls, want 1", got)
	}
	if got := countRows(t, d, `SELECT version FROM approvals`); got != 2 {
		t.Errorf("approvals.version = %d, want 2 (one request, one decision)", got)
	}
}

// B3 closure: 到期授权真的被回收——写 revoked_at、发一条 grant_expired、
// 版本进一格、审计链仍然自洽。再 sweep 一次不得重复产生事件。
func TestSweepReclaimsGrants(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	clock := func() int64 { return itNow }
	st := NewSQLiteStore(d, clock)
	svc := NewService(st, clock)

	a, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	err = svc.Resolve(ctx, a.ID, Resolution{State: APPROVED, By: "local-user",
		Temporary: &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: itTask, Duration: 10}})
	if err != nil {
		t.Fatal(err)
	}
	var grantID string
	if err := d.Read.QueryRow(`SELECT id FROM temporary_grants`).Scan(&grantID); err != nil {
		t.Fatal(err)
	}
	if revoked := strRows(t, d, `SELECT COALESCE(revoked_at,-1) FROM temporary_grants`)[0]; revoked != "-1" {
		t.Fatalf("brand new grant already revoked: %s", revoked)
	}

	later := NewSQLiteStore(d, func() int64 { return itNow + 20 })
	if err := NewService(later, func() int64 { return itNow + 20 }).SweepExpired(ctx); err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if revoked := strRows(t, d, `SELECT COALESCE(revoked_at,-1) FROM temporary_grants`)[0]; revoked != fmt.Sprint(itNow+20) {
		t.Errorf("revoked_at = %s, want %d", revoked, itNow+20)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.GrantExpired); got != 1 {
		t.Fatalf("%d grant_expired events, want 1", got)
	}
	var aggType, aggID string
	if err := d.Read.QueryRow(`SELECT aggregate_type, aggregate_id FROM event_journal WHERE event_type=?`,
		eventjournal.GrantExpired).Scan(&aggType, &aggID); err != nil {
		t.Fatal(err)
	}
	if aggType != "grant" || aggID != grantID {
		t.Errorf("grant_expired aggregate = %s/%s, want grant/%s (T-13: 用授权行自身 id)", aggType, aggID, grantID)
	}
	if got := countRows(t, d, `SELECT version FROM temporary_grants`); got != 2 {
		t.Errorf("grant version = %d, want 2 (issued then expired)", got)
	}

	if err := NewService(later, func() int64 { return itNow + 30 }).SweepExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.GrantExpired); got != 1 {
		t.Errorf("%d grant_expired events after a second sweep, want still 1", got)
	}

	entries := readAudit(t, d)
	head, err := audit.Checkpoint(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.VerifyChain(entries, head); err != nil {
		t.Errorf("chain broke across the sweep: %v", err)
	}
	if countRows(t, d, `SELECT count(*) FROM audit_entries WHERE action=?`, eventjournal.GrantExpired) != 1 {
		t.Error("the sweep wrote no audit row for the reclaimed grant")
	}
}

// REQ-T3 guard: the kind tag crosses a package boundary as a string conversion,
// so the two constant sets must say the same thing.
func TestHitKindsAgreeAcrossPackages(t *testing.T) {
	if string(audit.HitKey) != string(db.HitKey) || string(audit.HitPattern) != string(db.HitPattern) {
		t.Errorf("audit {%s,%s} and db {%s,%s} disagree; hits would be mis-attributed in the audit row",
			audit.HitKey, audit.HitPattern, db.HitKey, db.HitPattern)
	}
}

func TestStoreGetMissing(t *testing.T) {
	d := openMigrated(t)
	if _, err := NewSQLiteStore(d, func() int64 { return itNow }).Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// REQ-A8: "a temporary grant must have a bounded lifetime" is ONE rule
// (grantWindow) consulted at both layers, not two implementations of it. This
// drives each window down the Service leg and straight through the store's write
// gate, and requires the same verdict and the same row count both times. A
// hand-written copy of the rule on either side shows up here as one row
// disagreeing — which is exactly the drift the two layers used to invite.
func TestGrantWindowVerdictsMatchAcrossLayers(t *testing.T) {
	ctx := context.Background()
	for _, w := range []struct {
		name                string
		startsAt, expiresAt int64
		wantErr             bool
	}{
		{"one hour", itNow, itNow + itHour, false},
		{"exactly at the cap", itNow, itNow + MaxGrantDuration, false},
		{"one ms over the cap", itNow, itNow + MaxGrantDuration + 1, true},
		{"no end at all", itNow, 0, true},
		{"ends the moment it starts", itNow, itNow, true},
		{"ends before it starts", itNow, itNow - 1, true},
	} {
		wantRows := 1
		if w.wantErr {
			wantRows = 0
		}
		t.Run(w.name, func(t *testing.T) {
			// Service leg: the duration the user actually clicks.
			sd := openMigrated(t)
			seed(t, sd)
			svcClock := func() int64 { return w.startsAt }
			svc := NewService(NewSQLiteStore(sd, svcClock), svcClock)
			svcA, err := svc.Request(ctx, draft())
			if err != nil {
				t.Fatalf("service Request: %v", err)
			}
			svcErr := svc.Resolve(ctx, svcA.ID, Resolution{State: APPROVED, By: "local-user",
				Temporary: &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: itTask,
					Duration: w.expiresAt - w.startsAt}})
			if rows := countRows(t, sd, `SELECT count(*) FROM temporary_grants`); rows != wantRows {
				t.Errorf("service leg wrote %d grant rows, want %d", rows, wantRows)
			}

			// Store leg: the same absolute window handed to the write gate with no
			// Service anywhere in sight.
			td := openMigrated(t)
			seed(t, td)
			raw := NewSQLiteStore(td, func() int64 { return itNow })
			rawA, err := NewService(raw, func() int64 { return itNow }).Request(ctx, draft())
			if err != nil {
				t.Fatalf("store leg Request: %v", err)
			}
			_, storeErr := raw.MarkResolved(ctx, rawA, Resolution{State: APPROVED, By: "local-user"},
				permission.Grant{ID: newID(t), PrincipalID: "local-user",
					ScopeType: permission.ScopeTask, ScopeID: itTask,
					Action: "filesystem.write", Effect: permission.Allow,
					StartsAt: w.startsAt, ExpiresAt: w.expiresAt}, itNow)
			if rows := countRows(t, td, `SELECT count(*) FROM temporary_grants`); rows != wantRows {
				t.Errorf("store leg wrote %d grant rows, want %d", rows, wantRows)
			}

			if (svcErr != nil) != w.wantErr {
				t.Errorf("service leg: err = %v, wantErr = %v", svcErr, w.wantErr)
			}
			if (storeErr != nil) != w.wantErr {
				t.Errorf("store leg: err = %v, wantErr = %v", storeErr, w.wantErr)
			}
			if !w.wantErr {
				// The contract's own words: the row carries an expiry, not a NULL.
				if n := countRows(t, td, `SELECT count(*) FROM temporary_grants WHERE expires_at IS NOT NULL`); n != 1 {
					t.Errorf("accepted grant has no expires_at (%d rows with one), want 1", n)
				}
			}
		})
	}
}

// The P0 shape REQ-A8 was really about: a grant with no end must never reach the
// table, even when a caller bypasses the Service and hands the store a Grant it
// built itself. The rejection is the whole transaction failing, not a skipped
// insert — the approval stays PENDING and nothing else landed.
func TestStoreRefusesToMintAPermanentGrant(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	clock := func() int64 { return itNow }
	st := NewSQLiteStore(d, clock)
	a, err := NewService(st, clock).Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkResolved(ctx, a, Resolution{State: APPROVED, By: "local-user"},
		permission.Grant{ID: "grant-eternal", PrincipalID: "local-user",
			ScopeType: permission.ScopeTask, ScopeID: itTask, Action: "filesystem.write",
			Effect: permission.Allow, StartsAt: itNow}, itNow); err == nil {
		t.Fatal("the store wrote a grant with no expires_at")
	} else if !strings.Contains(err.Error(), "must end after it starts") {
		t.Errorf("err = %v, want grantWindow's own message", err)
	}
	if got := strRows(t, d, `SELECT state FROM approvals WHERE id=?`, a.ID)[0]; got != PENDING {
		t.Errorf("approvals.state = %s after the refused write, want %s", got, PENDING)
	}
	if n := countRows(t, d, `SELECT count(*) FROM temporary_grants`); n != 0 {
		t.Errorf("%d grant rows from a refused write", n)
	}
	for _, ev := range []string{eventjournal.ApprovalResolved, eventjournal.GrantIssued} {
		if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, ev); n != 0 {
			t.Errorf("%s: %d events from a refused write, want 0", ev, n)
		}
	}
	// Only the approval_requested audit row survives: a refused write may not
	// leave a trail that claims the decision happened.
	entries := readAudit(t, d)
	if len(entries) != 1 {
		t.Errorf("%d audit entries, want the 1 from Request", len(entries))
	}
	head, err := audit.Checkpoint(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.VerifyChain(entries, head); err != nil {
		t.Errorf("chain broke across the refused write: %v", err)
	}
}

// Approval.DeadlinePassed is the Go spelling of "out of time" and ExpirePending's
// `expires_at <= ?` is the SQL spelling of it. The sweep re-decides each row with
// the Go predicate, so a </<= change on one side alone cannot leave the two
// layers disagreeing about the boundary millisecond — this case is red if they do.
func TestSweepAgreesWithDeadlineBoundary(t *testing.T) {
	ctx := context.Background()
	deadline := itNow + itHour
	for _, tc := range []struct {
		name      string
		at        int64
		wantLive  bool
		wantEvent int
	}{
		{"one ms before the deadline", deadline - 1, true, 0},
		{"exactly at the deadline", deadline, false, 1},
		{"one ms after", deadline + 1, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := openMigrated(t)
			seed(t, d)
			createClock := func() int64 { return itNow }
			dd := draft()
			dd.ExpiresAt = deadline
			a, err := NewService(NewSQLiteStore(d, createClock), createClock).Request(ctx, dd)
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := NewSQLiteStore(d, createClock).Get(ctx, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := fresh.Live(tc.at); got != tc.wantLive {
				t.Errorf("Live(%d) = %v, want %v", tc.at, got, tc.wantLive)
			}
			if got := fresh.DeadlinePassed(tc.at); got == tc.wantLive {
				t.Errorf("DeadlinePassed(%d) = %v, which contradicts Live", tc.at, got)
			}

			sweepClock := func() int64 { return tc.at }
			if err := NewService(NewSQLiteStore(d, sweepClock), sweepClock).SweepExpired(ctx); err != nil {
				t.Fatalf("SweepExpired: %v", err)
			}
			if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`,
				eventjournal.ApprovalExpired); n != tc.wantEvent {
				t.Errorf("%d approval_expired events, want %d", n, tc.wantEvent)
			}
			want := PENDING
			if tc.wantEvent == 1 {
				want = EXPIRED
			}
			if got := strRows(t, d, `SELECT state FROM approvals WHERE id=?`, a.ID)[0]; got != want {
				t.Errorf("after the sweep approvals.state = %s, want %s", got, want)
			}
		})
	}
}
