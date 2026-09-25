package main

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// T-275's injection door, measured from outside the process the way `test` will use it.
// The pair of frames is what §4.2 裁定 ① cross-checks (the id the frame names vs the id
// Core bound to that ticket), and until now no outsider could produce a disagreement: the
// fake echoed Core's own value back, and the binding is Core-side state. So what is asserted
// here is not "the fake can be made weird" - it is "the weirdness is exactly one value, and
// nothing else moves".

type evidence struct {
	method string
	exec   string
	ref    string
	// keys is every params key on the wire, sorted: the shape claim, kept apart from values.
	keys []string
}

// stop closes the child's stdin and waits for it, so its stderr buffer is complete before
// anything reads it: os/exec copies a child's stderr in its own goroutine, and reading that
// buffer while the process is alive races - it can lose exactly the line being asserted.
func (p *fakeProcess) stop(t *testing.T) string {
	t.Helper()
	_ = p.stdin.Close()
	_ = p.cmd.Wait()
	return p.stderr.String()
}

// sendOnce starts a fake with the gate set to env (empty = leave it unset), sends one turn,
// and returns the two CO-8 evidence frames plus everything the child logged.
func sendOnce(t *testing.T, env string) ([]evidence, string) {
	t.Helper()
	if env != "" {
		t.Setenv(envBadExecutionID, env)
	}
	p := startFakeAdapter(t)
	initialize(t, p)
	reply := call(t, p, "1", "session/send", adapter.SendParams{
		RemoteSessionID: "s-1", Text: "hello", Delivery: adapter.DeliveryStartNow,
		ExecutionID: "exec-1", IdempotencyKey: "k1"})
	if reply.Error != nil {
		t.Fatalf("session/send: %s", reply.Error)
	}
	// trace runs every frame through the decoder Core uses, so a gate that put an illegal
	// frame on the wire fails here rather than at whoever reads it next.
	p.trace(t)

	var out []evidence
	for _, f := range p.notifications() {
		s, err := adapter.DecodeStream(f)
		if err != nil {
			t.Fatalf("Core could not decode our own notification: %v", err)
		}
		if s.Method != adapter.MethodTurnClaimed && s.Method != adapter.MethodTurnDispatch {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(f.Params, &raw); err != nil {
			t.Fatalf("params of %s are not a JSON object: %v", s.Method, err)
		}
		keys := make([]string, 0, len(raw))
		for k := range raw {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, evidence{method: s.Method, exec: s.ExecutionID, ref: s.TurnRef, keys: keys})
	}
	if len(out) != 2 {
		t.Fatalf("evidence frames on the wire = %d (%+v), want the claimed/dispatched pair", len(out), out)
	}
	return out, p.stop(t)
}

func TestBadExecutionIDGateOffEchoesWhatCoreBound(t *testing.T) {
	got, _ := sendOnce(t, "")
	if got[0].method != adapter.MethodTurnClaimed || got[1].method != adapter.MethodTurnDispatch {
		t.Fatalf("order = %s then %s, CO-8 wants claimed before dispatched", got[0].method, got[1].method)
	}
	for _, e := range got {
		if e.exec != "exec-1" {
			t.Errorf("%s carried execution_id %q, want Core's %q verbatim: off means today's shape",
				e.method, e.exec, "exec-1")
		}
		if e.ref != "1" {
			t.Errorf("%s carried turn_ref %q, want the ticket %q", e.method, e.ref, "1")
		}
	}
}

func TestBadExecutionIDGateOnContradictsTheBinding(t *testing.T) {
	got, logged := sendOnce(t, "1")
	for _, e := range got {
		if e.exec != conflictingExecutionID {
			t.Errorf("%s carried execution_id %q, want the injected %q", e.method, e.exec, conflictingExecutionID)
		}
		if e.exec == "exec-1" {
			t.Errorf("%s still echoes the bound id, so the contradiction never reaches the wire", e.method)
		}
		// The ticket stays honest: a frame that contradicts *and* loses its turn_ref is a
		// different accident (an unattributable frame), and this door is not for that.
		if e.ref != "1" {
			t.Errorf("%s carried turn_ref %q, want the ticket untouched", e.method, e.ref)
		}
	}
	// And the child says out loud what it contradicted, on the stream that is not the
	// protocol: a black-box test that trips the audit seam must be able to show which id it
	// lied about, without re-deriving it from the frames.
	for _, want := range []string{envBadExecutionID, conflictingExecutionID, "exec-1"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the gate never announced %q; the child logged:\n%s", want, logged)
		}
	}
}

// "不改协议声明与帧形状" is the claim this file exists to keep honest: same keys, same
// order, still decodable - one value differs.
func TestBadExecutionIDGateMovesNoFrameShape(t *testing.T) {
	off, _ := sendOnce(t, "")
	on, _ := sendOnce(t, "1")
	for i := range off {
		if off[i].method != on[i].method {
			t.Fatalf("frame %d method changed: %q -> %q", i, off[i].method, on[i].method)
		}
		if off[i].ref != on[i].ref {
			t.Errorf("frame %d turn_ref changed: %q -> %q (only execution_id may differ)",
				i, off[i].ref, on[i].ref)
		}
		if strings.Join(off[i].keys, ",") != strings.Join(on[i].keys, ",") {
			t.Fatalf("frame %d param keys = %v, want off's set %v: a new or dropped key is a protocol change",
				i, on[i].keys, off[i].keys)
		}
	}
}

// Only "1" opens the door. Without this case, `os.Getenv(...)` read as "any non-empty value"
// would pass everything above, and the default-off promise would describe a shape nobody
// tested.
func TestBadExecutionIDGateIgnoresOtherValues(t *testing.T) {
	for _, v := range []string{"0", "yes", "true", " "} {
		got, _ := sendOnce(t, v)
		for _, e := range got {
			if e.exec != "exec-1" {
				t.Errorf("gate=%q: %s carried %q, want the bound id - only %q opens the door",
					v, e.method, e.exec, "1")
			}
		}
	}
}
