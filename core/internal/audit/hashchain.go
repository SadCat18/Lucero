package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

// ErrTampered means a stored chain is not the chain that was written.
var ErrTampered = errors.New("audit: tamper-evident chain broken")

// Outcomes mirror the audit_entries.outcome CHECK in 000001, so a typo is a
// compile error rather than a rejected INSERT at runtime. OutcomeUnknown is not
// a softer "failure": 状态置信度 requires it whenever the real result is unknown.
//
// Two rows are pinned by the event contract and use these keys in details_json,
// so that one writer's audit row is readable by every other role:
//
//   - projection rejected (event contract §5.4): action = the execution event
//     type, outcome = OutcomeUnknown, details {task_state, execution_event,
//     reason}. No event_journal row is produced, and it must never be swallowed.
//   - entering or leaving UNKNOWN (§5.5): details {occasion:"recovery",
//     evidence:"..."} with evidence non-empty and the prior state non-terminal.
const (
	OutcomeSuccess = "SUCCESS"
	OutcomeFailure = "FAILURE"
	OutcomeDenied  = "DENIED"
	OutcomeUnknown = "UNKNOWN"
)

// Entry mirrors one audit_entries row. An empty string means SQL NULL for the
// nullable columns; Seq and CreatedAt are always set on a stored row.
type Entry struct {
	Seq           int64
	AuditID       string
	PrincipalID   string
	Action        string
	ResourceType  string
	ResourceID    string
	ProjectID     string
	TaskID        string
	ExecutionID   string
	CorrelationID string
	Outcome       string
	DetailsJSON   string
	CreatedAt     int64
	PreviousHash  string
	EntryHash     string
	// Signature is the checkpoint signature over this link (ADR-012). It is not
	// part of entry_hash: it signs the chain, it is not another link in it.
	Signature string
}

// hashFields is the ordered field set feeding entry_hash. Every field is
// length-prefixed because plain concatenation is ambiguous: "ab"+"c" would hash
// identically to "a"+"bc", letting an attacker shift bytes between adjacent
// columns undetected. EntryHash is excluded (it is the output) and Signature is
// excluded (it signs the chain, it is not part of each link).
func (e Entry) hashFields(previousHash string) []string {
	return []string{
		previousHash,
		strconv.FormatInt(e.Seq, 10),
		e.AuditID, e.PrincipalID, e.Action, e.ResourceType, e.ResourceID,
		e.ProjectID, e.TaskID, e.ExecutionID, e.CorrelationID, e.Outcome,
		e.DetailsJSON,
		strconv.FormatInt(e.CreatedAt, 10),
	}
}

// ComputeEntryHash returns sha256(previous_hash || canonical fields), hex.
func ComputeEntryHash(previousHash string, e Entry) string {
	h := sha256.New()
	for _, f := range e.hashFields(previousHash) {
		fmt.Fprintf(h, "%d:%s", len(f), f)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Seal stamps previousHash and the resulting entry_hash onto a copy of e.
// Callers must persist only what Seal returns, never a hand-set EntryHash.
func Seal(e Entry, previousHash string) Entry {
	e.PreviousHash = previousHash
	e.EntryHash = ComputeEntryHash(previousHash, e)
	return e
}

// Head is a checkpoint: the seq and entry_hash the chain is expected to end at.
// It must come from somewhere the writer cannot rewrite (a signed row, the
// audit_entries.signature column, or an offline record) — a hash chain cannot
// detect deletion of its own newest entries, because a truncated chain is
// still internally valid. Zero Head skips the check.
type Head struct {
	Seq  int64
	Hash string
}

// Checkpoint pins the newest entry of an already verified chain.
func Checkpoint(entries []Entry) (Head, error) {
	if len(entries) == 0 {
		return Head{}, fmt.Errorf("%w: cannot checkpoint an empty chain", ErrTampered)
	}
	last := entries[len(entries)-1]
	return Head{Seq: last.Seq, Hash: last.EntryHash}, nil
}

// VerifyChain re-derives every link and checks sequence and timestamp order,
// then confirms the chain reaches head. It catches rewriting (hash mismatch),
// deletion of a middle entry (link and seq gap), deletion of the newest
// entries (head mismatch) and out-of-order appends. A failure is an incident:
// never "repair" it by re-hashing, and never delete or forge entries (§9.2).
//
// Passing Head{} means "check self-consistency only", not "no check performed":
// every link is still recomputed, so rewriting or deleting a middle entry is
// caught, but deleting the newest entries is not — a truncated chain is
// internally valid. Callers who need the truncation case must pass a Head from
// the anchor file (see Check), or say plainly that they did not check it.
func VerifyChain(entries []Entry, head Head) error {
	if len(entries) == 0 {
		if head == (Head{}) {
			return nil
		}
		return fmt.Errorf("%w: chain is empty but head expects seq %d", ErrTampered, head.Seq)
	}
	for i, e := range entries {
		if want := ComputeEntryHash(e.PreviousHash, e); want != e.EntryHash {
			return fmt.Errorf("%w: seq %d entry_hash is %s, recomputed %s", ErrTampered, e.Seq, e.EntryHash, want)
		}
		if i == 0 {
			if e.PreviousHash != "" {
				return fmt.Errorf("%w: first seq %d claims previous_hash %s", ErrTampered, e.Seq, e.PreviousHash)
			}
			continue
		}
		prev := entries[i-1]
		if e.PreviousHash != prev.EntryHash {
			return fmt.Errorf("%w: seq %d links to %s, predecessor %d is %s", ErrTampered, e.Seq, e.PreviousHash, prev.Seq, prev.EntryHash)
		}
		if e.Seq != prev.Seq+1 {
			return fmt.Errorf("%w: seq gap between %d and %d", ErrTampered, prev.Seq, e.Seq)
		}
		if e.CreatedAt < prev.CreatedAt {
			return fmt.Errorf("%w: seq %d created_at %d precedes %d", ErrTampered, e.Seq, e.CreatedAt, prev.CreatedAt)
		}
	}
	if head == (Head{}) {
		return nil
	}
	last := entries[len(entries)-1]
	if last.Seq != head.Seq || last.EntryHash != head.Hash {
		return fmt.Errorf("%w: chain ends at seq %d (%s), checkpoint says seq %d (%s)", ErrTampered, last.Seq, last.EntryHash, head.Seq, head.Hash)
	}
	return nil
}
