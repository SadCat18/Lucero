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
func TestErrorCodeTable(t *testing.T) {
	cases := []struct {
		code ErrorCode
		num  int
		sym  string
	}{
		{ErrProtocolNotInitialized, -32600, "PROTOCOL_NOT_INITIALIZED"},
		{ErrWriteEntryNone, -32601, "WRITE_ENTRY_NONE"},
		{ErrCapabilityUnavailable, -32602, "CAPABILITY_UNAVAILABLE"},
		{ErrDeliveryModeUnsupported, -32603, "DELIVERY_MODE_UNSUPPORTED"},
		{ErrInvalidParams, -32001, "INVALID_PARAMS"},
		{ErrAdapterUnresponsive, -32004, "ADAPTER_UNRESPONSIVE"},
		{ErrProtocolVersionMismatch, -32048, "PROTOCOL_VERSION_MISMATCH"},
		{ErrLocalDataUnavailable, -32049, "LOCAL_DATA_UNAVAILABLE"},
		{ErrMethodNotFound, -32002, "METHOD_NOT_FOUND"},
		{ErrInternal, -32003, "INTERNAL"},
	}
	seen := map[int]ErrorCode{}
	for _, c := range cases {
		if int(c.code) != c.num {
			t.Errorf("%s = %d, want %d", c.sym, int(c.code), c.num)
		}
		if got := c.code.String(); got != c.sym {
			t.Errorf("code %d String() = %q, want %q", c.num, got, c.sym)
		}
		// -32050..-32099 is reserved (§1): Core must never answer from it.
		if c.num >= adapterCodeFirst && c.num <= adapterCodeLast {
			t.Errorf("%s = %d sits in the reserved band -32099..-32050", c.sym, c.num)
		}
		if dup, ok := seen[c.num]; ok {
			t.Fatalf("duplicate wire code %d for %s and %s", c.num, dup, c.code)
		}
		seen[c.num] = c.code
		if num, err := json.Marshal(c.code); err != nil || string(num) != strconv.Itoa(c.num) {
			t.Errorf("marshal %s = %s, %v, want the bare number %d", c.sym, num, err, c.num)
		}
	}
	for code, sym := range codeNames {
		if _, ok := seen[int(code)]; !ok {
			t.Errorf("%s (%d) is in codeNames but not in the table under test", sym, int(code))
		}
	}
	if len(codeNames) != len(cases) {
		t.Errorf("codeNames has %d entries, the table has %d", len(codeNames), len(cases))
	}
	if got := ErrorCode(-32050).String(); got != "UNKNOWN_CODE(-32050)" {
		t.Errorf("unknown code renders %q, want the number kept visible", got)
	}
}

// TestErrorFromWireRoundTrips is the other half of CO-4: a reply that carries a
// numeric code decodes back into the symbol, and a code-less error reply is
// refused rather than read as code 0.
func TestErrorFromWireRoundTrips(t *testing.T) {
	var f Frame
	const doc = `{"jsonrpc":"2.0","id":"1","request_id":"r","error":{"code":-32601,"message":"write_entry=false"}}`
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

// TestErrorCarriesSymbolInData pins §15.1: the Adapter layer's UPPER_SNAKE name
// travels in error.data.code, and a peer's data.code wins over the number when the
// two disagree (an Adapter may answer with a §15 code Core was never given a number
// for).
func TestErrorCarriesSymbolInData(t *testing.T) {
	f := NewErrorReply("9", "r9", ErrDeliveryModeUnsupported, "no guide")
	var wire struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				Code string `json:"code"`
			} `json:"data"`
		} `json:"error"`
	}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if wire.Error.Code != -32603 || wire.Error.Data.Code != "DELIVERY_MODE_UNSUPPORTED" {
		t.Errorf("wire error = %+v, want numeric code plus the §15.1 name in data.code", wire.Error)
	}
	// A number Core does not know keeps its number visible rather than being renamed.
	unknown := &Error{Code: ErrorCode(-32700), Message: "from a Provider"}
	if got := unknown.Symbol(); got != "UNKNOWN_CODE(-32700)" {
		t.Errorf("Symbol() = %q, want the number kept", got)
	}
	// data.code wins when it disagrees with the number.
	renamed := &Error{Code: ErrorCode(-32700), Message: "m",
		Data: map[string]any{"code": "RATE_LIMITED"}}
	if got := renamed.Symbol(); got != "RATE_LIMITED" {
		t.Errorf("Symbol() = %q, want the peer's §15.1 name to win", got)
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
