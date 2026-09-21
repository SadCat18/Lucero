package adapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNegotiate(t *testing.T) {
	cases := []struct {
		name       string
		core       Version
		peer       Version
		wantVer    Version
		wantExperi bool
		wantErr    ErrorCode // 0 means no error
	}{
		{"same", Version{1, 2}, Version{1, 2}, Version{1, 2}, false, 0},
		{"major 2 refuses and keeps no version", Version{1, 0}, Version{2, 0}, Version{}, false, ErrProtocolVersionMismatch},
		{"major 0 refuses", Version{1, 0}, Version{0, 9}, Version{}, false, ErrProtocolVersionMismatch},
		{"higher peer minor is allowed but experimental", Version{1, 0}, Version{1, 4}, Version{1, 0}, true, 0},
		{"lower peer minor wins", Version{1, 4}, Version{1, 2}, Version{1, 2}, false, 0},
		{"negative peer refused", Version{1, 0}, Version{1, -1}, Version{}, false, ErrInvalidParams},
	}
	for _, c := range cases {
		got, err := Negotiate(c.core, c.peer)
		if c.wantErr != 0 {
			var e *Error
			if !asError(err, &e) || e.Code != c.wantErr {
				t.Errorf("%s: Negotiate(%v,%v) = (%+v, %v), want code %s",
					c.name, c.core, c.peer, got, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: Negotiate = %+v, %v", c.name, got, err)
			continue
		}
		if got.Version != c.wantVer || got.Experimental != c.wantExperi {
			t.Errorf("%s: Negotiate = %+v, want %+v experimental=%v", c.name, got, c.wantVer, c.wantExperi)
		}
	}
}

func TestNewHandshake(t *testing.T) {
	base := withWriteEntry(true,
		liveCap("session.send", LevelNative, BackendNativeAPI, DeliveryStartNow, DeliveryGuide, DeliveryQueue))

	cases := []struct {
		name    string
		res     InitializeResult
		wantErr string // "" means the handshake must be accepted
	}{
		{"nominal", base, ""},
		{"major mismatch", with(func(r *InitializeResult) { r.Protocol = Version{2, 0} }, base.Capabilities...), "major"},
		{"empty software_versions", with(func(r *InitializeResult) { r.SoftwareVersions = nil }, base.Capabilities...), "software_versions"},
		{"no adapter_key", with(func(r *InitializeResult) { r.AdapterKey = "" }, base.Capabilities...), "adapter_key"},
		{"no adapter_version", with(func(r *InitializeResult) { r.AdapterVersion = "" }, base.Capabilities...), "adapter_version"},
		{"write_entry false is a legal declaration", with(func(r *InitializeResult) { r.WriteEntry = false }, base.Capabilities...), ""},
		{"bad auth_mode", with(func(r *InitializeResult) { r.AuthMode = "BYPASS" }, base.Capabilities...), "auth_mode"},
		{"bad health", with(func(r *InitializeResult) { r.Health = "FINE" }, base.Capabilities...), "health"},
		{"unknown support_level", with(func(r *InitializeResult) {
			r.Capabilities[0].SupportLevel = "SORTA"
		}, base.Capabilities...), "support_level"},
		{"a ninth support level is refused", with(func(r *InitializeResult) {
			r.Capabilities = append(r.Capabilities, liveCap("model.list", "MAGIC", BackendCLI))
		}, base.Capabilities...), "support_level"},
		{"unknown backend", with(func(r *InitializeResult) {
			r.Capabilities[0].EffectiveBackend = "keyboard"
		}, base.Capabilities...), "effective_backend"},
		{"unknown confidence", with(func(r *InitializeResult) {
			r.Capabilities[0].Confidence = "SUNNY"
		}, base.Capabilities...), "confidence"},
		{"unknown delivery mode", with(func(r *InitializeResult) {
			r.Capabilities[0].DeliveryModes = []string{DeliveryQueue, "later"}
		}, base.Capabilities...), "delivery_modes"},
		{"duplicate delivery mode", with(func(r *InitializeResult) {
			r.Capabilities[0].DeliveryModes = []string{DeliveryQueue, DeliveryQueue}
		}, base.Capabilities...), "twice"},
		{"duplicate capability row", with(func(r *InitializeResult) {
			r.Capabilities = append(r.Capabilities, base.Capabilities[0])
		}, base.Capabilities...), "declared twice"},
		{"unsupported declares no last_verified_at", with(func(r *InitializeResult) {
			c := liveCap("session.locate", LevelUnsupported, BackendReadOnly)
			c.LastVerifiedAt = 0
			r.Capabilities = append(r.Capabilities, c)
		}, base.Capabilities...), ""},
		{"ui automation must carry last_verified_at", with(func(r *InitializeResult) {
			c := liveCap("model.select", LevelUIAutomation, BackendUIAutomation)
			c.LastVerifiedAt = 0
			r.Capabilities = append(r.Capabilities, c)
		}, base.Capabilities...), "last_verified_at"},
		// §9: debug.* belongs to adapter_key=fake at EXPERIMENTAL and nowhere else.
		{"a real adapter declares debug.crash", with(func(r *InitializeResult) {
			r.Capabilities = append(r.Capabilities, liveCap("debug.crash", LevelExperimental, BackendNativeAPI))
		}, base.Capabilities...), "debug.crash"},
		{"fake declares debug.crash at NATIVE", with(func(r *InitializeResult) {
			r.AdapterKey = "fake"
			r.Capabilities = append(r.Capabilities, liveCap("debug.crash", LevelNative, BackendNativeAPI))
		}, base.Capabilities...), "debug.crash"},
		{"fake declares debug.crash at EXPERIMENTAL", with(func(r *InitializeResult) {
			r.AdapterKey = "fake"
			r.Capabilities = append(r.Capabilities, liveCap("debug.crash", LevelExperimental, BackendNativeAPI))
		}, base.Capabilities...), ""},
		{"debug spelled the method way is refused too", with(func(r *InitializeResult) {
			r.Capabilities = append(r.Capabilities, liveCap("debug/crash", LevelExperimental, BackendNativeAPI))
		}, base.Capabilities...), "debug/crash"},
	}
	for _, c := range cases {
		h, err := NewHandshake(Version{Major: ProtocolMajor, Minor: ProtocolMinor}, c.res)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: NewHandshake = %v, want accepted", c.name, err)
		case c.wantErr != "" && err == nil:
			t.Errorf("%s: NewHandshake accepted it, want refusal naming %q", c.name, c.wantErr)
		case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
			t.Errorf("%s: NewHandshake = %v, want it to name %q", c.name, err, c.wantErr)
		case c.wantErr != "" && h != nil:
			t.Errorf("%s: NewHandshake returned a handshake alongside %v", c.name, err)
		}
	}
}

// TestSoftwareVersionsMustBeAMap pins §2.1 at the wire level: a single version
// string is not decidable, so an Adapter cannot report one.
func TestSoftwareVersionsMustBeAMap(t *testing.T) {
	const doc = `{"adapter_key":"codex","adapter_version":"0.1.0","protocol":{"major":1,"minor":0},` +
		`"software_versions":"0.154.0","auth_mode":"NONE","health":"HEALTHY","capabilities":[],"write_entry":true}`
	var res InitializeResult
	err := json.Unmarshal([]byte(doc), &res)
	if err == nil {
		t.Fatal("single-string software_versions decoded fine, want a refusal")
	}
	if !strings.Contains(err.Error(), "software_versions") {
		t.Errorf("decode error = %v, want it to name software_versions", err)
	}
	if _, err := NewHandshake(Version{1, 0}, res); err == nil {
		t.Error("NewHandshake accepted a half-parsed result, want refusal")
	}
}

// TestCallGate covers §2 (nothing may be called before the handshake) and §3
// (write_entry:false means observation only).
func TestCallGate(t *testing.T) {
	send := liveCap("session.send", LevelNative, BackendNativeAPI, DeliveryStartNow)
	uiaSend := liveCap("session.send", LevelUIAutomation, BackendUIAutomation, DeliveryStartNow)
	uiaSendNotCapable := liveCap("session.send", LevelUIAutomation, BackendUIAutomation, DeliveryStartNow)
	uiaSendNotCapable.WriteCapable = false
	readOnly := hand(t, withWriteEntry(false, send, liveCap("session.list", LevelNative, BackendNativeAPI)))
	uiaWrite := hand(t, withWriteEntry(false, uiaSend))
	uiaInk := hand(t, withWriteEntry(false, uiaSendNotCapable))
	writable := hand(t, withWriteEntry(true, send))

	cases := []struct {
		name    string
		h       *Handshake
		method  string
		wantErr ErrorCode
	}{
		{"nil handshake refuses reads", nil, "session/list", ErrProtocolNotInitialized},
		{"nil handshake refuses writes", nil, "session/send", ErrProtocolNotInitialized},
		{"nil handshake refuses unlisted methods", nil, "nope/nope", ErrProtocolNotInitialized},
		{"nil handshake refuses cancel", nil, CancelMethod, ErrProtocolNotInitialized},
		// The one call that opens the gate must pass through it, or nobody can ever
		// handshake: this rule lives in Call so no caller has to remember it.
		{"initialize is allowed before the handshake", nil, MethodInitialize, 0},
		{"a second initialize is refused", readOnly, MethodInitialize, ErrInvalidParams},
		{"read-only adapter answers reads", readOnly, "session/list", 0},
		{"read-only adapter refuses send", readOnly, "session/send", ErrWriteEntryNone},
		{"read-only adapter refuses stop", readOnly, "session/stop", ErrWriteEntryNone},
		// §4.1's write set (Q1 ruling, 2026-09-20): session/send, session/stop,
		// model/select, approval/respond and debug/*. approval/respond is in it because
		// answering an approval authorises an external action - Permission Engine's
		// gate over *asking* cannot stand in for write_entry's gate over *doing*.
		// The old rows for approval/* being exempt cited "§12.11", a clause that does
		// not exist anywhere in specs/, and approval/cancel is not a method at all.
		{"read-only adapter refuses session create", readOnly, "session/create", ErrWriteEntryNone},
		{"read-only adapter refuses model select", readOnly, "model/select", ErrWriteEntryNone},
		{"read-only adapter refuses approval respond", readOnly, "approval/respond", ErrWriteEntryNone},
		{"read-only adapter still answers approval list", readOnly, "approval/list", 0},
		{"read-only adapter still answers quota read", readOnly, "quota/read", 0},
		{"read-only adapter refuses debug", readOnly, "debug/crash", ErrWriteEntryNone},
		{"UIA-declared send is the one write §3 allows", uiaWrite, "session/send", 0},
		// §3 + §4.1: a read-only Adapter may reach a write only via an
		// explicitly declared UI_AUTOMATION capability that is itself write-capable.
		{"UIA level without write_capable is not a write entry", uiaInk, "session/send", ErrWriteEntryNone},
		{"writable adapter answers send", writable, "session/send", 0},
	}
	for _, c := range cases {
		err := c.h.Call(c.method)
		if c.wantErr == 0 {
			if err != nil {
				t.Errorf("%s: Call(%s) = %v, want allowed", c.name, c.method, err)
			}
			continue
		}
		var e *Error
		if !asError(err, &e) || e.Code != c.wantErr {
			t.Errorf("%s: Call(%s) = %v, want code %s", c.name, c.method, err, c.wantErr)
		}
	}
}

// TestDeliveryGate pins §5: an undeclared state is refused explicitly, never
// quietly downgraded to queue.
func TestDeliveryGate(t *testing.T) {
	h := hand(t, withWriteEntry(true,
		liveCap("session.send", LevelNative, BackendNativeAPI, DeliveryStartNow, DeliveryGuide)))

	cases := []struct {
		mode    string
		wantErr ErrorCode
	}{
		{DeliveryStartNow, 0},
		{DeliveryGuide, 0},
		{DeliveryQueue, ErrDeliveryModeUnsupported},
		{"immediate", ErrDeliveryModeUnsupported},
		{"", ErrDeliveryModeUnsupported},
	}
	for _, c := range cases {
		err := h.Delivery("session/send", c.mode)
		if c.wantErr == 0 {
			if err != nil {
				t.Errorf("Delivery(%q) = %v, want allowed", c.mode, err)
			}
			continue
		}
		var e *Error
		if !asError(err, &e) || e.Code != c.wantErr {
			t.Errorf("Delivery(%q) = %v, want code %s", c.mode, err, c.wantErr)
		}
		// The refusal must name what the Adapter did declare, so the UI can explain
		// the difference instead of guessing.
		if e != nil && !strings.Contains(e.Message, "start_now") {
			t.Errorf("Delivery(%q) message = %q, want it to list the declared modes", c.mode, e.Message)
		}
	}
	// A method whose capability declares nothing cannot deliver at all.
	var e *Error
	if err := h.Delivery("session/stop", DeliveryQueue); !asError(err, &e) || e.Code != ErrCapabilityUnavailable {
		t.Errorf("Delivery on an undeclared capability = %v, want CAPABILITY_UNAVAILABLE", err)
	}
	// And the gate runs before delivery-mode checks, so an un-handshaken Adapter
	// is reported as such rather than as a mode problem.
	if err := (*Handshake)(nil).Delivery("session/send", DeliveryQueue); !asError(err, &e) ||
		e.Code != ErrProtocolNotInitialized {
		t.Errorf("Delivery before handshake = %v, want PROTOCOL_NOT_INITIALIZED", err)
	}
}

func TestEffectiveCapabilities(t *testing.T) {
	caps := []Capability{
		liveCap("session.list", LevelNative, BackendNativeAPI),
		liveCap("session.read", LevelLocalData, BackendLocalData),
		liveCap("model.select", LevelUIAutomation, BackendUIAutomation),
		{Name: "session.locate", SupportLevel: LevelCompatible, EffectiveBackend: BackendNativeAPI,
			Confidence: ConfidenceHigh, LastVerifiedAt: 1, Requires: []string{"session.read"}},
		{Name: "handoff.create", SupportLevel: LevelNative, EffectiveBackend: BackendNativeAPI,
			Confidence: ConfidenceHigh, LastVerifiedAt: 1, Requires: []string{"never.declared"}},
		{Name: "quota.read", SupportLevel: LevelUnsupported, EffectiveBackend: BackendReadOnly,
			Confidence: ConfidenceUnknown, Requires: []string{"session.list"}},
	}
	h := hand(t, withWriteEntry(true, caps...))
	if got := len(h.EffectiveCapabilities()); got != 4 {
		t.Errorf("effective = %d of %d, want 4 (UNSUPPORTED and unmet-requires rows excluded): %v",
			got, len(caps), h.EffectiveCapabilities())
	}
	if _, ok := h.Effective("handoff.create"); ok {
		t.Error("handoff.create is effective even though nothing declares never.declared")
	}
	if _, ok := h.Effective("session.locate"); !ok {
		t.Error("session.locate should be effective: its requirement session.read is")
	}
	if _, ok := h.Effective("quota.read"); ok {
		t.Error("quota.read is UNSUPPORTED and must not be effective")
	}
	var none *Handshake
	if none.Result().AdapterKey != "" || none.Version() != (Version{}) || none.Experimental() ||
		none.EffectiveCapabilities() != nil {
		t.Error("the nil Handshake must read as empty, not as an initialised Adapter")
	}
}

// TestHealthValidate pins CO-10: the reply carries
// health/confidence/checked_at, and UNKNOWN must not be dressed up as HEALTHY.
func TestHealthValidate(t *testing.T) {
	cases := []struct {
		name    string
		h       HealthResult
		wantErr ErrorCode
	}{
		{"healthy and sure", HealthResult{HealthHealthy, ConfidenceHigh, 1789800000000}, 0},
		{"degraded is still a definite claim", HealthResult{HealthDegraded, ConfidenceMedium, 1}, 0},
		{"unavailable with low confidence", HealthResult{HealthUnavailable, ConfidenceLow, 1}, 0},
		{"nothing known", HealthResult{HealthUnknown, ConfidenceUnknown, 1}, 0},
		{"HEALTHY on UNKNOWN confidence", HealthResult{HealthHealthy, ConfidenceUnknown, 1}, ErrInvalidParams},
		{"DEGRADED on UNKNOWN confidence", HealthResult{HealthDegraded, ConfidenceUnknown, 1}, ErrInvalidParams},
		{"undated reading", HealthResult{HealthHealthy, ConfidenceHigh, 0}, ErrInvalidParams},
		{"negative checked_at", HealthResult{HealthHealthy, ConfidenceHigh, -5}, ErrInvalidParams},
		{"invented health", HealthResult{"FINE", ConfidenceHigh, 1}, ErrInvalidParams},
		{"invented confidence", HealthResult{HealthHealthy, "SUNNY", 1}, ErrInvalidParams},
		{"empty health", HealthResult{"", ConfidenceHigh, 1}, ErrInvalidParams},
	}
	for _, c := range cases {
		err := c.h.Validate()
		if c.wantErr == 0 {
			if err != nil {
				t.Errorf("%s: Validate = %v, want legal", c.name, err)
			}
			continue
		}
		var e *Error
		if !asError(err, &e) || e.Code != c.wantErr {
			t.Errorf("%s: Validate = %v, want code %s", c.name, err, c.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), c.h.Health) && c.h.Health != "" {
			t.Errorf("%s: message %q does not name the health value", c.name, err)
		}
	}
}

func hand(t *testing.T, res InitializeResult) *Handshake {
	t.Helper()
	h, err := NewHandshake(Version{Major: ProtocolMajor, Minor: ProtocolMinor}, res)
	if err != nil {
		t.Fatalf("NewHandshake: %v", err)
	}
	return h
}

// with clones base so each table row mutates its own copy.
func with(mut func(*InitializeResult), caps ...Capability) InitializeResult {
	copied := make([]Capability, len(caps))
	copy(copied, caps)
	res := InitializeResult{
		AdapterKey: "codex", AdapterVersion: "0.1.0", Protocol: Version{1, 0},
		SoftwareVersions: map[string]string{"cli": "0.154.0", "desktop": "26.915.3509.0"},
		AuthMode:         AuthReuseClient, Health: HealthHealthy,
		Capabilities: copied, WriteEntry: true,
	}
	mut(&res)
	return res
}

func withWriteEntry(writeEntry bool, caps ...Capability) InitializeResult {
	return with(func(r *InitializeResult) { r.WriteEntry = writeEntry }, caps...)
}

func liveCap(name, level, backend string, modes ...string) Capability {
	return Capability{
		Name: name, SupportLevel: level, EffectiveBackend: backend,
		WriteCapable:   strings.HasSuffix(name, ".send") || strings.HasSuffix(name, ".stop"),
		DeliveryModes:  modes,
		Confidence:     ConfidenceHigh,
		LastVerifiedAt: 1789800000000,
	}
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

// TestWriteMethodSetIsSectionFourOnes closes the gap §4.1 had to record: the gate's own
// list, checked against the method names §4 actually defines. A name that is not in §4
// is only allowed here with an open REQ against it, because "code has it, contract does
// not" is how an ungated write (or a phantom clause) survives a review.
func TestWriteMethodSetIsContractWrites(t *testing.T) {
	// §4.1's set, verbatim: session/send, session/stop, model/select, approval/respond,
	// plus debug/* which isDebugName covers separately.
	defined := map[string]bool{
		"session/send": true, "session/stop": true, "model/select": true,
		"approval/respond": true, "session/list": true, "session/read": true,
		"model/list": true, "quota/read": true, "approval/list": true,
		"session/locate": true, "adapter/initialize": true, "adapter/health": true,
	}
	// Not in §4 yet: REQ-T8 asks for the row (CO-5 defined the shape, never written back).
	pending := map[string]bool{"session/create": true}

	for method := range writeMethods {
		if defined[method] {
			continue
		}
		if !pending[method] {
			t.Errorf("writeMethods names %q, which is neither a §4 method nor an open REQ", method)
		}
	}
	for method := range pending {
		if !writeMethods[method] {
			t.Errorf("%s is still pending §4 (REQ-T8) but left the write set: §3 would let it through", method)
		}
	}
	for _, want := range []string{"session/send", "session/stop", "model/select", "approval/respond"} {
		if !writeMethods[want] {
			t.Errorf("§4.1 lists %s as a write, writeMethods does not gate it", want)
		}
	}
	// Reads must stay readable for a write_entry:false Adapter, or §3 becomes a kill switch.
	for _, read := range []string{"session/list", "session/read", "model/list", "quota/read", "approval/list"} {
		if writeMethods[read] {
			t.Errorf("%s is a read in §4/§4.1 but sits in the write set", read)
		}
	}
	if len(writeMethods) != 5 {
		t.Errorf("writeMethods holds %d names, want 4 §4.1 writes + session/create (REQ-T8)", len(writeMethods))
	}
}
