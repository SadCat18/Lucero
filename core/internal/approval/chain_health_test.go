package approval

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// T-65② hands this package the reconciliation duty: the three chain-health states
// have to come out right for rows that have actually been through SQLite.
// audit's own anchor_test.go proves the judgement over in-memory Entry values;
// what could still be wrong here is the round trip — the bytes that were sealed
// not being the bytes that read back (NULL versus "", and the platform layer's
// re-encoding of JSON columns). Either mismatch would make an honest chain report
// UNVERIFIED forever, which fails closed but quietly blinds the panel.
//
// The reader below mirrors core/cmd/core/auditchain.go's column list rather than
// reusing audit's private one: reading through the writer's own constant would
// hide a schema drift instead of catching it. This file lives in approval because
// that is the package I own that may import both audit and platform/db — audit
// must never import db (internal contract §0).

func readBackChain(t *testing.T, d *db.DB) []audit.Entry {
	t.Helper()
	rows, err := d.Read.Query(`SELECT seq, audit_id, principal_id, action, resource_type, resource_id,
		project_id, task_id, execution_id, correlation_id, outcome, details_json,
		previous_hash, entry_hash, signature, created_at FROM audit_entries ORDER BY seq`)
	if err != nil {
		t.Fatalf("read audit_entries: %v", err)
	}
	defer rows.Close()

	var out []audit.Entry
	for rows.Next() {
		var (
			e                                    audit.Entry
			auditID, principalID, action, rType  sql.NullString
			rID, projectID, taskID, execID, corr sql.NullString
			outcome, details, prevHash           sql.NullString
			entryHash, signature                 sql.NullString
			created                              sql.NullInt64
		)
		if err := rows.Scan(&e.Seq, &auditID, &principalID, &action, &rType, &rID,
			&projectID, &taskID, &execID, &corr, &outcome, &details,
			&prevHash, &entryHash, &signature, &created); err != nil {
			t.Fatalf("scan audit_entries: %v", err)
		}
		e.AuditID, e.PrincipalID, e.Action = auditID.String, principalID.String, action.String
		e.ResourceType, e.ResourceID = rType.String, rID.String
		e.ProjectID, e.TaskID, e.ExecutionID, e.CorrelationID =
			projectID.String, taskID.String, execID.String, corr.String
		e.Outcome, e.DetailsJSON, e.PreviousHash = outcome.String, details.String, prevHash.String
		e.EntryHash, e.Signature, e.CreatedAt = entryHash.String, signature.String, created.Int64
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit_entries: %v", err)
	}
	return out
}

func chainStatus(t *testing.T, d *db.DB, anchor *audit.Anchor) (audit.ChainStatus, error) {
	t.Helper()
	return audit.Check(readBackChain(t, d), anchor)
}

// anchorAtTheStoredHead is what cmd/core does on a clean shutdown: commit the
// newest row's (seq, entry_hash) to the anchor file outside the database.
func anchorAtTheStoredHead(t *testing.T, d *db.DB, anchor *audit.Anchor) {
	t.Helper()
	seq, hash := lastAuditRow(t, d)
	if err := anchor.Commit(seq, hash, itNow); err != nil {
		t.Fatalf("anchor commit: %v", err)
	}
}

func TestChainHealthTriStateOnRealRows(t *testing.T) {
	t.Run("空库是 UNKNOWN，不是 VERIFIED_LOCAL", func(t *testing.T) {
		d, anchor, _, _ := newAnchoredStore(t)
		status, err := chainStatus(t, d, anchor)
		if status != audit.StatusUnknown {
			t.Errorf("status = %s (err %v), want UNKNOWN for a chain with no rows", status, err)
		}
	})

	t.Run("有行但锚点不存在是 UNVERIFIED", func(t *testing.T) {
		d, anchor, st, _ := newAnchoredStore(t)
		appendRows(t, st, 3)
		status, err := chainStatus(t, d, anchor)
		if status != audit.StatusUnverified {
			t.Errorf("status = %s, want UNVERIFIED when there is no checkpoint to reach", status)
		}
		if !errors.Is(err, audit.ErrNoAnchor) {
			t.Errorf("err = %v, want it to say the anchor is missing", err)
		}
	})

	t.Run("链撞到锚点是 VERIFIED_LOCAL", func(t *testing.T) {
		d, anchor, st, _ := newAnchoredStore(t)
		appendRows(t, st, 3)
		anchorAtTheStoredHead(t, d, anchor)
		status, err := chainStatus(t, d, anchor)
		if status != audit.StatusVerifiedLocal || err != nil {
			t.Fatalf("status = %s %v, want VERIFIED_LOCAL", status, err)
		}
	})

	// The ceiling on that VERIFIED_LOCAL, which the panel wording has to carry:
	// rows after the last checkpoint are the normal state between two "every 1000
	// appends" lines, so the verdict holds for the certified prefix only.
	t.Run("锚点之后继续追加仍是 VERIFIED_LOCAL", func(t *testing.T) {
		d, anchor, st, _ := newAnchoredStore(t)
		appendRows(t, st, 2)
		anchorAtTheStoredHead(t, d, anchor)
		appendRows(t, st, 4)
		status, err := chainStatus(t, d, anchor)
		if status != audit.StatusVerifiedLocal || err != nil {
			t.Fatalf("status = %s %v, want VERIFIED_LOCAL for a certified prefix plus newer rows", status, err)
		}
	})

	// Deleting the tail is the one thing an in-chain hash cannot see: what is left
	// still recomputes perfectly. The anchor outside the database is what makes it
	// visible, so this case is the reason ADR-018 exists.
	t.Run("真库里删掉尾部被检出", func(t *testing.T) {
		ctx := context.Background()
		d, anchor, st, _ := newAnchoredStore(t)
		appendRows(t, st, 5)
		anchorAtTheStoredHead(t, d, anchor)
		if _, err := d.Read.ExecContext(ctx, `DELETE FROM audit_entries WHERE seq > 2`); err != nil {
			t.Fatalf("delete tail: %v", err)
		}
		status, err := chainStatus(t, d, anchor)
		if status == audit.StatusVerifiedLocal {
			t.Fatalf("a chain truncated below its anchor still certified: %s %v", status, err)
		}
		if status != audit.StatusUnverified {
			t.Errorf("status = %s %v, want UNVERIFIED", status, err)
		}
		// Without the anchor the same deletion is invisible — recorded here so
		// nobody later "proves" hash chains alone detect truncation.
		if _, err := d.Read.ExecContext(ctx, `DELETE FROM audit_entries WHERE seq > 2`); err != nil {
			t.Fatalf("second delete: %v", err)
		}
		if head, err := audit.Checkpoint(readBackChain(t, d)); err != nil {
			t.Fatal(err)
		} else if err := audit.VerifyChain(readBackChain(t, d), head); err != nil {
			t.Errorf("the truncated remainder should still be self-consistent: %v", err)
		}
	})

	t.Run("真库里改写一行被检出", func(t *testing.T) {
		ctx := context.Background()
		d, anchor, st, _ := newAnchoredStore(t)
		appendRows(t, st, 4)
		anchorAtTheStoredHead(t, d, anchor)
		if _, err := d.Read.ExecContext(ctx, `UPDATE audit_entries SET outcome = 'DENIED' WHERE seq = 2`); err != nil {
			t.Fatalf("rewrite a row: %v", err)
		}
		status, err := chainStatus(t, d, anchor)
		if status == audit.StatusVerifiedLocal {
			t.Errorf("a rewritten outcome still certified: %s %v", status, err)
		}
	})

	// The reason Chain seals over canonicalJSON: the platform layer re-encodes JSON
	// columns with sorted keys, so sealing over the caller's raw string would store
	// bytes no honest recomputation reproduces. The keys below arrive unsorted.
	t.Run("未排序键的 details_json 也能撞到锚点", func(t *testing.T) {
		ctx := context.Background()
		d, anchor, st, _ := newAnchoredStore(t)
		if err := st.d.Write(ctx, func(tx *db.Tx) error {
			ch := st.chain(tx)
			for _, id := range []string{"audit-unsorted-1", "audit-unsorted-2"} {
				if _, err := ch.Append(ctx, audit.Entry{
					AuditID: id, Action: "test.roundtrip", Outcome: audit.OutcomeSuccess,
					DetailsJSON: `{"z":1,"m":"repo:src/main.go","a":2}`,
				}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
		stored := readBackChain(t, d)
		if len(stored) != 2 {
			t.Fatalf("%d rows, want 2", len(stored))
		}
		if stored[0].DetailsJSON != `{"a":2,"m":"repo:src/main.go","z":1}` {
			t.Logf("stored details_json = %s", stored[0].DetailsJSON)
		}
		anchorAtTheStoredHead(t, d, anchor)
		status, err := chainStatus(t, d, anchor)
		if status != audit.StatusVerifiedLocal || err != nil {
			t.Fatalf("status = %s %v, want VERIFIED_LOCAL over re-encoded JSON — the seal must cover the stored bytes", status, err)
		}
	})

	// Measurement, not an assertion yet: what does a value the redactor scrubs do
	// to a hash that was sealed over the pre-scrub string?
	t.Run("PROBE-scrubbed-value-in-details", func(t *testing.T) {
		ctx := context.Background()
		d, anchor, st, _ := newAnchoredStore(t)
		if err := st.d.Write(ctx, func(tx *db.Tx) error {
			ch := st.chain(tx)
			_, err := ch.Append(ctx, audit.Entry{
				AuditID: "audit-probe-1", Action: "test.probe", Outcome: audit.OutcomeSuccess,
				DetailsJSON: `{"note":"token=ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","z":1}`,
			})
			return err
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
		got := readBackChain(t, d)
		t.Logf("stored details = %s", got[0].DetailsJSON)
		anchorAtTheStoredHead(t, d, anchor)
		status, err := chainStatus(t, d, anchor)
		t.Logf("status = %s err = %v", status, err)
	})
}
