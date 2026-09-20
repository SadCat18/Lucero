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
