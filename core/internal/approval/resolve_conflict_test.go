package approval

// T-438 ② — "a second, different decision on a row that is already decided" used to
// answer `nil`, the same answer as a replay. Transport reads `nil` as success, so the
// panel said "resolved" while the row still carried the first answer. Contract ① §3
// rule 8 keeps the replay half inert and requires the other half to have a name, so
// these cases pin three things: the name exists, it is NOT reachable by accident
// (a replay must still be silence), and it cannot be confused with the missing-row
// answer.
//
// Two layers are covered on purpose. The Service's pre-read is what an ordinary
// caller sees; MarkResolved's own check is what the loser of a race sees, and it is
// the only answer that can be race-free because it runs inside the write transaction.
// A mutation that removes either one has to go red here, not only in the other file.

import (
	"context"
	"errors"
	"testing"

	"github.com/SadCat18/lucero/core/internal/eventjournal"
	"github.com/SadCat18/lucero/core/internal/permission"
)

func grantRequest(scope permission.Scope, scopeID string, duration int64) *GrantRequest {
	return &GrantRequest{ScopeType: scope, ScopeID: scopeID, Duration: duration}
}

// winner is the decision the row below records: APPROVED with a one-hour grant.
// The resolver is a principal seed() actually creates, because approvals.resolved_by
// has a foreign key and a made-up name would fail for a reason this file is not about.
var winner = Resolution{
	State: APPROVED, By: "local-user",
	Temporary: grantRequest(permission.ScopeTask, itTask, itHour),
}

func TestResolveReplayAndConflictAreDifferentAnswers(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name       string
		second     Resolution
		wantSilent bool
	}{
		{
			name:       "逐字重放：仍然必须是 silence",
			second:     winner,
			wantSilent: true,
		},
		{
			name:       "换了决议人与备注但判的是同一件事：重放",
			second:     Resolution{State: APPROVED, By: "system", Note: "再点一次", Temporary: winner.Temporary},
			wantSilent: true,
		},
		{
			name:       "反向决议：必须有名",
			second:     Resolution{State: REJECTED, By: "local-user"},
			wantSilent: false,
		},
		{
			name:       "同状态但窗口变长：必须有名",
			second:     Resolution{State: APPROVED, By: "local-user", Temporary: grantRequest(permission.ScopeTask, itTask, 2*itHour)},
			wantSilent: false,
		},
		{
			name:       "同状态同窗口但作用域换了对象：必须有名",
			second:     Resolution{State: APPROVED, By: "local-user", Temporary: grantRequest(permission.ScopeTask, "task-other", itHour)},
			wantSilent: false,
		},
		{
			name:       "同状态同范围但换了作用域类型：必须有名",
			second:     Resolution{State: APPROVED, By: "local-user", Temporary: grantRequest(permission.ScopeProject, itTask, itHour)},
			wantSilent: false,
		},
		{
			name:       "库里发了授权、这次要求不发：必须有名",
			second:     Resolution{State: APPROVED, By: "local-user"},
			wantSilent: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := service(newStore(), itNow).Request(ctx, pendingDraft())
			if err != nil {
				t.Fatal(err)
			}
			f := newStore(a)
			if err := service(f, itNow).Resolve(ctx, a.ID, winner); err != nil {
				t.Fatalf("the first decision must land: %v", err)
			}

			err = service(f, itNow+1).Resolve(ctx, a.ID, tc.second)
			switch {
			case tc.wantSilent && err != nil:
				t.Errorf("a replay came back named: %v", err)
			case !tc.wantSilent && !errors.Is(err, ErrConflictingResolution):
				t.Errorf("a different decision got err = %v, want ErrConflictingResolution", err)
			}
			if errors.Is(err, ErrNotFound) {
				t.Errorf("a conflict must never be reported as a missing row: %v", err)
			}

			// Whichever answer arrived, the row is terminal and must not have moved:
			// the flip is refused AND it wrote nothing on the way to refusing.
			if got := len(f.resolutions); got != 1 {
				t.Errorf("%d state writes, want exactly the winner's 1 (%+v)", got, f.resolutions)
			}
			if got := len(f.events()); got != 1 {
				t.Errorf("%d events, want exactly 1: %v", got, f.events())
			}
			if f.rows[a.ID].State != APPROVED {
				t.Errorf("the row became %s; the first decision must stand", f.rows[a.ID].State)
			}
		})
	}
}

// TestResolveChecksTheBodyBeforeTheRow is the ordering leg: an illegal decision is the
// caller's error whether the approval is pending or already terminal, and it must not
// be dressed up as either a replay or a conflict.
func TestResolveChecksTheBodyBeforeTheRow(t *testing.T) {
	ctx := context.Background()
	a, err := service(newStore(), itNow).Request(ctx, pendingDraft())
	if err != nil {
		t.Fatal(err)
	}
	f := newStore(a)
	if err := service(f, itNow).Resolve(ctx, a.ID, winner); err != nil {
		t.Fatal(err)
	}

	// A row whose state has already been recorded still cannot be re-decided with a
	// state that does not exist, nor with no resolver at all.
	for name, res := range map[string]Resolution{
		"非法 state": {State: "MAYBE", By: "x"},
		"缺决议人":   {State: REJECTED},
	} {
		err := service(f, itNow+1).Resolve(ctx, a.ID, res)
		if err == nil {
			t.Errorf("%s: a malformed decision was accepted on a decided row", name)
			continue
		}
		if errors.Is(err, ErrConflictingResolution) || errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want the caller's own error, not a conflict or a missing row", name, err)
		}
	}
	if len(f.resolutions) != 1 || len(f.events()) != 1 {
		t.Errorf("a malformed decision reached the store: %d writes / %d events", len(f.resolutions), len(f.events()))
	}
}

// TestStoreNamesTheLoserOfARace is the second layer. Two callers can both read
// PENDING, so the pre-read cannot be the only guard: the answer for "the row you are
// about to overwrite already carries a different decision" has to come from inside the
// write.
func TestStoreNamesTheLoserOfARace(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	st := NewSQLiteStore(d, func() int64 { return itNow })
	svc := NewService(st, func() int64 { return itNow })

	a, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Resolve(ctx, a.ID, winner); err != nil {
		t.Fatalf("first decision: %v", err)
	}
	decided, err := st.Get(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Straight at the store, with the row it re-reads already terminal: the CAS branch
	// must name the disagreement instead of returning the replay's silence.
	applied, err := st.MarkResolved(ctx, decided, Resolution{State: REJECTED, By: "system"}, permission.Grant{}, itNow+5)
	if !errors.Is(err, ErrConflictingResolution) {
		t.Errorf("loser of the race: applied=%v err=%v, want ErrConflictingResolution", applied, err)
	}
	if applied {
		t.Error("the loser reported itself as having written")
	}
	// Same content through the same door is still the silent answer, and it must not
	// report itself as having written.
	applied, err = st.MarkResolved(ctx, decided, winner, permission.Grant{}, itNow+5)
	if err != nil {
		t.Errorf("a replay through the store: err = %v, want nil", err)
	}
	if applied {
		t.Error("a replay reported itself as having written")
	}

	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.ApprovalResolved); got != 1 {
		t.Errorf("%d approval_resolved events, want 1", got)
	}
	if got := strRows(t, d, `SELECT state || '/' || version FROM approvals`)[0]; got != "APPROVED/2" {
		t.Errorf("approvals row = %s, want APPROVED/2 — the late decision must not move it", got)
	}
}

// TestRealStoreConflictAndReplay are the two answers on real storage, including the
// thing a fake cannot show: no second temporary_grants row.
func TestRealStoreConflictAndReplay(t *testing.T) {
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
	if err := svc.Resolve(ctx, a.ID, winner); err != nil {
		t.Fatalf("first decision: %v", err)
	}
	if got := countRows(t, d, `SELECT count(*) FROM temporary_grants`); got != 1 {
		t.Fatalf("%d grant rows after the first decision, want 1", got)
	}

	later := NewService(NewSQLiteStore(d, func() int64 { return itNow + 10 }), func() int64 { return itNow + 10 })
	if err := later.Resolve(ctx, a.ID, Resolution{
		State: APPROVED, By: "system",
		Temporary: grantRequest(permission.ScopeTask, itTask, 8*itHour),
	}); !errors.Is(err, ErrConflictingResolution) {
		t.Errorf("asking for a longer window against a decided row: err = %v, want ErrConflictingResolution", err)
	}
	if got := countRows(t, d, `SELECT count(*) FROM temporary_grants`); got != 1 {
		t.Errorf("the refused re-decision minted %d grant rows, want the original 1", got)
	}
	if err := later.Resolve(ctx, a.ID, winner); err != nil {
		t.Errorf("the same content again must stay a replay: %v", err)
	}
	if got := countRows(t, d, `SELECT count(*) FROM temporary_grants`); got != 1 {
		t.Errorf("a replay minted %d grant rows, want 1", got)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.ApprovalResolved); got != 1 {
		t.Errorf("%d approval_resolved events across conflict + replay, want 1", got)
	}
	if got := strRows(t, d, `SELECT resolved_by FROM approvals`)[0]; got != "local-user" {
		t.Errorf("resolved_by = %s; the winner's identity must stand", got)
	}
}

// TestSweptRowRejectsALateApproval is the case the sweep creates: the row is terminal
// because time ran out, and a human arriving afterwards with APPROVED is not replaying
// anything.
func TestSweptRowRejectsALateApproval(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	clock := func() int64 { return itNow }
	st := NewSQLiteStore(d, clock)
	svc := NewService(st, clock)

	a, err := svc.Request(ctx, func() Draft { dd := draft(); dd.ExpiresAt = itNow + itHour; return dd }())
	if err != nil {
		t.Fatal(err)
	}
	later := NewService(NewSQLiteStore(d, func() int64 { return itNow + 2*itHour }), func() int64 { return itNow + 2*itHour })
	if err := later.SweepExpired(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got, _ := st.Get(ctx, a.ID); got.State != EXPIRED {
		t.Fatalf("the sweep left the row at %s", got.State)
	}

	if err := later.Resolve(ctx, a.ID, Resolution{State: APPROVED, By: "local-user"}); !errors.Is(err, ErrConflictingResolution) {
		t.Errorf("approving an expired row: err = %v, want ErrConflictingResolution", err)
	}
	if got, _ := st.Get(ctx, a.ID); got.State != EXPIRED || got.Version != 2 {
		t.Errorf("row = %s v%d, want EXPIRED v2 — a late approval must not rewrite the sweep", got.State, got.Version)
	}
	if got := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, eventjournal.ApprovalResolved); got != 0 {
		t.Errorf("%d approval_resolved events, want 0: nothing was decided here", got)
	}
}
