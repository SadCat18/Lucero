package approval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// Judgement table A18 (internal contract §1.1 area, ADR-018). Both cases are
// written as behaviour: push through the threshold, then compare the anchor file
// against what the database actually holds. Neither one passes by checking that
// a symbol exists somewhere.

func lastAuditRow(t *testing.T, d *db.DB) (int64, string) {
	t.Helper()
	var seq int64
	var hash string
	if err := d.Read.QueryRow(`SELECT seq, entry_hash FROM audit_entries ORDER BY seq DESC LIMIT 1`).
		Scan(&seq, &hash); err != nil {
		t.Fatalf("read audit head: %v", err)
	}
	return seq, hash
}

func newAnchoredStore(t *testing.T) (*db.DB, *audit.Anchor, *SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	d := openMigratedAt(t, filepath.Join(dir, "lucero.db"))
	seed(t, d)
	anchor := &audit.Anchor{Path: filepath.Join(dir, "audit.head")}
	st := NewSQLiteStore(d, func() int64 { return itNow })
	st.UseCheckpoints(audit.NewCheckpointCounter(anchor, func() int64 { return itNow }))
	return d, anchor, st, dir
}

var a18Rows int64

// uniqueAuditID keeps ids distinct across batches; audit_id is UNIQUE, so a
// restarted counter fails the insert rather than hiding a bug.
func uniqueAuditID(tag string) string {
	return fmt.Sprintf("audit-%s-%08d", tag, atomic.AddInt64(&a18Rows, 1))
}

// appendRows writes n audit rows in one committed transaction, which is what
// "连续成功 append" means from the store's point of view.
func appendRows(t *testing.T, st *SQLiteStore, n int) {
	t.Helper()
	var ch *audit.Chain
	err := st.d.Write(context.Background(), func(tx *db.Tx) error {
		ch = st.chain(tx)
		for i := 0; i < n; i++ {
			if _, err := ch.Append(context.Background(), audit.Entry{
				AuditID: uniqueAuditID("a18"), Action: "test.threshold",
				Outcome: audit.OutcomeSuccess,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("%d appends: %v", n, err)
	}
	if err := ch.Checkpoint(); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
}

// A18-1: crossing the threshold must produce exactly one new anchor line, and
// that line's seq must be the last row actually in the database.
func TestA181ThresholdCrossingAnchorsTheStoredHead(t *testing.T) {
	ctx := context.Background()
	d, anchor, st, _ := newAnchoredStore(t)

	if _, err := os.Stat(anchor.Path); !os.IsNotExist(err) {
		t.Fatalf("anchor file already exists: %v", err)
	}

	// One append short of the threshold: nothing may be written yet. This is the
	// half of the judgement that a "it eventually wrote a line" test cannot see.
	appendRows(t, st, audit.CheckpointEveryAppends-1)
	if lines := readAnchorFile(t, anchor); len(lines) != 0 {
		t.Fatalf("%d anchor lines before the threshold: %v", len(lines), lines)
	}
	justBefore, _ := lastAuditRow(t, d)
	if justBefore != audit.CheckpointEveryAppends-1 {
		t.Fatalf("fixture: %d rows stored, want %d", justBefore, audit.CheckpointEveryAppends-1)
	}

	// The one append that crosses it.
	appendRows(t, st, 1)
	lines := readAnchorFile(t, anchor)
	if len(lines) != 1 {
		t.Fatalf("%d anchor lines after crossing, want 1: %v", len(lines), lines)
	}
	atSeq, atHash := lastAuditRow(t, d)
	fields := strings.Split(lines[0], "\t")
	if len(fields) != 3 {
		t.Fatalf("anchor line %q is not seq\\thash\\ttime", lines[0])
	}
	if fields[0] != strconv.FormatInt(atSeq, 10) {
		t.Errorf("anchor seq %s, but the last row in the database is %d", fields[0], atSeq)
	}
	if fields[1] != atHash {
		t.Errorf("anchor hash %s, but the last row's entry_hash is %s", fields[1], atHash)
	}
	if fields[2] != strconv.FormatInt(itNow, 10) {
		t.Errorf("anchor time %s, want the clock %d", fields[2], itNow)
	}

	// A second whole batch crosses once more, and the new line still names the
	// row that is actually there.
	appendRows(t, st, audit.CheckpointEveryAppends)
	lines = readAnchorFile(t, anchor)
	if len(lines) != 2 {
		t.Fatalf("%d anchor lines after a second crossing, want 2: %v", len(lines), lines)
	}
	atSeq, atHash = lastAuditRow(t, d)
	if got := strings.Split(lines[1], "\t")[0]; got != strconv.FormatInt(atSeq, 10) {
		t.Errorf("second anchor seq %s, last stored row is %d", got, atSeq)
	}
	if got := strings.Split(lines[1], "\t")[1]; got != atHash {
		t.Errorf("second anchor hash %s, last stored hash is %s", got, atHash)
	}

	// An empty transaction must not tip anything over or double-write.
	if err := st.write(ctx, func(tx *db.Tx, ch *audit.Chain) error { return nil }); err != nil {
		t.Fatal(err)
	}
	lines = readAnchorFile(t, anchor)
	if len(lines) != 2 {
		t.Errorf("a transaction with no audit row wrote an anchor line: %v", lines)
	}
}

// A18-2: a failed transaction leaves no anchor line, so the file can never lead
// the database. The rows themselves must be gone too, or the absent line would be
// describing nothing.
func TestA182FailedTransactionLeavesNoAnchorLine(t *testing.T) {
	d, anchor, st, _ := newAnchoredStore(t)

	boom := errors.New("injected failure")
	// Appends happen for real, then the transaction is abandoned: every one of
	// them is rolled back, so no seq exists to be anchored.
	err := st.write(context.Background(), func(tx *db.Tx, ch *audit.Chain) error {
		for i := 0; i < audit.CheckpointEveryAppends; i++ {
			if _, aerr := ch.Append(context.Background(), audit.Entry{
				AuditID: uniqueAuditID("boom"), Action: "test.rollback",
				Outcome: audit.OutcomeSuccess,
			}); aerr != nil {
				return aerr
			}
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("write returned %v, want the injected failure", err)
	}
	if n := countRows(t, d, `SELECT count(*) FROM audit_entries`); n != 0 {
		t.Errorf("%d audit rows survived the rollback", n)
	}
	if lines := readAnchorFile(t, anchor); len(lines) != 0 {
		t.Errorf("a failed transaction left anchor lines: %v", lines)
	}

	// Same thing one step further in: a committed crossing first, so the anchor
	// file exists and the assertion is "the line count did not change", not
	// "nothing was ever written".
	appendRows(t, st, audit.CheckpointEveryAppends)
	before := len(readAnchorFile(t, anchor))
	if before != 1 {
		t.Fatalf("%d anchor lines before the second failure, want 1", before)
	}
	err = st.write(context.Background(), func(tx *db.Tx, ch *audit.Chain) error {
		for i := 0; i < audit.CheckpointEveryAppends; i++ {
			if _, aerr := ch.Append(context.Background(), audit.Entry{
				AuditID: uniqueAuditID("boom2"), Action: "test.rollback",
				Outcome: audit.OutcomeSuccess,
			}); aerr != nil {
				return aerr
			}
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("write err = %v", err)
	}
	if after := len(readAnchorFile(t, anchor)); after != before {
		t.Errorf("anchor lines went from %d to %d on a failed transaction", before, after)
	}
	// The rolled-back rows must not be counted either: the next committed batch
	// has to cross on its own merits, not on the ghost rows' debt.
	seqBefore, _ := lastAuditRow(t, d)
	appendRows(t, st, 1)
	if seqNow, _ := lastAuditRow(t, d); seqNow != seqBefore+1 {
		t.Fatalf("stored head moved from %d to %d", seqBefore, seqNow)
	}
	if lines := readAnchorFile(t, anchor); len(lines) != 1 {
		t.Errorf("the ghost rows pushed a crossing: %v", lines)
	}
}
