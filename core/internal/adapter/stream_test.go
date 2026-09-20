package adapter

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func notifyFrame(t *testing.T, method, params string) *Frame {
	t.Helper()
	doc := `{"jsonrpc":"2.0","method":"` + method + `","request_id":"r1","params":` + params + `}`
	var f Frame
	if err := json.Unmarshal([]byte(doc), &f); err != nil {
		t.Fatalf("fixture %s: %v", method, err)
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("fixture %s is not a valid frame: %v", method, err)
	}
	return &f
}

func TestDecodeStream(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		params  string
		want    StreamFrame
		wantErr string
	}{
		{
			name:   "delta",
			method: MethodStreamDelta,
			params: `{"turn_ref":"t-1","sequence_no":7,"channel":"ASSISTANT","text":"hel","provider_raw":{"tok":123}}`,
			want:   StreamFrame{SequenceNo: 7, Channel: ChannelAssistant, Text: "hel"},
		},
		{
			name:   "state",
			method: MethodStreamState,
			params: `{"turn_ref":"t-1","sequence_no":8,"normalized_status":"RUNNING","provider_status_raw":"generating","confidence":"MEDIUM"}`,
			want:   StreamFrame{SequenceNo: 8, NormalizedState: "RUNNING", Confidence: ConfidenceMedium},
		},
		{
			name:   "finished is the mandatory one",
			method: MethodTurnFinished,
			params: `{"turn_ref":"t-1","sequence_no":9,"result_code":"OK","usage":{"input_tokens":10}}`,
			want:   StreamFrame{SequenceNo: 9, ResultCode: "OK"},
		},
		{
			name:    "delta without a cursor is refused (CO-6)",
			method:  MethodStreamDelta,
			params:  `{"channel":"THOUGHT","text":"hmm"}`,
			wantErr: "CO-6",
		},
		{name: "unknown channel", method: MethodStreamDelta, params: `{"turn_ref":"t-1","sequence_no":1,"channel":"SYSTEM","text":"x"}`, wantErr: "channel"},
		{name: "missing channel", method: MethodStreamDelta, params: `{"turn_ref":"t-1","sequence_no":1,"text":"x"}`, wantErr: "channel"},
		{name: "negative sequence", method: MethodStreamDelta, params: `{"turn_ref":"t-1","sequence_no":-1,"channel":"TOOL"}`, wantErr: "CO-6"},
		{name: "state without status", method: MethodStreamState, params: `{"turn_ref":"t-1","sequence_no":1,"confidence":"HIGH"}`, wantErr: "normalized_status"},
		{name: "state without confidence", method: MethodStreamState, params: `{"turn_ref":"t-1","sequence_no":1,"normalized_status":"RUNNING"}`, wantErr: "confidence"},
		{name: "finished without result_code", method: MethodTurnFinished, params: `{"turn_ref":"t-1","sequence_no":1}`, wantErr: "result_code"},
		// §6.7: a frame that cannot be attributed is a protocol violation, and it is
		// the four output notifications that pay for it. turn/claimed and
		// turn/dispatched attribute themselves by execution_id (CO-8), so requiring a
		// second key there would reject frames the contract itself defines.
		{name: "delta without turn_ref", method: MethodStreamDelta, params: `{"sequence_no":1,"channel":"ASSISTANT","text":"x"}`, wantErr: "turn_ref"},
		{name: "finished without turn_ref", method: MethodTurnFinished, params: `{"sequence_no":1,"result_code":"OK"}`, wantErr: "turn_ref"},
	}
	for _, c := range cases {
		f := notifyFrame(t, c.method, c.params)
		got, err := DecodeStream(f)
		if c.wantErr != "" {
			if err == nil {
				t.Errorf("%s: decoded %+#v, want refusal naming %q", c.name, got, c.wantErr)
				continue
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: err = %v, want it to name %q", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: DecodeStream = %v", c.name, err)
			continue
		}
		want := c.want
		want.Method = c.method
		if got.SequenceNo != want.SequenceNo || got.Channel != want.Channel || got.Text != want.Text ||
			got.NormalizedState != want.NormalizedState || got.Confidence != want.Confidence ||
			got.ResultCode != want.ResultCode || got.IsFinished() != (c.method == MethodTurnFinished) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, want)
		}
	}
}

// TestProviderPayloadsStayRaw pins §6.4: provider_raw and usage cross the wire
// untouched, because Core may not interpret what only the Provider defines.
func TestProviderPayloadsStayRaw(t *testing.T) {
	const raw = `{"tok":123,"nested":{"a":[1,2]}}`
	f := notifyFrame(t, MethodStreamDelta, `{"turn_ref":"t-1","sequence_no":1,"channel":"ASSISTANT","text":"x","provider_raw":`+raw+`}`)
	got, err := DecodeStream(f)
	if err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	if string(got.ProviderRaw) != raw {
		t.Errorf("provider_raw = %s, want it byte-identical to %s", got.ProviderRaw, raw)
	}
	fin := notifyFrame(t, MethodTurnFinished, `{"turn_ref":"t-1","sequence_no":2,"result_code":"OK","usage":{"input_tokens":10,"output_tokens":3}}`)
	s, err := DecodeStream(fin)
	if err != nil {
		t.Fatalf("finished: %v", err)
	}
	if string(s.Usage) != `{"input_tokens":10,"output_tokens":3}` {
		t.Errorf("usage = %s", s.Usage)
	}
}

func TestStreamItemIsOpaque(t *testing.T) {
	f := notifyFrame(t, MethodStreamItem, `{"turn_ref":"t-1","sequence_no":4,"tool":"bash","state":"READY","provider_raw":{"exit":0}}`)
	got, err := DecodeStream(f)
	if err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	if got.SequenceNo != 4 {
		t.Errorf("item sequence_no = %d, want 4", got.SequenceNo)
	}
	if !json.Valid(got.Item) || !strings.Contains(string(got.Item), `"tool":"bash"`) {
		t.Errorf("item body was not passed through: %s", got.Item)
	}
}

func TestStreamFrameMustBeNotification(t *testing.T) {
	var req Frame
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":"1","method":"stream/delta","request_id":"r","params":{}}`), &req); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStream(&req); err == nil || !strings.Contains(err.Error(), "notification") {
		t.Errorf("a request-shaped stream frame = %v, want it refused as not a notification", err)
	}
	resp := &Frame{JSONRPC: JSONRPCVersion, ID: "1", Result: json.RawMessage(`{}`)}
	if _, err := DecodeStream(resp); err == nil {
		t.Error("a response-shaped stream frame decoded, want refusal")
	}
	var unknown Frame
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"stream/nope","request_id":"r","params":{"turn_ref":"t-1","sequence_no":1}}`), &unknown); err != nil {
		t.Fatal(err)
	}
	var e *Error
	if _, err := DecodeStream(&unknown); !asError(err, &e) || e.Code != ErrMethodNotFound {
		t.Errorf("stream/nope = %v, want METHOD_NOT_FOUND", err)
	}
}

// TestStreamRoundTrip covers the Adapter side: what NewStream writes is what
// Reader reads and DecodeStream decodes, including stream/item's injected cursor.
func TestStreamRoundTrip(t *testing.T) {
	out := []*StreamFrame{
		{Method: MethodStreamDelta, SequenceNo: 1, TurnRef: "t-1", Channel: ChannelAssistant, Text: "a\nb"},
		{Method: MethodStreamItem, SequenceNo: 2, TurnRef: "t-1", Item: json.RawMessage(`{"tool":"bash"}`)},
		{Method: MethodStreamState, SequenceNo: 3, TurnRef: "t-1", NormalizedState: "RUNNING", Confidence: ConfidenceHigh},
		{Method: MethodTurnClaimed, SequenceNo: 4, TurnRef: "t-1", Accepted: true, ExecutionID: "e1", ProviderExecutionID: "pe1", QueuePosition: i64(2)},
		{Method: MethodTurnDispatch, SequenceNo: 5, TurnRef: "t-1", TurnID: "t1", ExecutionID: "e1", ProviderExecutionID: "pe1"},
		{Method: MethodTurnFinished, SequenceNo: 6, TurnRef: "t-1", ResultCode: "OK"},
	}
	var buf strings.Builder
	w := NewWriter(&buf)
	for i, s := range out {
		f, err := NewStream(fmt.Sprintf("r%d", i), s)
		if err != nil {
			t.Fatalf("NewStream %d: %v", i, err)
		}
		if err := w.Write(f); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	r := NewReader(strings.NewReader(buf.String()))
	for i, want := range out {
		f, err := r.Read()
		if err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		got, err := DecodeStream(f)
		if err != nil {
			t.Fatalf("DecodeStream %d: %v", i, err)
		}
		if got.Method != want.Method || got.SequenceNo != want.SequenceNo {
			t.Errorf("frame %d = %+v, want %+v", i, got, want)
		}
		if i == 1 {
			// stream/item keeps the Provider's own fields at the top level next to
			// the injected cursor, rather than nesting them a second time.
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(f.Params, &fields); err != nil {
				t.Fatalf("item params: %v", err)
			}
			if _, ok := fields["tool"]; !ok {
				t.Errorf("item params %s lost its own fields", f.Params)
			}
			if string(fields["sequence_no"]) != "2" {
				t.Errorf("item sequence_no = %s, want 2", fields["sequence_no"])
			}
		}
	}
}

// TestTurnTracker walks the §6.3/§6.4 script table: a turn needs both
// turn/claimed and turn/dispatched to count as evidence, only turn/finished closes
// it, and an unclosed turn across a restart goes UNKNOWN - never a definite result.
//
// Ops: "claim:<accepted>/<execution_id>/<provider_execution_id>",
// "dispatch:<turn_id>/<execution_id>/<provider_execution_id>",
// "finish:<result_code>", "restart", "recover:<result_code>".
func TestTurnTracker(t *testing.T) {
	cases := []struct {
		name      string
		ops       []string
		wantState string
		wantErrs  []ErrorCode
	}{
		{
			name:      "claimed, dispatched, finished",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e1/pe1", "finish:OK"},
			wantState: TurnClosed, wantErrs: []ErrorCode{0, 0, 0},
		},
		{
			name:      "a restart with the turn running is never a failure verdict",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e1/pe1", "restart"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{0, 0, 0},
		},
		{
			name:      "a restart in the claimed-but-not-dispatched window is unevidenced too",
			ops:       []string{"claim:1/e1/pe1", "restart"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{0, 0},
		},
		{
			name:      "a late turn/finished cannot make an UNKNOWN turn definite",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e1/pe1", "restart", "finish:OK"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{0, 0, 0, ErrInternal},
		},
		{
			name:      "recovery closes it with evidence",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e1/pe1", "restart", "recover:INTERRUPTED"},
			wantState: TurnClosed, wantErrs: []ErrorCode{0, 0, 0, 0},
		},
		{
			name:      "recovery without a result is not evidence",
			ops:       []string{"claim:1/e1/pe1", "restart", "recover:"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{0, 0, ErrInvalidParams},
		},
		{
			name:      "dispatched without claimed has no counterpart",
			ops:       []string{"dispatch:t1/e1/pe1"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{ErrInternal},
		},
		{
			name:      "a dispatch attributed to another Execution is never RUNNING",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e2/pe2"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{0, ErrInternal},
		},
		{
			name:      "finished without dispatched is unevidenced, not a success",
			ops:       []string{"claim:1/e1/pe1", "finish:OK"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{0, ErrInternal},
		},
		{
			name:      "a second claim cannot overwrite an open turn",
			ops:       []string{"claim:1/e1/pe1", "claim:1/e1/pe2"},
			wantState: TurnClaimed, wantErrs: []ErrorCode{0, ErrInternal},
		},
		{
			name:      "a second dispatch is equally unevidenced",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e1/pe1", "dispatch:t2/e1/pe2"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{0, 0, ErrInternal},
		},
		{
			name:      "claim with no execution_id cannot be looked up later",
			ops:       []string{"claim:1//pe1"},
			wantState: "", wantErrs: []ErrorCode{ErrInvalidParams},
		},
		{
			name:      "accepted claim with no turn_id",
			ops:       []string{"claim:1/e1/"},
			wantState: "", wantErrs: []ErrorCode{ErrInvalidParams},
		},
		{
			name:      "dispatch missing any of the three ids",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e1/"},
			wantState: TurnClaimed, wantErrs: []ErrorCode{0, ErrInvalidParams},
		},
		{
			name:      "the same finished frame twice is a duplicate, not a contradiction",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e1/pe1", "finish:OK", "finish:OK"},
			wantState: TurnClosed, wantErrs: []ErrorCode{0, 0, 0, 0},
		},
		{
			name:      "two different results for one turn is a contradiction",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e1/pe1", "finish:OK", "finish:FAILED"},
			wantState: TurnClosed, wantErrs: []ErrorCode{0, 0, 0, ErrInternal},
		},
		{
			name:      "a refused claim opens nothing, so a later finished has no evidence",
			ops:       []string{"claim:0/e1/pe1", "finish:OK"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{0, ErrInternal},
		},
		{
			name:      "finished with nothing in flight",
			ops:       []string{"finish:OK"},
			wantState: TurnUnknown, wantErrs: []ErrorCode{ErrInternal},
		},
		{
			name:      "a restart of an idle Adapter is nothing to recover",
			ops:       []string{"restart"},
			wantState: "", wantErrs: []ErrorCode{0},
		},
		{
			name: "guide opens a second turn after the first closed",
			ops: []string{"claim:1/e1/pe1", "dispatch:t1/e1/pe1", "finish:INTERRUPTED",
				"claim:1/e2/pe2", "dispatch:t2/e2/pe2", "finish:OK"},
			wantState: TurnClosed, wantErrs: []ErrorCode{0, 0, 0, 0, 0, 0},
		},
		{
			name:      "finished without a result_code",
			ops:       []string{"claim:1/e1/pe1", "dispatch:t1/e1/pe1", "finish:"},
			wantState: TurnRunning, wantErrs: []ErrorCode{0, 0, ErrInvalidParams},
		},
		{
			name:      "recovery cannot act on a turn that is not UNKNOWN",
			ops:       []string{"claim:1/e1/pe1", "recover:OK"},
			wantState: TurnClaimed, wantErrs: []ErrorCode{0, ErrInternal},
		},
	}
	for _, c := range cases {
		var tr TurnTracker
		if len(c.ops) != len(c.wantErrs) {
			t.Fatalf("%s: bad table", c.name)
		}
		for i, op := range c.ops {
			kind, rest, _ := strings.Cut(op, ":")
			var err error
			switch kind {
			case "claim":
				accepted, exec, provider, _ := cut3(rest)
				err = tr.Claim(accepted == "1", exec, provider)
			case "dispatch":
				turn, exec, provider, _ := cut3(rest)
				err = tr.Dispatch(turn, exec, provider)
			case "finish":
				err = tr.Finish(rest)
			case "restart":
				// The boolean is what the Supervisor reads; the dedicated test below
				// pins it, so the script only checks the resulting state.
				tr.ProcessRestarted()
			case "recover":
				err = tr.Recover(rest)
			default:
				t.Fatalf("%s op %d: unknown kind %q", c.name, i, kind)
			}
			if c.wantErrs[i] == 0 {
				if err != nil {
					t.Errorf("%s op %d (%s): %v, want allowed", c.name, i, op, err)
				}
				continue
			}
			var e *Error
			if !asError(err, &e) || e.Code != c.wantErrs[i] {
				t.Errorf("%s op %d (%s) = %v, want code %s", c.name, i, op, err, c.wantErrs[i])
			}
		}
		if tr.State() != c.wantState {
			t.Errorf("%s: state = %q, want %q", c.name, tr.State(), c.wantState)
		}
	}
}

// cut3 splits the three id slots of a scripted op.
func cut3(s string) (string, string, string, bool) {
	a, rest, ok := strings.Cut(s, "/")
	b, c, _ := strings.Cut(rest, "/")
	return a, b, c, ok
}

// TestTurnTrackerKeepsEvidence pins that the §6.4 evidence survives the frames that
// produced it: Recovery needs the execution id and the provider handle to ask
// "did this run, and can we reconnect" (§8) instead of guessing.
func TestTurnTrackerKeepsEvidence(t *testing.T) {
	var tr TurnTracker
	if err := tr.Claim(true, "exec-7", "pre-7"); err != nil {
		t.Fatal(err)
	}
	if tr.TurnID() != "" || tr.State() != TurnClaimed {
		t.Errorf("after claim: turnID=%q state=%q, want no turn_id until turn/dispatched (§6.4)",
			tr.TurnID(), tr.State())
	}
	if tr.ProviderExecutionID() != "pre-7" {
		t.Errorf("Provider handle = %q, want pre-7: §6.4 puts provider_execution_id on turn/claimed too, "+
			"that is what Recovery queries in the claimed-but-not-dispatched window", tr.ProviderExecutionID())
	}
	if err := tr.Dispatch("turn-7", "exec-7", "provider-42"); err != nil {
		t.Fatal(err)
	}
	if tr.State() != TurnRunning || tr.ProviderExecutionID() != "provider-42" || tr.ExecutionID() != "exec-7" {
		t.Errorf("after dispatch: state=%q providerExec=%q exec=%q",
			tr.State(), tr.ProviderExecutionID(), tr.ExecutionID())
	}
	if tr.Result() != "" {
		t.Errorf("Result = %q before any turn/finished, want empty", tr.Result())
	}
	if err := tr.Finish("OK"); err != nil {
		t.Fatal(err)
	}
	if tr.Result() != "OK" || tr.State() != TurnClosed {
		t.Errorf("after finish: result=%q state=%q", tr.Result(), tr.State())
	}
	// Recovery keeps the Provider handle even after the turn goes UNKNOWN, which is
	// the whole point of §6.4 keeping execution_id on these signals.
	var lost TurnTracker
	if err := lost.Claim(true, "e9", "provider-9"); err != nil {
		t.Fatal(err)
	}
	if err := lost.Dispatch("t9", "e9", "provider-9"); err != nil {
		t.Fatal(err)
	}
	if !lost.ProcessRestarted() {
		t.Fatal("restart of a running turn was not reported")
	}
	if lost.ExecutionID() != "e9" || lost.ProviderExecutionID() != "provider-9" {
		t.Errorf("evidence lost on restart: exec=%q provider=%q", lost.ExecutionID(), lost.ProviderExecutionID())
	}
}

// TestTurnTrackerRestartReports pins the boolean the Supervisor needs: only a turn
// that had not closed becomes somebody's recovery problem.
func TestTurnTrackerRestartReports(t *testing.T) {
	idle := &TurnTracker{}
	if idle.ProcessRestarted() {
		t.Error("an idle Adapter reported a lost turn")
	}
	claimed := &TurnTracker{}
	if err := claimed.Claim(true, "e1", "pe1"); err != nil {
		t.Fatal(err)
	}
	if !claimed.ProcessRestarted() {
		t.Error("a claimed turn across a restart was not reported, so nothing would hand it to Recovery")
	}
	if claimed.ProcessRestarted() {
		t.Error("the same turn reported twice; the second restart must be silent")
	}
	running := &TurnTracker{}
	if err := running.Claim(true, "e2", "pe2"); err != nil {
		t.Fatal(err)
	}
	if err := running.Dispatch("t2", "e2", "pe2"); err != nil {
		t.Fatal(err)
	}
	if !running.ProcessRestarted() {
		t.Error("a RUNNING turn across a restart was not reported")
	}
	closed := &TurnTracker{}
	if err := closed.Claim(true, "e3", "pe3"); err != nil {
		t.Fatal(err)
	}
	if err := closed.Dispatch("t3", "e3", "pe3"); err != nil {
		t.Fatal(err)
	}
	if err := closed.Finish("OK"); err != nil {
		t.Fatal(err)
	}
	if closed.ProcessRestarted() {
		t.Error("a closed turn reported as lost on restart")
	}
}

// TestDecodeStreamEvidence covers §6.4's two signals at the wire level, since a
// missing execution_id there is what leaves Core unable to reconstruct a turn.
func TestDecodeStreamEvidence(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		params  string
		wantErr string
	}{
		{"claimed nominal", MethodTurnClaimed, `{"turn_ref":"t-1","sequence_no":1,"accepted":true,"execution_id":"e1","provider_execution_id":"pe1","queue_position":2}`, ""},
		{"refused claim needs no turn_id", MethodTurnClaimed, `{"turn_ref":"t-1","sequence_no":1,"execution_id":"e1","provider_execution_id":"pe1"}`, ""},
		{"claimed without execution_id", MethodTurnClaimed, `{"turn_ref":"t-1","sequence_no":1,"accepted":true,"provider_execution_id":"pe1"}`, "execution_id"},
		{"claimed without provider handle", MethodTurnClaimed, `{"turn_ref":"t-1","sequence_no":1,"accepted":true,"execution_id":"e1"}`, "provider_execution_id"},
		{"dispatch nominal", MethodTurnDispatch, `{"turn_ref":"t-1","sequence_no":2,"turn_id":"t1","execution_id":"e1","provider_execution_id":"pe1"}`, ""},
		{"dispatch without execution_id", MethodTurnDispatch, `{"turn_ref":"t-1","sequence_no":2,"turn_id":"t1","provider_execution_id":"pe1"}`, "execution_id"},
		{"dispatch without provider handle", MethodTurnDispatch, `{"turn_ref":"t-1","sequence_no":2,"turn_id":"t1","execution_id":"e1"}`, "provider_execution_id"},
		{"dispatch without turn_id", MethodTurnDispatch, `{"turn_ref":"t-1","sequence_no":2,"execution_id":"e1","provider_execution_id":"pe1"}`, "turn_id"},
				// §6.7 is six frames, not four: the CO-8 pair owes a turn_ref like every other
		// notification, even though CO-8 already gives it an execution_id to attribute itself by.
		{"claimed without turn_ref", MethodTurnClaimed, `{"sequence_no":1,"accepted":true,"execution_id":"e1","provider_execution_id":"pe1"}`, "turn_ref"},
		{"dispatch without turn_ref", MethodTurnDispatch, `{"sequence_no":2,"turn_id":"t1","execution_id":"e1","provider_execution_id":"pe1"}`, "turn_ref"},
	}
	for _, c := range cases {
		f := notifyFrame(t, c.method, c.params)
		got, err := DecodeStream(f)
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: DecodeStream = %v, want legal", c.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: decoded %+v, want refusal naming %q", c.name, got, c.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: err = %v, want it to name %q", c.name, err, c.wantErr)
		}
	}
}
func TestCursor(t *testing.T) {
	cases := []struct {
		name       string
		reset      int64
		seqs       []int64
		wantAccept []bool
		wantLast   int64
		wantSince  int64
	}{
		{name: "monotonic", seqs: []int64{1, 2, 3}, wantAccept: []bool{true, true, true}, wantLast: 3},
		{name: "exact duplicate dropped", seqs: []int64{1, 2, 2, 3}, wantAccept: []bool{true, true, false, true}, wantLast: 3},
		{name: "replayed tail dropped", seqs: []int64{1, 2, 3, 1, 2, 3}, wantAccept: []bool{true, true, true, false, false, false}, wantLast: 3},
		{name: "late frame below the cursor dropped", seqs: []int64{5, 6, 4}, wantAccept: []bool{true, true, false}, wantLast: 6},
		{name: "a gap before the first frame is not a loss", seqs: []int64{5, 6}, wantAccept: []bool{true, true}, wantLast: 6},
		{name: "a gap after the first frame is", seqs: []int64{5, 9}, wantAccept: []bool{true, true}, wantLast: 9, wantSince: 6},
		{name: "gap records the first unseen", seqs: []int64{1, 2, 9, 10}, wantAccept: []bool{true, true, true, true}, wantLast: 10, wantSince: 3},
		{name: "second gap does not overwrite the first", seqs: []int64{1, 9, 20}, wantAccept: []bool{true, true, true}, wantLast: 20, wantSince: 2},
		{name: "frames without a cursor pass and do not move it", seqs: []int64{1, 0, 0, 2}, wantAccept: []bool{true, true, true, true}, wantLast: 2},
	}
	for _, c := range cases {
		var cur Cursor
		cur.Reset(c.reset)
		if len(c.seqs) != len(c.wantAccept) {
			t.Fatalf("%s: bad table", c.name)
		}
		for i, seq := range c.seqs {
			if got := cur.Accept(seq); got != c.wantAccept[i] {
				t.Errorf("%s: Accept(%d) at %d = %v, want %v", c.name, seq, i, got, c.wantAccept[i])
			}
		}
		if cur.Last() != c.wantLast || cur.SinceSeq() != c.wantSince {
			t.Errorf("%s: Last=%d SinceSeq=%d, want Last=%d SinceSeq=%d",
				c.name, cur.Last(), cur.SinceSeq(), c.wantLast, c.wantSince)
		}
	}
}

// TestCursorResetOnRestart is the §6.2 + §8 interaction that bites in production:
// a restarted Adapter numbers from 1 again, and without Reset the whole new
// stream would be dropped as duplicates.
func TestCursorResetOnRestart(t *testing.T) {
	var c Cursor
	for _, seq := range []int64{1, 2, 3} {
		c.Accept(seq)
	}
	if c.Accept(1) {
		t.Fatal("first frame of the new process run accepted, want the stale cursor to drop it")
	}
	c.Reset(0)
	if !c.Accept(1) {
		t.Error("after Reset(0) the restarted Adapter's frame 1 was still dropped")
	}
	// Reconnect backfill: resume from the hole, and the already-seen head stays dropped.
	c.Reset(4)
	if got := c.SinceSeq(); got != 0 {
		t.Errorf("SinceSeq after Reset = %d, want 0", got)
	}
	if !c.Accept(5) {
		t.Error("Accept(5) after Reset(4) = dropped, want forwarded")
	}
	if c.Accept(4) {
		t.Error("Accept(4) after Reset(4) = forwarded, want the already-delivered frame dropped")
	}
}
