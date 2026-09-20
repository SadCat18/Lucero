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

// ErrConnectionLost means the child process went away while a call was in flight.
// It is deliberately not a contract error code: whether the turn succeeded is
// unknown at this point, and §8 sends the Execution to UNKNOWN on that basis.
var ErrConnectionLost = errors.New("adapter process is gone; in-flight state is unknown")

// Inbound is one stream notification that survived §6.2's dedup. Err is set when the
// frame broke an evidence rule (§6.3/CO-8): the frame is still handed over, because
// swallowing it would hide exactly the violation Core needs to record.
type Inbound struct {
	Frame *adapter.StreamFrame
	Err   error
}

// Connection is one supervised Adapter process plus the protocol pump over its
// frames: requests out with a UUIDv7 request_id and an optional deadline, replies
// matched back by id, stream notifications deduplicated and forwarded.
//
// One goroutine reads the child's stdout, started by Initialize rather than by
// Connect: reading before the handshake is done would let frames pass the cursor
// untouched, and CO-6/A-5 require the cursor to be reset after the handshake and
// before the first frame.
type Connection struct {
	child *Child
	w     *adapter.Writer
	r     *adapter.Reader
	sup   *Supervisor

	// coreVersion goes into the initialize params; it is a string because §2 says so.
	coreVersion string

	mu      sync.Mutex
	hs      *adapter.Handshake
	pending map[string]chan *adapter.Frame
	turns   map[string]*adapter.TurnTracker
	cursor  adapter.Cursor
	inbound chan Inbound
	dead    chan struct{}
	dieOnce sync.Once
	closed  bool
}

// Connect starts the child and prepares the pump. Nothing is read or written until
// Initialize completes, so a child that dies before the handshake is reported by
// the Supervisor's events, not as a protocol failure.
func (s *Supervisor) Connect(ctx context.Context, coreVersion string) (*Connection, error) {
	child, err := s.Start(ctx)
	if err != nil {
		return nil, err
	}
	return &Connection{
		child: child, w: adapter.NewWriter(child.Stdin), r: adapter.NewReader(child.Stdout),
		sup: s, coreVersion: coreVersion,
		pending: map[string]chan *adapter.Frame{},
		turns:   map[string]*adapter.TurnTracker{},
		inbound: make(chan Inbound, 64),
		dead:    make(chan struct{}),
	}, nil
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
	f, err := c.request(ctx, adapter.MethodInitialize, params)
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
	c.cursor.Reset(0) // CO-6/A-5: after the handshake, before the first frame
	c.mu.Unlock()
	go c.read()
	return hs, nil
}

// Call sends a request and waits for its reply, honouring ctx as §1's deadline_at.
// Every method goes through the handshake gate first, so an un-initialised
// connection answers PROTOCOL_NOT_INITIALIZED without bothering the child.
func (c *Connection) Call(ctx context.Context, method string, params any) (*adapter.Frame, error) {
	if err := c.gate(method); err != nil {
		return nil, err
	}
	return c.request(ctx, method, params)
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
	defer c.mu.Unlock()
	if c.closed {
		return ErrConnectionLost
	}
	return c.w.Write(f)
}

// Inbound receives deduplicated stream notifications. It blocks the pump when full:
// dropping a delta would lose output with no trace, and back-pressure at least shows
// up as a stalled Adapter.
func (c *Connection) Inbound() <-chan Inbound { return c.inbound }

// Turn returns the §6.3/CO-8 evidence tracker for one Execution, creating it on
// first sight. Core keeps these per Execution because the resume cursor is the pair
// (execution_id, sequence_no).
func (c *Connection) Turn(executionID string) *adapter.TurnTracker {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turnLocked(executionID)
}

// LastSeq reports how far the inbound stream has been accepted, which is what a
// reconnect sends as since_seq (§6.2). CO-21 asks whether stream/delta should carry
// execution_id: until it does, the counter is per Adapter process, which is what
// §6.2 literally states, and whoever persists chunks pairs it with the execution.
func (c *Connection) LastSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cursor.Last()
}

// MissingSeq returns the first sequence_no that never arrived after a drop, or 0
// when the stream has been contiguous.
func (c *Connection) MissingSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cursor.SinceSeq()
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
func (c *Connection) request(ctx context.Context, method string, params any) (*adapter.Frame, error) {
	raw, err := marshal(params)
	if err != nil {
		return nil, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: method + " params: " + err.Error()}
	}
	id := string(adapter.NewRequestID())
	f := &adapter.Frame{JSONRPC: adapter.JSONRPCVersion, ID: id, Method: method,
		Params: raw, RequestID: id}
	if d, ok := ctx.Deadline(); ok {
		f.DeadlineAt = d.UnixMilli()
	}
	ch := make(chan *adapter.Frame, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrConnectionLost
	}
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.w.Write(f); err != nil {
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
		return nil, fmt.Errorf("%s: %w", method, ErrConnectionLost)
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

// notify applies §6.2's idempotent drop and drives the per-Execution evidence
// trackers, then forwards whatever is left.
func (c *Connection) notify(f *adapter.Frame) {
	s, err := adapter.DecodeStream(f)
	if err != nil {
		c.deliver(Inbound{Err: err})
		return
	}
	c.mu.Lock()
	keep := c.cursor.Accept(s.SequenceNo)
	var tr *adapter.TurnTracker
	if s.ExecutionID != "" {
		tr = c.turnLocked(s.ExecutionID)
	}
	c.mu.Unlock()
	if !keep {
		return // duplicate or stale: dropped by sequence_no, not re-delivered
	}
	if tr != nil {
		if err := applyTurn(tr, s); err != nil {
			c.deliver(Inbound{Frame: s, Err: err})
			return
		}
	}
	c.deliver(Inbound{Frame: s})
}

// applyTurn feeds one decoded notification into the tracker. Errors are returned
// rather than swallowed: an evidence rule broken by the Adapter is the caller's to
// record, and it is the only way §6.3's "不得改判" can be enforced anywhere.
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
// and every turn that had not closed is moved to UNKNOWN. That second half is §6.3 -
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
	for id, ch := range c.pending {
		ch <- &adapter.Frame{JSONRPC: adapter.JSONRPCVersion, ID: id, RequestID: id,
			Error: &adapter.Error{Code: adapter.ErrInternal,
				Message: "connection lost: " + cause.Error()}}
		delete(c.pending, id)
	}
	for execID, tr := range c.turns {
		if !tr.ProcessRestarted() {
			continue
		}
		// Non-blocking on purpose: a full inbound channel must not deadlock the death
		// path that holds the lock. The tracker itself keeps the verdict (UNKNOWN),
		// which is the durable part a caller can still read after draining.
		msg := fmt.Sprintf("execution %s turn %s is UNKNOWN: the process died before turn/finished (§6.3)",
			execID, tr.TurnID())
		select {
		case c.inbound <- Inbound{Err: &adapter.Error{Code: adapter.ErrInternal, Message: msg}}:
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
