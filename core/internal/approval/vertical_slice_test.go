package approval

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/eventjournal"
	"github.com/SadCat18/lucero/core/internal/permission"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// The slice this role owns, end to end against a real database:
//
//	policy_rules -> Permission.Decide -> (REQUIRE_APPROVAL) -> approval.Request
//	-> user Resolve(APPROVED + temporary) -> Permission.Decide = ALLOW_TEMPORARY
//	-> expiry + SweepExpired -> Decide falls back to the §3.1 risk default
//
// §3.1 requirement 3 is why this exists: under the risk default the chain must
// start without an approval, and the one approval on it must be a real high-risk
// action the adapter asked for. Otherwise the §7 acceptance path exercises the
// fallback instead of the approval link.
func TestVerticalSlicePermissionToApprovalToGrant(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	now := itNow
	clock := func() int64 { return now }

	eng := permission.New(permission.NewSQLiteSource(d), clock)
	st := NewSQLiteStore(d, clock)
	svc := NewService(st, clock)

	rule := func(id, scope, scopeID, action, effect string, priority int) {
		t.Helper()
		if _, err := d.Read.Exec(`INSERT INTO policy_rules
			(id, scope_type, scope_id, action_pattern, effect, priority, enabled, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)`, id, scope, scopeID, action, effect, priority, 1, itNow, itNow); err != nil {
			t.Fatalf("seed rule: %v", err)
		}
	}
	decide := func(r permission.Request) (permission.Effect, permission.Reason) {
		t.Helper()
		eff, why, err := eng.Decide(ctx, r)
		if err != nil {
			t.Fatalf("Decide(%s): %v", r.Action, err)
		}
		return eff, why
	}

	base := permission.Request{
		PrincipalID: "local-user", TrustDomainID: "td-home", ProjectID: "p-1",
		TaskID: itTask, ExecutionID: itExec, SessionID: "s-1",
	}
	highRisk := base
	highRisk.Action, highRisk.Resource = "filesystem.write", "repo:src/main.go"
	startup := base
	startup.Action = "execution_started"
	sendMsg := base
	sendMsg.Action = "session.send"
	unheard := base
	unheard.Action = "telescope.align" // in neither built-in list

	// (1) The §3.1 default, on an empty policy_rules table, with nothing seeded.
	if eff, why := decide(startup); eff != permission.Allow || why != permission.ReasonDefaultAllow {
		t.Errorf("execution_started: %s/%s, want ALLOW/DEFAULT_ALLOW — otherwise the chain stalls before it starts", eff, why)
	}
	if eff, why := decide(sendMsg); eff != permission.Allow || why != permission.ReasonDefaultAllow {
		t.Errorf("session.send: %s/%s, want ALLOW/DEFAULT_ALLOW", eff, why)
	}
	if eff, why := decide(highRisk); eff != permission.RequireApproval || why != permission.ReasonDefaultHighRisk {
		t.Errorf("filesystem.write: %s/%s, want REQUIRE_APPROVAL/DEFAULT_HIGH_RISK", eff, why)
	}
	if eff, why := decide(unheard); eff != permission.RequireApproval || why != permission.ReasonDefaultUnknown {
		t.Errorf("unclassified action: %s/%s, want REQUIRE_APPROVAL/DEFAULT_UNKNOWN_RISK (不确定即失败关闭)", eff, why)
	}

	// (2) A rule still outranks the default: same action, opposite verdict, and
	// the reason names the row that decided it.
	rule("r-allow-write", "PROJECT", "p-1", "filesystem.write", "ALLOW", 0)
	if eff, why := decide(highRisk); eff != permission.Allow || why != "r-allow-write" {
		t.Errorf("rule must override the default: %s/%s, want ALLOW/r-allow-write", eff, why)
	}
	if _, err := d.Read.Exec(`DELETE FROM policy_rules`); err != nil {
		t.Fatal(err)
	}
	if eff, why := decide(highRisk); eff != permission.RequireApproval || why != permission.ReasonDefaultHighRisk {
		t.Errorf("after removing the rule: %s/%s", eff, why)
	}

	// (3) The high-risk action is what raises the approval — an adapter asking to
	// write to the repo, not a fallback inventing a prompt.
	a, err := svc.Request(ctx, Draft{
		PrincipalID: "local-user", TaskID: itTask, ExecutionID: itExec, SessionID: "s-1",
		Type: "EXECUTION_APPROVAL", RiskLevel: "HIGH", Action: highRisk.Action, Resource: highRisk.Resource,
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if state, err := svc.BlockingState(ctx, itExec); err != nil || state != PENDING {
		t.Fatalf("BlockingState while pending = %s %v", state, err)
	}
	if eff, _ := decide(highRisk); eff != permission.RequireApproval {
		t.Errorf("an open approval widened the verdict to %s", eff)
	}

	// (4) The user approves for the rest of the task.
	if err := svc.Resolve(ctx, a.ID, Resolution{
		State: APPROVED, By: "local-user", Note: "clicked full access for this task",
		Temporary: &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: itTask, Duration: itHour},
	}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if eff, why := decide(highRisk); eff != permission.AllowTemporary || why != permission.Reason(oneGrantID(t, d)) {
		t.Errorf("after approval: %s/%s, want ALLOW_TEMPORARY with the grant row's own id as the reason", eff, why)
	}
	if state, err := svc.BlockingState(ctx, itExec); err != nil || state != APPROVED {
		t.Fatalf("BlockingState after approval = %s %v", state, err)
	}

	// The grant is bound to *this* task: another task gets the bare default back.
	other := highRisk
	other.TaskID, other.ExecutionID = "task-OTHER", "exec-2"
	if eff, why := decide(other); eff != permission.RequireApproval || why != permission.ReasonDefaultHighRisk {
		t.Errorf("grant leaked to another task: %s/%s", eff, why)
	}

	// (5) Time passes, the sweep reclaims, and the verdict falls back without
	// anyone having remembered to close a switch in the UI.
	now = itNow + itHour + 1
	if eff, _ := decide(highRisk); eff == permission.AllowTemporary {
		t.Errorf("expired grant still authorising at %d", now)
	}
	if err := svc.SweepExpired(ctx); err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if eff, why := decide(highRisk); eff != permission.RequireApproval || why != permission.ReasonDefaultHighRisk {
		t.Errorf("after sweep: %s/%s, want back to DEFAULT_HIGH_RISK", eff, why)
	}

	// (6) The events this slice owes §7, in the ledger's own order.
	for _, ev := range []string{eventjournal.ApprovalRequested, eventjournal.ApprovalResolved,
		eventjournal.GrantIssued, eventjournal.GrantExpired} {
		if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, ev); got != 1 {
			t.Errorf("%s: %d events, want 1", ev, got)
		}
	}
	types := strRows(t, d, `SELECT event_type FROM event_journal ORDER BY seq`)
	want := []string{eventjournal.ApprovalRequested, eventjournal.ApprovalResolved,
		eventjournal.GrantIssued, eventjournal.GrantExpired}
	if i := indexOfRun(types, want); i < 0 {
		t.Errorf("expected %v in order within %v", want, types)
	}

	entries := readAudit(t, d)
	head, err := audit.Checkpoint(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.VerifyChain(entries, head); err != nil {
		t.Errorf("audit chain broken across the slice: %v", err)
	}
	t.Logf("chain: %d audit entries; %d events", len(entries), countRows(t, d, `SELECT count(*) FROM event_journal`))

	// (7) Both JSON columns must still be JSON after passing the redactor, which
	// re-encodes whatever it walks. A mangled column here would mean the
	// enforcement point broke a payload it should have left alone.
	for _, col := range []string{"request_json", "resolution_json"} {
		for _, v := range strRows(t, d, `SELECT `+col+` FROM approvals`) {
			if !json.Valid([]byte(v)) {
				t.Errorf("%s is not valid JSON after redaction: %s", col, v)
			}
		}
	}
}

func oneGrantID(t *testing.T, d *db.DB) string {
	t.Helper()
	var id string
	if err := d.Read.QueryRow(`SELECT id FROM temporary_grants`).Scan(&id); err != nil {
		t.Fatalf("read grant id: %v", err)
	}
	return id
}

func indexOfRun(haystack, want []string) int {
	for i := 0; i+len(want) <= len(haystack); i++ {
		match := true
		for j, w := range want {
			if haystack[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// The slice must fail if the grant is not actually read back from the table —
// a Decide hardcoded to ALLOW_TEMPORARY would otherwise pass.
func TestVerticalSliceDependsOnRealRows(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	clock := func() int64 { return itNow }
	eng := permission.New(permission.NewSQLiteSource(d), clock)
	svc := NewService(NewSQLiteStore(d, clock), clock)

	highRisk := permission.Request{PrincipalID: "local-user", TaskID: itTask, ExecutionID: itExec,
		Action: "filesystem.write", Resource: "repo:a.go"}

	a, err := svc.Request(ctx, Draft{PrincipalID: "local-user", TaskID: itTask, ExecutionID: itExec,
		Type: "EXECUTION_APPROVAL", RiskLevel: "HIGH", Action: highRisk.Action, Resource: highRisk.Resource})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Resolve(ctx, a.ID, Resolution{State: APPROVED, By: "local-user",
		Temporary: &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: itTask, Duration: itHour}}); err != nil {
		t.Fatal(err)
	}
	if eff, _, _ := eng.Decide(ctx, highRisk); eff != permission.AllowTemporary {
		t.Fatalf("pre-delete verdict = %s", eff)
	}
	// Delete only the grant row; the approval stays APPROVED. If Decide were
	// reading the approval rather than the grant, it would still say ALLOW_TEMPORARY.
	if _, err := d.Read.Exec(`DELETE FROM temporary_grants`); err != nil {
		t.Fatal(err)
	}
	if eff, why, _ := eng.Decide(ctx, highRisk); eff != permission.RequireApproval || why != permission.ReasonDefaultHighRisk {
		t.Errorf("verdict survived deleting the grant row: %s/%s — the decision is not coming from temporary_grants", eff, why)
	}
}
