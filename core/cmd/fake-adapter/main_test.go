package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// TestHelperFakeAdapter is not a test: re-executed as the child process it *becomes*
// the fake adapter, serving real stdio. That keeps these tests honest about §10's
// premise (a separate process) without shelling out to the go tool to build a binary.
// Because it exits through main(), the crash status a parent asserts on is the one
// the shipped binary produces.
func TestHelperFakeAdapter(t *testing.T) {
	if os.Getenv("FAKE_ADAPTER_HELPER") != "1" {
		t.Skip("helper process: set FAKE_ADAPTER_HELPER=1 to run the fake adapter")
	}
	// main(), not run(): the child's stdout IS the protocol channel, so a single
	// line of test-framework output there would poison the stream. main() exits with
	// the process status the caller asserts on and writes nothing to stdout itself.
	main()
}

// fakeProcess is the fake adapter as it actually runs: a child process with its
// stdin, the frame streams on both ends, and whatever it wrote to stderr.
type fakeProcess struct {
	cmd    *exec.Cmd
	stdin  io.Closer
	out    *adapter.Writer
	stderr *bytes.Buffer
	// frames carries everything the child says, read by one goroutine so two
	// concurrent Read calls on one Reader can never interleave inside its buffer.
	frames chan read
	// seen is every frame as it arrived, responses included. CO-8's ordering claim
	// ("claimed before dispatched before the reply") has to be asserted on a log that
	// skipped nothing: a helper that reads past frames to find what it wants turns
	// "the first frame was X" into "X arrived eventually".
	seen []*adapter.Frame
}

type read struct {
	f   *adapter.Frame
	err error
}

// startFakeAdapter launches the fake as a real subprocess and hands back its pipes.
func startFakeAdapter(t *testing.T) *fakeProcess {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperFakeAdapter")
	cmd.Env = append(os.Environ(), "FAKE_ADAPTER_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p := &fakeProcess{cmd: cmd, stdin: stdin, out: adapter.NewWriter(stdin),
		stderr: stderr, frames: make(chan read)}
	go func() {
		r := adapter.NewReader(stdout)
		for {
			f, err := r.Read()
			p.frames <- read{f: f, err: err}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
	})
	return p
}

// nextFrame returns the frame the child sent next, failing rather than waiting
// forever: an adapter that stops answering is the bug under test, not a reason to
// park the suite until the go tool times out.
func (p *fakeProcess) nextFrame(t *testing.T) *adapter.Frame {
	t.Helper()
	select {
	case r := <-p.frames:
		if r.err != nil {
			t.Fatalf("read from the fake adapter: %v", r.err)
		}
		return r.f
	case <-time.After(10 * time.Second):
		t.Fatal("no frame from the fake adapter within 10s")
	}
	return nil
}

// silent reports whether the adapter kept its mouth shut for d. A frame that does
// arrive within d is returned so the caller can name it in the failure.
func (p *fakeProcess) silent(t *testing.T, d time.Duration) (*adapter.Frame, bool) {
	t.Helper()
	select {
	case r := <-p.frames:
		if r.err != nil {
			t.Fatalf("read from the fake adapter: %v", r.err)
		}
		return r.f, false
	case <-time.After(d):
		return nil, true
	}
}

func (p *fakeProcess) write(t *testing.T, f *adapter.Frame) {
	t.Helper()
	if err := p.out.Write(f); err != nil {
		t.Fatalf("write %s: %v", f.Method, err)
	}
}

// request builds a §1 call frame with a request_id of its own.
func request(id, method string, params any) *adapter.Frame {
	raw, _ := json.Marshal(params)
	return &adapter.Frame{JSONRPC: adapter.JSONRPCVersion, ID: id, Method: method,
		Params: raw, RequestID: "req-" + id}
}

// call sends one request and returns its reply, recording every frame it passed
// through on the way. Matching on the id rather than "the next frame" is what lets a
// session/send answer with notifications before its response - but the frames it read
// past stay in the log, so an ordering assertion on top of them is not a skipping one.
func call(t *testing.T, p *fakeProcess, id, method string, params any) *adapter.Frame {
	t.Helper()
	p.write(t, request(id, method, params))
	for {
		f := p.nextFrame(t)
		p.seen = append(p.seen, f)
		if f.Kind() == adapter.KindResponse {
			if f.ID != id {
				t.Fatalf("reply for %q while waiting for %q", f.ID, id)
			}
			return f
		}
	}
}

// initialize performs §2's handshake, which every other call is gated behind.
func initialize(t *testing.T, p *fakeProcess) {
	t.Helper()
	reply := call(t, p, "init", adapter.MethodInitialize, adapter.InitializeParams{
		Protocol: adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}})
	if reply.Error != nil {
		t.Fatalf("adapter/initialize: %s", reply.Error)
	}
}

// trace labels every frame in arrival order, responses included, and runs each
// notification through the decoder Core uses: a frame the fake is happy with and
// DecodeStream rejects is not evidence (CO-8).
func (p *fakeProcess) trace(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, f := range p.seen {
		if f.Kind() == adapter.KindResponse {
			out = append(out, "reply#"+f.ID)
			continue
		}
		s, err := adapter.DecodeStream(f)
		if err != nil {
			t.Fatalf("Core could not decode our own %s notification: %v", f.Method, err)
		}
		label := fmt.Sprintf("%s/%d", s.Method, s.SequenceNo)
		if s.TurnRef != "" { // §6 规则 5: the ticket is part of what the order proves
			label += "@" + s.TurnRef
		}
		out = append(out, label)
	}
	return out
}

// notifications is the stream alone, for counting what a call put on the wire.
func (p *fakeProcess) notifications() []*adapter.Frame {
	var out []*adapter.Frame
	for _, f := range p.seen {
		if f.Kind() != adapter.KindResponse {
			out = append(out, f)
		}
	}
	return out
}

// TestFakeAdapterRefusesBeforeHandshake is §2's gate measured across a process
// boundary, which no in-process test can prove: the very first call of any kind is
// refused, and only after that does the handshake go through.
func TestFakeAdapterRefusesBeforeHandshake(t *testing.T) {
	p := startFakeAdapter(t)

	for _, method := range []string{"session/list", "session/send", "quota/read", adapter.MethodDebugCrash} {
		reply := call(t, p, "pre-"+method, method, map[string]any{})
		if reply.Error == nil {
			t.Fatalf("%s before initialize: got %+v, want an error", method, reply)
		}
		if reply.Error.Code != adapter.ErrProtocolNotInitialized {
			t.Errorf("%s before initialize: code %s, want PROTOCOL_NOT_INITIALIZED", method, reply.Error.Code)
		}
		if !strings.Contains(reply.Error.Message, "initialize") {
			t.Errorf("%s: message %q does not say what is missing", method, reply.Error.Message)
		}
	}

	reply := call(t, p, "1", adapter.MethodInitialize, adapter.InitializeParams{
		Protocol: adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor},
	})
	if reply.Error != nil {
		t.Fatalf("initialize: %s", reply.Error)
	}
	// §1: stdout carried frames only, so the startup line has to be on stderr.
	if !strings.Contains(p.stderr.String(), "fake-adapter up") {
		t.Errorf("startup diagnostic missing from stderr: %q", p.stderr)
	}
}

// TestFakeAdapterHandshakeSatisfiesCore runs the fake's self-declaration through the
// same validator Core uses, and pins §9: debug.* is legal here solely because
// adapter_key is "fake" and the level is EXPERIMENTAL.
func TestFakeAdapterHandshakeSatisfiesCore(t *testing.T) {
	p := startFakeAdapter(t)
	reply := call(t, p, "1", adapter.MethodInitialize, adapter.InitializeParams{
		Protocol: adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor},
	})
	var res adapter.InitializeResult
	if err := json.Unmarshal(reply.Result, &res); err != nil {
		t.Fatalf("initialize reply does not decode into §2's shape: %v", err)
	}
	hs, err := adapter.NewHandshake(
		adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}, res)
	if err != nil {
		t.Fatalf("Core would reject the fake's own handshake: %v", err)
	}
	if res.AdapterKey != "fake" || !res.WriteEntry {
		t.Errorf("identity = %q write_entry=%v", res.AdapterKey, res.WriteEntry)
	}
	if len(res.SoftwareVersions) < 2 {
		t.Errorf("software_versions = %v, want the §2.1 multi-line shape", res.SoftwareVersions)
	}
	c, ok := hs.Effective("debug.crash")
	if !ok || c.SupportLevel != adapter.LevelExperimental {
		t.Errorf("debug.crash effective = %+v (found=%v), want it declared at EXPERIMENTAL", c, ok)
	}
	if _, ok := hs.Effective("session.locate"); ok {
		t.Error("session.locate is UNSUPPORTED and must not be an effective capability (§4)")
	}
	send, ok := hs.Effective("session.send")
	if !ok || len(send.DeliveryModes) != 3 {
		t.Errorf("session.send declares %v, want all three §5 states", send.DeliveryModes)
	}
}

// TestFakeAdapterHealth covers §4.1's reply shape and its no-impersonation rule, and
// that a method this build has not implemented is refused with INTERNAL_ERROR rather
// than a contract refusal that would read as a Provider decision.
func TestFakeAdapterHealth(t *testing.T) {
	p := startFakeAdapter(t)
	call(t, p, "1", adapter.MethodInitialize, adapter.InitializeParams{
		Protocol: adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}})

	reply := call(t, p, "2", adapter.MethodHealth, struct{}{})
	if reply.Error != nil {
		t.Fatalf("adapter/health: %s", reply.Error)
	}
	var h adapter.HealthResult
	if err := json.Unmarshal(reply.Result, &h); err != nil {
		t.Fatalf("health reply does not decode: %v", err)
	}
	if err := h.Validate(); err != nil {
		t.Errorf("our own health reply is illegal: %v", err)
	}

	// session/push is withdrawn for v0.1 by CO-40 (contract ② §4.0): it is an A->C notice,
	// and since §4.2 裁定 ① every notification carries a Core-issued turn_ref, a flow Core
	// never requested has no ticket to echo - a deadlock, not a wording gap. So this is a
	// decision being guarded, not a to-do: an accidental implementation would reopen the
	// attribution model that was just closed. (Every §4 C->A name is served as of REQ-T8's
	// close, which TestFakeAdapterServesEveryContractMethod enforces.)
	// The assertion
	// used to sit on session/list, which §10 now requires and this build serves.
	unbuilt := call(t, p, "3", "session/push", struct{}{})
	if unbuilt.Error == nil || unbuilt.Error.Code != adapter.ErrInternal {
		t.Errorf("session/push = %+v, want INTERNAL_ERROR until this build implements it", unbuilt)
	}
}

// TestFakeAdapterSurvivesSecondInitialize is the §14 acceptance blocker test 角色 reported:
// the second adapter/initialize must be answered with a refusal and the process must go
// on serving. It used to die right there, leaving the peer waiting on a frame that would
// never arrive - which a caller can only notice by hitting its deadline.
func TestFakeAdapterSurvivesSecondInitialize(t *testing.T) {
	p := startFakeAdapter(t)
	params := adapter.InitializeParams{
		Protocol: adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}}
	call(t, p, "1", adapter.MethodInitialize, params)

	second := call(t, p, "2", adapter.MethodInitialize, params)
	if second.Error == nil {
		t.Fatalf("second initialize got a result, want a refusal: %s", second.Result)
	}
	if second.Error.Code != adapter.ErrInvalidParams {
		t.Errorf("second initialize code = %s, want INVALID_PARAMS", second.Error.Code)
	}
	if reply := call(t, p, "3", adapter.MethodHealth, struct{}{}); reply.Error != nil {
		t.Errorf("health after the refused re-initialize: %s, want the session still serving", reply.Error)
	}
}

// TestFakeAdapterCrashExit is §10's debug/crash and the primitive under §8: the
// process dies with status 2 after answering, and the caller survives to observe it.
// The full "Core is still alive and other Adapters are unaffected" regression needs
// the Supervisor (internal/process) and lands with it.
func TestFakeAdapterCrashExit(t *testing.T) {
	p := startFakeAdapter(t)
	cmd := p.cmd
	call(t, p, "1", adapter.MethodInitialize, adapter.InitializeParams{
		Protocol: adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}})

	reply := call(t, p, "2", adapter.MethodDebugCrash, struct{}{})
	if reply.Error != nil {
		t.Fatalf("debug/crash: %s", reply.Error)
	}
	if err := p.stdin.Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	err := cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("Wait = %v, want exit status 2 after debug/crash", err)
	}
	if got := exit.ExitCode(); got != 2 {
		t.Errorf("exit code = %d, want 2 (§10)", got)
	}
	if !strings.Contains(p.stderr.String(), "crashing on demand") {
		t.Errorf("stderr lacks the crash notice: %q", p.stderr)
	}
}
