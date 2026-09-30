package approval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// The read side behind GET /api/v1/approvals (T-372/T-393). Three things these
// cases exist to prove, in the order the ruling asked for them: the filter cannot
// read a row it was not allowed to read, keyset paging neither repeats nor drops,
// and "still PENDING?" is answered by the one predicate the gate already uses
// rather than by a second spelling of it.

// seedPage writes n approvals, each with its own requested_at, and returns their
// ids in the order the listing must produce them.
func seedPage(t *testing.T, d *db.DB, at func() int64, n int) []string {
	t.Helper()
	ctx := context.Background()
	svc := NewService(NewSQLiteStore(d, at), at)
	var ids []string
	for i := 0; i < n; i++ {
		dd := draft()
		a, err := svc.Request(ctx, dd)
		if err != nil {
			t.Fatalf("Request %d: %v", i, err)
		}
		ids = append(ids, a.ID)
	}
	return ids
}

func listedIDs(list []Approval) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.ID)
	}
	return out
}

func TestListPageEmptyTableIsNotAnError(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	svc := NewService(NewSQLiteStore(d, func() int64 { return itNow }), func() int64 { return itNow })

	// 空表：0 行、零位置、无错误。"这里没有审批"必须能和一个坏查询区分开，
	// 所以三样都断，不能只看长度。
	list, last, err := svc.ListPage(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("empty table: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("%d rows from an empty table: %v", len(list), listedIDs(list))
	}
	if last != (ListPosition{}) {
		t.Errorf("position = %+v, want the zero value so transport can say `next_cursor:\"\"`", last)
	}
	// The other side of the same leg: with rows present, the same call must find
	// them — otherwise "0 rows" would be a passing answer no matter what.
	seedPage(t, d, func() int64 { return itNow }, 2)
	if list, _, err := svc.ListPage(ctx, ListFilter{}); err != nil || len(list) != 2 {
		t.Fatalf("after 2 approvals: %d rows %v, want 2", len(list), err)
	}
}

// An illegal state is refused before the store is reached (fakeStore counts the
// calls), and a legal one is not — both sides, so the leg cannot be satisfied by
// a fake that always errors or a filter that always passes.
func TestListPageIllegalStateReadsNoRows(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, state string
		wantRuns    int
		wantErr     bool
	}{
		{"lowercase pending", "pending", 0, true},
		{"empty string", "", 0, true},
		{"sql fragment", "PENDING' OR '1'='1", 0, true},
		{"a real state", PENDING, 1, false},
		{"another real state", CANCELLED, 1, false},
		{"the state a sweep writes", EXPIRED, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStore()
			state := tc.state
			_, _, err := NewService(f, func() int64 { return itNow }).
				ListPage(ctx, ListFilter{State: &state})
			if tc.wantErr && err == nil {
				t.Fatalf("accepted %q; an illegal filter must not answer at all", state)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("refused a legal state %q: %v", state, err)
			}
			if f.listRuns != tc.wantRuns {
				t.Errorf("%d store calls, want %d — a rejected filter must not read a row", f.listRuns, tc.wantRuns)
			}
			if tc.wantErr && !strings.Contains(err.Error(), "is not an approvals.state value") {
				t.Errorf("err = %v, want validState's own message", err)
			}
		})
	}
}

// Keyset paging over a real database: the pages concatenated must equal a single
// full read, in order, with no id twice. Two approvals share one requested_at so
// the (requested_at, id) tie-break is actually exercised — a cursor that only
// compares timestamps would either repeat or drop one of those two.
func TestListPageKeysetPagesCoverExactlyOnce(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)

	// Same clock for two rows (the tie), then moving on.
	tie := func() int64 { return itNow }
	ids := seedPage(t, d, tie, 2)
	svc := NewService(NewSQLiteStore(d, func() int64 { return itNow }), func() int64 { return itNow })
	for i := 0; i < 5; i++ {
		clock := itNow + int64(i+1)*1000
		one := func() int64 { return clock }
		s := NewService(NewSQLiteStore(d, one), one)
		dd := draft()
		a, err := s.Request(ctx, dd)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}

	whole, _, err := svc.ListPage(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// The order the listing promises is (requested_at, id), so that is what is
	// asserted — not "insertion order". Two rows share one millisecond and uuidv7's
	// random tail does not guarantee they come back in creation order, so a check
	// written against insertion order would be a coin flip wearing a test's name.
	if len(whole) != len(ids) {
		t.Fatalf("%d rows in the unfiltered read, want %d", len(whole), len(ids))
	}
	for i := 1; i < len(whole); i++ {
		p, q := whole[i-1], whole[i]
		if p.RequestedAt > q.RequestedAt ||
			(p.RequestedAt == q.RequestedAt && p.ID > q.ID) {
			t.Fatalf("rows %d,%d are out of keyset order: (%d,%s) then (%d,%s)",
				i-1, i, p.RequestedAt, p.ID, q.RequestedAt, q.ID)
		}
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}

	seen := map[string]int{}
	var pos ListPosition
	pages := 0
	for pages < 20 { // a broken cursor would spin; the bound makes that a failure, not a hang
		list, last, err := svc.ListPage(ctx, ListFilter{After: nilIfZero(pos), Limit: 2})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		if len(list) == 0 {
			break
		}
		for _, a := range list {
			seen[a.ID]++
		}
		if last == (ListPosition{}) {
			t.Fatalf("page %d returned %d rows but no position; paging cannot move forward", pages, len(list))
		}
		pos = last
	}
	if pages >= 20 {
		t.Fatal("paging never terminated")
	}
	if len(seen) != len(want) {
		t.Errorf("%d distinct ids across pages, want %d", len(seen), len(want))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("id %s appeared on %d pages", id, n)
		}
	}
	// 页数本身也断一次：7 行、每页 2 ⇒ 4 页数据 + 1 页空。少于这个数说明有行被跳过。
	if pages != 5 {
		t.Errorf("%d pages for 7 rows at limit 2, want 5 (4 full pages + the empty one)", pages)
	}
}

func nilIfZero(p ListPosition) *ListPosition {
	if p == (ListPosition{}) {
		return nil
	}
	return &p
}

// The leg REQ-A8 was raised for: "is this PENDING row still waiting" must have one
// answer. The listing's SQL is Approval.DeadlinePassed's spelling, so at the exact
// deadline millisecond both read paths move together, and BlockingState stops
// saying PENDING at the same instant. Pin the boundary from both sides so a </<=
// change on either spelling cannot pass quietly.
func TestListPagePendingAgreesWithDeadlineBoundary(t *testing.T) {
	ctx := context.Background()
	deadline := itNow + itHour

	for _, tc := range []struct {
		name       string
		at         int64
		wantInList bool
		wantBlock  string
		wantSwept  int
	}{
		{"one ms before the deadline", deadline - 1, true, PENDING, 0},
		{"exactly at the deadline", deadline, false, StateNone, 1},
		{"one ms after", deadline + 1, false, StateNone, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := openMigrated(t)
			seed(t, d)
			create := func() int64 { return itNow }
			cs := NewService(NewSQLiteStore(d, create), create)
			dd := draft()
			dd.ExpiresAt = deadline
			a, err := cs.Request(ctx, dd)
			if err != nil {
				t.Fatal(err)
			}

			stored, err := NewSQLiteStore(d, create).Get(ctx, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := stored.Live(tc.at); got != tc.wantInList {
				t.Fatalf("Live(%d) = %v, want %v — the Go truth moved", tc.at, got, tc.wantInList)
			}

			at := func() int64 { return tc.at }
			svc := NewService(NewSQLiteStore(d, at), at)
			pending, _, err := svc.ListPage(ctx, ListFilter{State: strPtr(PENDING)})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(pending) != 0; got != tc.wantInList {
				t.Errorf("listing says %d pending rows, Live says %v — the two spellings disagree",
					len(pending), tc.wantInList)
			}
			// The row is never made undiscoverable: the unfiltered read still returns it,
			// with its stored state and its expires_at. Nothing in production calls
			// SweepExpired (09-30 measurement), so a blanket exclusion would hide an
			// existing approval forever instead of labelling it.
			if all, _, err := svc.ListPage(ctx, ListFilter{}); err != nil || len(all) != 1 {
				t.Errorf("unfiltered listing = %d rows (%v), want the row still listed", len(all), err)
			}
			// Same instant, the gate's own answer.
			if block, err := svc.BlockingState(ctx, itExec); err != nil || block != tc.wantBlock {
				t.Errorf("BlockingState = %q %v, want %q — one approval, two answers", block, err, tc.wantBlock)
			}
			// A listing must never make an existing approval undiscoverable: the row is
			// still returned by the unfiltered read with its stored state, because
			// nothing in production calls SweepExpired yet (09-30 measurement), so a
			// blanket exclusion would hide it forever rather than label it.
			// And the sweep must agree with the boundary from both directions: it
			// converges the lapsed row, and leaves the one that is still in time alone.
			if err := svc.SweepExpired(ctx); err != nil {
				t.Fatalf("SweepExpired: %v", err)
			}
			expired, _, err := svc.ListPage(ctx, ListFilter{State: strPtr(EXPIRED)})
			if err != nil {
				t.Fatal(err)
			}
			if len(expired) != tc.wantSwept {
				t.Errorf("%d rows under state=EXPIRED after the sweep, want %d", len(expired), tc.wantSwept)
			}
			still, _, err := svc.ListPage(ctx, ListFilter{State: strPtr(PENDING)})
			if err != nil {
				t.Fatal(err)
			}
			if len(still) != 1-tc.wantSwept {
				t.Errorf("%d rows still listed as PENDING after the sweep, want %d", len(still), 1-tc.wantSwept)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// ① 的只读保证：一次列表调用不许留下任何痕迹——没有事件、没有审计行、
// 没有版本推进。这条断言存在的意义是"只读"不能只写在注释里。
func TestListPageWritesNothing(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	clock := func() int64 { return itNow }
	svc := NewService(NewSQLiteStore(d, clock), clock)
	seedPage(t, d, clock, 3)
	// One row is already past its deadline on purpose. A read path that also
	// converged state (the mistake this leg exists to catch) would move that row,
	// and the sweep would write its event, version and audit row. Without a lapsed
	// row the leg is blind: ExpirePending over nothing looks exactly like a read.
	back := itNow - itHour
	old := func() int64 { return back }
	lateSvc := NewService(NewSQLiteStore(d, old), old)
	late := draft()
	late.ExpiresAt = back + 1000 // 已过期：listing 时刻 itNow 在它之后
	if _, err := lateSvc.Request(ctx, late); err != nil {
		t.Fatal(err)
	}

	counts := func() [4]int {
		return [4]int{
			countRows(t, d, `SELECT count(*) FROM event_journal`),
			countRows(t, d, `SELECT count(*) FROM audit_entries`),
			countRows(t, d, `SELECT count(*) FROM approvals`),
			countRows(t, d, `SELECT COALESCE(sum(version),0) FROM approvals`),
		}
	}
	before := counts()
	for i := 0; i < 4; i++ {
		if _, _, err := svc.ListPage(ctx, ListFilter{Limit: 2}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.ListPage(ctx, ListFilter{State: strPtr(PENDING)}); err != nil {
			t.Fatal(err)
		}
	}
	if after := counts(); after != before {
		t.Errorf("a read-only call changed the tables: %+v → %+v", before, after)
	}
	// The leg's own control: an approval write does move those counters, so the
	// equality above is not two zeros agreeing with each other.
	if _, err := svc.Request(ctx, draft()); err != nil {
		t.Fatal(err)
	}
	if after := counts(); after[0] <= before[0] || after[1] <= before[1] {
		t.Errorf("a write moved nothing: %+v → %+v", before, after)
	}
}

// The port's read method must not be reachable through any write path by
// accident: errors from the store surface unchanged rather than as an empty page.
func TestListPageSurfacesStoreErrors(t *testing.T) {
	f := newStore()
	f.listErr = errors.New("db down")
	state := PENDING
	_, _, err := NewService(f, func() int64 { return itNow }).ListPage(context.Background(), ListFilter{State: &state})
	if err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v, want the store's own failure surfaced", err)
	}
}

// The listing must answer with the same row shape the rest of the aggregate uses,
// including the request_json round trip the grant path depends on.
func TestListPageRowsAreFullyPopulated(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seed(t, d)
	clock := func() int64 { return itNow }
	svc := NewService(NewSQLiteStore(d, clock), clock)
	ids := seedPage(t, d, clock, 2)

	list, _, err := svc.ListPage(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("%d rows, want 2", len(list))
	}
	for i, a := range list {
		if a.ID != ids[i] || a.TaskID != itTask || a.ExecutionID != itExec || a.State != PENDING {
			t.Errorf("row %d = %+v, want the links and state back", i, a)
		}
		if a.Version != 1 || a.ResolvedAt != 0 {
			t.Errorf("row %d version/resolvedAt = %d/%d, want 1/0 — a read must not look decided", i, a.Version, a.ResolvedAt)
		}
		if _, err := a.RequesterPrincipal(); err != nil {
			t.Errorf("row %d request_json unreadable: %v", i, err)
		}
	}
}
