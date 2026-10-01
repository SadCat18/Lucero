package permission

// T-438 ① — contract ① §3.0-d says approval.Draft.RiskLevel may only come from
// THIS package's built-in classifier, and neither transport nor backend may keep a
// second copy of the word lists. Two things have to be true for that to be provable
// rather than merely requested, and each gets its own case here:
//
//   1. The exported door and the chain's last rung are the same rule set. If
//      defaultFor walked the lists a second time, an action could be gated one way
//      and labelled another, and nothing on disk would show the split.
//   2. The values the door emits are legal where they land. approvals.risk_level has
//      a CHECK with a closed list, so an invented label fails at INSERT — loudly in
//      production instead of loudly in a test.
//
// These cases read the authoritative files at runtime instead of embedding a copy:
// a snapshot of a table someone else owns goes stale silently (the rule this role
// learned the hard way on redaction vocabulary).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// classifierEntries is every action the built-in lists can answer for, taken from
// the lists themselves. Ranging over the data rather than a hand-copied expectation
// is what keeps this honest: someone registering a new action moves both sides of
// every assertion below together.
func classifierEntries() []string {
	var out []string
	out = append(out, highRiskActions...)
	out = append(out, lowMediumActions...)
	return out
}

// TestRiskLevelAndTheChainsLastRungAgree is the anti-drift case. defaultFor is
// unexported on purpose — it is the rung Decide falls through to — so the only way
// to prove the door and the gate read one rule set is to compare their answers for
// every entry the lists carry.
func TestRiskLevelAndTheChainsLastRungAgree(t *testing.T) {
	high, medium, unknown := 0, 0, 0
	for _, action := range classifierEntries() {
		level, registered := RiskLevel(action)
		effect, reason := defaultFor(action)
		if !registered {
			t.Errorf("RiskLevel(%q) says unregistered, but a built-in list carries it", action)
			continue
		}
		switch level {
		case RiskHigh:
			high++
			if effect != RequireApproval || reason != ReasonDefaultHighRisk {
				t.Errorf("%q labelled HIGH but gated as %s/%s", action, effect, reason)
			}
		case RiskMedium:
			medium++
			if effect != Allow || reason != ReasonDefaultAllow {
				t.Errorf("%q labelled %s but gated as %s/%s", action, level, effect, reason)
			}
		default:
			t.Errorf("%q is in a built-in list but RiskLevel answered %q", action, level)
		}
	}
	// Both lists must actually be represented, or the loop above could be proving
	// agreement over one bucket and looking green while doing it.
	if high == 0 || medium == 0 {
		t.Fatalf("the sweep covered high=%d medium=%d; both buckets need entries to mean anything", high, medium)
	}

	// Unregistered is the third answer, and it must fail closed.
	for _, action := range []string{"brand.new.thing", "", "filesystem.rea", "remote", "deploy."} {
		level, registered := RiskLevel(action)
		if registered || level != RiskUnknown {
			t.Errorf("RiskLevel(%q) = (%q, %v), want (%q, false) for an unregistered action", action, level, registered, RiskUnknown)
		}
		if effect, reason := defaultFor(action); effect != RequireApproval || reason != ReasonDefaultUnknown {
			t.Errorf("%q is unregistered but the chain answered %s/%s — unknown must gate", action, effect, reason)
		}
		unknown++
	}
	if unknown == 0 {
		t.Fatal("no unregistered action was judged: the fail-closed half of this rule went untested")
	}
}

// TestRiskLevelPatternsAreTheOnesTheListsDeclare pins the wildcard entries
// ("remote.*", "adapter.debug.*"): the door has to answer for the actions the
// patterns cover, not only for the pattern text. If the door ever matched on the
// literal string instead of the pattern, "remote.exec" would be UNKNOWN while the
// chain still gated it — the exact split §3.0-d is trying to prevent.
func TestRiskLevelPatternsAreTheOnesTheListsDeclare(t *testing.T) {
	for _, action := range []string{"remote.exec", "remote.read", "adapter.debug.dump", "adapter.debug.attach"} {
		level, registered := RiskLevel(action)
		if !registered || level != RiskHigh {
			t.Errorf("RiskLevel(%q) = (%q, %v), want (%q, true): a wildcard entry covers it", action, level, registered, RiskHigh)
		}
		if effect, reason := defaultFor(action); effect != RequireApproval || reason != ReasonDefaultHighRisk {
			t.Errorf("%q matched a high-risk pattern but the chain answered %s/%s", action, effect, reason)
		}
	}
	// And the near misses stay unregistered: "remote" alone is not "remote.something".
	for _, action := range []string{"remote", "adapter.debug", "filesystem.wri"} {
		if level, registered := RiskLevel(action); registered {
			t.Errorf("RiskLevel(%q) = (%q, true); a prefix of a wildcard pattern is not a match", action, level)
		}
	}
}

// approvalsCheckRE pulls the closed list out of approvals.risk_level's CHECK
// constraint. It targets the approvals table only: tasks.risk_level carries the same
// five words, and matching the first CHECK in the file would let this test pass even
// if the approvals column were narrowed.
var approvalsCheckRE = regexp.MustCompile(`(?s)CREATE TABLE approvals .*?\brisk_level\s+TEXT NOT NULL[\s\S]*?CHECK \(risk_level IN \(([^)]*)\)`)

// TestRiskLevelAnswersAreLegalWhereTheyLand reads the migration rather than embedding
// the vocabulary, so the day db narrows that CHECK the door goes red here instead of
// handing transport a value that dies at INSERT.
func TestRiskLevelAnswersAreLegalWhereTheyLand(t *testing.T) {
	path := filepath.Join("..", "..", "migrations", "000001_core_schema.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the authoritative column definition at %s: %v", path, err)
	}
	body := strings.ReplaceAll(string(raw), "\r", "") // this repo's specs/migrations carry CRLF
	m := approvalsCheckRE.FindStringSubmatch(body)
	if m == nil {
		// An empty set here would make every assertion below vacuously true, which is
		// the failure mode this role has been burned by before.
		t.Fatal("the approvals risk_level CHECK was not parsed — the extractor is broken, not the schema")
	}
	legal := map[string]bool{}
	for _, v := range strings.Split(m[1], ",") {
		v = strings.TrimSpace(strings.Trim(strings.TrimSpace(v), "'"))
		if v != "" {
			legal[v] = true
		}
	}
	if len(legal) == 0 {
		t.Fatal("parsed the CHECK clause but found no values in it")
	}
	t.Logf("approvals.risk_level accepts %d values from %s", len(legal), filepath.ToSlash(path))

	// Every answer the door can give must be one of them...
	for _, action := range append(classifierEntries(), "brand.new.thing", "") {
		level, _ := RiskLevel(action)
		if !legal[level] {
			t.Errorf("RiskLevel(%q) = %q, which approvals.risk_level does not accept (%v)", action, level, legal)
		}
	}
	// ...and CRITICAL, which the column allows, is NOT something the built-in
	// classifier may invent: no list calls anything critical.
	if _, ok := findCritical(); ok {
		t.Error("the built-in classifier produced CRITICAL, which no list registers")
	}
}

func findCritical() (string, bool) {
	for _, action := range classifierEntries() {
		if level, _ := RiskLevel(action); level == "CRITICAL" {
			return action, true
		}
	}
	return "", false
}

// TestDraftRiskLevelComesFromThisDoorOnly is the §3.0-d rule pointed back at the
// consumer: approval carries the level as a plain string, so the only protection is
// that every value this door emits is one approval can store verbatim. Recorded here
// as an agreement test rather than a code dependency, because internal/permission must
// not import internal/approval (the dependency runs the other way: approval already
// imports permission for Grant and Scope).
func TestDraftRiskLevelComesFromThisDoorOnly(t *testing.T) {
	seen := map[string]int{}
	for _, action := range append(classifierEntries(), "brand.new.thing") {
		level, _ := RiskLevel(action)
		seen[level]++
	}
	// The answer set is closed: exactly three labels, no more.
	for level := range seen {
		switch level {
		case RiskHigh, RiskMedium, RiskUnknown:
		default:
			t.Errorf("the door emits %q, which is outside its declared three answers", level)
		}
	}
	for _, want := range []string{RiskHigh, RiskMedium, RiskUnknown} {
		if seen[want] == 0 {
			t.Errorf("%s is declared but never produced: the door cannot answer that class", want)
		}
	}
	t.Logf("door answers over %d actions: HIGH=%d MEDIUM=%d UNKNOWN=%d",
		seen[RiskHigh]+seen[RiskMedium]+seen[RiskUnknown], seen[RiskHigh], seen[RiskMedium], seen[RiskUnknown])
}
