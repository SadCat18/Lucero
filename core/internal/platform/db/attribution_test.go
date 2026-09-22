package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// 契约 §2.2.1 item 1: attribution for an audit row comes from the pre-seal
// Scrub, and Tx.Append's mandatory re-scrub must not count the same key a second
// time. Both directions of the same hole are pinned here: losing the pattern hit
// and double-counting the key hit.
func TestAuditEntryAttributionIsCountedOnce(t *testing.T) {
	d := newMigrated(t)
	cols := []string{"action", "details_json", "outcome"}
	args := []any{"approve",
		map[string]any{
			"note":  "the header was Bearer abcdefghijklmnopqrstuvwx",
			"token": "super-secret-value",
		},
		"SUCCESS"}

	sealed, preHits, err := Scrub("audit_entries", cols, args)
	if err != nil {
		t.Fatalf("pre-seal Scrub: %v", err)
	}

	var inTxFits []RedactionHit
	if err := d.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.Append("audit_entries",
			append([]string{"audit_id", "entry_hash", "created_at"}, cols...),
			append([]any{"a-1", "hash-1", nowMs}, sealed...))
		inTxFits = tx.RedactionHits()
		return err
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(inTxFits) != 0 {
		t.Fatalf("the re-scrub inside Tx.Append accumulated %v; audit_entries must not double-count", inTxFits)
	}

	merged := append(append([]RedactionHit{}, preHits...), inTxFits...)
	counted := map[string]int{}
	for _, h := range merged {
		counted[fmt.Sprintf("%s/%s", h.Kind, h.Match)] += h.Count
	}
	// ① both kinds present, ② the key hit counts 1 rather than 2.
	if counted[string(HitKey)+"/token"] != 1 {
		t.Fatalf("key hit count = %d; want exactly 1 (got %v)", counted[string(HitKey)+"/token"], counted)
	}
	if counted[string(HitPattern)+"/(?i)bearer\\s+[A-Za-z0-9._~+/=-]{16,}"] != 1 {
		t.Fatalf("pattern hit count = %d; want exactly 1 (got %v)",
			counted[string(HitPattern)+"/(?i)bearer\\s+[A-Za-z0-9._~+/=-]{16,}"], counted)
	}

	var stored string
	if err := d.Read.QueryRow(`SELECT details_json FROM audit_entries WHERE audit_id='a-1'`).Scan(&stored); err != nil {
		t.Fatalf("read stored row: %v", err)
	}
	for _, leak := range []string{"super-secret-value", "abcdefghijklmnopqrstuvwx"} {
		if strings.Contains(stored, leak) {
			t.Fatalf("stored row leaked %q: %s", leak, stored)
		}
	}
	if !strings.Contains(stored, "[REDACTED]") {
		t.Fatalf("stored row was not scrubbed: %s", stored)
	}
	t.Logf("stored details_json: %s", stored)
}

// A non-audit table must still accumulate attribution in the transaction, so the
// discarding collector really is scoped to audit_entries.
func TestOtherTablesStillAccumulateHits(t *testing.T) {
	d := newMigrated(t)
	var got []RedactionHit
	err := d.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.Append("principals",
			[]string{"id", "principal_type", "display_name", "created_at", "updated_at", "metadata_json"},
			[]any{"p-hit", "USER", "me", nowMs, nowMs, map[string]any{"token": "super-secret-value"}})
		got = tx.RedactionHits()
		return err
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(got) != 1 || got[0].Kind != HitKey || got[0].Match != "token" {
		t.Fatalf("principals append recorded %v; want one %q/token hit", got, HitKey)
	}
}
