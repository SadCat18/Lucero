package adapter

import (
	"encoding/json"
	"fmt"
	"math"
)

// Observation kinds (§7). A predicted value must never arrive as OBSERVED.
const (
	ObservationObserved  = "OBSERVED"
	ObservationEstimated = "ESTIMATED"
)

var observationKinds = []string{ObservationObserved, ObservationEstimated}

// percentTolerance absorbs the rounding a Provider does when it publishes used and
// remaining independently (62.5 + 37.5 is exact, 62.55 + 37.45 is not).
const percentTolerance = 1.0

// QuotaObservation is one §7 row. The numeric fields are pointers because §7
// demands "取不到就 null": a plain float64 cannot tell "no data" from 0, which is
// exactly the P0 this section exists to stop.
type QuotaObservation struct {
	QuotaKey         string          `json:"quota_key"`
	QuotaType        string          `json:"quota_type"`
	ObservationKind  string          `json:"observation_kind"`
	UsedPercent      *float64        `json:"used_percent"`
	RemainingPercent *float64        `json:"remaining_percent"`
	ResetAt          *int64          `json:"reset_at"`
	WindowStart      *int64          `json:"window_start"`
	WindowEnd        *int64          `json:"window_end"`
	Confidence       string          `json:"confidence"`
	Source           string          `json:"source"`
	ProviderRaw      json.RawMessage `json:"provider_raw,omitempty"`
}

// QuotaResult is the quota/read reply (§7).
type QuotaResult struct {
	Observations      []QuotaObservation `json:"observations"`
	OverallConfidence string             `json:"overall_confidence"`
}

// UnknownQuota is the reply an Adapter gives when it cannot read anything at all:
// no observations, overall UNKNOWN, and never a zero (§7's last bullet).
func UnknownQuota() QuotaResult {
	return QuotaResult{Observations: []QuotaObservation{}, OverallConfidence: ConfidenceUnknown}
}

// Validate enforces §7, where UNKNOWN and 0 are two different facts. The
// all-zero row is not a style complaint: Phase 0 measured Qoder publishing a
// complete message.usage whose every value is 0, and reporting that as OBSERVED 0%
// is handoff §12's "无法确认状态时却显示确定的 0 quota".
func (q QuotaResult) Validate() error {
	if !contains(confidences, q.OverallConfidence) {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("quota overall_confidence %q is not one of %v", q.OverallConfidence, confidences)}
	}
	if len(q.Observations) == 0 {
		if q.OverallConfidence != ConfidenceUnknown {
			return &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("quota read with no observations must report overall_confidence UNKNOWN, got %s (§7)",
					q.OverallConfidence)}
		}
		return nil
	}
	if q.OverallConfidence == ConfidenceUnknown {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("quota reports %d observations under overall_confidence UNKNOWN; §7 reserves UNKNOWN for the empty reply",
				len(q.Observations))}
	}
	for i, o := range q.Observations {
		if err := o.validate(); err != nil {
			return fmt.Errorf("quota observations[%d] (%s): %w", i, o.QuotaKey, err)
		}
	}
	return nil
}

func (o QuotaObservation) validate() error {
	if o.QuotaKey == "" {
		return fmt.Errorf("quota_key is empty")
	}
	// QuotaType has no frozen value set in any contract (CO-17), so it is only
	// required to be present; inventing one here would quietly reject real Providers.
	if o.QuotaType == "" {
		return fmt.Errorf("quota_type is empty")
	}
	if !contains(observationKinds, o.ObservationKind) {
		return fmt.Errorf("observation_kind %q is not one of %v", o.ObservationKind, observationKinds)
	}
	if !contains(confidences, o.Confidence) {
		return fmt.Errorf("confidence %q is not one of %v", o.Confidence, confidences)
	}
	if o.Source == "" {
		return fmt.Errorf("source is empty: an observation with no provenance cannot be re-verified")
	}
	if o.UsedPercent == nil && o.RemainingPercent == nil && o.ResetAt == nil &&
		o.WindowStart == nil && o.WindowEnd == nil {
		return fmt.Errorf("carries no value at all; §7 says an unreadable field is null with lower confidence, "+
			"and a wholly unreadable read is observations:[] with overall_confidence UNKNOWN, not an empty row (confidence %s)",
			o.Confidence)
	}
	for _, f := range []struct {
		name string
		val  *float64
	}{{"used_percent", o.UsedPercent}, {"remaining_percent", o.RemainingPercent}} {
		if f.val == nil {
			continue
		}
		if *f.val < 0 || *f.val > 100 {
			return fmt.Errorf("%s %v is outside 0..100", f.name, *f.val)
		}
	}
	if o.UsedPercent != nil && o.RemainingPercent != nil &&
		math.Abs(*o.UsedPercent+*o.RemainingPercent-100) > percentTolerance {
		return fmt.Errorf("used_percent %v + remaining_percent %v is not a full window; "+
			"a Provider that knows nothing reports both as 0, and §7 requires that be UNKNOWN, not OBSERVED",
			*o.UsedPercent, *o.RemainingPercent)
	}
	for _, f := range []struct {
		name string
		val  *int64
	}{{"reset_at", o.ResetAt}, {"window_start", o.WindowStart}, {"window_end", o.WindowEnd}} {
		if f.val != nil && *f.val <= 0 {
			return fmt.Errorf("%s %d is not a Unix millisecond timestamp; 0 must never stand in for \"unknown\"",
				f.name, *f.val)
		}
	}
	if o.WindowStart != nil && o.WindowEnd != nil && *o.WindowEnd <= *o.WindowStart {
		return fmt.Errorf("window_end %d is not after window_start %d", *o.WindowEnd, *o.WindowStart)
	}
	return nil
}
