package process

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// pipeConnection wires a Connection to an in-test peer over two one-way pipes, so the
// pump can be examined without a subprocess. Exactly one reader owns each pipe:
// the connection reads inbound and writes outbound, the peer does the reverse.
func pipeConnection(t *testing.T) (*Connection, *pipePeer) {
	t.Helper()
	inR, inW := io.Pipe()   // peer writes inW, connection reads inR
	outR, outW := io.Pipe() // connection writes outW, peer reads outR
	p := &pipePeer{in: adapter.NewWriter(inW), out: adapter.NewReader(outR)}
	c := &Connection{
		child: &Child{Stdin: outW, Stdout: inR, Stderr: io.NopCloser(strings.NewReader("")),
			PID: os.Getpid(), done: make(chan error, 1)},
		w:           adapter.NewWriter(outW),
		r:           adapter.NewReader(inR),
		coreVersion: "0.1.0-test",
		pending:     map[string]chan *adapter.Frame{},
		turns:       map[string]*adapter.TurnTracker{},
		inbound:     make(chan Inbound, 64),
		dead:        make(chan struct{}),
	}
	go c.read() // Connect does this in production; the literal above skips it.
	t.Cleanup(func() {
		inW.Close()
		outR.Close()
		outW.Close()
		inR.Close()
	})
	return c, p
}

type pipePeer struct {
	in  *adapter.Writer
	out *adapter.Reader
}

// read returns the next frame the connection sent, or fails the test after 2s so a
// missing send is a failure rather than a hang.
func (p *pipePeer) read(t *testing.T) *adapter.Frame {
	t.Helper()
	type res struct {
		f   *adapter.Frame
		err error
	}
	ch := make(chan res, 1)
	go func() {
		f, err := p.out.Read()
		ch <- res{f, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("peer read: %v", r.err)
		}
		return r.f
	case <-time.After(2 * time.Second):
		t.Fatal("no frame arrived from the connection within 2s")
		return nil
	}
}

// replyTo answers a frame the peer already read.
func (p *pipePeer) replyTo(t *testing.T, req *adapter.Frame, result any) {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("peer marshal: %v", err)
	}
	if err := p.in.Write(&adapter.Frame{JSONRPC: adapter.JSONRPCVersion, ID: req.ID,
		RequestID: req.RequestID, Result: raw}); err != nil {
		t.Fatalf("peer write: %v", err)
	}
}

// answerOne reads one request and answers it, reporting what was sent.
func (p *pipePeer) answerOne(t *testing.T, result any) *adapter.Frame {
	t.Helper()
	req := p.read(t)
	p.replyTo(t, req, result)
	return req
}

// openTurn starts a turn the only way production does: CallTurn, whose request id
// becomes the ticket every frame of that turn must echo (§6 规则 5, extended to the
// evidence pair by §4.2 裁定 ①). It returns that ticket. Tests could write execByRef
// directly, but then they would be pinning the map instead of the rule, so they do
// not.
func openTurn(t *testing.T, c *Connection, p *pipePeer, execID string) string {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := c.CallTurn(context.Background(), execID, "session/send", adapter.SendParams{
			RemoteSessionID: "rsn-1", Text: "open", Delivery: adapter.DeliveryStartNow,
			ExecutionID: execID, IdempotencyKey: "open-" + execID})
		done <- err
	}()
	req := p.read(t)
	p.replyTo(t, req, adapter.SendResult{Accepted: true, TurnID: "turn-" + execID})
	if err := <-done; err != nil {
		t.Fatalf("CallTurn(%s): %v", execID, err)
	}
	return req.ID
}

func (p *pipePeer) send(s *adapter.StreamFrame) error {
	f, err := adapter.NewStream("note", s)
	if err != nil {
		return err
	}
	return p.in.Write(f)
}

func handshakeResult() adapter.InitializeResult {
	return adapter.InitializeResult{
		AdapterKey: "fake", AdapterVersion: "0.1.0",
		Protocol:         adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor},
		SoftwareVersions: map[string]string{"fake": "0.1.0"},
		AuthMode:         adapter.AuthNone, Health: adapter.HealthHealthy, WriteEntry: true,
		Capabilities: []adapter.Capability{{
			Name: "session.send", SupportLevel: adapter.LevelNative, EffectiveBackend: adapter.BackendNativeAPI,
			WriteCapable: true, DeliveryModes: []string{adapter.DeliveryStartNow},
			Confidence: adapter.ConfidenceHigh, LastVerifiedAt: 1789800000000,
		}},
	}
}

// established returns a connection whose §2 handshake has completed, with the peer
// having answered the initialize request.
func established(t *testing.T) (*Connection, *pipePeer) {
	t.Helper()
	c, p := pipeConnection(t)
	go p.answerOne(t, handshakeResult())
	hs, err := c.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, ok := hs.Effective("session.send"); !ok {
		t.Fatal("session.send is not effective after the handshake")
	}
	return c, p
}

func TestConnectionCallRoundTrip(t *testing.T) {
	c, p := established(t)
	sent := make(chan *adapter.Frame, 1)
	go func() { sent <- p.answerOne(t, map[string]any{"health": adapter.HealthHealthy}) }()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()
	reply, err := c.Call(ctx, adapter.MethodHealth, struct{}{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if reply.Error != nil {
		t.Fatalf("Call reply = %s, want a result", reply.Error)
	}

	req := expectFrame(t, sent)
	// §1: a request carries a UUIDv7 request_id and, when the context has one,
	// deadline_at; the JSON-RPC id and the request id are the same value so a log line
	// with either can be correlated.
	if req.ID == "" || req.ID != req.RequestID {
		t.Errorf("id/request_id = %q/%q, want one UUIDv7 in both", req.ID, req.RequestID)
	}
	if _, ok := adapter.RequestID(req.RequestID).Timestamp(); !ok {
		t.Errorf("request_id %q is not a readable UUIDv7 (§1)", req.RequestID)
	}
	if req.DeadlineAt == 0 {
		t.Error("deadline_at missing although the context carried a deadline (§1)")
	}
}

// waitFor polls cond until the deadline, so a test asserts on pump effects without
// sleeping blindly.
func waitFor(cond func() bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return errors.New("condition not met in time")
}

func expectFrame(t *testing.T, ch chan *adapter.Frame) *adapter.Frame {
	t.Helper()
	select {
	case f := <-ch:
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("the peer never saw a request")
		return nil
	}
}

// TestConnectionStreamBeforeInitializeIsRefused locks §2 on the inbound side: an
// Adapter that starts talking before the handshake is reported, and its frames must
// not pre-seed the dedup cursor.
func TestConnectionStreamBeforeInitializeIsRefused(t *testing.T) {
	c, p := pipeConnection(t)
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodStreamDelta, SequenceNo: 5,
		TurnRef: "r-early", Channel: adapter.ChannelAssistant, Text: "early"}); err != nil {
		t.Fatalf("peer send: %v", err)
	}
	select {
	case ev := <-c.Inbound():
		var e *adapter.Error
		if !errors.As(ev.Err, &e) || e.Code != adapter.ErrProtocolNotInitialized {
			t.Errorf("pre-handshake inbound = %+v, want a PROTOCOL_NOT_INITIALIZED error", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a pre-handshake frame was dropped silently")
	}
	if c.LastSeq() != 0 {
		t.Errorf("LastSeq = %d, want 0: a pre-handshake frame must not move the cursor", c.LastSeq())
	}
}

// TestConnectionDegradesUnknownReplyCode is T-45's requirement measured through the
// pump rather than against a helper: an Adapter answering with a number outside §15.1
// (a newer build, or debug/fail pointed at a made-up code) must come back as INTERNAL
// with the original number preserved, and must not take the connection down.
//
// One goroutine serves the whole peer side here: two readers on the same pipe would
// race over which request each of them answers.
func TestConnectionDegradesUnknownReplyCode(t *testing.T) {
	c, p := pipeConnection(t)
	answered := make(chan struct{}, 1)
	go func() {
		init := p.read(t)
		p.replyTo(t, init, handshakeResult())
		req := p.read(t)
		if err := p.in.Write(&adapter.Frame{JSONRPC: adapter.JSONRPCVersion, ID: req.ID,
			RequestID: req.RequestID,
			Error:     &adapter.Error{Code: adapter.ErrorCode(-32499), Message: "brand new failure"}}); err != nil {
			t.Errorf("peer write: %v", err)
		}
		close(answered)
	}()
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reply, err := c.Call(ctx, "session/list", struct{}{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if reply.Error == nil {
		t.Fatal("unknown code produced no error")
	}
	if reply.Error.Code != adapter.ErrInternal {
		t.Errorf("code = %s (%d), want the degradation to INTERNAL", reply.Error.Code, int(reply.Error.Code))
	}
	if got := reply.Error.Symbol(); got != "INTERNAL" {
		t.Errorf("Symbol() = %q, want INTERNAL", got)
	}
	data, _ := reply.Error.Data.(map[string]any)
	if data["original_code"] != -32499 {
		t.Errorf("data = %v, want original_code -32499 kept for the log and Audit line", reply.Error.Data)
	}
	<-answered
}

func TestConnectionRefusesBeforeInitialize(t *testing.T) {
	c, _ := pipeConnection(t)
	_, err := c.Call(context.Background(), "session/list", struct{}{})
	var e *adapter.Error
	if !errors.As(err, &e) || e.Code != adapter.ErrProtocolNotInitialized {
		t.Fatalf("Call before Initialize = %v, want PROTOCOL_NOT_INITIALIZED (§2)", err)
	}
}

func TestConnectionInitializeStartsThePump(t *testing.T) {
	c, p := pipeConnection(t)
	go p.answerOne(t, handshakeResult())
	hs, err := c.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, ok := hs.Effective("session.send"); !ok {
		t.Fatal("session.send is not effective after the handshake")
	}
	// A second Initialize must not re-negotiate (§2.2) and must not start a second pump.
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Errorf("second Initialize = %v, want the cached handshake returned", err)
	}
}

func TestConnectionDeadlineSendsCancel(t *testing.T) {
	c, p := established(t)
	seen := make(chan *adapter.Frame, 1)
	go func() {
		p.read(t)         // the request, which the peer never answers
		seen <- p.read(t) // what must follow: the $/cancel notification
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Call(ctx, adapter.MethodHealth, struct{}{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call = %v, want the deadline error", err)
	}
	if !strings.Contains(err.Error(), adapter.CancelMethod) {
		t.Errorf("error %q does not mention that %s was sent (§1)", err, adapter.CancelMethod)
	}

	cancelFrame := expectFrame(t, seen)
	if cancelFrame.Method != adapter.CancelMethod {
		t.Fatalf("second frame is %q, want $/cancel (§1)", cancelFrame.Method)
	}
	var params map[string]string
	if err := json.Unmarshal(cancelFrame.Params, &params); err != nil {
		t.Fatalf("$/cancel params: %v", err)
	}
	if params["id"] == "" {
		t.Errorf("$/cancel carries no target id: %s", cancelFrame.Params)
	}
}

// TestConnectionDeathMarksTurnUnknown is the CO-8 half of §8: the process going away
// is not evidence about the turn, so an unclosed turn becomes UNKNOWN and an in-flight
// call comes back instead of hanging.
func TestConnectionDeathMarksTurnUnknown(t *testing.T) {
	c, p := established(t)
	// Drive the turn through the pump itself, the same way production does, so the
	// test does not write tracker state from a second goroutine.
	ref := openTurn(t, c, p, "exec-1")
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodTurnClaimed, SequenceNo: 1,
		TurnRef: ref, Accepted: true, ExecutionID: "exec-1", ProviderExecutionID: "prov-1"}); err != nil {
		t.Fatal(err)
	}
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodTurnDispatch, SequenceNo: 2,
		TurnRef: ref, ExecutionID: "exec-1", TurnID: "turn-1", ProviderExecutionID: "prov-1"}); err != nil {
		t.Fatal(err)
	}
	tr := c.Turn("exec-1")
	// Wait on the thing being asserted. The cursor moves inside c.mu and applyTurn runs
	// after that lock is dropped, so waiting on LastSeq here left a window where frame 2
	// was counted but the turn was still CLAIMED - a full-suite run on a loaded machine
	// took it once (during the T-314 mutation batch, unrelated to the mutation being tested).
	if err := waitFor(func() bool { return tr.State() == adapter.TurnRunning }, 2*time.Second); err != nil {
		t.Fatalf("the pump did not move the turn to RUNNING: %v", err)
	}
	if got := c.LastSeq(); got != 2 {
		t.Fatalf("connection watermark = %d, want 2 once the evidence pair landed", got)
	}
	if tr.State() != adapter.TurnRunning {
		t.Fatalf("turn state before the crash = %q, want RUNNING", tr.State())
	}

	sawRequest := make(chan *adapter.Frame, 1)
	go func() { sawRequest <- p.read(t) }()
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), adapter.MethodHealth, struct{}{})
		errCh <- err
	}()
	expectFrame(t, sawRequest)

	c.kill(errors.New("child exited"))
	select {
	case err := <-errCh:
		// The call was registered *before* the death. It must look exactly like the
		// one below, which is issued *after* the death: two shapes for one condition
		// made the outcome depend on a race, and this test caught it.
		expectLost(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the call hung after the process died; §8 requires it to come back")
	}
	if got := tr.State(); got != adapter.TurnUnknown {
		t.Errorf("turn state after death = %q, want UNKNOWN (CO-8, not FAILED)", got)
	}
	if got := tr.ProviderExecutionID(); got != "prov-1" {
		t.Errorf("provider handle = %q, want it kept for Recovery to query by (CO-8)", got)
	}
	// A second kill must be a no-op rather than a panic on a closed channel.
	c.kill(errors.New("again"))

	// And a call issued after the death takes the same path.
	if _, err := c.Call(context.Background(), adapter.MethodHealth, struct{}{}); err == nil {
		t.Error("Call on a dead connection returned no error")
	} else {
		expectLost(t, err)
	}
}

// expectLost pins the single failure shape of a dead connection.
func expectLost(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrConnectionLost) {
		t.Fatalf("err = %v, want errors.Is(ErrConnectionLost)", err)
	}
	var le *LostError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want a *LostError carrying the method", err)
	}
	if le.Method != adapter.MethodHealth {
		t.Errorf("LostError.Method = %q, want %q", le.Method, adapter.MethodHealth)
	}
	if !strings.Contains(err.Error(), adapter.ErrAdapterUnresponsive.String()) {
		t.Errorf("err = %q, want it to name %s", err, adapter.ErrAdapterUnresponsive)
	}
}

func TestConnectionDedupsAndTracksTurn(t *testing.T) {
	c, p := pipeConnection(t)
	go p.answerOne(t, handshakeResult())
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// §6 规则 5's whole point in one stream: two executions on one connection where the
	// second one's numbers are lower than the first one's watermark. A pump that
	// deduplicated on sequence_no alone would drop e2's frames as "stale", and that is
	// exactly how two executions end up written into one chunk row.
	// Both tickets come from a real turn-opening call: since §4.2 裁定 ① the pair is
	// attributed by ticket too, so a claimed frame naming a ticket Core never issued is
	// a stranger and gets dropped, which would defeat the point of this stream.
	r1 := openTurn(t, c, p, "e1")
	r2 := openTurn(t, c, p, "e2")
	stream := []*adapter.StreamFrame{
		{Method: adapter.MethodTurnClaimed, SequenceNo: 5, TurnRef: r1, Accepted: true, ExecutionID: "e1", ProviderExecutionID: "p1"},
		{Method: adapter.MethodTurnClaimed, SequenceNo: 5, TurnRef: r1, Accepted: true, ExecutionID: "e1", ProviderExecutionID: "p1"}, // replay of (r1,5)
		{Method: adapter.MethodTurnDispatch, SequenceNo: 6, TurnRef: r1, ExecutionID: "e1", TurnID: "t1", ProviderExecutionID: "p1"},
		{Method: adapter.MethodTurnClaimed, SequenceNo: 1, TurnRef: r2, Accepted: true, ExecutionID: "e2", ProviderExecutionID: "p2"},
		{Method: adapter.MethodStreamDelta, SequenceNo: 2, TurnRef: r2, Channel: adapter.ChannelAssistant, Text: "e2 speaks"},
		{Method: adapter.MethodStreamDelta, SequenceNo: 2, TurnRef: r2, Channel: adapter.ChannelAssistant, Text: "again"}, // replay of (r2,2)
		{Method: adapter.MethodTurnFinished, SequenceNo: 7, TurnRef: r1, ResultCode: "OK"},
	}
	go func() {
		for _, s := range stream {
			if err := p.send(s); err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	var got []string
	deadline := time.After(3 * time.Second)
	for len(got) < 5 { // nine frames in, five distinct ones out
		select {
		case ev := <-c.Inbound():
			if ev.Err != nil {
				t.Fatalf("unexpected evidence error: %v", ev.Err)
			}
			got = append(got, ev.Frame.Method)
		case <-deadline:
			t.Fatalf("only forwarded %v of the five distinct frames", got)
		}
	}
	if want := []string{adapter.MethodTurnClaimed, adapter.MethodTurnDispatch,
		adapter.MethodTurnClaimed, adapter.MethodStreamDelta, adapter.MethodTurnFinished}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("forwarded %v, want %v (§6 规则 5 drops the two replays and keeps the second turn)", got, want)
	}
	if c.LastSeq() != 7 {
		t.Errorf("LastSeq = %d, want the connection watermark at 7", c.LastSeq())
	}
	if c.MissingSeq() != 0 {
		t.Errorf("MissingSeq = %d, want 0: another turn's lower numbers are not a hole in this one", c.MissingSeq())
	}
	if got, want := c.SinceSeq(r1), int64(7); got != want {
		t.Errorf("SinceSeq(r1) = %d, want %d: each turn keeps its own resume number", got, want)
	}
	if got, want := c.SinceSeq(r2), int64(2); got != want {
		t.Errorf("SinceSeq(r2) = %d, want %d", got, want)
	}
	tr := c.Turn("e1")
	if tr.State() != adapter.TurnClosed || tr.Result() != "OK" {
		t.Errorf("turn after the stream = %s/%q, want CLOSED/OK", tr.State(), tr.Result())
	}
	// e2 was claimed but never dispatched, so §7.1 forbids reading it as running.
	if tr2 := c.Turn("e2"); tr2.State() != adapter.TurnClaimed {
		t.Errorf("e2 = %q, want CLAIMED", tr2.State())
	}
	drain(c)
}

// TestConnectionDropsUnboundTurnRef is the other half of §6 规则 5: a frame whose ticket
// this connection never bound is dropped and reported, and specifically must not be
// handed to "the one turn currently in flight". Forwarding it with the frame attached
// would let a caller write another execution's output into its chunk row.
func TestConnectionDropsUnboundTurnRef(t *testing.T) {
	c, p := pipeConnection(t)
	go p.answerOne(t, handshakeResult())
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	// One turn is genuinely in flight, which is what a guessing pump would pick.
	live := openTurn(t, c, p, "e-live")
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodTurnClaimed, SequenceNo: 1,
		TurnRef: live, Accepted: true, ExecutionID: "e-live", ProviderExecutionID: "p1"}); err != nil {
		t.Fatalf("peer send: %v", err)
	}
	// The stranger names a real execution_id. Since §4.2 裁定 ① that is no route in:
	// the ticket decides attribution and the id is only cross-checked, so this frame
	// must still be dropped rather than laundered into e-live's turn.
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodTurnClaimed, SequenceNo: 2,
		TurnRef: "never-bound", Accepted: true, ExecutionID: "e-live", ProviderExecutionID: "px"}); err != nil {
		t.Fatalf("peer send: %v", err)
	}
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodStreamDelta, SequenceNo: 3,
		TurnRef: "never-bound", Channel: adapter.ChannelAssistant, Text: "orphan"}); err != nil {
		t.Fatalf("peer send: %v", err)
	}
	// The live turn's own frame goes through first, so the next two events are drops.
	if ev := <-c.Inbound(); ev.Frame == nil || ev.Frame.Method != adapter.MethodTurnClaimed {
		t.Fatalf("first inbound = %+#v, want the live turn forwarded", ev)
	}
	for i := 0; i < 2; i++ {
		select {
		case ev := <-c.Inbound():
			if ev.Frame != nil {
				t.Fatalf("unattributable frame %d was forwarded as %+#v, want it dropped", i+1, ev.Frame)
			}
			if ev.Err == nil || !strings.Contains(ev.Err.Error(), "never-bound") {
				t.Fatalf("drop %d was not reported properly: %v", i+1, ev.Err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("the pump neither forwarded the frame nor reported drop %d", i+1)
		}
	}
	// The live turn is untouched by the stranger's frames: not its state, and not the
	// handle Recovery would query it by.
	if tr := c.Turn("e-live"); tr.State() != adapter.TurnClaimed || tr.ProviderExecutionID() != "p1" {
		t.Errorf("e-live = %q/%q, want CLAIMED/p1: a dropped frame must not have moved it",
			tr.State(), tr.ProviderExecutionID())
	}
	if got := c.SinceSeq("never-bound"); got != 0 {
		t.Errorf("SinceSeq for the unbound ticket = %d: a dropped frame must not have advanced a watermark", got)
	}
}

// TestDroppedFrameReachesTheAuditSeam is T84-7's half of §4.2 裁定 ① inside this package:
// a frame the pump throws away must still reach the injector, because "dropped and
// reported to the consumer" leaves no trace once the consumer is gone. It asserts the
// seam only - what row the injector writes is audit's own outlet, and this package must
// never name the audit_entries table (W13-2).
func TestDroppedFrameReachesTheAuditSeam(t *testing.T) {
	c, p := pipeConnection(t)
	rec := new(stubRecorder)
	// Connect normally wires sup; the pipe harness builds a Connection without one, so
	// the test attaches the Supervisor the drop path reads its recorder from.
	c.sup = &Supervisor{Kind: KindAdapter, Rec: rec,
		Now: func() time.Time { return time.UnixMilli(1700000000000) }}
	go p.answerOne(t, handshakeResult())
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodStreamDelta, SequenceNo: 1,
		TurnRef: "never-issued", Channel: adapter.ChannelAssistant, ExecutionID: "e-stranger",
		Text: "orphan"}); err != nil {
		t.Fatalf("peer send: %v", err)
	}
	select {
	case ev := <-c.Inbound():
		if ev.Frame != nil {
			t.Fatalf("the unattributable frame was forwarded as %+#v", ev.Frame)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the pump neither forwarded the frame nor reported the drop")
	}
	if len(rec.drops) != 1 {
		t.Fatalf("RecordDrop fired %d times, want exactly one per discarded frame", len(rec.drops))
	}
	got := rec.drops[0]
	if got.Method != adapter.MethodStreamDelta || got.TurnRef != "never-issued" ||
		got.FrameExecutionID != "e-stranger" || got.BoundExecutionID != "" {
		t.Errorf("drop = %+v, want the frame's own method, ticket, and the execution_id it claimed (bound stays empty: nothing was ever bound)", got)
	}
	if got.Kind != KindAdapter || got.Reason == "" || got.Timestamp != 1700000000000 {
		t.Errorf("drop = %+v, want the supervised kind, a reason, and the clock's ms", got)
	}
	// A drop is not a process lifecycle row: the three lifecycle methods must be silent.
	if len(rec.started) != 0 || len(rec.exits) != 0 || len(rec.crashes) != 0 {
		t.Errorf("lifecycle rows fired for a dropped frame: %+v", rec)
	}
}

// TestConnectionDropsContradictoryIdentity is §4.2 裁定 ①'s cross-check: once the pair
// carries a ticket too, a frame that names an execution the ticket was never bound to
// has two truths in it, and neither may win by default. Forwarding it is how one
// execution's evidence ends up closing another's turn.
func TestConnectionDropsContradictoryIdentity(t *testing.T) {
	c, p := pipeConnection(t)
	go p.answerOne(t, handshakeResult())
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	live := openTurn(t, c, p, "e-live")
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodTurnClaimed, SequenceNo: 1,
		TurnRef: live, Accepted: true, ExecutionID: "e-live", ProviderExecutionID: "p1"}); err != nil {
		t.Fatalf("peer send: %v", err)
	}
	if ev := <-c.Inbound(); ev.Err != nil || ev.Frame == nil {
		t.Fatalf("the consistent claim came back as %+#v", ev)
	}
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodTurnDispatch, SequenceNo: 2,
		TurnRef: live, ExecutionID: "e-other", TurnID: "t-x", ProviderExecutionID: "p1"}); err != nil {
		t.Fatalf("peer send: %v", err)
	}
	select {
	case ev := <-c.Inbound():
		if ev.Frame != nil {
			t.Fatalf("the contradictory dispatch was forwarded as %+#v", ev.Frame)
		}
		if ev.Err == nil || !strings.Contains(ev.Err.Error(), "e-other") {
			t.Fatalf("contradiction not reported: %v", ev.Err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the pump neither forwarded the frame nor reported the drop")
	}
	// The turn stays CLAIMED: a dispatch that crossed identities must not have started
	// it, and the watermark must not have moved either.
	if tr := c.Turn("e-live"); tr.State() != adapter.TurnClaimed {
		t.Errorf("e-live = %q, want CLAIMED", tr.State())
	}
	if got := c.SinceSeq(live); got != 1 {
		t.Errorf("SinceSeq(live) = %d, want 1: the dropped dispatch must not have advanced the cursor", got)
	}
}

// TestInboundNeverCarriesTheTicket is C20: turn_ref is internal/process's own
// bookkeeping, so a consumer gets the attribution already resolved and never the
// ticket it was resolved from. Were it otherwise, chunk flush would end up routing by
// a number that means nothing outside this connection.
func TestInboundNeverCarriesTheTicket(t *testing.T) {
	c, p := pipeConnection(t)
	go p.answerOne(t, handshakeResult())
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	ticket := openTurn(t, c, p, "e1")
	for _, s := range []*adapter.StreamFrame{
		{Method: adapter.MethodTurnClaimed, SequenceNo: 1, TurnRef: ticket,
			Accepted: true, ExecutionID: "e1", ProviderExecutionID: "p1"},
		{Method: adapter.MethodStreamDelta, SequenceNo: 2, TurnRef: ticket,
			Channel: adapter.ChannelAssistant, Text: "hello"},
	} {
		if err := p.send(s); err != nil {
			t.Fatalf("peer send %s: %v", s.Method, err)
		}
	}
	// The pair names its execution, so it is forwarded with the ticket stripped too:
	// one rule for every consumer, not one for the frames that happen to carry an id.
	for _, want := range []struct {
		method string
		seq    int64
	}{{adapter.MethodTurnClaimed, 1}, {adapter.MethodStreamDelta, 2}} {
		ev := <-c.Inbound()
		if ev.Err != nil {
			t.Fatalf("%s reported %v", want.method, ev.Err)
		}
		if ev.Frame == nil {
			t.Fatalf("%s came through as a drop", want.method)
		}
		if ev.ExecutionID != "e1" {
			t.Errorf("%s: ExecutionID = %q, want the ticket resolved to e1", want.method, ev.ExecutionID)
		}
		if ev.Frame.TurnRef != "" {
			t.Errorf("%s: Frame.TurnRef = %q, want it stopped at this boundary (C20)", want.method, ev.Frame.TurnRef)
		}
		if ev.Frame.SequenceNo != want.seq {
			t.Errorf("%s: SequenceNo = %d, want %d", want.method, ev.Frame.SequenceNo, want.seq)
		}
	}
	if got := c.SinceSeq(ticket); got != 2 {
		t.Errorf("SinceSeq(ticket) = %d, want 2: the pump still tracks the ticket internally", got)
	}
}

// TestConnectionChainClosesBeforeItOpens is §6.3's chain seen from the only place that
// is allowed to know which Execution a frame belongs to: the pump's consumer view. The
// guide lands on the running turn, session/stop closes it, and exec-2's claim must
// arrive after exec-1's close - all asserted through resolved execution_ids, with no
// hand-written sequence numbers and no ticket ever reaching the consumer (§6 规则 5 决议 ②).
// holdOpen arms §10's debug/hold on the real double (T-386 ① licenses the endpoint).
// Since §6 第 6 条③ an ordinary run closes itself on the wire, so anything asking about
// "while a turn is in flight" - §5's guide, §4's stop, §6 规则 5's interleaving - has to
// ask for the open turn rather than rely on the double never finishing one.
func holdOpen(t *testing.T, ctx context.Context, conn *Connection) {
	t.Helper()
	if _, err := conn.Call(ctx, "debug/hold", map[string]any{"on": true}); err != nil {
		t.Fatalf("debug/hold: %v", err)
	}
}

func TestConnectionChainClosesBeforeItOpens(t *testing.T) {
	exe := buildFakeAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever,
		Launch: func() (*exec.Cmd, error) { return exec.CommandContext(ctx, exe), nil }}
	conn, err := sup.Connect(ctx, "0.1.0-test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	holdOpen(t, ctx, conn) // this test is about a turn that is still running (§6 第 6 条③ changed the default)

	send := func(id, execID, delivery string) adapter.SendResult {
		t.Helper()
		reply, err := conn.CallTurn(ctx, execID, "session/send", adapter.SendParams{
			RemoteSessionID: "rsn-1", Text: id, Delivery: delivery,
			ExecutionID: execID, IdempotencyKey: "k-" + id})
		if err != nil {
			t.Fatalf("session/send %s: %v", id, err)
		}
		var res adapter.SendResult
		if err := json.Unmarshal(reply.Result, &res); err != nil {
			t.Fatalf("send %s reply: %v", id, err)
		}
		return res
	}
	drain := func(want int) []Inbound {
		var got []Inbound
		for len(got) < want {
			select {
			case ev := <-conn.Inbound():
				if ev.Err != nil {
					t.Fatalf("the pump reported a violation: %v", ev.Err)
				}
				got = append(got, ev)
			case <-time.After(10 * time.Second):
				t.Fatalf("only %d of %d notifications arrived", len(got), want)
			}
		}
		return got
	}

	first := send("s1", "exec-1", adapter.DeliveryStartNow)
	drain(5) // claimed, dispatched, state, delta, item
	steer := send("s2", "exec-1", adapter.DeliveryGuide)
	if steer.TurnID != first.TurnID {
		t.Errorf("guide opened %q over the running %q (§5)", steer.TurnID, first.TurnID)
	}
	drain(1)
	if tr := conn.Turn("exec-1"); tr.State() != adapter.TurnRunning {
		t.Fatalf("exec-1 = %s before the stop, want RUNNING", tr.State())
	}

	if _, err := conn.Call(ctx, "session/stop",
		map[string]string{"remote_session_id": "rsn-1"}); err != nil {
		t.Fatalf("session/stop: %v", err)
	}
	// Two frames close the turn: the state and the mandatory signal.
	closing := drain(2)
	for _, ev := range closing {
		if ev.ExecutionID != "exec-1" {
			t.Errorf("closing frame for %q, want exec-1", ev.ExecutionID)
		}
	}
	if got := closing[len(closing)-1].Frame; got == nil || got.Method != adapter.MethodTurnFinished {
		t.Errorf("last closing frame = %+#v, want turn/finished", got)
	}
	tr1 := conn.Turn("exec-1")
	if tr1.State() != adapter.TurnClosed || tr1.Result() != "CANCELLED" {
		t.Errorf("exec-1 = %s/%q, want CLOSED/CANCELLED from session/stop", tr1.State(), tr1.Result())
	}

	send("s3", "exec-2", adapter.DeliveryStartNow)
	// exec-1 was already closed when this call went out, so the chain holds by state
	// rather than by counting frames: nothing arriving now may reopen it.
	if tr1.State() != adapter.TurnClosed {
		t.Errorf("exec-1 = %s while exec-2 was starting, want it still CLOSED (§6.3)", tr1.State())
	}
	for _, ev := range drain(5) {
		if ev.ExecutionID != "exec-2" {
			t.Errorf("frame after the close resolved to %q, want exec-2", ev.ExecutionID)
		}
		if ev.Frame != nil && ev.Frame.TurnRef != "" {
			t.Errorf("consumer saw the ticket %q past the boundary (§6 规则 5 决议 ②)", ev.Frame.TurnRef)
		}
	}
	if tr2 := conn.Turn("exec-2"); tr2.State() != adapter.TurnRunning {
		t.Errorf("exec-2 = %q after its pair, want RUNNING", tr2.State())
	}
	if tr1.State() != adapter.TurnClosed {
		t.Errorf("exec-1 reopened as %s: one close must be final (§6.3)", tr1.State())
	}
}

// drain makes sure no extra frame slipped through the dedup.
func drain(c *Connection) {
	for {
		select {
		case ev := <-c.Inbound():
			ev.Frame = nil
			continue
		default:
			return
		}
	}
}

// TestConnectionAgainstRealFakeAdapter is why §10 put the FakeAdapter in its own
// process: everything above shares one binary with the peer, this crosses a real exec
// boundary with the shipped cmd/fake-adapter.
func TestConnectionAgainstRealFakeAdapter(t *testing.T) {
	exe := buildFakeAdapter(t)
	var mu sync.Mutex
	var events []Event
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever,
		Launch: func() (*exec.Cmd, error) { return exec.CommandContext(ctx, exe), nil },
		Sink:   func(e Event) { mu.Lock(); events = append(events, e); mu.Unlock() }}

	conn, err := sup.Connect(ctx, "0.1.0-test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()

	hs, err := conn.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	send, ok := hs.Effective("session.send")
	if !ok || len(send.DeliveryModes) != 3 {
		t.Errorf("session.send effective = %+v (found=%v), want all three §5 states", send, ok)
	}
	if _, ok := hs.Effective("debug.crash"); !ok {
		t.Error("debug.crash should be effective for adapter_key=fake (§9)")
	}
	if pid := conn.Child().PID; pid <= 0 {
		t.Errorf("child pid = %d, want a real process", pid)
	}

	reply, err := conn.Call(ctx, adapter.MethodHealth, struct{}{})
	if err != nil {
		t.Fatalf("Call(health): %v", err)
	}
	var h adapter.HealthResult
	if err := json.Unmarshal(reply.Result, &h); err != nil {
		t.Fatalf("health reply: %v", err)
	}
	if err := h.Validate(); err != nil {
		t.Errorf("the fake's health reply breaks §4.1: %v", err)
	}

	// An unimplemented method must come back as an explicit refusal over a real pipe,
	// not as a timeout.
	// session/push is withdrawn for v0.1 by CO-40 (contract ② §4.0): as an A->C notice it
	// would need a turn_ref Core never issued, which §4.2 裁定 ① makes impossible. This
	// probes that the pump passes such a refusal through unchanged, not that push is due;
	// session/list is
	// served now, so it can no longer stand in for "not implemented".
	unbuilt, err := conn.Call(ctx, "session/push", struct{}{})
	if err != nil {
		t.Fatalf("Call(model/select): %v", err)
	}
	if unbuilt.Error == nil || unbuilt.Error.Code != adapter.ErrInternal {
		t.Errorf("session/push = %+v, want INTERNAL_ERROR until FakeAdapter implements it", unbuilt)
	}
	if conn.LastSeq() != 0 {
		t.Errorf("cursor moved before any stream frame: %d", conn.LastSeq())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 || events[0].Kind != EventStarted {
		t.Errorf("supervisor events = %v, want a STARTED first", events)
	}
}

// buildFakeAdapter compiles cmd/fake-adapter into t.TempDir(), which the testing
// package creates and removes as its own specific directory: no shared temp root,
// no hand-rolled cleanup, and nothing left behind if the test fails (T-47).
func buildFakeAdapter(t *testing.T) string {
	t.Helper()
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	name := "fake-adapter"
	if runtime.GOOS == "windows" {
		goTool += ".exe"
		name += ".exe"
	}
	exe := filepath.Join(t.TempDir(), name)
	buildCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, goTool, "build", "-o", exe, "../../cmd/fake-adapter")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build fake-adapter here: %v / %s", err, out)
	}
	return exe
}

// TestConnectionSendOverRealPipe is §14's acceptance shape: Core's own pump driving
// the shipped FakeAdapter across an exec boundary, with CO-8's evidence pair arriving
// in the order that makes it readable. The in-process tests above prove the pump's
// rules against frames this package wrote; only this proves the two sides mean the
// same thing by turn/claimed on the wire.
func TestConnectionSendOverRealPipe(t *testing.T) {
	exe := buildFakeAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever,
		Launch: func() (*exec.Cmd, error) { return exec.CommandContext(ctx, exe), nil }}
	conn, err := sup.Connect(ctx, "0.1.0-test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	holdOpen(t, ctx, conn) // this test is about a turn that is still running (§6 第 6 条③ changed the default)

	send := func(id, execID string) adapter.SendResult {
		t.Helper()
		// CallTurn is what binds this requests id as the turn_ref §6 规则 5 attributes the
		// following frames by - the point of the second half of the acceptance item.
		reply, err := conn.CallTurn(ctx, execID, "session/send", adapter.SendParams{
			RemoteSessionID: "s-1", Text: "hello " + id, Delivery: adapter.DeliveryStartNow,
			ExecutionID: execID, IdempotencyKey: "k-" + id})
		if err != nil {
			t.Fatalf("session/send %s: %v", id, err)
		}
		var res adapter.SendResult
		if err := json.Unmarshal(reply.Result, &res); err != nil {
			t.Fatalf("session/send %s reply: %v", id, err)
		}
		if err := res.Validate(adapter.DeliveryStartNow); err != nil {
			t.Errorf("the fake's %s reply breaks CO-7: %v", id, err)
		}
		return res
	}

	first := send("1", "exec-1")
	// The reply is already in hand, so everything that preceded it has been applied:
	// the reader goroutine is what resolves the call, and it feeds the tracker first.
	// That ordering is what lets Call return a result Core can act on at all.
	if got := nextNotice(t, conn); got.Method != adapter.MethodTurnClaimed || got.ExecutionID != "exec-1" {
		t.Fatalf("first notification = %s/%s, want turn/claimed for exec-1", got.Method, got.ExecutionID)
	}
	if got := nextNotice(t, conn); got.Method != adapter.MethodTurnDispatch {
		t.Fatalf("second notification = %s, want turn/dispatched", got.Method)
	}
	for _, want := range []string{adapter.MethodStreamState, adapter.MethodStreamDelta, adapter.MethodStreamItem} {
		if got := nextNotice(t, conn); got.Method != want {
			t.Fatalf("notification = %s, want %s (a turn emits the normal path §10 ① asks for)", got.Method, want)
		}
	}
	tr1 := conn.Turn("exec-1")
	if tr1.State() != adapter.TurnRunning {
		t.Errorf("turn state after the pair = %q, want RUNNING (§7.1 may now show it)", tr1.State())
	}
	if tr1.TurnID() != first.TurnID || tr1.ProviderExecutionID() != first.ProviderExecutionID {
		t.Errorf("evidence = %q/%q, reply = %q/%q: the pair and the answer name different runs",
			tr1.TurnID(), tr1.ProviderExecutionID(), first.TurnID, first.ProviderExecutionID)
	}

	// A second start_now interrupts the turn in flight (§5), so exec-1 is still talking
	// after exec-2 opened: two executions, one connection, each frame landing on the
	// tracker its ticket names. That is §6 规则 5's acceptance item, asserted where it
	// actually bites.
	send("2", "exec-2")
	// §6 第 6 条② (09-27): an interrupted run puts **no** turn-level code on the wire -
	// "被打断" is Core's observation to make, and this is where the fake used to steal it
	// with `INTERRUPTED`. So the second send releases six notifications before its reply:
	// its own five, plus the preempted turn's status tail. Six is the assertion: the frame
	// that is *not* there is the thing being pinned.
	codes := []string{}
	for i := 0; i < 6; i++ {
		s := nextNotice(t, conn)
		if s.Method == adapter.MethodTurnFinished {
			// C20: the consumer-facing view carries the resolved Execution and never the
			// ticket; the ticket is pinned on the wire and at the boundary instead.
			if s.TurnRef != "" {
				t.Errorf("turn/finished leaked the ticket %q past the pump boundary", s.TurnRef)
			}
			codes = append(codes, s.ResultCode)
		}
	}
	if len(codes) > 0 {
		t.Errorf("an interrupted run produced turn/finished %v; §6 第 6 条② allows none", codes)
	}
	if tr1.State() != adapter.TurnRunning || tr1.Result() != "" {
		t.Errorf("exec-1 = %s/%q, want RUNNING/\"\": with no closing frame the pump must not"+
			" invent an end for it (§7、CO-8: 没有收尾信号不许读成失败)", tr1.State(), tr1.Result())
	}
	if tr2 := conn.Turn("exec-2"); tr2.State() != adapter.TurnRunning {
		t.Errorf("exec-2 = %q, want RUNNING: the interrupted turn must not drag the new one down", tr2.State())
	}
	if seq, miss := conn.LastSeq(), conn.MissingSeq(); seq <= 0 || miss != 0 {
		t.Errorf("cursor = last %d, missing %d: the real stream arrived contiguous, so no hole may be recorded",
			seq, miss)
	}
}

// nextNotice waits for one notification that survived §6 规则 2's dedup. It waits rather
// than polling because a pump that stops forwarding is the failure under test, and a
// test that sleeps through it reports nothing.
func nextNotice(t *testing.T, conn *Connection) *adapter.StreamFrame {
	t.Helper()
	select {
	case ev := <-conn.Inbound():
		if ev.Err != nil {
			t.Fatalf("the pump reported an evidence violation from the real fake: %v", ev.Err)
		}
		if ev.Frame == nil {
			t.Fatal("the pump forwarded an empty inbound slot")
		}
		return ev.Frame
	case <-time.After(10 * time.Second):
		t.Fatal("no stream notification within 10s")
	}
	return nil
}

// T-314 ⑤: closing a connection must reap the child, not just hand back the pipes. The
// measured shape it covers is a child that ignores stdin - polite EOF never arrives, so
// before this change the process outlived the "closed" connection (and its §14 row).
func TestCloseReapsTheChild(t *testing.T) {
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: helperLauncher("sleep")}
	conn, err := sup.Connect(context.Background(), "0.1.0-test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if conn.child.Reaped() {
		t.Fatal("the child is born reaped - the guard below would prove nothing")
	}
	start := time.Now()
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !conn.child.Reaped() {
		t.Fatalf("Close returned while pid %d is still unreaped - that is the orphan T-314 ⑤ is about", conn.child.PID)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("Close took %v; the reap is bounded by closeReapTimeout (2s) plus a kill, not by the child", el)
	}
}

// Whoever reaps first owns the child: cmd/core's pipeline already calls Stop(child, 2s) for
// this exact process, so a later Close must not aim a second Kill at it. Read this case as
// documentation rather than as teeth - mutation m2 (dropping the Reaped() check) stays green,
// because the error a second Kill would produce is swallowed by ChildAlreadyGone. The check
// earns its place against pid reuse, which no test here can manufacture on demand.
func TestCloseAfterAnExistingReapStaysQuiet(t *testing.T) {
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: helperLauncher("sleep")}
	conn, err := sup.Connect(context.Background(), "0.1.0-test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := sup.Stop(conn.child, 2*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !conn.child.Reaped() {
		t.Fatal("Stop returned without reaping")
	}
	if err := conn.Close(); err != nil {
		t.Errorf("Close after an existing reap = %v, want nil (no second kill)", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("second Close = %v, want nil (still idempotent)", err)
	}
}

// The pump harness builds a Connection with no Supervisor and a Child whose PID is
// os.Getpid(), so "Close is safe on that shape" is a claim worth pinning even though the
// protection comes from Stop's own `c.cmd == nil` gate rather than from Close's (T-314 ⑤
// mutation m3: dropping the sup check stays green - do not cite this case as its teeth).
func TestCloseWithoutASupervisorReapsNothing(t *testing.T) {
	conn, _ := pipeConnection(t)
	if err := conn.Close(); err != nil {
		t.Errorf("Close on a supervisor-less connection = %v, want nil", err)
	}
}

// An Adapter that dies while we hold it is the shape cmd/core's comment insists is not
// hypothetical: the reaper goroutine's cmd.Wait releases the process handle, so the Kill in
// Stop answers `kill: invalid argument` on Windows rather than os.ErrProcessDone. Without
// the already-gone filter that becomes a failed shutdown - T-314 probe P1 measured exactly
// that message coming out of Close.
func TestCloseAfterTheChildAlreadyCrashedStaysQuiet(t *testing.T) {
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: helperLauncher("crash")}
	conn, err := sup.Connect(context.Background(), "0.1.0-test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	if conn.child.Reaped() {
		t.Fatal("this case only means something when the child is gone and nobody has reaped it" +
			" through Child.Wait yet - if that stops holding, the assertion below is vacuous")
	}
	if err := conn.Close(); err != nil {
		t.Errorf("Close on an already-crashed child = %v, want nil (the process is not running,"+
			" which is what Close was for)", err)
	}
}

// T-326 ③'s own criteria, deliberately separate from the ⑤ batch above: a deadline must
// bound the **write**, not just the wait for a reply. The stuck child never reads stdin, so
// a 256KB frame fills the OS pipe and the write parks - before this change the caller sat
// there past its deadline because the select came after it.
func TestAbandonedWriteComesBackInsideTheDeadline(t *testing.T) {
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: helperLauncher("sleep")}
	conn, err := sup.Connect(context.Background(), "0.1.0-test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() }) // kills the child, which frees the parked write

	// `request`, not `Call`: Call refuses at the §2 handshake gate before it ever reaches
	// the pipe, and the stuck helper never completes that handshake. The gate has its own
	// cases (TestCallGate); this one is about the write.
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = conn.request(ctx, "probe/big", map[string]any{"blob": strings.Repeat("x", 256*1024)}, "")
	el := time.Since(start)
	t.Logf("400ms 期限：request 返回耗时=%v err=%v", el.Round(time.Millisecond), err)
	if el > 3*time.Second {
		t.Fatalf("a 400ms deadline took %v to answer - the write is still the step that"+
			" decides whether deadlines mean anything", el)
	}
	// Both halves of the truth have to be reachable: the caller's deadline, and the fact
	// that the connection is now gone ( callers branch on these differently).
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want errors.Is(context.DeadlineExceeded)", err)
	}
	if !errors.Is(err, ErrConnectionLost) {
		t.Errorf("err = %v, want errors.Is(ErrConnectionLost): an abandoned write takes the"+
			" connection down with it", err)
	}

	// And nothing may be written behind the abandoned frame: the next request must be
	// refused outright rather than queued behind a parked writeMu.
	start = time.Now()
	_, err2 := conn.request(context.Background(), "probe/after", map[string]any{}, "")
	if !errors.Is(err2, ErrConnectionLost) {
		t.Errorf("second request err = %v, want ErrConnectionLost", err2)
	}
	if el := time.Since(start); el > time.Second {
		t.Errorf("second request took %v on a connection we gave up on, want a fast refusal", el)
	}
}

// The second uncancelable step CO-52 named: request's ctx branch calls Cancel, which writes
// to the same jammed pipe. It goes away because Cancel refuses a dead connection outright -
// it may not queue behind the writeMu the abandoned goroutine still holds.
func TestCancelAfterAnAbandonedWriteRefusesFast(t *testing.T) {
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: helperLauncher("sleep")}
	conn, err := sup.Connect(context.Background(), "0.1.0-test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, _ = conn.request(ctx, "probe/big", map[string]any{"blob": strings.Repeat("x", 256*1024)}, "")

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- conn.Cancel("in-flight-id") }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrConnectionLost) {
			t.Errorf("Cancel err = %v, want ErrConnectionLost", err)
		}
		if el := time.Since(start); el > time.Second {
			t.Errorf("Cancel took %v, want a fast refusal: it is queueing behind the parked write again", el)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Cancel never returned - that is CO-52's second uncancelable step, still open")
	}
}

// The comment on writeWithin says this is not what §9.2 rule 5 bans ("不裸启动**无限**
// goroutine"), and that is a testable claim: the parked goroutine must finish once Close
// kills the child, which only holds because its one send lands in a buffered channel.
// Unbuffer it and this case goes red with a leaked writer.
func TestAbandonedWriteDoesNotLeakItsGoroutine(t *testing.T) {
	base := runtime.NumGoroutine()
	func() {
		sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: helperLauncher("sleep")}
		conn, err := sup.Connect(context.Background(), "0.1.0-test")
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		defer cancel()
		_, _ = conn.request(ctx, "probe/big", map[string]any{"blob": strings.Repeat("x", 256*1024)}, "")
		if err := conn.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && runtime.NumGoroutine() > base {
		time.Sleep(50 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > base {
		t.Errorf("goroutines = %d, want back to the starting %d: the abandoned writer is parked"+
			" forever, which is the shape §9.2 rule 5 is actually about", got, base)
	}
}
