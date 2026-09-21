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

// CheckpointCounter is ADR-018's "every 1000 successful appends" rule, measured
// as a gap in the durable chain rather than as a tally of calls. Advance must
// only be called after the writing transaction has COMMITTED: a checkpoint that
// leads the rows turns a crash into a false tampering alarm, which is worse than
// no checkpoint at all.
type CheckpointCounter struct {
	mu     sync.Mutex
	anchor *Anchor
	now    func() int64

	// lastSeq is the newest chain head seen; anchoredSeq the newest one written to
	// the file. The threshold is the gap between them, so neither *which* leg
	// reports a commit nor how many times it reports it can move it: the second
	// sight of the same seq range finds a gap of zero. That is A18-3's
	// "推进必须幂等", and it replaces the "only one advancer allowed" whitelist
	// this judgement grew out of — two production legs already report commits, so
	// uniqueness was never going to hold.
	//
	// Both start from what the file already claims, so a Core that restarts
	// mid-threshold does not spend its first commit re-anchoring a range the
	// previous process anchored.
	lastSeq     int64
	lastHash    string
	anchoredSeq int64
	seeded      bool
}

func NewCheckpointCounter(anchor *Anchor, now func() int64) *CheckpointCounter {
	if now == nil {
		panic("audit: NewCheckpointCounter requires a clock")
	}
	return &CheckpointCounter{anchor: anchor, now: now}
}

// Advance accounts for the chain having grown to head and writes one checkpoint
// line once it has grown CheckpointEveryAppends past the last anchored head.
//
// n — the caller's own count of rows it appended — is advisory and deliberately
// not what the threshold counts: it is the number two legs can disagree about,
// while the seq they report is the same fact. A writer that never reports at all
// is still covered, because the next Advance's gap includes its rows.
func (c *CheckpointCounter) Advance(n int, head Head) error {
	if head.Seq == 0 || head.Hash == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.seeded {
		c.seeded = true
		if seq, _, err := c.anchor.latest(); err == nil {
			c.anchoredSeq, c.lastSeq = seq, seq
		}
	}
	if head.Seq > c.lastSeq {
		// seq and hash only ever move together, so the pair written below is one
		// real row rather than a newest seq glued to an older head's hash — an
		// unpaired line would read back as tampering on a chain that is intact.
		c.lastSeq, c.lastHash = head.Seq, head.Hash
	}
	if c.lastSeq-c.anchoredSeq < CheckpointEveryAppends {
		return nil
	}
	if err := c.anchor.Commit(c.lastSeq, c.lastHash, c.now()); err != nil {
		return err
	}
	c.anchoredSeq = c.lastSeq
	return nil
}

// Counted reports how many committed appends the anchor does not cover yet, for
// tests and for a shutdown hook that wants to know whether anything is owed.
func (c *CheckpointCounter) Counted() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastSeq <= c.anchoredSeq {
		return 0
	}
	return int(c.lastSeq - c.anchoredSeq)
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
	// No anchor configured is "cannot certify", never "certified". cmd/core hands
	// us a possibly-unset *Anchor on a fresh install, so this guard is what keeps
	// the status endpoint from taking down Core with a nil dereference.
	if anchor == nil || anchor.Path == "" {
		return StatusUnverified, ErrNoAnchor
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
