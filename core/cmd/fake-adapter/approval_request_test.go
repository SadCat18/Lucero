package main

// T-441: §4's seventh Adapter->Core notification, `approval/request`, on the real wire from a
// real subprocess. Three things are pinned here and each has a reason:
//   * it is **sent by a path** (a prompt that asks for a risky tool), not by a debug endpoint -
//     §10's exemption covers injection tools existing, not the double's real behaviour being
//     reachable only through them;
//   * the `action` string is one the built-in §3.1 classifier knows, checked against
//     `permission.RiskLevel` rather than against a copy of its list, because an unrecognised
//     action fails closed into "needs approval" and the test would then pass for a reason
//     nobody asked for;
//   * the frame does not end anything, and the two C->A approval methods are still C->A.
//     Direction is what the §4 table is built on; flipping it is a separate ruling.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/permission"
)

// sendText sends one start_now prompt and returns its receipt.
func sendText(t *testing.T, p *fakeProcess, id, text string) *adapter.Frame {
	t.Helper()
	return call(t, p, id, methodSessionSend, adapter.SendParams{
		RemoteSessionID: "s-1", Text: text, Delivery: adapter.DeliveryStartNow,
		ExecutionID: "exec-" + id, IdempotencyKey: "k-" + id})
}

// TestApprovalRequestIsSentByARiskyPromptAndParksTheTurn is ②'s main leg.
func TestApprovalRequestIsSentByARiskyPromptAndParksTheTurn(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)
	reply := sendText(t, p, "ap1", "run tool write")
	if reply.Error != nil {
		t.Fatalf("session/send: %s", reply.Error)
	}

	// Position, and why it is not the one turn/finished got: the gate's frame is part of the
	// run's output - it names which tool call the receipt is answering - so it sits with the
	// CO-8 pair, in front of the reply. T-392 ① moved only the terminal frame behind the
	// receipt, because that is the one a reader can miss by answering first. `call` read every
	// frame in front of the reply into the log, so this loop sees them in arrival order and the
	// absence asserted below is a measured absence.
	// `trace` labels every frame in arrival order, responses included, so its index is an index
	// into `p.seen` and not into a notifications-only slice: matching the two by hand is how a
	// frame gets attributed to the wrong row.
	trace := p.trace(t)
	var item, request *adapter.Frame
	itemAt, requestAt, replyAt := -1, -1, -1
	for i, label := range trace {
		switch {
		case strings.HasPrefix(label, "stream/item/"):
			if itemAt < 0 {
				itemAt, item = i, p.seen[i]
			}
		case strings.HasPrefix(label, "approval/request/"):
			requestAt, request = i, p.seen[i]
		case label == "reply#ap1":
			replyAt = i
		}
	}
	if requestAt < 0 {
		t.Fatalf("no approval/request on the wire; trace: %v", trace)
	}
	if !(itemAt < requestAt && requestAt < replyAt) {
		t.Errorf("wire order is item@%d request@%d reply@%d, want the tool call, then the request, then the receipt: %v",
			itemAt, requestAt, replyAt, trace)
	}
	s, err := adapter.DecodeStream(request)
	if err != nil {
		t.Fatalf("Core could not decode our own approval/request: %v", err)
	}
	if s.TurnRef != "ap1" {
		t.Errorf("turn_ref = %q, want the ticket that started this turn (§4.2 裁定 ① covers this seventh member too)", s.TurnRef)
	}
	if s.Action != "filesystem.write" || s.Resource == "" {
		t.Errorf("payload = action %q resource %q, want filesystem.write with a resource", s.Action, s.Resource)
	}
	// The payload claim is about bytes on the pipe, not about a struct Core would fill in. The
	// method name is not in params (it is the envelope's own field), so it is asserted where it
	// is actually written - the same frame the test found, parsed by nothing the test controls.
	if request.Method != adapter.MethodApprovalRequest {
		t.Errorf("frame method = %q, want %q", request.Method, adapter.MethodApprovalRequest)
	}
	for _, want := range []string{`"action":"filesystem.write"`, `"turn_ref":"ap1"`} {
		if !strings.Contains(string(request.Params), want) {
			t.Errorf("raw params %s lack %s", request.Params, want)
		}
	}
	if item == nil {
		t.Error("the tool call was not reported before the request, so a reader cannot see which item the gate is about")
	}

	// The parked turn has not ended: no closing frame in the log, and nothing arriving after the
	// receipt either (§6 规则 1 leaves turn/finished as the only closer, so "the run is waiting"
	// must not be reported as "the run finished"). The bounded wait is what makes the second
	// half of that claim more than a look at a snapshot.
	if got := framesWithTicket(p.seen, "ap1", adapter.MethodTurnFinished); len(got) != 0 {
		t.Errorf("a run parked at the gate closed itself: %v", got)
	}
	if f, quiet := p.silent(t, 300*time.Millisecond); !quiet {
		t.Errorf("the parked turn put another frame on the wire after its receipt: %s", f.Method)
	}
	// The PENDING row the panel will see is the same fact, not a second story about it.
	list := call(t, p, "ap-list", methodApprovalList, struct{}{})
	var out struct {
		Approvals []approvalRow `json:"approvals"`
	}
	if err := json.Unmarshal(list.Result, &out); err != nil {
		t.Fatalf("approval/list reply: %v", err)
	}
	rows := out.Approvals
	var pending *approvalRow
	for i := range rows {
		if rows[i].ApprovalID == "apr-ap1" {
			pending = &rows[i]
		}
	}
	if pending == nil {
		t.Fatalf("approval/list shows no apr-ap1 among %d rows; a request with nothing to answer is §10's fake-that-lies", len(rows))
	}
	if pending.Action != s.Action || pending.State != "PENDING" {
		t.Errorf("row = %s/%s, want %s/PENDING: the frame and the list must be one vocabulary",
			pending.Action, pending.State, s.Action)
	}
	// The log claim is made after the child is reaped: reading os/exec's stderr copier while
	// the process is alive can lose the line (round 36's lesson, applied here rather than here).
	if !strings.Contains(p.stop(t), "waits at the gate") {
		t.Error("the park was not reported on the log stream")
	}
}

// TestApprovalRequestActionsAreClassifierVocabulary is the anti-accident check ② asks for:
// every action this double can put on that frame must be a string Core's built-in §3.1
// classifier registers, and registers as HIGH - because an unknown action is not "no gate",
// it is "fail closed for a reason nobody chose".
//
// permission.RiskLevel is called rather than a copied list: two passes over one rule set is
// the REQ-A8 disease permission.go's own comment warns about.
func TestApprovalRequestActionsAreClassifierVocabulary(t *testing.T) {
	for _, rt := range riskyTools {
		level, registered := permission.RiskLevel(rt.action)
		if !registered {
			t.Errorf("action %q for tool %q is not in permission's built-in classification: Core would judge it UNKNOWN and the gate would close for the wrong reason",
				rt.action, rt.tool)
			continue
		}
		if level != permission.RiskHigh {
			t.Errorf("action %q classifies as %s, not %s: a risky tool that is not high-risk should not go through the gate",
				rt.action, level, permission.RiskHigh)
		}
	}
	// The negative control, so the check above cannot be satisfied by an empty table: the old
	// fixture string is deliberately not registered.
	if _, registered := permission.RiskLevel("write_file"); registered {
		t.Error("write_file is registered after all: then this test's negative control proves nothing and the vocabulary needs re-reading")
	}
	if len(riskyTools) < 2 {
		t.Errorf("riskyTools has %d rows; one row would let a rename pass while naming nothing", len(riskyTools))
	}
}

// TestApprovalDirectionsDidNotMove is ③. The two C->A methods still answer a request with a
// reply; the new one travels the other way and is answered by nothing. A frame with no reply
// is not a request that timed out - it is a notification, and the difference is the table.
func TestApprovalDirectionsDidNotMove(t *testing.T) {
	p := startFakeAdapter(t)
	initialize(t, p)

	if got := call(t, p, "d-list", methodApprovalList, struct{}{}); got.ID != "d-list" || got.Error != nil {
		t.Errorf("approval/list came back as %+v, want a reply to the same id (C->A unchanged)", got)
	}
	// A request frame must not produce a response: if it did, someone made it C->A.
	sendText(t, p, "d1", "run tool exec")
	var request *adapter.Frame
	got := 0
	for _, f := range p.seen {
		if f.Kind() != adapter.KindNotification {
			continue
		}
		got++
		if f.Method == adapter.MethodApprovalRequest {
			request = f
		}
	}
	if request == nil {
		t.Fatalf("no approval/request among the %d notifications: %v", got, p.trace(t))
	}
	if request.ID != "" || request.RequestID == "" {
		t.Errorf("approval/request carried id %q / request_id %q: a notification names no call to answer",
			request.ID, request.RequestID)
	}
	// Nothing answers it, either: the reply to a notification would mean someone turned it
	// back into a C->A call. The parked turn produces no further frames either.
	if f, quiet := p.silent(t, 300*time.Millisecond); !quiet {
		t.Errorf("something followed the approval/request on stdout (%s): that is an answer to a frame that must not be answered",
			f.Method)
	}
}
