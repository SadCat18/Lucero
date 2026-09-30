package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// get calls one of §4's reader methods and decodes its reply into want.
func get(t *testing.T, p *fakeProcess, id, method string, params any, want any) *adapter.Frame {
	t.Helper()
	reply := call(t, p, id, method, params)
	if reply.Error != nil {
		t.Fatalf("%s: %s", method, reply.Error)
	}
	if want == nil {
		return reply
	}
	if err := json.Unmarshal(reply.Result, want); err != nil {
		t.Fatalf("%s reply: %v", method, err)
	}
	return reply
}

// TestFakeAdapterReadersPaginate covers §4's session/list and session/read, where the
// only real contract obligation is the cursor: an empty next_cursor must mean "no more
// pages", because Core cannot tell "at the end" from "the Provider handed me one page
// and forgot the cursor" any other way.
func TestFakeAdapterReadersPaginate(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	var page1 sessionListResult
	get(t, p, "l1", methodSessionList, sessionListParams{Limit: 1}, &page1)
	if len(page1.Sessions) != 1 || page1.NextCursor == "" {
		t.Fatalf("page 1 = %+v, want one row plus a next_cursor", page1)
	}
	if page1.Sessions[0].RemoteSessionID == "" || page1.Sessions[0].UpdatedAt == 0 {
		t.Errorf("row lacks §4's remote_session_id/updated_at: %+v", page1.Sessions[0])
	}
	var page2 sessionListResult
	get(t, p, "l2", methodSessionList, sessionListParams{Cursor: page1.NextCursor, Limit: 5}, &page2)
	if len(page2.Sessions) == 0 || page2.NextCursor != "" {
		t.Errorf("last page = %+v, want rows and an empty next_cursor", page2)
	}
	for _, s := range page2.Sessions {
		if s.RemoteSessionID == page1.Sessions[0].RemoteSessionID {
			t.Errorf("page 2 repeated %s: the cursor did not advance", s.RemoteSessionID)
		}
	}
	// A cursor this fake never issued is a request error, not an empty page.
	reply := call(t, p, "l3", methodSessionList, sessionListParams{Cursor: "not-a-cursor"})
	if reply.Error == nil || reply.Error.Code != adapter.ErrInvalidParams {
		t.Errorf("bogus cursor = %+v, want INVALID_PARAMS", reply)
	}

	var read1 sessionReadResult
	get(t, p, "r1", methodSessionRead, sessionReadParams{RemoteSessionID: "rsn-1", Limit: 2}, &read1)
	if len(read1.Messages) != 2 || read1.NextCursor == "" || read1.RemoteRevision == "" {
		t.Fatalf("read = %+v, want two messages plus next_cursor and remote_revision (§4)", read1)
	}
	var read2 sessionReadResult
	get(t, p, "r2", methodSessionRead,
		sessionReadParams{RemoteSessionID: "rsn-1", SinceCursor: read1.NextCursor}, &read2)
	if len(read2.Messages) == 0 || read2.Messages[0].SequenceNo <= read1.Messages[1].SequenceNo {
		t.Errorf("resume from %s gave %+v, want only later messages", read1.NextCursor, read2.Messages)
	}
	if read2.RemoteRevision != read1.RemoteRevision {
		t.Logf("remote_revision moved between reads: %q -> %q", read1.RemoteRevision, read2.RemoteRevision)
	}

	// §15.1's SESSION_NOT_FOUND (-32006) is Adapter-produced and deliberately has no
	// Go constant, so the number is named here the way §6 规则 5 names the ticket.
	missing := call(t, p, "r3", methodSessionRead, sessionReadParams{RemoteSessionID: "rsn-nope"})
	if missing.Error == nil || int(missing.Error.Code) != -32006 {
		t.Errorf("unknown session = %+v, want -32006 SESSION_NOT_FOUND", missing)
	}
	if data, ok := missing.Error.Data.(map[string]any); !ok || data["code"] != "SESSION_NOT_FOUND" {
		t.Errorf("unknown-session refusal carries data %v, want §15.1's name in data.code", missing.Error.Data)
	}
}

// TestFakeAdapterModelsAndApprovals covers §4's model/list, approval/list and
// approval/respond: structured payloads, and the idempotency rule that makes a retry
// safe. The approval must actually leave the pending list once answered, or the fake
// would be a fixture that pretends nothing happened.
func TestFakeAdapterModelsAndApprovals(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	var models modelListResult
	get(t, p, "m1", methodModelList, struct{}{}, &models)
	if len(models.Models) < 2 {
		t.Fatalf("model/list = %+v, want at least two rows", models.Models)
	}
	for _, m := range models.Models {
		// The wire names are parameters / provider_raw; parameter_schema_json is the
		// process_models column, not a field on this boundary.
		if !json.Valid(m.Parameters) || m.Parameters[0] != '{' {
			t.Errorf("%s: parameters = %s, want an object (§4)", m.ModelKey, m.Parameters)
		}
		if !json.Valid(m.ProviderRaw) || m.ProviderRaw[0] != '{' {
			t.Errorf("%s: provider_raw = %s, want an object", m.ModelKey, m.ProviderRaw)
		}
	}
	// The reply must not smuggle the DB column name onto the wire.
	if raw := call(t, p, "m2", methodModelList, struct{}{}); strings.Contains(string(raw.Result), "parameter_schema_json") {
		t.Errorf("model/list reply uses the DB column name on the wire: %s", raw.Result)
	}

	var pending approvalListResult
	get(t, p, "a1", methodApprovalList, struct{}{}, &pending)
	if len(pending.Approvals) != 1 || pending.Approvals[0].ApprovalID != "apr-1" {
		t.Fatalf("approval/list = %+v, want the one pending approval", pending.Approvals)
	}
	if a := pending.Approvals[0]; a.Payload[0] != '{' || a.RiskLevel == "" || a.Action == "" {
		t.Errorf("approval payload must be structured with action/resource/risk_level: %+v", a)
	}

	answer := approvalRespondParams{ApprovalID: "apr-1", State: "APPROVED",
		ResolvedBy: "core:test", IdempotencyKey: "ak-1"}
	var first approvalRespondResult
	get(t, p, "a2", methodApprovalUpdate, answer, &first)
	if !first.Resolved || first.State != "APPROVED" || first.IdempotencyHit || first.GrantID == "" {
		t.Errorf("first answer = %+v, want resolved with a grant handle", first)
	}
	// §4's idempotency rule, with the same key.
	var retry approvalRespondResult
	get(t, p, "a3", methodApprovalUpdate, answer, &retry)
	if !retry.IdempotencyHit || retry.State != first.State || retry.GrantID != first.GrantID {
		t.Errorf("retry = %+v, want the first answer with idempotency_hit", retry)
	}
	// A different key on an already-answered approval is a second opinion, not a
	// re-decision: it must report rather than flip the state.
	var late approvalRespondResult
	get(t, p, "a4", methodApprovalUpdate,
		approvalRespondParams{ApprovalID: "apr-1", State: "DENIED", IdempotencyKey: "ak-2"}, &late)
	if late.Resolved || late.State != "APPROVED" {
		t.Errorf("second decision = %+v, want it refused-and-explained, not applied", late)
	}
	if late.Reason == "" {
		t.Error("the stale decision answer carries no reason")
	}
	var after approvalListResult
	get(t, p, "a5", methodApprovalList, struct{}{}, &after)
	if len(after.Approvals) != 0 {
		t.Errorf("approval/list after the decision = %+v, want nothing pending", after.Approvals)
	}
	// §4 makes the key mandatory on this write for the same reason it makes it
	// mandatory on session/send.
	noKey := call(t, p, "a6", methodApprovalUpdate,
		approvalRespondParams{ApprovalID: "apr-1", State: "APPROVED"})
	if noKey.Error == nil || noKey.Error.Code != adapter.ErrInvalidParams {
		t.Errorf("approval/respond without a key = %+v, want INVALID_PARAMS", noKey)
	}
}

// TestFakeAdapterModelSelectKeepsRefusalsApart covers §4's "不支持必须显式报错" under
// §4.1's rule that different refusals answer different user conclusions. Three codes
// are in play over a real process and none may stand in for another: a bad request is
// the caller's bug, a missing session is a bad reference, and a model this Adapter does
// not offer is a Provider limitation (Phase 0: Qoder and Z Code are UIA-only here).
// The fourth one §4.1 names, WRITE_ENTRY_NONE, is a Core-side gate the fake cannot show
// from inside (it always declares write_entry:true), so it stays pinned in
// handshake_test.go's table rather than being faked here.
func TestFakeAdapterModelSelectKeepsRefusalsApart(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	// Offer and switch must read the same table: a fake that refuses a model it just
	// published would be its own second source of truth.
	var offer modelListResult
	get(t, p, "ms0", methodModelList, struct{}{}, &offer)
	if len(offer.Models) < 2 {
		t.Fatalf("model/list offered %d rows, want the fixture's two - an empty offer would "+
			"make the loop below pass by accident", len(offer.Models))
	}
	for i, m := range offer.Models {
		var got adapter.ModelSelectResult
		get(t, p, "ms"+string(rune('a'+i)), methodModelSelect,
			adapter.ModelSelectParams{RemoteSessionID: "rsn-1", ModelKey: m.ModelKey}, &got)
		if got.ModelKey != m.ModelKey || got.RemoteSessionID != "rsn-1" {
			t.Errorf("select(%s) = %+v, want the pair it was asked for", m.ModelKey, got)
		}
	}

	cases := []struct {
		name   string
		id     string
		params adapter.ModelSelectParams
		want   adapter.ErrorCode
		deny   string
	}{
		{"unknown model is a capability answer", "msx",
			adapter.ModelSelectParams{RemoteSessionID: "rsn-1", ModelKey: "gpt-not-here"},
			adapter.ErrCapabilityUnavailable, "not implemented"},
		{"unknown session is not a capability answer", "msy",
			adapter.ModelSelectParams{RemoteSessionID: "rsn-nope", ModelKey: "fake-fast"},
			adapter.ErrorCode(-32006), "CAPABILITY"},
		{"a missing field is the caller's bug", "msz",
			adapter.ModelSelectParams{RemoteSessionID: "rsn-1"},
			adapter.ErrInvalidParams, "CAPABILITY"},
	}
	for _, c := range cases {
		reply := call(t, p, c.id, methodModelSelect, c.params)
		if reply.Error == nil {
			t.Errorf("%s: answered %+v, want a refusal naming %s", c.name, reply.Result, c.want)
			continue
		}
		if reply.Error.Code != c.want {
			t.Errorf("%s: code = %s, want %s (§4.1: 两个码不许互相代替) - message %q",
				c.name, reply.Error.Code, c.want, reply.Error.Message)
		}
		if strings.Contains(reply.Error.Message, c.deny) {
			t.Errorf("%s: message %q reads as %q, which is a different conclusion",
				c.name, reply.Error.Message, c.deny)
		}
	}
}

// TestFakeAdapterSessionCreateIsIdempotentByClientRequestID is §4's REQ-T8 row:
// client_request_id is mandatory precisely because this call is not naturally
// idempotent, so the proof that matters is not the reply but the **session count** -
// a retry that answers "created:false" while still appending a row would be the worst
// shape available (Core records a hit, the Provider ends up with two sessions).
func TestFakeAdapterSessionCreateIsIdempotentByClientRequestID(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	count := func(id string) int {
		t.Helper()
		var l sessionListResult
		// Limit 50 because paginate's default page is two rows: an unpaged count is the
		// only way "no second session" means what it has to mean here.
		get(t, p, id, methodSessionList, sessionListParams{Limit: 50}, &l)
		return len(l.Sessions)
	}
	before := count("sc0")
	if before == 0 {
		t.Fatalf("session/list returned 0 rows: a growth check against an empty fixture passes by accident")
	}

	var first adapter.CreateSessionResult
	get(t, p, "sc1", methodSessionCreate, adapter.CreateSessionParams{
		ClientRequestID: "cr-1", Title: "新会话", BaseSHA: strings.Repeat("a", 40)}, &first)
	if !first.Created || first.RemoteSessionID == "" {
		t.Fatalf("first call = %+v, want created:true with a remote id (§4)", first)
	}
	if got := count("sc2"); got != before+1 {
		t.Fatalf("sessions went %d -> %d, want exactly one more", before, got)
	}

	var retry adapter.CreateSessionResult
	get(t, p, "sc3", methodSessionCreate,
		adapter.CreateSessionParams{ClientRequestID: "cr-1", Title: "同一件事的重试"}, &retry)
	if retry.Created {
		t.Errorf("retry answered created:true, want the idempotent hit (§4: 幂等命中不是失败)")
	}
	if retry.RemoteSessionID != first.RemoteSessionID {
		t.Errorf("retry returned %q, want the session the first call already made (%q)",
			retry.RemoteSessionID, first.RemoteSessionID)
	}
	if got := count("sc4"); got != before+1 {
		t.Errorf("the retry grew the fixture to %d sessions: the answer said hit, the state said new", got)
	}

	// A different key is a different session, and base_sha absent is legal (CO-14).
	var other adapter.CreateSessionResult
	get(t, p, "sc5", methodSessionCreate,
		adapter.CreateSessionParams{ClientRequestID: "cr-2"}, &other)
	if !other.Created || other.RemoteSessionID == first.RemoteSessionID {
		t.Errorf("second key = %+v, want a new distinct session with no base_sha", other)
	}

	for _, c := range []struct {
		id   string
		p    adapter.CreateSessionParams
		want string
	}{
		{"sc6", adapter.CreateSessionParams{Title: "no key"}, "client_request_id"},
		{"sc7", adapter.CreateSessionParams{ClientRequestID: "cr-3", BaseSHA: "deadbeef"}, "40-hex"},
	} {
		reply := call(t, p, c.id, methodSessionCreate, c.p)
		if reply.Error == nil || reply.Error.Code != adapter.ErrInvalidParams {
			t.Errorf("%s: = %+v, want INVALID_PARAMS naming %q", c.id, reply.Error, c.want)
			continue
		}
		if !strings.Contains(reply.Error.Message, c.want) {
			t.Errorf("%s: message %q does not name %q", c.id, reply.Error.Message, c.want)
		}
	}
	if got := count("sc8"); got != before+2 {
		t.Errorf("refused calls left %d sessions, want %d: a refusal must not have created anything",
			got, before+2)
	}
}

// contractRow is a §4 method-table row: `name` in column one, direction in column two.
var contractRow = regexp.MustCompile("(?m)^\\|\\s*`([a-z]+/[a-z*]+)`\\s*\\|\\s*(C→A|A→C)[^\\|]*\\|")

// contractFile walks up to the repo root: a hardcoded ../.. breaks when the package
// moves, and an unreachable contract must skip loudly rather than pass.
func contractFile(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "specs", "adapter-protocol", "adapter-protocol-v0.1.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("contract ② not reachable from this working tree; the completeness check is skipped, not passed")
	return ""
}

// section4 is the method table alone: the whole document would drag §4.1's gap prose
// and §6's notification rows in, which is exactly how p1 came back SILENT in
// internal/adapter. An empty parse is a fatal, never a vacuous pass.
func section4(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(contractFile(t))
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	body := strings.ReplaceAll(string(raw), "\r", "")
	start := strings.Index(body, "## 4. 方法表")
	if start < 0 {
		t.Fatal(`no "## 4. 方法表" heading: the anchor is stale, not the contract empty`)
	}
	rest := body[start:]
	stop := strings.Index(rest, "### 4.1")
	if stop < 0 {
		t.Fatal("no ### 4.1 after the §4 heading: the section boundary moved")
	}
	var names []string
	for _, m := range contractRow.FindAllStringSubmatch(body[start:start+stop], -1) {
		if m[2] == "C→A" {
			names = append(names, m[1])
		}
	}
	if len(names) < 10 {
		t.Fatalf("§4 parsed to %d C->A names (%v); fix the parser rather than trusting the pass", len(names), names)
	}
	return names
}

// TestFakeAdapterServesEveryContractMethod turns "FakeAdapter covers §4" from a claim in
// a progress file into an assertion: every C→A name the contract grows, this build has
// to answer with something other than "not implemented in this build". A method is
// probed with an empty object on purpose - INVALID_PARAMS is an answer about the
// request, the unbuilt marker is an answer about this binary, and only the second one
// means the surface moved.
func TestFakeAdapterServesEveryContractMethod(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)
	for _, name := range section4(t) {
		probe := name
		if strings.Contains(name, "*") {
			probe = "debug/fail" // the one debug member that cannot crash or hang the child
		}
		reply := call(t, p, "cmp-"+probe, probe, struct{}{})
		if reply.Error != nil && strings.Contains(reply.Error.Message, "not implemented in this build") {
			t.Errorf("§4 defines %q as C->A but this build answers it as unbuilt", name)
		}
		if reply.Error == nil && reply.Result == nil {
			t.Errorf("%s answered neither result nor error", probe)
		}
	}
}

// waitForTicket drains the pipe into the same log p.notifications reads from, until a
// turn/finished carrying ticket shows up. It has to share that log: call() already
// consumes every notification that arrives before the reply it waits for, so a second
// reader would steal frames and the ordering assertions downstream would be looking at
// a stream with holes in it.
func (p *fakeProcess) waitForTicket(t *testing.T, ticket string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if len(framesWithTicket(p.seen, ticket, adapter.MethodTurnFinished)) > 0 {
			return
		}
		select {
		case r := <-p.frames:
			if r.err != nil {
				t.Fatalf("read from the fake adapter while waiting for %q: %v", ticket, r.err)
			}
			p.seen = append(p.seen, r.f)
		case <-deadline:
			t.Fatalf("no turn/finished with ticket %q within 5s. log so far: %v", ticket, p.trace(t))
		}
	}
}

// framesWithTicket returns the decoded notifications for one ticket, in arrival order.
func framesWithTicket(all []*adapter.Frame, ticket, method string) []*adapter.StreamFrame {
	var out []*adapter.StreamFrame
	for _, f := range all {
		if f.Kind() != adapter.KindNotification || f.Method == "" {
			continue
		}
		s, err := adapter.DecodeStream(f)
		if err != nil || s.TurnRef != ticket {
			continue
		}
		if method == "" || s.Method == method {
			out = append(out, s)
		}
	}
	return out
}

// TestFakeAdapterQueueDrainsAtTurnFinishedNotAtNextSend is CO-25 as T-110 ruled it: §5
// promises a queued send runs "when the current turn ends", and this build used to drain
// on the *next send*. Both readings produce the identical wire order in
// TestFakeAdapterDeliversThreeStates, because that script happens to send again after
// the close - so this one closes the running turn with session/stop and sends nothing
// else afterwards. A stranded queue is the expensive failure mode: the reply already said
// "queued, position 1", so every symptom reads as success.
func TestFakeAdapterQueueDrainsAtTurnFinishedNotAtNextSend(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	// CO-25's whole question - "did the close drain it, or did the next send?" - only
	// exists while a turn is in flight, and since §6 第 6 条③ an ordinary run closes
	// itself. debug/hold is how this test asks for a run that is still going (T-386 ①).
	armHold(t, p, "dq0", true)
	call(t, p, "dq1", methodSessionSend, adapter.SendParams{RemoteSessionID: "rsn-1",
		Text: "running", Delivery: adapter.DeliveryStartNow,
		ExecutionID: "exec-dq1", IdempotencyKey: "k-dq1"})

	var queued adapter.SendResult
	reply := call(t, p, "dq2", methodSessionSend, adapter.SendParams{RemoteSessionID: "rsn-1",
		Text: "queued behind it", Delivery: adapter.DeliveryQueue,
		ExecutionID: "exec-dq2", IdempotencyKey: "k-dq2"})
	if err := json.Unmarshal(reply.Result, &queued); err != nil {
		t.Fatalf("queue reply: %v", err)
	}
	if queued.TurnID != "" || queued.QueuePosition == nil {
		t.Fatalf("queue reply = %+v, want a position and no turn yet (§5)", queued)
	}
	// §5: the queued prompt must not have started while another turn held the session.
	if got := framesWithTicket(p.seen, "dq2", ""); len(got) > 0 {
		t.Fatalf("the queued prompt emitted %d frames before its turn even closed", len(got))
	}

	// The only thing that happens next is the close: no second send, no timer.
	call(t, p, "dq3", methodSessionStop, sessionStopParams{RemoteSessionID: "rsn-1"})
	if got := framesWithTicket(p.seen, "dq1", adapter.MethodTurnFinished); len(got) == 0 {
		t.Fatal("session/stop did not close the running turn")
	}

	p.waitForTicket(t, "dq2")
	p.waitForTicket(t, "dq1")
	var claim, dispatch, finish *adapter.StreamFrame
	for _, s := range framesWithTicket(p.seen, "dq2", "") {
		switch s.Method {
		case adapter.MethodTurnClaimed:
			claim = s
		case adapter.MethodTurnDispatch:
			dispatch = s
		case adapter.MethodTurnFinished:
			finish = s
		}
	}
	if claim == nil || dispatch == nil {
		t.Errorf("the queued prompt ran without the CO-8 pair (claimed=%v dispatched=%v): "+
			"Core cannot tell a run from a queue entry that fizzled", claim != nil, dispatch != nil)
	}
	if claim != nil && claim.ExecutionID != "exec-dq2" {
		t.Errorf("queued turn claimed execution %q, want exec-dq2", claim.ExecutionID)
	}
	// §6 第 6 条 fixes the legal codes (SUCCEEDED / FAILED / CANCELLED) and this path is a
	// run that ended on its own, so the old `"OK"` - a value nobody ruled - is gone.
	if finish != nil && finish.ResultCode != "SUCCEEDED" {
		t.Errorf("queued turn closed %q, want SUCCEEDED", finish.ResultCode)
	}
	// Position, not just presence: the queued claim must land *after* dq1's close frame.
	// The check is unconditional because both frames are in the log by now - a guard like
	// "if the close is already there" is how this assertion stayed silent through a
	// mutant that emitted the queued turn first.
	posFinish, posClaim := -1, -1
	for i, f := range p.seen {
		s, err := adapter.DecodeStream(f)
		if err != nil {
			continue
		}
		if s.Method == adapter.MethodTurnFinished && s.TurnRef == "dq1" {
			posFinish = i
		}
		if s.Method == adapter.MethodTurnClaimed && s.TurnRef == "dq2" {
			posClaim = i
		}
	}
	if posFinish < 0 || posClaim < 0 {
		t.Fatalf("ordering cannot be judged: close at %d, queued claim at %d, log %v",
			posFinish, posClaim, p.trace(t))
	}
	if posClaim < posFinish {
		t.Errorf("queued claim at %d arrived before the close at %d (§5: it waits for the turn to end)", posClaim, posFinish)
	}
}

// section10 returns the two lists contract ② §10 gives this build: the "必须实现"
// method names and the debug/* injection table. Both come out of the file rather than
// out of my progress notes, because "§10 的 11 项全部到齐" is exactly the kind of
// sentence that can stay true in a ledger while stopping being true in code.
func section10(t *testing.T) (required, injections []string) {
	t.Helper()
	raw, err := os.ReadFile(contractFile(t))
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	body := strings.ReplaceAll(string(raw), "\r", "")
	start := strings.Index(body, "## 10. FakeAdapter 规格")
	if start < 0 {
		t.Fatal(`no "## 10. FakeAdapter 规格" heading: the anchor is stale, not the contract empty`)
	}
	rest := body[start:]
	stop := strings.Index(rest, "\n## 11.")
	if stop < 0 {
		t.Fatal("no ## 11 after §10: the section boundary moved")
	}
	sec := rest[:stop]

	head := strings.Index(sec, "必须实现：")
	if head < 0 {
		t.Fatal(`§10 no longer carries the "必须实现：" sentence - the list moved, do not judge green`)
	}
	tail := sec[head:]
	end := strings.Index(tail, "。")
	if end < 0 {
		t.Fatal("the 必须实现 sentence is unterminated")
	}
	names := regexp.MustCompile("`([a-z]+/[a-z*]+)`")
	for _, tok := range strings.Split(tail[:end], "、") {
		if m := names.FindStringSubmatch(tok); m != nil {
			required = append(required, m[1])
		}
	}
	if len(required) < 10 {
		t.Fatalf("§10 parsed to %d required names (%v); fix the parser rather than trusting the pass",
			len(required), required)
	}

	rows := regexp.MustCompile("(?m)^\\|\\s*`(debug/[a-z_]+)`")
	for _, m := range rows.FindAllStringSubmatch(sec, -1) {
		injections = append(injections, m[1])
	}
	if len(injections) != 4 {
		t.Fatalf("§10's injection table parsed to %d rows (%v), want the four it documents",
			len(injections), injections)
	}
	return required, injections
}

// TestFakeAdapterSatisfiesContractSection10 is §10 read literally: every method that
// sentence names must be answered by the real child with something other than
// "not implemented in this build", the stream/* family must actually reach the wire, and
// all four debug/* rows must behave as their row promises.
func TestFakeAdapterSatisfiesContractSection10(t *testing.T) {
	required, injections := section10(t)
	p := startFakeAdapter(t)
	initialize(t, p)

	for _, name := range required {
		switch name {
		case "stream/*":
			// §10's family row is output the Adapter pushes, not a call it answers, so it
			// is checked on the wire: all three stream kinds in one real turn.
			call(t, p, "s10-stream", methodSessionSend, adapter.SendParams{
				RemoteSessionID: "rsn-1", Text: "stream probe", Delivery: adapter.DeliveryStartNow,
				ExecutionID: "exec-s10", IdempotencyKey: "k-s10-stream"})
			seen := map[string]bool{}
			for _, f := range p.notifications() {
				s, err := adapter.DecodeStream(f)
				if err == nil && strings.HasPrefix(s.Method, "stream/") {
					seen[s.Method] = true
				}
			}
			for _, kind := range []string{adapter.MethodStreamDelta, adapter.MethodStreamItem, adapter.MethodStreamState} {
				if !seen[kind] {
					t.Errorf("§10 requires stream/* but %s never reached the wire", kind)
				}
			}
		case "session/send":
			// The three delivery states are this file's other tests; an empty object here
			// would only prove the validator works.
			continue
		default:
			reply := call(t, p, "s10-"+name, name, struct{}{})
			if reply.Error != nil && strings.Contains(reply.Error.Message, "not implemented in this build") {
				t.Errorf("§10 requires %q, this build answers it as unbuilt", name)
			}
			if reply.Error == nil && reply.Result == nil {
				t.Errorf("%s answered neither result nor error", name)
			}
		}
	}

	// Two of the four injections are hazards for a shared child: debug/crash is specified
	// to end the process and debug/hang never answers, so probing them inline would delete
	// the rest of the assertions silently. Each gets its own child, and the assertion is
	// the behaviour its §10 row promises rather than "it replied".
	for _, name := range injections {
		switch name {
		case "debug/crash":
			// Wait, not silent: after os.Exit(2) the reader reports EOF on the pipe, and
			// the harness treats a dead pipe as a failure. The exit status is the promise.
			crasher := startFakeAdapter(t)
			initialize(t, crasher)
			call(t, crasher, "inj-crash", name, struct{}{})
			_ = crasher.stdin.Close()
			var exit *exec.ExitError
			if err := crasher.cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 2 {
				t.Errorf("%s left the child with %v, want exit status 2 (§10: 立即 os.Exit(2))", name, err)
			}
		case "debug/hang":
			hanger := startFakeAdapter(t)
			initialize(t, hanger)
			hanger.write(t, request("inj-hang", name, map[string]any{"seconds": 30}))
			if f, idle := hanger.silent(t, 300*time.Millisecond); !idle {
				t.Errorf("%s answered %s: §10 says it hangs, so deadline and session/stop can be tested", name, f.Method)
			}
		default:
			reply := call(t, p, "inj-"+name, name, map[string]any{"code": "PROVIDER_RATE_LIMITED"})
			if reply.Error != nil && strings.Contains(reply.Error.Message, "not implemented in this build") {
				t.Errorf("§10 documents %q, this build answers it as unbuilt", name)
			}
		}
	}
}

// TestFakeAdapterStopEndsTheTurn is the first half of §6.3's chain: session/stop must
// really end the generation, and with nothing running it must say so rather than
// answer "已停止" (§4 forbids that exact shortcut).
func TestFakeAdapterStopEndsTheTurn(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	send := func(id, exec string) adapter.SendResult {
		t.Helper()
		reply := call(t, p, id, methodSessionSend, adapter.SendParams{
			RemoteSessionID: "rsn-1", Text: "x", Delivery: adapter.DeliveryStartNow,
			ExecutionID: exec, IdempotencyKey: "k-" + id})
		var res adapter.SendResult
		if err := json.Unmarshal(reply.Result, &res); err != nil {
			t.Fatalf("send %s: %v", id, err)
		}
		return res
	}
	// §4's stop is about "the turn in flight", and since §6 第 6 条③ an ordinary run no
	// longer stays in flight by itself - hold is how this test keeps one open (T-386 ①).
	armHold(t, p, "s0", true)
	first := send("s1", "exec-1")

	var stopped sessionStopResult
	get(t, p, "st1", methodSessionStop, sessionStopParams{RemoteSessionID: "rsn-1"}, &stopped)
	if !stopped.Stopped || stopped.TurnID != first.TurnID {
		t.Errorf("stop = %+v, want stopped with the running turn_id", stopped)
	}
	// The close must already be on the wire when the reply arrives. Computed from the
	// trace rather than hand-written: sequence numbers belong to the Adapter's counter
	// and shift whenever the normal path grows a frame.
	tr := p.trace(t)
	at := -1
	for i, label := range tr {
		if label == "reply#st1" {
			at = i
		}
	}
	if at < 2 {
		t.Fatalf("no reply#st1 in the trace: %v", tr)
	}
	if got := tr[at-2 : at]; !strings.HasPrefix(got[0], "stream/state/") ||
		!strings.HasPrefix(got[1], "turn/finished/") {
		t.Errorf("the two frames before the stop reply are %v, want stream/state then turn/finished", got)
	}
	if !strings.HasSuffix(tr[at-1], "@s1") {
		t.Errorf("turn/finished %q is not ticketed to the stopped turn", tr[at-1])
	}
	// Nothing is running now, so a second stop must not claim credit for one.
	var again sessionStopResult
	get(t, p, "st2", methodSessionStop, sessionStopParams{}, &again)
	if again.Stopped || again.Reason == "" {
		t.Errorf("second stop = %+v, want stopped=false with a reason (§4)", again)
	}
	// Wrong target is refused rather than stopping somebody else's generation.
	send("s2", "exec-2")
	other := call(t, p, "st3", methodSessionStop, sessionStopParams{RemoteSessionID: "rsn-2"})
	if other.Error == nil || other.Error.Code != adapter.ErrInvalidParams {
		t.Errorf("stop on another session = %+v, want INVALID_PARAMS", other)
	}
}

// TestFakeAdapterChainIsClaimedThenDispatched pins §6.3's chain as wire facts only:
// the previous turn's turn/finished precedes the next turn's turn/claimed,
// turn/claimed never names a turn_id, and only turn/dispatched does. Which Execution a
// frame belongs to is deliberately NOT asserted here - §6 规则 5 决议 ② makes that
// internal/process's job, and a test that rebuilt the mapping by hand would be proving
// the wrong layer. The execution-level half lives in the pump's
// TestConnectionChainClosesBeforeItOpens.
func TestFakeAdapterChainIsClaimedThenDispatched(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	send := func(id, exec string) {
		t.Helper()
		call(t, p, id, methodSessionSend, adapter.SendParams{
			RemoteSessionID: "rsn-1", Text: "x", Delivery: adapter.DeliveryStartNow,
			ExecutionID: exec, IdempotencyKey: "k-" + id})
	}
	armHold(t, p, "c0", true)
	send("c1", "exec-1")
	// An interruption lands on the running turn rather than opening a second one.
	steer := call(t, p, "c2", methodSessionSend, adapter.SendParams{
		RemoteSessionID: "rsn-1", Text: "steer", Delivery: adapter.DeliveryGuide,
		ExecutionID: "exec-1", IdempotencyKey: "k-c2"})
	var steerRes adapter.SendResult
	if err := json.Unmarshal(steer.Result, &steerRes); err != nil {
		t.Fatalf("guide: %v", err)
	}
	call(t, p, "c3", methodSessionStop, sessionStopParams{RemoteSessionID: "rsn-1"})
	send("c4", "exec-2")

	// Core's own bookkeeping for this exchange: the request id that started each
	// execution. §4.2 裁定 ① makes every pair frame carry that ticket alongside the
	// execution_id, so the two identities can be checked against each other instead of
	// one of them being trusted. The guide (c2) opens nothing, so it never appears here.
	startedBy := map[string]string{"c1": "exec-1", "c4": "exec-2"}
	claimed, dispatched, closed, output := 0, 0, 0, 0
	closeAt, lastClaim := -1, -1
	for i, f := range p.notifications() {
		s, err := adapter.DecodeStream(f)
		if err != nil {
			t.Fatalf("Core could not decode our own %s: %v", f.Method, err)
		}
		switch s.Method {
		case adapter.MethodTurnClaimed:
			claimed++
			lastClaim = i
			if s.TurnID != "" {
				t.Errorf("turn/claimed carried turn_id %q: §6.3 keeps it off on purpose", s.TurnID)
			}
			if s.ExecutionID == "" {
				t.Error("turn/claimed without execution_id cannot be cross-checked (§6 规则 5 子项 2)")
			}
			if want, ok := startedBy[s.TurnRef]; !ok || want != s.ExecutionID {
				t.Errorf("turn/claimed names (%q, %q), want a ticket Core issued and the execution it binds to (§4.2 裁定 ①)",
					s.TurnRef, s.ExecutionID)
			}
		case adapter.MethodTurnDispatch:
			dispatched++
			if s.TurnID == "" {
				t.Error("turn/dispatched with no turn_id cannot prove a start")
			}
			if s.ExecutionID == "" {
				t.Error("turn/dispatched without execution_id cannot be cross-checked")
			}
			if want, ok := startedBy[s.TurnRef]; !ok || want != s.ExecutionID {
				t.Errorf("turn/dispatched names (%q, %q), want the same pair as its turn/claimed (§4.2 裁定 ①)",
					s.TurnRef, s.ExecutionID)
			}
		case adapter.MethodTurnFinished:
			closed++
			closeAt = i
			fallthrough
		case adapter.MethodStreamDelta, adapter.MethodStreamState, adapter.MethodStreamItem:
			if s.Method != adapter.MethodTurnFinished {
				output++
			}
			// The other four travel on the ticket alone and never name Core's identity,
			// so a Provider log cannot pick up an execution_id (§6 规则 5 子项 3).
			if s.ExecutionID != "" {
				t.Errorf("%s carried execution_id %q past the boundary (§6 规则 5 子项 3)", s.Method, s.ExecutionID)
			}
			if s.TurnRef == "" {
				t.Errorf("%s has no turn_ref (§6 规则 5 makes it mandatory)", s.Method)
			}
		}
	}
	if claimed != 2 || dispatched != 2 || closed != 1 {
		t.Errorf("pairs = %d claimed / %d dispatched / %d finished, want 2 / 2 / 1",
			claimed, dispatched, closed)
	}
	if output == 0 {
		t.Error("no output notification reached the wire")
	}
	if closeAt < 0 || lastClaim < 0 || closeAt > lastClaim {
		t.Errorf("the close must precede the next turn/claimed: finished at %d, last claim at %d",
			closeAt, lastClaim)
	}
	if steerRes.TurnID == "" {
		t.Error("the guide must name the turn it steered into")
	}
}

// TestFakeAdapterLocateIsTheHonestStub is §4's v0.1 rule for session/locate: deep links
// were verified in none of the five products, so CAPABILITY_UNAVAILABLE is the correct
// implementation and a fake that answered as if it worked would hide the gap.
func TestFakeAdapterLocateIsTheHonestStub(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	reply := call(t, p, "loc", methodSessionLocate, map[string]string{"remote_session_id": "rsn-1"})
	if reply.Error == nil || reply.Error.Code != adapter.ErrCapabilityUnavailable {
		t.Fatalf("session/locate = %+v, want CAPABILITY_UNAVAILABLE (§4)", reply)
	}
	if !strings.Contains(reply.Error.Message, "stub") {
		t.Errorf("message %q should say it is the v0.1 stub", reply.Error.Message)
	}
	// It must stay a refusal rather than turning into "not implemented in this build",
	// which would read as a missing feature instead of a Provider limitation.
	if strings.Contains(reply.Error.Message, "not implemented") {
		t.Errorf("locate answered as an unbuilt method: %q", reply.Error.Message)
	}
}

// TestFakeAdapterEmitsEveryNotificationType is §10 ① read literally after A-8: all six
// Adapter->Core notifications must actually appear in one real process run, and each
// one where its own path owes it. The split matters: session/stop also emits a
// stream/state, so a single "did we see all six" check would pass even if the normal
// turn path never sent one. Collecting from the wire rather than from this file's
// expectations is what keeps the guard honest.
func TestFakeAdapterEmitsEveryNotificationType(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	seen := map[string]bool{}
	note := func() {
		for _, f := range p.notifications() {
			s, err := adapter.DecodeStream(f)
			if err != nil {
				t.Fatalf("undecodable notification %s: %v", f.Method, err)
			}
			seen[s.Method] = true
		}
	}
	call(t, p, "n1", methodSessionSend, adapter.SendParams{
		RemoteSessionID: "rsn-1", Text: "hello", Delivery: adapter.DeliveryStartNow,
		ExecutionID: "exec-n", IdempotencyKey: "kn"})
	note()
	for _, want := range []string{adapter.MethodTurnClaimed, adapter.MethodTurnDispatch,
		adapter.MethodStreamState, adapter.MethodStreamDelta, adapter.MethodStreamItem} {
		if !seen[want] {
			t.Errorf("%s never reached the wire during the turn: §6 规则 1 makes the first three "+
				"mandatory on the normal path and §10 ① wants every type exercised", want)
		}
	}

	call(t, p, "n2", methodSessionStop, map[string]string{"remote_session_id": "rsn-1"})
	note()
	if !seen[adapter.MethodTurnFinished] {
		t.Errorf("turn/finished never reached the wire; §6 规则 1 keeps it the one mandatory signal")
	}
}
