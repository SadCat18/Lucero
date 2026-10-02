package main

// declaration.go is the tier's whole identity: what it claims, and how it reports being
// alive. §16.7 第 3 条 says the read-only nature travels in the declaration and nowhere
// else ("只读性由档位表达，不由状态表达"), so this file is either where that ruling is
// honoured or where a later reader quietly loses it.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// coreVersionAssumed is what the startup self-check negotiates against. It is not a
// claim about Core's build: the real version arrives with adapter/initialize and o.hs is
// built from that (see initialize). This one only answers "would Core accept our
// capability table at all", which must be known before the first request.
var coreVersionAssumed = adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}

// observedSoftwareVersionDefault is the product version the profile measured
// (1.18.32, FileVersion/ProductVersion double-checked). It is a default and not a
// constant in the declaration because §16.5 makes the version a dispatch input: reading
// it off the installed binary would mean touching a third party's files beyond the store
// §16.7's range allows, so the operator supplies it (LUCERO_OPENCODE_VERSION).
const observedSoftwareVersionDefault = "1.18.32"

// lastVerifiedAt is §3's mandatory "when did anyone last check this" stamp for every
// live capability. The value is the survey role's own read of this machine's store,
// recorded in docs/adapters/phase0/opencode-integration-profile.md at
// 2026-10-01T23:01:16+08:00 = 1790866876000 ms.
//
// Two honest caveats, both reasons not to invent something fresher:
//   - the profile writes that read as a 23:0x window; 23:01:16 is the one instant in it
//     stamped to the second, so this is the conservative end of a window and not a
//     measurement this process took;
//   - nobody has re-verified the schema since, and the message/session column list is
//     still missing from the profile (CO-64 asks survey for the DDL). That is why every
//     capability below is declared at MEDIUM rather than HIGH: a half-known shape is not
//     a verified one.
const lastVerifiedAt = int64(1790866876000)

// readOnlyCapability is the single shape a §16.7 declaration takes here, so a fifth read
// method cannot be added later with a write_capable flag by accident.
//
// support_level = LevelReadOnly is deliberate and load-bearing twice over: §16.7 第 1 条
// names the tier, and core/test/read_only_tier_test.go decides which packages to scan by
// finding that constant used in a declaration (its own comment records that the
// vocabulary package internal/adapter is not a subject - only a user is).
//
// effective_backend = local_data is the other half of the same honesty: the mechanism
// really is "we read this product's own database". Using read_only there would describe
// the policy, not the source, and §3 keeps that column to answer "how".
func readOnlyCapability(name, confidence, reason string) adapter.Capability {
	return adapter.Capability{
		Name:             name,
		SupportLevel:     adapter.LevelReadOnly,
		EffectiveBackend: adapter.BackendLocalData,
		WriteCapable:     false,
		Confidence:       confidence,
		LastVerifiedAt:   lastVerifiedAt,
		Metadata:         json.RawMessage(`{"schema":1,"tier":"READ_ONLY","reason":` + quoteJSON(reason) + `}`),
	}
}

// initializeResult is §2's reply. Three parts of it carry the ruling and are the ones a
// later reader is most likely to "improve" into a lie:
//
//   - WriteEntry=false: §3 then makes every write method answer WRITE_ENTRY_NONE, and
//     internal/adapter's own gate implements that with §4.1's set, so this adapter
//     refuses through the same table Core applies rather than a private copy;
//   - exactly four live capabilities, all at READ_ONLY: §16.7 第 1 条's refused list is
//     therefore absent from the offer, which is what makes "这一家不进派发路径" checkable
//     from Core without reading this file;
//   - software_versions is a map with two lines, because §2.1 exists for products that
//     carry 2-3 independent version strings.
func initializeResult(observed string) adapter.InitializeResult {
	if observed == "" {
		observed = observedSoftwareVersionDefault
	}
	return adapter.InitializeResult{
		AdapterKey:       adapterKey,
		AdapterVersion:   adapterVersion,
		Protocol:         adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor},
		SoftwareVersions: map[string]string{"opencode": observed, "codec": "internal/adapter"},
		AuthMode:         adapter.AuthNone,
		Health:           adapter.HealthHealthy,
		WriteEntry:       false,
		Capabilities: []adapter.Capability{
			readOnlyCapability("session.list", adapter.ConfidenceMedium,
				"session table read locally; profile §④ measured 24 rows, title column not recorded (CO-64)"),
			readOnlyCapability("session.read", adapter.ConfidenceMedium,
				"transcript rebuilt from the event journal; deltas are adapter-synthesized (§16.2), "+
					"message role/title have no recorded source (CO-64)"),
			readOnlyCapability("model.list", adapter.ConfidenceMedium,
				"cache-file channel only (§16.5): /provider and /config/providers measured 401"),
			readOnlyCapability("quota.read", adapter.ConfidenceMedium,
				"session cost/tokens readable; §7 has no field to carry an amount (CO-66), so the "+
					"reply is observations:[] + overall UNKNOWN"),
			// §4 keeps session/locate a v0.1 stub. Declaring it UNSUPPORTED is what makes
			// the CAPABILITY_UNAVAILABLE answer the correct implementation rather than an
			// undeclared gap.
			{Name: "session.locate", SupportLevel: adapter.LevelUnsupported,
				EffectiveBackend: adapter.BackendReadOnly, Confidence: adapter.ConfidenceUnknown,
				Metadata: json.RawMessage(`{"reason":"Phase 0 §5: deep links unverified for all five products"}`)},
			// approval/list is answered (empty, honestly) but is not a live capability: the
			// provider's permission table measured 0 rows and approvals never reach its
			// journal (§16.7 第 5 条), so declaring it usable would advertise an offer with
			// nothing behind it.
			{Name: "approval.list", SupportLevel: adapter.LevelUnsupported,
				EffectiveBackend: adapter.BackendLocalData, Confidence: adapter.ConfidenceUnknown,
				Metadata: json.RawMessage(`{"reason":"permission table measured 0 rows (profile §③)"}`)},
		},
	}
}

// health answers adapter/health by trying the only dependency this tier has: the
// read-only open of the store. §4 forbids dressing UNKNOWN up as HEALTHY, so an
// unreadable store is reported as HEALTH_UNKNOWN with the reason on stderr - never as a
// healthy reader of an empty world.
func (o *opencode) health(req *adapter.Frame) error {
	res := adapter.HealthResult{Health: adapter.HealthHealthy,
		Confidence: adapter.ConfidenceHigh, CheckedAt: o.cfg.now()}
	s, err := o.ensureStore()
	if err != nil {
		fmt.Fprintf(o.logs, "health: store unreadable (%v) - reporting HEALTH_UNKNOWN, not HEALTHY\n", err)
		res.Health, res.Confidence = adapter.HealthUnknown, adapter.ConfidenceMedium
	} else if err := s.ping(context.Background()); err != nil {
		fmt.Fprintf(o.logs, "health: store opened, probe query failed (%v)\n", err)
		res.Health, res.Confidence = adapter.HealthUnknown, adapter.ConfidenceMedium
	}
	return o.reply(result(req, res))
}

func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
