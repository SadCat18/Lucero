package audit

import (
	"errors"
	"testing"
)

func buildChain(n int) []Entry {
	entries := make([]Entry, 0, n)
	prev := ""
	for i := 0; i < n; i++ {
		e := Entry{
			Seq: int64(i + 1), AuditID: "audit-" + string(rune('a'+i)),
			PrincipalID: "local-user", Action: "approval.resolve",
			ResourceType: "approval", ResourceID: "approval-1",
			Outcome: "SUCCESS", DetailsJSON: `{"schema":1,"state":"APPROVED"}`,
			CreatedAt: int64(1700000000000 + i),
		}
		e = Seal(e, prev)
		entries = append(entries, e)
		prev = e.EntryHash
	}
	return entries
}

func head(t *testing.T, entries []Entry) Head {
	t.Helper()
	h, err := Checkpoint(entries)
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	return h
}

func TestHonestChainVerifies(t *testing.T) {
	for _, n := range []int{0, 1, 2, 9} {
		chain := buildChain(n)
		if err := VerifyChain(chain, Head{}); err != nil {
			t.Errorf("chain of %d rejected without checkpoint: %v", n, err)
		}
		if n > 0 {
			if err := VerifyChain(chain, head(t, chain)); err != nil {
				t.Errorf("chain of %d rejected against its own checkpoint: %v", n, err)
			}
		}
	}
}

func TestRewriteIsDetected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutator func(*Entry)
	}{
		{"outcome", func(e *Entry) { e.Outcome = OutcomeDenied }},
		{"details", func(e *Entry) { e.DetailsJSON = `{"schema":1,"state":"REJECTED"}` }},
		{"principal", func(e *Entry) { e.PrincipalID = "system" }},
		{"action", func(e *Entry) { e.Action = "approval.requested" }},
		{"audit_id", func(e *Entry) { e.AuditID = "audit-forged" }},
		{"created_at", func(e *Entry) { e.CreatedAt += 5 }},
		// bytes moved between adjacent fields, total length unchanged
		{"field boundary shift", func(e *Entry) { e.Action, e.ResourceType = "approval.resolveX", "Xapproval" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain := buildChain(5)
			forged := chain[2]
			tc.mutator(&forged)
			forged.EntryHash = chain[2].EntryHash // forger keeps the old hash
			chain[2] = forged
			if err := VerifyChain(chain, head(t, buildChain(5))); !errors.Is(err, ErrTampered) {
				t.Errorf("rewrite of %s was not detected (err = %v)", tc.name, err)
			}
		})
	}
}

func TestReSealAfterRewriteIsStillDetected(t *testing.T) {
	// A forger who edits a field and recomputes that entry's hash still breaks
	// the next link, because entry N+1 pins N's hash in previous_hash.
	chain := buildChain(4)
	chain[1].Outcome = "DENIED"
	chain[1].EntryHash = ComputeEntryHash(chain[1].PreviousHash, chain[1])
	if err := VerifyChain(chain, Head{}); !errors.Is(err, ErrTampered) {
		t.Errorf("re-sealed rewrite not detected (err = %v)", err)
	}
}

func TestDeletionIsDetected(t *testing.T) {
	for _, drop := range []int{0, 1, 3} {
		t.Run("middle", func(t *testing.T) {
			chain := buildChain(5)
			checkpoint := head(t, chain)
			short := append(append([]Entry{}, chain[:drop]...), chain[drop+1:]...)
			if err := VerifyChain(short, checkpoint); !errors.Is(err, ErrTampered) {
				t.Errorf("deleting index %d was not detected (err = %v)", drop, err)
			}
		})
	}
	t.Run("newest entries need a checkpoint", func(t *testing.T) {
		chain := buildChain(5)
		checkpoint := head(t, chain)
		truncated := chain[:3]
		if err := VerifyChain(truncated, Head{}); err != nil {
			t.Fatalf("unexpected: a truncated chain fails without a checkpoint: %v", err)
		}
		if err := VerifyChain(truncated, checkpoint); !errors.Is(err, ErrTampered) {
			t.Errorf("tail deletion not detected against checkpoint (err = %v)", err)
		}
	})
	t.Run("emptying the chain", func(t *testing.T) {
		chain := buildChain(5)
		checkpoint := head(t, chain)
		if err := VerifyChain(nil, checkpoint); !errors.Is(err, ErrTampered) {
			t.Errorf("full deletion not detected (err = %v)", err)
		}
	})
}

func TestOutOfOrderAppendIsRejected(t *testing.T) {
	chain := buildChain(3)
	checkpoint := head(t, chain)

	// A later append must extend seq and time; both are checked independently.
	t.Run("backdated timestamp", func(t *testing.T) {
		stray := Seal(Entry{
			Seq: 4, AuditID: "audit-z", PrincipalID: "local-user", Action: "approval.requested",
			Outcome: "SUCCESS", DetailsJSON: "{}", CreatedAt: 1600000000000,
		}, chain[2].EntryHash)
		grown := append(append([]Entry{}, chain...), stray)
		if err := VerifyChain(grown, Head{}); !errors.Is(err, ErrTampered) {
			t.Errorf("backdated append accepted (err = %v)", err)
		}
	})
	t.Run("duplicate seq", func(t *testing.T) {
		dup := Seal(Entry{
			Seq: 3, AuditID: "audit-z", PrincipalID: "local-user", Action: "x",
			Outcome: "SUCCESS", DetailsJSON: "{}", CreatedAt: chain[2].CreatedAt + 1,
		}, chain[2].EntryHash)
		grown := append(append([]Entry{}, chain...), dup)
		if err := VerifyChain(grown, checkpoint); !errors.Is(err, ErrTampered) {
			t.Errorf("duplicate seq accepted (err = %v)", err)
		}
	})
	t.Run("skipped seq", func(t *testing.T) {
		gap := Seal(Entry{
			Seq: 5, AuditID: "audit-z", PrincipalID: "local-user", Action: "x",
			Outcome: "SUCCESS", DetailsJSON: "{}", CreatedAt: chain[2].CreatedAt + 1,
		}, chain[2].EntryHash)
		grown := append(append([]Entry{}, chain...), gap)
		if err := VerifyChain(grown, checkpoint); !errors.Is(err, ErrTampered) {
			t.Errorf("seq gap accepted (err = %v)", err)
		}
	})
	t.Run("reparented genesis", func(t *testing.T) {
		genesis := chain[0]
		genesis.PreviousHash = "0000000000000000000000000000000000000000000000000000000000000000"
		genesis.EntryHash = ComputeEntryHash(genesis.PreviousHash, genesis)
		grown := append([]Entry{genesis}, chain[1:]...)
		if err := VerifyChain(grown, checkpoint); !errors.Is(err, ErrTampered) {
			t.Errorf("non-empty previous_hash on the first entry accepted (err = %v)", err)
		}
	})
}

func TestComputeEntryHashIsDeterministicAndDistinct(t *testing.T) {
	e := buildChain(1)[0]
	if got, want := ComputeEntryHash(e.PreviousHash, e), e.EntryHash; got != want {
		t.Errorf("Seal and ComputeEntryHash disagree: %s vs %s", got, want)
	}
	other := e
	other.DetailsJSON = e.DetailsJSON + " "
	if ComputeEntryHash(e.PreviousHash, other) == e.EntryHash {
		t.Error("trailing whitespace in details produces the same hash")
	}
	if ComputeEntryHash("a", e) == ComputeEntryHash("b", e) {
		t.Error("previous_hash does not feed the hash")
	}
}
