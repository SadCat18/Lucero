package main

// harness_test.go builds the two worlds these tests need without ever touching a real
// OpenCode install: a fixture database with the schema the profile recorded, and a pipe
// pair that speaks §1's ndjson to run() itself.
//
// Why a fixture rather than the machine's own 674 MB store: the criteria below must be
// reproducible by anyone who runs them, on a machine that may not have the product
// installed at all, and a test that reads real conversation data would put session
// content into this repository's logs. The red line for this role says 只读观察, not 把
// 观察结果带走.

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	_ "modernc.org/sqlite"
)

// fixtureSpec is what one store contains. The numbers are the tiers §16.4 was ruled on,
// so a change in the mapping shows up as a count that no longer matches rather than as a
// test that still agrees.
type fixtureSpec struct {
	Sessions     int
	CostPositive int
	CostZero     int
	CostNull     int
	WithTitle    bool
	// Journal is the ordered list of (message id, part id, text) snapshots. Order matters
	// and is therefore explicit: the provider's own seq is assigned as 1507, 1514, ... in
	// this order (sparse and large on purpose - see TestSynthesizedDeltasAreTailsNotResends
	// for why a reply that published those numbers would be wrong).
	Journal []journalStep
}

type journalStep struct {
	message string
	part    string
	text    string
}

// newFixture writes a store shaped like the profile's description: `session` with
// cost/tokens columns and `event` with the four recorded columns and no timestamp column
// (that absence is measured, not invented - profile §①).
func newFixture(t *testing.T, spec fixtureSpec) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	sessionDDL := `CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT, time_updated INTEGER, cost REAL, tokens_input INTEGER)`
	if spec.WithTitle {
		sessionDDL = `CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT, directory TEXT, time_updated INTEGER, cost REAL, tokens_input INTEGER)`
	}
	for _, s := range []string{sessionDDL,
		`CREATE TABLE event (id TEXT PRIMARY KEY, aggregate_id TEXT, seq INTEGER, type TEXT, data TEXT)`} {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("fixture schema: %v", err)
		}
	}
	costs := make([]any, 0, spec.Sessions)
	for i := 0; i < spec.CostPositive; i++ {
		costs = append(costs, 0.0053+float64(i)/1000)
	}
	for i := 0; i < spec.CostZero; i++ {
		costs = append(costs, 0.0)
	}
	for i := 0; i < spec.CostNull; i++ {
		costs = append(costs, nil)
	}
	for i, c := range costs {
		id := fmt.Sprintf("ses-%03d", i)
		var err error
		if spec.WithTitle {
			_, err = d.Exec(`INSERT INTO session (id, title, directory, time_updated, cost, tokens_input) VALUES (?,?,?,?,?,?)`,
				id, "session "+id, "D:/proj/x", 1790000000000+int64(i), c, 1200)
		} else {
			_, err = d.Exec(`INSERT INTO session (id, directory, time_updated, cost, tokens_input) VALUES (?,?,?,?,?)`,
				id, "D:/proj/x", 1790000000000+int64(i), c, 1200)
		}
		if err != nil {
			t.Fatalf("fixture session: %v", err)
		}
	}
	seq := int64(1500)
	for _, step := range spec.Journal {
		seq += 7
		payload := map[string]any{
			"part": map[string]any{
				"id":        step.part,
				"messageID": step.message,
				"sessionID": "ses-000",
				"type":      "text",
				"text":      step.text,
			},
			"sessionID": "ses-000",
			"time":      1790000100000 + seq,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("fixture payload: %v", err)
		}
		if _, err := d.Exec(`INSERT INTO event (id, aggregate_id, seq, type, data) VALUES (?,?,?,?,?)`,
			fmt.Sprintf("ev-%d", seq), "ses-000", seq, eventPartUpdated, string(raw)); err != nil {
			t.Fatalf("fixture event: %v", err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	return path
}

func defaultFixture() fixtureSpec {
	// One turn, two parts, three snapshots: p1 grows by an appended tail and p2 arrives
	// whole, which is the 2-snapshot-per-text shape §② measured (a third snapshot is added
	// by the diff tests that need one).
	return fixtureSpec{
		Sessions: 3, CostPositive: 1, CostZero: 1, CostNull: 1, WithTitle: true,
		Journal: []journalStep{
			{message: "msg-a", part: "p1", text: "Hello"},
			{message: "msg-a", part: "p2", text: "tool ran"},
			{message: "msg-a", part: "p1", text: "Hello world"},
		},
	}
}

// replacedFixture holds the shape §② says is *not* the common case: a snapshot that is
// not an extension of its neighbour. It exists so Replaced is a measured branch rather
// than a defensive one nobody can reach.
func replacedFixture() fixtureSpec {
	return fixtureSpec{
		Sessions: 1, CostPositive: 1, WithTitle: true,
		Journal: []journalStep{
			{message: "msg-a", part: "p1", text: "abc"},
			{message: "msg-a", part: "p1", text: "totally different"},
		},
	}
}

// pipeAdapter drives run() over two in-memory pipes, one request and one reply at a
// time. The criteria below are asserted on the bytes Core would read rather than on a
// handler's return value, because "the tier sends no notifications" and "a write method
// got no result frame" are claims about the wire.
type pipeAdapter struct {
	in   io.WriteCloser
	logs *strings.Builder
	done chan error

	// frames is every frame the adapter has written, in arrival order, and inbound is the
	// same stream as a channel. A reader that takes exactly one frame per request would
	// block the adapter's writer the moment an unsolicited frame appeared (io.Pipe is
	// synchronous), and a "no notifications" criterion would then be satisfied by a
	// deadlock instead of by silence - which is how this harness was wrong before the r7
	// mutation caught it. The collector is what lets the criterion say "I looked, and
	// nothing was there".
	mu      sync.Mutex
	frames  []*adapter.Frame
	strays  []*adapter.Frame
	inbound chan *adapter.Frame

	closed  bool
	drained bool // a test consumed done itself; cleanup must not re-assert on it
}

func startAdapter(t *testing.T, cfg config) *pipeAdapter {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	logs := &strings.Builder{}
	done := make(chan error, 1)
	// Closing outW when run() returns is what turns "the adapter stopped serving" into an
	// end-of-stream a reader sees, instead of a 300s test-binary timeout on a pipe nobody
	// writes to any more.
	go func() {
		defer outW.Close()
		done <- run(inR, outW, logs, cfg)
	}()
	p := &pipeAdapter{in: inW, logs: logs, done: done,
		inbound: make(chan *adapter.Frame, 256)}
	go p.collect(outR)
	t.Cleanup(func() {
		if !p.closed {
			_ = inW.Close()
			p.closed = true
		}
		if p.drained {
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("run() ended with an error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("run() did not finish within 5s of stdin closing")
		}
	})
	return p
}

// call writes one request frame and reads exactly one reply frame. A second frame would
// be a notification, and a test that wants to look for one asks quiet() rather than being
// surprised here.
func (p *pipeAdapter) call(t *testing.T, id, method string, params any) *adapter.Frame {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	line, err := json.Marshal(adapter.Frame{JSONRPC: adapter.JSONRPCVersion, ID: id,
		Method: method, Params: raw, RequestID: "req-" + id})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	if _, err := p.in.Write(append(line, '\n')); err != nil {
		t.Fatalf("write request: %v", err)
	}
	select {
	case f := <-p.inbound:
		return f
	case <-time.After(10 * time.Second):
		t.Fatalf("no frame at all for %s within 10s (stderr: %s)", method, p.logs.String())
		return nil
	}
}

// collect is the always-reading side. Every frame is recorded and forwarded, so a reply
// and an unsolicited notification are both visible to the tests rather than one of them
// blocking the writer forever.
func (p *pipeAdapter) collect(r io.Reader) {
	scan := bufio.NewScanner(r)
	// Sized up front, once: a transcript with synthesized segments can outgrow the 64 KB
	// default, and Scanner.Buffer after the first Scan panics.
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	for scan.Scan() {
		f := &adapter.Frame{}
		if err := json.Unmarshal(scan.Bytes(), f); err != nil {
			p.mu.Lock()
			p.frames = append(p.frames, nil)
			p.mu.Unlock()
			p.inbound <- &adapter.Frame{JSONRPC: "garbage", Result: scan.Bytes()}
			continue
		}
		p.mu.Lock()
		p.frames = append(p.frames, f)
		p.mu.Unlock()
		select {
		case p.inbound <- f:
		default: // a full buffer means a test stopped reading; the frame is still recorded
		}
	}
}

// allFrames is the recorded stream, which is what the notification criterion asserts over.
func (p *pipeAdapter) allFrames() []*adapter.Frame {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*adapter.Frame, len(p.frames))
	copy(out, p.frames)
	return out
}

// nextFrame waits a bounded time for any frame on the stream. It is the primitive behind
// the "no result frame was produced" claims: the answer has to be measured on the wire.
func (p *pipeAdapter) nextFrame(d time.Duration) (*adapter.Frame, bool) {
	select {
	case f := <-p.inbound:
		return f, true
	case <-time.After(d):
		return nil, false
	}
}

// quiet waits a bounded time for anything further to arrive, and files whatever does.
// Silence is the assertion in TestReadOnlySendsNoNotifications, so it has to be a
// measured silence rather than a snapshot taken before the frame could have landed.
func (p *pipeAdapter) quiet(d time.Duration) (*adapter.Frame, bool) {
	f, ok := p.nextFrame(d)
	if ok {
		p.noteStray(f)
	}
	return f, ok
}

// noteStray files a frame that answered no request of ours; strays are what the
// "no notifications" criterion reads.
func (p *pipeAdapter) noteStray(f *adapter.Frame) {
	p.mu.Lock()
	p.strays = append(p.strays, f)
	p.mu.Unlock()
}

func (p *pipeAdapter) strayFrames() []*adapter.Frame {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*adapter.Frame, len(p.strays))
	copy(out, p.strays)
	return out
}

// close shuts stdin and waits for run() to return, which is the §14-shaped half of the
// harness: an adapter that cannot stop cleanly is a defect even when every reply was fine.
func (p *pipeAdapter) close(t *testing.T) {
	t.Helper()
	if err := p.in.Close(); err != nil {
		t.Errorf("close stdin: %v", err)
	}
	p.closed = true
	p.drained = true
	select {
	case err := <-p.done:
		if err != nil {
			t.Errorf("run() returned an error after stdin closed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return within 5s of stdin closing")
	}
}

// initialize performs §2's handshake, which every other method is gated behind.
func initialize(t *testing.T, p *pipeAdapter) *adapter.Frame {
	t.Helper()
	reply := p.call(t, "init", adapter.MethodInitialize, adapter.InitializeParams{
		Protocol: adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}})
	if reply.Error != nil {
		t.Fatalf("adapter/initialize: %s", reply.Error.Message)
	}
	return reply
}

func testConfig(dbPath, modelsPath string) config {
	return config{dbPath: dbPath, modelsPath: modelsPath, now: func() int64 { return 1790900000000 }}
}

func writeFile(path string, raw []byte) error { return os.WriteFile(path, raw, 0o600) }

// writeCatalogue drops a catalogue in the test's own temp dir. The shape is the one the
// profile recorded (§④: keyed by provider id, each carrying a models object with
// limit/cost/reasoning metadata).
func writeCatalogue(t *testing.T, providers int) string {
	t.Helper()
	top := map[string]any{}
	for i := 0; i < providers; i++ {
		pid := fmt.Sprintf("prov-%d", i)
		top[pid] = map[string]any{
			"id":   pid,
			"name": "Provider " + pid,
			"models": map[string]any{
				fmt.Sprintf("model-%d", i): map[string]any{
					"id":           fmt.Sprintf("model-%d", i),
					"name":         "Model " + pid,
					"limit":        map[string]any{"context": 128000, "output": 8000},
					"cost":         map[string]any{"input": 0.3},
					"release_date": "2026-08-01",
				},
			},
		}
	}
	raw, err := json.Marshal(top)
	if err != nil {
		t.Fatalf("marshal catalogue: %v", err)
	}
	path := filepath.Join(t.TempDir(), "models.json")
	if err := writeFile(path, raw); err != nil {
		t.Fatalf("write catalogue: %v", err)
	}
	return path
}
