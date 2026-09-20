package adapter

import (
	"encoding/json"
	"strings"
	"testing"
)

// contractSample is the quota/read example copied verbatim out of contract §7, so
// the struct is pinned against the contract's own words rather than a paraphrase.
const contractSample = `{"observations":[
  {"quota_key":"weekly","quota_type":"PERCENT","observation_kind":"OBSERVED",
   "used_percent":62.5,"remaining_percent":37.5,
   "reset_at":1789900000000,"window_start":null,"window_end":null,
   "confidence":"HIGH","source":"app_server/GetAccountRateLimits","provider_raw":{}}
], "overall_confidence":"HIGH"}`

func TestQuotaValidate(t *testing.T) {
	var nominal QuotaResult
	if err := json.Unmarshal([]byte(contractSample), &nominal); err != nil {
		t.Fatalf("the contract's own §7 sample does not decode: %v", err)
	}
	if err := nominal.Validate(); err != nil {
		t.Fatalf("§7 sample rejected: %v", err)
	}
	if len(nominal.Observations) != 1 || nominal.Observations[0].WindowStart != nil {
		t.Fatalf("§7 sample decoded wrong: %+v", nominal.Observations)
	}

	row := func(mut func(*QuotaObservation)) QuotaResult {
		o := QuotaObservation{
			QuotaKey: "weekly", QuotaType: "PERCENT", ObservationKind: ObservationObserved,
			UsedPercent: f64(62.5), RemainingPercent: f64(37.5), ResetAt: i64(1789900000000),
			Confidence: ConfidenceHigh, Source: "app_server/GetAccountRateLimits",
		}
		mut(&o)
		return QuotaResult{Observations: []QuotaObservation{o}, OverallConfidence: ConfidenceHigh}
	}

	observationsUnderUnknown := row(func(*QuotaObservation) {})
	observationsUnderUnknown.OverallConfidence = ConfidenceUnknown

	cases := []struct {
		name    string
		q       QuotaResult
		wantErr string // "" means the reply is legal
	}{
		{"nothing readable", UnknownQuota(), ""},
		{"empty observations claim confidence", QuotaResult{OverallConfidence: ConfidenceHigh}, "UNKNOWN"},
		{"bad overall confidence", QuotaResult{OverallConfidence: "SUNNY"}, "overall_confidence"},
		{"observations under UNKNOWN overall", observationsUnderUnknown, "overall_confidence"},
		// The measured Qoder case: every usage field present, every value 0.
		{"all-zero is not \"no data\"", row(func(o *QuotaObservation) {
			o.UsedPercent, o.RemainingPercent = f64(0), f64(0)
		}), "full window"},
		{"one-sided zero is not \"no data\" either", row(func(o *QuotaObservation) {
			o.UsedPercent, o.RemainingPercent, o.ResetAt = f64(0), f64(0), nil
		}), "full window"},
		{"genuinely exhausted stays legal", row(func(o *QuotaObservation) {
			o.UsedPercent, o.RemainingPercent = f64(100), f64(0)
		}), ""},
		{"unused stays legal", row(func(o *QuotaObservation) {
			o.UsedPercent, o.RemainingPercent = f64(0), f64(100)
		}), ""},
		{"one field unknown is null not zero", row(func(o *QuotaObservation) {
			o.RemainingPercent, o.Confidence = nil, ConfidenceMedium
		}), ""},
		{"row with no value at all", row(func(o *QuotaObservation) {
			o.UsedPercent, o.RemainingPercent, o.ResetAt = nil, nil, nil
		}), "no value at all"},
		{"percent out of range", row(func(o *QuotaObservation) { o.UsedPercent = f64(150) }), "outside 0..100"},
		{"negative percent", row(func(o *QuotaObservation) { o.UsedPercent = f64(-1) }), "outside 0..100"},
		{"reset_at zero", row(func(o *QuotaObservation) { o.ResetAt = i64(0) }), "reset_at"},
		{"window inverted", row(func(o *QuotaObservation) {
			o.WindowStart, o.WindowEnd = i64(2000), i64(1000)
		}), "window_end"},
		{"window equal", row(func(o *QuotaObservation) {
			o.WindowStart, o.WindowEnd = i64(1000), i64(1000)
		}), "window_end"},
		{"estimated is legal as estimated", row(func(o *QuotaObservation) {
			o.ObservationKind, o.Confidence = ObservationEstimated, ConfidenceLow
		}), ""},
		{"invented observation kind", row(func(o *QuotaObservation) { o.ObservationKind = "GUESSED" }), "observation_kind"},
		{"no provenance", row(func(o *QuotaObservation) { o.Source = "" }), "source"},
		{"no quota_key", row(func(o *QuotaObservation) { o.QuotaKey = "" }), "quota_key"},
		{"no quota_type", row(func(o *QuotaObservation) { o.QuotaType = "" }), "quota_type"},
	}
	for _, c := range cases {
		err := c.q.Validate()
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: Validate = %v, want legal", c.name, err)
		case c.wantErr != "" && err == nil:
			t.Errorf("%s: Validate = nil, want refusal naming %q", c.name, c.wantErr)
		case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
			t.Errorf("%s: Validate = %v, want it to name %q", c.name, err, c.wantErr)
		}
	}
}

// TestQuotaFieldsDistinguishNullFromZero is §7 at the wire level: an absent or
// null number must arrive as nil, because a plain float64 would read as "used up".
func TestQuotaFieldsDistinguishNullFromZero(t *testing.T) {
	const doc = `{"observations":[{"quota_key":"weekly","quota_type":"PERCENT",
		"observation_kind":"OBSERVED","used_percent":null,"remaining_percent":62.5,
		"confidence":"HIGH","source":"cli/quota --json"}],"overall_confidence":"HIGH"}`
	var q QuotaResult
	if err := json.Unmarshal([]byte(doc), &q); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := q.Validate(); err != nil {
		t.Fatalf("explicit nulls were refused: %v", err)
	}
	o := q.Observations[0]
	if o.UsedPercent != nil {
		t.Errorf("used_percent null decoded as %v, want nil", *o.UsedPercent)
	}
	if o.RemainingPercent == nil || *o.RemainingPercent != 62.5 {
		t.Errorf("remaining_percent = %v, want 62.5", o.RemainingPercent)
	}
	if o.ResetAt != nil || o.WindowStart != nil || o.WindowEnd != nil {
		t.Error("absent timestamps must stay nil, not become 0")
	}
}

func TestUnknownQuotaWireForm(t *testing.T) {
	raw, err := json.Marshal(UnknownQuota())
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"observations":[],"overall_confidence":"UNKNOWN"}`
	if string(raw) != want {
		t.Errorf("UnknownQuota() = %s, want %s", raw, want)
	}
}

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }
