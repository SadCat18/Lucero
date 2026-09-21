package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// lostError is the one shape every caller of a dead connection gets, whatever the
// race. The §15.1 code ADAPTER_UNRESPONSIVE is named in the text because that is the
// condition, but it is never put on the wire from here: a reply needs a peer request
// to answer, and inventing one would be Core speaking for an Adapter.
func lostError(method, why string) error {
	return &LostError{Method: method, Reason: why,
		Err: fmt.Errorf("%w: %s: %s (%s)", ErrConnectionLost, method, why,
			adapter.ErrAdapterUnresponsive)}
}

// LostError keeps both halves machine-readable: errors.Is against ErrConnectionLost,
// and errors.As against it for the method that was in flight.
type LostError struct {
	Method string
	Reason string
	Err    error
}

func (e *LostError) Error() string { return e.Err.Error() }
func (e *LostError) Unwrap() error { return e.Err }

// ErrConnectionLost means the child process went away while a call was in flight.
// It is deliberately not a contract error code: whether the turn succeeded is
// unknown at this point, and §8 sends the Execution to UNKNOWN on that basis.
var ErrConnectionLost = errors.New("adapter process is gone; in-flight state is unknown")

// Inbound is one stream notification that survived §6 规则 2's dedup, already attributed.
// ExecutionID is §6 规则 5's ticket resolved to the key a consumer actually needs
// (execution_output_chunks is UNIQUE on (execution_id, sequence_no)). Frame arrives
// with TurnRef emptied: C20 keeps the ticket inside internal/process, so chunk flush
// and idempotent-drop consumers never see it and cannot route by it. Err is set when
// the frame broke an evidence rule (CO-8/CO-8), and the frame still comes through -
// swallowing it would hide exactly the violation Core needs to record.
type Inbound struct {
	ExecutionID string
	Frame       *adapter.StreamFrame
	Err         error
}

// Connection is one supervised Adapter process plus the protocol pump over its
// frames: requests out with a UUIDv7 request_id and an optional deadline, replies
// matched back by id, stream notifications deduplicated and forwarded.
//
// One goroutine reads the child's stdout from the moment Connect returns, before the
// handshake: stdout pipes are unbuffered, so a reader that waits for
// Initialize would let an Adapter that talks first block on its own write and the
// handshake would never complete. Frames that arrive that early are reported as §2
// violations and never move the cursor, which Initialize resets (CO-6/A-5).
type Connection struct {
	child *Child
	w     *adapter.Writer
	r     *adapter.Reader
	sup   *Supervisor

	// coreVersion goes into the initialize params; it is a string because §2 says so.
	coreVersion string

	mu      sync.Mutex
	writeMu sync.Mutex // one stdout stream, many callers: frames must not interleave
	hs      *adapter.Handshake
	pending map[string]chan *adapter.Frame
	turns   map[string]*adapter.TurnTracker
	// cursor is the connection-level watermark and hole tracker: §6 规则 5 states plainly
	// that sequence_no is connection-level, and since_seq is one number per connection.
	cursor adapter.Cursor
	// lastByRef is §6 规则 5's idempotency key (turn_ref, sequence_no). A frame is a
	// duplicate only of the same turn's same number - gating on the watermark alone
	// would drop a second execution's lower-numbered frame as a "duplicate" of the
	// first one's higher one, which is the accident the key exists to stop.
	lastByRef map[string]int64
	// execByRef is the §6 规则 5 turn_ref -> execution_id map, valid only for this
	// connection. turn/claimed and turn/dispatched are what populate it (CO-8 lets
	// those two frames name the execution), and every other frame is attributed
	// through it.
	execByRef map[string]string
	inbound   chan Inbound
	dead      chan struct{}
	dieOnce   sync.Once
	closed    bool
}

// Connect starts the child and the pump. The reader must be running before any reply
// can arrive - and on a pipe, nothing is buffered, so a pump that starts only after
// the handshake would deadlock on the handshake reply itself. Frames that arrive
// before the handshake are reported as §2 violations instead of moving the cursor.
func (s *Supervisor) Connect(ctx context.Context, coreVersion string) (*Connection, error) {
	child, err := s.Start(ctx)
	if err != nil {
		return nil, err
	}
	conn := &Connection{
		child: child, w: adapter.NewWriter(child.Stdin), r: adapter.NewReader(child.Stdout),
		sup: s, coreVersion: coreVersion,
		pending:   map[string]chan *adapter.Frame{},
		turns:     map[string]*adapter.TurnTracker{},
		lastByRef: map[string]int64{},
		execByRef: map[string]string{},
		inbound:   make(chan Inbound, 64),
		dead:      make(chan struct{}),
	}
	go conn.read()
	return conn, nil
}

// Child exposes the pipes and pid, so a caller can attach the stderr sink and
// register the process row (§14) with the real pid.
func (c *Connection) Child() *Child { return c.child }

// Handshake is the verified capability table, or nil before Initialize succeeds.
func (c *Connection) Handshake() *adapter.Handshake {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hs
}

// Initialize performs the §2 handshake and only then starts reading, which is where
// the cursor reset belongs.
func (c *Connection) Initialize(ctx context.Context) (*adapter.Handshake, error) {
	c.mu.Lock()
	if c.hs != nil {
		hs := c.hs
		c.mu.Unlock()
		return hs, nil
	}
	c.mu.Unlock()

	params := adapter.InitializeParams{
		Protocol:    adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor},
		CoreVersion: c.coreVersion,
	}
	f, err := c.request(ctx, adapter.MethodInitialize, params, "")
	if err != nil {
		return nil, err
	}
	var res adapter.InitializeResult
	if err := decodeResult(f, &res); err != nil {
		return nil, err
	}
	hs, err := adapter.NewHandshake(params.Protocol, res)
	if err != nil {
		return nil, fmt.Errorf("adapter refused by the handshake rules: %w", err)
	}
	c.mu.Lock()
	c.hs = hs
	// CO-6/A-5: the cursor starts counting from the handshake, so anything that
	// arrived beforehand is not part of this session's sequence. A restarted process
	// gets a whole new Connection, which is the same reset by construction.
	c.cursor.Reset(0)
	c.lastByRef = map[string]int64{}
	c.execByRef = map[string]string{}
	c.mu.Unlock()
	return hs, nil
}

// Call sends one request and waits for its reply, honouring ctx as §1's deadline_at.
// Every method goes through the handshake gate first, so an un-initialised
// connection answers PROTOCOL_NOT_INITIALIZED without bothering the child.
func (c *Connection) Call(ctx context.Context, method string, params any) (*adapter.Frame, error) {
	if err := c.gate(method); err != nil {
		return nil, err
	}
	return c.request(ctx, method, params, "")
}

// CallTurn is the form a caller uses when the call starts a turn: it binds the
// request's JSON-RPC id - which is what §6 规则 5 says comes back as turn_ref - to the
// Execution it belongs to, before the frame goes out. Without that binding there is
// no way to attribute a stream/delta or close the right tracker on a turn/finished,
// because §6 规则 5 keeps Core's identity off those frames and the CO-8 pair names no
// ticket. The mapping lives exactly as long as this connection does.
func (c *Connection) CallTurn(ctx context.Context, executionID, method string, params any) (*adapter.Frame, error) {
	if err := c.gate(method); err != nil {
		return nil, err
	}
	if executionID == "" {
		return nil, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "CallTurn needs the Execution this call starts; §6 规则 5 attributes frames by that binding"}
	}
	return c.request(ctx, method, params, executionID)
}

// gate applies §2's not-initialised rule and §3's write_entry rule.
func (c *Connection) gate(method string) error {
	c.mu.Lock()
	hs := c.hs
	c.mu.Unlock()
	return hs.Call(method)
}

// Cancel sends $/cancel for an in-flight request id (§1). The Adapter must move the
// Execution to CANCELLED or INTERRUPTED and answer; swallowing it silently is the P0
// this method exists to make impossible to forget.
func (c *Connection) Cancel(targetID string) error {
	if targetID == "" {
		return &adapter.Error{Code: adapter.ErrInvalidParams, Message: "$/cancel needs the id to cancel"}
	}
	f := &adapter.Frame{JSONRPC: adapter.JSONRPCVersion, Method: adapter.CancelMethod,
		RequestID: string(adapter.NewRequestID())}
	raw, err := marshal(map[string]string{"id": targetID})
	if err != nil {
		return err
	}
	f.Params = raw
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrConnectionLost
	}
	return c.write(f)
}

// write serialises frame writes: Call and Cancel run on different goroutines and a
// half-written line would break the peer's stream (§1: one object per line).
func (c *Connection) write(f *adapter.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.w.Write(f)
}

// Inbound receives deduplicated stream notifications. It blocks the pump when full:
// dropping a delta would lose output with no trace, and back-pressure at least shows
// up as a stalled Adapter.
func (c *Connection) Inbound() <-chan Inbound { return c.inbound }

// Turn returns the CO-8/CO-8 evidence tracker for one Execution, creating it on
// first sight. The tracker stays keyed by Core's own id: turn_ref is how frames
// attribute themselves (§6 规则 5), and this map is where one becomes the other.
//
// The pump is the only writer: callers read State/TurnID/ProviderExecutionID and hand
// an UNKNOWN turn to Recovery, which closes it through Recover. Claiming from
// somewhere else would race with the frames.
func (c *Connection) Turn(executionID string) *adapter.TurnTracker {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turnLocked(executionID)
}

// LastSeq is the connection-level watermark, which is what a reconnect's since_seq
// is measured against: §6 规则 5 states plainly that sequence_no is monotonic per Adapter
// process, not per turn.
func (c *Connection) LastSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cursor.Last()
}

// MissingSeq returns the first sequence_no that never arrived after a jump, or 0
// while the connection's numbering has been contiguous.
func (c *Connection) MissingSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cursor.SinceSeq()
}

// SinceSeq is the per-turn resume number §6 规则 5 asks Core to keep: the highest
// sequence_no accepted for that turn_ref, or 0 when nothing has arrived for it.
// Independent of any other execution's cursor, which is the point of the turn_ref.
func (c *Connection) SinceSeq(turnRef string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastByRef[turnRef]
}

// Close stops the pump and releases the pipes. It does not kill the child: shutting
// an Adapter down is Supervisor.Stop's job, because that is where §14's STOPPING
// transition belongs.
func (c *Connection) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.kill(errors.New("connection closed"))
	c.child.CloseStreams()
	return nil
}

// request writes one frame and waits, and is the only place that knows a call has an
// id, a request_id and an optional deadline.
func (c *Connection) request(ctx context.Context, method string, params any, executionID string) (*adapter.Frame, error) {
	raw, err := marshal(params)
	if err != nil {
		return nil, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: method + " params: " + err.Error()}
	}
	id := string(adapter.NewRequestID())
	if executionID != "" {
		c.mu.Lock()
		c.execByRef[id] = executionID
		c.mu.Unlock()
	}
	f := &adapter.Frame{JSONRPC: adapter.JSONRPCVersion, ID: id, Method: method,
		Params: raw, RequestID: id}
	if d, ok := ctx.Deadline(); ok {
		f.DeadlineAt = d.UnixMilli()
	}
	ch := make(chan *adapter.Frame, 1)

	c.mu.Lock()
	select {
	case <-c.dead:
		c.mu.Unlock()
		return nil, lostError(method, "connection already gone")
	default:
	}
	if c.closed {
		c.mu.Unlock()
		return nil, lostError(method, "connection closed")
	}
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(f); err != nil {
		c.forget(id)
		return nil, fmt.Errorf("send %s: %w", method, err)
	}
	select {
	case reply := <-ch:
		c.forget(id)
		return reply, nil
	case <-ctx.Done():
		// §1: give up by cancelling, so the Adapter moves the Execution instead of
		// leaving it running with nobody watching.
		if cerr := c.Cancel(id); cerr != nil {
			return nil, errors.Join(ctx.Err(), cerr)
		}
		c.forget(id)
		return nil, fmt.Errorf("%s: %w (sent %s)", method, ctx.Err(), adapter.CancelMethod)
	case <-c.dead:
		c.forget(id)
		return nil, lostError(method, "no reply")
	}
}

func (c *Connection) forget(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// turnLocked returns the tracker for one Execution. Callers must hold c.mu.
func (c *Connection) turnLocked(executionID string) *adapter.TurnTracker {
	tr, ok := c.turns[executionID]
	if !ok {
		tr = &adapter.TurnTracker{}
		c.turns[executionID] = tr
	}
	return tr
}

// read is the pump: replies to pending calls, stream notifications to Inbound.
func (c *Connection) read() {
	for {
		f, err := c.r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				c.kill(errors.New("adapter closed stdout"))
			} else {
				c.kill(err)
			}
			return
		}
		switch f.Kind() {
		case adapter.KindResponse:
			// T-45: a code outside §15.1 degrades to INTERNAL with the original
			// number preserved, so no caller can branch on a meaning Core invented.
			f.Error = f.Error.Degrade()
			c.mu.Lock()
			ch := c.pending[f.ID]
			c.mu.Unlock()
			if ch != nil {
				ch <- f
			}
		case adapter.KindNotification:
			c.notify(f)
		default:
			// An Adapter sending us a request is outside §4; answering would need a
			// server side we do not have, so it surfaces as a dropped-but-logged frame.
			c.deliver(Inbound{Err: &adapter.Error{Code: adapter.ErrMethodNotFound,
				Message: fmt.Sprintf("adapter sent %s as a request; Core serves no methods", f.Method)}})
		}
	}
}

// notify applies §6 规则 2's idempotent drop and drives the per-Execution evidence
// trackers, then forwards whatever is left.
func (c *Connection) notify(f *adapter.Frame) {
	c.mu.Lock()
	initialized := c.hs != nil
	c.mu.Unlock()
	if !initialized {
		// §2: no traffic belongs on the stream before the handshake, and accepting it
		// would let an Adapter pre-seed the dedup cursor for a session nobody started.
		c.deliver(Inbound{Err: &adapter.Error{Code: adapter.ErrProtocolNotInitialized,
			Message: f.Method + " arrived before adapter/initialize completed (§2)"}})
		return
	}
	s, err := adapter.DecodeStream(f)
	if err != nil {
		c.deliver(Inbound{Err: err})
		return
	}
	c.mu.Lock()
	execID, why := c.attributeLocked(s)
	if why != nil {
		c.mu.Unlock()
		// Unknown turn_ref: dropped and reported, and it advances nothing. Recording
		// a watermark for a ticket this connection never issued would let a stranger's
		// numbers shadow the resume position of a real turn. This is also the one
		// place the frame does not travel onward with its error attached: a lossy
		// stream is survivable, a crossed one writes two executions into one row.
		c.deliver(Inbound{Err: why})
		return
	}
	// Every frame that gets this far carries a ticket (§4.2 裁定 ① made it mandatory on
	// all six), so the dedup key is the ticket itself - no second keying scheme for the
	// evidence pair to fall back on.
	ref := s.TurnRef
	// §6 规则 5's idempotency key is (turn_ref, sequence_no), so that pair and not the
	// connection's watermark decides whether a frame repeats one. Gating on the
	// watermark alone would drop a second execution's lower-numbered frame as a
	// "duplicate" of the first execution's higher one - the exact accident the key
	// exists to prevent.
	fresh := s.SequenceNo > c.lastByRef[ref]
	if fresh {
		c.lastByRef[ref] = s.SequenceNo
	}
	// The watermark stays connection-level, because sequence_no is: since_seq and the
	// hole it reports are one number per Adapter process.
	c.cursor.Accept(s.SequenceNo)
	c.mu.Unlock()
	if !fresh {
		return // duplicate of (turn_ref, sequence_no): dropped, not re-delivered
	}
	tr := c.Turn(execID)
	if err := applyTurn(tr, s); err != nil {
		c.deliver(Inbound{ExecutionID: execID, Frame: s, Err: err})
		return
	}
	// The ticket has done its job: it stops here.
	s.TurnRef = ""
	c.deliver(Inbound{ExecutionID: execID, Frame: s})
}

// attributeLocked resolves a frame to Core's own Execution id. There is exactly one
// route: the ticket, looked up in the turn_ref -> execution_id binding CallTurn wrote
// when the request went out (§6 规则 5, and §4.2 裁定 ① extended it to all six
// notifications). An execution_id the Adapter brings on the evidence pair is a
// cross-checkable redundancy, never a second truth source, so nothing in here writes
// to the map - a frame that contradicts the binding is dropped instead of believed.
// The second result is why the frame must be dropped rather than forwarded.
func (c *Connection) attributeLocked(s *adapter.StreamFrame) (string, *adapter.Error) {
	execID, ok := c.execByRef[s.TurnRef]
	if !ok {
		return "", &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: fmt.Sprintf("%s carries turn_ref %q this connection never bound; §6 规则 5 forbids attributing it to the turn in flight",
				s.Method, s.TurnRef)}
	}
	if s.ExecutionID != "" && s.ExecutionID != execID {
		return "", &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: fmt.Sprintf("%s names execution %q but its turn_ref %q is bound to %q; §4.2 裁定 ① cross-checks the two rather than picking one",
				s.Method, s.ExecutionID, s.TurnRef, execID)}
	}
	return execID, nil
}

// applyTurn feeds one decoded notification into the tracker. Errors are returned
// rather than swallowed: an evidence rule broken by the Adapter is the caller's to
// record, and it is the only way CO-8's "不得改判" can be enforced anywhere.
func applyTurn(tr *adapter.TurnTracker, s *adapter.StreamFrame) error {
	switch s.Method {
	case adapter.MethodTurnClaimed:
		return tr.Claim(s.Accepted, s.ExecutionID, s.ProviderExecutionID)
	case adapter.MethodTurnDispatch:
		return tr.Dispatch(s.TurnID, s.ExecutionID, s.ProviderExecutionID)
	case adapter.MethodTurnFinished:
		return tr.Finish(s.ResultCode)
	}
	return nil
}

// deliver pushes an event, unblocking if the connection dies mid-send.
func (c *Connection) deliver(ev Inbound) {
	select {
	case c.inbound <- ev:
	case <-c.dead:
	}
}

// kill ends the pump once: every waiter is failed instead of left until its deadline,
// and every turn that had not closed is moved to UNKNOWN. That second half is CO-8 -
// the process dying is not evidence that the turn failed, so nothing here may call it
// FAILED or SUCCEEDED; Recovery decides with lease/heartbeat/provider evidence.
func (c *Connection) kill(cause error) {
	lost := false
	c.dieOnce.Do(func() {
		close(c.dead)
		lost = true
	})
	if !lost {
		return
	}
	c.mu.Lock()
	// Waiters are left in place on purpose: each Call is already selecting on c.dead,
	// so closing it gives every caller the same result. Synthesising reply frames
	// instead made the outcome depend on a race - a call registered before the death
	// got a fabricated frame with err==nil, one after got ErrConnectionLost.
	for execID, tr := range c.turns {
		if !tr.ProcessRestarted() {
			continue
		}
		// Non-blocking on purpose: a full inbound channel must not deadlock the death
		// path that holds the lock. The tracker itself keeps the verdict (UNKNOWN),
		// which is the durable part a caller can still read after draining.
		msg := fmt.Sprintf("execution %s turn %s is UNKNOWN: the process died before turn/finished (CO-8)",
			execID, tr.TurnID())
		select {
		case c.inbound <- Inbound{Err: &adapter.Error{Code: adapter.ErrAdapterUnresponsive, Message: msg}}:
		default:
		}
	}
	c.mu.Unlock()
}

func marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

// decodeResult turns a reply into either its error or its result, so a caller never
// has to remember that a frame can carry both shapes.
func decodeResult(f *adapter.Frame, target any) error {
	if f.Error != nil {
		return f.Error
	}
	if len(f.Result) == 0 {
		return &adapter.Error{Code: adapter.ErrInternal,
			Message: "reply carries neither result nor error"}
	}
	return json.Unmarshal(f.Result, target)
}
