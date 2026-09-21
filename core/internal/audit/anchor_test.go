package audit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newAnchor(t *testing.T) *Anchor {
	t.Helper()
	return &Anchor{Path: filepath.Join(t.TempDir(), "audit.head")}
}

func TestAnchorCommitAndReadBack(t *testing.T) {
	a := newAnchor(t)
	chain := buildChain(3)
	for _, e := range chain {
		if err := a.Commit(e.Seq, e.EntryHash, e.CreatedAt); err != nil {
			t.Fatalf("commit %d: %v", e.Seq, err)
		}
	}
	head, err := a.Expected(3)
	if err != nil {
		t.Fatal(err)
	}
	if head.Seq != 3 || head.Hash != chain[2].EntryHash {
		t.Errorf("head = %+v, want seq 3 at the newest link", head)
	}
	b, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Split(strings.TrimRight(string(b), "\n"), "\n")); got != 3 {
		t.Errorf("%d lines in the anchor, want one per checkpoint", got)
	}
	if !strings.Contains(string(b), "\t") || strings.Contains(string(b), "3\t") == false {
		t.Errorf("anchor is not in seq\\thash\\ttime form: %q", b)
	}
}

// T-34 ③: deleting the newest rows must be caught. VerifyChain alone cannot see
// tail truncation, which is the entire reason the anchor exists.
func TestCheckDetectsTailDeletion(t *testing.T) {
	chain := buildChain(5)
	a := newAnchor(t)
	for _, e := range chain {
		if err := a.Commit(e.Seq, e.EntryHash, e.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	status, err := Check(chain, a)
	if err != nil || status != StatusVerifiedLocal {
		t.Fatalf("honest chain: %s %v, want VERIFIED_LOCAL", status, err)
	}
	for _, n := range []int{1, 2, 4} {
		status, err := Check(chain[:5-n], a)
		if err == nil || status != StatusUnverified {
			t.Errorf("deleting the newest %d row(s) passed as %s %v, want UNVERIFIED", n, status, err)
		}
	}
	// Rewriting a middle row is caught too, and by the link check rather than
	// by luck.
	forged := append([]Entry{}, chain...)
	forged[1].Outcome = OutcomeDenied
	if status, err := Check(forged, a); err == nil || status != StatusUnverified {
		t.Errorf("rewritten middle row passed as %s %v", status, err)
	}
}

// T-34 ③: no anchor is "we could not check", never "the chain is fine".
func TestCheckWithoutAnchorIsUnverifiedNotVerified(t *testing.T) {
	chain := buildChain(4)
	missing := &Anchor{Path: filepath.Join(t.TempDir(), "audit.head")}
	status, err := Check(chain, missing)
	if !errors.Is(err, ErrNoAnchor) || status != StatusUnverified {
		t.Errorf("missing anchor: %s %v, want UNVERIFIED with ErrNoAnchor", status, err)
	}
	if status == StatusVerifiedLocal {
		t.Error("a chain nobody could compare against must never be reported VERIFIED_LOCAL")
	}

	emptyFile := newAnchor(t)
	if err := os.WriteFile(emptyFile.Path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, err := Check(chain, emptyFile); err == nil || status != StatusUnverified {
		t.Errorf("anchor with no lines: %s %v, want UNVERIFIED", status, err)
	}
	if status, err := Check(nil, missing); err != nil || status != StatusUnknown {
		t.Errorf("no rows at all: %s %v, want UNKNOWN", status, err)
	}
}

// A checkpoint is only appended after a successful COMMIT, so a line past the
// stored rows means the newest rows are gone or the DB was rolled back: the
// answer is UNVERIFIED, not a silent pass. Expected() still refuses to certify
// from a leading line, which is the other half of the same rule.
func TestAnchorLineAheadOfDatabaseIsUnverified(t *testing.T) {
	chain := buildChain(2)
	a := newAnchor(t)
	for _, e := range chain {
		if err := a.Commit(e.Seq, e.EntryHash, e.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Commit(9, "a-future-hash", 1234); err != nil {
		t.Fatal(err)
	}
	head, err := a.Expected(chain[1].Seq)
	if err != nil {
		t.Fatal(err)
	}
	if head.Seq != 2 || head.Hash != chain[1].EntryHash {
		t.Errorf("head = %+v, want the newest line at or before seq 2, not the one at 9", head)
	}
	if status, err := Check(chain, a); err == nil || status != StatusUnverified {
		t.Errorf("leading checkpoint: %s %v, want UNVERIFIED", status, err)
	}
	// A bogus line at seq 9 is a checkpoint that does not match the row there.
	if status, err := Check(buildChain(9), a); err == nil || status != StatusUnverified {
		t.Errorf("checkpoint hash that matches no row: %s %v, want UNVERIFIED", status, err)
	}
	lagging := newAnchor(t)
	long := buildChain(9)
	for _, e := range long[:2] {
		if err := lagging.Commit(e.Seq, e.EntryHash, e.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	if status, err := Check(long, lagging); err != nil || status != StatusVerifiedLocal {
		t.Errorf("a chain running ahead of its last checkpoint: %s %v, want VERIFIED_LOCAL up to seq 2", status, err)
	}
}

// T-34 ③, as far as this package can enforce it: a rejected Commit leaves no
// line behind. The remaining half of that requirement — only call Commit after a
// successful db COMMIT — belongs to the startup/shutdown code, which is why
// Check treats an anchor ahead of the rows as "cannot certify" instead of
// silently passing.
func TestAnchorCommitRejectsWithoutWriting(t *testing.T) {
	a := newAnchor(t)
	chain := buildChain(2)
	for _, e := range chain {
		if err := a.Commit(e.Seq, e.EntryHash, e.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(a.Path)
	for name, err := range map[string]func() error{
		"rewind to an older seq": func() error { return a.Commit(1, chain[0].EntryHash, 1) },
		"duplicate seq":          func() error { return a.Commit(2, chain[1].EntryHash, 1) },
		"empty hash":             func() error { return a.Commit(3, "", 1) },
		"negative seq":           func() error { return a.Commit(-1, "x", 1) },
		"path unset":             func() error { return (&Anchor{}).Commit(3, "x", 1) },
	} {
		if e := err(); e == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	after, _ := os.ReadFile(a.Path)
	if string(before) != string(after) {
		t.Errorf("rejected commits still appended: before=%q after=%q", before, after)
	}
	// seq -1 must fail on monotonicity, not by luck of ordering.
	if err := a.Commit(3, "hash-3", 999); err != nil {
		t.Errorf("the good commit after the rejected ones: %v", err)
	}
}

// cmd/core passes whatever anchor it configured, which on a fresh install may be
// nil or a zero Anchor. That has to read as "cannot certify", and it must not
// take the process down.
func TestCheckWithoutAnchorConfigured(t *testing.T) {
	chain := buildChain(3)
	for name, a := range map[string]*Anchor{
		"nil anchor":       nil,
		"unset path":       {},
		"empty path value": {Path: ""},
	} {
		status, err := Check(chain, a)
		if err == nil || !errors.Is(err, ErrNoAnchor) {
			t.Errorf("%s: err = %v, want ErrNoAnchor", name, err)
		}
		if status != StatusUnverified {
			t.Errorf("%s: status = %s, want UNVERIFIED", name, status)
		}
	}
	// An empty chain still needs no anchor to be judged.
	if status, err := Check(nil, nil); err != nil || status != StatusUnknown {
		t.Errorf("empty chain with no anchor: %s %v, want UNKNOWN", status, err)
	}
}

func TestAnchorMalformedLineFailsClosed(t *testing.T) {
	for name, body := range map[string]string{
		"two fields":      "1\tabcdef\n",
		"bad seq":         "x\tabcdef\t123\n",
		"empty hash":      "1\t\t123\n",
		"bad time":        "1\tabcdef\tnow\n",
		"space separated": "1 abcdef 123\n",
	} {
		a := newAnchor(t)
		if err := os.WriteFile(a.Path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Expected(10); err == nil {
			t.Errorf("%s: a corrupt anchor was treated as certifying", name)
		}
		if _, err := Check(buildChain(1), a); err == nil {
			t.Errorf("%s: Check passed over a corrupt anchor", name)
		}
	}
}
