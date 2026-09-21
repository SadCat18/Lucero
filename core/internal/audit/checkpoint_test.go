package audit

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The threshold is ADR-018's, not a knob: these tests pin the behaviour around
// the constant rather than around a configurable value.
//
// Every Advance here carries a fresh head, because that is what a committed
// append actually looks like from here. Repeating one head to fake progress is
// the double count A18-3 is about, and TestAdvanceIsIdempotentPerCommit pins
// that the counter now refuses to do it.
func TestCheckpointCounterCrossing(t *testing.T) {
	a := newAnchor(t)
	c := NewCheckpointCounter(a, func() int64 { return 1234 })
	head := func(seq int64) Head {
		return Head{Seq: seq, Hash: "hash-" + strconv.FormatInt(seq, 10)}
	}
	wantLine := func(seq int64) string {
		s := strconv.FormatInt(seq, 10)
		return s + "\thash-" + s + "\t1234"
	}

	for i := int64(1); i < CheckpointEveryAppends; i++ {
		if err := c.Advance(1, head(i)); err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
		if b, _ := os.ReadFile(a.Path); len(b) != 0 {
			t.Fatalf("a checkpoint appeared after only %d appends", i)
		}
	}
	if err := c.Advance(1, head(CheckpointEveryAppends)); err != nil {
		t.Fatal(err)
	}
	lines, err := anchorLines(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("%d checkpoint lines at the boundary, want 1: %v", len(lines), lines)
	}
	want := wantLine(int64(CheckpointEveryAppends))
	if lines[0] != want {
		t.Errorf("line = %q, want %q (seq\\thash\\ttime)", lines[0], want)
	}

	// Nothing owed until the next boundary. A checkpoint may also never rewind,
	// so the second crossing has to carry a newer head than the first one did.
	for i := int64(CheckpointEveryAppends + 1); i <= 2*CheckpointEveryAppends-2; i++ {
		if err := c.Advance(1, head(i)); err != nil {
			t.Fatal(err)
		}
	}
	if held, _ := anchorLines(a); len(held) != 1 {
		t.Errorf("mid-boundary advances wrote a checkpoint: %v", held)
	}
	if err := c.Advance(2, head(2*CheckpointEveryAppends)); err != nil {
		t.Fatal(err)
	}
	lines, err = anchorLines(a)
	if err != nil {
		t.Fatal(err)
	}
	want2 := wantLine(2 * CheckpointEveryAppends)
	if len(lines) != 2 || lines[1] != want2 {
		t.Errorf("second boundary: %v, want a line %q for the newest head", lines, want2)
	}
	if err := c.Advance(1, Head{Seq: 8, Hash: "rewound"}); err != nil {
		t.Fatal(err)
	}
	if lines, _ := anchorLines(a); len(lines) != 2 {
		t.Errorf("a rewound head must not write a line, got %v", lines)
	}
}

// A18-3, in the form the contract now states it (ADR-018 12:4x 精化): the
// threshold is a gap in the durable seq, so the same seq range advanced twice
// writes one line, not two. Two production legs really do report commits (approval's
// post-commit Checkpoint and cmd/core's seq-delta reporter), which is why a
// "only one advancer" whitelist could never have been the guarantee.
//
// Each re-report below carries a DIFFERENT n on purpose: the caller's own row
// count must not matter, or the legs can still halve the interval by disagreeing
// about it.
func TestAdvanceIsIdempotentPerCommit(t *testing.T) {
	a := newAnchor(t)
	c := NewCheckpointCounter(a, func() int64 { return 7 })

	for i := int64(1); i < CheckpointEveryAppends; i++ {
		if err := c.Advance(1, Head{Seq: i, Hash: "hash-" + strconv.FormatInt(i, 10)}); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.Counted(); got != CheckpointEveryAppends-1 {
		t.Fatalf("counted %d one short of the boundary, want %d", got, CheckpointEveryAppends-1)
	}

	commit := Head{Seq: int64(CheckpointEveryAppends), Hash: "hash-commit"}
	if err := c.Advance(1, commit); err != nil {
		t.Fatal(err)
	}
	lines, err := anchorLines(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("%d lines after the crossing, want 1: %v", len(lines), lines)
	}
	if got := c.Counted(); got != 0 {
		t.Errorf("counted %d once the anchor covers the chain, want 0 owed", got)
	}

	// The same committed head reported again by another leg, with another row
	// count each time: no line, and nothing newly owed.
	for _, n := range []int{1, 7, CheckpointEveryAppends} {
		if err := c.Advance(n, commit); err != nil {
			t.Fatal(err)
		}
		if again, _ := anchorLines(a); len(again) != 1 {
			t.Fatalf("re-advancing one commit with n=%d wrote %d lines: %v", n, len(again), again)
		}
		if got := c.Counted(); got != 0 {
			t.Fatalf("n=%d moved the tally to %d, want 0", n, got)
		}
	}
	// A stale head carrying a big n cannot move the threshold either.
	if err := c.Advance(CheckpointEveryAppends, Head{Seq: 5, Hash: "hash-stale"}); err != nil {
		t.Fatal(err)
	}
	if stale, _ := anchorLines(a); len(stale) != 1 {
		t.Errorf("a stale head wrote a line: %v", stale)
	}

	// Real progress still counts, and it counts from the anchored head rather
	// than from a tally: 999 more rows owe nothing, the 1000th writes line two.
	for i := int64(CheckpointEveryAppends + 1); i < 2*int64(CheckpointEveryAppends); i++ {
		if err := c.Advance(1, Head{Seq: i, Hash: "hash-" + strconv.FormatInt(i, 10)}); err != nil {
			t.Fatal(err)
		}
	}
	if mid, _ := anchorLines(a); len(mid) != 1 {
		t.Fatalf("999 rows short of the next gap wrote a line: %v", mid)
	}
	if got := c.Counted(); got != CheckpointEveryAppends-1 {
		t.Errorf("counted %d, want %d rows the anchor does not cover", got, CheckpointEveryAppends-1)
	}
	next := Head{Seq: 2 * int64(CheckpointEveryAppends), Hash: "hash-next"}
	if err := c.Advance(1, next); err != nil {
		t.Fatal(err)
	}
	if second, _ := anchorLines(a); len(second) != 2 {
		t.Errorf("%d lines after the second gap, want 2: %v", len(second), second)
	}
}

// The anchor pairs a seq with the hash reported for THAT seq: writing the newest
// seq against an older head's hash would read back as tampering on an intact
// chain, so an out-of-order report may neither write nor split the pair.
func TestAdvanceAnchorsOnlyTheHeadItSaw(t *testing.T) {
	a := newAnchor(t)
	c := NewCheckpointCounter(a, func() int64 { return 9 })
	for i := int64(1); i <= CheckpointEveryAppends; i++ {
		if err := c.Advance(1, Head{Seq: i, Hash: "hash-" + strconv.FormatInt(i, 10)}); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := anchorLines(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("%d lines, want 1: %v", len(lines), lines)
	}
	want := strconv.FormatInt(int64(CheckpointEveryAppends), 10) + "\thash-" +
		strconv.FormatInt(int64(CheckpointEveryAppends), 10) + "\t9"
	if lines[0] != want {
		t.Errorf("line = %q, want the seq and its own hash %q", lines[0], want)
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
