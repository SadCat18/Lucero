// Command fake-adapter is the FakeAdapter of contract §10: a real subprocess that
// speaks the Core<->Adapter protocol over stdio and injects the failures Core must
// survive. It shares internal/adapter with Core on purpose - §10 rejected a second
// module because two copies of the codec would drift and make the contract test
// meaningless, and because only a real process can prove §8's "an Adapter crash must
// not take Core down".
//
// Contract §1 is the rule this file exists to obey: stdout carries protocol frames
// only, every diagnostic goes to stderr. A log line on stdout breaks the stream,
// which §1 calls a P0 implementation defect.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// errCrash unwinds run() so the caller can exit with the status §10 pins for
// debug/crash. run() does not call os.Exit itself so the same code stays testable.
var errCrash = errors.New("fake-adapter: debug/crash requested")

const adapterVersion = "0.1.0"

// Methods §10 requires of this build. They stay literals rather than constants in
// internal/adapter because a name Core does not call yet has no reason to exist
// there twice (T-45).
const (
	methodSessionSend  = "session/send"
	methodQuotaRead    = "quota/read"
	methodDebugFail    = "debug/fail"
	methodDebugHang    = "debug/hang"
	methodDebugHold    = "debug/hold"
	methodDebugNoQuota = "debug/unknown_quota"
)

func main() {
	if err := run(os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, errCrash) {
			os.Exit(2) // §10: debug/crash exits with status 2
		}
		fmt.Fprintf(os.Stderr, "fake-adapter: %v\n", err)
		os.Exit(1)
	}
}

// fake is one process run: the handshake, the §6 规则 2 sequence numbers, the §5 turn
// state and whatever debug/* has armed. Everything the protocol remembers lives
// here, so nothing is hidden in package globals a second test could trip over.
type fake struct {
	r    *adapter.Reader
	w    *adapter.Writer
	logs io.Writer
	hs   *adapter.Handshake // nil until initialize succeeds: that is §2's gate

	seq  int64 // §6 规则 2 sequence_no, monotonic for the life of the process
	open *turn // the turn a guide can steer into, nil when none
	// queued holds §5 queue sends waiting for the open turn to close.
	queued     []*turn
	position   int64
	sent       map[string]adapter.SendResult // §4 Idempotency-Key -> first answer
	fail       *injection                    // armed debug/fail
	quotaBlind bool                          // armed debug/unknown_quota
	// hold is armed debug/hold: while it is on, an opened turn does not close at the end
	// of its own output (§6 第 6 条③'s mandatory signal then arrives when the hold is
	// released, not when the send returns). T-386 ① is what licenses this endpoint.
	hold bool
	// db is the fake's whole world: sessions, their logs and the approvals a turn
	// raises. service.go explains why it is compiled in rather than read from disk.
	db *store
	// badExecID is read once from the environment at startup (envBadExecutionID). It
	// changes only what the two CO-8 evidence frames say; see T-275's three limits.
	badExecID bool
}

// turn is one CO-8 evidence pair. ref is §6 规则 5's turn_ref: the JSON-RPC id Core
// put on the send that started this turn, echoed back verbatim on every frame the
// turn produces. executionID is the Core-side id the pair names (CO-8), and the
// per-turn seq is why §6 规则 5 can call the cursors independent - see reportable.
type turn struct {
	ref         string
	sessionID   string
	executionID string
	turnID      string
	providerID  string
}

// injection is an armed debug/fail: the error code and how many calls are left
// before it fires (§10 allows "the next call" or "the Nth").
type injection struct {
	code adapter.ErrorCode
	left int
}

// envBadExecutionID is T-275's black-box injection door: with it set to "1", the two CO-8
// evidence frames name an execution_id that is **not** the one Core bound to that ticket,
// so a test outside this process can finally produce the §4.2 裁定 ① contradiction (today
// the fake only echoes what Core gave it, and the binding is Core-side state no outsider
// can bend). Three limits, all of them load-bearing: it lives in this double and nowhere
// in product code; off (unset or any other value) the frames are byte-identical to before;
// and only the **value** changes - §4.2's frame keys, ordering and decode rules do not, so
// this is a fixture switch, not a new capability, and it adds no §10 debug/* method.
const envBadExecutionID = "LUCERO_FAKE_ADAPTER_BAD_EXECUTION_ID"

// conflictingExecutionID is what the door puts in the frames. The shape does not matter to
// the protocol (§4.2 only requires a non-empty execution_id), so this one is deliberately
// id-shaped: an audit row that later has to explain itself reads better with a value that
// could have been real than with "WRONG".
const conflictingExecutionID = "0194f4c2-7c3e-7fff-8000-000000000bad"

// resultSucceeded is the turn-level code this double puts on the wire when a run ends on
// its own. §6 第 6 条 keeps **one** table - the Execution state constants in
// internal/execution - and aligns producers by name, so this is a name aligned to that
// table, not a second enumeration: the fake must not import a Core domain package to say
// "this run succeeded", and `"OK"` (what it used to send) is not in that table.
const resultSucceeded = "SUCCEEDED"

// run serves the protocol until stdin closes. logs receives everything that is not a
// frame, so a caller can keep stdout and stderr apart the way Core's supervisor does.
func run(in io.Reader, out io.Writer, logs io.Writer) error {
	f := &fake{r: adapter.NewReader(in), w: adapter.NewWriter(out), logs: logs,
		sent: map[string]adapter.SendResult{}, db: newStore(nowMillis()),
		badExecID: os.Getenv(envBadExecutionID) == "1"}
	fmt.Fprintf(logs, "fake-adapter up, pid=%d\n", os.Getpid())
	// Check the self-declaration up front, against the same validator Core uses.
	// Doing it per reply instead (as this file used to) turned a second
	// adapter/initialize into an undecodable reply plus a dead process: the caller
	// then waits for a response that will never come.
	if _, err := selfHandshake(); err != nil {
		return fmt.Errorf("Core would reject our own capability table: %w", err)
	}
	for {
		req, err := f.r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// A frame we cannot parse has no id to answer to, so the session ends
			// rather than us guessing what was meant (§1).
			return err
		}
		if err := f.serve(req); err != nil {
			return err
		}
	}
}

// serve answers one request. Reading is allowed inside it (debug/hang does) because
// the fake has one goroutine and a hang is expected to be ended by $/cancel, which
// can only arrive by reading.
func (f *fake) serve(req *adapter.Frame) error {
	if req.Kind() != adapter.KindRequest {
		if req.Method == adapter.CancelMethod {
			// Nothing was in flight, so there is nothing to acknowledge. Saying so on
			// stderr is the alternative to swallowing it (§15.3 gives a cancel no
			// error reply to return when no request is pending).
			fmt.Fprintf(f.logs, "$/cancel for %q ignored: nothing is in flight\n", cancelTarget(req))
			return nil
		}
		fmt.Fprintf(f.logs, "ignoring %s frame %q\n", req.Kind(), req.Method)
		return nil
	}
	if err := f.hs.Call(req.Method); err != nil {
		// The nil handshake is the un-initialised state, so this is §2's
		// PROTOCOL_NOT_INITIALIZED gate and §3's write_entry gate at once.
		return f.reply(errReply(req, err))
	}
	if e := f.injected(); e != nil {
		return f.reply(errReply(req, e))
	}
	switch req.Method {
	case adapter.MethodInitialize:
		if err := f.reply(result(req, initializeResult())); err != nil {
			return err
		}
		f.hs = declaredHandshake()
		return nil
	case adapter.MethodHealth:
		return f.reply(result(req, adapter.HealthResult{
			Health: adapter.HealthHealthy, Confidence: adapter.ConfidenceHigh, CheckedAt: nowMillis(),
		}))
	case adapter.MethodDebugCrash:
		if err := f.reply(result(req, map[string]any{"crashing": true})); err != nil {
			return err
		}
		fmt.Fprintf(f.logs, "crashing on demand\n")
		return errCrash
	case methodDebugFail:
		return f.debugFail(req)
	case methodDebugHang:
		return f.debugHang(req)
	case methodDebugHold:
		return f.debugHold(req)
	case methodDebugNoQuota:
		f.quotaBlind = true
		return f.reply(result(req, map[string]any{"quota_unknown": true}))
	case methodQuotaRead:
		return f.quotaRead(req)
	case methodSessionSend:
		return f.sessionSend(req)
	case methodSessionList:
		return f.sessionList(req)
	case methodSessionRead:
		return f.sessionRead(req)
	case methodSessionStop:
		return f.sessionStop(req)
	case methodSessionLocate:
		return f.sessionLocate(req)
	case methodModelList:
		return f.modelList(req)
	case methodModelSelect:
		return f.modelSelect(req)
	case methodSessionCreate:
		return f.sessionCreate(req)
	case methodApprovalList:
		return f.approvalList(req)
	case methodApprovalUpdate:
		return f.approvalRespond(req)
	default:
		// Anything this build does not implement is refused with INTERNAL rather than
		// a contract refusal, so "not built yet" can never be mistaken for a
		// Provider-side decision.
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInternal,
			Message: "fake-adapter: " + req.Method + " is not implemented in this build"}))
	}
}

// injected consumes one armed debug/fail and reports the error it asks for, or nil
// when nothing is armed or the call count has not been reached yet.
func (f *fake) injected() *adapter.Error {
	if f.fail == nil {
		return nil
	}
	f.fail.left--
	if f.fail.left > 0 {
		return nil
	}
	code := f.fail.code
	f.fail = nil
	return &adapter.Error{Code: code, Message: "fake-adapter: failure injected by debug/fail (§10)"}
}

func (f *fake) debugFail(req *adapter.Frame) error {
	var p struct {
		Code string `json:"code"`
		Nth  int    `json:"nth,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return f.reply(errReply(req, badParams(req.Method, err)))
	}
	code, ok := adapter.CodeByName(p.Code)
	if !ok {
		// A number instead of a name is how a test asks for a code outside §15.1:
		// Core must degrade it to INTERNAL and keep the original visible.
		n, err := strconv.Atoi(p.Code)
		if err != nil || n >= 0 {
			return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
				Message: fmt.Sprintf("debug/fail code %q is neither a §15.1 name nor a negative number", p.Code)}))
		}
		code = adapter.ErrorCode(n)
	}
	nth := p.Nth
	if nth < 1 {
		nth = 1 // §10's default: the next call
	}
	f.fail = &injection{code: code, left: nth}
	return f.reply(result(req, map[string]any{"armed": true, "code": p.Code, "nth": nth}))
}

// debugHang stops answering, which is how §10's deadline case is produced. The
// request is only released by a $/cancel naming it; §15.3 rule 5 makes that
// acknowledgement a result frame, never an error reply, so CANCELLED keeps its
// number off the wire.
func (f *fake) debugHang(req *adapter.Frame) error {
	fmt.Fprintf(f.logs, "hanging on %s until $/cancel names it\n", req.ID)
	for {
		fr, err := f.r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if fr.Kind() != adapter.KindNotification || fr.Method != adapter.CancelMethod {
			fmt.Fprintf(f.logs, "ignoring %s frame %q while hanging\n", fr.Kind(), fr.Method)
			continue
		}
		if got := cancelTarget(fr); got != req.ID {
			fmt.Fprintf(f.logs, "$/cancel names %q, not the hanging %q\n", got, req.ID)
			continue
		}
		return f.reply(result(req, map[string]bool{"cancelled": true}))
	}
}

// debugHold arms or releases §10's fifth injection (T-386 ① licenses it, and only its
// existence here). Held on: an opened turn does not close at the end of its own output,
// so a test can still put two executions on one connection for real (§6 规则 5's
// acceptance item), steer a guide into the turn in flight (§5), or leave a queue waiting
// behind a running turn (§5's position). Held off: the turn open at that moment ends
// there, which is where its closing frame and CO-25's one drain belong.
//
// `on` is mandatory rather than defaulting to true: a forgotten arm would make the double
// look like an adapter that never finishes, and that is the accident this file exists to
// keep unproducible.
func (f *fake) debugHold(req *adapter.Frame) error {
	var p struct {
		On *bool `json:"on"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return f.reply(errReply(req, badParams(req.Method, err)))
	}
	if p.On == nil {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "debug/hold needs an explicit on: true|false"}))
	}
	if *p.On {
		f.hold = true
		fmt.Fprintf(f.logs, "debug/hold on: the next turn/finished arrives when the hold is released\n")
		return f.reply(result(req, map[string]any{"armed": true}))
	}
	f.hold = false
	open := f.open
	if open != nil {
		if err := f.finish(open, resultSucceeded); err != nil {
			return err
		}
	}
	fmt.Fprintf(f.logs, "debug/hold off, closed the open turn: %v\n", open != nil)
	return f.reply(result(req, map[string]any{"armed": false, "closed": open != nil}))
}

// quotaRead answers §7. The self-Validate is the same rule selfHandshake applies to
// the capability table: an illegal reply is this process's bug, and it must surface
// as one instead of reaching Core as a surprise.
func (f *fake) quotaRead(req *adapter.Frame) error {
	res := adapter.UnknownQuota()
	if !f.quotaBlind {
		res = observedQuota()
	}
	if err := res.Validate(); err != nil {
		fmt.Fprintf(f.logs, "our own quota reply breaks §7: %v\n", err)
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInternal,
			Message: "fake-adapter: quota/read reply is illegal: " + err.Error()}))
	}
	return f.reply(result(req, res))
}

// sessionSend is §5's tri-state, §4's mandatory Idempotency-Key and CO-8's two-phase
// evidence in one place.
func (f *fake) sessionSend(req *adapter.Frame) error {
	var p adapter.SendParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return f.reply(errReply(req, badParams(req.Method, err)))
	}
	// SendParams.Validate already carries §5's and CO-7's refusals with their codes,
	// so honouring it is what makes the fake reject an unknown state rather than
	// quietly picking one it likes.
	if err := p.Validate(); err != nil {
		return f.reply(errReply(req, err))
	}
	c, ok := f.hs.Effective("session.send")
	if !ok || !containsMode(c.DeliveryModes, p.Delivery) {
		// §5's red line: an unsupported state is named, never silently downgraded.
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrDeliveryModeUnsupported,
			Message: fmt.Sprintf("fake-adapter declares %v and was asked for %s (§5)", c.DeliveryModes, p.Delivery)}))
	}
	if hit, again := f.sent[p.IdempotencyKey]; again {
		hit.IdempotencyHit = true
		return f.reply(result(req, hit))
	}
	res, err := f.accept(req.ID, p)
	if err != nil {
		return f.reply(errReply(req, &adapter.Error{Code: adapter.ErrInternal,
			Message: "fake-adapter: " + err.Error()}))
	}
	if vErr := res.Validate(p.Delivery); vErr != nil {
		fmt.Fprintf(f.logs, "our own %s reply is illegal: %v\n", p.Delivery, vErr)
		return f.reply(errReply(req, vErr))
	}
	if err := f.reply(result(req, res)); err != nil {
		return err
	}
	f.sent[p.IdempotencyKey] = res
	return nil
}

// accept is the part that changes state, and returns the reply only once the frames
// that evidence it are on the wire: CO-7 forbids reporting a delivery the fake did
// not actually open.
func (f *fake) accept(ref string, p adapter.SendParams) (adapter.SendResult, error) {
	switch p.Delivery {
	case adapter.DeliveryQueue:
		f.position++
		pos := f.position
		f.queued = append(f.queued, &turn{ref: ref, executionID: p.ExecutionID,
			providerID: fmt.Sprintf("fake-q-%d", pos)})
		// No turn_id here: CO-7 lets a queued reply claim only "queued + position".
		return adapter.SendResult{Accepted: true,
			Reason:        fmt.Sprintf("queued behind the open turn (%s)", p.Strategy()),
			QueuePosition: &pos}, nil

	case adapter.DeliveryGuide:
		if f.open != nil {
			// Steering: the open turn keeps its evidence pair and gets another delta.
			if err := f.delta(f.open, p.Text); err != nil {
				return adapter.SendResult{}, err
			}
			return adapter.SendResult{Accepted: true, Reason: "steered into the open turn (§5 guide)",
				TurnID: f.open.turnID, ProviderExecutionID: f.open.providerID}, nil
		}
		// §5 makes guide "an interruption of the turn in flight"; with none, the only
		// honest readings are a refusal or a new turn. The fake reports which it did,
		// because a reply that silently opens a turn is §5's other half of the accident.
		t, err := f.startTurn(ref, p)
		if err != nil {
			return adapter.SendResult{}, err
		}
		res := started(t)
		res.Reason = "no turn was in flight, so guide opened one (§5)"
		return res, nil

	default: // start_now
		t, err := f.startTurn(ref, p)
		if err != nil {
			return adapter.SendResult{}, err
		}
		return started(t), nil
	}
}

func started(t *turn) adapter.SendResult {
	return adapter.SendResult{Accepted: true, TurnID: t.turnID, ProviderExecutionID: t.providerID}
}

// startTurn opens the new turn with CO-8's pair and interrupts the one in flight, in
// that order: a start_now preempts (§5), and the preempted run still owes output
// after the new one has started. That is what makes two executions share a
// connection for real instead of in a comment, which §6 规则 5's FakeAdapter acceptance
// asks for. Without an execution_id from Core there is nothing for CO-8's pair to
// name, so no evidence frames go out - and CO-8 leaves that Execution UNKNOWN, which
// is the honest outcome rather than the fake inventing an id.
func (f *fake) startTurn(ref string, p adapter.SendParams) (*turn, error) {
	prev := f.open
	t := &turn{ref: ref, sessionID: p.RemoteSessionID, executionID: p.ExecutionID,
		turnID: adapter.NewRequestID().String()}
	t.providerID = "fake-" + t.turnID
	f.open = t
	if err := f.evidence(t, p.Text); err != nil {
		return nil, err
	}
	if prev != nil {
		if err := f.notice(prev, &adapter.StreamFrame{Method: adapter.MethodStreamDelta,
			Channel: adapter.ChannelStatus, Text: "interrupted by a start_now (§5)"}); err != nil {
			return nil, err
		}
		// §6 第 6 条②: a preempted run gets **no** turn-level code. "被打断" is Core's
		// observation about a process that died (Occasion=Recovery, REQ-RC-1); self-reporting
		// it from this side is what `finish(prev, "INTERRUPTED")` used to do, and it would
		// make the crash-recovery cell unmeasurable.
		//
		// The drain stays, though, and that split is deliberate: CO-25 pins the queue's one
		// drain point at "a turn ended", not at "a closing frame went out". Dropping the
		// frame together with the drain strands everything queued behind the preempted run
		// (measured: the queued turn never reaches the wire at all).
		if err := f.drainQueue(); err != nil {
			return nil, err
		}
	}
	// §6 第 6 条③: evidence() above is this run's whole output, so by default the closing
	// frame goes right here - without it a gate watches a session where every turn opens
	// and none ends. debug/hold is the only thing that defers it, and releasing the hold
	// is what closes a held turn (that release is also where CO-25's drain fires).
	if !f.hold {
		if err := f.finish(t, resultSucceeded); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// evidence emits turn/claimed then turn/dispatched: "the Adapter took the job" and
// "a turn started" are two facts, and §7.1 lets Core show running only on the second.
func (f *fake) evidence(t *turn, text string) error {
	if t.executionID == "" {
		fmt.Fprintf(f.logs, "no execution_id on this send (CO-24): emitting no turn/claimed or turn/dispatched\n")
		return nil
	}
	// The gate changes this one value and nothing else: same two frames, same order,
	// same keys. It says so on the log stream, because a black-box test that trips the
	// audit seam needs to be able to show *which* id it contradicted.
	exec := t.executionID
	if f.badExecID {
		exec = conflictingExecutionID
		fmt.Fprintf(f.logs, "%s is on: evidence frames name execution_id %q while Core bound %q to ticket %s\n",
			envBadExecutionID, exec, t.executionID, t.ref)
	}
	if err := f.notice(t, &adapter.StreamFrame{Method: adapter.MethodTurnClaimed,
		ExecutionID: exec, Accepted: true, ProviderExecutionID: t.providerID}); err != nil {
		return err
	}
	if err := f.notice(t, &adapter.StreamFrame{Method: adapter.MethodTurnDispatch, ExecutionID: exec,
		TurnID: t.turnID, ProviderExecutionID: t.providerID}); err != nil {
		return err
	}
	// §10 ① now requires every notification type this double can emit to reach the wire
	// at least once (§6 规则 1 still only makes turn/finished mandatory), so a turn
	// opens with claimed, dispatched, state, delta, item.
	if err := f.notice(t, &adapter.StreamFrame{Method: adapter.MethodStreamState,
		NormalizedState: "RUNNING", Confidence: adapter.ConfidenceHigh}); err != nil {
		return err
	}
	if err := f.delta(t, text); err != nil {
		return err
	}
	return f.notice(t, &adapter.StreamFrame{Method: adapter.MethodStreamItem,
		Item: json.RawMessage(`{"kind":"tool_call","name":"fake.read","state":"READY"}`)})
}

func (f *fake) delta(t *turn, text string) error {
	return f.notice(t, &adapter.StreamFrame{Method: adapter.MethodStreamDelta,
		Channel: adapter.ChannelAssistant, Text: "echo: " + text})
}

// drainQueue dispatches what §5's queue holds behind the turn that just ended, oldest
// first. A queued send that ran gets the same evidence pair as one that started
// immediately, because that is the only difference between "ran" and "never left the
// queue".
//
// A drained turn closes at the end of its own run even while debug/hold is armed, and that
// is deliberate: hold defers the close of the turn the caller is holding *open* - the one
// §5's guide and queue positions are about - while CO-25's drain is observed at exactly
// these closings. Making hold swallow them would move the drain point instead of deferring
// a turn.
func (f *fake) drainQueue() error {
	drained := f.queued
	f.queued = nil
	for _, t := range drained {
		t.turnID = adapter.NewRequestID().String()
		if err := f.evidence(t, "queued prompt"); err != nil {
			return err
		}
		if err := f.finish(t, resultSucceeded); err != nil {
			return err
		}
	}
	return nil
}

// finish is §6 规则 1's one mandatory signal. It names the turn by turn_ref only:
// execution_id stays off the four output notifications (§6 规则 5, §4.2 裁定 ①).
//
// It is also CO-25's drain point (T-110, 14:46): §5 says a queued send waits "until the
// current turn ends", and this frame is the only end a Provider reports. Draining on the
// next send instead - what this file used to do - strands a queued prompt in any session
// nobody sends to again, and a stranded queue is invisible: the reply already said
// "queued, position 1", so every symptom reads as success.
func (f *fake) finish(t *turn, code string) error {
	// A closed turn is no longer something a guide can steer into: leaving f.open pointed
	// here would let a later delta land *after* the closing frame, which is the
	// misattribution §6 规则 5 exists to prevent.
	if t == f.open {
		f.open = nil
	}
	if err := f.notice(t, &adapter.StreamFrame{Method: adapter.MethodTurnFinished, ResultCode: code}); err != nil {
		return err
	}
	return f.drainQueue()
}

func (f *fake) notice(t *turn, s *adapter.StreamFrame) error {
	f.seq++
	s.SequenceNo = f.seq
	// §4.2 裁定 ① makes the ticket mandatory on all six notifications, so this is one
	// rule with no exception branch: the predicate is the same one Core decodes with,
	// which is what keeps a producer bug from hiding behind a permissive consumer.
	if adapter.CarriesTurnRef(s.Method) {
		s.TurnRef = t.ref
	}
	fr, err := adapter.NewStream(adapter.NewRequestID().String(), s)
	if err != nil {
		return err
	}
	return f.w.Write(fr)
}

func (f *fake) reply(fr *adapter.Frame) error { return f.w.Write(fr) }

// selfHandshake runs our own declaration through the validator Core uses, so an
// illegal capability table is this process's bug rather than a peer-side surprise.
func selfHandshake() (*adapter.Handshake, error) {
	return adapter.NewHandshake(
		adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}, initializeResult())
}

// declaredHandshake is the checked version of the same thing: run() validates the
// declaration before the loop, so reaching here with an error is a programming bug.
func declaredHandshake() *adapter.Handshake {
	hs, err := selfHandshake()
	if err != nil {
		panic("fake-adapter: " + err.Error())
	}
	return hs
}

func result(req *adapter.Frame, v any) *adapter.Frame {
	fr, err := adapter.NewResponse(req.ID, req.RequestID, v)
	if err != nil {
		// Marshal of our own types only fails on a programming bug, and a nil frame
		// would be written as nothing at all, so it has to be this process's failure.
		panic("fake-adapter: " + err.Error())
	}
	return fr
}

func errReply(req *adapter.Frame, err error) *adapter.Frame {
	code, message := adapter.ErrInternal, err.Error()
	if e := new(adapter.Error); errors.As(err, &e) {
		code, message = e.Code, e.Message
	}
	return adapter.NewErrorReply(req.ID, req.RequestID, code, message)
}

func badParams(method string, err error) error {
	return &adapter.Error{Code: adapter.ErrInvalidParams, Message: method + " params: " + err.Error()}
}

// cancelTarget is the id $/cancel names (contract §1). A malformed cancel cancels
// nothing, which is the safe direction.
func cancelTarget(fr *adapter.Frame) string {
	var p struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(fr.Params, &p)
	return p.ID
}

func containsMode(modes []string, want string) bool {
	for _, m := range modes {
		if m == want {
			return true
		}
	}
	return false
}

// initializeResult is the fake's self-declaration. debug.* sits at EXPERIMENTAL,
// which §9 allows only because adapter_key is "fake".
func initializeResult() adapter.InitializeResult {
	return adapter.InitializeResult{
		AdapterKey: "fake", AdapterVersion: adapterVersion,
		Protocol: adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor},
		// Two lines on purpose: §2.1 exists because real products carry 2-3.
		SoftwareVersions: map[string]string{"fake": adapterVersion, "codec": "internal/adapter"},
		AuthMode:         adapter.AuthNone, Health: adapter.HealthHealthy,
		WriteEntry: true,
		Capabilities: []adapter.Capability{
			capability("session.list", false),
			capability("session.read", false),
			capability("session.create", true),
			capability("session.send", true,
				adapter.DeliveryStartNow, adapter.DeliveryGuide, adapter.DeliveryQueue),
			capability("session.stop", true),
			capability("model.list", false),
			capability("model.select", true),
			capability("quota.read", false),
			capability("approval.list", false),
			capability("approval.respond", true),
			// §4: session/locate is a stub in v0.1 and returning CAPABILITY_UNAVAILABLE
			// is the correct implementation, so it declares itself unsupported.
			{Name: "session.locate", SupportLevel: adapter.LevelUnsupported,
				EffectiveBackend: adapter.BackendReadOnly, Confidence: adapter.ConfidenceUnknown,
				Metadata: json.RawMessage(`{"reason":"Phase 0 §5: deep links unverified for all five"}`)},
			// §9: debug.* is legal only for adapter_key=fake AND only at EXPERIMENTAL.
			debugCapability("debug.crash"), debugCapability("debug.fail"),
			debugCapability("debug.hang"), debugCapability("debug.hold"),
			debugCapability("debug.unknown_quota"),
		},
	}
}

func capability(name string, write bool, modes ...string) adapter.Capability {
	return adapter.Capability{
		Name: name, SupportLevel: adapter.LevelNative, EffectiveBackend: adapter.BackendNativeAPI,
		WriteCapable: write, DeliveryModes: modes,
		Confidence: adapter.ConfidenceHigh, LastVerifiedAt: nowMillis(),
	}
}

func debugCapability(name string) adapter.Capability {
	c := capability(name, true)
	c.SupportLevel = adapter.LevelExperimental
	return c
}

// observedQuota is §7's readable case: a real value, OBSERVED, and the used/remaining
// pair that proves it is a full window rather than a Provider's all-zero default.
func observedQuota() adapter.QuotaResult {
	used, remaining, reset := 62.5, 37.5, nowMillis()+3600_000
	return adapter.QuotaResult{
		Observations: []adapter.QuotaObservation{{
			QuotaKey: "weekly", QuotaType: "PERCENT", ObservationKind: adapter.ObservationObserved,
			UsedPercent: &used, RemainingPercent: &remaining, ResetAt: &reset,
			Confidence: adapter.ConfidenceHigh, Source: "fake/self-report",
		}},
		OverallConfidence: adapter.ConfidenceHigh,
	}
}

func nowMillis() int64 { return time.Now().UnixMilli() }
