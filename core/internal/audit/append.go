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

// Chain appends tamper-evident rows to audit_entries. Head reads the newest
// stored row; Insert writes into the transaction whose state change is being
// recorded. Both are injected so this package keeps its §0 direction.
type Chain struct {
	Head   func(ctx context.Context) (seq int64, hash string, err error)
	Insert Appender
	Now    func() int64

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
}

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
