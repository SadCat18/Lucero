package db

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// 契约 §2.2.1 item 3: the same bytes scrubbed twice must not change by a single
// byte. This is the glue between audit scrubbing before it seals entry_hash and
// Tx.Append re-scrubbing on the way in — without it, "scrub at two places" drifts
// back into the original defect, only silently.
func TestScrubIsByteStable(t *testing.T) {
	deny, patterns := auditRules()
	if err := InitRedaction(deny, patterns, ""); err != nil {
		t.Fatalf("InitRedaction: %v", err)
	}

	cols := []string{"payload_json", "details_json", "text_content", "metadata_json", "seq_no", "keep"}
	args := []any{
		map[string]any{
			"token":     "super-secret-value",
			"api_key":   "sk-abcdefghijklmnopqrstuvwxyz",
			"vault_ref": "vault://github_token",
			"nested":    map[string]any{"Authorization": "Bearer abcdefghijklmnopqrstuvwx"},
			"list":      []any{map[string]any{"password": "hunter2hunter2"}, "safe"},
			"n":         42,
		},
		`{"zebra":1,"alpha":2,"token":"super-secret-value"}`,
		"the header was Bearer abcdefghijklmnopqrstuvwx and key sk-abcdefghijklmnopqrstuvwxyz",
		map[string]any{"clean": "nothing to hide"},
		int64(1700000000000),
		"plain text",
	}

	once, hits1, err := Scrub("audit_entries", cols, args)
	if err != nil {
		t.Fatalf("Scrub pass 1: %v", err)
	}
	twice, _, err := Scrub("audit_entries", cols, once)
	if err != nil {
		t.Fatalf("Scrub pass 2: %v", err)
	}

	if !reflect.DeepEqual(once, twice) {
		for i := range once {
			if !reflect.DeepEqual(once[i], twice[i]) {
				t.Fatalf("column %s not byte-stable:\n  pass1=%v\n  pass2=%v", cols[i], once[i], twice[i])
			}
		}
		t.Fatal("Scrub output changed on a second pass")
	}
	// reflect.DeepEqual can hide a type change; compare the textual form too.
	for i := range once {
		a, b := fmt.Sprintf("%v", once[i]), fmt.Sprintf("%v", twice[i])
		if a != b || reflect.TypeOf(once[i]) != reflect.TypeOf(twice[i]) {
			t.Fatalf("column %s differs on second pass:\n  pass1=%q (%T)\n  pass2=%q (%T)", cols[i], a, once[i], b, twice[i])
		}
	}

	// The point of the pass: no secret survives, and a clean vault ref does.
	joined := fmt.Sprintf("%v %v %v", once[0], once[1], once[2])
	for _, leak := range []string{"super-secret-value", "abcdefghijklmnopqrstuvwxyz", "hunter2hunter2", "bcdefghijklmnopqrstuvwx"} {
		if strings.Contains(joined, leak) {
			t.Fatalf("Scrub leaked %q: %s", leak, joined)
		}
	}
	for _, want := range []string{"[REDACTED]", "vault://github_token", `"n":42`, "safe"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Scrub lost %s: %s", want, joined)
		}
	}
	// Non-text values must pass through untouched, not stringified.
	if v, ok := once[4].(int64); !ok || v != 1700000000000 {
		t.Fatalf("int64 arg came back as %#v", once[4])
	}
	if once[5] != "plain text" {
		t.Fatalf("plain text changed to %#v", once[5])
	}

	// Scrub hands the attribution back, and it covers both kinds.
	var sawKey, sawPattern bool
	for _, h := range hits1 {
		switch h.Kind {
		case HitKey:
			sawKey = true
		case HitPattern:
			sawPattern = true
		}
	}
	if !sawKey || !sawPattern {
		t.Fatalf("Scrub returned hits %v; want at least one %q and one %q", hits1, HitKey, HitPattern)
	}
}

// Scrub is the same fail-closed entry the write path uses, and it rejects a
// mismatched column/arg list rather than guessing.
func TestScrubFailsClosedAndValidates(t *testing.T) {
	old := red.Swap(nil)
	t.Cleanup(func() { red.Store(old) })

	if _, _, err := Scrub("audit_entries", []string{"details_json"}, []any{map[string]any{"token": "x"}}); err == nil {
		t.Fatal("Scrub accepted input with redaction uninitialized")
	} else {
		t.Logf("%v", err)
	}

	deny, patterns := auditRules()
	if err := InitRedaction(deny, patterns, ""); err != nil {
		t.Fatalf("InitRedaction: %v", err)
	}
	if _, _, err := Scrub("audit_entries", []string{"a", "b"}, []any{"x"}); err == nil {
		t.Fatal("Scrub accepted 2 columns with 1 arg")
	}
}
