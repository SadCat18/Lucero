package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/eventjournal"
	"github.com/SadCat18/lucero/core/internal/permission"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// SQLiteStore is the only place in the tree allowed to write the approvals and
// temporary_grants tables (internal contract §3 rule 1). Every write goes
// through the db platform layer — Create for the first row, Touch for a state
// change — so version-taking, mandatory redaction and the Event Journal row
// cannot be skipped, and each write is atomic with its audit_entries row.
type SQLiteStore struct {
	d     *db.DB
	now   func() int64
	newID func() (string, error)
	// cp writes ADR-018's "every 1000 successful appends" checkpoint line.
	// Unset by default: the store still links every audit row, it just leaves
	// anchoring to whoever owns the file.
	cp *audit.CheckpointCounter
}

// UseCheckpoints turns anchor writing on for this store.
func (s *SQLiteStore) UseCheckpoints(cp *audit.CheckpointCounter) { s.cp = cp }

// write runs fn in one transaction and hands its audit appends to the
// checkpoint counter only if the transaction committed. That ordering is the
// whole point: a checkpoint must never lead the rows, or a crash reads as
// truncation and a false tampering alarm is worse than no anchor at all.
func (s *SQLiteStore) write(ctx context.Context, fn func(tx *db.Tx, ch *audit.Chain) error) error {
	var ch *audit.Chain
	err := s.d.Write(ctx, func(tx *db.Tx) error {
		ch = s.chain(tx)
		return fn(tx, ch)
	})
	if err != nil || ch == nil {
		return err
	}
	return ch.Checkpoint()
}

func NewSQLiteStore(d *db.DB, now func() int64) *SQLiteStore {
	if now == nil {
		panic("approval: NewSQLiteStore requires a clock")
	}
	return &SQLiteStore{d: d, now: now, newID: newUUIDv7}
}

// selectColumns is the approvals read list; COALESCE keeps scanning free of
// sql.NullString for a table whose links are nullable by design.
const selectColumns = `id, COALESCE(task_id,''), COALESCE(execution_id,''), COALESCE(session_id,''),
	COALESCE(adapter_instance_id,''), approval_type, risk_level, action, COALESCE(resource,''),
	request_json, state, requested_at, COALESCE(expires_at,0), COALESCE(resolved_at,0),
	COALESCE(resolved_by,''), resolution_json, version`

func scanApproval(row interface{ Scan(...any) error }) (Approval, error) {
	var a Approval
	err := row.Scan(&a.ID, &a.TaskID, &a.ExecutionID, &a.SessionID, &a.AdapterInstanceID,
		&a.Type, &a.RiskLevel, &a.Action, &a.Resource, &a.RequestJSON, &a.State,
		&a.RequestedAt, &a.ExpiresAt, &a.ResolvedAt, &a.ResolvedBy, &a.ResolutionJSON, &a.Version)
	return a, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullMs(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// chain binds an audit chain writer to one open transaction. Head reads through
// the same tx, so a second append in the transaction sees the row the first one
// wrote and the chain cannot fork behind our back.
func (s *SQLiteStore) chain(tx *db.Tx) *audit.Chain {
	return &audit.Chain{Head: audit.HeadQuery(tx), Insert: tx, Now: s.now, Checkpoints: s.cp}
}

// eventIDs mints the envelope ids. Per event contract §4 a command with no
// correlation id yet uses its own first event id, and causation defaults there.
func (s *SQLiteStore) eventIDs(correlation, causation string) (eventID, corr, caus string, err error) {
	if eventID, err = s.newID(); err != nil {
		return
	}
	if corr = correlation; corr == "" {
		corr = eventID
	}
	if caus = causation; caus == "" {
		caus = corr
	}
	return eventID, corr, caus, nil
}

func (s *SQLiteStore) Insert(ctx context.Context, a Approval) error {
	principal, err := a.RequesterPrincipal()
	if err != nil {
		return err
	}
	return s.write(ctx, func(tx *db.Tx, ch *audit.Chain) error {
		evID, corr, caus, err := s.eventIDs(a.correlationHint(), "")
		if err != nil {
			return err
		}
		if err := tx.Create(db.AggApproval, a.ID, []string{
			"task_id", "execution_id", "session_id", "adapter_instance_id",
			"approval_type", "risk_level", "action", "resource",
			"request_json", "state", "requested_at", "expires_at",
		}, []any{
			nullStr(a.TaskID), nullStr(a.ExecutionID), nullStr(a.SessionID), nullStr(a.AdapterInstanceID),
			a.Type, a.RiskLevel, a.Action, nullStr(a.Resource),
			a.RequestJSON, a.State, a.RequestedAt, nullMs(a.ExpiresAt),
		}, db.EventDraft{
			EventID: evID, Type: eventjournal.ApprovalRequested, Aggregate: db.AggApproval,
			AggregateID: a.ID, CorrelationID: corr, CausationID: caus, PrincipalID: principal,
			Payload: s.requestPayload(a),
		}); err != nil {
			return err
		}
		if _, err := ch.Append(ctx, s.entry(a, eventjournal.ApprovalRequested, principal, evID, corr, audit.OutcomeSuccess)); err != nil {
			return err
		}
		return s.recordHits(ctx, tx, ch, a, principal, eventjournal.ApprovalRequested)
	})
}

func (s *SQLiteStore) requestPayload(a Approval) map[string]any {
	return map[string]any{
		"schema": 1, "approval_id": a.ID, "action": a.Action, "resource": a.Resource,
		"risk_level": a.RiskLevel, "task_id": a.TaskID, "execution_id": a.ExecutionID,
	}
}

// correlationHint is not stored on the approvals row, so the Draft/Resolution
// correlation reaches the store through the row's request/resolution JSON.
func (a Approval) correlationHint() string { return correlationOf(a.RequestJSON) }

func correlationOf(raw string) string {
	var p struct {
		CorrelationID string `json:"correlation_id"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return ""
	}
	return p.CorrelationID
}

// MarkResolved applies the decision. Tx.Touch keys on id only, so the
// "still PENDING?" guard is a read taken through the same transaction. A row
// that is no longer PENDING produces no update, no event, no audit row and no
// grant — that is the idempotency guarantee under a double click.
func (s *SQLiteStore) MarkResolved(ctx context.Context, current Approval, res Resolution, grant permission.Grant, at int64) (bool, error) {
	applied := false
	err := s.write(ctx, func(tx *db.Tx, ch *audit.Chain) error {
		fresh, err := s.fetchIn(ctx, tx, current.ID)
		if err != nil {
			return err
		}
		if fresh.Resolved() {
			return nil
		}
		resolutionJSON, err := json.Marshal(map[string]any{
			"schema": 1, "state": res.State, "resolved_by": res.By, "note": res.Note,
			"grant_id": grant.ID,
		})
		if err != nil {
			return fmt.Errorf("approval: encode resolution_json: %w", err)
		}
		evID, corr, caus, err := s.eventIDs(res.CorrelationID, res.CausationID)
		if err != nil {
			return err
		}
		if _, err := tx.Touch(db.AggApproval, fresh.ID,
			"state = ?, resolved_at = ?, resolved_by = ?, resolution_json = ?",
			[]any{res.State, at, res.By, string(resolutionJSON)},
			db.EventDraft{
				EventID: evID, Type: eventjournal.ApprovalResolved, Aggregate: db.AggApproval,
				AggregateID: fresh.ID, CorrelationID: corr, CausationID: caus, PrincipalID: res.By,
				Payload: map[string]any{
					"schema": 1, "approval_id": fresh.ID, "state": res.State,
					"grant_id": grant.ID, "resolved_by": res.By,
				},
			}); err != nil {
			return err
		}
		if grant.ID != "" {
			if err := s.insertGrant(tx, grant, at, corr, caus); err != nil {
				return err
			}
		}
		base := fresh
		base.State, base.ResolvedAt, base.ResolvedBy = res.State, at, res.By
		if _, err := ch.Append(ctx, s.entry(base, eventjournal.ApprovalResolved, res.By, evID, corr, audit.OutcomeSuccess)); err != nil {
			return err
		}
		if err := s.recordHits(ctx, tx, ch, base, res.By, eventjournal.ApprovalResolved); err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}

// insertGrant writes the temporary_grants row an ALLOW_TEMPORARY decision
// promises, plus the grant_issued event §7 expects.
//
// The gate below is not a second copy of the Service's check — it is the SAME
// function (grantWindow), called at the only place that can actually mint a
// privilege. temporary_grants.expires_at is nullable and nullMs turns a 0 into
// NULL, so without this the store would happily write a grant that never ends.
// Returning an error here rolls the whole transaction back: no state change, no
// event, no audit row, which is why an invalid grant cannot half-land.
//
// temporary_grants is an aggregate since 000003 (AggGrant), so this goes through
// Create rather than Append: Append now refuses aggregate rows on purpose, which
// is what stops a grant being written without a version and an event.
func (s *SQLiteStore) insertGrant(tx *db.Tx, g permission.Grant, at int64, corr, caus string) error {
	if err := grantWindow(g.StartsAt, g.ExpiresAt); err != nil {
		return err
	}
	evID, err := s.newID()
	if err != nil {
		return err
	}
	return tx.Create(db.AggGrant, g.ID, []string{
		"principal_id", "scope_type", "scope_id", "action", "effect",
		"constraints_json", "starts_at", "expires_at", "created_at",
	}, []any{
		g.PrincipalID, string(g.ScopeType), nullStr(g.ScopeID), g.Action, string(g.Effect),
		`{"schema":1}`, g.StartsAt, nullMs(g.ExpiresAt), at,
	}, db.EventDraft{
		EventID: evID, Type: eventjournal.GrantIssued, Aggregate: db.AggGrant, AggregateID: g.ID,
		CorrelationID: corr, CausationID: caus, PrincipalID: g.PrincipalID,
		Payload: grantPayload(g),
	})
}

// grantPayload is the §6 key fields for the grant family. The id of the subject
// is the grant row's own id, never a task or session id (T-13).
func grantPayload(g permission.Grant) map[string]any {
	return map[string]any{
		"schema": 1, "grant_id": g.ID, "scope_type": string(g.ScopeType), "scope_id": g.ScopeID,
		"action": g.Action, "expires_at": g.ExpiresAt,
	}
}

// ExpirePending converges pending approvals whose deadline has passed. Each row
// is a normal Touch: one version, one approval_expired event, one audit row.
//
// `expires_at <= ?` here is the SQL spelling of Approval.DeadlinePassed, which
// is the truth the read paths use. A set query has to compare columns, so the
// rule cannot be called here — TestSweepAgreesWithDeadlineBoundary is what keeps
// the two spellings from drifting apart at the boundary millisecond.
func (s *SQLiteStore) ExpirePending(ctx context.Context, at int64) error {
	return s.write(ctx, func(tx *db.Tx, ch *audit.Chain) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id FROM approvals WHERE state = 'PENDING' AND expires_at IS NOT NULL AND expires_at <= ?`, at)
		if err != nil {
			return fmt.Errorf("approval: list expiring: %w", err)
		}
		var due []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			due = append(due, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		for _, id := range due {
			fresh, err := s.fetchIn(ctx, tx, id)
			if err != nil {
				return err
			}
			// The SQL above narrows the set; this is the decision, made with the
			// same predicate the read paths use. If the two spellings ever drift,
			// the row that is not actually past its deadline stays PENDING.
			if fresh.Resolved() || !fresh.DeadlinePassed(at) {
				continue
			}
			evID, corr, caus, err := s.eventIDs("", "")
			if err != nil {
				return err
			}
			if _, err := tx.Touch(db.AggApproval, id, "state = ?, resolved_at = ?",
				[]any{EXPIRED, at}, db.EventDraft{
					EventID: evID, Type: eventjournal.ApprovalExpired, Aggregate: db.AggApproval,
					AggregateID: id, CorrelationID: corr, CausationID: caus, PrincipalID: "system",
					Payload: map[string]any{
						"schema": 1, "approval_id": id, "state": EXPIRED,
						"expired_at": fresh.ExpiresAt,
					},
				}); err != nil {
				return err
			}
			base := fresh
			base.State, base.ResolvedAt = EXPIRED, at
			if _, err := ch.Append(ctx, s.entry(base, eventjournal.ApprovalExpired, "system", evID, corr, audit.OutcomeSuccess)); err != nil {
				return err
			}
		}
		if len(due) == 0 {
			return nil
		}
		return s.recordHits(ctx, tx, ch, Approval{ID: due[0]}, "system", eventjournal.ApprovalExpired)
	})
}

// RevokeGrants 回收了期限已过但还挂着的授权：写 revoked_at、取一版、
// 发一条 grant_expired、记一条审计，全部同一个事务。
//
// 事件类型用 grant_expired 而不是 T-13 行文里的 grant_revoked：这批行不是谁撤销的，
// 是到期了。两者都已在事件目录登记，aggregate_type 都按裁定取 grant、
// aggregate_id 用授权行自身 id。若主控确实要字面上的 revoked，改一个常量即可。
//
// reason 只有计数与来源，没有值；它仍是 payload 的一个自由文本字段，
// 所以照样要过 audit 的 value patterns（见 audit 的 TestFreeTextReasonIsScrubbedByPatterns）。
func (s *SQLiteStore) RevokeGrants(ctx context.Context, at int64) error {
	return s.write(ctx, func(tx *db.Tx, ch *audit.Chain) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, principal_id, scope_type, COALESCE(scope_id, ''), action, COALESCE(expires_at, 0)
			 FROM temporary_grants
			 WHERE revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= ?`, at)
		if err != nil {
			return fmt.Errorf("approval: list expired grants: %w", err)
		}
		type ref struct {
			id, principal, scope, scopeID, action string
			expiresAt                             int64
		}
		var due []ref
		for rows.Next() {
			var g ref
			if err := rows.Scan(&g.id, &g.principal, &g.scope, &g.scopeID, &g.action, &g.expiresAt); err != nil {
				rows.Close()
				return err
			}
			due = append(due, g)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(due) == 0 {
			return nil
		}

		for _, g := range due {
			evID, corr, caus, err := s.eventIDs("", "")
			if err != nil {
				return err
			}
			if _, err := tx.Touch(db.AggGrant, g.id, "revoked_at = ?", []any{at}, db.EventDraft{
				EventID: evID, Type: eventjournal.GrantExpired, Aggregate: db.AggGrant, AggregateID: g.id,
				CorrelationID: corr, CausationID: caus, PrincipalID: "system",
				Payload: map[string]any{
					"schema": 1, "grant_id": g.id, "scope_type": g.scope, "scope_id": g.scopeID,
					"action": g.action, "reason": "expires_at passed", "expired_at": g.expiresAt,
				},
			}); err != nil {
				return err
			}
			if _, err := ch.Append(ctx, audit.Entry{
				AuditID: "audit-" + evID, PrincipalID: "system", Action: eventjournal.GrantExpired,
				ResourceType: "grant", ResourceID: g.id, CorrelationID: corr,
				Outcome: audit.OutcomeSuccess,
				DetailsJSON: `{"schema":1,"grant_id":"` + g.id + `","scope_type":"` + g.scope +
					`","scope_id":"` + g.scopeID + `","action":"` + g.action + `"}`,
			}); err != nil {
				return err
			}
		}
		id, err := s.newID()
		if err != nil {
			return err
		}
		_, err = ch.RecordRedaction(ctx, audit.Entry{
			AuditID: "audit-" + id, PrincipalID: "system", Action: "redaction." + eventjournal.GrantExpired,
			ResourceType: "grant",
		}, toAuditHits(tx.RedactionHits()))
		return err
	})
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (Approval, error) {
	a, err := s.fetch(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Approval{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return a, err
}

func (s *SQLiteStore) fetch(ctx context.Context, id string) (Approval, error) {
	return scanApproval(s.d.Read.QueryRowContext(ctx,
		`SELECT `+selectColumns+` FROM approvals WHERE id = ?`, id))
}

// fetchIn reads inside an open transaction. Writes use this rather than the read
// pool so the "still PENDING?" guard sees the state the UPDATE is about to
// change, instead of relying on the single-writer channel to make the two agree.
func (s *SQLiteStore) fetchIn(ctx context.Context, tx *db.Tx, id string) (Approval, error) {
	return scanApproval(tx.QueryRowContext(ctx,
		`SELECT `+selectColumns+` FROM approvals WHERE id = ?`, id))
}

func (s *SQLiteStore) ListByExecution(ctx context.Context, executionID string) ([]Approval, error) {
	rows, err := s.d.Read.QueryContext(ctx,
		`SELECT `+selectColumns+` FROM approvals WHERE execution_id = ? ORDER BY requested_at`, executionID)
	if err != nil {
		return nil, fmt.Errorf("approval: list by execution: %w", err)
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// entry builds the audit row for one approval action. "谁以什么权限做了什么":
// principal, the approval's own task/execution link, and the event it echoes.
func (s *SQLiteStore) entry(a Approval, action, principal, eventID, correlation, outcome string) audit.Entry {
	return audit.Entry{
		AuditID: "audit-" + eventID, PrincipalID: principal, Action: action,
		ResourceType: "approval", ResourceID: a.ID, TaskID: a.TaskID,
		ExecutionID: a.ExecutionID, CorrelationID: correlation, Outcome: outcome,
		DetailsJSON: `{"schema":1,"approval_id":"` + a.ID + `","state":"` + a.State + `"}`,
	}
}

// recordHits writes what the redactor scrubbed in this transaction. Only column,
// rule name and count survive; the values are gone before this runs.
// recordHits names what the redactor scrubbed. It shares the transaction's
// Chain with the state-change row above it, so the two link to each other;
// a fresh Chain per append would re-read a head that cannot include a row this
// transaction has already written but not yet committed.
func (s *SQLiteStore) recordHits(ctx context.Context, tx *db.Tx, ch *audit.Chain, a Approval, principal, action string) error {
	id, err := s.newID()
	if err != nil {
		return err
	}
	_, err = ch.RecordRedaction(ctx,
		audit.Entry{
			AuditID: "audit-" + id, PrincipalID: principal, Action: "redaction." + action,
			ResourceType: "approval", ResourceID: a.ID, TaskID: a.TaskID, ExecutionID: a.ExecutionID,
		}, toAuditHits(tx.RedactionHits()))
	return err
}

// toAuditHits copies the platform's hit record into audit's row shape. Both
// packages name the same two kinds ("key"/"pattern"), and TestHitKindsAgreeAcrossPackages
// is what stops that becoming an unspoken assumption again: a silent mismatch
// would mis-attribute a deny-key hit as a regex hit inside a value covered by
// entry_hash, which cannot be corrected afterwards without breaking the chain.
func toAuditHits(hs []db.RedactionHit) []audit.RedactionHit {
	out := make([]audit.RedactionHit, 0, len(hs))
	for _, h := range hs {
		out = append(out, audit.RedactionHit{
			Column: h.Column, Kind: audit.HitKind(h.Kind), Match: h.Match, Count: h.Count,
		})
	}
	return out
}
