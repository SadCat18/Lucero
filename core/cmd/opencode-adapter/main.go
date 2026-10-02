// Command opencode-adapter is the READ_ONLY tier of contract §16.7 for OpenCode
// Desktop 1.18.32: it observes that product's own persistent store and answers the
// read methods §16.7 第 1 条 names. It sends nothing to OpenCode - no message, no
// interrupt, no approval reply - because the user chose option (c) on 10-02 and the
// ruling closes the range with "除此之外一条命令都不发".
//
// Three facts about this package are load-bearing, so they are written down before
// any code:
//
//   - §1: stdout carries protocol frames only. Every diagnostic, including which path
//     was opened and how many rows were read, goes to stderr.
//   - the tier is expressed by the *declaration*, not by a state: write_entry:false
//     plus capabilities at support_level READ_ONLY (§16.7 第 3 条 forbids inventing a
//     trigger_type, and core/test/read_only_tier_test.go looks for exactly this
//     literal when it decides which packages to scan).
//   - the Provider's own `seq` never leaves this process. It orders journal rows
//     inside one aggregate and nothing more: §16.2 bans reusing it, because our
//     idempotency key is (turn_ref, sequence_no) and their seq is only unique per
//     aggregate. See read.go.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// adapterVersion is this adapter's own semver. The observed product version travels
// separately in software_versions (§2.1 requires a map, never a single string).
const adapterVersion = "0.1.0"

// adapterKey must match the config entry and the probe's, because adapter_instances
// is keyed on it. It is "opencode", never "fake": §9 reserves debug.* for the fake.
const adapterKey = "opencode"

// The read methods §16.7 第 1 条 lists as this tier's whole surface, plus the two
// read-side §4 rows that need an explicit answer rather than a not-found. They stay
// literals here (not new constants in internal/adapter) because Core does not call
// them yet - the rule fake-adapter's header records from T-45.
const (
	methodSessionList = "session/list"
	methodSessionRead = "session/read"
	methodModelList   = "model/list"
	methodQuotaRead   = "quota/read"
	methodSessionStop = "session/stop"
	// methodSessionSend is named here rather than inline because the test that proves
	// §3's gate refuses a delivery mode needs the method, and §16.7's list is data: it
	// belongs in this const block next to the others, not in a call argument.
	methodSessionSend  = "session/send"
	methodSessionLocat = "session/locate"
	methodApprovalList = "approval/list"
)

// readTimeout bounds one store query. §1/§15.1 give a caller no way to wait forever
// on an Adapter, and a 674 MB file with a live writer can take longer than a page
// render; a TIMEOUT error frame is honest, silence is not.
const readTimeout = 10 * time.Second

func main() {
	if err := run(os.Stdin, os.Stdout, os.Stderr, envConfig()); err != nil {
		fmt.Fprintf(os.Stderr, "opencode-adapter: %v\n", err)
		os.Exit(1)
	}
}

// config is the whole outside world this adapter may touch: two paths, an observed
// version and a clock. No HTTP client appears here at all - §16.7's range and
// core/test/read_only_tier_test.go's criterion ① both forbid constructing a write
// request, and the service port is unreachable by 401 besides.
type config struct {
	dbPath     string
	modelsPath string
	version    string
	now        func() int64
}

// envConfig keeps machine-specific paths out of the product code. The defaults are the
// junction the installer made (%USERPROFILE%\.local\share\opencode -> D:\opencode\data,
// profile §③/E8); overriding them is an operator decision, not something this binary
// may do by editing a third party's config.
func envConfig() config {
	base := filepath.Join(os.Getenv("USERPROFILE"), ".local", "share", "opencode")
	if p := os.Getenv("LUCERO_OPENCODE_HOME"); p != "" {
		base = p
	}
	c := config{
		dbPath:     filepath.Join(base, "opencode.db"),
		modelsPath: filepath.Join(filepath.Dir(base), "cache", "models.json"),
		version:    os.Getenv("LUCERO_OPENCODE_VERSION"),
		now:        func() int64 { return time.Now().UnixMilli() },
	}
	if p := os.Getenv("LUCERO_OPENCODE_MODELS"); p != "" {
		c.modelsPath = p
	}
	return c
}

// opencode is one process run: the §2 handshake, the read-only store handle, and the
// sequence numbers this adapter maintains for the segments it synthesizes.
type opencode struct {
	r    *adapter.Reader
	w    *adapter.Writer
	logs io.Writer
	cfg  config

	hs *adapter.Handshake // nil until initialize succeeds: that is §2's gate
	db *sqlStore
}

func run(in io.Reader, out io.Writer, logs io.Writer, cfg config) error {
	o := &opencode{r: adapter.NewReader(in), w: adapter.NewWriter(out), logs: logs, cfg: cfg}
	fmt.Fprintf(logs, "opencode-adapter up, pid=%d, tier=READ_ONLY\n", os.Getpid())

	// Check our own declaration against the validator Core uses, before serving. A
	// table Core would reject must not be discovered by the second initialize:
	// fake-adapter's round-31 lesson is that the caller then waits on a reply that
	// never comes.
	if _, err := adapter.NewHandshake(coreVersionAssumed, initializeResult(cfg.observedVersion())); err != nil {
		return fmt.Errorf("Core would reject our own capability table: %w", err)
	}

	// The read-only handle is released when the session ends rather than at process exit:
	// §14 says a stop gives back what it opened, and on Windows a test's temp directory
	// cannot be removed at all while a SQLite handle is still live.
	defer func() {
		if o.db != nil {
			_ = o.db.close()
		}
	}()

	for {
		req, err := o.r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// §1: an unparseable frame has no id to answer to, so the session ends
			// rather than this process guessing what was meant.
			return err
		}
		if err := o.serve(req); err != nil {
			return err
		}
	}
}

// serve answers exactly one request. This tier emits no notifications at all: every
// frame it writes is a response to a request Core issued, because a synthesized A->C
// frame would have no Core-issued turn_ref to echo (§4.2 裁定 ①; §4.0 withdrew
// session/push for that same dead knot, and the ruling for T-447 put the synthesized
// deltas inside session/read's result instead). TestReadOnlySendsNoNotifications
// asserts it on the wire rather than documenting it.
func (o *opencode) serve(req *adapter.Frame) error {
	if req.Kind() != adapter.KindRequest {
		fmt.Fprintf(o.logs, "ignoring %s frame %q\n", req.Kind(), req.Method)
		return nil
	}

	if req.Method == adapter.MethodInitialize {
		return o.initialize(req)
	}
	if o.hs == nil {
		return o.reply(adapter.NewErrorReply(req.ID, req.RequestID, adapter.ErrProtocolNotInitialized,
			req.Method+": adapter/initialize has not completed (contract §2)"))
	}
	// §2.2 negotiates at handshake and never re-negotiates at runtime, so a second
	// initialize is refused with the same code Core's own gate uses.
	if err := o.hs.Call(req.Method); err != nil {
		// This single call is §3's write_entry gate: for a method in §4.1's write set
		// it answers WRITE_ENTRY_NONE, the registered exit §16.7 第 2 条 demands,
		// using the same table Core applies. Refusing it a second way would be a
		// second vocabulary for one fact.
		return o.reply(errReply(req, err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	switch req.Method {
	case adapter.MethodHealth:
		return o.health(req)
	case methodSessionList:
		return o.sessionList(ctx, req)
	case methodSessionRead:
		return o.sessionRead(ctx, req)
	case methodModelList:
		return o.modelList(req)
	case methodQuotaRead:
		return o.quotaRead(ctx, req)
	case methodSessionLocat:
		// §4 keeps this a v0.1 stub (Phase 0 measured no verified deep link for any of
		// the five products) and its own row says an unavailable answer is the correct
		// implementation.
		return o.reply(errReply(req, &adapter.Error{Code: adapter.ErrCapabilityUnavailable,
			Message: "session/locate is a v0.1 stub (§4): no verified deep link for " + adapterKey}))
	case methodApprovalList:
		// Read-side, so it is answered rather than refused - and the honest answer is
		// empty: the provider's permission table measured 0 rows and approvals never
		// reach its journal (profile §③). Inventing a row here would be §16.7 第 5 条's
		// "造假数据凑绿".
		return o.reply(result(req, map[string]any{"approvals": []any{}}))
	}

	if refusedMethods[req.Method] {
		return o.reply(errReply(req, refusedWrite(req.Method)))
	}
	return o.reply(errReply(req, &adapter.Error{Code: adapter.ErrMethodNotFound,
		Message: req.Method + " is not a method of this build"}))
}

// initialize answers §2 and installs the handshake the rest of the session is gated
// on. A foreign major is refused, never guessed at (§2.2).
func (o *opencode) initialize(req *adapter.Frame) error {
	var p adapter.InitializeParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return o.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "adapter/initialize params do not decode: " + truncate(err.Error())}))
	}
	if p.Protocol.Major != adapter.ProtocolMajor {
		return o.reply(errReply(req, &adapter.Error{Code: adapter.ErrProtocolVersionMismatch,
			Message: fmt.Sprintf("Core speaks protocol %d.%d, this adapter speaks %d.%d (§2.2 refuses, never guesses)",
				p.Protocol.Major, p.Protocol.Minor, adapter.ProtocolMajor, adapter.ProtocolMinor)}))
	}
	res := initializeResult(o.cfg.observedVersion())
	if err := o.reply(result(req, res)); err != nil {
		return err
	}
	hs, err := adapter.NewHandshake(p.Protocol, res)
	if err != nil {
		// Unreachable in practice: run() validated the same table at startup. Reaching
		// it means the table changed mid-process, so this process stops rather than
		// serving under a declaration nobody checked.
		return fmt.Errorf("handshake after reply: %w", err)
	}
	o.hs = hs
	return nil
}

// ensureStore opens the observed database lazily and once. Lazy because Core probes
// adapter/initialize before anyone has asked for a session, and an adapter whose file
// is missing must still answer the handshake - then say clearly which read failed.
func (o *opencode) ensureStore() (*sqlStore, error) {
	if o.db != nil {
		return o.db, nil
	}
	s, err := openReadOnlyStore(o.cfg.dbPath)
	if err != nil {
		return nil, err
	}
	o.db = s
	s.log = o.logs
	fmt.Fprintf(o.logs, "store open read-only: %s (journal rows for last read: see next lines)\n", o.cfg.dbPath)
	return s, nil
}

// observedVersion is the product version this adapter reports and dispatches on. Empty
// means "the operator did not say", and the declaration then falls back to the version
// the profile actually measured - never to a guess about a newer build.
func (c config) observedVersion() string {
	if c.version != "" {
		return c.version
	}
	return observedSoftwareVersionDefault
}

func (o *opencode) reply(f *adapter.Frame) error { return o.w.Write(f) }

// errReply converts any error into a §15.1-shaped frame. A non-adapter.Error is
// reported as INTERNAL with the text kept, because §15.3 rule 1.1 reserves the
// protocol-layer codes for protocol faults only.
func errReply(req *adapter.Frame, err error) *adapter.Frame {
	code, message := adapter.ErrInternal, err.Error()
	var e *adapter.Error
	if errors.As(err, &e) {
		code, message = e.Code, e.Message
	}
	return adapter.NewErrorReply(req.ID, req.RequestID, code, message)
}

func result(req *adapter.Frame, v any) *adapter.Frame {
	f, err := adapter.NewResponse(req.ID, req.RequestID, v)
	if err != nil {
		// Marshalling our own types fails only on a programming bug, and a nil frame
		// would be written as nothing at all: surface it instead.
		return adapter.NewErrorReply(req.ID, req.RequestID, adapter.ErrInternal,
			"marshal reply for "+req.Method+": "+err.Error())
	}
	return f
}

func badParams(method string, err error) error {
	return &adapter.Error{Code: adapter.ErrInvalidParams,
		Message: method + " params do not decode: " + truncate(err.Error())}
}

// truncate keeps a Provider's own error text from flooding stderr, and a newline out
// of a log line that a test greps for.
func truncate(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
