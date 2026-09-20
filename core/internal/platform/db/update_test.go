package db

import (
	"context"
	"strings"
	"testing"
)

func TestUpdateOnceSealsResponseExactlyOnce(t *testing.T) {
	d := newMigrated(t)

	first := map[string]any{"body": "created", "token": "super-secret-value"}
	var sealed int64
	err := d.Write(context.Background(), func(tx *Tx) error {
		if _, err := tx.Append("idempotency_keys",
			[]string{"key", "operation", "request_hash", "state", "created_at", "updated_at"},
			[]any{"idem-1", "task_created", "h1", "IN_PROGRESS", nowMs, nowMs}); err != nil {
			return err
		}
		n, err := tx.UpdateOnce("idempotency_keys", "idem-1", "state", "COMPLETED",
			"state = ?, response_json = ?, updated_at = ?",
			[]any{"COMPLETED", first, nowMs + 10})
		sealed = n
		return err
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed != 1 {
		t.Fatalf("first seal changed %d rows; want 1", sealed)
	}

	var state, stored string
	if err := d.Read.QueryRow(`SELECT state, response_json FROM idempotency_keys WHERE key='idem-1'`).
		Scan(&state, &stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if state != "COMPLETED" {
		t.Fatalf("state = %q; want COMPLETED", state)
	}
	// Requirement (c): this path is not an event path, but it is still a redaction
	// path — response_json is where a replayed provider answer could carry a key.
	if strings.Contains(stored, "super-secret-value") {
		t.Fatalf("UpdateOnce let a secret into response_json: %s", stored)
	}
	if !strings.Contains(stored, "[REDACTED]") || !strings.Contains(stored, "created") {
		t.Fatalf("response_json not sealed as expected: %s", stored)
	}
	t.Logf("sealed response_json: %s", stored)

	// A replay must change nothing, so the caller can return the first response.
	var replay int64
	err = d.Write(context.Background(), func(tx *Tx) error {
		n, err := tx.UpdateOnce("idempotency_keys", "idem-1", "state", "COMPLETED",
			"state = ?, response_json = ?, updated_at = ?",
			[]any{"COMPLETED", map[string]any{"body": "second attempt"}, nowMs + 20})
		replay = n
		return err
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay != 0 {
		t.Fatalf("replay changed %d rows; want 0", replay)
	}
	if again := readResponse(t, d); again != stored {
		t.Fatalf("replay overwrote the sealed response:\n first=%s\n again=%s", stored, again)
	}
}

func readResponse(t *testing.T, d *DB) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(`SELECT response_json FROM idempotency_keys WHERE key='idem-1'`).Scan(&s); err != nil {
		t.Fatalf("read response_json: %v", err)
	}
	return s
}

// Requirements (a) and (b): anything outside the whitelist is refused, and the
// aggregate tables stay unreachable from this path even though they are mutable.
func TestUpdateOnceRefusesEverythingElse(t *testing.T) {
	d := newMigrated(t)
	ctx := context.Background()

	cases := []struct{ table, col string }{
		{"tasks", "status"},                  // ADR-017 aggregate
		{"temporary_grants", "revoked_at"},   // 000003 aggregate
		{"policy_rules", "enabled"},          // 000003 aggregate
		{"adapter_capabilities", "health"},   // 000003 aggregate
		{"settings", "value_json"},           // event-less but not whitelisted yet
		{"execution_leases", "heartbeat_at"}, // no production writer (REQ-DB-1 ruling)
		{"event_journal", "event_type"},      // append-only journal
		{"schema_migrations", "name"},        // the migrator's own ledger
		{"principals", "display_name"},       // unlisted table
		{"idempotency_keys", "principal_id"}, // whitelisted table, unlisted column
		{"idempotency_keys", "request_hash"}, // whitelisted table, unlisted column
	}
	for _, tc := range cases {
		err := d.Write(ctx, func(tx *Tx) error {
			_, err := tx.UpdateOnce(tc.table, "whatever", tc.col, "x",
				tc.col+" = ?", []any{"y"})
			return err
		})
		if err == nil {
			t.Fatalf("UpdateOnce accepted %s.%s", tc.table, tc.col)
		}
		t.Logf("%s.%s refused: %v", tc.table, tc.col, err)
	}

	// Seed a row here (each test gets its own database) so the injection attempts
	// below have something to try to destroy.
	if err := d.Write(ctx, func(tx *Tx) error {
		_, err := tx.Append("idempotency_keys",
			[]string{"key", "operation", "request_hash", "state", "created_at", "updated_at"},
			[]any{"idem-shape", "op", "h", "IN_PROGRESS", nowMs, nowMs})
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The SET list goes through the same "column = ?" shape check as Touch.
	for _, set := range []string{"state = 'COMPLETED'", "state = ?; DROP TABLE idempotency_keys", ""} {
		err := d.Write(ctx, func(tx *Tx) error {
			_, err := tx.UpdateOnce("idempotency_keys", "idem-shape", "state", "COMPLETED", set, []any{"COMPLETED"})
			return err
		})
		if err == nil {
			t.Fatalf("UpdateOnce accepted setSQL %q", set)
		}
		t.Logf("setSQL %q refused: %v", set, err)
	}
	var stillThere int
	if err := d.Read.QueryRow(`SELECT count(*) FROM idempotency_keys WHERE key='idem-shape'`).Scan(&stillThere); err != nil {
		t.Fatalf("count: %v", err)
	}
	if stillThere != 1 {
		t.Fatal("the seeded row did not survive the refused attempts")
	}
}

// This path must not touch versions or emit events (requirement c).
func TestUpdateOnceTakesNoVersionAndWritesNoEvent(t *testing.T) {
	d := newMigrated(t)
	ctx := context.Background()
	if err := d.Write(ctx, func(tx *Tx) error {
		_, err := tx.Append("idempotency_keys",
			[]string{"key", "operation", "request_hash", "state", "created_at", "updated_at"},
			[]any{"idem-v", "op", "h", "IN_PROGRESS", nowMs, nowMs})
		return err
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	before := eventCount(t, d)

	err := d.Write(ctx, func(tx *Tx) error {
		_, err := tx.UpdateOnce("idempotency_keys", "idem-v", "state", "COMPLETED",
			"state = ?, updated_at = ?", []any{"COMPLETED", nowMs + 5})
		return err
	})
	if err != nil {
		t.Fatalf("UpdateOnce: %v", err)
	}
	if after := eventCount(t, d); after != before {
		t.Fatalf("event_journal rows went %d -> %d; this path must write none", before, after)
	}
}

func eventCount(t *testing.T, d *DB) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal`).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

// Callers commonly pass an already-marshaled JSON string (backend marshals its
// view struct). That must still go through the deny-key walk, not just the
// value patterns. Note the stored bytes are re-encoded, so key order is
// normalised: a caller that returns the sealed row to the client will hand back
// semantically identical but not byte-identical JSON.
func TestUpdateOnceRedactsPreSerializedJSON(t *testing.T) {
	d := newMigrated(t)
	const in = `{"zebra":1,"alpha":2,"token":"super-secret-value"}`

	err := d.Write(context.Background(), func(tx *Tx) error {
		if _, err := tx.Append("idempotency_keys",
			[]string{"key", "operation", "request_hash", "state", "created_at", "updated_at"},
			[]any{"idem-json", "op", "h", "IN_PROGRESS", nowMs, nowMs}); err != nil {
			return err
		}
		_, err := tx.UpdateOnce("idempotency_keys", "idem-json", "state", "COMPLETED",
			"state = ?, response_json = ?, updated_at = ?", []any{"COMPLETED", in, nowMs + 5})
		return err
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	var stored string
	if err := d.Read.QueryRow(`SELECT response_json FROM idempotency_keys WHERE key='idem-json'`).Scan(&stored); err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(stored, "super-secret-value") {
		t.Fatalf("a pre-marshaled payload smuggled a secret: %s", stored)
	}
	for _, want := range []string{`"token":"[REDACTED]"`, `"alpha":2`, `"zebra":1`} {
		if !strings.Contains(stored, want) {
			t.Fatalf("stored %s lacks %s", stored, want)
		}
	}
	t.Logf("in=%s out=%s", in, stored)
}
