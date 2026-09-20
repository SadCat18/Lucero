// Package adapter implements the Core<->Adapter wire protocol (Adapter Protocol
// v0.1): ndjson frames over stdio, the adapter/initialize handshake and the
// capability table.
//
// It owns no subprocess lifecycle (that is internal/process), opens no database
// connection and emits no events; contract §0 keeps those on the caller's side.
//
// Two kinds of citation live here. A plain "§n" points into
// specs/adapter-protocol/adapter-protocol-v0.1.md. Anything labelled "CO-x" is a
// decision from the orchestrator, itemised in .qoder/progress/adapter.md - check the
// contract file before quoting one onward, and use the CO label if it is not there.
package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// JSONRPCVersion is the only accepted value of the jsonrpc field.
const JSONRPCVersion = "2.0"

// CancelMethod aborts an in-flight request by its id (contract §1). It is the one
// method outside the frozen 域/动作 naming of contract §4.
const CancelMethod = "$/cancel"

// The §4 methods this repository speaks today. Only the ones with call sites are
// named here; §4's table stays the complete list, and a typo in a bare string lit
// would fail as METHOD_NOT_FOUND far from where it was written.
const (
	MethodInitialize = "adapter/initialize"
	MethodHealth     = "adapter/health"
	MethodDebugCrash = "debug/crash"
)

// ErrorCode is the numeric JSON-RPC error code on the wire (CO-4 ruled 2026-09-19:
// integer, because a standard JSON-RPC client type-checks it). The symbol stays as
// the Go-side name for logs, Audit and callers that branch; TestErrorCodeTable pins
// each pair so the two cannot drift, and pins the reserved band.
//
// CO-4's table: -32600..-32603 for the named refusals, which deliberately does not
// line up with JSON-RPC's predefined meanings for those numbers, -32001..-32049 for
// the rest, and -32050..-32099 kept free for a real Adapter's native errors. That is
// the contract owner's call: only Lucero Adapters speak this protocol, and a
// Provider's own codes are mapped by that Adapter, never by Core.
type ErrorCode int

const (
	ErrProtocolNotInitialized  ErrorCode = -32600 // CO-4
	ErrWriteEntryNone          ErrorCode = -32601 // CO-4
	ErrCapabilityUnavailable   ErrorCode = -32602 // CO-4
	ErrDeliveryModeUnsupported ErrorCode = -32603 // CO-4
	ErrInvalidParams           ErrorCode = -32001 // CO-4
	ErrMethodNotFound          ErrorCode = -32002 // CO-4
	ErrInternal                ErrorCode = -32003 // CO-4

	// -32005..-32049 is the band CO-4 leaves for later codes. These two are codes this
	// package returns that CO-4 has not numbered, so they sit at the far end: -32005
	// and -32006 are earmarked there for READ_ONLY and EXECUTION_STATE_UNKNOWN, and
	// two symbols on one number is a misread waiting to happen. READ_ONLY has no call
	// site here yet - §3 sends WRITE_ENTRY_NONE through the same door - so it is not
	// declared, and its number stays unclaimed.
	ErrAdapterUnresponsive     ErrorCode = -32004 // CO-4
	ErrProtocolVersionMismatch ErrorCode = -32048 // unnumbered
	ErrLocalDataUnavailable    ErrorCode = -32049 // unnumbered
)

// adapterCodeFirst/Last bound -32050..-32099, which CO-4 reserves and Core must
// never occupy.
const (
	adapterCodeFirst = -32099
	adapterCodeLast  = -32050
)

// codeNames is the symbol side of CO-4's table.
var codeNames = map[ErrorCode]string{
	ErrProtocolNotInitialized:  "PROTOCOL_NOT_INITIALIZED",
	ErrWriteEntryNone:          "WRITE_ENTRY_NONE",
	ErrCapabilityUnavailable:   "CAPABILITY_UNAVAILABLE",
	ErrDeliveryModeUnsupported: "DELIVERY_MODE_UNSUPPORTED",
	ErrInvalidParams:           "INVALID_PARAMS",
	ErrMethodNotFound:          "METHOD_NOT_FOUND",
	ErrInternal:                "INTERNAL",
	ErrAdapterUnresponsive:     "ADAPTER_UNRESPONSIVE",
	ErrProtocolVersionMismatch: "PROTOCOL_VERSION_MISMATCH",
	ErrLocalDataUnavailable:    "LOCAL_DATA_UNAVAILABLE",
}

// String renders the symbol; an unknown number keeps the number visible rather
// than silently renaming someone else's failure.
func (c ErrorCode) String() string {
	if n, ok := codeNames[c]; ok {
		return n
	}
	return "UNKNOWN_CODE(" + strconv.Itoa(int(c)) + ")"
}

// Error is the JSON-RPC 2.0 error object.
//
// §15.1 pins where the *name* of the failure lives: the Adapter layer speaks
// UPPER_SNAKE in `error.data.code`, while `error.code` stays a number because
// JSON-RPC says it must be one. So both travel, and Symbol() is what a peer reads:
// a code number this package does not know still reports the number instead of
// inventing a name for it.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Data    any       `json:"data,omitempty"`
}

// Symbol returns §15.1's name for the error, preferring a `data.code` the peer sent
// over the number-derived name: a real Adapter may answer with a §15 code whose
// number Core has never been given.
func (e *Error) Symbol() string {
	if m, ok := e.Data.(map[string]any); ok {
		if name, ok := m["code"].(string); ok && name != "" {
			return name
		}
	}
	return e.Code.String()
}

func (e *Error) Error() string { return e.Symbol() + ": " + e.Message }

// Frame is one ndjson line: a request, a notification or a response. They share
// one struct because JSON-RPC distinguishes them by which keys are present, and
// because decoding an unknown frame before knowing its kind is unavoidable.
type Frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`

	// RequestID is the UUIDv7 carried by every call (contract §1); it survives
	// reconnects and log correlation, where the JSON-RPC id does not.
	RequestID string `json:"request_id,omitempty"`
	// DeadlineAt is Unix milliseconds, 0 meaning absent.
	DeadlineAt int64 `json:"deadline_at,omitempty"`
}

// Kind classifies a frame the way JSON-RPC does: by present keys.
type Kind int

const (
	KindInvalid Kind = iota
	KindRequest
	KindNotification
	KindResponse
)

func (k Kind) String() string {
	switch k {
	case KindRequest:
		return "request"
	case KindNotification:
		return "notification"
	case KindResponse:
		return "response"
	}
	return "invalid"
}

func (f *Frame) Kind() Kind {
	switch {
	case f.JSONRPC != JSONRPCVersion:
		return KindInvalid
	case f.Method != "" && f.ID != "":
		return KindRequest
	case f.Method != "":
		return KindNotification
	case f.ID != "" && (f.Result != nil || f.Error != nil):
		return KindResponse
	}
	return KindInvalid
}

// Validate rejects frames the peer must never have sent. Content errors leave
// the stream positioned after the offending line, so callers may keep reading;
// only a syntax error from Reader desyncs it.
func (f *Frame) Validate() error {
	if f.JSONRPC != JSONRPCVersion {
		return fmt.Errorf("frame: jsonrpc is %q, want %q", f.JSONRPC, JSONRPCVersion)
	}
	switch k := f.Kind(); k {
	case KindRequest, KindNotification:
		if err := validateMethodName(f.Method); err != nil {
			return fmt.Errorf("frame: %w", err)
		}
		if f.RequestID == "" {
			return fmt.Errorf("frame: %s %q has no request_id (contract §1)", k, f.Method)
		}
	case KindResponse:
		if f.Result != nil && f.Error != nil {
			return fmt.Errorf("frame: response %q carries both result and error", f.ID)
		}
		if f.Error != nil && f.Error.Code == 0 {
			return fmt.Errorf("frame: response %q error has no code", f.ID)
		}
	case KindInvalid:
		return fmt.Errorf("frame: neither request, notification nor response")
	}
	return nil
}

// validateMethodName enforces contract §4's 域/动作 shape.
func validateMethodName(name string) error {
	if name == CancelMethod {
		return nil
	}
	domain, action, ok := strings.Cut(name, "/")
	if !ok || action == "" || domain != strings.ToLower(domain) || action != strings.ToLower(action) {
		return fmt.Errorf("method %q is not 域/动作 lowercase", name)
	}
	for _, r := range domain + "_" + action {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return fmt.Errorf("method %q has invalid character %q", name, r)
		}
	}
	return nil
}

// NewResponse builds a result reply, echoing the call's request_id.
func NewResponse(id, requestID string, result any) (*Frame, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal result for %q: %w", id, err)
	}
	return &Frame{JSONRPC: JSONRPCVersion, ID: id, Result: raw, RequestID: requestID}, nil
}

// NewCancel builds the $/cancel notification for an in-flight request (§1). The
// Adapter must then move that Execution to CANCELLED or INTERRUPTED; swallowing
// the cancel silently is forbidden.
func NewCancel(requestID, targetID string) (*Frame, error) {
	if targetID == "" {
		return nil, fmt.Errorf("%s needs the id of the request to cancel", CancelMethod)
	}
	raw, err := json.Marshal(map[string]string{"id": targetID})
	if err != nil {
		return nil, fmt.Errorf("%s params: %w", CancelMethod, err)
	}
	return &Frame{JSONRPC: JSONRPCVersion, Method: CancelMethod, Params: raw, RequestID: requestID}, nil
}

// NewErrorReply builds an error reply, carrying §15.1's name in data.code alongside
// the numeric error.code.
func NewErrorReply(id, requestID string, code ErrorCode, message string) *Frame {
	return &Frame{
		JSONRPC:   JSONRPCVersion,
		ID:        id,
		Error:     &Error{Code: code, Message: message, Data: map[string]any{"code": code.String()}},
		RequestID: requestID,
	}
}

// Reader decodes ndjson frames from a stream that carries frames only. Adapter
// logs arrive on a separate stderr pipe that internal/process drains (contract §1:
// a log line on stdout breaks the protocol stream, which is a P0 defect).
type Reader struct {
	dec *json.Decoder
}

func NewReader(r io.Reader) *Reader { return &Reader{dec: json.NewDecoder(r)} }

// Read returns the next frame, or io.EOF once the stream ends.
func (r *Reader) Read() (*Frame, error) {
	var f Frame
	if err := r.dec.Decode(&f); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("frame decode: %w", err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Writer encodes one JSON object per line to a stream that must be a protocol
// pipe, never a log sink.
type Writer struct {
	enc *json.Encoder
}

func NewWriter(w io.Writer) *Writer {
	enc := json.NewEncoder(w)
	// Provider payloads are copied verbatim into provider_raw; HTML escaping would
	// only mutate them and inflate every delta.
	enc.SetEscapeHTML(false)
	return &Writer{enc: enc}
}

// Write serialises and validates f. Validation runs first so a malformed frame
// never reaches the wire and desyncs the peer.
func (w *Writer) Write(f *Frame) error {
	if err := f.Validate(); err != nil {
		return err
	}
	return w.enc.Encode(f) // Encoder terminates each line.
}
