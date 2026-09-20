package adapter

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// CarriesTurnRef is §6.7.s scope, stated once so the producer, the consumer and every
// Adapter in the tree agree on it: the ticket belongs to the four notifications §6.s
// table defines, namely stream/delta, stream/item, stream/state and turn/finished.
// turn/claimed and turn/dispatched are absent because CO-8 defines them with an
// execution_id to attribute themselves by and no ticket at all.
func CarriesTurnRef(method string) bool {
	switch method {
	case MethodStreamDelta, MethodStreamItem, MethodStreamState, MethodTurnFinished:
		return true
	}
	return false
}

// Stream notification methods (contract §6). Only MethodTurnFinished is
// mandatory: §6.1 exists because Phase 0 measured Qoder's 真流式 as UNSUPPORTED,
// so the protocol must tolerate a Provider that only speaks in turns.
const (
	MethodStreamDelta  = "stream/delta"
	MethodStreamItem   = "stream/item"
	MethodStreamState  = "stream/state"
	MethodTurnClaimed  = "turn/claimed"
	MethodTurnDispatch = "turn/dispatched"
	MethodTurnFinished = "turn/finished"
)

// stream/delta channels (§6).
const (
	ChannelAssistant = "ASSISTANT"
	ChannelThought   = "THOUGHT"
	ChannelTool      = "TOOL"
	ChannelStatus    = "STATUS"
)

var streamChannels = []string{ChannelAssistant, ChannelThought, ChannelTool, ChannelStatus}

// StreamFrame is one decoded stream notification. Every §6.1 notification carries
// SequenceNo; together with TurnRef it is the idempotency key (§6 rule 2), and
// TurnRef is what makes a frame attributable when one connection carries several
// executions at once (§6.7).
type StreamFrame struct {
	Method     string `json:"-"`
	SequenceNo int64  `json:"sequence_no"`
	// TurnRef is Core's own correlation string - the JSON-RPC id of the request that
	// started the turn - echoed back verbatim. The Adapter neither generates nor
	// interprets it, and no Core-side identity travels outward in a frame (§6.7).
	TurnRef string `json:"turn_ref"`

	Channel         string          `json:"channel,omitempty"`
	Text            string          `json:"text,omitempty"`
	NormalizedState string          `json:"normalized_status,omitempty"`
	StateRaw        json.RawMessage `json:"provider_status_raw,omitempty"`
	Confidence      string          `json:"confidence,omitempty"`
	ResultCode      string          `json:"result_code,omitempty"`

	// The §6.4 evidence pair. turn/claimed says the Adapter took the job,
	// turn/dispatched says a turn actually started; without the second one Core may
	// not show "running".
	Accepted            bool   `json:"accepted,omitempty"`
	QueuePosition       *int64 `json:"queue_position,omitempty"`
	TurnID              string `json:"turn_id,omitempty"`
	ProviderExecutionID string `json:"provider_execution_id,omitempty"`
	// ExecutionID rides only on turn/claimed and turn/dispatched, because CO-8 names
	// it there. §6.7 keeps it off the other four notifications: it is a Core-side
	// identity, and a frame that travels to a Provider must not carry one. Every
	// frame - including these two, which §6.7 also requires - carries turn_ref, so
	// once Core resolves the ticket this field is attribution belt-and-braces, and
	// CO-24 asks 主控 whether it is still needed at all.
	ExecutionID string `json:"execution_id,omitempty"`

	// Usage and the provider payloads stay raw: their shape belongs to the
	// Provider, and §6.4 forbids Core from interpreting them.
	Usage       json.RawMessage `json:"usage,omitempty"`
	ProviderRaw json.RawMessage `json:"provider_raw,omitempty"`

	// Item is stream/item's whole params object. §6 declares no fields for it
	// (CO-11), so it passes through untouched.
	Item json.RawMessage `json:"-"`
}

// IsFinished reports the one signal §6 makes mandatory.
func (s *StreamFrame) IsFinished() bool { return s.Method == MethodTurnFinished }

// DecodeStream decodes a stream/* or turn/finished notification. It validates what
// §6 pins and refuses anything that is not a notification: a request or a reply on
// the stream channel is a protocol breach, not a frame to drop.
func DecodeStream(f *Frame) (*StreamFrame, error) {
	if f.Kind() != KindNotification {
		return nil, fmt.Errorf("%s: stream frames must be notifications, got %s", f.Method, f.Kind())
	}
	s := &StreamFrame{Method: f.Method}
	if err := json.Unmarshal(f.Params, s); err != nil {
		return nil, fmt.Errorf("%s payload: %w", f.Method, err)
	}
	// CO-6 (2026-09-19): every notification carries sequence_no, so the resume cursor
	// works even for a Provider that only speaks at turn granularity. §6.7 pairs it
	// with turn_ref: the idempotency key and the resume cursor are per turn, not per
	// connection, because one connection can carry several executions at once.
	if s.SequenceNo <= 0 {
		return nil, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("%s sequence_no %d: CO-6 requires a positive one on every stream notification",
				f.Method, s.SequenceNo)}
	}
	// §6.7: turn_ref is mandatory on the four output notifications, and on those
	// four only - turn/claimed and turn/dispatched attribute themselves by the
	// execution_id CO-8 gives them, and carry no ticket. A frame that cannot be
	// attributed is a protocol violation rather than a droppable one: guessing
	// "the only turn in flight" is the accident the rule exists to prevent.
	if s.TurnRef == "" && CarriesTurnRef(f.Method) {
		return nil, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("%s has no turn_ref (§6.7 makes it mandatory on every notification)", f.Method)}
	}
	switch f.Method {
	case MethodStreamDelta:
		if !contains(streamChannels, s.Channel) {
			return nil, &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("stream/delta channel %q is not one of %v", s.Channel, streamChannels)}
		}
	case MethodStreamItem:
		s.Item = f.Params
	case MethodStreamState:
		if s.NormalizedState == "" {
			return nil, &Error{Code: ErrInvalidParams, Message: "stream/state has no normalized_status"}
		}
		// normalized_status itself has no frozen value set in any contract (CO-16):
		// it has to line up with the Execution states, so pinning a private guess
		// here would only create a second source of truth.
		if !contains(confidences, s.Confidence) {
			return nil, &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("stream/state confidence %q is not one of %v", s.Confidence, confidences)}
		}
	case MethodTurnClaimed:
		// CO-8 (final form): turn/claimed = {turn_ref, execution_id, accepted,
		// queue_position?, provider_execution_id} and deliberately carries **no
		// turn_id** - "the Adapter took the instruction" is not "a turn started", and
		// the only proof of a start is turn/dispatched. provider_execution_id belongs
		// on claimed precisely because Core may die in the claimed-but-not-dispatched
		// window: Recovery queries the Provider by it before judging the turn UNKNOWN.
		// These two frames are where execution_id may appear at all: §6.7 keeps the
		// Core-side identity off the other four, whose attribution is turn_ref alone.
		if s.ExecutionID == "" {
			return nil, &Error{Code: ErrInvalidParams, Message: "turn/claimed has no execution_id (CO-8)"}
		}
		if s.ProviderExecutionID == "" {
			return nil, &Error{Code: ErrInvalidParams,
				Message: "turn/claimed has no provider_execution_id; Recovery cannot query a run it cannot name (CO-8)"}
		}
	case MethodTurnDispatch:
		// CO-8's second signal is the only proof a turn started. What is mandatory is
		// the frame plus the two ids Core has to correlate on; provider_execution_id
		// may be empty (CO-13), which must never be read back as "never ran".
		if s.TurnID == "" {
			return nil, &Error{Code: ErrInvalidParams, Message: "turn/dispatched has no turn_id (CO-8)"}
		}
		if s.ExecutionID == "" {
			return nil, &Error{Code: ErrInvalidParams, Message: "turn/dispatched has no execution_id (CO-8)"}
		}
		if s.ProviderExecutionID == "" {
			return nil, &Error{Code: ErrInvalidParams,
				Message: "turn/dispatched has no provider_execution_id (§6.4 makes it part of the evidence)"}
		}
	case MethodTurnFinished:
		if s.ResultCode == "" {
			return nil, &Error{Code: ErrInvalidParams, Message: "turn/finished has no result_code"}
		}
	default:
		return nil, &Error{Code: ErrMethodNotFound, Message: f.Method + " is not a stream notification"}
	}
	return s, nil
}

// NewStream builds a stream notification for the Adapter side of the protocol.
// requestID is the frame's own UUIDv7 (§1), not the id of the call that started the
// turn.
func NewStream(requestID string, s *StreamFrame) (*Frame, error) {
	if s.SequenceNo <= 0 {
		return nil, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("%s: CO-6 requires sequence_no >= 1, got %d", s.Method, s.SequenceNo)}
	}
	// The producer is checked as strictly as the consumer, through the same
	// predicate, so the two sides cannot disagree about which frames owe a ticket.
	if s.TurnRef == "" && CarriesTurnRef(s.Method) {
		return nil, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("%s: §6.7 requires a turn_ref on its four notifications", s.Method)}
	}
	var params any = s
	if s.Method == MethodStreamItem {
		// stream/item's body is opaque, so the envelope fields are injected into it
		// rather than wrapped, which would move the Provider's fields a second time.
		var body map[string]json.RawMessage
		if err := json.Unmarshal(s.Item, &body); err != nil {
			return nil, fmt.Errorf("stream/item body %s: %w", s.Item, err)
		}
		body["sequence_no"] = json.RawMessage(fmt.Sprintf("%d", s.SequenceNo))
		body["turn_ref"] = json.RawMessage(strconv.Quote(s.TurnRef))
		params = body
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("%s params: %w", s.Method, err)
	}
	return &Frame{JSONRPC: JSONRPCVersion, Method: s.Method, Params: raw, RequestID: requestID}, nil
}

// Turn states (§6.3 and CO-8). Neither CLAIMED nor RUNNING is a terminal verdict,
// and UNKNOWN is not a failure: §6.3 forbids reading "no turn/finished signal" as
// SUCCEEDED or FAILED, while CO-8 makes the pair turn/claimed + turn/dispatched the
// only evidence a turn exists - missing either one counts as no evidence at all.
const (
	TurnClaimed = "CLAIMED"
	TurnRunning = "RUNNING"
	TurnClosed  = "CLOSED"
	TurnUnknown = "UNKNOWN"
)

// TurnTracker holds one in-flight turn's §6.4 evidence. Core keeps one per
// Execution, keyed like the §6.1 cursor (execution_id).
//
// The methods return an error whenever the evidence is unsound and leave the state
// UNKNOWN in that case, so a caller that ignores the error still cannot render a
// definite status. A late turn/finished cannot rewrite UNKNOWN by itself: the frame
// proves the Adapter said something, not what happened while it was dead, so
// Recovery decides with lease/heartbeat/provider_execution_id evidence and calls
// Recover (event contract §5.5, §9.4 "不得把 UNKNOWN 猜测成确定状态").
type TurnTracker struct {
	state       string
	turnID      string
	executionID string
	// providerExecutionID is the Provider's own handle, from turn/dispatched: the
	// key Recovery uses to reconnect instead of starting a second run (§8).
	providerExecutionID string
	result              string
}

// State is TurnClaimed, TurnRunning, TurnClosed, TurnUnknown, or "" when no turn
// was ever claimed.
func (t *TurnTracker) State() string { return t.state }

// TurnID stays empty until turn/dispatched: §6.4 keeps turn_id off turn/claimed,
// so a turn id is proof a turn started and nothing less.
func (t *TurnTracker) TurnID() string { return t.turnID }

// ExecutionID is the Core-side execution id the evidence pair is keyed by (CO-8).
func (t *TurnTracker) ExecutionID() string { return t.executionID }

// ProviderExecutionID is the Provider's handle, set by turn/dispatched; Recovery
// uses it to reconnect instead of starting a second run (§8).
func (t *TurnTracker) ProviderExecutionID() string { return t.providerExecutionID }

// Result is the result_code that closed the turn, empty until then.
func (t *TurnTracker) Result() string { return t.result }

// Claim records turn/claimed. accepted=false is a refusal: no turn opens, which is
// still an answer rather than the silence §6.3 treats as unevidenced. Both ids stay
// on the tracker even then, because that is how Recovery looks the run up (CO-8).
func (t *TurnTracker) Claim(accepted bool, executionID, providerExecutionID string) error {
	if executionID == "" || providerExecutionID == "" {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("turn/claimed needs execution_id and provider_execution_id, got %q / %q (CO-8)",
				executionID, providerExecutionID)}
	}
	if t.state != "" && t.state != TurnClosed {
		return &Error{Code: ErrInternal,
			Message: fmt.Sprintf("turn/claimed(accepted=%v) arrived while the turn is %s (CO-8)", accepted, t.state)}
	}
	t.executionID, t.providerExecutionID = executionID, providerExecutionID
	if !accepted {
		return nil
	}
	t.state, t.result = TurnClaimed, ""
	return nil
}

// Dispatch records turn/dispatched, the only signal that may put a turn in RUNNING
// (§7.1: 没有 turn/dispatched 就不得显示「运行中」).
func (t *TurnTracker) Dispatch(turnID, executionID, providerExecutionID string) error {
	if turnID == "" || executionID == "" || providerExecutionID == "" {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("turn/dispatched needs turn_id, execution_id and provider_execution_id, got %q / %q / %q (CO-8)",
				turnID, executionID, providerExecutionID)}
	}
	if t.state != TurnClaimed {
		// A dispatch with no claim, or a second dispatch, has no counterpart: §6.4
		// calls that no evidence, so the turn goes UNKNOWN rather than RUNNING.
		from := t.state
		t.state, t.executionID, t.providerExecutionID = TurnUnknown, executionID, providerExecutionID
		return &Error{Code: ErrInternal,
			Message: fmt.Sprintf("turn/dispatched(%s) arrived with the turn in %q: CO-8 needs turn/claimed too", turnID, from)}
	}
	if t.executionID != executionID {
		// Attributing a dispatch to the wrong Execution is the same class of accident
		// as delivering to the wrong Session (§12), so it never becomes RUNNING.
		t.state = TurnUnknown
		return &Error{Code: ErrInternal,
			Message: fmt.Sprintf("turn/dispatched names execution %q but the claimed turn is %q (CO-8)",
				executionID, t.executionID)}
	}
	t.state, t.turnID, t.providerExecutionID = TurnRunning, turnID, providerExecutionID
	return nil
}

// Finish closes the turn on turn/finished, the only signal §6 makes mandatory - but
// §6.4 still requires the dispatch evidence behind it.
func (t *TurnTracker) Finish(result string) error {
	if result == "" {
		return &Error{Code: ErrInvalidParams, Message: "turn/finished with no result_code (§6)"}
	}
	switch t.state {
	case TurnRunning:
		t.state, t.result = TurnClosed, result
		return nil
	case TurnClaimed, "":
		// It claims to have finished without ever being dispatched, so the pair is
		// incomplete and this is no evidence of what happened.
		t.state = TurnUnknown
		return &Error{Code: ErrInternal,
			Message: fmt.Sprintf("turn/finished(%q) without turn/dispatched: §6.4 leaves it UNKNOWN", result)}
	case TurnUnknown:
		return &Error{Code: ErrInternal,
			Message: fmt.Sprintf("turn %q is UNKNOWN; turn/finished(%q) alone is not evidence to make it definite (§6.3)",
				t.turnID, result)}
	}
	if t.result == result {
		return nil // the same signal twice is §6.2's duplicate, not a contradiction
	}
	return &Error{Code: ErrInternal,
		Message: fmt.Sprintf("turn %q already closed as %q, now claims %q (§6.3)", t.turnID, t.result, result)}
}

// ProcessRestarted marks a turn that never closed as UNKNOWN. It reports true when
// the caller has work to do: hand the turn to Recovery and, per §8, leave every
// other Adapter's turns alone.
func (t *TurnTracker) ProcessRestarted() bool {
	if t.state != TurnClaimed && t.state != TurnRunning {
		return false
	}
	t.state = TurnUnknown
	return true
}

// Recover is Recovery's exit from UNKNOWN and needs the evidenced result.
func (t *TurnTracker) Recover(result string) error {
	if t.state != TurnUnknown {
		return &Error{Code: ErrInternal,
			Message: fmt.Sprintf("Recover(%q) on a turn in state %q, want UNKNOWN", result, t.state)}
	}
	if result == "" {
		return &Error{Code: ErrInvalidParams, Message: "Recover needs the evidenced result_code"}
	}
	t.state, t.result = TurnClosed, result
	return nil
}

// Cursor applies §6.2 to one Adapter process run: sequence_no increases
// monotonically, a frame at or below the cursor is dropped idempotently, and a
// jump records the first unseen number so the caller can request since_seq after a
// reconnect. §6.7 makes the key (turn_ref, sequence_no) rather than sequence_no
// alone, so one connection carrying two executions keeps one cursor each.
type Cursor struct {
	last int64
	hole int64
}

// Last is the highest sequence_no accepted so far.
func (c *Cursor) Last() int64 { return c.last }

// SinceSeq returns the first sequence_no that never arrived, or 0 when the stream
// has been contiguous.
func (c *Cursor) SinceSeq() int64 { return c.hole }

// Reset moves the cursor to a known sequence_no. Two callers need it: the
// reconnect backfill (§6.2), and every process (re)start - a fresh Adapter instance
// numbers from 1 again, so a stale cursor would drop its whole stream as
// duplicates. internal/process must call Reset when it restarts an Adapter.
func (c *Cursor) Reset(at int64) { c.last, c.hole = at, 0 }

// Accept reports whether the frame should be forwarded upstream. The first frame
// sets the baseline wherever it lands: a gap before it is not a loss, since nothing
// was subscribed to miss. A zero sequence_no cannot happen through DecodeStream
// (§6.1), so it only reaches here from a caller bug, and forwarding beats silently
// truncating a stream.
//
// ponytail: "at or below last is a duplicate" assumes in-order emission, which is
// what §6.2 promises. If a real Adapter delivers out of order, keep the last N
// sequence numbers instead of one watermark.
func (c *Cursor) Accept(seq int64) bool {
	if seq == 0 {
		return true
	}
	if seq <= c.last {
		return false
	}
	if c.last > 0 && seq > c.last+1 && c.hole == 0 {
		c.hole = c.last + 1
	}
	c.last = seq
	return true
}
