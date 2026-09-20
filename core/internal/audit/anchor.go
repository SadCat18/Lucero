package audit

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// ChainStatus is the whole vocabulary for "can this chain be trusted". ADR-018
// fixes it at three words on purpose: signature is not implemented in v0.1, so
// "signed", "tamper-proof" and "防篡改" are not available claims, and UNKNOWN is
// a legal answer rather than a softer no.
type ChainStatus string

const (
	// StatusVerifiedLocal means every link recomputes and the chain reaches the
	// checkpoint in the anchor file. It says nothing about an attacker who can
	// rewrite the data directory, and nothing about a signed external backup.
	StatusVerifiedLocal ChainStatus = "VERIFIED_LOCAL"
	// StatusUnverified means something did not line up, or there was no
	// checkpoint to compare against. It is not proof of tampering and not proof
	// of integrity either.
	StatusUnverified ChainStatus = "UNVERIFIED"
	// StatusUnknown is the answer when there is nothing to judge yet, e.g. no
	// audit rows at all. Never report it as verified.
	StatusUnknown ChainStatus = "UNKNOWN"
)

// ErrNoAnchor means the checkpoint file does not exist yet. A missing anchor is
// reported as UNVERIFIED, never as VERIFIED_LOCAL: "we could not check" is not
// "the chain is fine".
var ErrNoAnchor = errors.New("audit: no checkpoint file")

// Anchor is ADR-018's checkpoint record: an append-only file beside the
// database, one line per checkpoint as "seq\thash\tunix_ms\n".
//
// It lives outside the database because an anchor inside it would be as
// deletable as the rows it is meant to protect (that was the rejected
// settings-table option). Two consequences are load-bearing:
//
//   - Commit must only be called after a successful COMMIT, and must never lead
//     the database. A checkpoint ahead of the rows makes a crash look like
//     truncation, which is a false tampering alarm.
//   - What this detects is truncation and rewriting through the SQL or API
//     surface. It does not claim to stop someone with write access to the
//     directory. Say it that way in any UI.
type Anchor struct {
	Path string
}

// Commit appends one checkpoint line and flushes it to disk. The seq must be
// greater than the last recorded one, so a double append or a replayed batch
// cannot quietly rewrite history.
func (a *Anchor) Commit(seq int64, hash string, at int64) error {
	if a.Path == "" {
		return errors.New("audit: Anchor.Path is required")
	}
	if hash == "" {
		return fmt.Errorf("audit: checkpoint for seq %d has no hash", seq)
	}
	last, _, err := a.last(1 << 62)
	if err != nil && !errors.Is(err, ErrNoAnchor) {
		return err
	}
	if seq <= last {
		return fmt.Errorf("audit: checkpoint seq %d does not follow the recorded %d", seq, last)
	}
	f, err := os.OpenFile(a.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit: open checkpoint %s: %w", a.Path, err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%d\t%s\t%d\n", seq, hash, at); err != nil {
		return fmt.Errorf("audit: append checkpoint: %w", err)
	}
	return f.Sync()
}

// Expected returns the newest checkpoint that is not ahead of the database.
// Lines beyond atMostSeq are ignored rather than trusted, which is what keeps a
// stale or over-eager file from certifying rows that do not exist.
func (a *Anchor) Expected(atMostSeq int64) (Head, error) {
	seq, hash, err := a.last(atMostSeq)
	if err != nil {
		return Head{}, err
	}
	if seq == 0 {
		return Head{}, fmt.Errorf("%w: no usable checkpoint at or before seq %d in %s", ErrNoAnchor, atMostSeq, a.Path)
	}
	return Head{Seq: seq, Hash: hash}, nil
}

// latest returns the newest line in the file regardless of what the database
// currently holds.
func (a *Anchor) latest() (int64, string, error) { return a.last(1 << 62) }

// CheckpointEveryAppends is ADR-018's third append时机. It is a constant on
// purpose: a threshold nobody can consume yet would be a speculative knob, and
// keeping the count in the database would cost a write per audit row.
const CheckpointEveryAppends = 1000

// CheckpointCounter is the in-process "every 1000 successful appends" rule.
// Advance must only be called after the writing transaction has COMMITTED: a
// checkpoint that leads the rows turns a crash into a false tampering alarm,
// which is worse than no checkpoint at all.
type CheckpointCounter struct {
	mu     sync.Mutex
	count  int
	anchor *Anchor
	now    func() int64
}

func NewCheckpointCounter(anchor *Anchor, now func() int64) *CheckpointCounter {
	if now == nil {
		panic("audit: NewCheckpointCounter requires a clock")
	}
	return &CheckpointCounter{anchor: anchor, now: now}
}

// Advance adds n committed appends and writes one checkpoint line when the
// running total crosses a multiple of CheckpointEveryAppends. n stays small in
// practice (one audit row per approval write), so a single call crosses at most
// one boundary; crossing more than one would need an intermediate hash nobody
// has, so it is not attempted.
func (c *CheckpointCounter) Advance(n int, head Head) error {
	if n <= 0 || head.Seq == 0 || head.Hash == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	before, after := c.count, c.count+n
	c.count = after
	if after/CheckpointEveryAppends == before/CheckpointEveryAppends {
		return nil
	}
	return c.anchor.Commit(head.Seq, head.Hash, c.now())
}

// Counted reports the running total, for tests and for a shutdown hook that
// wants to know whether anything is owed.
func (c *CheckpointCounter) Counted() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

func (a *Anchor) last(atMostSeq int64) (int64, string, error) {
	f, err := os.Open(a.Path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, "", ErrNoAnchor
	}
	if err != nil {
		return 0, "", fmt.Errorf("audit: open checkpoint %s: %w", a.Path, err)
	}
	defer f.Close()

	var (
		bestSeq  int64
		bestHash string
	)
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		fields := strings.Split(text, "\t")
		if len(fields) != 3 {
			return 0, "", fmt.Errorf("audit: checkpoint line %d is not seq\\thash\\ttime: %q", line, text)
		}
		seq, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, "", fmt.Errorf("audit: checkpoint line %d has a bad seq: %q", line, fields[0])
		}
		if fields[1] == "" {
			return 0, "", fmt.Errorf("audit: checkpoint line %d has an empty hash", line)
		}
		if _, err := strconv.ParseInt(fields[2], 10, 64); err != nil {
			return 0, "", fmt.Errorf("audit: checkpoint line %d has a bad time: %q", line, fields[2])
		}
		if seq <= atMostSeq && seq >= bestSeq {
			bestSeq, bestHash = seq, fields[1]
		}
	}
	return bestSeq, bestHash, sc.Err()
}

// ErrChainBehindAnchor means the database holds fewer rows than the last
// checkpoint certifies. A checkpoint is only ever appended after a successful
// COMMIT, so this cannot happen by accident: either newest rows were deleted, or
// the database file was rolled back under the anchor. Both mean "cannot certify".
var ErrChainBehindAnchor = errors.New("audit: fewer rows than the last checkpoint")

// Check judges the rows read back from the database against the anchor. A broken
// link, or a chain that no longer reaches its checkpoint, is UNVERIFIED with the
// underlying error: that is an incident to investigate, and the remedy is never
// to re-hash or delete rows (§9.2).
//
// The expected head is the newest checkpoint at or before the stored rows, per
// ADR-018. That rule alone cannot see tail deletion — a truncated chain simply
// matches a truncated expectation — so the file's absolute newest line is
// compared against the row count as well, which is where that case surfaces.
//
// Honesty limit: rows written after the last checkpoint are covered by the links
// but not by the anchor, so deleting only that uncheckpointed tail is not
// detectable here. Reporting VERIFIED_LOCAL therefore means "verified up to
// checkpoint seq N"; anything past N is unconfirmed by the anchor.
func Check(entries []Entry, anchor *Anchor) (ChainStatus, error) {
	if len(entries) == 0 {
		return StatusUnknown, nil
	}
	if err := VerifyChain(entries, Head{}); err != nil {
		return StatusUnverified, err
	}
	dbMax := entries[len(entries)-1].Seq
	latestSeq, _, err := anchor.latest()
	if err != nil {
		return StatusUnverified, err
	}
	if dbMax < latestSeq {
		return StatusUnverified, fmt.Errorf("%w: %d rows, checkpoint at %d", ErrChainBehindAnchor, dbMax, latestSeq)
	}
	head, err := anchor.Expected(dbMax)
	if err != nil {
		return StatusUnverified, err
	}
	// Rows written after the last checkpoint are normal — that is what "every
	// 1000 appends" means — so the chain is judged up to the checkpoint instead
	// of being required to end there. Its links beyond that point were already
	// recomputed by the self-consistency pass above.
	certified := entries
	for i, e := range entries {
		if e.Seq == head.Seq {
			certified = entries[:i+1]
			break
		}
		if e.Seq > head.Seq {
			return StatusUnverified, fmt.Errorf("%w: no row at checkpoint seq %d", ErrChainBehindAnchor, head.Seq)
		}
	}
	if err := VerifyChain(certified, head); err != nil {
		return StatusUnverified, err
	}
	return StatusVerifiedLocal, nil
}
