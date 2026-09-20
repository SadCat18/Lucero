package approval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// The duplicate-insert case above fails before any audit row is written, so it
// cannot tell "checkpoint after commit" from "checkpoint whenever an append was
// attempted". This one appends for real and then rolls the transaction back.
func TestCheckpointNotWrittenOnRollbackAfterAppend(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d := openMigratedAt(t, filepath.Join(dir, "lucero.db"))
	seed(t, d)

	anchor := &audit.Anchor{Path: filepath.Join(dir, "audit.head")}
	clock := func() int64 { return itNow }
	st := NewSQLiteStore(d, clock)
	cp := audit.NewCheckpointCounter(anchor, clock)
	st.UseCheckpoints(cp)
	if err := cp.Advance(audit.CheckpointEveryAppends-1, audit.Head{Seq: 1, Hash: "primed"}); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	err := st.write(ctx, func(tx *db.Tx, ch *audit.Chain) error {
		if _, err := ch.Append(ctx, audit.Entry{
			AuditID: "audit-rollback", Action: "test.rollback", Outcome: audit.OutcomeSuccess,
		}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("write returned %v, want the injected failure", err)
	}
	if n := countRows(t, d, `SELECT count(*) FROM audit_entries`); n != 0 {
		t.Errorf("the rolled-back audit row reached the table: %d rows", n)
	}
	if got := cp.Counted(); got != audit.CheckpointEveryAppends-1 {
		t.Errorf("counter moved on a rolled-back write: %d", got)
	}
	if lines := readAnchorFile(t, anchor); len(lines) != 0 {
		t.Errorf("a rolled-back write left a checkpoint line: %v", lines)
	}

	// Same append, this time committed: the line must appear.
	if err := st.write(ctx, func(tx *db.Tx, ch *audit.Chain) error {
		_, err := ch.Append(ctx, audit.Entry{
			AuditID: "audit-commit", Action: "test.commit", Outcome: audit.OutcomeSuccess,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lines := readAnchorFile(t, anchor)
	if len(lines) != 1 {
		t.Fatalf("%d checkpoint lines after a committed append, want 1", len(lines))
	}
	var seq int64
	var hash string
	if err := d.Read.QueryRow(`SELECT seq, entry_hash FROM audit_entries`).Scan(&seq, &hash); err != nil {
		t.Fatal(err)
	}
	if lines[0] != strconv.FormatInt(seq, 10)+"	"+hash+"	"+strconv.FormatInt(itNow, 10) {
		t.Errorf("checkpoint %q does not describe the row in the database (%d %s)", lines[0], seq, hash)
	}
}

// T-34's third case, end to end: a checkpoint line may only ever follow a
// committed transaction, and it must name the row that is actually in the
// database. A counter that advanced on a rolled-back write, or an anchor that
// got ahead of the rows, would turn the next crash into a false tampering alarm.
func TestCheckpointWrittenOnlyAfterCommit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "lucero.db")
	d := openMigratedAt(t, path)
	seed(t, d)

	anchor := &audit.Anchor{Path: filepath.Join(dir, "audit.head")}
	clock := func() int64 { return itNow }
	st := NewSQLiteStore(d, clock)
	cp := audit.NewCheckpointCounter(anchor, clock)
	st.UseCheckpoints(cp)
	svc := NewService(st, clock)

	// Sit one append below the boundary, with a deliberately old head.
	if err := cp.Advance(audit.CheckpointEveryAppends-1, audit.Head{Seq: 1, Hash: "primed"}); err != nil {
		t.Fatal(err)
	}
	if lines := readAnchorFile(t, anchor); len(lines) != 0 {
		t.Fatalf("%d checkpoint lines before the boundary: %v", len(lines), lines)
	}

	a, err := svc.Request(ctx, draft())
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	lines := readAnchorFile(t, anchor)
	if len(lines) != 1 {
		t.Fatalf("%d checkpoint lines after crossing the boundary, want 1", len(lines))
	}
	var storedSeq int64
	var storedHash string
	if err := d.Read.QueryRow(`SELECT seq, entry_hash FROM audit_entries ORDER BY seq DESC LIMIT 1`).
		Scan(&storedSeq, &storedHash); err != nil {
		t.Fatal(err)
	}
	want := []string{strconv.FormatInt(storedSeq, 10), storedHash, strconv.FormatInt(itNow, 10)}
	if got := strings.Split(lines[0], "\t"); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("checkpoint = %q, want the newest stored row %v — the anchor must never lead or lag the database", got, want)
	}
	if got := cp.Counted(); got != audit.CheckpointEveryAppends {
		t.Errorf("counted %d, want %d", got, audit.CheckpointEveryAppends)
	}

	// Same row twice: the insert fails and the transaction rolls back. Nothing
	// may have been counted or appended.
	before := cp.Counted()
	if err := st.Insert(ctx, a); err == nil {
		t.Fatal("inserting the same approval id twice succeeded; the rollback path is not being exercised")
	}
	if got := cp.Counted(); got != before {
		t.Errorf("a rolled-back write advanced the counter from %d to %d", before, got)
	}
	if got := readAnchorFile(t, anchor); len(got) != 1 {
		t.Errorf("a rolled-back write left a checkpoint line: %v", got)
	}

	// And a store without a counter stays purely transactional.
	plain := NewService(NewSQLiteStore(d, clock), clock)
	if _, err := plain.Request(ctx, draft()); err != nil {
		t.Fatal(err)
	}
	if got := readAnchorFile(t, anchor); len(got) != 1 {
		t.Errorf("a store that never asked for checkpoints wrote one: %v", got)
	}
}

func readAnchorFile(t *testing.T, a *audit.Anchor) []string {
	t.Helper()
	b, err := os.ReadFile(a.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read anchor: %v", err)
	}
	text := strings.TrimRight(string(b), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
