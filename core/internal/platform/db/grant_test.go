package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const nowMs = int64(1700000000000)

// seedFKs fills the two parents the grant and capability rows point at.
// adapter_instances is an aggregate, so it has to come through Create — which
// also proves Append cannot be used to dodge a version there.
func seedFKs(t *testing.T, d *DB) {
	t.Helper()
	err := d.Write(context.Background(), func(tx *Tx) error {
		if _, err := tx.Append("principals",
			[]string{"id", "principal_type", "display_name", "created_at", "updated_at"},
			[]any{"p-1", "USER", "me", nowMs, nowMs}); err != nil {
			return err
		}
		return tx.Create(AggAdapter, "ad-1",
			[]string{"adapter_key", "display_name", "created_at", "updated_at"},
			[]any{"fake", "Fake", nowMs, nowMs},
			EventDraft{EventID: "ad-1", Type: "adapter_registered", AggregateID: "ad-1"})
	})
	if err != nil {
		t.Fatalf("seed FKs: %v", err)
	}
}

func versionsOf(t *testing.T, d *DB, aggregateType, aggregateID string) []int64 {
	t.Helper()
	rows, err := d.Read.Query(`SELECT aggregate_version FROM event_journal
		WHERE aggregate_type = ? AND aggregate_id = ? ORDER BY seq`, aggregateType, aggregateID)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	return out
}

func storedVersion(t *testing.T, d *DB, table, id string) int64 {
	t.Helper()
	var v int64
	if err := d.Read.QueryRow("SELECT version FROM "+table+" WHERE id = ?", id).Scan(&v); err != nil {
		t.Fatalf("read %s.version: %v", table, err)
	}
	return v
}

func eqInt64(a, b []int64) bool {
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

// T-12: temporary_grants must be expressible through Touch. Issue → revoke →
// expire, and the three grant events must carry consecutive versions. Whether
// that exact sequence is legal is audit's state machine, not this test's claim;
// here it only pins version continuity.
func TestGrantLifecycleTakesConsecutiveVersions(t *testing.T) {
	d := newMigrated(t)
	seedFKs(t, d)

	err := d.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Create(AggGrant, "grant-1",
			[]string{"principal_id", "scope_type", "action", "starts_at", "created_at"},
			[]any{"p-1", "GLOBAL", "fs.delete", nowMs, nowMs},
			EventDraft{EventID: "g-1", Type: "grant_issued", Aggregate: AggGrant, AggregateID: "grant-1"}); err != nil {
			return err
		}
		if _, err := tx.Touch(AggGrant, "grant-1", "revoked_at = ?", []any{nowMs + 1000},
			EventDraft{EventID: "g-2", Type: "grant_revoked", Aggregate: AggGrant, AggregateID: "grant-1"}); err != nil {
			return err
		}
		_, err := tx.Touch(AggGrant, "grant-1", "expires_at = ?", []any{nowMs + 2000},
			EventDraft{EventID: "g-3", Type: "grant_expired", Aggregate: AggGrant, AggregateID: "grant-1"})
		return err
	})
	if err != nil {
		t.Fatalf("grant lifecycle: %v", err)
	}

	if got := storedVersion(t, d, "temporary_grants", "grant-1"); got != 3 {
		t.Fatalf("temporary_grants.version = %d; want 3", got)
	}
	want := []int64{1, 2, 3}
	if got := versionsOf(t, d, "grant", "grant-1"); !eqInt64(got, want) {
		t.Fatalf("grant event versions = %v; want %v", got, want)
	}

	// aggregate_type must be the singular domain word, not the table name.
	var wrong int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE aggregate_type = 'temporary_grants'`).Scan(&wrong); err != nil {
		t.Fatalf("count: %v", err)
	}
	if wrong != 0 {
		t.Fatalf("%d grant events used the table name as aggregate_type", wrong)
	}

	// Now an aggregate: Append must refuse it, so no write can skip a version.
	err = d.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.Append("temporary_grants", []string{"id", "principal_id", "scope_type", "action", "starts_at", "created_at"},
			[]any{"g-sneaky", "p-1", "GLOBAL", "x", nowMs, nowMs})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "must go through Touch or Create") {
		t.Fatalf("Append into temporary_grants = %v; want the aggregate refusal", err)
	}
}

// adapter_capabilities is the third shape of the same fix: health and
// last_verified_at are mutable, and adapter_capability_verified already exists.
func TestAdapterCapabilityTakesVersions(t *testing.T) {
	d := newMigrated(t)
	seedFKs(t, d)

	err := d.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Create(AggAdapterCapability, "cap-1",
			[]string{"adapter_instance_id", "capability_name", "support_level", "created_at", "updated_at"},
			[]any{"ad-1", "list_sessions", "NATIVE", nowMs, nowMs},
			EventDraft{EventID: "c-1", Type: "adapter_capability_verified",
				Aggregate: AggAdapterCapability, AggregateID: "cap-1"}); err != nil {
			return err
		}
		_, err := tx.Touch(AggAdapterCapability, "cap-1", "health = ?, last_verified_at = ?",
			[]any{"HEALTHY", nowMs + 500},
			EventDraft{EventID: "c-2", Type: "adapter_capability_verified",
				Aggregate: AggAdapterCapability, AggregateID: "cap-1"})
		return err
	})
	if err != nil {
		t.Fatalf("capability lifecycle: %v", err)
	}
	if got := storedVersion(t, d, "adapter_capabilities", "cap-1"); got != 2 {
		t.Fatalf("adapter_capabilities.version = %d; want 2", got)
	}
	if got := versionsOf(t, d, "adapter_capability", "cap-1"); !eqInt64(got, []int64{1, 2}) {
		t.Fatalf("capability event versions = %v; want [1 2]", got)
	}
}

// policy_rules now takes the same shape as the other two: 000003 gave it the
// version column and the contract §6 registered policy_created / policy_updated.
func TestPolicyRulesTakesVersions(t *testing.T) {
	d := newMigrated(t)

	err := d.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Create(AggPolicy, "pr-1",
			[]string{"scope_type", "action_pattern", "effect", "created_at", "updated_at"},
			[]any{"GLOBAL", "*", "DENY", nowMs, nowMs},
			EventDraft{EventID: "p-1", Type: "policy_created", Aggregate: AggPolicy, AggregateID: "pr-1"}); err != nil {
			return err
		}
		_, err := tx.Touch(AggPolicy, "pr-1", "enabled = ?", []any{0},
			EventDraft{EventID: "p-2", Type: "policy_updated", Aggregate: AggPolicy, AggregateID: "pr-1"})
		return err
	})
	if err != nil {
		t.Fatalf("policy lifecycle: %v", err)
	}
	if got := storedVersion(t, d, "policy_rules", "pr-1"); got != 2 {
		t.Fatalf("policy_rules.version = %d; want 2", got)
	}
	if got := versionsOf(t, d, "policy", "pr-1"); !eqInt64(got, []int64{1, 2}) {
		t.Fatalf("policy event versions = %v; want [1 2]", got)
	}

	// Only the two registered names are legal: the platform must not quietly
	// accept a policy event type the catalogue never listed.
	err = d.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.Touch(AggPolicy, "pr-1", "enabled = ?", []any{1},
			EventDraft{EventID: "p-3", Type: "policy_rule_deleted", Aggregate: AggPolicy, AggregateID: "pr-1"})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "not in the event journal catalogue") {
		t.Fatalf("unregistered policy type = %v; want the catalogue refusal", err)
	}
	// The refused Touch must not have taken a version or emitted a row either.
	if got := storedVersion(t, d, "policy_rules", "pr-1"); got != 2 {
		t.Fatalf("policy_rules.version = %d after the refused tx; want 2", got)
	}
	var refused int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_id='p-3'`).Scan(&refused); err != nil {
		t.Fatalf("count: %v", err)
	}
	if refused != 0 {
		t.Fatal("the refused policy event was written")
	}

	// Touch reaches the table, so a missing id is a not-found, not a registry error.
	err = d.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.Touch(AggPolicy, "pr-none", "enabled = ?", []any{0},
			EventDraft{EventID: "p-4", Type: "policy_updated", Aggregate: AggPolicy, AggregateID: "pr-none"})
		return err
	})
	if !errors.Is(err, ErrAggregateNotFound) {
		t.Fatalf("Touch on a missing policy id = %v; want ErrAggregateNotFound", err)
	}
}

// 契约 §1.1.11: the If-Match pattern needs "read version → compare → Touch" inside
// one transaction. Reading through the transaction's own connection sees work this
// transaction has not committed yet; reading through the pool would not, which is
// exactly the race window the clause closes.
func TestTxReadsItsOwnUncommittedWork(t *testing.T) {
	d := newMigrated(t)

	err := d.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Create(AggPolicy, "pr-tx",
			[]string{"scope_type", "action_pattern", "effect", "created_at", "updated_at"},
			[]any{"GLOBAL", "*", "DENY", nowMs, nowMs},
			EventDraft{EventID: "tx-1", Type: "policy_created", Aggregate: AggPolicy, AggregateID: "pr-tx"}); err != nil {
			return err
		}

		var inTx int64
		if err := tx.QueryRowContext(context.Background(),
			`SELECT version FROM policy_rules WHERE id = ?`, "pr-tx").Scan(&inTx); err != nil {
			return fmt.Errorf("in-tx read could not see its own row: %w", err)
		}
		if inTx != 1 {
			t.Fatalf("in-tx read saw version %d; want 1", inTx)
		}

		// The same read on the pool must NOT see it — proof the two paths differ.
		var outside int64
		err := d.Read.QueryRowContext(context.Background(),
			`SELECT version FROM policy_rules WHERE id = ?`, "pr-tx").Scan(&outside)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("read pool saw version %d with err %v; want sql.ErrNoRows", outside, err)
		}

		// QueryContext goes through the same connection: it must see this
		// transaction's own event row.
		rows, err := tx.QueryContext(context.Background(),
			`SELECT aggregate_version FROM event_journal WHERE aggregate_id = ?`, "pr-tx")
		if err != nil {
			return err
		}
		var seen []int64
		for rows.Next() {
			var v int64
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return err
			}
			seen = append(seen, v)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if !eqInt64(seen, []int64{1}) {
			t.Fatalf("in-tx QueryContext saw events %v; want [1]", seen)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Committed, so the pool sees it now, and the version never moved past 1
	// because this path deliberately did not Touch.
	if got := storedVersion(t, d, "policy_rules", "pr-tx"); got != 1 {
		t.Fatalf("policy_rules.version = %d after the no-touch path; want 1", got)
	}
}
