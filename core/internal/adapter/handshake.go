package adapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Protocol version Core speaks. Contract §2.2: negotiated at handshake only, and
// never changed while the process runs.
const (
	ProtocolMajor = 1
	ProtocolMinor = 0
)

// Version is the protocol version pair exchanged in the handshake.
type Version struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

// Delivery modes (contract §5) are a first-class protocol concept, not a Core
// implementation detail: an unsupported mode is an explicit refusal.
const (
	DeliveryStartNow = "start_now"
	DeliveryGuide    = "guide"
	DeliveryQueue    = "queue"
)

// The eight frozen support levels from the architecture doc's 能力等级 section.
const (
	LevelNative       = "NATIVE"
	LevelCompatible   = "COMPATIBLE"
	LevelCLI          = "CLI"
	LevelLocalData    = "LOCAL_DATA"
	LevelUIAutomation = "UI_AUTOMATION"
	LevelReadOnly     = "READ_ONLY"
	LevelExperimental = "EXPERIMENTAL"
	LevelUnsupported  = "UNSUPPORTED"
)

// Effective backends (§3). ui_automation sits at the same rank as cli and
// local_data: it is a real implementation, not a stopgap.
const (
	BackendNativeAPI    = "native_api"
	BackendCLI          = "cli"
	BackendLocalData    = "local_data"
	BackendUIAutomation = "ui_automation"
	BackendReadOnly     = "read_only"
)

// Confidence values. CO-5: no contract freezes this set yet; §3 and §7 only ever
// show HIGH and UNKNOWN.
const (
	ConfidenceHigh    = "HIGH"
	ConfidenceMedium  = "MEDIUM"
	ConfidenceLow     = "LOW"
	ConfidenceUnknown = "UNKNOWN"
)

// Auth modes and health values from contract §2.
const (
	AuthReuseClient = "REUSE_CLIENT"
	AuthOwnLogin    = "OWN_LOGIN"
	AuthAPIKey      = "API_KEY"
	AuthNone        = "NONE"
	AuthUnknown     = "UNKNOWN"

	HealthHealthy     = "HEALTHY"
	HealthDegraded    = "DEGRADED"
	HealthUnavailable = "UNAVAILABLE"
	HealthUnknown     = "UNKNOWN"
)

var (
	supportLevels = []string{
		LevelNative, LevelCompatible, LevelCLI, LevelLocalData,
		LevelUIAutomation, LevelReadOnly, LevelExperimental, LevelUnsupported,
	}
	backends      = []string{BackendNativeAPI, BackendCLI, BackendLocalData, BackendUIAutomation, BackendReadOnly}
	confidences   = []string{ConfidenceHigh, ConfidenceMedium, ConfidenceLow, ConfidenceUnknown}
	deliveryModes = []string{DeliveryStartNow, DeliveryGuide, DeliveryQueue}
	authModes     = []string{AuthReuseClient, AuthOwnLogin, AuthAPIKey, AuthNone, AuthUnknown}
	healthValues  = []string{HealthHealthy, HealthDegraded, HealthUnavailable, HealthUnknown}
)

// Capability is one declared ability, stored as one row of adapter_capabilities
// (§3), so the slice Core keeps has exactly one entry per capability name.
type Capability struct {
	Name             string          `json:"name"`
	SupportLevel     string          `json:"support_level"`
	EffectiveBackend string          `json:"effective_backend"`
	WriteCapable     bool            `json:"write_capable"`
	DeliveryModes    []string        `json:"delivery_modes"`
	Confidence       string          `json:"confidence"`
	Requires         []string        `json:"requires,omitempty"`
	LastVerifiedAt   int64           `json:"last_verified_at"`
	Metadata         json.RawMessage `json:"metadata_json,omitempty"`
}

// InitializeParams is the Core->Adapter handshake request (§2).
type InitializeParams struct {
	Protocol    Version `json:"protocol"`
	CoreVersion string  `json:"core_version"`
}

// InitializeResult is the Adapter->Core handshake reply (§2).
type InitializeResult struct {
	AdapterKey     string  `json:"adapter_key"`
	AdapterVersion string  `json:"adapter_version"`
	Protocol       Version `json:"protocol"`
	// SoftwareVersions is a map, never a string: Phase 0 measured 2-3 independent
	// version lines per product (§2.1), and adapter_instances.software_version keeps
	// this as JSON text for the UI to expand by key.
	SoftwareVersions map[string]string `json:"software_versions"`
	AuthMode         string            `json:"auth_mode"`
	Health           string            `json:"health"`
	Capabilities     []Capability      `json:"capabilities"`
	// WriteEntry false is a legal declaration (Phase 0 §6.1: Z Code has no CLI, no
	// local listener, no API), and makes this Adapter read-only-observable.
	WriteEntry bool `json:"write_entry"`
}

// Negotiated is the outcome of the §2.2 compatibility rule.
type Negotiated struct {
	Version Version
	// Experimental marks "Adapter claims a minor above Core's": allowed, but the
	// caller must record EXPERIMENTAL in audit_entries.
	Experimental bool
}

// Negotiate applies contract §2.2. A different major is refused with no guessing;
// within one major the lower minor wins, and a higher peer minor is accepted but
// flagged. The contract only spells out "minor 高于 Core 已知"; downgrading to a
// lower peer minor is the reading that keeps §2.2's "不猜" promise honest.
func Negotiate(core, peer Version) (Negotiated, error) {
	if core.Major < 0 || core.Minor < 0 || peer.Major < 0 || peer.Minor < 0 {
		return Negotiated{}, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("negative protocol version: core=%v peer=%v", core, peer)}
	}
	if core.Major != peer.Major {
		return Negotiated{}, &Error{Code: ErrProtocolVersionMismatch,
			Message: fmt.Sprintf("protocol major %d, Core speaks %d: refusing to start", peer.Major, core.Major)}
	}
	n := Negotiated{Version: Version{Major: core.Major, Minor: peer.Minor}}
	if peer.Minor > core.Minor {
		n.Version.Minor = core.Minor
		n.Experimental = true
	}
	return n, nil
}

// Handshake is a validated adapter/initialize outcome, and the gate every later
// call passes through. The nil *Handshake is the "not handshaken yet" state, so
// the Core side cannot forget to initialise and quietly reach an Adapter.
type Handshake struct {
	res       InitializeResult
	neg       Negotiated
	effective map[string]Capability
}

// NewHandshake validates a reply against Core's version and the §9 gating. Every
// failure here is a startup rejection - the caller sets health=UNAVAILABLE on
// version mismatch and records the audit entries §9 asks for.
func NewHandshake(core Version, res InitializeResult) (*Handshake, error) {
	neg, err := Negotiate(core, res.Protocol)
	if err != nil {
		return nil, err
	}
	if res.AdapterKey == "" {
		return nil, &Error{Code: ErrInvalidParams, Message: "handshake has no adapter_key"}
	}
	if res.AdapterVersion == "" {
		return nil, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("%s handshake has no adapter_version", res.AdapterKey)}
	}
	if len(res.SoftwareVersions) == 0 {
		return nil, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("%s reports no software_versions; a single string cannot hold 2-3 version lines (§2.1)", res.AdapterKey)}
	}
	if !contains(authModes, res.AuthMode) {
		return nil, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("%s auth_mode %q is not one of %v", res.AdapterKey, res.AuthMode, authModes)}
	}
	if !contains(healthValues, res.Health) {
		return nil, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("%s health %q is not one of %v", res.AdapterKey, res.Health, healthValues)}
	}
	if err := validateCapabilities(res.AdapterKey, res.Capabilities); err != nil {
		return nil, err
	}
	h := &Handshake{res: res, neg: neg}
	h.effective = resolveEffective(res.Capabilities)
	return h, nil
}

// resolveEffective computes 当前有效能力 (§3): a capability is effective when it
// is declared, is not UNSUPPORTED, and everything it requires is effective. The
// loop settles at a fixed point; a require cycle therefore leaves its members
// ineffective rather than hanging or silently enabling them.
func resolveEffective(caps []Capability) map[string]Capability {
	byName := make(map[string]Capability, len(caps))
	for _, c := range caps {
		if c.SupportLevel != LevelUnsupported {
			byName[c.Name] = c
		}
	}
	effective := make(map[string]Capability, len(byName))
	for changed := true; changed; {
		changed = false
		for name, c := range byName {
			if _, done := effective[name]; done || !allEffective(effective, c.Requires) {
				continue
			}
			effective[name] = c
			changed = true
		}
	}
	return effective
}

func allEffective(effective map[string]Capability, requires []string) bool {
	for _, r := range requires {
		if _, ok := effective[r]; !ok {
			return false
		}
	}
	return true
}

// validateCapabilities checks the enum sets, one-row-per-capability, and the §9
// rule that debug.* may only be declared by the fake Adapter at EXPERIMENTAL.
func validateCapabilities(adapterKey string, caps []Capability) error {
	seen := map[string]bool{}
	for _, c := range caps {
		where := fmt.Sprintf("%s capability %q", adapterKey, c.Name)
		if c.Name == "" {
			return &Error{Code: ErrInvalidParams, Message: where + " has no name"}
		}
		if seen[c.Name] {
			return &Error{Code: ErrInvalidParams, Message: where + " is declared twice (adapter_capabilities holds one row per capability)"}
		}
		seen[c.Name] = true
		if isDebugName(c.Name) && (adapterKey != "fake" || c.SupportLevel != LevelExperimental) {
			return &Error{Code: ErrInvalidParams,
				Message: where + " declares debug.* support_level=" + c.SupportLevel +
					"; only adapter_key=fake may at EXPERIMENTAL (contract §9)"}
		}
		if !contains(supportLevels, c.SupportLevel) {
			return &Error{Code: ErrInvalidParams, Message: where + " support_level " + c.SupportLevel +
				" is not one of the eight frozen levels"}
		}
		if !contains(backends, c.EffectiveBackend) {
			return &Error{Code: ErrInvalidParams, Message: where + " effective_backend " +
				c.EffectiveBackend + " is not one of " + fmt.Sprint(backends)}
		}
		if !contains(confidences, c.Confidence) {
			return &Error{Code: ErrInvalidParams, Message: where + " confidence " + c.Confidence +
				" is not one of " + fmt.Sprint(confidences)}
		}
		if c.SupportLevel != LevelUnsupported && c.LastVerifiedAt <= 0 {
			return &Error{Code: ErrInvalidParams,
				Message: where + " declares no last_verified_at; §3 requires it for every live level, UIA included"}
		}
		for i, m := range c.DeliveryModes {
			if !contains(deliveryModes, m) {
				return &Error{Code: ErrInvalidParams,
					Message: where + " delivery_modes[" + fmt.Sprint(i) + "] " + m + " is not one of " + fmt.Sprint(deliveryModes)}
			}
			for _, other := range c.DeliveryModes[i+1:] {
				if other == m {
					return &Error{Code: ErrInvalidParams, Message: where + " declares delivery mode " + m + " twice"}
				}
			}
		}
	}
	return nil
}

// isDebugName recognises both spellings the contract uses - capability names are
// dotted (§3), method names slashed (§4) - because gating on one of them alone
// would leave a safety hole. CO-6.
func isDebugName(name string) bool {
	return strings.HasPrefix(name, "debug.") || strings.HasPrefix(name, "debug/")
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// Result exposes the verified handshake, including write_entry for the UI: with
// write_entry false no write affordance may be rendered (§3).
func (h *Handshake) Result() InitializeResult {
	if h == nil {
		return InitializeResult{}
	}
	return h.res
}

// Version is the negotiated protocol version.
func (h *Handshake) Version() Version {
	if h == nil {
		return Version{}
	}
	return h.neg.Version
}

// Experimental reports the §2.2 case the caller must record in audit_entries.
func (h *Handshake) Experimental() bool { return h != nil && h.neg.Experimental }

// Effective returns the capability as Core currently sees it, if any. §3: Core
// consumes only 当前有效能力, and a capability is ineffective when it is
// UNSUPPORTED or when anything it requires is.
func (h *Handshake) Effective(name string) (Capability, bool) {
	if h == nil {
		return Capability{}, false
	}
	c, ok := h.effective[name]
	return c, ok
}

// EffectiveCapabilities lists what Core may use, for the capability panel.
func (h *Handshake) EffectiveCapabilities() []Capability {
	if h == nil {
		return nil
	}
	out := make([]Capability, 0, len(h.effective))
	for _, c := range h.res.Capabilities {
		if _, ok := h.effective[c.Name]; ok {
			out = append(out, c)
		}
	}
	return out
}

// Call is the gate in front of every method after the handshake (§2/§3).
func (h *Handshake) Call(method string) error {
	// The call that opens the gate must pass through it, and only once: §2.2
	// negotiates the version at handshake and never changes it at runtime. Both rules
	// live here because a caller that forgets the exemption can never handshake, and
	// a caller that forgets the once-only rule can re-negotiate under a peer's feet.
	if method == MethodInitialize {
		if h != nil {
			return &Error{Code: ErrInvalidParams,
				Message: "adapter/initialize already completed; §2.2 does not re-negotiate the protocol at runtime"}
		}
		return nil
	}
	if h == nil {
		return &Error{Code: ErrProtocolNotInitialized,
			Message: method + ": adapter/initialize has not completed (contract §2)"}
	}
	if !h.res.WriteEntry && isWriteMethod(method) {
		// §3: write_entry false means observation only. Falling back to clicking
		// the UI while reporting success is a P0; it is allowed only when the
		// Adapter itself declared that capability at UI_AUTOMATION.
		if c, ok := h.Effective(capabilityName(method)); !ok ||
			c.SupportLevel != LevelUIAutomation || !c.WriteCapable {
			return &Error{Code: ErrWriteEntryNone,
				Message: method + ": adapter " + h.res.AdapterKey + " declares write_entry=false"}
		}
	}
	return nil
}

// Delivery gates a session/send on the §5 tri-state. An undeclared mode is an
// explicit refusal: quietly queueing while the user believes they interrupted the
// turn is the same class of mis-delivery accident as §12's "wrong session".
func (h *Handshake) Delivery(method, mode string) error {
	if err := h.Call(method); err != nil {
		return err
	}
	c, ok := h.Effective(capabilityName(method))
	if !ok {
		return &Error{Code: ErrCapabilityUnavailable, Message: method + " is not an effective capability"}
	}
	if !contains(c.DeliveryModes, mode) {
		return &Error{Code: ErrDeliveryModeUnsupported,
			Message: fmt.Sprintf("%s does not declare delivery mode %q; it declares %v",
				method, mode, c.DeliveryModes)}
	}
	return nil
}

// writeMethods is §4.1's write set, so write_entry:false must refuse each of them (§3).
//
// approval/respond is in here per §4.1's Q1 ruling, which reverses the reason this list
// used to give for leaving it out: answering an approval authorises an external action,
// and Permission Engine's gate ("should Core ask at all") cannot stand in for
// write_entry's ("can this Adapter do anything"). debug/* is covered by isDebugName.
//
// session/create became a §4 row at REQ-T8's close (13:39, written from this package's
// CreateSessionParams/CreateSessionResult). It is gated because creating a remote
// session is the least reversible thing an Adapter can be asked to do. Note §4.1's own
// write-set sentence lists it now (CO-34's fourth fix, 14:39) - and the reason that
// mattered is worth keeping: when the row existed in §4 but not in §4.1, anyone aligning
// writeMethods to §4.1 verbatim would have dropped a "create a remote session" call
// out of §3's gate. The contract and this map are cross-checked at runtime by
// contract_table_test.go so the two cannot drift apart again silently.
//
// model/select stays listed per §4.1's last paragraph: its refusal is really the
// capability gate, but an Adapter that declares write_entry=false must still mean it.
var writeMethods = map[string]bool{
	"session/send":     true,
	"session/stop":     true,
	"session/create":   true, // §4, REQ-T8 closed
	"model/select":     true,
	"approval/respond": true, // §4.1, Q1
}

func isWriteMethod(method string) bool {
	return writeMethods[method] || isDebugName(method)
}

// HealthResult is the adapter/health reply (CO-10).
type HealthResult struct {
	Health     string `json:"health"`
	Confidence string `json:"confidence"`
	CheckedAt  int64  `json:"checked_at"`
}

// Validate applies CO-10: UNKNOWN must not be dressed up as HEALTHY. The definite
// states are refused whenever confidence is UNKNOWN, which generalises the ruling
// "也不得用 HEALTHY 冒充 DEGRADED" to the form Core can actually detect - a firm
// claim with no confidence. CO-12 asks 主控 to confirm the generalisation.
func (h HealthResult) Validate() error {
	if !contains(healthValues, h.Health) {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("health %q is not one of %v", h.Health, healthValues)}
	}
	if !contains(confidences, h.Confidence) {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("health %q confidence %q is not one of %v", h.Health, h.Confidence, confidences)}
	}
	if h.CheckedAt <= 0 {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("health %q has checked_at %d, want a Unix ms timestamp: an undated reading cannot be re-checked",
				h.Health, h.CheckedAt)}
	}
	if h.Health != HealthUnknown && h.Confidence == ConfidenceUnknown {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("health %q with confidence UNKNOWN is CO-10's forbidden impersonation; report health UNKNOWN instead",
				h.Health)}
	}
	return nil
}

// capabilityName maps a method to its capability row: session/send -> session.send.
// CO-6: the contract spells both names from the same pair of words.
func capabilityName(method string) string {
	return strings.ReplaceAll(method, "/", ".")
}
