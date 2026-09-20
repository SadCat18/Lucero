package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

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
	in     *adapter.Reader
	stderr *bytes.Buffer
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
		in: adapter.NewReader(stdout), stderr: stderr}
	t.Cleanup(func() {
		_ = stdin.Close()
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
	})
	return p
}

// call sends one request and returns the reply, reading exactly one frame so nothing
// after it (the test framework's own output) gets parsed.
func call(t *testing.T, p *fakeProcess, id, method string, params any) *adapter.Frame {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	f := &adapter.Frame{JSONRPC: adapter.JSONRPCVersion, ID: id, Method: method,
		Params: raw, RequestID: "req-" + id}
	if err := p.out.Write(f); err != nil {
		t.Fatalf("write %s: %v", method, err)
	}
	reply, err := p.in.Read()
	if err != nil {
		t.Fatalf("read reply of %s: %v", method, err)
	}
	return reply
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

	unbuilt := call(t, p, "3", "session/list", struct{}{})
	if unbuilt.Error == nil || unbuilt.Error.Code != adapter.ErrInternal {
		t.Errorf("session/list = %+v, want INTERNAL_ERROR until this build implements it", unbuilt)
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
