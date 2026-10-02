package main

// quota.go answers quota/read the only way §7 and §16.4 both allow today.
//
// §16.4's three tiers are a rule about *confidence*, and it is implemented here in
// full: cost > 0 reads as HIGH, cost = 0 reads as LOW because "free" and "not reported"
// are indistinguishable at that value, and a NULL produces no observation at all. That
// last part is the §7 sentence this file exists to obey - 严禁用 0 表示"没有额度数据".
//
// What is *not* expressible is the number. QuotaObservation's value fields are
// used_percent / remaining_percent / reset_at / window_start / window_end (§7), and this
// provider publishes money and tokens with no budget denominator to turn into a
// percentage. Putting cost 0.0053 into used_percent would be a lie with the right type;
// putting used=0/remaining=100 for a zero-cost row would claim an account balance nobody
// measured, which is the exact P0 §7 names. So the reply today is §7's own empty
// answer - observations: [] with overall_confidence UNKNOWN - and the missing field is
// CO-66 for the 主控 to rule on. The tier computation still runs and is logged, so the
// day a value field exists the mapping is already tested rather than written fresh.

import (
	"context"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// spendSummary is what this machine's session table actually says about cost. It is
// internal (logged, not published) until CO-66 gives §7 a field that can carry money.
type spendSummary struct {
	greaterThanZero int
	equalZero       int
	null            int
	// noCostColumn is the "this store does not have the column at all" state, kept apart
	// from "the column is NULL everywhere": the first means the schema moved, the second
	// is a real measurement (§7's UNKNOWN 与 0 分家的另一层).
	noCostColumn bool
}

func (s spendSummary) String() string {
	if s.noCostColumn {
		return "session.cost column absent"
	}
	return fmt.Sprintf("cost>0=%d cost=0=%d cost NULL=%d", s.greaterThanZero, s.equalZero, s.null)
}

// costConfidence is §16.4's tier rule, as a pure function so the rule itself - not just
// the reply that carries it - has a criterion behind it. `produce` false is the NULL
// leg: no observation, not an observation of zero.
func costConfidence(v float64, isNull bool) (confidence string, produce bool) {
	if isNull {
		return "", false
	}
	if v > 0 {
		return adapter.ConfidenceHigh, true
	}
	return adapter.ConfidenceLow, true
}

// readSpend counts the three states of the provider's own cost column. It asks the file
// which column exists instead of assuming the name, because the profile records
// session.cost but not its exact spelling in this build (CO-64).
func (s *sqlStore) readSpend(ctx context.Context) (spendSummary, error) {
	col, err := s.firstColumn(ctx, "session", "cost", "total_cost", "spend")
	if err != nil {
		return spendSummary{}, err
	}
	if col == "" {
		return spendSummary{noCostColumn: true}, nil
	}
	rows, err := s.query(ctx, fmt.Sprintf("SELECT %s AS cost FROM session", quoteIdent(col)))
	if err != nil {
		return spendSummary{}, err
	}
	var out spendSummary
	for _, r := range rows {
		c := r["cost"]
		if c.null || !c.isNum {
			out.null++
			continue
		}
		n, _ := c.number()
		if n > 0 {
			out.greaterThanZero++
		} else {
			out.equalZero++
		}
	}
	return out, nil
}

// quotaRead returns §7's empty answer and says why on stderr. Returning a fabricated
// row would be the "看起来一切正常" failure §12 records as a P0, and returning an error
// would hide that the store itself is readable.
func (o *opencode) quotaRead(ctx context.Context, req *adapter.Frame) error {
	s, err := o.ensureStore()
	if err != nil {
		return o.reply(errReply(req, storeUnavailable(err)))
	}
	spend, err := s.readSpend(ctx)
	if err != nil {
		return o.reply(errReply(req, storeUnavailable(err)))
	}
	// The tiers are computed even though nothing carries them today, so a mutation that
	// mislabels a 0 as HIGH has something to bite (TestQuotaTiersFollowContract164).
	for i := 0; i < spend.greaterThanZero; i++ {
		if c, ok := costConfidence(0.01, false); !ok || c != adapter.ConfidenceHigh {
			fmt.Fprintf(o.logs, "quota tier rule broken for cost>0: %s/%v\n", c, ok)
		}
	}
	for i := 0; i < spend.equalZero; i++ {
		if c, ok := costConfidence(0, false); !ok || c != adapter.ConfidenceLow {
			fmt.Fprintf(o.logs, "quota tier rule broken for cost=0: %s/%v\n", c, ok)
		}
	}
	if _, ok := costConfidence(0, true); ok {
		fmt.Fprintf(o.logs, "quota tier rule broken for NULL: a null produced an observation\n")
	}
	fmt.Fprintf(o.logs, "quota/read: %s; §7 carries no money field (CO-66) so the reply is observations:[] + UNKNOWN\n", spend)
	return o.reply(result(req, adapter.UnknownQuota()))
}
