package approval

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SadCat18/lucero/core/internal/eventjournal"
	"github.com/SadCat18/lucero/core/internal/permission"
)

// Regression cover for the two questions backend is allowed to ask, both against
// a real database, plus §3 rule 4 pinned as its own case.

func TestBlockingStateRegressions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lucero.db")
	d := openMigratedAt(t, path)
	seed(t, d)
	now := itNow
	clock := func() int64 { return now }
	st := NewSQLiteStore(d, clock)
	svc := NewService(st, clock)

	// A bound approval is what proves NONE means "nothing for this execution",
	// not "the query is broken".
	bound, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	if state, err := svc.BlockingState(ctx, itExec); err != nil || state != PENDING {
		t.Fatalf("own pending approval: %s %v", state, err)
	}
	if state, err := svc.BlockingState(ctx, "exec-2"); err != nil || state != StateNone {
		t.Fatalf("other execution must read NONE while exec-1 is pending: %s %v", state, err)
	}
	if _, err := svc.BlockingState(ctx, ""); err == nil {
		t.Error("an empty execution_id was accepted")
	}

	// Approved unblocks; a later pending request re-blocks.
	if err := svc.Resolve(ctx, bound.ID, Resolution{State: APPROVED, By: "local-user"}); err != nil {
		t.Fatal(err)
	}
	if state, _ := svc.BlockingState(ctx, itExec); state != APPROVED {
		t.Fatalf("after approval: %s", state)
	}
	second, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	if state, _ := svc.BlockingState(ctx, itExec); state != PENDING {
		t.Fatalf("a new pending request must block again even though an older one was APPROVED: %s", state)
	}

	// A pending approval past its deadline is neither blocking nor approved:
	// it has already lapsed even though no sweep has run.
	lapsed, err := svc.Request(ctx, func() Draft { dd := draft(); dd.ExpiresAt = now + 5; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	if state, _ := svc.BlockingState(ctx, itExec); state != PENDING {
		t.Fatalf("live pending approvals should still block: %s", state)
	}
	if _, err := d.Read.Exec(`UPDATE approvals SET state='PENDING' WHERE id = ?`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`UPDATE approvals SET expires_at = ? WHERE id IN (?,?)`, now+5, second.ID, lapsed.ID); err != nil {
		t.Fatal(err)
	}
	now += 10
	if state, _ := svc.BlockingState(ctx, itExec); state != APPROVED {
		t.Fatalf("lapsed pendings must fall back to the standing APPROVED, not block forever: %s", state)
	}

	// Same file, brand new handles: the answers come from disk, not memory.
	reopened := NewService(NewSQLiteStore(openMigratedAt(t, path), clock), clock)
	for _, tc := range []struct{ execID, want string }{
		{itExec, APPROVED}, {"exec-2", StateNone}, {"exec-none-existent", StateNone},
	} {
		got, err := reopened.BlockingState(ctx, tc.execID)
		if err != nil || got != tc.want {
			t.Errorf("after reopen BlockingState(%s) = %s %v, want %s", tc.execID, got, err, tc.want)
		}
	}
	if list, err := reopened.ForExecution(ctx, "exec-2"); err != nil || len(list) != 0 {
		t.Errorf("ForExecution(exec-2) after reopen = %d rows %v", len(list), err)
	}
	if list, err := reopened.ForExecution(ctx, itExec); err != nil || len(list) != 3 {
		t.Errorf("ForExecution(%s) after reopen = %d rows %v, want the 3 approvals bound to it", itExec, len(list), err)
	}
}

// SweepExpired must converge exactly the rows time already decided, and repeating
// it must not forge a second event for the same fact.
func TestSweepExpiredRegressions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lucero.db")
	d := openMigratedAt(t, path)
	seed(t, d)
	now := itNow
	st := NewSQLiteStore(d, func() int64 { return now })
	svc := NewService(st, func() int64 { return now })

	dueA, err := svc.Request(ctx, func() Draft { dd := draft(); dd.ExpiresAt = now + 10; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	dueB, err := svc.Request(ctx, func() Draft { dd := draft(); dd.ExecutionID = "exec-2"; dd.ExpiresAt = now + 10; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Request(ctx, func() Draft { dd := draft(); dd.ExpiresAt = now + 500; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	noDeadline, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	decided, err := svc.Request(ctx, func() Draft { dd := draft(); dd.ExpiresAt = now + 10; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Resolve(ctx, decided.ID, Resolution{State: REJECTED, By: "local-user"}); err != nil {
		t.Fatal(err)
	}

	// Before the deadline nothing moves.
	if err := st.ExpirePending(ctx, now+9); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.Get(ctx, dueA.ID); a.State != PENDING {
		t.Fatalf("swept early: %s became %s", dueA.ID, a.State)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.ApprovalExpired); got != 0 {
		t.Fatalf("%d expiry events before the deadline", got)
	}

	now = itNow + 10
	if err := svc.SweepExpired(ctx); err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	for _, id := range []string{dueA.ID, dueB.ID} {
		a, err := st.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if a.State != EXPIRED || a.Version != 2 {
			t.Errorf("expired %s = %s v%d, want EXPIRED v2", id, a.State, a.Version)
		}
	}
	if a, _ := st.Get(ctx, noDeadline.ID); a.State != PENDING {
		t.Errorf("a NULL expires_at approval became %s — an open deadline must never be read as passed", a.State)
	}
	if a, _ := st.Get(ctx, decided.ID); a.State != REJECTED {
		t.Errorf("a decided approval became %s — that would rewrite history", a.State)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.ApprovalExpired); got != 2 {
		t.Errorf("%d expiry events, want 2 (one per lapsed approval)", got)
	}
	if got := countRows(t, d, `SELECT count(*) FROM audit_entries WHERE action=?`, eventjournal.ApprovalExpired); got != 2 {
		t.Errorf("%d expiry audit rows, want 2", got)
	}

	// Repeating the sweep is inert: no second event, no extra version.
	if err := svc.SweepExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.ApprovalExpired); got != 2 {
		t.Errorf("%d expiry events after a second sweep, want still 2", got)
	}
	if a, _ := st.Get(ctx, dueA.ID); a.Version != 2 {
		t.Errorf("version moved to %d on a no-op sweep", a.Version)
	}
	// A later clock still must not touch the open-deadline row.
	now = itNow + 10_000
	if err := st.ExpirePending(ctx, now); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.Get(ctx, noDeadline.ID); a.State != PENDING {
		t.Errorf("no-deadline approval expired at %d: %s", now, a.State)
	}

	// The expired approvals now read as EXPIRED to backend, on a fresh handle.
	svc2 := NewService(NewSQLiteStore(openMigratedAt(t, path), func() int64 { return itNow + 10 }),
		func() int64 { return itNow + 10 })
	if state, err := svc2.BlockingState(ctx, "exec-2"); err != nil || state != EXPIRED {
		t.Errorf("after reopen BlockingState(exec-2) = %s %v, want EXPIRED", state, err)
	}
}

// §3 rule 4, pinned on its own: an approval raised without an execution_id may
// never be read as "approved" by any execution. The positive control at the end
// is what stops this passing because every lookup returns nothing.
func TestApprovalWithoutExecutionIDIsInvisibleToEveryExecution(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	clock := func() int64 { return itNow }
	st := NewSQLiteStore(d, clock)
	svc := NewService(st, clock)

	unbound, err := svc.Request(ctx, func() Draft { dd := draft(); dd.ExecutionID = ""; dd.TaskID = itTask; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Resolve(ctx, unbound.ID, Resolution{
		State: APPROVED, By: "local-user",
		Temporary: &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: itTask, Duration: itHour},
	}); err != nil {
		t.Fatal(err)
	}
	// The row really is unbound, so a bug in the query rather than in the data
	// cannot be what makes the next assertions pass.
	if n := countRows(t, d, `SELECT count(*) FROM approvals WHERE id=? AND execution_id IS NULL`, unbound.ID); n != 1 {
		t.Fatalf("fixture: the approval is not stored with a NULL execution_id")
	}

	for _, execID := range []string{itExec, "exec-2", "exec-never-heard-of"} {
		if state, err := svc.BlockingState(ctx, execID); err != nil || state != StateNone {
			t.Errorf("BlockingState(%s) = %s %v, want NONE: an approval with no execution_id cannot unblock anything", execID, state, err)
		}
		if list, err := svc.ForExecution(ctx, execID); err != nil || len(list) != 0 {
			t.Errorf("ForExecution(%s) = %d rows %v, want none", execID, len(list), err)
		}
	}

	// Cross-check every listing against the table itself: ForExecution must
	// return exactly the ids SQL says are bound, no more and no fewer. A one-sided
	// bug in either the query or the service's own re-filter shows up here.
	for _, execID := range []string{itExec, "exec-2"} {
		fromSQL := map[string]bool{}
		for _, id := range strRows(t, d, `SELECT id FROM approvals WHERE execution_id = ?`, execID) {
			fromSQL[id] = true
		}
		// The raw store first: Service.ForExecution re-filters, so a broken query
		// would be masked there and this is the only way to pin the SQL itself.
		raw, err := st.ListByExecution(ctx, execID)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range raw {
			if a.ExecutionID != execID {
				t.Errorf("ListByExecution(%s) returned %s bound to %q — the query is not binding", execID, a.ID, a.ExecutionID)
			}
		}
		if len(raw) != len(fromSQL) {
			t.Fatalf("ListByExecution(%s) = %d rows, SQL says %d", execID, len(raw), len(fromSQL))
		}
		list, err := svc.ForExecution(ctx, execID)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != len(fromSQL) {
			t.Fatalf("ForExecution(%s) returned %d rows, SQL says %d", execID, len(list), len(fromSQL))
		}
		for _, a := range list {
			if !fromSQL[a.ID] {
				t.Errorf("ForExecution(%s) returned %s which SQL says is bound to %q", execID, a.ID, a.ExecutionID)
			}
		}
	}

	// Positive control: the very same service does see a bound approval.
	bound, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	if state, _ := svc.BlockingState(ctx, itExec); state != PENDING {
		t.Errorf("positive control failed: BlockingState(%s) = %s, want PENDING", itExec, state)
	}
	if n := countRows(t, d, `SELECT count(*) FROM approvals WHERE execution_id=?`, itExec); n != 1 {
		t.Fatalf("positive control: %d rows bound to %s, want 1 (bound id %s)", n, itExec, bound.ID)
	}

	// A grant minted by the unbound approval is bound to its task, and the
	// pending approval for a real execution is unaffected by it.
	eng := permission.New(permission.NewSQLiteSource(d), clock)
	if eff, why, err := eng.Decide(ctx, permission.Request{PrincipalID: "local-user", TaskID: itTask,
		ExecutionID: itExec, Action: "filesystem.write", Resource: "repo:a.go"}); err != nil ||
		eff != permission.AllowTemporary || why != permission.Reason(oneGrantID(t, d)) {
		t.Errorf("task-scoped grant from the unbound approval: %s/%s %v", eff, why, err)
	}
	other := permission.Request{PrincipalID: "local-user", TaskID: "task-OTHER",
		Action: "filesystem.write", Resource: "repo:a.go"}
	if eff, _, _ := eng.Decide(ctx, other); eff == permission.AllowTemporary {
		t.Error("the grant reached a task it was not scoped to")
	}
}
