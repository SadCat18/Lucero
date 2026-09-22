package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// probe_test.go covers §7.2's registration entry point. The store is a recording fake
// here and gets its own real-DB test in probe_store_test.go: what this file has to prove
// is the decision logic (one attempt, which code, what may and may not be written), and
// a fake store states that as an assertion instead of hiding it behind SQL.

type recordedProbe struct {
	res      ProbeResult
	marks    []string
	calls    int
	markCall int
}

type fakeStore struct {
	mu       sync.Mutex
	probes   []ProbeResult
	verifies []string // "key:reason" from MarkUnverified
}

func (s *fakeStore) RecordProbe(_ context.Context, res ProbeResult) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probes = append(s.probes, res)
	return "inst-" + res.AdapterKey, nil
}

func (s *fakeStore) MarkUnverified(_ context.Context, key, displayName, reason string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.verifies = append(s.verifies, key+": "+reason)
	return "inst-failed-" + key, nil
}

// scriptedLauncher answers the way a real Adapter's stdio does, or fails to start at all.
// answers == nil means "the child never comes up"; answers == -1 means it comes up and
// then says nothing, which is the deadline path.
type scriptedLauncher struct {
	calls   int
	answers int
	result  InitializeResult
	err     error
	// pipe lets a test observe that Probe closed what it was handed.
	pipe io.ReadWriteCloser
}

func (l *scriptedLauncher) Start(ctx context.Context, key string) (io.ReadWriteCloser, error) {
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	if l.answers == 0 {
		return nil, os.ErrClosed
	}
	in, _ := io.Pipe() // Adapter -> Core
	_, fwd := io.Pipe()
	go func() {
		defer fwd.Close()
		if l.answers < 0 {
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
			}
			return
		}
		r := NewReader(in)
		w := NewWriter(fwd)
		f, err := r.Read()
		if err != nil {
			return
		}
		res := l.result
		if res.AdapterKey == "" {
			res = healthyResult(key)
		}
		reply, err := NewResponse(f.ID, f.RequestID, res)
		if err != nil {
			return
		}
		_ = w.Write(reply)
	}()
	p := &duplex{r: in, w: fwd}
	l.pipe = p
	return p, nil
}

type duplex struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (d *duplex) Read(p []byte) (int, error)  { return d.r.Read(p) }
func (d *duplex) Write(p []byte) (int, error) { return d.w.Write(p) }
func (d *duplex) Close() error                { d.r.Close(); return d.w.Close() }

func healthyResult(key string) InitializeResult {
	return InitializeResult{
		AdapterKey: key, AdapterVersion: "0.1.0", Protocol: Version{Major: 1, Minor: 0},
		SoftwareVersions: map[string]string{key: "26.1.0"},
		AuthMode:         AuthNone, Health: HealthHealthy, WriteEntry: true,
		Capabilities: []Capability{{
			Name: "session.send", SupportLevel: LevelNative, EffectiveBackend: BackendNativeAPI,
			WriteCapable: true, DeliveryModes: []string{DeliveryStartNow}, Confidence: ConfidenceHigh,
			LastVerifiedAt: 1789800000000,
		}},
	}
}

func registryWith(t *testing.T, l Launcher, s ProbeStore) *Registry {
	t.Helper()
	r, err := NewRegistry(Version{Major: 1, Minor: 0}, []Entry{{
		AdapterKey: "fake", DisplayName: "Fake Adapter", Launcher: l,
	}}, s, func() int64 { return 1789900000000 })
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func TestProbeRecordsWhatTheHandshakeSaid(t *testing.T) {
	store := new(fakeStore)
	r := registryWith(t, &scriptedLauncher{answers: 1}, store)

	got, err := r.Probe(context.Background(), "fake")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got.AdapterInstanceID != "inst-fake" {
		t.Errorf("instance id = %q, want the one the store reported", got.AdapterInstanceID)
	}
	if got.HealthState != HealthHealthy || got.VerifiedAt != 1789900000000 {
		t.Errorf("health/verified = %q/%d, want the reported HEALTHY at the clock's time", got.HealthState, got.VerifiedAt)
	}
	// §2.1's software_versions is keyed by component: the Adapter's own line is the one
	// that belongs in software_version, not whichever entry a map walk happened to hit.
	if got.SoftwareVersion != "26.1.0" {
		t.Errorf("software_version = %q, want fake's own line out of the map", got.SoftwareVersion)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0].Name != "session.send" {
		t.Errorf("capabilities = %+v, want the §3 effective set", got.Capabilities)
	}
	if len(store.probes) != 1 || len(store.verifies) != 0 {
		t.Fatalf("store saw %d probes / %d failure rows, want 1/0", len(store.probes), len(store.verifies))
	}
}

// TestProbeTriesOnceAndNeverRetries is the v0.1 boundary in §7.2: probe must not turn into
// an auto-recovery loop with a backoff story of its own.
func TestProbeTriesOnceAndNeverRetries(t *testing.T) {
	launch := &scriptedLauncher{err: os.ErrPermission}
	store := new(fakeStore)
	r := registryWith(t, launch, store)

	_, err := r.Probe(context.Background(), "fake")
	if launch.calls != 1 {
		t.Errorf("launcher called %d times, want exactly one attempt (§7.2: 不重试)", launch.calls)
	}
	if len(store.probes) != 0 {
		t.Error("a failed probe wrote capability rows, which §7.2 forbids")
	}
	if len(store.verifies) != 1 {
		t.Fatalf("failure rows = %d, want the one health write", len(store.verifies))
	}
	var e *Error
	if !asProbeError(err, &e) || e.Code != ErrAdapterUnresponsive {
		t.Errorf("err = %v, want ADAPTER_UNRESPONSIVE (§15.2's 503 side)", err)
	}
}

// TestProbeKeepsTheTwoRefusalsApart is §4.1's rule again: an unknown key and an Adapter
// that will not come up answer different questions, so they may not share a code, and
// only the second one is allowed to write a health row.
func TestProbeKeepsTheTwoRefusalsApart(t *testing.T) {
	store := new(fakeStore)
	r := registryWith(t, &scriptedLauncher{answers: 1}, store)

	_, err := r.Probe(context.Background(), "never-registered")
	var e *Error
	if !asProbeError(err, &e) || e.Code != ErrInvalidParams {
		t.Fatalf("unknown key = %v, want INVALID_PARAMS - a registry lookup miss is not an outage", err)
	}
	if len(store.verifies) != 0 || len(store.probes) != 0 {
		t.Errorf("the unknown key wrote %d/%d rows: nothing here is evidence about any Adapter",
			len(store.probes), len(store.verifies))
	}
	if !strings.Contains(err.Error(), "not a registered adapter_key") {
		t.Errorf("message %q should say the key is unregistered", err)
	}
}

func TestProbeRefusesAnotherMajorWithoutGuessing(t *testing.T) {
	res := healthyResult("fake")
	res.Protocol = Version{Major: 2, Minor: 0}
	launch := &scriptedLauncher{answers: 1, result: res}
	store := new(fakeStore)
	r := registryWith(t, launch, store)

	_, err := r.Probe(context.Background(), "fake")
	var e *Error
	if !asProbeError(err, &e) || e.Code != ErrProtocolVersionMismatch {
		t.Fatalf("err = %v, want PROTOCOL_VERSION_MISMATCH (§2.2), not the generic outage code", err)
	}
	if len(store.verifies) != 1 {
		t.Errorf("failure rows = %d, want exactly one health row for the mismatch", len(store.verifies))
	}
	if len(store.probes) != 0 {
		t.Error("a refused handshake still wrote capability rows")
	}
}

func TestProbeGivesUpOnTheCallersDeadline(t *testing.T) {
	store := new(fakeStore)
	r := registryWith(t, &scriptedLauncher{answers: -1}, store)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := r.Probe(ctx, "fake")
	if time.Since(started) > 3*time.Second {
		t.Fatalf("probe waited %s for a silent Adapter: it must honour the caller's deadline", time.Since(started))
	}
	var e *Error
	if !asProbeError(err, &e) || e.Code != ErrAdapterUnresponsive {
		t.Fatalf("err = %v, want ADAPTER_UNRESPONSIVE on the silence path", err)
	}
	if !strings.Contains(err.Error(), "deadline") {
		t.Errorf("message %q should name the deadline as the reason", err)
	}
}

// TestProbeResultCarriesEveryRequiredAdapterInstanceField reads the OpenAPI document
// rather than a copy of it: §7.2 lets the handler add nothing, so a field yaml calls
// required has to be obtainable from the probe, and the day the schema grows one this
// test is where it shows up.
func TestProbeResultCarriesEveryRequiredAdapterInstanceField(t *testing.T) {
	raw, err := os.ReadFile(yamlPath(t))
	if err != nil {
		t.Skipf("core-v1.yaml not reachable (%v): skipped, not passed", err)
	}
	body := strings.ReplaceAll(string(raw), "\r", "")
	i := strings.Index(body, "\n    AdapterInstance:")
	if i < 0 {
		t.Fatal(`no "    AdapterInstance:" schema in core-v1.yaml: the anchor moved`)
	}
	line := body[i:]
	j := strings.Index(line, "required:")
	if j < 0 {
		t.Fatal("AdapterInstance has no required: list")
	}
	list := line[j+len("required:"):]
	list = list[strings.Index(list, "[")+1:]
	list = list[:strings.Index(list, "]")]
	required := strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' })
	if len(required) < 4 {
		t.Fatalf("required parsed to %v: fix the parser, do not judge green", required)
	}

	view, err := json.Marshal((&ProbeResult{
		AdapterInstanceID: "i", AdapterKey: "k", DisplayName: "d", SoftwareVersion: "s",
		AdapterVersion: "a", ProtocolVersion: "1.0", VerifiedAt: 5, HealthState: HealthHealthy,
		EffectiveBackend: BackendNativeAPI,
	}).AdapterInstance())
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(view, &asMap); err != nil {
		t.Fatal(err)
	}
	for _, want := range required {
		if _, ok := asMap[want]; !ok {
			t.Errorf("yaml requires %q on AdapterInstance but the projection omits it: the handler would have to invent it", want)
		}
	}
}

// yamlPath walks up to the OpenAPI document the same way the contract checks do.
func yamlPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "specs", "openapi", "core-v1.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// asProbeError keeps the assertions readable: the probe's contract is that it answers
// with a coded *Error, not a wrapped string a caller has to substring-match - the code is
// what §15.2 turns into an HTTP status.
func asProbeError(err error, target **Error) bool {
	return errors.As(err, target)
}
