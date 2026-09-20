package process

import (
	"bufio"
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

// pipeConnection wires a Connection to an in-test peer over two pipes, so the pump's
// behaviour can be checked without a subprocess. Everything the peer sends goes
// through sent; everything it receives comes back on replies.
type pipePeer struct {
	in     *adapter.Writer      // peer -> connection (frames the pump reads)
	out    *adapter.Reader      // connection -> peer (frames the pump writes)
	closed chan struct{}
	once   sync.Once
}

func pipeConnection(t *testing.T) (*Connection, *pipePeer) {
	t.Helper()
	usR, peerW := io.Pipe()  // connection reads what the peer writes
	peerR, usW := io.Pipe()  // connection writes what the peer reads
	p := &pipePeer{
		in:     adapter.NewWriter(peerW),
		out:    adapter.NewReader(usR),
		closed: make(chan struct{}),
	}
	c := &Connection{
		child:       &Child{Stdin: usW, Stdout: usR, Stderr: io.NopCloser(strings.NewReader("")), PID: os.Getpid(), done: make(chan error, 1)},
		w:           adapter.NewWriter(usW),
		r:           adapter.NewReader(usR),
		coreVersion: "0.1.0-test",
		pending:     map[string]chan *adapter.Frame{},
		turns:       map[string]*adapter.TurnTracker{},
		inbound:     make(chan Inbound, 64),
		dead:        make(chan struct{}),
	}
	t.Cleanup(func() {
		peerW.Close()
		peerR.Close()
		usW.Close()
		usR.Close()
	})
	return c, p
}

// answer reads one request from the connection and writes back a reply.
func (p *pipePeer) answer(t *testing.T, result any, e *adapter.Error) *adapter.Frame {
	t.Helper()
	f, err := p.out.Read()
	if err != nil {
		t.Fatalf("peer read: %v", err)
	}
	reply := &adapter.Frame{JSONRPC: adapter.JSONRPCVersion, ID: f.ID, RequestID: f.RequestID}
	switch {
	case e != nil:
		reply.Error = e
	default:
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("peer marshal: %v", err)
		}
		reply.Result = raw
	}
	if err := p.in.Write(reply); err != nil {
		t.Fatalf("peer write: %v", err)
	}
	return f
}

func (p *pipePeer) send(f *adapter.Frame) error { return p.in.Write(f) }

// readFrame returns the next frame the connection writes, or fails after 2s so a
// missing send shows up as a test failure instead of a hang.
func (p *pipePeer) readFrame(t *testing.T) *adapter.Frame {
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
		t.Fatal("no frame arrived within 2s")
		return nil
	}
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

func TestConnectionCallRoundTrip(t *testing.T) {
	c, p := pipeConnection(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		wg.Done()
		p.answer(t, map[string]any{"health": adapter.HealthHealthy, "confidence": adapter.ConfidenceHigh,
			"checked_at": 1789800000000})
	}()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()
	reply, err := c.Call(ctx, adapter.MethodHealth, struct{}{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if reply.Error != nil {
		t.Fatalf("Call reply = %s, want a result", reply.Error)
	}
	_ = wg

	// §1: the frame that went out carried a UUIDv7 request_id and deadline_at.
	sent := p.replay(t)
	if sent.RequestID == "" || sent.ID != sent.RequestID {
		t.Errorf("request id/request_id = %q/%q, want the same UUIDv7 in both", sent.ID, sent.RequestID)
	}
	if _, ok := adapter.RequestID(sent.RequestID).Timestamp(); !ok {
		t.Errorf("request_id %q is not a readable UUIDv7 (§1)", sent.RequestID)
	}
	if sent.DeadlineAt == 0 {
		t.Error("deadline_at missing although the context carried a deadline (§1)")
	}
}

// replay peeks at the last request the connection sent. pipePeer records nothing, so
// the test re-reads by asking the writer to echo: instead the frame is captured by
// answer(), which returns it.
func (p *pipePeer) replay(t *testing.T) *adapter.Frame {
	t.Helper()
	t.Fatal("replay is not available; use answer()'s return value")
	return nil
}

func TestConnectionRefusesBeforeInitialize(t *testing.T) {
	c, _ := pipeConnection(t)
	_, err := c.Call(context.Background(), "session/list", struct{}{})
	var e *adapter.Error
	if !errors.As(err, &e) || e.Code != adapter.ErrProtocolNotInitialized {
		t.Fatalf("Call before Initialize = %v, want PROTOCOL_NOT_INITIALIZED (§2)", err)
	}
}

func TestConnectionDeadlineSendsCancel(t *testing.T) {
	c, p := pipeConnection(t)
	go func() {
		// Never answer the request; do observe the cancellation that must follow.
		f := p.readFrame(t)
		if f.Method != adapter.CancelMethod {
			t.Errorf("second frame is %q, want $/cancel (§1)", f.Method)
		}
		var params map[string]string
		json.Unmarshal(f.Params, &params)
		if params["id"] == "" {
			t.Errorf("$/cancel carries no target id: %s", f.Params)
		}
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
}

// TestConnectionDeathMarksTurnUnknown is the §6.3 half of §8: the process going away
// is not evidence about the turn, so an unclosed turn becomes UNKNOWN and any
// in-flight call comes back instead of hanging.
func TestConnectionDeathMarksTurnUnknown(t *testing.T) {
	c, p := pipeConnection(t)
	c.mu.Lock()
	tr := c.turnLocked("exec-1")
	c.mu.Unlock()
	if err := tr.Claim(true, "exec-1", "prov-1"); err != nil {
		t.Fatal(err)
	}
	if err := tr.Dispatch("turn-1", "exec-1", "prov-1"); err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), adapter.MethodHealth, struct{}{})
		errCh <- err
	}()
	// Let the request reach the peer, then pull the stream away under it.
	go func() {
		f := p.readFrame(t)
		if f.Method != adapter.MethodHealth {
			t.Errorf("peer saw %q", f.Method)
		}
	}()
	time.Sleep(30 * time.Millisecond)

	c.kill(errors.New("child exited"))
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrConnectionLost) {
			t.Errorf("Call = %v, want ErrConnectionLost", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the call hung after the process died; §8 requires it to come back")
	}
	if tr.State() != adapter.TurnUnknown {
		t.Errorf("turn state after death = %q, want UNKNOWN (§6.3, not FAILED)", tr.State())
	}
}

func TestConnectionDedupsAndTracksTurn(t *testing.T) {
	c, p := pipeConnection(t)
	go func() { p.answer(t, handshakeResult(), nil) }()
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	frames := []*adapter.StreamFrame{
		{Method: adapter.MethodTurnClaimed, SequenceNo: 1, Accepted: true, ExecutionID: "e1", ProviderExecutionID: "p1"},
		{Method: adapter.MethodTurnClaimed, SequenceNo: 1, Accepted: true, ExecutionID: "e1", ProviderExecutionID: "p1"}, // replay
		{Method: adapter.MethodTurnDispatch, SequenceNo: 2, ExecutionID: "e1", TurnID: "t1", ProviderExecutionID: "p1"},
		{Method: adapter.MethodStreamDelta, SequenceNo: 2, Channel: adapter.ChannelAssistant, Text: "late"}, // duplicate seq
		{Method: adapter.MethodTurnFinished, SequenceNo: 3, ExecutionID: "e1", ResultCode: "OK"},
	}
	go func() {
		for _, s := range frames {
			f, err := adapter.NewStream("note", s)
			if err != nil {
				t.Errorf("NewStream: %v", err)
				return
			}
			if err := p.send(f); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	var got []string
	deadline := time.After(3 * time.Second)
	for range 3 { // 5 frames, 2 of them duplicates per §6.2
		select {
		case ev := <-c.Inbound():
			if ev.Err != nil {
				t.Errorf("unexpected evidence error: %v", ev.Err)
			}
			got = append(got, ev.Frame.Method)
		case <-deadline:
			t.Fatalf("only received %v of the 3 distinct frames", got)
		}
	}
	if strings.Join(got, ",") != strings.Join([]string{
		adapter.MethodTurnClaimed, adapter.MethodTurnDispatch, adapter.MethodTurnFinished}, ",") {
		t.Errorf("forwarded %v, want the three distinct frames in order", got)
	}
	if c.LastSeq() != 3 {
		t.Errorf("LastSeq = %d, want 3", c.LastSeq())
	}
	if c.MissingSeq() != 0 {
		t.Errorf("MissingSeq = %d, want 0 for a contiguous stream", c.MissingSeq())
	}
	if tr := c.Turn("e1"); tr.State() != adapter.TurnClosed || tr.Result() != "OK" {
		t.Errorf("turn after the stream = %+v, want CLOSED/OK", tr)
	}
}

// TestConnectionAgainstRealFakeAdapter is the reason §10 put the FakeAdapter in its own
// process: everything above runs in one binary, this one crosses a real exec boundary
// with the shipped cmd/fake-adapter, including the cursor reset after the handshake.
func TestConnectionAgainstRealFakeAdapter(t *testing.T) {
	exe := buildFakeAdapter(t)
	var events []Event
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: func() (*exec.Cmd, error) {
		return exec.Command(exe), nil
	}, Sink: func(e Event) { events = append(events, e) }}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
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
	if c := conn.Child(); c.PID <= 0 {
		t.Errorf("child pid = %d, want a real process", c.PID)
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

	// A method this build has not implemented must come back as an explicit refusal
	// over a real pipe, not as a timeout.
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
}

var (
	fakeExeOnce sync.Once
	fakeExePath string
	fakeExeErr  error
)

// buildFakeAdapter compiles cmd/fake-adapter once per package run. It shells out to
// the same toolchain the tests are running under, so no pre-built artefact is needed.
func buildFakeAdapter(t *testing.T) string {
	t.Helper()
	fakeExeOnce.Do(func() {
		goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
		if runtime.GOOS == "windows" {
			goTool += ".exe"
		}
		dir, err := os.MkdirTemp("", "fake-adapter")
		if err != nil {
			fakeExeErr = err
			return
		}
		fakeExePath = filepath.Join(dir, "fake-adapter.exe")
		if runtime.GOOS != "windows" {
			fakeExePath = filepath.Join(dir, "fake-adapter")
		}
		cmd := exec.Command(goTool, "build", "-o", fakeExePath, "../../cmd/fake-adapter")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			fakeExeErr = errors.New("go build: " + err.Error() + "\n" + string(out))
			return
		}
		_ = bufio.NewReader
	})
	if fakeExeErr != nil {
		t.Skipf("cannot build fake-adapter for this test: %v", fakeExeErr)
	}
	return fakeExePath
}
