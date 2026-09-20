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

// ErrorCode is the numeric JSON-RPC error code on the wire. CO-4 ruled it an
// integer because a standard JSON-RPC client type-checks the field; §15.1 then ruled
// that the *name* of a domain failure travels in `error.data.code` as UPPER_SNAKE.
// TestErrorCodeTable pins every number here so the contract, Core and any Adapter
// cannot drift apart.
//
// Two bands, deliberately:
//
//	-32600..-32700  JSON-RPC's predefined codes. Only the protocol-layer failures
//	                live here, and they carry no §15.1 name: the number already says
//	                everything (-32601 is method-not-found to any client, and
//	                naming it would invite a second, conflicting vocabulary).
//	-32000..-32049  the contract's domain refusals (§15.1), each mirrored in data.code.
//	-32050..-32099  reserved, Core never answers from it; a Provider's native error
//	                travels inside PROVIDER_ERROR's data.provider_raw instead.
//
// Before T-36 three domain codes sat on -32600..-32602, which are predefined
// (-32600 invalid request / -32601 method not found / -32602 invalid params): a
// generic client would have routed a WRITE_ENTRY_NONE as "method not found". That was
// the bug T-36(b) caught.
type ErrorCode int

// Domain codes Core itself puts on the wire (T-45's class 甲: "Core:internal/adapter"
// or "Core:internal/process"). Only these get Go symbols - the rest of §15.1 is
// vocabulary a real Adapter produces, and Core recognises those by the name in
// data.code plus the number in domainCodes below. Declaring a constant nobody sends
// would be speculative code that drifts the moment the contract table moves.
const (
	ErrProtocolNotInitialized  ErrorCode = -32000 // §2, Core refuses a pre-handshake call
	ErrProtocolVersionMismatch ErrorCode = -32001 // §2.2, Core rejects a foreign major
	ErrCapabilityUnavailable   ErrorCode = -32002 // §4, Core gates on the capability table
	ErrWriteEntryNone          ErrorCode = -32003 // §3, Core gates writes on write_entry
	ErrDeliveryModeUnsupported ErrorCode = -32004 // §5, Core refuses an undeclared state
	ErrInternal                ErrorCode = -32013 // §15.1 names it INTERNAL, not INTERNAL_ERROR
	// Vocabulary lives here by §15.1.1's own reading, producer be damned: ADAPTER_UNRESPONSIVE
	// is raised by internal/process when the child dies mid-call, but an Adapter reply
	// carrying it must still be recognisable inside the codec.
	ErrAdapterUnresponsive ErrorCode = -32014
)

// Protocol-layer failures use JSON-RPC's predefined numbers (§15.3), so they carry no
// §15.1 name in data.code: -32601 already means method-not-found to any client, and a
// second spelling would be a second truth (T-36's ruling).
const (
	ErrMethodNotFound ErrorCode = -32601
	ErrInvalidParams  ErrorCode = -32602
)

// domainCodes is §15.1 in full: the vocabulary Core must recognise, whether or not it
// ever produces a given code. Keyed by the numbers T-36 asked for; the producer of
// each one belongs in the contract (§15.1's 生产方 column), not duplicated here.
var domainCodes = map[ErrorCode]string{
	ErrProtocolNotInitialized:  "PROTOCOL_NOT_INITIALIZED",
	ErrProtocolVersionMismatch: "PROTOCOL_VERSION_MISMATCH",
	ErrCapabilityUnavailable:   "CAPABILITY_UNAVAILABLE",
	ErrWriteEntryNone:          "WRITE_ENTRY_NONE",
	ErrDeliveryModeUnsupported: "DELIVERY_MODE_UNSUPPORTED",
	-32005:                     "LOCAL_DATA_UNAVAILABLE", // Adapter (§4)
	-32006:                     "SESSION_NOT_FOUND",      // Adapter (§4)
	-32007:                     "AUTH_REQUIRED",          // Adapter (§2); never log in for the user
	-32008:                     "RATE_LIMITED",           // Adapter (§7)
	-32009:                     "DANGEROUS_FLAG_REFUSED", // Core:internal/process (§9 arg gate)
	-32010:                     "CANCELLED",              // 结果帧: $/cancel ack, never an error reply (§15.3)
	-32011:                     "TIMEOUT",                // Adapter (§1 deadline_at)
	-32012:                     "PROVIDER_ERROR",         // Adapter, raw kept in data.provider_raw
	ErrInternal:                "INTERNAL",
	ErrAdapterUnresponsive:     "ADAPTER_UNRESPONSIVE",
	-32015:                     "EXECUTION_STATE_UNKNOWN", // §7 crash semantics: refuse to advance an UNKNOWN execution
	-32016:                     "READ_ONLY",               // object-level read-only (§12.11), unlike §3's Adapter-level
}

var protocolCodeNames = map[ErrorCode]string{
	ErrMethodNotFound: "METHOD_NOT_FOUND",
	ErrInvalidParams:  "INVALID_PARAMS",
}

// IsDomainCode reports whether a number is one of §15.1's vocabulary.
func IsDomainCode(c ErrorCode) bool { _, ok := domainCodes[c]; return ok }

// CodeByName is the reverse of String, for a caller that holds the symbol and needs
// the number to put on the wire (FakeAdapter's debug/fail takes a name). An unknown
// name reports false rather than inventing a number: the caller then sends the
// number it was given, and Core's Degrade path handles it as an unknown code.
func CodeByName(name string) (ErrorCode, bool) {
	for c, n := range codeNames {
		if n == name {
			return c, true
		}
	}
	return 0, false
}

// adapterCodeFirst/Last bound -32050..-32099, which CO-4 reserves and Core must
// never occupy.
const (
	adapterCodeFirst = -32099
	adapterCodeLast  = -32050
)

// jsonRPCPredefinedFirst/Last bound the band Core may only use for §15.3's
// protocol-layer failures.
const (
	jsonRPCPredefinedFirst = -32700
	jsonRPCPredefinedLast  = -32600
)

// codeNames is everything Core may print, for logs, Audit and Error.Symbol. It is
// built from the two tables above, so the vocabulary stays in one place even though
// only class-甲 codes have Go constants.
var codeNames = func() map[ErrorCode]string {
	m := map[ErrorCode]string{}
	for k, v := range domainCodes {
		m[k] = v
	}
	for k, v := range protocolCodeNames {
		m[k] = v
	}
	return m
}()

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

// Known reports whether the number is in §15.1/§15.3's vocabulary. An Adapter may
// answer with a code Core has never been given (a newer Provider build, or
// debug/fail aimed at a made-up number); callers use this to degrade instead of
// branching on a code that means nothing here.
func (e *Error) Known() bool {
	if e == nil {
		return false
	}
	_, ok := codeNames[e.Code]
	return ok
}

// Degrade maps an unknown code to INTERNAL, keeping the original number in data so a
// log line and Audit still carry what the Adapter actually said. Never a guess: the
// unknown code stays visible, Core just stops pretending it understands it.
func (e *Error) Degrade() *Error {
	if e == nil || e.Known() {
		return e
	}
	// original_code and original_name, never code: writing a name into data.code
	// would make Symbol() report the unknown code again instead of INTERNAL, which is
	// the one thing this function exists to prevent.
	raw := map[string]any{"original_code": int(e.Code)}
	if data, ok := e.Data.(map[string]any); ok {
		for k, v := range data {
			if k == "code" {
				if name, ok := v.(string); ok && name != "" {
					raw["original_name"] = name
				}
				continue
			}
			raw[k] = v
		}
	}
	return &Error{Code: ErrInternal, Message: e.Message, Data: raw}
}

// Symbol is §15.1's name for the error, preferring a `data.code` the peer sent over
// the number-derived name: a real Adapter may answer with a domain code whose number
// Core has never been given.
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
	e := &Error{Code: code, Message: message}
	// §15.1's name travels in data.code for domain refusals only; a predefined
	// protocol code needs no name, because -32601 already means method-not-found to
	// any client and a second spelling would be a second truth (T-36c).
	if name, ok := domainCodes[code]; ok {
		e.Data = map[string]any{"code": name}
	}
	return &Frame{JSONRPC: JSONRPCVersion, ID: id, Error: e, RequestID: requestID}
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
