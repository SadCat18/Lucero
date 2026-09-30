package approval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/permission"
	"github.com/google/uuid"
)

const (
	tNow        int64 = 1_700_000_000_000
	hourInMs    int64 = 3600 * 1000
	executionID       = "e-1"
)

type resolveCall struct {
	id    string
	state string
	grant permission.Grant
	casOK bool
}

type fakeStore struct {
	rows        map[string]*Approval
	inserts     []Approval
	resolutions []resolveCall
	expireRuns  []int64
	revokeRuns  []int64
	listRuns    int // how many times ListPage reached the store
	listErr     error
	expireErr   error
	revokeErr   error
	nextID      int
}

func newStore(rows ...Approval) *fakeStore {
	f := &fakeStore{rows: map[string]*Approval{}}
	for _, r := range rows {
		cp := r
		f.rows[r.ID] = &cp
	}
	return f
}

func (f *fakeStore) Insert(_ context.Context, a Approval) error {
	if a.State != PENDING {
		return errors.New("fake: only PENDING approvals may be inserted")
	}
	cp := a
	f.rows[a.ID] = &cp
	f.inserts = append(f.inserts, a)
	return nil
}

func (f *fakeStore) Get(_ context.Context, id string) (Approval, error) {
	row, ok := f.rows[id]
	if !ok {
		return Approval{}, ErrNotFound
	}
	return *row, nil
}

// ListByExecution is deliberately sloppy here (it returns everything) so the
// service's own re-filter is what the binding tests actually exercise.
func (f *fakeStore) ListByExecution(context.Context, string) ([]Approval, error) {
	out := make([]Approval, 0, len(f.rows))
	for _, row := range f.rows {
		out = append(out, *row)
	}
	return out, nil
}

func (f *fakeStore) MarkResolved(_ context.Context, current Approval, res Resolution, grant permission.Grant, at int64) (bool, error) {
	row, ok := f.rows[current.ID]
	if !ok {
		return false, ErrNotFound
	}
	if row.State != PENDING { // the real UPDATE ... WHERE state='PENDING' CAS
		f.resolutions = append(f.resolutions, resolveCall{current.ID, res.State, grant, false})
		return false, nil
	}
	row.State, row.ResolvedAt, row.ResolvedBy = res.State, at, res.By
	f.resolutions = append(f.resolutions, resolveCall{current.ID, res.State, grant, true})
	return true, nil
}

func (f *fakeStore) ExpirePending(_ context.Context, at int64) error {
	f.expireRuns = append(f.expireRuns, at)
	for _, row := range f.rows {
		if row.State == PENDING && row.ExpiresAt != 0 && row.ExpiresAt <= at {
			row.State = EXPIRED
			row.ResolvedAt = at
		}
	}
	return f.expireErr
}

func (f *fakeStore) RevokeGrants(_ context.Context, at int64) error {
	f.revokeRuns = append(f.revokeRuns, at)
	return f.revokeErr
}

// ListPage counts the calls rather than answering them: the illegal-state leg
// asserts the store was never reached, and a fake that returned rows here would
// hide exactly that. The rows themselves are tested against a real database.
func (f *fakeStore) ListPage(context.Context, ListFilter) ([]Approval, ListPosition, error) {
	f.listRuns++
	if f.listErr != nil {
		return nil, ListPosition{}, f.listErr
	}
	return nil, ListPosition{}, nil
}

func (f *fakeStore) events() []string {
	var out []string
	for _, r := range f.resolutions {
		if r.casOK {
			out = append(out, "approval_resolved:"+r.id+":"+r.state)
		}
	}
	for _, a := range f.inserts {
		out = append(out, "approval_requested:"+a.ID)
	}
	return out
}

func service(f *fakeStore, at int64) *Service {
	s := NewService(f, func() int64 { return at })
	s.newID = func() (string, error) {
		f.nextID++
		return "id-" + string(rune('a'+f.nextID-1)), nil
	}
	return s
}

func pendingDraft() Draft {
	return Draft{
		PrincipalID: "local-user", TaskID: "t-1", ExecutionID: executionID, SessionID: "s-1",
		Type: "EXECUTION_APPROVAL", RiskLevel: "HIGH", Action: "filesystem.write",
		Resource: "repo:src/main.go",
	}
}

func TestRequestCreatesPendingApproval(t *testing.T) {
	f := newStore()
	a, err := service(f, tNow).Request(context.Background(), pendingDraft())
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if a.State != PENDING || a.Version != 1 || a.RequestedAt != tNow {
		t.Errorf("got %+v, want PENDING v1 requested at %d", a, tNow)
	}
	if !strings.Contains(a.RequestJSON, `"principal_id":"local-user"`) || !strings.Contains(a.RequestJSON, `"schema":1`) {
		t.Errorf("request_json lost the requester: %s", a.RequestJSON)
	}
	if len(f.inserts) != 1 {
		t.Errorf("%d inserts, want 1", len(f.inserts))
	}
}

func TestRequestDefaultsUnknownRisk(t *testing.T) {
	d := pendingDraft()
	d.RiskLevel = ""
	a, err := service(newStore(), tNow).Request(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if a.RiskLevel != "UNKNOWN" {
		t.Errorf("RiskLevel = %q, want UNKNOWN (never claim a risk we were not told)", a.RiskLevel)
	}
}

func TestRequestRejectsBadDrafts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Draft)
	}{
		{"no principal", func(d *Draft) { d.PrincipalID = "" }},
		{"no action", func(d *Draft) { d.Action = "" }},
		{"no type", func(d *Draft) { d.Type = "" }},
		{"expires in the past", func(d *Draft) { d.ExpiresAt = tNow - 1 }},
		{"expires exactly now", func(d *Draft) { d.ExpiresAt = tNow }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStore()
			d := pendingDraft()
			tc.mutate(&d)
			if _, err := service(f, tNow).Request(context.Background(), d); err == nil {
				t.Error("Request accepted it")
			}
			if len(f.inserts) != 0 {
				t.Errorf("a rejected draft still reached the store: %+v", f.inserts)
			}
		})
	}
}

func TestRequestMintsDistinctUUIDv7(t *testing.T) {
	f := newStore()
	s := NewService(f, func() int64 { return tNow })
	first, err := s.Request(context.Background(), pendingDraft())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Request(context.Background(), pendingDraft())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("duplicate ids: %s", first.ID)
	}
	for _, id := range []string{first.ID, second.ID} {
		parsed, err := uuid.Parse(id)
		if err != nil {
			t.Fatalf("id %q is not a UUID: %v", id, err)
		}
		if parsed.Version() != 7 {
			t.Errorf("id %q is UUIDv%d, domain model requires v7", id, parsed.Version())
		}
	}
}

func TestResolveIsIdempotent(t *testing.T) {
	ctx := context.Background()
	a, err := service(newStore(), tNow).Request(ctx, pendingDraft())
	if err != nil {
		t.Fatal(err)
	}
	f := newStore(a)
	s := service(f, tNow+5)
	res := Resolution{State: APPROVED, By: "local-user"}

	for i := 0; i < 3; i++ {
		if err := s.Resolve(ctx, a.ID, res); err != nil {
			t.Fatalf("Resolve call %d: %v", i+1, err)
		}
	}
	if got := len(f.resolutions); got != 1 {
		t.Errorf("%d state writes, want exactly 1 (%+v)", got, f.resolutions)
	}
	if got := f.events(); len(got) != 1 {
		t.Errorf("%d events, want exactly 1: %v", len(got), got)
	}
	// A later, different decision must not overwrite the first one.
	if err := s.Resolve(ctx, a.ID, Resolution{State: REJECTED, By: "local-user"}); err != nil {
		t.Fatalf("Resolve after decision: %v", err)
	}
	if f.rows[a.ID].State != APPROVED {
		t.Errorf("state became %s; the first decision must stand", f.rows[a.ID].State)
	}
	if len(f.resolutions) != 1 {
		t.Errorf("the flipped decision produced %d writes", len(f.resolutions))
	}
}

func TestResolveLosesTheRaceWithoutDuplicates(t *testing.T) {
	a, _ := service(newStore(), tNow).Request(context.Background(), pendingDraft())
	f := newStore(a)
	f.rows[a.ID].State = APPROVED // a concurrent request already decided it
	f.rows[a.ID].ResolvedAt = tNow

	s := service(f, tNow+1)
	err := s.Resolve(context.Background(), a.ID, Resolution{
		State: APPROVED, By: "local-user", Temporary: &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: "t-1", Duration: hourInMs},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(f.events()) != 0 {
		t.Errorf("the losing caller wrote events: %v", f.events())
	}
	for _, r := range f.resolutions {
		if r.casOK {
			t.Error("the losing caller won the CAS")
		}
	}
}

func TestApproveWithTemporaryWritesGrant(t *testing.T) {
	ctx := context.Background()
	d := pendingDraft()
	d.Detail = []byte(`{"command":"rm -rf build"}`)
	a, err := service(newStore(), tNow).Request(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	f := newStore(a)
	s := service(f, tNow+10)
	err = s.Resolve(ctx, a.ID, Resolution{
		State: APPROVED, By: "local-user", Note: "user clicked full access for this task",
		Temporary: &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: "t-1", Duration: 3 * hourInMs},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(f.resolutions) != 1 || !f.resolutions[0].casOK {
		t.Fatalf("no resolution recorded: %+v", f.resolutions)
	}
	g := f.resolutions[0].grant
	if g.ID == "" {
		t.Fatal("ALLOW_TEMPORARY did not produce a grant row")
	}
	if g.ScopeType != permission.ScopeTask || g.ScopeID != "t-1" {
		t.Errorf("grant scope = %s/%s, want TASK/t-1", g.ScopeType, g.ScopeID)
	}
	if g.ExpiresAt != tNow+10+3*hourInMs {
		t.Errorf("grant expires_at = %d, want %d", g.ExpiresAt, tNow+10+3*hourInMs)
	}
	if g.PrincipalID != "local-user" {
		t.Errorf("grant principal = %q, want the requester recovered from request_json", g.PrincipalID)
	}
	if g.Action != a.Action || g.Effect != permission.Allow {
		t.Errorf("grant action/effect = %s/%s, want %s/ALLOW", g.Action, g.Effect, a.Action)
	}
}

func TestResolveRejectsBadResolutions(t *testing.T) {
	base := Resolution{State: APPROVED, By: "local-user"}
	for _, tc := range []struct {
		name   string
		mutate func(*Resolution)
	}{
		{"unknown state", func(r *Resolution) { r.State = "YES" }},
		{"pending is not a decision", func(r *Resolution) { r.State = PENDING }},
		{"no resolver", func(r *Resolution) { r.By = "" }},
		{"grant on a rejection", func(r *Resolution) {
			r.State = REJECTED
			r.Temporary = &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: "t-1", Duration: hourInMs}
		}},
		{"task mode grant", func(r *Resolution) {
			r.Temporary = &GrantRequest{ScopeType: permission.ScopeTaskMode, ScopeID: "PLAN", Duration: hourInMs}
		}},
		{"grant scope without id", func(r *Resolution) {
			r.Temporary = &GrantRequest{ScopeType: permission.ScopeProject, Duration: hourInMs}
		}},
		{"zero duration", func(r *Resolution) {
			r.Temporary = &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: "t-1"}
		}},
		{"negative duration", func(r *Resolution) {
			r.Temporary = &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: "t-1", Duration: -1}
		}},
		{"duration beyond cap", func(r *Resolution) {
			r.Temporary = &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: "t-1", Duration: MaxGrantDuration + 1}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := service(newStore(), tNow).Request(context.Background(), pendingDraft())
			f := newStore(a)
			res := base
			tc.mutate(&res)
			if err := service(f, tNow).Resolve(context.Background(), a.ID, res); err == nil {
				t.Error("Resolve accepted it")
			}
			if len(f.resolutions) != 0 {
				t.Errorf("an invalid resolution reached the store: %+v", f.resolutions)
			}
		})
	}
}

func TestUnknownApprovalIsNotFound(t *testing.T) {
	f := newStore()
	if err := service(f, tNow).Resolve(context.Background(), "nope", Resolution{State: APPROVED, By: "u"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGrantNeedsAKnownRequester(t *testing.T) {
	a, _ := service(newStore(), tNow).Request(context.Background(), pendingDraft())
	a.RequestJSON = `{"schema":1}` // e.g. a row written before this convention
	f := newStore(a)
	err := service(f, tNow).Resolve(context.Background(), a.ID, Resolution{
		State: APPROVED, By: "local-user",
		Temporary: &GrantRequest{ScopeType: permission.ScopeTask, ScopeID: "t-1", Duration: hourInMs},
	})
	if err == nil || !strings.Contains(err.Error(), "principal_id") {
		t.Errorf("err = %v, want a principal_id error", err)
	}
	if len(f.resolutions) != 0 {
		t.Error("approved without knowing whom to grant")
	}
}

func TestUnboundApprovalIsNeverApprovedForAnExecution(t *testing.T) {
	ctx := context.Background()
	unbound := Approval{ID: "a-none", State: APPROVED, Action: "filesystem.write", RequestedAt: tNow, ResolvedAt: tNow, RequestJSON: `{}`}
	elsewhere := Approval{ID: "a-other", ExecutionID: "e-999", State: APPROVED, Action: "filesystem.write", RequestedAt: tNow, ResolvedAt: tNow, RequestJSON: `{}`}
	f := newStore(unbound, elsewhere)
	s := service(f, tNow)

	list, err := s.ForExecution(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("ForExecution leaked approvals bound elsewhere: %+v", list)
	}
	state, err := s.BlockingState(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if state != StateNone {
		t.Errorf("BlockingState = %s, want NONE; an approval for another execution (or none) cannot unblock this one", state)
	}
}

func TestBlockingState(t *testing.T) {
	approved := Approval{ID: "a1", ExecutionID: executionID, State: APPROVED, RequestedAt: tNow, ResolvedAt: tNow, RequestJSON: `{}`}
	pending := Approval{ID: "a2", ExecutionID: executionID, State: PENDING, RequestedAt: tNow, ExpiresAt: tNow + hourInMs, RequestJSON: `{}`}
	stale := Approval{ID: "a3", ExecutionID: executionID, State: PENDING, RequestedAt: tNow, ExpiresAt: tNow - 1, RequestJSON: `{}`}
	rejectedEarlier := Approval{ID: "a4", ExecutionID: executionID, State: REJECTED, RequestedAt: tNow, ResolvedAt: tNow, RequestJSON: `{}`}
	rejectedLater := Approval{ID: "a5", ExecutionID: executionID, State: CANCELLED, RequestedAt: tNow, ResolvedAt: tNow + 5, RequestJSON: `{}`}

	for _, tc := range []struct {
		name string
		rows []Approval
		want string
	}{
		{"nothing bound", nil, StateNone},
		{"pending blocks", []Approval{approved, pending}, PENDING},
		{"approved unblocks", []Approval{approved}, APPROVED},
		{"expired but unswept does not block", []Approval{stale}, StateNone},
		{"most recent decision answers", []Approval{rejectedEarlier, rejectedLater}, CANCELLED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := service(newStore(tc.rows...), tNow)
			got, err := s.BlockingState(context.Background(), executionID)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("BlockingState = %s, want %s", got, tc.want)
			}
		})
	}
	t.Run("empty execution id is refused", func(t *testing.T) {
		if _, err := service(newStore(), tNow).BlockingState(context.Background(), ""); err == nil {
			t.Error("accepted an empty execution_id")
		}
	})
}

func TestLiveBoundary(t *testing.T) {
	row := Approval{State: PENDING, ExpiresAt: 200}
	for _, tc := range []struct {
		at   int64
		want bool
	}{{199, true}, {200, false}, {201, false}} {
		if got := row.Live(tc.at); got != tc.want {
			t.Errorf("Live(%d) = %v, want %v", tc.at, got, tc.want)
		}
	}
	noExpiry := Approval{State: PENDING}
	if !noExpiry.Live(1 << 40) {
		t.Error("a NULL expires_at approval must stay live")
	}
	decided := Approval{State: APPROVED}
	if decided.Live(tNow) {
		t.Error("a decided approval is not live")
	}
}

func TestSweepExpired(t *testing.T) {
	expiring := Approval{ID: "a1", State: PENDING, RequestedAt: tNow, ExpiresAt: tNow + 10, RequestJSON: `{}`}
	kept := Approval{ID: "a2", State: PENDING, RequestedAt: tNow, RequestJSON: `{}`}
	decided := Approval{ID: "a3", State: APPROVED, RequestedAt: tNow, ResolvedAt: tNow, RequestJSON: `{}`}
	f := newStore(expiring, kept, decided)
	if err := service(f, tNow+11).SweepExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.rows["a1"].State != EXPIRED {
		t.Errorf("expired approval still %s", f.rows["a1"].State)
	}
	if f.rows["a2"].State != PENDING {
		t.Error("sweep expired an approval with no deadline")
	}
	if f.rows["a3"].State != APPROVED {
		t.Error("sweep rewrote a decided approval — that would be forging audit-visible state")
	}
	if len(f.expireRuns) != 1 || len(f.revokeRuns) != 1 {
		t.Errorf("sweep runs: expire=%v revoke=%v, want one each", f.expireRuns, f.revokeRuns)
	}
	if f.expireRuns[0] != tNow+11 || f.revokeRuns[0] != tNow+11 {
		t.Errorf("sweep used %d/%d, want the clock value %d", f.expireRuns[0], f.revokeRuns[0], tNow+11)
	}
}

func TestSweepExpiredReportsStoreErrors(t *testing.T) {
	f := newStore()
	f.expireErr = errors.New("busy")
	if err := service(f, tNow).SweepExpired(context.Background()); err == nil {
		t.Error("swallowd the expire error")
	}
	f2 := newStore()
	f2.revokeErr = errors.New("busy")
	err := service(f2, tNow).SweepExpired(context.Background())
	if err == nil || !strings.Contains(err.Error(), "grants") {
		t.Errorf("err = %v, want the grant sweep to surface", err)
	}
	if len(f2.expireRuns) != 1 {
		t.Error("approvals were not swept before the grant failure")
	}
}
