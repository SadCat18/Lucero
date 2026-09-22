// service.go is FakeAdapter's §4 reader surface: session/list, session/read,
// session/stop, model/list, approval/list and approval/respond, plus the
// session/locate stub. The turn machinery lives in main.go.
//
// The fixture data is compiled in rather than read from disk on purpose: §10 says the
// fake may only read "自己 .scratch/ 下的 fixture 状态", but T-47 made .scratch a
// per-role throwaway directory that other roles may wipe, so a fixture there would be
// unrunnable by design. In-code rows are deterministic, reviewable, and touch no
// shared path - the contradiction itself is CO-30.
package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

const (
	methodSessionList    = "session/list"
	methodSessionRead    = "session/read"
	methodSessionStop    = "session/stop"
	methodSessionLocate  = "session/locate"
	methodSessionCreate  = "session/create"
	methodModelList      = "model/list"
	methodModelSelect    = "model/select"
	methodApprovalList   = "approval/list"
	methodApprovalUpdate = "approval/respond"
)

// sessionRow is §4's session/list payload: the four fields §4 names, no more.
type sessionRow struct {
	RemoteSessionID string `json:"remote_session_id"`
	Title           string `json:"title"`
	ProjectPathHint string `json:"project_path_hint"`
	UpdatedAt       int64  `json:"updated_at"`
}

type sessionListParams struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// sessionListResult carries next_cursor, which is the only way §4's pagination can be
// distinguished from "there is nothing more".
type sessionListResult struct {
	Sessions   []sessionRow `json:"sessions"`
	NextCursor string       `json:"next_cursor"`
}

type messageRow struct {
	SequenceNo int64  `json:"sequence_no"`
	Role       string `json:"role"`
	Text       string `json:"text"`
	At         int64  `json:"at"`
}

type sessionReadParams struct {
	RemoteSessionID string `json:"remote_session_id"`
	SinceCursor     string `json:"since_cursor,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

// sessionReadResult keeps remote_revision as a string: §4 gives it no arithmetic, and
// Core only compares it to detect that the remote log moved underneath a read.
type sessionReadResult struct {
	Messages       []messageRow `json:"messages"`
	NextCursor     string       `json:"next_cursor"`
	RemoteRevision string       `json:"remote_revision"`
}

type modelRow struct {
	ModelKey string `json:"model_key"`
	Label    string `json:"label"`
	// The wire names are `parameters` and `provider_raw` per 主控's 16:2x ruling;
	// parameter_schema_json is the process_models **DB column** name, not a field on
	// this boundary. Both stay json.RawMessage because interpreting them here would
	// put Provider-specific semantics inside a protocol test double. Contract ② §4's
	// model/list row still reads `parameter_schema_json` on disk, which is why the
	// difference is reported rather than silently followed by one side.
	Parameters  json.RawMessage `json:"parameters"`
	ProviderRaw json.RawMessage `json:"provider_raw"`
}

type modelListResult struct {
	Models []modelRow `json:"models"`
}

// approvalRow is §4's "载荷必须结构化": payload is an object, never a prose string,
// because the Permission Engine has to route on it.
type approvalRow struct {
	ApprovalID      string          `json:"approval_id"`
	RemoteSessionID string          `json:"remote_session_id"`
	Action          string          `json:"action"`
	Resource        string          `json:"resource"`
	RiskLevel       string          `json:"risk_level"`
	State           string          `json:"state"`
	Payload         json.RawMessage `json:"payload"`
	RequestedAt     int64           `json:"requested_at"`
}

type approvalListResult struct {
	Approvals []approvalRow `json:"approvals"`
}

// approvalRespondParams uses "state" for the answer because that is the field name the
// event contract gives execution_approval_resolved; the value vocabulary itself is not
// frozen anywhere in specs/ (CO-31).
type approvalRespondParams struct {
	ApprovalID     string `json:"approval_id"`
	State          string `json:"state"`
	ResolvedBy     string `json:"resolved_by,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type approvalRespondResult struct {
	ApprovalID     string `json:"approval_id"`
	State          string `json:"state"`
	Resolved       bool   `json:"resolved"`
	Reason         string `json:"reason"`
	IdempotencyHit bool   `json:"idempotency_hit"`
	GrantID        string `json:"grant_id,omitempty"`
}

type sessionStopParams struct {
	RemoteSessionID string `json:"remote_session_id"`
}

// sessionStopResult must never claim a stop that did not happen: §4's rule for
// session/stop is that "已停止" as a bare answer is forbidden.
type sessionStopResult struct {
	Stopped bool   `json:"stopped"`
	TurnID  string `json:"turn_id"`
	Reason  string `json:"reason"`
}

// store is the fake's whole world: one session log, two models, and the approvals the
// turns below raise. Decisions are remembered so approval/respond can prove §4's
// idempotency rule with real state rather than a canned reply.
type store struct {
	sessions  []sessionRow
	logs      map[string][]messageRow
	approvals []*approvalRow
	decided   map[string]string // approval_id -> the state that resolved it
	responded map[string]approvalRespondResult
	// created remembers which remote session a client_request_id first produced, so
	// session/create can be genuinely idempotent. Unlike model/select's would-be
	// selection, this one is observable: session/list shows how many sessions exist.
	created map[string]string
}

func newStore(now int64) *store {
	return &store{
		sessions: []sessionRow{
			{RemoteSessionID: "rsn-1", Title: "修 §12 P0 的 redaction 洞",
				ProjectPathHint: "D:/Project/Lucero", UpdatedAt: now - 60_000},
			{RemoteSessionID: "rsn-2", Title: "空会话",
				ProjectPathHint: "D:/tmp/elsewhere", UpdatedAt: now - 120_000},
		},
		logs: map[string][]messageRow{
			"rsn-1": {
				{SequenceNo: 1, Role: "USER", Text: "第一条", At: now - 300_000},
				{SequenceNo: 2, Role: "ASSISTANT", Text: "第二条", At: now - 290_000},
				{SequenceNo: 3, Role: "USER", Text: "第三条", At: now - 280_000},
				{SequenceNo: 4, Role: "ASSISTANT", Text: "第四条", At: now - 270_000},
			},
			"rsn-2": {},
		},
		approvals: []*approvalRow{
			{ApprovalID: "apr-1", RemoteSessionID: "rsn-1", Action: "write_file",
				Resource:  "D:/Project/Lucero/specs/contracts-internal-v0.1.md",
				RiskLevel: "HIGH", State: "PENDING",
				Payload:     json.RawMessage(`{"path":"specs/contracts-internal-v0.1.md","bytes":2048}`),
				RequestedAt: now - 30_000},
		},
		decided:   map[string]string{},
		responded: map[string]approvalRespondResult{},
		created:   map[string]string{},
	}
}

// paginate is one place for §4's cursor rule: the cursor is the offset of the next
// unread row, and next_cursor is empty only when the page was the last one. Both list
// and read use it so the two cannot disagree about what a cursor means.
func paginate(total int, cursor string, limit int, page func(from, to int)) (string, error) {
	from, err := cursorOffset(cursor)
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = 2
	}
	if limit > 50 {
		limit = 50 // a fake that lets one page drain the world is not a useful fixture
	}
	to := from + limit
	if to > total {
		to = total
	}
	if from < total {
		page(from, to)
	}
	if to >= total {
		return "", nil
	}
	return strconv.Itoa(to), nil
}

func cursorOffset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(cursor)
	if err != nil || n < 0 {
		return 0, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: fmt.Sprintf("cursor %q is not an offset this fake issued", cursor)}
	}
	return n, nil
}

func (f *fake) sessionList(req *adapter.Frame) error {
	var p sessionListParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return f.reply(errReply(req, badParams(req.Method, err)))
	}
	out := sessionListResult{Sessions: []sessionRow{}}
	next, err := paginate(len(f.db.sessions), p.Cursor, p.Limit, func(from, to int) {
		out.Sessions = append(out.Sessions, f.db.sessions[from:to]...)
	})
	if err != nil {
		return f.reply(errReply(req, err))
	}
	out.NextCursor = next
	return f.reply(result(req, out))
}

func (f *fake) sessionRead(req *adapter.Frame) error {
	var p sessionReadParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return f.reply(errReply(req, badParams(req.Method, err)))
	}
	if p.RemoteSessionID == "" {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "session/read has no remote_session_id (§4)"}))
	}
	rows, ok := f.db.logs[p.RemoteSessionID]
	if !ok {
		// §15.1's SESSION_NOT_FOUND has no Go constant on purpose (Adapter-produced):
		// this is the call site that names it.
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrorCode(-32006),
			Message: fmt.Sprintf("session/read: no remote session %q", p.RemoteSessionID)}))
	}
	// The revision moves when the log is appended to, which is the only signal §4's
	// remote_revision can carry: "what you just read is not what was there before".
	rev := fmt.Sprintf("r%d-%d", len(rows), len(f.sent))
	out := sessionReadResult{Messages: []messageRow{}, RemoteRevision: rev}
	next, err := paginate(len(rows), p.SinceCursor, p.Limit, func(from, to int) {
		out.Messages = append(out.Messages, rows[from:to]...)
	})
	if err != nil {
		return f.reply(errReply(req, err))
	}
	out.NextCursor = next
	return f.reply(result(req, out))
}

func (f *fake) modelList(req *adapter.Frame) error {
	return f.reply(result(req, modelListResult{Models: fixtureModels()}))
}

// fixtureModels is the whole offer, in one place, because model/select has to refuse a
// key it did not publish and duplicating the rows would make that check a guess.
func fixtureModels() []modelRow {
	return []modelRow{
		{ModelKey: "fake-fast", Label: "Fake 快模型",
			Parameters: json.RawMessage(`{"type":"object","properties":{` +
				`"temperature":{"type":"number","minimum":0,"maximum":2}},"required":[]}`),
			ProviderRaw: json.RawMessage(`{"native_id":"fast-1","context_window":8000}`)},
		{ModelKey: "fake-strong", Label: "Fake 强模型",
			Parameters: json.RawMessage(`{"type":"object","properties":{` +
				`"temperature":{"type":"number","minimum":0,"maximum":1}},"required":[]}`),
			ProviderRaw: json.RawMessage(`{"native_id":"strong-2","context_window":32000}`)},
	}
}

// modelSelect is §4's "对已有会话切模型；不支持必须显式报错". The three refusals are
// three different codes because they are three different user conclusions (§4.1 says
// so about the WRITE_ENTRY_NONE / CAPABILITY_UNAVAILABLE pair, and the same rule
// decides the third): a malformed request is the caller's bug, a session that does not
// exist is a bad reference, and a model this Adapter never advertised is a Provider
// limitation - Phase 0 measured Qoder and Z Code as UIA-only for exactly this call.
// Reporting the last one as INVALID_PARAMS would tell Core to fix its params forever
// without the run ever becoming possible.
func (f *fake) modelSelect(req *adapter.Frame) error {
	var p adapter.ModelSelectParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return f.reply(errReply(req, badParams(req.Method, err)))
	}
	if err := p.Validate(); err != nil {
		return f.reply(errReply(req, err))
	}
	if _, ok := f.db.logs[p.RemoteSessionID]; !ok {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrorCode(-32006),
			Message: fmt.Sprintf("model/select: no remote session %q", p.RemoteSessionID)}))
	}
	if !knownModelKey(p.ModelKey) {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrCapabilityUnavailable,
			Message: fmt.Sprintf("model/select: this Adapter does not offer model %q; model/list is the offer (§4: 不支持必须显式报错)",
				p.ModelKey)}))
	}
	// No model state is kept here: §4 has no read path that reports a session's model
	// back, so storing it would be a field nothing can observe - the reply is the whole
	// answer this method owes, and what the fake actually proves is the refusal shape.
	return f.reply(result(req, adapter.ModelSelectResult{
		RemoteSessionID: p.RemoteSessionID, ModelKey: p.ModelKey}))
}

// knownModelKey reads the offer from the same table model/list answers from, so the
// two cannot drift into disagreeing about what this Adapter has.
func knownModelKey(key string) bool {
	for _, m := range fixtureModels() {
		if m.ModelKey == key {
			return true
		}
	}
	return false
}

// sessionCreate is §4's row as REQ-T8 closed it: the payload types are this package's
// own CreateSessionParams/CreateSessionResult (read straight out of internal/adapter by
// 主控, which is why the row and the code cannot disagree), and Validate() carries the
// two rules the row states - client_request_id is mandatory because this call is not
// naturally idempotent, and base_sha is either absent (CO-14) or a full 40-hex sha.
// created:false is an answer, not a failure: it is the retry hitting the session the
// first call already made.
func (f *fake) sessionCreate(req *adapter.Frame) error {
	var p adapter.CreateSessionParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return f.reply(errReply(req, badParams(req.Method, err)))
	}
	if err := p.Validate(); err != nil {
		return f.reply(errReply(req, err))
	}
	if hit, ok := f.db.created[p.ClientRequestID]; ok {
		return f.reply(result(req, adapter.CreateSessionResult{RemoteSessionID: hit, Created: false}))
	}
	id := fmt.Sprintf("rsn-new-%d", len(f.db.created)+1)
	f.db.sessions = append(f.db.sessions, sessionRow{
		RemoteSessionID: id, Title: p.Title, UpdatedAt: nowMillis(),
	})
	f.db.logs[id] = []messageRow{}
	f.db.created[p.ClientRequestID] = id
	out := adapter.CreateSessionResult{RemoteSessionID: id, Created: true}
	if err := out.Validate(); err != nil {
		// Checked on the way out too: a fake that answers without remote_session_id
		// would be handing Core a reply it cannot record as either created or hit.
		return f.reply(errReply(req, err))
	}
	return f.reply(result(req, out))
}

func (f *fake) approvalList(req *adapter.Frame) error {
	out := approvalListResult{Approvals: []approvalRow{}}
	for _, a := range f.db.approvals {
		if a.State == "PENDING" {
			out.Approvals = append(out.Approvals, *a)
		}
	}
	return f.reply(result(req, out))
}

// approvalRespond is §4's idempotent write: the second answer for the same approval
// returns the first result with idempotency_hit set, and an approval that is no longer
// pending is refused rather than silently re-resolved.
func (f *fake) approvalRespond(req *adapter.Frame) error {
	var p approvalRespondParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return f.reply(errReply(req, badParams(req.Method, err)))
	}
	if p.ApprovalID == "" {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "approval/respond has no approval_id (§4)"}))
	}
	if p.State != "APPROVED" && p.State != "DENIED" {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: fmt.Sprintf("approval/respond state %q: this fake answers APPROVED or DENIED only "+
				"(the value set is not frozen in any contract - CO-31)", p.State)}))
	}
	// §4 makes this call idempotent, so it needs a key of its own: a retry without one
	// would be indistinguishable from a deliberate second decision.
	if p.IdempotencyKey == "" {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "approval/respond without an idempotency key: §4 makes the call idempotent, " +
				"a retry would otherwise be a second decision on the same approval"}))
	}
	if hit, again := f.db.responded[p.IdempotencyKey]; again {
		hit.IdempotencyHit = true
		return f.reply(result(req, hit))
	}
	var target *approvalRow
	for _, a := range f.db.approvals {
		if a.ApprovalID == p.ApprovalID {
			target = a
			break
		}
	}
	if target == nil {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: fmt.Sprintf("approval/respond names unknown approval_id %q", p.ApprovalID)}))
	}
	res := approvalRespondResult{ApprovalID: target.ApprovalID, State: target.State,
		Reason: fmt.Sprintf("approval %s was already resolved as %s", target.ApprovalID, target.State)}
	if target.State != "PENDING" {
		// A stale decision is reported, not applied: overwriting it would make the
		// audit trail and the Adapter's answer disagree.
		return f.reply(result(req, res))
	}
	target.State = p.State
	f.db.decided[target.ApprovalID] = p.State
	res = approvalRespondResult{ApprovalID: target.ApprovalID, State: p.State, Resolved: true,
		Reason:  fmt.Sprintf("resolved by %s", orDefault(p.ResolvedBy, "core")),
		GrantID: orDefault(grantID(f.db.decided), "")}
	if res.GrantID == "" {
		res.GrantID = "grt-" + target.ApprovalID
	}
	f.db.responded[p.IdempotencyKey] = res
	return f.reply(result(req, res))
}

// sessionStop ends the generation in flight and closes that turn the way §6.1 wants to
// see it closed: a stream/state first, then turn/finished, and only then the reply.
// With nothing running it answers stopped=false plus the reason, because §4 forbids
// replying "已停止" to a session that had no generation to stop.
func (f *fake) sessionStop(req *adapter.Frame) error {
	var p sessionStopParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return f.reply(errReply(req, badParams(req.Method, err)))
	}
	t := f.open
	if t == nil {
		return f.reply(result(req, sessionStopResult{Reason: "no generation in flight to stop (§4)"}))
	}
	if p.RemoteSessionID != "" && p.RemoteSessionID != t.sessionID {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: fmt.Sprintf("session/stop names session %q but the running turn is in %q (§12's wrong-target class)",
				p.RemoteSessionID, t.sessionID)}))
	}
	f.open = nil
	if err := f.notice(t, &adapter.StreamFrame{Method: adapter.MethodStreamState,
		NormalizedState: "STOPPING", Confidence: adapter.ConfidenceHigh}); err != nil {
		return err
	}
	// No drain call here on purpose: finish is the queue's one drain point (CO-25 as
	// T-110 ruled it), and a second one beside it would make this method pass the
	// drain tests even if finish stopped draining - which is exactly what an earlier
	// draft of this file did, and u1 caught it.
	if err := f.finish(t, "CANCELLED"); err != nil {
		return err
	}
	return f.reply(result(req, sessionStopResult{Stopped: true, TurnID: t.turnID,
		Reason: "generation stopped on request (§4)"}))
}

// sessionLocate answers the way §4 says is correct for v0.1: Phase 0 measured deep
// links to session granularity in none of the five products, so CAPABILITY_UNAVAILABLE
// is the honest reply and a fake that pretended otherwise would hide the gap.
func (f *fake) sessionLocate(req *adapter.Frame) error {
	return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrCapabilityUnavailable,
		Message: "session/locate is a v0.1 stub: §4 makes CAPABILITY_UNAVAILABLE the correct answer",
		Data:    map[string]any{"code": "CAPABILITY_UNAVAILABLE"}}))
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// grantID is deliberately meaningless: the fake has no Permission Engine, and a
// fabricated-looking grant handle is exactly what §12's approval rules warn about. It
// exists so approval/list and approval/respond can be checked for carrying the same
// approval_id through a round trip.
func grantID(decided map[string]string) string {
	for id := range decided {
		return "grt-" + id
	}
	return ""
}
