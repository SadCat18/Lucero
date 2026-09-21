// Package permission decides whether an action is allowed. It reads policy and
// grants; it never writes approvals, events or audit rows.
package permission

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Effect is Permission Engine output (ADR-011).
type Effect string

const (
	Allow           Effect = "ALLOW"
	Deny            Effect = "DENY"
	RequireApproval Effect = "REQUIRE_APPROVAL"
	AllowTemporary  Effect = "ALLOW_TEMPORARY"
)

// Scope is the scope_type domain shared with policy_rules and temporary_grants.
// temporary_grants has no TASK_MODE column value (see 000001 CHECK), so a grant
// must never be created with ScopeTaskMode.
type Scope string

const (
	ScopeGlobal      Scope = "GLOBAL"
	ScopeTrustDomain Scope = "TRUST_DOMAIN"
	ScopeProject     Scope = "PROJECT"
	ScopeTaskMode    Scope = "TASK_MODE"
	ScopeTask        Scope = "TASK"
	ScopeSession     Scope = "SESSION"
	ScopeAdapter     Scope = "ADAPTER"
	ScopeModel       Scope = "MODEL"
)

// Request is the decision input, per internal contract §3.
type Request struct {
	PrincipalID   string
	ProjectID     string
	TrustDomainID string
	TaskID        string
	ExecutionID   string
	SessionID     string
	AdapterID     string
	ModelID       string
	TaskMode      string
	Action        string
	Resource      string
}

// Rule is one enabled policy_rules row.
type Rule struct {
	ID string // reported as Reason when this rule decides, so the
	// decision names its own source
	ScopeType       Scope
	ScopeID         string // empty means "any id within this scope"
	ActionPattern   string
	ResourcePattern string
	Effect          Effect
	Priority        int
}

// Grant is one temporary_grants row. Zero ExpiresAt/RevokedAt means SQL NULL;
// Active decides what a NULL expires_at authorises (nothing).
type Grant struct {
	ID              string
	PrincipalID     string
	ScopeType       Scope
	ScopeID         string
	Action          string
	ResourcePattern string
	Effect          Effect
	StartsAt        int64
	ExpiresAt       int64
	RevokedAt       int64
}

// Active reports whether the grant is usable at now (Unix ms). Expiry is
// enforced here, not only by SweepExpired, so a missed sweep can never leave a
// privilege alive.
//
// A grant with no expires_at is refused rather than treated as permanent:
// temporary_grants.expires_at is nullable in the schema, and reading NULL as
// "never expires" would hand whoever wrote that row an ALLOW that outlives the
// task it was granted for — the exact privilege this rung is not allowed to
// create. approval.grantWindow is the write-side spelling of the same rule.
func (g Grant) Active(now int64) bool {
	if g.RevokedAt != 0 || g.StartsAt > now {
		return false
	}
	return g.ExpiresAt != 0 && now < g.ExpiresAt
}

// Source is the persistence port. Implementations return only enabled rows.
type Source interface {
	EmergencyStopped(ctx context.Context) (bool, error)
	// Rules returns the enabled policy rules that could matter. Filtering by
	// scope, id and pattern is the Engine's job, so the row set may be broader.
	Rules(ctx context.Context) ([]Rule, error)
	Grants(ctx context.Context, principalID string) ([]Grant, error)
}

// Reason names what produced an Effect. §3.1 requires the default to be visible
// in the output: an invisible fail-open is worse than a visible fail-closed, and
// it is the breeding ground of the "repeated high-risk action" P0.
//
// (The contract writes the return as "(Effect, Reason string, error)", which is
// not valid Go; a named string type carries the same meaning and is checked.)
type Reason string

const (
	// The deny rungs report the row that fired, not a generic label: "which rule
	// blocked me" is the question anyone reading this will ask next.
	ReasonEmergencyStop  Reason = "EMERGENCY_STOP"
	ReasonGrantDeny      Reason = "GRANT_DENY"
	ReasonPolicyRule     Reason = "POLICY_RULE"
	ReasonTemporaryGrant Reason = "TEMPORARY_GRANT"

	ReasonDefaultHighRisk Reason = "DEFAULT_HIGH_RISK"
	ReasonDefaultUnknown  Reason = "DEFAULT_UNKNOWN_RISK"
	ReasonDefaultAllow    Reason = "DEFAULT_ALLOW"
)

// highRiskActions and lowMediumActions are §3.1's built-in classifier. They are
// not read from policy_rules on purpose: an empty strategy table must still
// classify. An action in neither list is UNKNOWN and fails closed, so a newly
// invented action is gated until someone registers it.
var (
	highRiskActions = []string{
		"filesystem.write", "command.exec", "db.write", "deploy",
		"remote.*", "secret.read", "adapter.debug.*",
	}
	lowMediumActions = []string{
		"session.list", "session.read", "session.send", "model.select",
		"execution_started",
	}
)

// defaultFor is the chain's last rung.
func defaultFor(action string) (Effect, Reason) {
	for _, p := range highRiskActions {
		if matchPattern(p, action) {
			return RequireApproval, ReasonDefaultHighRisk
		}
	}
	for _, p := range lowMediumActions {
		if matchPattern(p, action) {
			return Allow, ReasonDefaultAllow
		}
	}
	return RequireApproval, ReasonDefaultUnknown
}

// Engine evaluates the chain frozen in handoff §7.2 / ADR-011:
//
//	emergency stop -> explicit deny -> trust domain -> project -> task mode
//	-> session override -> temporary grant -> the §3.1 risk default
//
// A rule at any rung overrides the default; the default only applies when no
// rule and no grant matched.

// ScopeTask/ScopeAdapter/ScopeModel are policy_rules scopes that §7.2 does not
// name; they are placed by ascending specificity (task after task mode,
// adapter and model after session override) rather than being dropped.
//
// "Explicit deny" is implemented as a deny at ANY scope, hoisted above every
// positive rule and every grant: an ALLOW must never outrank a DENY that is
// already on the books, which is what keeps a temporary grant from overturning
// a deny.
type Engine struct {
	src Source
	now func() int64
}

// positiveRungs are evaluated in order, first rung with a match wins. GLOBAL
// leads because it is the operator's own statement; §7.2's "Explicit Deny"
// pass runs above all of them, so no rung can outrank a deny.
var positiveRungs = []Scope{
	ScopeGlobal, ScopeTrustDomain, ScopeProject, ScopeTaskMode, ScopeTask,
	ScopeSession, ScopeAdapter, ScopeModel,
}

func New(src Source, now func() int64) *Engine {
	if now == nil {
		panic("permission: New requires a clock")
	}
	return &Engine{src: src, now: now}
}

// Decide returns the effect and what produced it. Anything it cannot answer —
// unknown emergency-stop state, unreadable policy or grants — is Deny plus an
// error and an empty Reason, never a silent Allow.
func (e *Engine) Decide(ctx context.Context, r Request) (Effect, Reason, error) {
	if r.PrincipalID == "" || r.Action == "" {
		return Deny, "", fmt.Errorf("permission: Request needs PrincipalID and Action")
	}
	stopped, err := e.src.EmergencyStopped(ctx)
	if err != nil {
		return Deny, "", fmt.Errorf("permission: emergency-stop state unknown: %w", err)
	}
	if stopped {
		return Deny, ReasonEmergencyStop, nil
	}
	rules, err := e.src.Rules(ctx)
	if err != nil {
		return Deny, "", fmt.Errorf("permission: policy unreadable: %w", err)
	}
	if denied := matching(rules, r, func(ru Rule) bool { return ru.Effect == Deny }); len(denied) > 0 {
		return Deny, ruleReason(best(denied)), nil
	}
	for _, scope := range positiveRungs {
		at := matching(rules, r, func(ru Rule) bool { return ru.ScopeType == scope })
		if len(at) > 0 {
			ru := best(at)
			return ru.Effect, ruleReason(ru), nil
		}
	}
	grants, err := e.src.Grants(ctx, r.PrincipalID)
	if err != nil {
		return Deny, "", fmt.Errorf("permission: grants unreadable: %w", err)
	}
	var live *Grant
	for i, g := range grants {
		if !g.Active(e.now()) || !g.matches(r) {
			continue
		}
		if g.Effect == Deny {
			return Deny, grantReason(g), nil
		}
		if live == nil {
			live = &grants[i]
		}
	}
	if live != nil {
		return AllowTemporary, grantReason(*live), nil
	}
	eff, reason := defaultFor(r.Action)
	return eff, reason, nil
}

func ruleReason(ru Rule) Reason {
	if ru.ID == "" {
		return ReasonPolicyRule
	}
	return Reason(ru.ID)
}

func grantReason(g Grant) Reason {
	if g.ID == "" {
		return ReasonTemporaryGrant
	}
	return Reason(g.ID)
}

// CheckTemporaryGrant answers the narrow question the contract exposes: is
// there a live grant for exactly this principal, scope and action?
// Service is the cross-module surface of internal contract §3/§3.1: callers ask
// Permission, they do not read policy_rules. Asserted against Engine so the
// signature cannot drift from the contract unnoticed.
type Service interface {
	Decide(ctx context.Context, r Request) (Effect, Reason, error)
	CheckTemporaryGrant(ctx context.Context, principalID, scopeType, scopeID, action string) (bool, error)
}

var _ Service = (*Engine)(nil)

func (e *Engine) CheckTemporaryGrant(ctx context.Context, principalID, scopeType, scopeID, action string) (bool, error) {
	if principalID == "" || action == "" {
		return false, fmt.Errorf("permission: CheckTemporaryGrant needs principalID and action")
	}
	grants, err := e.src.Grants(ctx, principalID)
	if err != nil {
		return false, fmt.Errorf("permission: grants unreadable: %w", err)
	}
	now := e.now()
	for _, g := range grants {
		if !g.Active(now) || g.Effect != Allow {
			continue
		}
		// A GLOBAL grant covers any scope; an empty scope_id covers any id
		// within its scope, matching how policy_rules are read.
		inScope := g.ScopeType == ScopeGlobal ||
			(string(g.ScopeType) == scopeType && (g.ScopeID == "" || g.ScopeID == scopeID))
		if inScope && matchPattern(g.Action, action) {
			return true, nil
		}
	}
	return false, nil
}

func (g Grant) matches(r Request) bool {
	if g.ScopeType != ScopeGlobal && g.ScopeID != "" && g.ScopeID != r.scopeValue(g.ScopeType) {
		return false
	}
	return matchPattern(g.Action, r.Action) && matchPattern(g.ResourcePattern, r.Resource)
}

func (r Request) scopeValue(s Scope) string {
	switch s {
	case ScopeTrustDomain:
		return r.TrustDomainID
	case ScopeProject:
		return r.ProjectID
	case ScopeTaskMode:
		return r.TaskMode
	case ScopeTask:
		return r.TaskID
	case ScopeSession:
		return r.SessionID
	case ScopeAdapter:
		return r.AdapterID
	case ScopeModel:
		return r.ModelID
	default: // GLOBAL, and any scope with no request field
		return ""
	}
}

// matching filters to rules whose scope id, action and resource all apply.
func matching(rules []Rule, r Request, keep func(Rule) bool) []Rule {
	var out []Rule
	for _, ru := range rules {
		if !keep(ru) {
			continue
		}
		if ru.ScopeType != ScopeGlobal && ru.ScopeID != "" && ru.ScopeID != r.scopeValue(ru.ScopeType) {
			continue
		}
		if matchPattern(ru.ActionPattern, r.Action) && matchPattern(ru.ResourcePattern, r.Resource) {
			out = append(out, ru)
		}
	}
	return out
}

// best returns the winning rule without reordering the caller's slice.
func best(rules []Rule) Rule {
	winner := rules[0]
	for _, ru := range rules[1:] {
		if less(winner, ru) {
			winner = ru
		}
	}
	return winner
}

// less reports whether a ranks below b: priority first, then the more
// restrictive effect wins a tie, so a data-entry collision cannot become a
// privilege.
func less(a, b Rule) bool {
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	return rank(a.Effect) < rank(b.Effect)
}

func rank(e Effect) int {
	switch e {
	case Allow, AllowTemporary:
		return 1
	case RequireApproval:
		return 2
	case Deny:
		return 3
	default:
		return 0
	}
}

// matchPattern is "literal text with '*' wildcards". The stdlib path matcher is
// deliberately not used: its '*' refuses to cross '/', so "repo:*" would
// silently never match "repo:src/main.go" — a silent privilege hole in a
// resource pattern.
// ponytail: compiles per call; cache compiled patterns per rule if Decide ever
// shows up in a profile.
func matchPattern(pattern, v string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == v
	}
	re, err := regexp.Compile("^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, `.*`) + "$")
	if err != nil {
		return false
	}
	return re.MatchString(v)
}
