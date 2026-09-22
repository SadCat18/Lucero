package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// auditColumns is the audit_entries insert list, matching Entry.rowValues order.
// seq is the AUTOINCREMENT primary key and is never named.
var auditColumns = []string{
	"audit_id", "principal_id", "action", "resource_type", "resource_id",
	"project_id", "task_id", "execution_id", "correlation_id", "outcome",
	"details_json", "previous_hash", "entry_hash", "signature", "created_at",
}

// Appender is satisfied structurally by *db.Tx.Append. audit must not import
// internal/platform/db (§0), so the write arrives as an injected interface and
// the caller binds it to the same transaction that changed the aggregate.
type Appender interface {
	Append(table string, columns []string, values []any) (int64, error)
}

// Queryer is satisfied by *sql.DB, *sql.Conn and *sql.Tx.
type Queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Entry also carries the optional signature column, excluded from entry_hash
// because it signs the chain rather than being one link in it.
func (e Entry) rowValues() []any {
	null := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	return []any{
		e.AuditID, null(e.PrincipalID), e.Action, null(e.ResourceType), null(e.ResourceID),
		null(e.ProjectID), null(e.TaskID), null(e.ExecutionID), null(e.CorrelationID),
		e.Outcome, e.DetailsJSON, null(e.PreviousHash), e.EntryHash, null(e.Signature), e.CreatedAt,
	}
}

// applyScrubbed copies the scrubbed row values back onto the Entry, so the seal
// that follows covers exactly what will be written. It keys on the column name
// rather than an index and refuses an unmapped column instead of dropping it: a
// column added to audit_entries later has to be decided about here, not silently
// left outside entry_hash. previous_hash and entry_hash are read back too, which
// is harmless — Seal overwrites both immediately after.
func applyScrubbed(e Entry, args []any) (Entry, error) {
	if len(args) != len(auditColumns) {
		return e, fmt.Errorf("got %d values for %d columns", len(args), len(auditColumns))
	}
	for i, col := range auditColumns {
		var (
			text  string
			stamp int64
			isInt bool
		)
		switch v := args[i].(type) {
		case nil:
		case string:
			text = v
		case int64:
			stamp, isInt = v, true
		default:
			return e, fmt.Errorf("column %s holds %T", col, args[i])
		}
		switch col {
		case "audit_id":
			e.AuditID = text
		case "principal_id":
			e.PrincipalID = text
		case "action":
			e.Action = text
		case "resource_type":
			e.ResourceType = text
		case "resource_id":
			e.ResourceID = text
		case "project_id":
			e.ProjectID = text
		case "task_id":
			e.TaskID = text
		case "execution_id":
			e.ExecutionID = text
		case "correlation_id":
			e.CorrelationID = text
		case "outcome":
			e.Outcome = text
		case "details_json":
			e.DetailsJSON = text
		case "previous_hash":
			e.PreviousHash = text
		case "entry_hash":
			e.EntryHash = text
		case "signature":
			e.Signature = text
		case "created_at":
			if !isInt {
				return e, fmt.Errorf("column created_at holds %T, want int64", args[i])
			}
			e.CreatedAt = stamp
		default:
			return e, fmt.Errorf("no Entry field maps to column %q", col)
		}
	}
	return e, nil
}

// Chain appends tamper-evident rows to audit_entries. Head reads the newest
// stored row; Insert writes into the transaction whose state change is being
// recorded. Scrub is the platform's redactor. All of them are injected so this
// package keeps its §0 direction.
type Chain struct {
	Head   func(ctx context.Context) (seq int64, hash string, err error)
	Insert Appender
	Now    func() int64

	// Scrub is the platform's redactor, injected rather than imported (internal
	// contract §2.2.1): the rules stay audit's, the only implementation stays db's,
	// and cmd/core binds the two so this package never imports platform/db (§0).
	//
	// Append runs it BEFORE Seal, because entry_hash has to cover the bytes that
	// land. Sealing first and letting the write path replace a value afterwards
	// leaves a row no honest recomputation reproduces, which reads as tampering
	// forever from that link on — the defect §2.2.1 was written for.
	//
	// nil means no scrubber is wired, and that is NOT neutral: db still rewrites
	// values inside Tx.Append, so the seal would cover pre-scrub bytes and the row
	// would stop verifying — the §2.2.1 defect, per row. A Chain with Scrub nil is
	// therefore only safe while everything it writes is scrub-immune, which audit
	// checks for its own shapes (ScrubRisk +
	// TestAuditRowsThisPackageWritesAreNotScrubTargets). §2.2.1's third item
	// (scrubbing twice changes no bytes) is what makes the wired case agree with
	// db's own pass; it does not cover a nil Scrub.
	//
	// The hits come back with the values because they are the point (REQ-A11):
	// a deny-key hit is found by column name and survives a second pass, while a
	// pattern hit is found by value and can never be re-found once the value is a
	// marker. Drop them here and the record silently shrinks by half.
	Scrub func(table string, columns []string, args []any) ([]any, []RedactionHit, error)

	// Checkpoints is optional: nil keeps a Chain purely transactional. When set,
	// the append count accrues inside the transaction and is only handed over
	// after the caller's write has COMMITTED (see Checkpoint) - ADR-018's
	// "every 1000 successful appends" must never lead the rows.
	Checkpoints *CheckpointCounter

	// The head is read once per transaction and then tracked locally: a second
	// Append in the same transaction cannot see the first row through the read
	// pool, because it is still inside the open write transaction.
	cached   bool
	appends  int
	lastSeq  int64
	lastHash string
	// scrubHits accumulates what Scrub found in the rows this Chain wrote, in
	// write order. See ScrubHits for why the caller has to be the one to record it.
	scrubHits []RedactionHit
}

// ScrubHits returns what the injected Scrub rewrote inside the audit rows this
// Chain has written so far — the only attribution those hits will ever get, since
// db's own pass no longer counts audit_entries (§2.2.1③). The caller records them
// in its own redaction row; audit does not write that row on its own, because
// deciding which aggregate/action it belongs to is the caller's knowledge.
func (c *Chain) ScrubHits() []RedactionHit { return c.scrubHits }

// Certified is the newest link this Chain wrote, or the zero Head if it wrote
// none. It is named apart from the Head field, which reads the stored chain.
func (c *Chain) Certified() Head { return Head{Seq: c.lastSeq, Hash: c.lastHash} }

// Checkpoint hands this transaction's committed appends to the shared counter.
// Call it only after the transaction has COMMITTED, and never on a rolled-back
// one — that is what keeps the anchor from leading the database.
func (c *Chain) Checkpoint() error {
	if c.Checkpoints == nil {
		return nil
	}
	n := c.appends
	c.appends = 0
	return c.Checkpoints.Advance(n, c.Certified())
}

// canonicalJSON re-encodes details_json the way the platform's redactor will:
// decode to a generic value and re-marshal, which sorts object keys. Sealing
// over the raw string instead would produce an entry_hash that does not match
// the bytes actually stored, so every honest verification would fail.
func canonicalJSON(raw string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", fmt.Errorf("audit: details_json is not valid JSON: %w", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ErrChainFork means another writer appended between this Chain's head read and
// its insert, so the predicted link is wrong. The transaction must roll back and
// the caller retry: appending anyway would fork the chain, and a forked chain is
// indistinguishable from tampering when someone later runs VerifyChain.
var ErrChainFork = errors.New("audit: chain head moved under us")

// Append seals e against the current head and writes it, returning its seq.
func (c *Chain) Append(ctx context.Context, e Entry) (int64, error) {
	if c.Head == nil || c.Insert == nil || c.Now == nil {
		return 0, errors.New("audit: Chain needs Head, Insert and Now")
	}
	if e.AuditID == "" || e.Action == "" {
		return 0, fmt.Errorf("audit: entry %q needs audit_id and action", e.Action)
	}
	if e.Outcome == "" {
		return 0, fmt.Errorf("audit: entry %s has no outcome", e.Action)
	}
	if e.DetailsJSON == "" {
		e.DetailsJSON = `{"schema":1}`
	}
	var err error
	e.DetailsJSON, err = canonicalJSON(e.DetailsJSON)
	if err != nil {
		return 0, err
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = c.Now()
	}
	// §2.2.1: scrub, then seal, then write — so the hash covers the bytes that land.
	if c.Scrub != nil {
		args, hits, err := c.Scrub("audit_entries", auditColumns, e.rowValues())
		if err != nil {
			return 0, fmt.Errorf("audit: scrub %s: %w", e.Action, err)
		}
		if len(args) != len(auditColumns) {
			return 0, fmt.Errorf("audit: scrub %s returned %d values for %d columns",
				e.Action, len(args), len(auditColumns))
		}
		if e, err = applyScrubbed(e, args); err != nil {
			return 0, fmt.Errorf("audit: scrub %s: %w", e.Action, err)
		}
		// Kept for the caller's redaction row (ScrubHits). Appended in write order
		// and never deduplicated here: "when are these the same hit" is the
		// enforcement point's rule, and a second copy of it on this side is the
		// drift §2.2.1 was written to end.
		c.scrubHits = append(c.scrubHits, hits...)
	}
	if !c.cached {
		head, hash, err := c.Head(ctx)
		if err != nil {
			return 0, fmt.Errorf("audit: read chain head: %w", err)
		}
		c.cached, c.lastSeq, c.lastHash = true, head, hash
	}
	e.Seq = c.lastSeq + 1
	e = Seal(e, c.lastHash)

	seq, err := c.Insert.Append("audit_entries", auditColumns, e.rowValues())
	if err != nil {
		return 0, fmt.Errorf("audit: append %s: %w", e.Action, err)
	}
	if seq != e.Seq {
		return 0, fmt.Errorf("%w: predicted seq %d, stored %d", ErrChainFork, e.Seq, seq)
	}
	c.lastSeq, c.lastHash = seq, e.EntryHash
	c.appends++
	return seq, nil
}

// RecordRedaction writes the row that names what the redactor scrubbed in this
// transaction: column, rule, count — never a value (contract §2.2). No hits, no
// row, because an empty entry would only add noise to the chain.
//
// base supplies who/what/correlation; outcome defaults to SUCCESS.
func (c *Chain) RecordRedaction(ctx context.Context, base Entry, hits []RedactionHit) (int64, error) {
	if len(hits) == 0 {
		return 0, nil
	}
	details, err := RedactionDetails(hits)
	if err != nil {
		return 0, err
	}
	e := base
	e.DetailsJSON = details
	if e.Outcome == "" {
		e.Outcome = OutcomeSuccess
	}
	return c.Append(ctx, e)
}

// HeadQuery returns a Head func that reads the newest audit_entries row from q.
// An empty chain is (0, ""), which is also what Seal expects for the first row.
//
// Pass the open transaction rather than the read pool: db.Tx has had
// QueryRowContext since REQ-A6① landed, and a pooled read cannot see a row this
// transaction already appended. The seq check in Append stays as a tripwire
// against a head that moved underneath us.
func HeadQuery(q Queryer) func(context.Context) (int64, string, error) {
	return func(ctx context.Context) (int64, string, error) {
		var (
			seq  int64
			hash *string
		)
		err := q.QueryRowContext(ctx,
			`SELECT seq, entry_hash FROM audit_entries ORDER BY seq DESC LIMIT 1`).Scan(&seq, &hash)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", nil
		}
		if err != nil {
			return 0, "", err
		}
		if hash == nil || *hash == "" {
			return 0, "", fmt.Errorf("audit: newest row seq %d has no entry_hash", seq)
		}
		return seq, *hash, nil
	}
}
