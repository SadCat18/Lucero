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
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// errCrash unwinds run() so the caller can exit with the status §10 pins for
// debug/crash. run() does not call os.Exit itself so the same code stays testable.
var errCrash = errors.New("fake-adapter: debug/crash requested")

const adapterVersion = "0.1.0"

func main() {
	if err := run(os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, errCrash) {
			os.Exit(2) // §10: debug/crash exits with status 2
		}
		fmt.Fprintf(os.Stderr, "fake-adapter: %v\n", err)
		os.Exit(1)
	}
}

// run serves the protocol until stdin closes. logs receives everything that is not a
// frame, so a caller can keep stdout and stderr apart the way Core's supervisor does.
func run(in io.Reader, out io.Writer, logs io.Writer) error {
	r, w := adapter.NewReader(in), adapter.NewWriter(out)
	fmt.Fprintf(logs, "fake-adapter up, pid=%d\n", os.Getpid())
	// Check the self-declaration up front, against the same validator Core uses.
	// Doing it per reply instead (as this file used to) turned a second
	// adapter/initialize into an undecodable reply plus a dead process: the caller
	// then waits for a response that will never come.
	if _, err := selfHandshake(); err != nil {
		return fmt.Errorf("Core would reject our own capability table: %w", err)
	}
	var hs *adapter.Handshake // nil until initialize succeeds: that is §2's gate
	for {
		f, err := r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// A frame we cannot parse has no id to answer to, so the session ends
			// rather than us guessing what was meant (§1).
			return err
		}
		if f.Kind() != adapter.KindRequest {
			// Notifications and replies get no answer. $/cancel and stream frames
			// belong here once the pump lands; logging instead of silently dropping
			// keeps a missing feature visible in stderr.
			fmt.Fprintf(logs, "ignoring %s frame %q\n", f.Kind(), f.Method)
			continue
		}
		crash := f.Method == adapter.MethodDebugCrash && hs != nil
		reply, err := handle(f, hs)
		if err != nil {
			return err
		}
		if err := w.Write(reply); err != nil {
			return err
		}
		switch {
		case crash:
			fmt.Fprintf(logs, "crashing on demand\n")
			return errCrash
		case f.Method == adapter.MethodInitialize && reply.Error == nil:
			hs = declaredHandshake()
		}
	}
}

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

// handle answers one request. Anything this build does not implement is refused with
// INTERNAL_ERROR rather than a contract refusal, so "not built yet" can never be
// mistaken for a Provider-side decision.
func handle(f *adapter.Frame, hs *adapter.Handshake) (*adapter.Frame, error) {
	if err := hs.Call(f.Method); err != nil {
		// The nil handshake is the un-initialised state, so this is §2's
		// PROTOCOL_NOT_INITIALIZED gate and §3's write_entry gate at once.
		return errorReply(f, err), nil
	}
	switch f.Method {
	case adapter.MethodInitialize:
		return adapter.NewResponse(f.ID, f.RequestID, initializeResult())
	case adapter.MethodHealth:
		return adapter.NewResponse(f.ID, f.RequestID, adapter.HealthResult{
			Health: adapter.HealthHealthy, Confidence: adapter.ConfidenceHigh, CheckedAt: nowMillis(),
		})
	case adapter.MethodDebugCrash:
		return adapter.NewResponse(f.ID, f.RequestID, map[string]any{"crashing": true})
	default:
		return adapter.NewErrorReply(f.ID, f.RequestID, adapter.ErrInternal,
			"fake-adapter: "+f.Method+" is not implemented in this build"), nil
	}
}

func errorReply(f *adapter.Frame, err error) *adapter.Frame {
	code, message := adapter.ErrInternal, err.Error()
	if e := new(adapter.Error); errors.As(err, &e) {
		code, message = e.Code, e.Message
	}
	return adapter.NewErrorReply(f.ID, f.RequestID, code, message)
}

// initializeResult is the fake's self-declaration. debug.crash sits at EXPERIMENTAL,
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
			{Name: "debug.crash", SupportLevel: adapter.LevelExperimental,
				EffectiveBackend: adapter.BackendNativeAPI, Confidence: adapter.ConfidenceHigh,
				LastVerifiedAt: nowMillis()},
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

func nowMillis() int64 { return time.Now().UnixMilli() }
