package main

// readers_test.go holds the criteria T-447 named, one test per ruling, and the two
// "say it, don't hide it" cases §16.7's range depends on.
//
// ① synthesis and numbering are asserted on the reply bytes rather than on an internal
// call, because the claim is about what Core would see: a tail, not a re-send; a number
// we own, never the provider's; and stable numbers across a continuation.
// ② the three tiers are asserted both as a rule (costConfidence) and as a reply
// (Validate on the frame Core would decode), because a rule that only exists in a
// function can still be bypassed by whoever assembles the reply.
// ③ and ④ live in tier_test.go; the mapping table's vocabulary check is here because it
// is the same "don't invent a risk level" discipline as ②.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/permission"
)

// startReadFixture brings the adapter up over a fixture store and a catalogue, already
// handshaken.
func startReadFixture(t *testing.T, spec fixtureSpec, providers int) (*pipeAdapter, string) {
	t.Helper()
	db := newFixture(t, spec)
	p := startAdapter(t, testConfig(db, writeCatalogue(t, providers)))
	initialize(t, p)
	return p, db
}

// TestSynthesizedDeltasAreTailsNotResends is ①'s main leg: the second snapshot of a text
// part must arrive as the appended tail (" world"), never the whole text again, and the
// numbers must be ours - dense per turn and nothing like the provider's sparse 1500-band
// seq. Reusing that seq would collide two aggregates' frames onto one (turn_ref,
// sequence_no) key, which §16.2 bans by name.
func TestSynthesizedDeltasAreTailsNotResends(t *testing.T) {
	p, _ := startReadFixture(t, defaultFixture(), 1)
	reply := p.call(t, "r", methodSessionRead, map[string]any{"remote_session_id": "ses-000"})
	if reply.Error != nil {
		t.Fatalf("session/read: %s", reply.Error.Message)
	}
	var res sessionReadResult
	if err := json.Unmarshal(reply.Result, &res); err != nil {
		t.Fatalf("session/read reply does not decode: %v (%s)", err, reply.Result)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("%d messages, want the fixture's single turn msg-a: %+v", len(res.Messages), res.Messages)
	}
	var numbers []int64
	var replaced bool
	for _, m := range res.Messages {
		for _, part := range m.Parts {
			for _, seg := range part.Segments {
				numbers = append(numbers, seg.SequenceNo)
				if !seg.Synthesized {
					t.Errorf("a segment arrived without synthesized=true: a diff we produced cannot be read " +
						"as a provider frame (§16.3's warning that 收到≠成功 is easier to misread here)")
				}
				if seg.Replaced {
					replaced = true
				}
			}
			if part.PartID == "" {
				t.Error("a part reached the wire with no id: a reader cannot join segments to anything")
			}
			if part.PartID != "p1" {
				continue
			}
			if part.Text != "Hello world" {
				t.Errorf("p1 current text = %q, want the last snapshot in full", part.Text)
			}
			if len(part.Segments) != 2 {
				t.Fatalf("p1 produced %d segments for its 2 snapshots: %v", len(part.Segments), part.Segments)
			}
			if part.Segments[0].Text != "Hello" {
				t.Errorf("p1 segment 1 = %q, want the first snapshot whole (there is no neighbour to diff yet)",
					part.Segments[0].Text)
			}
			if got := part.Segments[1].Text; got != " world" {
				t.Errorf("p1 segment 2 = %q, want the appended tail \" world\": re-sending the whole text is "+
					"exactly what §16.2 says the diff exists to avoid", got)
			}
			if part.Segments[1].Replaced {
				t.Error("p1 segment 2 was flagged as a replacement although its snapshot extends its neighbour")
			}
		}
	}
	if replaced {
		t.Error("a replaced segment showed up in a fixture whose snapshots are pure extensions")
	}
	// The turn's numbers are ours, dense from 1, and assigned in **journal order across
	// the parts of that turn**: the fixture interleaves p1, p2, p1, so p1's own pair is
	// (1, 3) and p2 sits at 2. Numbering per part instead would let a reader who sorts by
	// sequence_no reconstruct an order the journal never had.
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	want := []int64{1, 2, 3}
	if len(numbers) != len(want) {
		t.Fatalf("segment numbers = %v, want three of them dense per turn", numbers)
	}
	for i, n := range numbers {
		if n != want[i] {
			t.Errorf("after sorting, segment number %d = %d, want %d (dense from 1, no gaps, no repeats)",
				i, n, want[i])
		}
		if n >= 1000 {
			t.Errorf("segment number %d sits in the provider's seq band: the fixture's seq starts at 1500, so "+
				"a number this large means their value was published (§16.2 forbids it)", n)
		}
	}
	if p1Numbers := partNumbers(res.Messages[0].Parts, "p1"); !sameInts(p1Numbers, []int64{1, 3}) {
		t.Errorf("p1's own numbers = %v, want [1 3]: the turn counter must interleave parts, not restart per "+
			"part", p1Numbers)
	}
	if p2Numbers := partNumbers(res.Messages[0].Parts, "p2"); !sameInts(p2Numbers, []int64{2}) {
		t.Errorf("p2's numbers = %v, want [2]", p2Numbers)
	}
	p.close(t)
}

// TestNonExtensionSnapshotIsFlaggedNotSmoothed runs the one shape §② says is rare: a
// snapshot that does not extend its neighbour. Reporting it as an appended tail would
// invent an ordering the provider never wrote; the honest answer is the whole new text
// plus a flag, and that flag is what a panel can show as 「这一段是整体替换」.
func TestNonExtensionSnapshotIsFlaggedNotSmoothed(t *testing.T) {
	p, _ := startReadFixture(t, replacedFixture(), 1)
	reply := p.call(t, "r", methodSessionRead, map[string]any{"remote_session_id": "ses-000"})
	var res sessionReadResult
	if err := json.Unmarshal(reply.Result, &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	p1 := partNumbers(res.Messages[0].Parts, "p1")
	if len(p1) != 2 {
		t.Fatalf("p1 segments = %v, want two numbered rows", p1)
	}
	var second segment
	for _, part := range res.Messages[0].Parts {
		if part.PartID != "p1" {
			continue
		}
		second = part.Segments[1]
	}
	if !second.Replaced {
		t.Errorf("a non-extension snapshot came back as %q with no replaced flag: smoothing it into a tail "+
			"invents content the provider never sent", second.Text)
	}
	if second.Text != "totally different" {
		t.Errorf("a flagged replacement should carry the whole new text, got %q", second.Text)
	}
	p.close(t)
}

// TestContinuationKeepsTheSameSegmentNumbers is ①'s stability leg. §6 规则 5's idempotency
// key only means anything if a segment keeps its number across reads, so a continuation
// must continue the count rather than restart it - and must not re-emit what the cursor
// already covered.
func TestContinuationKeepsTheSameSegmentNumbers(t *testing.T) {
	p, _ := startReadFixture(t, defaultFixture(), 1)
	first := p.call(t, "r1", methodSessionRead, map[string]any{"remote_session_id": "ses-000"})
	var full sessionReadResult
	if err := json.Unmarshal(first.Result, &full); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cursor := full.NextCursor
	if cursor == "" {
		t.Fatal("the first reply carries no cursor, so a continuation cannot be expressed at all")
	}
	second := p.call(t, "r2", methodSessionRead,
		map[string]any{"remote_session_id": "ses-000", "since_cursor": cursor})
	if second.Error != nil {
		t.Fatalf("continuation: %s", second.Error.Message)
	}
	var cont sessionReadResult
	if err := json.Unmarshal(second.Result, &cont); err != nil {
		t.Fatalf("decode continuation: %v", err)
	}
	for _, m := range cont.Messages {
		for _, part := range m.Parts {
			for _, seg := range part.Segments {
				t.Errorf("the continuation re-emitted %s's segment %d (%q): the cursor said everything up to "+
					"there was already read", part.PartID, seg.SequenceNo, seg.Text)
			}
		}
	}
	// The numbers are still the ones the full read assigned: ask for the middle of the
	// journal and the surviving segment must keep number 2, not become number 1.
	watermark := full.Messages[0].Parts[0].Segments[0].SequenceNo
	if watermark != 1 {
		t.Fatalf("fixture drift: first segment number = %d", watermark)
	}
	mid := midCursor(t, full)
	third := p.call(t, "r3", methodSessionRead,
		map[string]any{"remote_session_id": "ses-000", "since_cursor": mid})
	var fromMid sessionReadResult
	if err := json.Unmarshal(third.Result, &fromMid); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var nums []int64
	for _, m := range fromMid.Messages {
		for _, part := range m.Parts {
			for _, seg := range part.Segments {
				nums = append(nums, seg.SequenceNo)
			}
		}
	}
	if len(nums) == 0 {
		t.Fatal("reading from the middle produced no segments at all, so the number could not have been kept")
	}
	for _, n := range nums {
		if n == 1 {
			t.Errorf("a segment kept number 1 after a mid-journal cursor: numbering restarted instead of "+
				"continuing, which makes (turn_ref, sequence_no) mean a different segment per read (%v)", nums)
		}
	}
	t.Logf("segments after the mid cursor keep their numbers: %v", nums)
	p.close(t)
}

// midCursor builds a cursor that lands between the fixture's two snapshots of p1.
func midCursor(t *testing.T, full sessionReadResult) string {
	t.Helper()
	// The fixture's seq is sparse by design; the revision string carries the watermark,
	// so take the first read's own revision minus one journal step by walking the two
	// cursors the reply already exposes.
	rev := full.RemoteRevision
	if !strings.HasPrefix(rev, "seq-") {
		t.Fatalf("remote_revision %q is not the seq- form this adapter publishes", rev)
	}
	// Subtract one journal step (the fixture's step is 7) without reparsing the number:
	// the helper below is the only place that knows the fixture's shape.
	return "e" + decrement(rev[4:], 7)
}

func decrement(s string, by int64) string {
	var n int64
	for _, r := range s {
		n = n*10 + int64(r-'0')
	}
	return itoa(n - by)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestQuotaTiersFollowTheRuling is ②'s rule and reply together.
//
// The rule half is the pure function: >0 HIGH, =0 LOW, NULL no row at all. The reply half
// is what Core can actually decode, and it is asserted through §7's own Validate() so a
// future change to the assembly cannot quietly produce a frame Core would reject - which
// is how a tier rule that is right in the mapping still ends up wrong on the wire.
func TestQuotaTiersFollowTheRuling(t *testing.T) {
	cases := []struct {
		name   string
		value  float64
		isNull bool
		want   string
		keep   bool
	}{
		{"cost > 0", 0.0053, false, adapter.ConfidenceHigh, true},
		{"cost = 0", 0, false, adapter.ConfidenceLow, true},
		{"cost NULL", 0, true, "", false},
	}
	for _, c := range cases {
		got, keep := costConfidence(c.value, c.isNull)
		if keep != c.keep {
			t.Errorf("%s: produce=%v, want %v", c.name, keep, c.keep)
		}
		if got != c.want {
			t.Errorf("%s: confidence=%q, want %q (§16.4 says a 0 is neither 用完 nor 免费, and a NULL is neither "+
				"of the two either)", c.name, got, c.want)
		}
	}
	if got, keep := costConfidence(0, false); keep && got == adapter.ConfidenceHigh {
		t.Error("cost=0 read as HIGH")
	}

	// The reply itself. Today it is §7's empty answer (CO-66 is why: no field can carry
	// an amount), and the empty answer is only legal paired with UNKNOWN.
	p, db := startReadFixture(t, defaultFixture(), 1)
	_ = db
	reply := p.call(t, "q", methodQuotaRead, map[string]any{})
	if reply.Error != nil {
		t.Fatalf("quota/read: %s", reply.Error.Message)
	}
	var q adapter.QuotaResult
	if err := json.Unmarshal(reply.Result, &q); err != nil {
		t.Fatalf("quota/read reply does not decode: %v", err)
	}
	if err := q.Validate(); err != nil {
		t.Errorf("our own quota reply fails §7's validator: %v", err)
	}
	if len(q.Observations) != 0 || q.OverallConfidence != adapter.ConfidenceUnknown {
		t.Errorf("quota/read = %d observations / %s, want 0 / UNKNOWN while §7 has no field for an amount "+
			"(CO-66): a fabricated row here is the P0 §7 names", len(q.Observations), q.OverallConfidence)
	}
	for _, o := range q.Observations {
		if o.UsedPercent != nil && *o.UsedPercent == 0 && o.Confidence == adapter.ConfidenceHigh {
			t.Errorf("observation %s reports 0 used at HIGH confidence: that is 「用完」 or 「免费」, and neither "+
				"was measured (§7)", o.QuotaKey)
		}
	}
	if !strings.Contains(p.logs.String(), "cost>0=1 cost=0=1 cost NULL=1") {
		t.Errorf("the store's three tiers were not counted on stderr; the log line is where the measured "+
			"split is visible today: %s", p.logs.String())
	}
	p.close(t)
}

// TestNullOnlyStoreProducesNothing covers the NULL leg at the store boundary: a session
// table whose costs are all NULL must read as "nothing to observe", not as a row of
// zeros and not as an error.
func TestNullOnlyStoreProducesNothing(t *testing.T) {
	spec := fixtureSpec{Sessions: 2, CostNull: 2, WithTitle: false}
	p, _ := startReadFixture(t, spec, 1)
	reply := p.call(t, "q", methodQuotaRead, map[string]any{})
	if reply.Error != nil {
		t.Fatalf("quota/read: %s", reply.Error.Message)
	}
	var q adapter.QuotaResult
	if err := json.Unmarshal(reply.Result, &q); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(q.Observations) != 0 {
		t.Errorf("a NULL-only store produced %d observations: §16.4 says NULL 不产出该条", len(q.Observations))
	}
	if !strings.Contains(p.logs.String(), "cost NULL=2") {
		t.Errorf("the NULL count is not visible on stderr: %s", p.logs.String())
	}
	p.close(t)
}

// TestUnknownSessionIsNotFoundNotAnEmptyTranscript pins the two-answer split §15.1
// requires. Answering an unknown session with an empty transcript would read to the panel
// as "this conversation has no messages", which is a different fact and an unrecoverable
// one to discover later.
func TestUnknownSessionIsNotFoundNotAnEmptyTranscript(t *testing.T) {
	p, _ := startReadFixture(t, defaultFixture(), 1)
	reply := p.call(t, "r", methodSessionRead, map[string]any{"remote_session_id": "ses-nope"})
	if reply.Error == nil {
		t.Fatalf("an unknown session answered %s, want SESSION_NOT_FOUND", reply.Result)
	}
	if reply.Error.Symbol() != "SESSION_NOT_FOUND" {
		t.Errorf("symbol = %s, want SESSION_NOT_FOUND (the §15.1 name travels in error.data.code)", reply.Error.Symbol())
	}
	if reply.Error.Code != codeSessionNotFound {
		t.Errorf("number = %d, want %d per §15.1.1", reply.Error.Code, codeSessionNotFound)
	}
	if len(reply.Result) != 0 {
		t.Errorf("the refusal also carried a result member: %s", reply.Result)
	}
	p.close(t)
}

// TestMissingStoreSaysSoEverywhere is the §16.7 half that keeps "只读" from degrading into
// "看起来正常": with no readable store, health must not claim HEALTHY and the reads must
// name LOCAL_DATA_UNAVAILABLE rather than returning empty collections.
func TestMissingStoreSaysSo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here.db")
	p := startAdapter(t, testConfig(missing, writeCatalogue(t, 1)))
	initialize(t, p)

	h := p.call(t, "h", adapter.MethodHealth, map[string]any{})
	var hs adapter.HealthResult
	if err := json.Unmarshal(h.Result, &hs); err != nil {
		t.Fatalf("health reply does not decode: %v", err)
	}
	if hs.Health == adapter.HealthHealthy {
		t.Errorf("health reported HEALTHY with an unreadable store: §4 forbids using UNKNOWN as HEALTHY, and "+
			"the mirror mistake is just as loud here: %+v", hs)
	}
	if hs.Health != adapter.HealthUnknown {
		t.Errorf("health = %s, want UNKNOWN", hs.Health)
	}
	for _, m := range []string{methodSessionList, methodSessionRead, methodQuotaRead} {
		reply := p.call(t, "x", m, map[string]any{"remote_session_id": "ses-000"})
		if reply.Error == nil {
			t.Fatalf("%s succeeded against a missing store: %s", m, reply.Result)
		}
		if reply.Error.Symbol() != "LOCAL_DATA_UNAVAILABLE" {
			t.Errorf("%s refused with %s, want LOCAL_DATA_UNAVAILABLE (§15.1's name for 「读到了 Provider 但本地正文取不到」)",
				m, reply.Error.Symbol())
		}
	}
	p.close(t)
}

// TestSessionListPagesAndReportsUnits covers §4's pagination cursor and the unit question
// a local store raises: §4 wants Unix milliseconds, and a column in seconds must not
// silently become a 1970 timestamp.
func TestSessionListPagesAndReportsUnits(t *testing.T) {
	p, _ := startReadFixture(t, defaultFixture(), 1)
	page1 := p.call(t, "l1", methodSessionList, map[string]any{"limit": 2})
	var r1 sessionListResult
	if err := json.Unmarshal(page1.Result, &r1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(r1.Sessions) != 2 || r1.NextCursor == "" {
		t.Fatalf("first page = %d sessions with cursor %q, want 2 and a continuation", len(r1.Sessions), r1.NextCursor)
	}
	if r1.Sessions[0].Title == "" || r1.Sessions[0].ProjectPathHint == "" {
		t.Errorf("§4's title/project_path_hint came back empty although the fixture has both columns: %+v", r1.Sessions[0])
	}
	if r1.Sessions[0].UpdatedAt < 1e12 {
		t.Errorf("updated_at = %d is not a millisecond stamp; the unit field says %q",
			r1.Sessions[0].UpdatedAt, r1.Sessions[0].UpdatedAtUnit)
	}
	page2 := p.call(t, "l2", methodSessionList, map[string]any{"limit": 2, "cursor": r1.NextCursor})
	var r2 sessionListResult
	if err := json.Unmarshal(page2.Result, &r2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(r2.Sessions) != 1 || r2.NextCursor != "" {
		t.Errorf("last page = %d sessions with cursor %q, want 1 and no continuation", len(r2.Sessions), r2.NextCursor)
	}
	if r2.Sessions[0].RemoteSessionID == r1.Sessions[0].RemoteSessionID {
		t.Error("the second page repeated the first session: the cursor is not being applied")
	}
	// A cursor this build never issued is refused rather than reinterpreted: silently
	// treating a foreign cursor as "from the start" would duplicate rows into the panel.
	bad := p.call(t, "l3", methodSessionList, map[string]any{"cursor": "7"})
	if bad.Error == nil || bad.Error.Symbol() != "INVALID_PARAMS" {
		t.Errorf("a malformed cursor answered %+v, want INVALID_PARAMS", bad.Error)
	}
	p.close(t)
}

// TestModelListCarriesCacheProvenance is §16.5's ruling on the wire: the offer came from a
// file that can be stale, so every row says so and at MEDIUM. Confidence HIGH here would
// be a claim about freshness nobody measured, since the endpoints that could confirm it
// answered 401.
func TestModelListCarriesCacheProvenance(t *testing.T) {
	p, _ := startReadFixture(t, defaultFixture(), 2)
	reply := p.call(t, "m", methodModelList, map[string]any{})
	if reply.Error != nil {
		t.Fatalf("model/list: %s", reply.Error.Message)
	}
	var res modelListResult
	if err := json.Unmarshal(reply.Result, &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Models) != 2 {
		t.Fatalf("%d models, want one per provider in the fixture", len(res.Models))
	}
	for _, m := range res.Models {
		if m.ModelKey == "" || m.Label == "" {
			t.Errorf("model row without key/label: %+v", m)
		}
		var raw map[string]any
		if err := json.Unmarshal(m.ProviderRaw, &raw); err != nil {
			t.Fatalf("provider_raw does not decode: %v", err)
		}
		if raw["source"] != "cache_file" {
			t.Errorf("%s says source=%v: §16.5 requires the channel to be named, because a cache answer is "+
				"not a live answer", m.ModelKey, raw["source"])
		}
		if raw["confidence"] != adapter.ConfidenceMedium {
			t.Errorf("%s says confidence=%v, want MEDIUM: the catalogue can be stale and nothing measured it "+
				"today", m.ModelKey, raw["confidence"])
		}
		if !strings.Contains(string(raw["live_endpoints"].(string)), "401") {
			t.Errorf("%s hides why the live channel is absent: %v", m.ModelKey, raw["live_endpoints"])
		}
	}
	p.close(t)
}

// TestModelListRefusesToPublishAnEmptyOffer covers the other direction: a cache file that
// parses to nothing must not become "this product offers no models".
func TestModelListRefusesToPublishAnEmptyOffer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	db := newFixture(t, defaultFixture())
	p := startAdapter(t, testConfig(db, path))
	initialize(t, p)
	reply := p.call(t, "m", methodModelList, map[string]any{})
	if reply.Error == nil {
		t.Fatalf("an empty catalogue answered as a list: %s - §16.5 forbids 「读不到就不显示」 dressed up as an "+
			"answer", reply.Result)
	}
	if reply.Error.Symbol() != "LOCAL_DATA_UNAVAILABLE" {
		t.Errorf("empty catalogue refused with %s, want LOCAL_DATA_UNAVAILABLE", reply.Error.Symbol())
	}
	// And a file that is not a catalogue at all is named as such, not silently mapped.
	if err := os.WriteFile(path, []byte(`[1,2,3]`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p2 := startAdapter(t, testConfig(db, path))
	initialize(t, p2)
	second := p2.call(t, "m2", methodModelList, map[string]any{})
	if second.Error == nil || !strings.Contains(second.Error.Message, "not the recorded") {
		t.Errorf("a non-catalogue file answered %+v, want a message naming the shape we expected", second.Error)
	}
	p2.close(t)
	p.close(t)
}

// TestToolMappingValuesAreRegisteredInPermission is REQ-OC-1's vocabulary check, and it is
// the same shape as T-441's: the strings are verified against permission's own built-in
// classifier rather than against a copy of its list, because an unregistered action fails
// closed into approval and the gate would then close for a reason nobody chose.
//
// permission is imported in this file only: internal/adapter's purity guard forbids the
// protocol layer from knowing about it, and an Adapter that judges risk itself would be a
// second classifier (§3.1 第 2 行's default is Core's to apply).
func TestToolMappingValuesAreRegisteredInPermission(t *testing.T) {
	table := registeredActions()
	if len(table) == 0 {
		t.Fatal("the mapping table is empty: then this test proves nothing and §16.1's ruling has no landing")
	}
	for tool, action := range table {
		level, registered := permission.RiskLevel(action)
		if !registered {
			t.Errorf("tool %q maps to %q, which permission has never heard of: Core would judge it UNKNOWN and "+
				"fail closed for a reason the mapping chose by accident", tool, action)
			continue
		}
		if level != permission.RiskHigh {
			t.Errorf("tool %q maps to %q, which classifies as %s, not HIGH: this table exists to name the "+
				"高危动作, and an adapter may not label anything low/medium on Core's behalf (§16.1 禁令 ②)",
				tool, action, level)
		}
	}
	// The unregistered leg, with the two tool names the profile measured most often: they
	// must come back as "not registered here", which is Core's fail-closed input, not as a
	// verdict.
	for _, tool := range []string{"read", "grep", "glob", "todowrite", "task", "question"} {
		if action, ok := classifyTool(tool); ok {
			t.Errorf("tool %q is mapped to %q: §16.1 禁令 ② says an unmapped name must not be judged here, and "+
				"mapping it to a 低/中 action is exactly that judgement", tool, action)
		}
	}
	// The negative control that keeps the loop above from being satisfied by an empty set:
	// a string permission does not know must stay unknown.
	if _, registered := permission.RiskLevel("opencode.everything"); registered {
		t.Error("an invented action registered after all: the classifier's list needs re-reading before this " +
			"table can be trusted")
	}
}

// TestInterruptRoutesStayUnverified is ④: the two generations are dispatched by version,
// nothing is ever claimed verified, and an unmeasured version gets no path at all rather
// than inheriting one.
func TestInterruptRoutesStayUnverified(t *testing.T) {
	if len(interruptRoutes) == 0 {
		t.Fatal("the route table is empty")
	}
	for version, r := range interruptRoutes {
		if r.Verified {
			t.Errorf("route table entry %s says Verified: §16.5 measured 401 and 未调用, so this tier may not "+
				"record any route as behaviourally known", version)
		}
		if r.Legacy == "" || r.Current == "" || r.Legacy == r.Current {
			t.Errorf("%s: legacy/current naming is not the coexisting pair the profile measured (%q, %q)",
				version, r.Legacy, r.Current)
		}
		if !strings.Contains(r.Why, "401") && !strings.Contains(r.Why, "not invoked") &&
			!strings.Contains(r.Why, "neither invoked") {
			t.Errorf("%s: Verified=false without a measured reason (%q) is a placeholder, not evidence",
				version, r.Why)
		}
	}
	// An unmeasured version must be refused, not defaulted.
	err := stopAnswer("9.9.9")
	var e *adapter.Error
	if !asError(err, &e) || e.Code != adapter.ErrCapabilityUnavailable {
		t.Errorf("unknown version answered %v, want CAPABILITY_UNAVAILABLE", err)
	}
	if !strings.Contains(err.Error(), "no measured route table") {
		t.Errorf("the refusal for an unmeasured version does not say so: %v", err)
	}
	// A measured version refuses too - it only refuses with the routes named, so the
	// panel can show 「路由存在、行为未验证」 instead of a generic failure.
	known := stopAnswer("1.18.32")
	if !strings.Contains(known.Error(), "unverified") {
		t.Errorf("the known-version refusal does not say unverified: %v", known)
	}
	if !strings.Contains(known.Error(), "/session/{id}/abort") || !strings.Contains(known.Error(), "interrupt") {
		t.Errorf("the refusal lost the two generations it is talking about: %v", known)
	}
}

// asError is this package's copy of the decode helper internal/adapter keeps private.
func asError(err error, target **adapter.Error) bool {
	e, ok := err.(*adapter.Error)
	if ok {
		*target = e
	}
	return ok
}

// partNumbers reads one part's segment numbers back out of a reply.
func partNumbers(parts []partView, id string) []int64 {
	for _, p := range parts {
		if p.PartID == id {
			out := make([]int64, 0, len(p.Segments))
			for _, s := range p.Segments {
				out = append(out, s.SequenceNo)
			}
			return out
		}
	}
	return nil
}

func sameInts(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
