package adapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
)

func TestFrameKind(t *testing.T) {
	cases := []struct {
		name  string
		frame Frame
		want  Kind
	}{
		{"request", Frame{JSONRPC: JSONRPCVersion, ID: "1", Method: "session/list", RequestID: "r1"}, KindRequest},
		{"notification", Frame{JSONRPC: JSONRPCVersion, Method: "stream/delta", RequestID: "r1"}, KindNotification},
		{"response result", Frame{JSONRPC: JSONRPCVersion, ID: "1", Result: json.RawMessage(`{}`)}, KindResponse},
		{"response error", Frame{JSONRPC: JSONRPCVersion, ID: "1", Error: &Error{Code: ErrInternal, Message: "x"}}, KindResponse},
		{"wrong jsonrpc", Frame{JSONRPC: "2.1", ID: "1", Result: json.RawMessage(`{}`)}, KindInvalid},
		{"id without result", Frame{JSONRPC: JSONRPCVersion, ID: "1"}, KindInvalid},
		{"empty", Frame{}, KindInvalid},
	}
	for _, c := range cases {
		if got := c.frame.Kind(); got != c.want {
			t.Errorf("%s: Kind() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFrameValidate(t *testing.T) {
	cases := []struct {
		name    string
		frame   Frame
		wantErr string // empty means the frame must validate
	}{
		{"initialize request", Frame{JSONRPC: JSONRPCVersion, ID: "a", Method: "adapter/initialize",
			Params: json.RawMessage(`{}`), RequestID: "r"}, ""},
		{"cancel notification", Frame{JSONRPC: JSONRPCVersion, ID: "", Method: CancelMethod, RequestID: "r"}, ""},
		{"response", Frame{JSONRPC: JSONRPCVersion, ID: "a", Result: json.RawMessage(`{}`)}, ""},
		{"no jsonrpc", Frame{ID: "a", Method: "session/list", RequestID: "r"}, "jsonrpc"},
		{"request without request_id", Frame{JSONRPC: JSONRPCVersion, ID: "a", Method: "session/list"}, "request_id"},
		{"notification without request_id", Frame{JSONRPC: JSONRPCVersion, Method: "turn/finished"}, "request_id"},
		{"method without domain", Frame{JSONRPC: JSONRPCVersion, ID: "a", Method: "session-list", RequestID: "r"}, "域/动作"},
		{"method with uppercase", Frame{JSONRPC: JSONRPCVersion, ID: "a", Method: "Session/list", RequestID: "r"}, "域/动作"},
		{"method with empty action", Frame{JSONRPC: JSONRPCVersion, ID: "a", Method: "session/", RequestID: "r"}, "域/动作"},
		{"both result and error", Frame{JSONRPC: JSONRPCVersion, ID: "a", Result: json.RawMessage(`{}`),
			Error: &Error{Code: ErrInternal, Message: "x"}}, "both"},
		{"error without code", Frame{JSONRPC: JSONRPCVersion, ID: "a", Error: &Error{Message: "x"}}, "no code"},
	}
	for _, c := range cases {
		err := c.frame.Validate()
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: Validate() = %v, want nil", c.name, err)
		case c.wantErr != "" && err == nil:
			t.Errorf("%s: Validate() = nil, want error naming %q", c.name, c.wantErr)
		case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
			t.Errorf("%s: Validate() = %v, want it to name %q", c.name, err, c.wantErr)
		}
	}
}

// TestFramePerLineInvariant pins contract §1: stdout carries one complete JSON
// object per line, so a frame must never contain a bare newline even when the
// payload text does, and the stream must survive reading back what was written.
func TestFramePerLineInvariant(t *testing.T) {
	frames := []*Frame{
		{JSONRPC: JSONRPCVersion, ID: "1", Method: "session/send", RequestID: "r1", DeadlineAt: 1789800000000,
			Params: json.RawMessage(`{"text":"a\nb\r\nc","delivery":"start_now"}`)},
		{JSONRPC: JSONRPCVersion, Method: "stream/delta", RequestID: "r2",
			Params: json.RawMessage(`{"sequence_no":1,"channel":"ASSISTANT","text":"<script> & \"quoted\" 多行\n结尾"}`)},
		{JSONRPC: JSONRPCVersion, ID: "1", RequestID: "r1", Result: json.RawMessage(`{"ok":true}`)},
		{JSONRPC: JSONRPCVersion, ID: "2", RequestID: "r2",
			Error: &Error{Code: ErrDeliveryModeUnsupported, Message: "no guide"}},
	}
	var buf bytes.Buffer
	w := NewWriter(&buf)
	for _, f := range frames {
		if err := w.Write(f); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if got, want := strings.Count(buf.String(), "\n"), len(frames); got != want {
		t.Fatalf("wrote %d newlines for %d frames, want one line each:\n%s", got, want, buf.String())
	}
	if strings.Contains(buf.String(), "&lt;") {
		t.Errorf("HTML-escaped the provider payload: %s", buf.String())
	}

	r := NewReader(&buf)
	for i, want := range frames {
		got, err := r.Read()
		if err != nil {
			t.Fatalf("frame %d Read: %v", i, err)
		}
		if got.Kind() != want.Kind() || got.Method != want.Method || got.ID != want.ID ||
			got.RequestID != want.RequestID || got.DeadlineAt != want.DeadlineAt {
			t.Fatalf("frame %d = %+v, want %+v", i, got, want)
		}
		if string(got.Params) != string(want.Params) {
			t.Errorf("frame %d params = %s, want %s", i, got.Params, want.Params)
		}
		if !bytes.Equal(got.Result, want.Result) {
			t.Errorf("frame %d result = %s, want %s", i, got.Result, want.Result)
		}
	}
	if _, err := r.Read(); !errors.Is(err, io.EOF) {
		t.Errorf("Read past the end = %v, want io.EOF", err)
	}
}

func TestReaderRejectsNonFrames(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"log line on stdout", "adapter starting up\n"},
		{"jsonrpc batch", `[{"jsonrpc":"2.0","id":"1","method":"session/list"}]` + "\n"},
		{"bare number", `41` + "\n"},
		{"numeric id", `{"jsonrpc":"2.0","id":1,"method":"session/list","request_id":"r"}` + "\n"},
	}
	for _, c := range cases {
		_, err := NewReader(strings.NewReader(c.input)).Read()
		if err == nil {
			t.Errorf("%s: Read() = nil error, want the frame refused", c.name)
		}
	}
}

// TestReaderKeepsStreamAfterContentError documents the recovery path the process
// supervisor relies on: a semantically bad frame is reportable without having to
// kill the connection.
func TestReaderKeepsStreamAfterContentError(t *testing.T) {
	const good = `{"jsonrpc":"2.0","method":"turn/finished","request_id":"r2","params":{"result_code":"OK"}}`
	r := NewReader(strings.NewReader(`{"jsonrpc":"2.0","method":"session/list"}` + "\n" + good + "\n"))
	if _, err := r.Read(); err == nil || !strings.Contains(err.Error(), "request_id") {
		t.Fatalf("first Read = %v, want the missing-request_id error", err)
	}
	f, err := r.Read()
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if f.Method != "turn/finished" {
		t.Fatalf("second Read = %q, want turn/finished", f.Method)
	}
}

func TestNewCancel(t *testing.T) {
	f, err := NewCancel("r9", "req-42")
	if err != nil {
		t.Fatalf("NewCancel: %v", err)
	}
	if f.Kind() != KindNotification || f.Method != CancelMethod {
		t.Fatalf("NewCancel = %+v, want a $/cancel notification", f)
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("cancel frame does not validate: %v", err)
	}
	if got := string(f.Params); got != `{"id":"req-42"}` {
		t.Errorf("cancel params = %s, want the original id (§1)", got)
	}
	if _, err := NewCancel("r9", ""); err == nil {
		t.Error("NewCancel with no target id = nil error, want refusal")
	}
}

// TestErrorCodeTable pins the §1 symbol<->number table (CO-4): the numbers are
// what a Provider-facing Adapter will put on the wire, so renumbering silently is
// a protocol break. 主控 pastes this table into §1.
// §15.1 in full, as the table T-36/T-45 asked me to hand back. `producer` is the
// column T-45 wants folded into the contract: Adapter (a real Adapter answers it),
// Core:internal/adapter, Core:internal/process, or result-frame (never on the wire).
// Only the Core:* rows have Go constants; the rest is vocabulary Core recognises.
var codeTable = []struct {
	num      int
	name     string
	producer string
	constant ErrorCode
	hasConst bool
}{
	{-32000, "PROTOCOL_NOT_INITIALIZED", "Core:internal/adapter", ErrProtocolNotInitialized, true},
	{-32001, "PROTOCOL_VERSION_MISMATCH", "Core:internal/adapter", ErrProtocolVersionMismatch, true},
	{-32002, "CAPABILITY_UNAVAILABLE", "Core:internal/adapter", ErrCapabilityUnavailable, true},
	{-32003, "WRITE_ENTRY_NONE", "Core:internal/adapter", ErrWriteEntryNone, true},
	{-32004, "DELIVERY_MODE_UNSUPPORTED", "Core:internal/adapter", ErrDeliveryModeUnsupported, true},
	{-32005, "LOCAL_DATA_UNAVAILABLE", "Adapter", 0, false},
	{-32006, "SESSION_NOT_FOUND", "Adapter", 0, false},
	{-32007, "AUTH_REQUIRED", "Adapter", 0, false},
	{-32008, "RATE_LIMITED", "Adapter", 0, false},
	{-32009, "DANGEROUS_FLAG_REFUSED", "Core:internal/process", 0, false},
	{-32010, "CANCELLED", "result-frame", 0, false},
	{-32011, "TIMEOUT", "Adapter", 0, false},
	{-32012, "PROVIDER_ERROR", "Adapter", 0, false},
	{-32013, "INTERNAL", "Core:internal/adapter", ErrInternal, true},
	{-32014, "ADAPTER_UNRESPONSIVE", "Core:internal/process", ErrAdapterUnresponsive, true},
	{-32015, "EXECUTION_STATE_UNKNOWN", "Core:internal/process", 0, false},
	{-32016, "READ_ONLY", "Adapter", 0, false},
	{-32601, "METHOD_NOT_FOUND", "Core:internal/adapter (协议层, §15.3)", ErrMethodNotFound, true},
	{-32602, "INVALID_PARAMS", "Core:internal/adapter (协议层, §15.3)", ErrInvalidParams, true},
}

func TestErrorCodeTable(t *testing.T) {
	seen := map[int]string{}
	domain, protocol := 0, 0
	for _, c := range codeTable {
		if c.num >= adapterCodeFirst && c.num <= adapterCodeLast {
			t.Errorf("%s = %d sits in the reserved Adapter band -32099..-32050", c.name, c.num)
		}
		predefined := c.num >= jsonRPCPredefinedFirst && c.num <= jsonRPCPredefinedLast
		name, isDomain := domainCodes[ErrorCode(c.num)]
		if isDomain {
			domain++
		}
		if _, ok := protocolCodeNames[ErrorCode(c.num)]; ok {
			protocol++
		}
		// Membership must agree with the band: domain codes keep clear of the
		// predefined numbers (T-36b), and only the predefined pair carries no name.
		if isDomain == predefined {
			t.Errorf("%s = %d: domain=%v but predefined=%v", c.name, c.num, isDomain, predefined)
		}
		if isDomain && name != c.name {
			t.Errorf("%d is named %q in domainCodes, the table says %q", c.num, name, c.name)
		}
		if got := ErrorCode(c.num).String(); got != c.name {
			t.Errorf("code %d String() = %q, want %q", c.num, got, c.name)
		}
		// And back again: §15.3 rule 3 makes this file the only truth for the
		// name↔number pair, so a lookup by name has to land on the same row.
		if got, ok := CodeByName(c.name); !ok || int(got) != c.num {
			t.Errorf("CodeByName(%q) = %d, %v, want %d", c.name, int(got), ok, c.num)
		}
		// Only Core-produced codes may have a constant, and a constant must match.
		if c.hasConst && int(c.constant) != c.num {
			t.Errorf("%s constant = %d, table says %d", c.name, int(c.constant), c.num)
		}
		// A constant is required only where this package is the producer; a code
		// produced by internal/process declares its own symbol there, and listing
		// exemptions here would rot the moment it grows one.
		if c.producer == "Core:internal/adapter" && !c.hasConst {
			t.Errorf("%s is produced here but has no constant", c.name)
		}
		if !strings.HasPrefix(c.producer, "Core:") && c.hasConst {
			t.Errorf("%s is produced by %s yet Core declares a constant (speculative)", c.name, c.producer)
		}
		if dup, ok := seen[c.num]; ok {
			t.Fatalf("duplicate wire code %d for %s and %s", c.num, dup, c.name)
		}
		seen[c.num] = c.name
		if raw, err := json.Marshal(ErrorCode(c.num)); err != nil || string(raw) != strconv.Itoa(c.num) {
			t.Errorf("marshal %s = %s, %v, want the bare number %d", c.name, raw, err, c.num)
		}
	}
	if domain != 17 || protocol != 2 {
		t.Errorf("membership = %d domain + %d protocol, want 17 + 2", domain, protocol)
	}
	if len(domainCodes) != 17 || len(protocolCodeNames) != 2 {
		t.Errorf("tables hold %d domain + %d protocol, want 17 + 2",
			len(domainCodes), len(protocolCodeNames))
	}
	if len(codeNames) != len(codeTable) {
		t.Errorf("codeNames covers %d entries, the table has %d", len(codeNames), len(codeTable))
	}
	for code, name := range codeNames {
		if _, ok := seen[int(code)]; !ok {
			t.Errorf("%s (%d) is in codeNames but not in the table", name, int(code))
		}
	}
	if got := ErrorCode(-32098).String(); got != "UNKNOWN_CODE(-32098)" {
		t.Errorf("unknown code renders %q, want the number kept visible", got)
	}
}

// TestUnknownCodeDegradesToInternal is T-45's "Core 必须按未知码降级为 INTERNAL 且不崩":
// an Adapter answering with a number outside §15.1 (a newer Provider build, or
// debug/fail pointed at a made-up code) must not be routed by a meaning Core invented,
// and the original number has to survive for the log and Audit line.
func TestUnknownCodeDegradesToInternal(t *testing.T) {
	unknown := &Error{Code: ErrorCode(-32499), Message: "brand new failure",
		Data: map[string]any{"retry_after_ms": 500, "code": "SOMETHING_NEW"}}
	if unknown.Known() {
		t.Error("Known() = true for a number that is in neither table")
	}
	got := unknown.Degrade()
	if got.Code != ErrInternal || got.Message != "brand new failure" {
		t.Errorf("Degrade() = %+v, want INTERNAL with the message kept", got)
	}
	data, ok := got.Data.(map[string]any)
	if !ok || data["original_code"] != -32499 || data["retry_after_ms"] != 500 ||
		data["original_name"] != "SOMETHING_NEW" {
		t.Errorf("degraded data = %+v, want original_code plus the peer's own fields", got.Data)
	}
	if got := unknown.Degrade().Symbol(); got != "INTERNAL" {
		t.Errorf("Symbol() after Degrade = %q, want INTERNAL: a name in data.code must not resurrect the unknown code", got)
	}
	if _, present := got.Data.(map[string]any)["code"]; present {
		t.Error("data.code survived the degradation, which is what Symbol() reads")
	}
	// A known code is returned untouched, and a nil error must not panic.
	known := &Error{Code: ErrWriteEntryNone, Message: "m"}
	if known.Degrade() != known {
		t.Error("a known code was rewritten by Degrade()")
	}
	var nilErr *Error
	if nilErr.Degrade() != nil || nilErr.Known() {
		t.Error("nil *Error must pass through as nil rather than panicking")
	}
}

// TestDomainNameTravelsInDataCode is §15.1's shape rule and T-36(c)'s consequence: a
// domain refusal carries its UPPER_SNAKE name in data.code, while a predefined
// protocol failure carries only its number - naming both would give one concept two
// spellings on the wire, which is what T-36 asked me to settle.
func TestDomainNameTravelsInDataCode(t *testing.T) {
	raw, err := json.Marshal(NewErrorReply("9", "r9", ErrDeliveryModeUnsupported, "no guide"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"data":{"code":"DELIVERY_MODE_UNSUPPORTED"}`) {
		t.Errorf("domain reply lost its §15.1 name: %s", raw)
	}
	raw, err = json.Marshal(NewErrorReply("9", "r9", ErrMethodNotFound, "stream/nope"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"data"`) {
		t.Errorf("a predefined code invented a second name: %s", raw)
	}
}

// TestErrorFromWireRoundTrips is the other half of CO-4: a reply that carries a
// numeric code decodes back into the symbol, and a code-less error reply is
// refused rather than read as code 0.
func TestErrorFromWireRoundTrips(t *testing.T) {
	var f Frame
	const doc = `{"jsonrpc":"2.0","id":"1","request_id":"r","error":{"code":-32003,"message":"write_entry=false"}}`
	if err := json.Unmarshal([]byte(doc), &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if f.Error.Code != ErrWriteEntryNone || f.Error.Error() != "WRITE_ENTRY_NONE: write_entry=false" {
		t.Fatalf("error = %+v", f.Error)
	}
	var bare Frame
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":"1","request_id":"r","error":{"message":"oops"}}`), &bare); err != nil {
		t.Fatal(err)
	}
	if err := bare.Validate(); err == nil || !strings.Contains(err.Error(), "no code") {
		t.Errorf("code-less error reply = %v, want it refused", err)
	}
}

func TestNewResponseAndErrorReply(t *testing.T) {
	res, err := NewResponse("7", "r7", map[string]any{"observations": []any{}})
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	if res.JSONRPC != JSONRPCVersion || res.ID != "7" || res.RequestID != "r7" ||
		string(res.Result) != `{"observations":[]}` {
		t.Fatalf("NewResponse = %+v", res)
	}
	bad := NewErrorReply("7", "r7", ErrProtocolNotInitialized, "no handshake")
	if bad.Error == nil || bad.Error.Code != ErrProtocolNotInitialized || bad.Result != nil {
		t.Fatalf("NewErrorReply = %+v", bad)
	}
	if err := bad.Validate(); err != nil {
		t.Fatalf("reply Validate: %v", err)
	}
}
