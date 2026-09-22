package eventjournal

import (
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

	out := map[string]bool{}
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
		names := eventTypesIn(cells[1])
		for _, n := range names {
			out[n] = true
		}
	}
	if len(out) < 40 {
		t.Fatalf("only parsed %d event types from %s; the parser and the contract have drifted apart, "+
			"and a silent under-parse would make this test meaningless", len(out), path)
	}
	return out
}

// eventTypesIn expands the contract's shorthand row form, e.g.
// `execution_cancelled` / `_failed` / `_succeeded`  ->  three full names.
func eventTypesIn(cell string) []string {
	var out []string
	prefix := ""
	for _, tok := range strings.Split(cell, "/") {
		tok = strings.TrimSpace(tok)
		if tok == "" || strings.Contains(tok, "同上") {
			continue
		}
		if !strings.Contains(tok, "`") {
			prefix = ""
			continue
		}
		name := tok[strings.Index(tok, "`")+1:]
		if i := strings.IndexByte(name, '`'); i >= 0 {
			name = name[:i]
		}
		name = strings.TrimSpace(name)
		switch {
		case name == "" || !strings.Contains(name, "_"):
			prefix = ""
		case strings.HasPrefix(name, "_"):
			if prefix != "" {
				out = append(out, prefix+name[1:])
			}
		default:
			out = append(out, name)
			if i := strings.LastIndexByte(name, '_'); i >= 0 {
				prefix = name[:i+1]
			} else {
				prefix = ""
			}
		}
	}
	return out
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
