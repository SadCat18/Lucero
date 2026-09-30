package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// codeOf reads §15.1.1 instead of repeating its numbers here: an edit to the table
// must move this test with it rather than leave a stale literal behind. RATE_LIMITED
// and AUTH_REQUIRED have no Go constant on purpose (T-45: only codes Core produces
// get one), and looking them up is also what proves the Adapter-side rows exist.
func codeOf(t *testing.T, name string) adapter.ErrorCode {
	t.Helper()
	c, ok := adapter.CodeByName(name)
	if !ok {
		t.Fatalf("%s is not in the §15.1.1 table", name)
	}
	return c
}

// TestFakeAdapterDeliversThreeStates is §10's "三态全支持" measured as behaviour rather
// than as a capability row: each state answers with what CO-7 lets it claim, and the
// CO-8 evidence pair reaches the wire in the order that makes it readable.
func TestFakeAdapterDeliversThreeStates(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	send := func(id, delivery, key, text string) adapter.SendResult {
		t.Helper()
		reply := call(t, p, id, "session/send", adapter.SendParams{
			RemoteSessionID: "s-1", Text: text, Delivery: delivery,
			ExecutionID: "exec-" + id, IdempotencyKey: key})
		if reply.Error != nil {
			t.Fatalf("session/send %s (%s): %s", id, delivery, reply.Error)
		}
		var res adapter.SendResult
		if err := json.Unmarshal(reply.Result, &res); err != nil {
			t.Fatalf("session/send %s reply: %v", id, err)
		}
		// Core's own rule, run against the mode that was asked for: this is the check
		// that turns a quiet downgrade into a failure.
		if err := res.Validate(delivery); err != nil {
			t.Errorf("our %s reply breaks CO-7: %v", delivery, err)
		}
		return res
	}

	// §6 第 6 条③ makes an ordinary run close itself, so "a turn is still in flight" is
	// no longer the default: this test needs one, and §5's guide/queue shapes only exist
	// while something is running. debug/hold is the sanctioned way to ask for it (T-386 ①).
	armHold(t, p, "hold1", true)

	now := send("1", adapter.DeliveryStartNow, "k1", "hello")
	if now.TurnID == "" || now.ProviderExecutionID == "" || now.IdempotencyHit {
		t.Errorf("start_now = %+v, want a turn and a Provider handle", now)
	}
	// §5: guide interrupts the turn that is running; it does not open a second one.
	steer := send("2", adapter.DeliveryGuide, "k2", "and one more thing")
	if steer.TurnID != now.TurnID {
		t.Errorf("guide opened %q over the running %q (§5 does not allow that)", steer.TurnID, now.TurnID)
	}
	// CO-7: a queued answer may claim a position and nothing else.
	queued := send("3", adapter.DeliveryQueue, "k3", "later please")
	if queued.QueuePosition == nil || *queued.QueuePosition != 1 {
		t.Errorf("queue = %+v, want queue_position 1", queued)
	}
	if queued.TurnID != "" {
		t.Errorf("queued reply carried turn_id %q before the turn ran", queued.TurnID)
	}

	// §4's Idempotency-Key: the retry answers like the first call, says it is a hit,
	// and above all does not start a second turn.
	before := len(p.notifications())
	again := send("4", adapter.DeliveryStartNow, "k1", "hello")
	if !again.IdempotencyHit || again.TurnID != now.TurnID {
		t.Errorf("retry = %+v, want the first answer with idempotency_hit", again)
	}
	if got := len(p.notifications()) - before; got != 0 {
		t.Errorf("the idempotent retry emitted %d stream frames, want 0", got)
	}

	// The next send closes the running turn and drains the queue behind it, which is
	// the only thing that distinguishes "queued" from "never ran".
	next := send("5", adapter.DeliveryStartNow, "k5", "next one")
	if next.TurnID == now.TurnID {
		t.Error("the send after the queue reused the closed turn")
	}
	// The whole exchange, in arrival order, responses included: CO-8's pair has to be
	// claimed-then-dispatched-then-answered, and a log that only lists the
	// notifications cannot tell that apart from three frames that arrived after the
	// reply. sequence_no is pinned in each label, so §6 规则 2's monotonic rule is here too.
	want := strings.Join([]string{
		"reply#init",
		"reply#hold1", // arming debug/hold is a call, and its reply belongs on the log
		// A turn now opens with the whole normal path §10 ① asks for, and every frame
		// of it - the CO-8 pair included - names the same ticket.
		"turn/claimed/1@1", "turn/dispatched/2@1", "stream/state/3@1", "stream/delta/4@1", "stream/item/5@1",
		"reply#1",
		"stream/delta/6@1", "reply#2", // the guide's, on the same turn and ticket
		"reply#3", // queued: a position, and no turn on the stream yet
		"reply#4", // idempotent hit: nothing at all on the stream
		// the send after them interrupts the running turn: B opens on ticket 5, and A
		// is still talking afterwards - its tail carries ticket 1, and §6 第 6 条② says
		// that is the last anyone hears of A: a preempted run sends no turn-level code.
		"turn/claimed/7@5", "turn/dispatched/8@5", "stream/state/9@5", "stream/delta/10@5", "stream/item/11@5",
		"stream/delta/12@1",
		// then the queued send runs, on its own ticket, with its own evidence pair -
		// it moved because the preempted turn *ended*, not because a frame went out (CO-25).
		"turn/claimed/13@3", "turn/dispatched/14@3", "stream/state/15@3", "stream/delta/16@3", "stream/item/17@3",
		"turn/finished/18@3",
		"reply#5",
	}, " ")
	got := strings.Join(p.trace(t), " ")
	if got != want {
		t.Errorf("wire order = %s\n       want %s", got, want)
	}
	// Say the absence out loud: the order above would still match if a closing frame for
	// A merely moved position, and "no turn-level code for an interrupted run" is the
	// thing §6 第 6 条② actually rules.
	for _, fr := range strings.Fields(got) {
		if strings.HasPrefix(fr, "turn/finished/") && strings.HasSuffix(fr, "@1") {
			t.Errorf("A (ticket 1) got a turn-level code (%s); §6 第 6 条② forbids self-reporting an interruption", fr)
		}
	}
}

// TestFakeAdapterRefusesUndeliverableSend pins §5's mandatory refusals across a
// process boundary: an unknown state and a missing Idempotency-Key get named, never
// absorbed into whichever state the Adapter happens to support.
func TestFakeAdapterRefusesUndeliverableSend(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	for _, tc := range []struct {
		name string
		p    adapter.SendParams
		want adapter.ErrorCode
	}{
		{"unknown state", adapter.SendParams{RemoteSessionID: "s", Delivery: "teleport",
			IdempotencyKey: "k"}, adapter.ErrDeliveryModeUnsupported},
		{"no idempotency key", adapter.SendParams{RemoteSessionID: "s", Delivery: adapter.DeliveryStartNow},
			adapter.ErrInvalidParams},
		{"strategy off a queue", adapter.SendParams{RemoteSessionID: "s", Delivery: adapter.DeliveryStartNow,
			QueueStrategy: adapter.QueueAfterTurn, IdempotencyKey: "k"}, adapter.ErrInvalidParams},
	} {
		reply := call(t, p, "bad-"+tc.name, "session/send", tc.p)
		if reply.Error == nil {
			t.Errorf("%s: got %+v, want a refusal", tc.name, reply)
			continue
		}
		if reply.Error.Code != tc.want {
			t.Errorf("%s: code %s, want %s", tc.name, reply.Error.Code, tc.want)
		}
	}
	// A refusal must not have opened a turn either.
	if got := len(p.notifications()); got != 0 {
		t.Errorf("the refused sends put %d frames on the stream, want 0", got)
	}
}

// TestFakeAdapterQuotaUnknownVersusObserved is §7's "UNKNOWN and 0 are two different
// facts" as two answers from the same process, so a reader can compare them.
func TestFakeAdapterQuotaUnknownVersusObserved(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	var seen adapter.QuotaResult
	reply := call(t, p, "q1", "quota/read", struct{}{})
	if reply.Error != nil {
		t.Fatalf("quota/read: %s", reply.Error)
	}
	if err := json.Unmarshal(reply.Result, &seen); err != nil {
		t.Fatalf("quota/read reply: %v", err)
	}
	if err := seen.Validate(); err != nil {
		t.Errorf("our observed quota breaks §7: %v", err)
	}
	if len(seen.Observations) != 1 || seen.Observations[0].UsedPercent == nil {
		t.Fatalf("observations = %+v, want one readable row", seen.Observations)
	}

	call(t, p, "arm", "debug/unknown_quota", struct{}{})
	unread := call(t, p, "q2", "quota/read", struct{}{})
	if unread.Error != nil {
		t.Fatalf("quota/read after debug/unknown_quota: %s", unread.Error)
	}
	// The wire shape is the contract here, not the decoded struct: null would decode
	// to the same empty slice, and §7 asks for observations: [].
	if !strings.Contains(string(unread.Result), `"observations":[]`) {
		t.Errorf("unreadable quota = %s, want observations:[] rather than null", unread.Result)
	}
	seen = adapter.QuotaResult{}
	if err := json.Unmarshal(unread.Result, &seen); err != nil {
		t.Fatalf("quota/read reply: %v", err)
	}
	if seen.OverallConfidence != adapter.ConfidenceUnknown || len(seen.Observations) != 0 {
		t.Errorf("unreadable quota = %+v, want UNKNOWN with no observations", seen)
	}
	if err := seen.Validate(); err != nil {
		t.Errorf("the UNKNOWN reply is not a shape §7 allows: %v", err)
	}
}

// TestFakeAdapterFailInjection covers §10's debug/fail in the three shapes a contract
// test needs: the next call, the Nth call, and a code Core has never heard of. The
// last one is what makes Core's degradation path testable without editing the table.
func TestFakeAdapterFailInjection(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	arm := func(id, code string, nth int) *adapter.Frame {
		t.Helper()
		return call(t, p, id, "debug/fail", map[string]any{"code": code, "nth": nth})
	}
	if reply := arm("a1", "RATE_LIMITED", 0); reply.Error != nil {
		t.Fatalf("debug/fail: %s", reply.Error)
	}
	// §10's default: the injection fires on the call right after the arming one.
	fired := call(t, p, "h1", adapter.MethodHealth, struct{}{})
	if fired.Error == nil {
		t.Fatalf("debug/fail did not fire on the next call: %s", fired.Result)
	}
	if want := codeOf(t, "RATE_LIMITED"); fired.Error.Code != want {
		t.Errorf("injected code = %s, want %s", fired.Error.Code, want)
	}
	if data, ok := fired.Error.Data.(map[string]any); !ok || data["code"] != "RATE_LIMITED" {
		t.Errorf("injected data = %v, want §15.1's name in data.code", fired.Error.Data)
	}
	// And it is spent: §10 arms one failure, not a permanent outage.
	if spent := call(t, p, "h2", adapter.MethodHealth, struct{}{}); spent.Error != nil {
		t.Errorf("adapter/health after the injection was spent: %s", spent.Error)
	}

	if reply := arm("a2", "AUTH_REQUIRED", 3); reply.Error != nil {
		t.Fatalf("debug/fail nth=3: %s", reply.Error)
	}
	for i := 1; i <= 2; i++ {
		if r := call(t, p, fmt.Sprintf("ok%d", i), adapter.MethodHealth, struct{}{}); r.Error != nil {
			t.Errorf("call %d of 3 should still work, got %s", i, r.Error)
		}
	}
	if r := call(t, p, "third", adapter.MethodHealth, struct{}{}); r.Error == nil ||
		r.Error.Code != codeOf(t, "AUTH_REQUIRED") {
		t.Errorf("nth=3 injection = %+v, want AUTH_REQUIRED on the third call", r)
	}

	// A number outside §15.1 travels as a number and gets no name, which is how Core
	// learns to degrade rather than to guess.
	const outside = -32900
	if reply := arm("a3", fmt.Sprintf("%d", outside), 0); reply.Error != nil {
		t.Fatalf("debug/fail with a numeric code: %s", reply.Error)
	}
	odd := call(t, p, "h4", adapter.MethodHealth, struct{}{})
	if odd.Error == nil || int(odd.Error.Code) != outside {
		t.Fatalf("unknown-code injection = %+v, want it on the wire unchanged", odd)
	}
	if odd.Error.Known() {
		t.Errorf("%d is not in §15.1 but Known() said otherwise", outside)
	}
	if odd.Error.Data != nil {
		t.Errorf("the unknown code carried data %v; only §15.1's codes get a name", odd.Error.Data)
	}
	degraded := odd.Error.Degrade()
	raw, err := json.Marshal(degraded.Data)
	if err != nil {
		t.Fatalf("degraded data: %v", err)
	}
	keeps := fmt.Sprintf(`"original_code":%d`, outside)
	if degraded.Code != adapter.ErrInternal || !strings.Contains(string(raw), keeps) {
		t.Errorf("Degrade = %s with %s, want INTERNAL that keeps the original number visible", degraded.Code, raw)
	}

	if reply := arm("a4", "NOT_A_CODE", 0); reply.Error == nil ||
		reply.Error.Code != adapter.ErrInvalidParams {
		t.Errorf("debug/fail accepted a code that is neither a name nor a number: %+v", reply)
	}
}

// TestFakeAdapterHangWaitsForCancel is §10's debug/hang and §15.3 rule 5 together:
// nothing answers until a $/cancel names this request, and that acknowledgement is a
// result frame. A cancel for some other id must not release it either, or a timeout
// on one call would silently free a different one.
func TestFakeAdapterHangWaitsForCancel(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	p.write(t, request("h1", "debug/hang", struct{}{}))
	if f, quiet := p.silent(t, 300*time.Millisecond); !quiet {
		t.Fatalf("debug/hang answered early with %v", f)
	}

	other, err := adapter.NewCancel("req-x", "not-h1")
	if err != nil {
		t.Fatalf("NewCancel: %v", err)
	}
	p.write(t, other)
	if f, quiet := p.silent(t, 300*time.Millisecond); !quiet {
		t.Fatalf("a $/cancel for another request released the hang: %v", f)
	}

	mine, err := adapter.NewCancel("req-y", "h1")
	if err != nil {
		t.Fatalf("NewCancel: %v", err)
	}
	p.write(t, mine)
	reply := p.nextFrame(t)
	if reply.ID != "h1" {
		t.Fatalf("reply id %q, want the hanging request's", reply.ID)
	}
	// §15.3: CANCELLED has a number but must never appear in an error response.
	if reply.Error != nil {
		t.Fatalf("the cancel acknowledgement arrived as an error: %s", reply.Error)
	}
	var ack struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := json.Unmarshal(reply.Result, &ack); err != nil || !ack.Cancelled {
		t.Errorf("cancel acknowledgement = %s, want the cancelled flag set", reply.Result)
	}
	if !strings.Contains(p.stderr.String(), "hanging on h1") {
		t.Errorf("the hang was not reported on stderr: %q", p.stderr)
	}
	// The session has to keep serving afterwards.
	if r := call(t, p, "h2", adapter.MethodHealth, struct{}{}); r.Error != nil {
		t.Errorf("adapter/health after the cancel: %s, want the process still serving", r.Error)
	}
}

// TestFakeAdapterHoldReleaseIsWhereTheCloseGoes pins the other half of debug/hold: while
// it is armed nothing closes, and the mandatory frame appears **at the release** - not at
// the next send, not on a timer. Without this case the `on:false` branch has no witness
// and a mutation that drops it would sit silently.
func TestFakeAdapterHoldReleaseIsWhereTheCloseGoes(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)
	armHold(t, p, "h-on", true)
	call(t, p, "h1", methodSessionSend, adapter.SendParams{RemoteSessionID: "s-1",
		Text: "held", Delivery: adapter.DeliveryStartNow,
		ExecutionID: "exec-h1", IdempotencyKey: "k-h1"})
	if got := framesWithTicket(p.seen, "h1", adapter.MethodTurnFinished); len(got) != 0 {
		t.Fatalf("a held run closed on its own (%d finished frames): hold is not holding", len(got))
	}
	armHold(t, p, "h-off", false)
	got := framesWithTicket(p.seen, "h1", adapter.MethodTurnFinished)
	if len(got) != 1 {
		t.Fatalf("the release produced %d turn/finished, want exactly 1", len(got))
	}
	if got[0].ResultCode != resultSucceeded {
		t.Errorf("the release closed with %q, want SUCCEEDED", got[0].ResultCode)
	}
}

// TestFakeAdapterStartNowClosesItsRun is T-382 ③ Ⅰ: the real subprocess, one start_now
// send, and the assertion on the **frame** rather than on any reply - §6 规则 1 makes
// turn/finished the one mandatory closing signal and 第 6 条③ names this path as the gap
// that used to leave it unsent. A green here means Core can watch a run end.
func TestFakeAdapterStartNowClosesItsRun(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)
	reply := call(t, p, "n1", methodSessionSend, adapter.SendParams{
		RemoteSessionID: "s-1", Text: "go", Delivery: adapter.DeliveryStartNow,
		ExecutionID: "exec-n1", IdempotencyKey: "k-n1"})
	if reply.Error != nil {
		t.Fatalf("start_now: %s", reply.Error)
	}
	var closed *adapter.StreamFrame
	for _, s := range framesWithTicket(p.seen, "n1", adapter.MethodTurnFinished) {
		closed = s
	}
	if closed == nil {
		t.Fatal("no turn/finished on the wire for a start_now run: §6 第 6 条③ is unmet")
	}
	if closed.ResultCode != resultSucceeded {
		t.Errorf("closing frame carries %q, want SUCCEEDED: this run ended on its own", closed.ResultCode)
	}
}

// TestFakeAdapterGuideOnQuietSessionIsQ10 pins the 2026-09-21 ruling: steering a
// session that has no turn running is not a failure. accepted stays true, the reply
// carries a turn_id, and the reason field is what tells Core it became a new turn -
// without that sentence a queued interruption reads exactly like the guide it asked
// for, which is §5's other half of the mis-delivery accident.
func TestFakeAdapterGuideOnQuietSessionIsQ10(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	reply := call(t, p, "g1", "session/send", adapter.SendParams{
		RemoteSessionID: "s-1", Text: "steer me", Delivery: adapter.DeliveryGuide,
		ExecutionID: "exec-g", IdempotencyKey: "kg"})
	if reply.Error != nil {
		t.Fatalf("guide with nothing running: %s, want Q10 acceptance", reply.Error)
	}
	var res adapter.SendResult
	if err := json.Unmarshal(reply.Result, &res); err != nil {
		t.Fatalf("guide reply: %v", err)
	}
	if err := res.Validate(adapter.DeliveryGuide); err != nil {
		t.Errorf("the guide reply breaks CO-7: %v", err)
	}
	if !res.Accepted || res.TurnID == "" {
		t.Errorf("guide = %+v, want accepted with a turn_id", res)
	}
	if !strings.Contains(res.Reason, "guide") {
		t.Errorf("reason = %q, want it to say the guide opened a turn (Q10)", res.Reason)
	}
	// The pair is real evidence, not just a word in the reply. And the run it opened is
	// over: §6 第 6 条③ closes it, so the trace ends with the frame the gate needs.
	want := []string{"reply#init",
		"turn/claimed/1@g1", "turn/dispatched/2@g1", "stream/state/3@g1",
		"stream/delta/4@g1", "stream/item/5@g1", "turn/finished/6@g1", "reply#g1"}
	if got := strings.Join(p.trace(t), " "); got != strings.Join(want, " ") {
		t.Errorf("wire order = %s\n       want %s", got, strings.Join(want, " "))
	}
}
