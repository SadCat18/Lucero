package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeAppend struct {
	gotTable   string
	gotCols    []string
	gotValues  []any
	storedSeq  int64 // what the append-only row actually got
	returnsErr error
	calls      int
}

func (f *fakeAppend) Append(table string, columns []string, values []any) (int64, error) {
	f.calls++
	if f.returnsErr != nil {
		return 0, f.returnsErr
	}
	f.gotTable, f.gotCols, f.gotValues = table, columns, values
	return f.storedSeq, nil
}

func testChain(headSeq int64, headHash string, app *fakeAppend) *Chain {
	return &Chain{
		Head:   func(context.Context) (int64, string, error) { return headSeq, headHash, nil },
		Insert: app, Now: func() int64 { return 1234 },
	}
}

func TestChainAppendLinksToHead(t *testing.T) {
	app := &fakeAppend{storedSeq: 4}
	c := testChain(3, "hash-3", app)
	seq, err := c.Append(context.Background(), Entry{
		AuditID: "a4", PrincipalID: "local-user", Action: "approval.resolved",
		ResourceType: "approval", ResourceID: "ap-1", Outcome: OutcomeSuccess,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if seq != 4 {
		t.Errorf("seq = %d, want the stored 4", seq)
	}
	if app.gotTable != "audit_entries" {
		t.Errorf("wrote to %q", app.gotTable)
	}
	if len(app.gotCols) != len(app.gotValues) {
		t.Fatalf("%d columns but %d values", len(app.gotCols), len(app.gotValues))
	}
	if app.gotCols[0] != "audit_id" || app.gotCols[len(app.gotCols)-1] != "created_at" {
		t.Errorf("column list looks wrong: %v", app.gotCols)
	}
	for _, c := range app.gotCols {
		if c == "seq" || c == "version" {
			t.Errorf("platform-owned column %s was named by audit", c)
		}
	}
	got := Entry{
		Seq: 4, AuditID: "a4", PrincipalID: "local-user", Action: "approval.resolved",
		ResourceType: "approval", ResourceID: "ap-1", Outcome: OutcomeSuccess,
		DetailsJSON: `{"schema":1}`, PreviousHash: "hash-3", CreatedAt: 1234,
	}
	if app.gotValues[11] != "hash-3" {
		t.Errorf("previous_hash = %v, want the head", app.gotValues[11])
	}
	if app.gotValues[12] != ComputeEntryHash("hash-3", got) {
		t.Errorf("entry_hash was not the sealed value")
	}
}

func TestChainAppendRejectsIncompleteEntries(t *testing.T) {
	for name, e := range map[string]Entry{
		"no audit id": {Action: "x", Outcome: OutcomeSuccess},
		"no action":   {AuditID: "a", Outcome: OutcomeSuccess},
		"no outcome":  {AuditID: "a", Action: "x"},
	} {
		app := &fakeAppend{storedSeq: 1}
		if _, err := testChain(0, "", app).Append(context.Background(), e); err == nil {
			t.Errorf("%s: accepted, and the chain would have a hole", name)
		}
		if app.calls != 0 {
			t.Errorf("%s: reached the appender anyway", name)
		}
	}
}

// A head that moved between the read and the insert would fork the chain; the
// predicted-vs-stored seq check has to turn that into an error, not a row.
func TestChainDetectsFork(t *testing.T) {
	app := &fakeAppend{storedSeq: 9}
	_, err := testChain(3, "hash-3", app).Append(context.Background(),
		Entry{AuditID: "a", Action: "x", Outcome: OutcomeSuccess})
	if !errors.Is(err, ErrChainFork) {
		t.Fatalf("err = %v, want ErrChainFork", err)
	}
}

func TestChainPropagatesInsertError(t *testing.T) {
	app := &fakeAppend{returnsErr: errors.New("no such column")}
	_, err := testChain(0, "", app).Append(context.Background(),
		Entry{AuditID: "a", Action: "x", Outcome: OutcomeSuccess})
	if err == nil || !strings.Contains(err.Error(), "no such column") {
		t.Fatalf("err = %v", err)
	}
}

func TestRecordRedaction(t *testing.T) {
	t.Run("no hits writes no row", func(t *testing.T) {
		app := &fakeAppend{storedSeq: 1}
		seq, err := testChain(0, "", app).RecordRedaction(context.Background(),
			Entry{AuditID: "a", Action: "x"}, nil)
		if err != nil || seq != 0 || app.calls != 0 {
			t.Fatalf("seq=%d err=%v calls=%d", seq, err, app.calls)
		}
	})
	t.Run("hits become names and counts only", func(t *testing.T) {
		app := &fakeAppend{storedSeq: 2}
		secret := "ghp_A1bCA1bCA1bCA1bCA1bCA1bCA1bCA1bCA1bC"
		_, err := testChain(1, "hash-1", app).RecordRedaction(context.Background(),
			Entry{AuditID: "a2", Action: "approval.requested", PrincipalID: "local-user"},
			[]RedactionHit{{Column: "request_json", Kind: HitKey, Match: "apikey", Count: 3}})
		if err != nil {
			t.Fatal(err)
		}
		details, _ := app.gotValues[10].(string)
		if !strings.Contains(details, `"kind":"key"`) || !strings.Contains(details, `"match":"apikey"`) ||
			!strings.Contains(details, `"count":3`) {
			t.Errorf("details lost the rule kind, name or count: %s", details)
		}
		if strings.Contains(details, secret) {
			t.Errorf("value leaked into the audit row: %s", details)
		}
		if app.gotValues[9] != OutcomeSuccess {
			t.Errorf("outcome = %v, want the default SUCCESS", app.gotValues[9])
		}
	})
}

// §2.2.1: entry_hash must describe the bytes that land. The redactor therefore
// runs before the seal, arriving as an injected function because audit may not
// import internal/platform/db (§0) — cmd/core binds the two. This stands in for
// db.Scrub with the same job: replace a secret-shaped value with the marker.
func TestScrubRunsBeforeSeal(t *testing.T) {
	ctx := context.Background()
	const secret = "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	app := &fakeAppend{storedSeq: 7}
	c := testChain(6, "hash-6", app)
	c.Scrub = func(table string, cols []string, args []any) ([]any, []RedactionHit, error) {
		if table != "audit_entries" || len(cols) != len(args) {
			t.Errorf("scrub called with table %q and %d cols / %d args", table, len(cols), len(args))
		}
		out := make([]any, len(args))
		copy(out, args)
		for i := range cols {
			if s, ok := out[i].(string); ok {
				out[i] = strings.ReplaceAll(s, secret, DefaultMarker)
			}
		}
		return out, nil, nil
	}

	if _, err := c.Append(ctx, Entry{
		AuditID: "a-scrub", Action: "execution.failed", ResourceType: "task", ResourceID: "t-1",
		TaskID: "t-1", ExecutionID: "e-1", Outcome: OutcomeUnknown,
		DetailsJSON: `{"note":"token=` + secret + `","attempt":2}`,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	stored, err := applyScrubbed(Entry{}, app.gotValues)
	if err != nil {
		t.Fatal(err)
	}
	stored.Seq = app.storedSeq
	if strings.Contains(stored.DetailsJSON, secret) {
		t.Fatalf("the row was written unscrubbed, so this case proves nothing: %s", stored.DetailsJSON)
	}
	if !strings.Contains(stored.DetailsJSON, DefaultMarker) {
		t.Errorf("the marker is missing: %s", stored.DetailsJSON)
	}
	// The invariant in one line: recomputing over the stored bytes must reproduce
	// the stored hash. Revert Append to Seal-then-Scrub and this is red.
	if got := ComputeEntryHash(stored.PreviousHash, stored); got != stored.EntryHash {
		t.Errorf("stored entry_hash %s does not describe the stored row (recomputed %s)", stored.EntryHash, got)
	}
	// A chain that is whole and honest still verifies end to end, so the scrub
	// cannot have quietly shifted a link out from under its successor.
	joined := &fakeAppend{storedSeq: 1}
	c1 := testChain(0, "", joined)
	c1.Scrub = c.Scrub
	if _, err := c1.Append(ctx, Entry{AuditID: "b-1", Action: "x", Outcome: OutcomeSuccess,
		DetailsJSON: `{"note":"token=` + secret + `"}`}); err != nil {
		t.Fatal(err)
	}
	var firstHash string
	for i, col := range joined.gotCols {
		if col == "entry_hash" {
			firstHash, _ = joined.gotValues[i].(string)
		}
	}
	if firstHash == "" {
		t.Fatal("the first row came back with no entry_hash to link onto")
	}
	second := &fakeAppend{storedSeq: 2}
	c2 := testChain(1, firstHash, second)
	c2.Scrub = c.Scrub
	if _, err := c2.Append(ctx, Entry{AuditID: "b-2", Action: "y", Outcome: OutcomeSuccess}); err != nil {
		t.Fatal(err)
	}
	rows := []Entry{}
	for _, app := range []*fakeAppend{joined, second} {
		e, err := applyScrubbed(Entry{}, app.gotValues)
		if err != nil {
			t.Fatal(err)
		}
		e.Seq = app.storedSeq
		rows = append(rows, e)
	}
	if err := VerifyChain(rows, Head{Seq: 2, Hash: rows[1].EntryHash}); err != nil {
		t.Errorf("two honestly scrubbed rows do not chain: %v", err)
	}
}

// An identity scrubber has to be a no-op, and a broken one has to be refused
// rather than half-written: both keep the injected function from becoming a way
// to corrupt a row on the way in.
func TestScrubIsTransparentOrFatal(t *testing.T) {
	ctx := context.Background()
	base := Entry{
		AuditID: "a-identity", Action: "session.read", ResourceType: "session", ResourceID: "s-1",
		Outcome: OutcomeSuccess, DetailsJSON: `{"schema":1}`,
	}

	plain := &fakeAppend{storedSeq: 1}
	if _, err := testChain(0, "", plain).Append(ctx, base); err != nil {
		t.Fatal(err)
	}
	ident := &fakeAppend{storedSeq: 1}
	c := testChain(0, "", ident)
	c.Scrub = func(_ string, _ []string, args []any) ([]any, []RedactionHit, error) { return args, nil, nil }
	if _, err := c.Append(ctx, base); err != nil {
		t.Fatal(err)
	}
	if len(ident.gotCols) != len(plain.gotValues) {
		t.Fatalf("%d columns vs %d values", len(ident.gotCols), len(plain.gotValues))
	}
	for i, col := range ident.gotCols {
		if ident.gotValues[i] != plain.gotValues[i] {
			t.Errorf("an identity scrub changed %s: %v → %v", col, plain.gotValues[i], ident.gotValues[i])
		}
	}
	for _, tc := range []struct {
		name  string
		scrub func([]string, []any) ([]any, []RedactionHit, error)
		want  string
	}{
		{"short row", func(_ []string, args []any) ([]any, []RedactionHit, error) { return args[:2], nil, nil }, "values for"},
		{"wrong type", func(cols []string, args []any) ([]any, []RedactionHit, error) {
			out := append([]any(nil), args...)
			for i, col := range cols {
				if col == "created_at" {
					out[i] = "not a timestamp"
				}
			}
			return out, nil, nil
		}, "created_at"},
		{"scrubber fails", func(_ []string, _ []any) ([]any, []RedactionHit, error) { return nil, nil, errors.New("redactor down") }, "redactor down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &fakeAppend{storedSeq: 1}
			c := testChain(0, "", app)
			c.Scrub = func(_ string, cols []string, args []any) ([]any, []RedactionHit, error) { return tc.scrub(cols, args) }
			if _, err := c.Append(ctx, base); err == nil {
				t.Fatal("the row was written despite a broken scrub")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
			if app.calls != 0 {
				t.Errorf("%d inserts after a rejected scrub, want 0", app.calls)
			}
		})
	}
}

// applyScrubbed is the one place that maps a stored row back onto Entry. A
// column that silently lost its case here would drop out of entry_hash without
// any test noticing, so round-trip every field of a fully populated row.
func TestScrubbedRowRoundTripsEveryColumn(t *testing.T) {
	e := Entry{
		Seq: 9, AuditID: "a-rt", PrincipalID: "local-user", Action: "approval.requested",
		ResourceType: "approval", ResourceID: "ap-1", ProjectID: "p-1", TaskID: "t-1",
		ExecutionID: "e-1", CorrelationID: "corr-1", Outcome: OutcomeSuccess,
		DetailsJSON:  `{"schema":1,"approval_id":"ap-1","state":"PENDING"}`,
		PreviousHash: "prev-hash", EntryHash: "entry-hash", Signature: "sig", CreatedAt: 1700000000000,
	}
	got, err := applyScrubbed(Entry{}, e.rowValues())
	if err != nil {
		t.Fatal(err)
	}
	got.Seq = e.Seq
	if got != e {
		t.Errorf("a round trip through the row values lost a field:\n got %+v\nwant %+v", got, e)
	}
	// NULL-able columns have to come back as "", not as a nil the hash would miss.
	empty := Entry{AuditID: "a-min", Action: "x", Outcome: OutcomeSuccess,
		DetailsJSON: `{"schema":1}`, CreatedAt: 1}
	back, err := applyScrubbed(Entry{}, empty.rowValues())
	if err != nil {
		t.Fatal(err)
	}
	if back != empty {
		t.Errorf("minimal row round trip = %+v, want %+v", back, empty)
	}
}

// REQ-A11 is the reason Scrub returns hits at all. A deny-key hit is found by
// column name and would survive a second pass; a pattern hit is found by value and
// is gone for good once that value is a marker. Drop the pre-seal hits and half the
// record disappears while every byte still lines up — "the hash is right, the
// attribution quietly shrank", which is the same class of failure §2.2.1 ends.
func TestScrubHitsReachTheCaller(t *testing.T) {
	ctx := context.Background()
	app := &fakeAppend{storedSeq: 1}
	c := testChain(0, "", app)
	c.Scrub = func(_ string, _ []string, args []any) ([]any, []RedactionHit, error) {
		out := append([]any(nil), args...)
		for i, v := range out {
			if s, ok := v.(string); ok && strings.Contains(s, "ghp_AAAA") {
				out[i] = strings.ReplaceAll(s, "ghp_AAAA", DefaultMarker)
			}
		}
		return out, []RedactionHit{{Column: "audit_entries.details_json", Kind: HitPattern,
			Match: "gh[pousr]_[A-Za-z0-9]{20,}", Count: 1}}, nil
	}
	if n := len(c.ScrubHits()); n != 0 {
		t.Fatalf("%d hits before anything was written", n)
	}
	if _, err := c.Append(ctx, Entry{AuditID: "h-1", Action: "x", Outcome: OutcomeSuccess,
		DetailsJSON: `{"note":"ghp_AAAA"}`}); err != nil {
		t.Fatal(err)
	}
	hits := c.ScrubHits()
	if len(hits) != 1 || hits[0].Kind != HitPattern {
		t.Fatalf("%+v, want the pattern hit the scrubber reported", hits)
	}

	// And the caller can actually record it: this is the row the counts live in.
	// The fake plays the database's monotonic seq, or the fork check (correctly)
	// refuses the second row.
	app.storedSeq = 2
	if _, err := c.RecordRedaction(ctx, Entry{AuditID: "h-2", Action: "redaction.x",
		ResourceType: "audit"}, hits); err != nil {
		t.Fatal(err)
	}
	var stored string
	for i, col := range app.gotCols {
		if col == "details_json" {
			stored, _ = app.gotValues[i].(string)
		}
	}
	if !strings.Contains(stored, "gh[pousr]_[A-Za-z0-9]{20,}") || !strings.Contains(stored, `"kind":"pattern"`) {
		t.Errorf("the redaction row lost the pattern attribution: %s", stored)
	}

	// No scrubber wired means no hits, so a merge on the caller's side cannot
	// invent a record out of nothing.
	plain := testChain(0, "", &fakeAppend{storedSeq: 1})
	if _, err := plain.Append(ctx, Entry{AuditID: "h-3", Action: "y", Outcome: OutcomeSuccess}); err != nil {
		t.Fatal(err)
	}
	if n := len(plain.ScrubHits()); n != 0 {
		t.Errorf("%d hits with no Scrub wired", n)
	}
}
