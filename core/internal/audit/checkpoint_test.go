package audit

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// The threshold is ADR-018's, not a knob: these tests pin the behaviour around
// the constant rather than around a configurable value.
func TestCheckpointCounterCrossing(t *testing.T) {
	a := newAnchor(t)
	c := NewCheckpointCounter(a, func() int64 { return 1234 })
	head := Head{Seq: 7, Hash: "hash-7"}

	for i := 1; i < CheckpointEveryAppends; i++ {
		if err := c.Advance(1, head); err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
		if b, _ := os.ReadFile(a.Path); len(b) != 0 {
			t.Fatalf("a checkpoint appeared after only %d appends", i)
		}
	}
	if err := c.Advance(1, head); err != nil {
		t.Fatal(err)
	}
	lines, err := anchorLines(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("%d checkpoint lines at the boundary, want 1: %v", len(lines), lines)
	}
	if lines[0] != "7\thash-7\t1234" {
		t.Errorf("line = %q, want seq\\thash\\ttime", lines[0])
	}

	// Nothing owed until the next boundary. A checkpoint may also never rewind,
	// so the second crossing has to carry a newer head than the first one did.
	next := Head{Seq: 9, Hash: "hash-9"}
	for i := 0; i < CheckpointEveryAppends-2; i++ {
		if err := c.Advance(1, next); err != nil {
			t.Fatal(err)
		}
	}
	if held, _ := anchorLines(a); len(held) != 1 {
		t.Errorf("mid-boundary advances wrote a checkpoint: %v", held)
	}
	if err := c.Advance(2, next); err != nil {
		t.Fatal(err)
	}
	lines, err = anchorLines(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[1] != "9	hash-9	1234" {
		t.Errorf("second boundary: %v, want a line for the newest head", lines)
	}
	if err := c.Advance(1, Head{Seq: 8, Hash: "rewound"}); err != nil {
		t.Fatal(err)
	}
	if lines, _ := anchorLines(a); len(lines) != 2 {
		t.Errorf("a rewound head must not write a line, got %v", lines)
	}
}

func TestCheckpointCounterIgnoresEmptyWork(t *testing.T) {
	for name, head := range map[string]Head{
		"zero head":     {},
		"hashless head": {Seq: 3},
		"zero seq":      {Hash: "h"},
	} {
		a := newAnchor(t)
		c := NewCheckpointCounter(a, func() int64 { return 1 })
		for _, n := range []int{0, -1, CheckpointEveryAppends * 2} {
			if err := c.Advance(n, head); err != nil {
				t.Errorf("%s with n=%d: %v", name, n, err)
			}
		}
		if c.Counted() != 0 {
			t.Errorf("%s: counted %d, want nothing counted", name, c.Counted())
		}
		if b, _ := os.ReadFile(a.Path); len(b) != 0 {
			t.Errorf("%s: wrote %q for a transaction with no audit row", name, b)
		}
	}
}

// A Chain only reports what it actually appended, so the caller's commit is what
// turns a count into a checkpoint.
func TestChainCheckpointsOnlyWhatItAppended(t *testing.T) {
	app := &fakeAppend{storedSeq: 1}
	c := NewCheckpointCounter(newAnchor(t), func() int64 { return 5 })
	ch := &Chain{
		Head:   func(context.Context) (int64, string, error) { return 0, "", nil },
		Insert: app, Now: func() int64 { return 5 }, Checkpoints: c,
	}
	if err := ch.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if c.Counted() != 0 {
		t.Errorf("counted %d before any append", c.Counted())
	}
	if _, err := ch.Append(context.Background(), Entry{AuditID: "a", Action: "x", Outcome: OutcomeSuccess}); err != nil {
		t.Fatal(err)
	}
	if err := ch.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if c.Counted() != 1 {
		t.Errorf("counted %d, want the 1 append this Chain made", c.Counted())
	}
	// A second Checkpoint on the same Chain must not double-count it.
	if err := ch.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if c.Counted() != 1 {
		t.Errorf("counted %d after a repeat call, want 1", c.Counted())
	}
	// Without a counter, a Chain stays purely transactional.
	plain := &fakeAppend{storedSeq: 1}
	if err := (&Chain{Insert: plain, Now: func() int64 { return 1 }}).Checkpoint(); err != nil {
		t.Errorf("counterless Checkpoint: %v", err)
	}
}

func anchorLines(a *Anchor) ([]string, error) {
	b, err := os.ReadFile(a.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(string(b), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}
