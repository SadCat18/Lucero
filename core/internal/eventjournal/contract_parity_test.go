package eventjournal

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// contractCatalog parses the event contract §6 table live rather than keeping a
// copy of the names here, because a copied list is exactly the way this test
// would start passing while the real drift continued.
func contractCatalog(t *testing.T) map[string]bool {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "specs", "events", "event-contract-v0.1.md"))
	if err != nil {
		t.Fatalf("resolve contract path: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract %s: %v", path, err)
	}

	counts := map[string]int{}
	inSection := false
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "## 6."):
			inSection = true
			continue
		case inSection && strings.HasPrefix(line, "## "):
			inSection = false
		}
		if !inSection || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 2 {
			continue
		}
		if err := addNames(counts, cells[1]); err != nil {
			// Report and keep going: t.Errorf lists every broken row instead of
			// stopping at the first, and the checks below still run.
			t.Errorf("event contract §6 row unparseable: %v", err)
		}
	}
	// A name listed twice used to vanish into the set below, so "agree on N" could
	// be reported while the table double-registered one type.
	if dups := duplicated(counts); len(dups) > 0 {
		t.Errorf("event contract §6 lists %v more than once; the total token count (%d) and the "+
			"distinct name count (%d) disagree, so either a merged cell expands onto an existing row "+
			"or the same type was registered twice", dups, totalTokens(counts), len(counts))
	}
	out := map[string]bool{}
	for n := range counts {
		out[n] = true
	}
	if len(out) < 40 {
		t.Fatalf("only parsed %d event types from %s; the parser and the contract have drifted apart, "+
			"and a silent under-parse would make this test meaningless", len(out), path)
	}
	return out
}

// A merged cell may take exactly two shapes (contract §6 rule ③b): every token a
// full name, or one full name followed by only "_xxx" shorthands. Anything else
// — a shorthand with no full name before it, a shorthand after two or more full
// names, or a full name after a shorthand — means the table or this parser is
// broken, so it is reported rather than skipped: silently dropping a token would
// make the parity check see one fewer type on both sides and still pass.
func eventTypesIn(cell string) ([]string, error) {
	var out []string
	stem := ""
	fulls := 0
	sawShort := false
	for _, tok := range strings.Split(cell, "/") {
		tok = strings.TrimSpace(tok)
		if tok == "" || strings.Contains(tok, "同上") {
			continue
		}
		if !strings.Contains(tok, "`") {
			continue
		}
		name := tok[strings.Index(tok, "`")+1:]
		if i := strings.IndexByte(name, '`'); i >= 0 {
			name = name[:i]
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "_") {
			if fulls != 1 || stem == "" {
				return nil, fmt.Errorf("row %q has the shorthand %q with %d full name(s) and stem %q; "+
					"§6 rule ③b allows only all-full or one-full-then-shorthands, and a shorthand needs "+
					"a domain_action stem to attach to, so the table or this parser has lost its shape",
					cell, name, fulls, stem)
			}
			out = append(out, stem+name[1:])
			sawShort = true
			continue
		}
		if sawShort {
			return nil, fmt.Errorf("row %q has the full name %q after a shorthand; §6 rule ③b allows "+
				"only all-full or one-full-then-shorthands", cell, name)
		}
		out = append(out, name)
		fulls++
		if i := strings.LastIndexByte(name, '_'); i >= 0 {
			stem = name[:i+1]
		} else {
			stem = ""
		}
	}
	return out, nil
}

// addNames is the single place a merged cell's expanded names get tallied, so
// the duplicate check below is the same code the parser feeds and a mistake here
// shows up in the synthetic test, not only against real contract data.
func addNames(counts map[string]int, cell string) error {
	names, err := eventTypesIn(cell)
	if err != nil {
		return err
	}
	for _, n := range names {
		counts[n]++
	}
	return nil
}

// duplicated names a count appears more than once in, sorted for a stable message.
func duplicated(counts map[string]int) []string {
	var out []string
	for n, c := range counts {
		if c > 1 {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func totalTokens(counts map[string]int) int {
	total := 0
	for _, c := range counts {
		total += c
	}
	return total
}

// The expansion convention is asserted against the parser directly, in both
// directions: a malformed row must be reported, and legitimate row shapes must
// not be flagged.
func TestEventTypesInExpansion(t *testing.T) {
	ok := []struct{ cell, want string }{
		{" `notification_created` / `_handled` / `_read` ", "notification_created,notification_handled,notification_read"},
		{" `execution_cancelled` / `_failed` / `_succeeded` ", "execution_cancelled,execution_failed,execution_succeeded"},
		{" `grant_issued` / `grant_revoked` / `grant_expired` ", "grant_issued,grant_revoked,grant_expired"},
		{" `task_accepted` ", "task_accepted"},
		{" `execution_paused` / `execution_resumed` ", "execution_paused,execution_resumed"},
	}
	for _, tc := range ok {
		got, err := eventTypesIn(tc.cell)
		if err != nil {
			t.Errorf("legitimate row %q reported as broken: %v", tc.cell, err)
			continue
		}
		if joined := strings.Join(got, ","); joined != tc.want {
			t.Errorf("eventTypesIn(%q) = %q; want %q", tc.cell, joined, tc.want)
		}
	}

	// Rule ③b: only the two shapes are legal. These are the mixed forms §6 says
	// must be treated as a broken table, including the one that used to expand
	// silently to "c_e" using the nearest-stem rule.
	bad := []string{" `_handled` / `_read` ", " `x` / `_read` ",
		" `task_created` / `grant_issued` / `_failed` ", " `a_b` / `c_d` / `_e` ",
		" `notification_created` / `_handled` / `task_queued` "}
	for _, cell := range bad {
		if _, err := eventTypesIn(cell); err == nil {
			t.Errorf("eventTypesIn(%q) returned no error; §6 rule ③b says this row shape must be reported", cell)
		} else {
			t.Logf("%q correctly reported: %v", cell, err)
		}
	}
}

func setKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func missing(a, b map[string]bool) []string {
	var out []string
	for _, k := range setKeys(a) {
		if !b[k] {
			out = append(out, k)
		}
	}
	return out
}

// The catalogue and the contract §6 table must be the same set, in both
// directions: a registered type with no constant means the writer is refused
// with no legal way out, and a constant with no contract row is an unregistered
// event type one refactor away from a P0 (Event Contract §9.1).
func TestCatalogMatchesContract(t *testing.T) {
	fromContract := contractCatalog(t)
	inCode := map[string]bool{}
	for name := range catalog {
		inCode[name] = true
	}

	if only := missing(fromContract, inCode); len(only) > 0 {
		t.Fatalf("event contract §6 registers %v but the catalogue has no entry/constant for them", only)
	}
	if only := missing(inCode, fromContract); len(only) > 0 {
		t.Fatalf("the catalogue registers %v which event contract §6 never registered", only)
	}
	t.Logf("contract §6 and the catalogue agree on %d event types", len(inCode))
}

// REQ-DB-5: notification_read specifically — notifications.state has had the
// UNREAD->READ edge since 000001, so the platform must accept the event.
func TestNotificationReadIsUsable(t *testing.T) {
	if NotificationRead != "notification_read" {
		t.Fatalf("NotificationRead = %q; want %q", NotificationRead, "notification_read")
	}
	if !Registered(NotificationRead) {
		t.Fatal("notification_read is a constant but not in the catalogue")
	}
	if !Registered("notification_created") || !Registered("notification_handled") {
		t.Fatal("a sibling notification type went missing from the catalogue")
	}
	if Registered("notification_read ") || Registered("notificationread") || Registered("NotificationRead") {
		t.Fatal("catalogue accepted a near-miss spelling")
	}
}

// A double-registered name must be reported and distinct names must not be. Both
// halves run through the real expander, so this does not depend on the current
// contents of the contract file.
func TestDetectsDuplicateRegistration(t *testing.T) {
	cases := []struct {
		name  string
		cells []string
		want  string
	}{
		{
			name:  "merged cell expands onto a name the same row already lists",
			cells: []string{" `notification_created` / `_created` "},
			want:  "notification_created",
		},
		{
			name:  "two rows list the same type",
			cells: []string{" `project_updated` ", " `project_created` / `_updated` "},
			want:  "project_updated",
		},
		{
			name:  "distinct names across a merged row and a full-name row",
			cells: []string{" `notification_created` / `_handled` / `_read` ", " `grant_issued` / `grant_revoked` "},
			want:  "",
		},
	}

	for _, tc := range cases {
		counts := map[string]int{}
		for _, cell := range tc.cells {
			if err := addNames(counts, cell); err != nil {
				t.Fatalf("%s: unexpected shape error on %q: %v", tc.name, cell, err)
			}
		}
		got := strings.Join(duplicated(counts), ",")
		if got != tc.want {
			t.Errorf("%s: duplicated() = %q; want %q", tc.name, got, tc.want)
			continue
		}
		t.Logf("%s -> duplicates=%q tokens=%d distinct=%d", tc.name, got, totalTokens(counts), len(counts))
	}
}
