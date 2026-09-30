// Package approval owns the approvals aggregate: creation on REQUIRE_APPROVAL,
// the user's Resolve, temporary grants issued by an approval, and expiry
// sweeps. No SQL against approvals or temporary_grants may live outside this
// package (internal contract §3 rule 1).
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/permission"
	"github.com/google/uuid"
)

// States mirror the approvals.state CHECK in 000001.
const (
	PENDING   = "PENDING"
	APPROVED  = "APPROVED"
	REJECTED  = "REJECTED"
	EXPIRED   = "EXPIRED"
	CANCELLED = "CANCELLED"
)

// allStates is that domain in one place. Listing a state anywhere else invites a
// second copy that drifts from the CHECK — the failure REQ-A8 is about.
var allStates = []string{PENDING, APPROVED, REJECTED, EXPIRED, CANCELLED}

// validState is the only arbiter of "is this a state". An unknown value is an
// error rather than an empty page: a typo'd filter that answers "nothing here"
// would look like an empty inbox to whoever is checking whether an approval ran.
func validState(s string) error {
	for _, v := range allStates {
		if v == s {
			return nil
		}
	}
	return fmt.Errorf("approval: %q is not an approvals.state value", s)
}

// StateNone is BlockingState's answer when nothing is bound to the execution:
// no approval was required, or none is outstanding, so the caller may proceed.
const StateNone = "NONE"

// ErrNotFound is what Store.Get returns for an unknown approval id.
var ErrNotFound = errors.New("approval: not found")

// MaxGrantDuration caps a temporary grant. 8h covers a working session while
// keeping a mis-clicked "always allow" from outliving the task that needed it.
const MaxGrantDuration = int64(8 * 60 * 60 * 1000)

// Approval is one approvals row. Timestamps are Unix milliseconds, and 0 means
// SQL NULL. An empty string on the optional id columns means SQL NULL too.
type Approval struct {
	ID                string
	TaskID            string
	ExecutionID       string
	SessionID         string
	AdapterInstanceID string
	Type              string
	RiskLevel         string
	Action            string
	Resource          string
	RequestJSON       string
	State             string
	RequestedAt       int64
	ExpiresAt         int64
	ResolvedAt        int64
	ResolvedBy        string
	ResolutionJSON    string
	Version           int64
}

// Resolved reports whether the decision is already made. A resolved approval is
// terminal — there is no legal transition out of it — which is what makes
// Resolve idempotent rather than merely re-runnable. It is the only expression
// of "already decided" in this package: the store's CAS guards call it instead
// of re-writing the comparison, so the two layers cannot drift apart.
func (a Approval) Resolved() bool { return a.State != PENDING }

// DeadlinePassed is the single statement of "this approval's time is up".
// SweepExpired's SQL has to compare columns and cannot call this, so
// TestSweepAgreesWithDeadlineBoundary pins the two to the same millisecond —
// a `<`/`<=` change on either side alone makes that case red. An ExpiresAt of 0
// means the row carries no deadline at all.
func (a Approval) DeadlinePassed(at int64) bool {
	return a.ExpiresAt != 0 && at >= a.ExpiresAt
}

// Live reports whether the approval can still be acted on at now. Expiry is
// checked here as well as by SweepExpired so a missed sweep cannot leave a
// pending approval usable.
func (a Approval) Live(now int64) bool {
	return !a.Resolved() && !a.DeadlinePassed(now)
}

// Draft is the input to Request. PrincipalID is the actor whose action needs
// approval; approvals has no requested_by column, so it travels inside
// request_json (see requestPayload) and is recovered when a grant is issued.
type Draft struct {
	PrincipalID       string
	TaskID            string
	ExecutionID       string
	SessionID         string
	AdapterInstanceID string
	Type              string
	RiskLevel         string
	Action            string
	Resource          string
	// Detail is the provider's own approval payload, kept raw (domain model
	// "Provider Raw Preservation"). Redaction still runs over it on the way in.
	Detail    json.RawMessage
	ExpiresAt int64
	// CorrelationID ties the approval's events to the command that triggered
	// them (event contract §4). Empty means the store mints one from the first
	// event's own id.
	CorrelationID string
}

// Resolution is the recorded decision.
type Resolution struct {
	State string // APPROVED | REJECTED | EXPIRED | CANCELLED
	By    string // principal id that resolved it; required for audit
	Note  string
	// Temporary asks for an ALLOW_TEMPORARY grant alongside the approval. Only
	// meaningful with APPROVED, and honoured in the same transaction.
	Temporary *GrantRequest
	// CorrelationID/CausationID carry the user command that produced this
	// decision (event contract §4). CausationID should be the id of the
	// approval_requested (or execution_approval_requested) event the user was
	// answering; empty falls back to the correlation id.
	CorrelationID string
	CausationID   string
}

// GrantRequest describes the allowance the user clicked, e.g. "full access for
// this task" -> {ScopeTask, taskID, duration}.
type GrantRequest struct {
	ScopeType permission.Scope
	ScopeID   string
	Duration  int64 // milliseconds, 0 < Duration <= MaxGrantDuration
}

// requestPayload is the shape of approvals.request_json.
type requestPayload struct {
	Schema        int             `json:"schema"`
	PrincipalID   string          `json:"principal_id"`
	Action        string          `json:"action"`
	Resource      string          `json:"resource"`
	Detail        json.RawMessage `json:"detail,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
}

// ListFilter is a read-only page request for the approvals list.
type ListFilter struct {
	// State nil means "every state". The distinction is not decoration: an empty
	// string must not quietly mean PENDING, or the panel can no longer ask for
	// "everything" (same ruling as the notification inbox's C26 ②).
	State *string
	// After is the keyset position from the previous page's last row. Internal
	// contract §2 bans offset paging, and encoding this pair into a wire cursor is
	// the transport's job — this package never sees a cursor string.
	After *ListPosition
	// Limit ≤ 0 falls back to defaultListLimit at the store.
	Limit int
}

// ListPosition is a row's place in the (requested_at, id) keyset order. The
// zero value means "no more pages", which is a pagination statement, not a
// judgement about the row — the two cursor vocabularies in contract §3 stay apart.
type ListPosition struct {
	RequestedAt int64
	ID          string
}

// RequesterPrincipal recovers the acting principal from request_json.
func (a Approval) RequesterPrincipal() (string, error) {
	var p requestPayload
	if err := json.Unmarshal([]byte(a.RequestJSON), &p); err != nil {
		return "", fmt.Errorf("approval %s: unreadable request_json: %w", a.ID, err)
	}
	if p.PrincipalID == "" {
		return "", fmt.Errorf("approval %s: request_json carries no principal_id", a.ID)
	}
	return p.PrincipalID, nil
}

// Store is the persistence port. Implementations keep the SQL inside this
// package and make each write atomic with its Event Journal row and its
// audit_entries row:
//
//   - Insert: Tx.Create (AggApproval, version 1) + approval_requested + audit.
//   - MarkResolved: Tx.Touch with a state='PENDING' guard + approval_resolved +
//     audit, plus the temporary_grants row and grant_issued when grant.ID is set.
//   - ExpirePending: Tx.Touch PENDING -> EXPIRED + approval_expired + audit.
//   - RevokeGrants: revoke expired temporary_grants + grant_expired + audit.
//
// Tx.Create / Tx.Touch validate the event type against the registry centrally
// (§1.1 rule 8), so this package does not re-check it. Approval event payloads
// stay in this package (§1.1 rule 9). None of these methods may emit an event
// for a write that did not happen — that is how Resolve stays single-event under
// a double click and under concurrency.
type Store interface {
	Insert(ctx context.Context, a Approval) error
	Get(ctx context.Context, id string) (Approval, error)
	ListByExecution(ctx context.Context, executionID string) ([]Approval, error)
	MarkResolved(ctx context.Context, current Approval, res Resolution, grant permission.Grant, at int64) (bool, error)
	ExpirePending(ctx context.Context, at int64) error
	RevokeGrants(ctx context.Context, at int64) error
	// ListPage is the read-only page behind GET /api/v1/approvals: no event, no
	// audit row, no state change, and no write of any kind.
	ListPage(ctx context.Context, f ListFilter) ([]Approval, ListPosition, error)
}

// Service is the approval.Service of internal contract §3. BlockingState is the
// only method backend may call: the verdict lives here, the task advance there.
type Service struct {
	store Store
	now   func() int64
	// newID mints UUIDv7 ids; tests substitute a deterministic one.
	newID func() (string, error)
}

func NewService(store Store, now func() int64) *Service {
	if now == nil {
		panic("approval: NewService requires a clock")
	}
	return &Service{store: store, now: now, newID: newUUIDv7}
}

func newUUIDv7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("approval: mint id: %w", err)
	}
	return id.String(), nil
}

// Request creates an approval for a REQUIRE_APPROVAL decision.
func (s *Service) Request(ctx context.Context, d Draft) (Approval, error) {
	if d.PrincipalID == "" || d.Action == "" || d.Type == "" {
		return Approval{}, fmt.Errorf("approval: Draft needs PrincipalID, Action and Type")
	}
	id, err := s.newID()
	if err != nil {
		return Approval{}, err
	}
	requestedAt := s.now()
	if d.ExpiresAt != 0 && d.ExpiresAt <= requestedAt {
		return Approval{}, fmt.Errorf("approval: expires_at %d is not after requested_at %d", d.ExpiresAt, requestedAt)
	}
	payload, err := json.Marshal(requestPayload{
		Schema: 1, PrincipalID: d.PrincipalID, Action: d.Action, Resource: d.Resource,
		Detail: d.Detail, CorrelationID: d.CorrelationID,
	})
	if err != nil {
		return Approval{}, fmt.Errorf("approval: encode request_json: %w", err)
	}
	risk := d.RiskLevel
	if risk == "" {
		risk = "UNKNOWN" // 状态置信度: never claim a risk level we were not told
	}
	a := Approval{
		ID: id, TaskID: d.TaskID, ExecutionID: d.ExecutionID, SessionID: d.SessionID,
		AdapterInstanceID: d.AdapterInstanceID, Type: d.Type, RiskLevel: risk,
		Action: d.Action, Resource: d.Resource, RequestJSON: string(payload),
		State: PENDING, RequestedAt: requestedAt, ExpiresAt: d.ExpiresAt, Version: 1,
	}
	if err := s.store.Insert(ctx, a); err != nil {
		return Approval{}, fmt.Errorf("approval: insert %s: %w", id, err)
	}
	return a, nil
}

// Resolve records the decision. Calling it again on a decided approval is a
// no-op that leaves the first answer standing: no second event, no second audit
// row, no second grant.
func (s *Service) Resolve(ctx context.Context, id string, res Resolution) error {
	current, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.Resolved() {
		return nil
	}
	if err := validateResolution(res); err != nil {
		return err
	}
	grant, err := s.grantFor(current, res)
	if err != nil {
		return err
	}
	// applied=false means a concurrent resolver won the CAS; that call wrote
	// the only event, so this one reports success without duplicating anything.
	if _, err := s.store.MarkResolved(ctx, current, res, grant, s.now()); err != nil {
		return fmt.Errorf("approval: resolve %s: %w", id, err)
	}
	return nil
}

// grantFor builds the temporary_grants row an APPROVED+Temporary decision
// promises. The zero Grant means "no grant requested".
func (s *Service) grantFor(a Approval, res Resolution) (permission.Grant, error) {
	if res.Temporary == nil {
		return permission.Grant{}, nil
	}
	if err := validateGrantRequest(*res.Temporary); err != nil {
		return permission.Grant{}, err
	}
	principal, err := a.RequesterPrincipal()
	if err != nil {
		return permission.Grant{}, err
	}
	id, err := s.newID()
	if err != nil {
		return permission.Grant{}, err
	}
	at := s.now()
	g := permission.Grant{
		ID: id, PrincipalID: principal,
		ScopeType: res.Temporary.ScopeType, ScopeID: res.Temporary.ScopeID,
		Action: a.Action, Effect: permission.Allow,
		StartsAt: at, ExpiresAt: at + res.Temporary.Duration,
	}
	if err := grantWindow(g.StartsAt, g.ExpiresAt); err != nil {
		return permission.Grant{}, err
	}
	return g, nil
}

// grantWindow is the single statement of internal contract §3's "temporary means
// it ends". Both the Service (before it promises anything to the user) and the
// store (before it writes the row) call THIS function, so neither layer can
// drift into its own idea of a valid lifetime. The write-side call is the one
// that matters: temporary_grants.expires_at is nullable and a 0 there used to
// read as "never expires", so a grant reaching the store with no end would have
// become a permanent privilege that no sweep ever reclaims.
func grantWindow(startsAt, expiresAt int64) error {
	switch {
	case expiresAt <= startsAt:
		return fmt.Errorf("approval: a temporary grant must end after it starts: starts_at %d, expires_at %d", startsAt, expiresAt)
	case expiresAt-startsAt > MaxGrantDuration:
		return fmt.Errorf("approval: grant window %dms exceeds the %dms cap", expiresAt-startsAt, MaxGrantDuration)
	}
	return nil
}

func validateResolution(res Resolution) error {
	if err := validState(res.State); err != nil {
		return err
	}
	if res.State == PENDING {
		return fmt.Errorf("approval: PENDING is not a decision")
	}
	if res.By == "" {
		return fmt.Errorf("approval: Resolution.By is required for audit")
	}
	if res.Temporary != nil && res.State != APPROVED {
		return fmt.Errorf("approval: a %s decision cannot also grant access", res.State)
	}
	return nil
}

// validateGrantRequest checks the half of the rule that only the request shape
// can express — which scopes may hold a grant, and whether it names its scope.
// The lifetime half deliberately is NOT here: grantWindow owns it, and it is
// checked on the absolute window so the store can check the same thing.
func validateGrantRequest(g GrantRequest) error {
	switch g.ScopeType {
	case permission.ScopeGlobal, permission.ScopeTrustDomain, permission.ScopeProject,
		permission.ScopeTask, permission.ScopeSession, permission.ScopeAdapter, permission.ScopeModel:
	case permission.ScopeTaskMode:
		// temporary_grants.scope_type has no TASK_MODE value (000001 CHECK); only
		// policy_rules does. Catch it here rather than at INSERT time.
		return fmt.Errorf("approval: a temporary grant cannot be scoped to TASK_MODE")
	default:
		return fmt.Errorf("approval: unknown grant scope_type %q", g.ScopeType)
	}
	if g.ScopeType != permission.ScopeGlobal && g.ScopeID == "" {
		return fmt.Errorf("approval: grant scope %s needs a scope_id", g.ScopeType)
	}
	return nil
}

// ForExecution lists the approvals bound to exactly this execution.
func (s *Service) ForExecution(ctx context.Context, executionID string) ([]Approval, error) {
	if executionID == "" {
		return nil, fmt.Errorf("approval: ForExecution requires an execution_id")
	}
	list, err := s.store.ListByExecution(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("approval: list %s: %w", executionID, err)
	}
	// Re-filter rather than trusting the query: a row with a NULL or different
	// execution_id must never surface here (P0 "Approval 应用于错误 Execution").
	out := make([]Approval, 0, len(list))
	for _, a := range list {
		if a.ExecutionID == executionID {
			out = append(out, a)
		}
	}
	return out, nil
}

// BlockingState is the only question backend is allowed to ask. It reports the
// state of the approval that gates this execution: PENDING while a decision is
// outstanding, otherwise the most recent resolved state, otherwise NONE.
//
// Approvals with no execution_id are excluded by construction — an approval
// raised for a different execution, or for none at all, can never be read as
// "approved" here.
func (s *Service) BlockingState(ctx context.Context, executionID string) (string, error) {
	list, err := s.ForExecution(ctx, executionID)
	if err != nil {
		return "", err
	}
	var (
		pending *Approval
		newest  *Approval
	)
	for i, a := range list {
		if !a.Resolved() {
			if !a.Live(s.now()) {
				continue // expired but not yet swept: not blocking, and not approved
			}
			pending = &list[i]
			continue
		}
		if newest == nil || a.ResolvedAt >= newest.ResolvedAt {
			newest = &list[i]
		}
	}
	switch {
	case pending != nil:
		return PENDING, nil
	case newest != nil:
		return newest.State, nil
	default:
		return StateNone, nil
	}
}

// ListPage answers "which approvals are there" — the read side of the same
// aggregate, for the panel's list. It is deliberately the Service's method and not
// a query backend may write: the approvals table is this package's to read as
// well as to write (internal contract §3 rule 1).
//
// Nothing here decides anything new. The state vocabulary is validState's, the
// "is this PENDING still waiting" rule is Approval.DeadlinePassed's (see
// SQLiteStore.ListPage for how the SQL stays honest to it), and the keyset order
// is the store's. An illegal State is rejected before the store is reached, so a
// typo cannot come back as "no approvals today" — that answer would look like an
// empty inbox while the real cause was the filter.
func (s *Service) ListPage(ctx context.Context, f ListFilter) ([]Approval, ListPosition, error) {
	if f.State != nil {
		if err := validState(*f.State); err != nil {
			return nil, ListPosition{}, err
		}
	}
	list, last, err := s.store.ListPage(ctx, f)
	if err != nil {
		return nil, ListPosition{}, fmt.Errorf("approval: list page: %w", err)
	}
	return list, last, nil
}

// SweepExpired converges state that time itself decided: pending approvals past
// their expires_at become EXPIRED, and grants past theirs stop being usable.
// Read paths already refuse expired rows, so a failed sweep degrades to "an
// extra prompt", never to "a privilege that stayed open".
func (s *Service) SweepExpired(ctx context.Context) error {
	at := s.now()
	if err := s.store.ExpirePending(ctx, at); err != nil {
		return fmt.Errorf("approval: sweep approvals: %w", err)
	}
	if err := s.store.RevokeGrants(ctx, at); err != nil {
		return fmt.Errorf("approval: sweep grants: %w", err)
	}
	return nil
}
