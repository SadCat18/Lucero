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
		Channel: adapter.ChannelAssistant, Text: "early"}); err != nil {
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

// TestConnectionDeathMarksTurnUnknown is the §6.3 half of §8: the process going away
// is not evidence about the turn, so an unclosed turn becomes UNKNOWN and an in-flight
// call comes back instead of hanging.
func TestConnectionDeathMarksTurnUnknown(t *testing.T) {
	c, p := established(t)
	// Drive the turn through the pump itself, the same way production does, so the
	// test does not write tracker state from a second goroutine.
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodTurnClaimed, SequenceNo: 1,
		Accepted: true, ExecutionID: "exec-1", ProviderExecutionID: "prov-1"}); err != nil {
		t.Fatal(err)
	}
	if err := p.send(&adapter.StreamFrame{Method: adapter.MethodTurnDispatch, SequenceNo: 2,
		ExecutionID: "exec-1", TurnID: "turn-1", ProviderExecutionID: "prov-1"}); err != nil {
		t.Fatal(err)
	}
	tr := c.Turn("exec-1")
	if err := waitFor(func() bool { return c.LastSeq() == 2 }, 2*time.Second); err != nil {
		t.Fatalf("the pump did not process the evidence pair: %v", err)
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
		t.Errorf("turn state after death = %q, want UNKNOWN (§6.3, not FAILED)", got)
	}
	if got := tr.ProviderExecutionID(); got != "prov-1" {
		t.Errorf("provider handle = %q, want it kept for Recovery to query by (§6.3)", got)
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

	stream := []*adapter.StreamFrame{
		{Method: adapter.MethodTurnClaimed, SequenceNo: 1, Accepted: true, ExecutionID: "e1", ProviderExecutionID: "p1"},
		{Method: adapter.MethodTurnClaimed, SequenceNo: 1, Accepted: true, ExecutionID: "e1", ProviderExecutionID: "p1"}, // replay
		{Method: adapter.MethodTurnDispatch, SequenceNo: 2, ExecutionID: "e1", TurnID: "t1", ProviderExecutionID: "p1"},
		{Method: adapter.MethodStreamDelta, SequenceNo: 2, Channel: adapter.ChannelAssistant, Text: "late"}, // stale seq
		{Method: adapter.MethodTurnFinished, SequenceNo: 3, ExecutionID: "e1", ResultCode: "OK"},
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
	for len(got) < 3 { // five frames in, three distinct ones out
		select {
		case ev := <-c.Inbound():
			if ev.Err != nil {
				t.Fatalf("unexpected evidence error: %v", ev.Err)
			}
			got = append(got, ev.Frame.Method)
		case <-deadline:
			t.Fatalf("only forwarded %v of the three distinct frames", got)
		}
	}
	if want := []string{adapter.MethodTurnClaimed, adapter.MethodTurnDispatch,
		adapter.MethodTurnFinished}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("forwarded %v, want %v (§6.2 drops the replays)", got, want)
	}
	if c.LastSeq() != 3 {
		t.Errorf("LastSeq = %d, want 3", c.LastSeq())
	}
	if c.MissingSeq() != 0 {
		t.Errorf("MissingSeq = %d, want 0 for a contiguous stream", c.MissingSeq())
	}
	tr := c.Turn("e1")
	if tr.State() != adapter.TurnClosed || tr.Result() != "OK" {
		t.Errorf("turn after the stream = %s/%q, want CLOSED/OK", tr.State(), tr.Result())
	}
	drain(c)
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
	unbuilt, err := conn.Call(ctx, "session/list", struct{}{})
	if err != nil {
		t.Fatalf("Call(session/list): %v", err)
	}
	if unbuilt.Error == nil || unbuilt.Error.Code != adapter.ErrInternal {
		t.Errorf("session/list = %+v, want INTERNAL_ERROR until FakeAdapter implements it", unbuilt)
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

	send := func(id, execID string) adapter.SendResult {
		t.Helper()
		reply, err := conn.Call(ctx, "session/send", adapter.SendParams{
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
	if got := nextNotice(t, conn); got.Method != adapter.MethodStreamDelta {
		t.Fatalf("third notification = %s, want stream/delta", got.Method)
	}
	tr1 := conn.Turn("exec-1")
	if tr1.State() != adapter.TurnRunning {
		t.Errorf("turn state after the pair = %q, want RUNNING (§7.1 may now show it)", tr1.State())
	}
	if tr1.TurnID() != first.TurnID || tr1.ProviderExecutionID() != first.ProviderExecutionID {
		t.Errorf("evidence = %q/%q, reply = %q/%q: the pair and the answer name different runs",
			tr1.TurnID(), tr1.ProviderExecutionID(), first.TurnID, first.ProviderExecutionID)
	}

	// The second send closes the first turn, which is turn/finished arriving on
	// exec-1's key while exec-2 opens its own pair.
	send("2", "exec-2")
	closed := false
	for i := 0; i < 4; i++ {
		s := nextNotice(t, conn)
		switch {
		case s.Method == adapter.MethodTurnFinished && s.ExecutionID == "exec-1":
			closed = true
		case s.Method == adapter.MethodTurnFinished:
			t.Errorf("turn/finished named %q, want exec-1", s.ExecutionID)
		}
	}
	if !closed {
		t.Error("exec-1 never received its turn/finished (§6.1: it is the mandatory signal)")
	}
	if tr1.State() != adapter.TurnClosed || tr1.Result() != "OK" {
		t.Errorf("exec-1 = %s/%q, want CLOSED/OK", tr1.State(), tr1.Result())
	}
	if tr2 := conn.Turn("exec-2"); tr2.State() != adapter.TurnRunning {
		t.Errorf("exec-2 = %q, want RUNNING", tr2.State())
	}
	if seq, miss := conn.LastSeq(), conn.MissingSeq(); seq <= 0 || miss != 0 {
		t.Errorf("cursor = last %d, missing %d: the real stream arrived contiguous, so no hole may be recorded",
			seq, miss)
	}
}

// nextNotice waits for one notification that survived §6.2's dedup. It waits rather
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
