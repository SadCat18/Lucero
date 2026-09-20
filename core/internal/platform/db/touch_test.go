package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// auditRules mirrors what cmd/core injects from audit.Rules(): deny keys in
// audit's canonical form (no '_', '-' or '.') and its value patterns. This
// package must not import audit, so the shape is reproduced here on purpose.
func auditRules() (denyKeys, patterns []string) {
	return []string{
		"token", "accesstoken", "refreshtoken", "idtoken", "bearertoken",
		"apikey", "xapikey", "authorization", "auth", "cookie", "setcookie",
		"password", "passwd", "secret", "secretkey", "clientsecret",
		"sessionkey", "sessiontoken", "privatekey", "accesskey", "secretaccesskey",
		"credential", "credentials", "connectionstring", "dsn",
	}, []string{
		`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`,
		`gh[pousr]_[A-Za-z0-9]{20,}`,
		`github_pat_[A-Za-z0-9_]{20,}`,
		`sk-[A-Za-z0-9_-]{16,}`,
		`xox[baprs]-[A-Za-z0-9-]{10,}`,
		`AKIA[0-9A-Z]{16}`,
		`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`,
		`(?i)bearer\s+[A-Za-z0-9._~+/=-]{16,}`,
	}
}

// newMigrated returns a migrated database with redaction switched on.
func newMigrated(t *testing.T) *DB {
	t.Helper()
	deny, patterns := auditRules()
	if err := InitRedaction(deny, patterns, ""); err != nil {
		t.Fatalf("InitRedaction: %v", err)
	}
	d := openTemp(t)
	if err := d.Migrate(repoMigrations(t)); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return d
}

func createTask(t *testing.T, d *DB, id string) int64 {
	t.Helper()
	now := int64(1700000000000)
	var v int64
	err := d.Write(context.Background(), func(tx *Tx) error {
		err := tx.Create(AggTask, id, []string{"title", "created_at", "updated_at"},
			[]any{"t", now, now},
			EventDraft{EventID: "ev-" + id + "-1", Type: "task_created", AggregateID: id})
		if err != nil {
			return err
		}
		var err2 error
		v, err2 = tx.Touch(AggTask, id, "status = ?", []any{"QUEUED"},
			EventDraft{EventID: "ev-" + id + "-2", Type: "task_queued", AggregateID: id})
		return err2
	})
	if err != nil {
		t.Fatalf("createTask %s: %v", id, err)
	}
	return v
}

func payloadOf(t *testing.T, d *DB, eventID string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE event_id = ?`, eventID).Scan(&s); err != nil {
		t.Fatalf("read payload for %s: %v", eventID, err)
	}
	return s
}

func TestTouchReturnsIncrementingVersion(t *testing.T) {
	d := newMigrated(t)
	createTask(t, d, "task-1")

	for want := int64(3); want <= 5; want++ {
		var got int64
		err := d.Write(context.Background(), func(tx *Tx) error {
			var err error
			got, err = tx.Touch(AggTask, "task-1", "status = ?, updated_at = ?",
				[]any{"RUNNING", int64(1700000000001)},
				EventDraft{EventID: fmt.Sprintf("ev-task-1-%d", want), Type: "task_running", AggregateID: "task-1"})
			return err
		})
		if err != nil {
			t.Fatalf("touch %d: %v", want, err)
		}
		if got != want {
			t.Fatalf("Touch returned version %d; want %d", got, want)
		}
	}

	var stored int64
	if err := d.Read.QueryRow(`SELECT version FROM tasks WHERE id='task-1'`).Scan(&stored); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if stored != 5 {
		t.Fatalf("tasks.version = %d; want 5", stored)
	}
	// The events carry exactly the versions the aggregate went through.
	rows, err := d.Read.Query(`SELECT aggregate_version FROM event_journal
		WHERE aggregate_type='task' AND aggregate_id='task-1' ORDER BY seq`)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	defer rows.Close()
	var seen []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen = append(seen, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	want := []int64{1, 2, 3, 4, 5}
	if len(seen) != len(want) {
		t.Fatalf("event versions = %v; want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("event versions = %v; want %v", seen, want)
		}
	}
}

// Concurrent writers against the same aggregate must not be handed the same
// version: taking it and changing state is one statement on one write channel.
func TestConcurrentTouchesProduceNoDuplicateVersions(t *testing.T) {
	d := newMigrated(t)
	createTask(t, d, "task-x")

	const writers, each = 6, 8
	var mu sync.Mutex
	seen := map[int64]int{}
	touchErrs := make([]error, 0)
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				err := d.Write(context.Background(), func(tx *Tx) error {
					v, err := tx.Touch(AggTask, "task-x", "priority = ?", []any{"HIGH"},
						EventDraft{EventID: fmt.Sprintf("cx-%d-%d", w, i), Type: "task_queued", AggregateID: "task-x"})
					if err != nil {
						return err
					}
					mu.Lock()
					seen[v]++
					mu.Unlock()
					return nil
				})
				mu.Lock()
				if err != nil {
					touchErrs = append(touchErrs, err)
				}
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	if len(touchErrs) > 0 {
		t.Fatalf("%d concurrent touches failed, first: %v", len(touchErrs), touchErrs[0])
	}
	if len(seen) != writers*each {
		t.Fatalf("%d touches produced %d distinct versions, want %d", writers*each, len(seen), writers*each)
	}
	for v, n := range seen {
		if n != 1 {
			t.Fatalf("version %d handed out %d times", v, n)
		}
	}
	// ...and every version is also in the journal, with no duplicates there.
	var dupes int
	err := d.Read.QueryRow(`SELECT count(*) FROM (
		SELECT aggregate_version FROM event_journal
		WHERE aggregate_type='task' AND aggregate_id='task-x'
		GROUP BY aggregate_version HAVING count(*) > 1)`).Scan(&dupes)
	if err != nil {
		t.Fatalf("count duplicate journal versions: %v", err)
	}
	if dupes != 0 {
		t.Fatalf("%d aggregate_versions appear twice in event_journal", dupes)
	}
}

// A write that bypasses Touch and reuses an (aggregate_type, aggregate_id,
// aggregate_version) triple must be rejected by the schema itself.
func TestEventJournalUniqueConstraintBlocksBypass(t *testing.T) {
	d := newMigrated(t)
	createTask(t, d, "task-u")

	const dup = `INSERT INTO event_journal
		(event_id, event_type, aggregate_type, aggregate_id, aggregate_version, payload_json, created_at)
		VALUES ('bypass-1','task_queued','task','task-u',2,'{"schema":1}',1700000000000)`
	if _, err := d.Read.Exec(dup); err == nil {
		t.Fatal("duplicate aggregate_version was accepted")
	} else {
		t.Logf("expected rejection: %v", err)
	}

	// The same statement with a fresh version must succeed, proving the previous
	// failure came from the constraint and not from the statement shape.
	if _, err := d.Read.Exec(strings.Replace(dup, "'task-u',2", "'task-u',99", 1)); err != nil {
		t.Fatalf("non-conflicting insert failed: %v", err)
	}
}

// deny-key values must not survive into payload_json, and the scrubbing must be
// reported by rule name rather than by value.
func TestDenyKeyValuesNeverReachPayloadJSON(t *testing.T) {
	d := newMigrated(t)
	now := int64(1700000000000)
	var hits []RedactionHit
	err := d.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Create(AggTask, "task-r", []string{"title", "created_at", "updated_at"},
			[]any{"t", now, now}, EventDraft{EventID: "r-0", Type: "task_created", AggregateID: "task-r"}); err != nil {
			return err
		}
		_, err := tx.Touch(AggTask, "task-r", "status = ?", []any{"RUNNING"}, EventDraft{
			EventID: "r-1", Type: "task_running", AggregateID: "task-r",
			Payload: map[string]any{
				"x-api-key":     "super-secret-value",
				"authorization": "Bearer abcdefghijklmnopqrstuvwx",
				"note":          "the header was Bearer abcdefghijklmnopqrstuvwx",
				"token":         "vault://github_token",
				"nested":        map[string]any{"API_KEY": "sk-abcdefghijklmnopqrstuvwxyz"},
				"list":          []any{map[string]any{"password": "hunter2hunter2"}, "safe"},
				"keep":          "visible",
			},
		})
		hits = tx.RedactionHits()
		return err
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	stored := payloadOf(t, d, "r-1")
	for _, leak := range []string{"super-secret-value", "sk-abcdefghijklmnopqrstuvwxyz", "hunter2hunter2", "abcdefghijklmnopqrstuvwx"} {
		if strings.Contains(stored, leak) {
			t.Fatalf("payload_json leaked %q: %s", leak, stored)
		}
	}
	for _, want := range []string{`"[REDACTED]"`, `"visible"`, `"schema":1`, `"vault://github_token"`} {
		if !strings.Contains(stored, want) {
			t.Fatalf("payload_json missing %s: %s", want, stored)
		}
	}
	t.Logf("stored payload: %s", stored)

	type rule struct {
		kind  HitKind
		match string
	}
	counted := map[rule]int{}
	for _, h := range hits {
		counted[rule{h.Kind, h.Match}] += h.Count
		if strings.Contains(fmt.Sprint(h), "super-secret") || strings.Contains(h.Match, "sk-abc") {
			t.Fatalf("hit record carries the original value: %+v", h)
		}
		if strings.HasPrefix(h.Match, "key:") {
			t.Fatalf("REQ-T3 not honoured: Match still carries the %q prefix convention: %+v", "key:", h)
		}
		if h.Kind != HitKey && h.Kind != HitPattern {
			t.Fatalf("hit %v carries Kind %q; want %q or %q", h.Match, h.Kind, HitKey, HitPattern)
		}
	}
	// Deny-key hits: the rule name in canonical form, tagged as key.
	for _, want := range []rule{
		{HitKey, "xapikey"}, {HitKey, "apikey"}, {HitKey, "password"}, {HitKey, "authorization"},
	} {
		if counted[want] == 0 {
			t.Fatalf("no hit recorded for %+v; got %v", want, counted)
		}
	}
	// Free text under a non-denied key is only caught by the value patterns,
	// and is tagged as pattern.
	var patternHits []rule
	for r, n := range counted {
		if r.kind == HitPattern {
			if n == 0 || !strings.HasPrefix(r.match, "(?i)bearer") {
				t.Fatalf("unexpected pattern hit %q (n=%d)", r.match, n)
			}
			patternHits = append(patternHits, r)
		}
	}
	if len(patternHits) != 1 {
		t.Fatalf("pattern hits = %v; want exactly the bearer rule", patternHits)
	}
	if counted[rule{HitKey, "token"}] != 0 {
		t.Fatalf("a vault:// reference was redacted: %v", counted)
	}
}

// Fail-closed: a bad pattern must not start Core, and a write before
// InitRedaction must not be able to store anything.
func TestRedactionFailsClosed(t *testing.T) {
	old := red.Swap(nil)
	t.Cleanup(func() { red.Store(old) })

	if err := InitRedaction([]string{"token"}, []string{`(unclosed`}, ""); err == nil {
		t.Fatal("InitRedaction accepted a pattern that cannot compile")
	} else {
		t.Logf("expected failure: %v", err)
	}
	if err := InitRedaction(nil, nil, ""); err == nil {
		t.Fatal("InitRedaction accepted an empty rule set")
	}
	red.Store(nil)

	d := openTemp(t)
	if err := d.Migrate(repoMigrations(t)); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	err := d.Write(context.Background(), func(tx *Tx) error {
		return tx.Create(AggTask, "t-noredact", []string{"title", "created_at", "updated_at"},
			[]any{"x", int64(1), int64(1)},
			EventDraft{EventID: "nr-1", Type: "task_created", AggregateID: "t-noredact"})
	})
	if err == nil {
		t.Fatal("Create wrote a payload with redaction uninitialized")
	}
	t.Logf("expected failure: %v", err)
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM tasks WHERE id='t-noredact'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatal("the failed write left a task row behind")
	}
}

// Only validated "column = ?" pairs are compiled into SQL, and only for columns
// that exist and are not platform-owned.
func TestWriteWhitelistRejectsAnythingElse(t *testing.T) {
	d := newMigrated(t)
	createTask(t, d, "task-w")

	cases := []struct {
		name, set string
		args      []any
	}{
		{"trailing sql", "status = ?; DROP TABLE tasks", []any{"RUNNING"}},
		{"inlined literal", "status = 'RUNNING'", nil},
		{"unknown column", "not_a_column = ?", []any{1}},
		{"platform column", "version = ?", []any{99}},
		{"id rewrite", "id = ?", []any{"other"}},
		{"empty", "", nil},
	}
	for _, tc := range cases {
		err := d.Write(context.Background(), func(tx *Tx) error {
			_, err := tx.Touch(AggTask, "task-w", tc.set, tc.args,
				EventDraft{EventID: "w-" + tc.name, Type: "task_queued", AggregateID: "task-w"})
			return err
		})
		if err == nil {
			t.Fatalf("%s: Touch accepted setSQL %q", tc.name, tc.set)
		}
		t.Logf("%s rejected: %v", tc.name, err)
	}

	var version int64
	var tables int
	if err := d.Read.QueryRow(`SELECT version FROM tasks WHERE id='task-w'`).Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if err := d.Read.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='tasks'`).Scan(&tables); err != nil {
		t.Fatalf("count: %v", err)
	}
	if version != 2 || tables != 1 {
		t.Fatalf("after rejected touches: version=%d tasks table present=%d; want 2 and 1", version, tables)
	}
}

// An aggregate change whose event is invalid must not commit on its own: the
// whole transaction rolls back (ADR-008).
func TestRejectedEventRollsBackTheAggregateChange(t *testing.T) {
	d := newMigrated(t)
	createTask(t, d, "task-rb")

	err := d.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.Touch(AggTask, "task-rb", "status = ?", []any{"RUNNING"},
			EventDraft{EventID: "rb-1", Type: "TaskCreated", AggregateID: "task-rb"})
		return err
	})
	if err == nil {
		t.Fatal("an unregistered event type was accepted")
	}
	t.Logf("expected failure: %v", err)

	var version, events int
	if err := d.Read.QueryRow(`SELECT version FROM tasks WHERE id='task-rb'`).Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_id LIKE 'rb-%'`).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if version != 2 || events != 0 {
		t.Fatalf("after the rejected tx: version=%d events=%d; want 2 and 0", version, events)
	}
}

// Append is for append-only rows: it may not write an aggregate row, and it
// scrubs TEXT columns the same way Touch does.
func TestAppendWritesAppendOnlyRowsAndRefusesAggregates(t *testing.T) {
	d := newMigrated(t)
	now := int64(1700000000000)
	createTask(t, d, "task-a")

	var id int64
	err := d.Write(context.Background(), func(tx *Tx) error {
		if _, err := tx.Append("tasks", []string{"id", "title", "created_at", "updated_at"},
			[]any{"sneaky", "t", now, now}); err == nil {
			return errors.New("Append wrote an aggregate row")
		} else {
			t.Logf("aggregate append refused: %v", err)
		}
		if _, err := tx.Append("principals", []string{"id"}, []any{"x"}); err == nil {
			return errors.New("Append accepted an insert that violates NOT NULL")
		}
		var err error
		id, err = tx.Append("principals",
			[]string{"id", "principal_type", "display_name", "created_at", "updated_at", "metadata_json"},
			[]any{"local-user", "USER", "me", now, now,
				map[string]any{"nickname": "me", "access_token": "super-secret-value"}})
		return err
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id == 0 {
		t.Fatal("Append returned no rowid")
	}

	var meta string
	if err := d.Read.QueryRow(`SELECT metadata_json FROM principals WHERE id='local-user'`).Scan(&meta); err != nil {
		t.Fatalf("read principal: %v", err)
	}
	if strings.Contains(meta, "super-secret-value") {
		t.Fatalf("Append leaked a secret into %s", meta)
	}
	if !strings.Contains(meta, "[REDACTED]") {
		t.Fatalf("Append did not redact: %s", meta)
	}

	// principal_id is a real FK: an unknown one must not be storable.
	err = d.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.Touch(AggTask, "task-a", "status = ?", []any{"RUNNING"},
			EventDraft{EventID: "p-1", Type: "task_running", AggregateID: "task-a", PrincipalID: "nobody"})
		return err
	})
	if err == nil {
		t.Fatal("an event with an unknown principal was accepted")
	}
	t.Logf("expected failure: %v", err)
}

// REQ-A2 (§12 P0): a vault:// prefix must not smuggle a secret past redaction.
// Both paths are covered: a plain string value, and the same value sitting under
// a denied key, where a clean reference is allowed to survive.
func TestVaultPrefixDoesNotSmuggleSecret(t *testing.T) {
	d := newMigrated(t)
	now := int64(1700000000000)
	const secret = "sk-ABCDEFGHIJKLMNOP"

	err := d.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Create(AggTask, "task-vault", []string{"title", "created_at", "updated_at"},
			[]any{"t", now, now}, EventDraft{EventID: "v-0", Type: "task_created", AggregateID: "task-vault"}); err != nil {
			return err
		}
		_, err := tx.Touch(AggTask, "task-vault", "status = ?", []any{"RUNNING"}, EventDraft{
			EventID: "v-1", Type: "task_running", AggregateID: "task-vault",
			Payload: map[string]any{
				"note":      "vault://" + "x " + secret,
				"token":     "vault://x " + secret,
				"clean":     "vault://github_token",
				"untouched": "vault://plain_ref",
			},
		})
		return err
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	stored := payloadOf(t, d, "v-1")
	if strings.Contains(stored, secret) {
		t.Fatalf("P0: the vault:// prefix smuggled a secret into payload_json: %s", stored)
	}
	if !strings.Contains(stored, "[REDACTED]") {
		t.Fatalf("expected the secret to be replaced: %s", stored)
	}
	// A reference that carries nothing secret still survives under a denied key.
	if !strings.Contains(stored, `"clean":"vault://github_token"`) {
		t.Fatalf("a clean vault reference was destroyed: %s", stored)
	}
	t.Logf("stored payload: %s", stored)
}

// Q4 ruling: quota's aggregate_type must be "quota" so it matches the
// quota_observed event prefix (Event Contract §2).
func TestAggQuotaMatchesEventPrefix(t *testing.T) {
	if string(AggQuota) != "quota" {
		t.Fatalf("AggQuota = %q; want %q", AggQuota, "quota")
	}
	if got := aggTable[AggQuota]; got != "quota_accounts" {
		t.Fatalf("AggQuota maps to table %q; want quota_accounts", got)
	}
	d := newMigrated(t)
	now := int64(1700000000000)
	err := d.Write(context.Background(), func(tx *Tx) error {
		return tx.Create(AggQuota, "quota-1", []string{"adapter_instance_id", "created_at", "updated_at"},
			[]any{"nope", now, now},
			EventDraft{EventID: "q-1", Type: "quota_observed", Aggregate: AggQuota, AggregateID: "quota-1"})
	})
	// Reaching the FK error proves quota_observed + AggQuota("quota") passed the
	// aggregate-type check inside appendEvent.
	if err == nil {
		t.Fatal("expected the adapter_instance_id FK to reject this row")
	}
	if errors.Is(err, ErrAggregateNotFound) {
		t.Fatalf("quota aggregate kind rejected outright: %v", err)
	}
	t.Logf("reached the FK as expected: %v", err)
	var bad int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE aggregate_type='quota_account'`).Scan(&bad); err != nil {
		t.Fatalf("count: %v", err)
	}
	if bad != 0 {
		t.Fatalf("%d events were written with the old aggregate_type quota_account", bad)
	}
}

// 契约 §1.1.6: Touch never creates a row on the side, and Create writes
// version = 1 explicitly so the row and its first event cannot disagree.
func TestTouchDoesNotCreateAndCreateStartsAtVersionOne(t *testing.T) {
	d := newMigrated(t)

	err := d.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.Touch(AggTask, "ghost", "status = ?", []any{"QUEUED"},
			EventDraft{EventID: "g-1", Type: "task_queued", AggregateID: "ghost"})
		return err
	})
	if !errors.Is(err, ErrAggregateNotFound) {
		t.Fatalf("Touch on a missing id = %v; want ErrAggregateNotFound", err)
	}
	var rows, events int
	if err := d.Read.QueryRow(`SELECT count(*) FROM tasks WHERE id='ghost'`).Scan(&rows); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_id='g-1'`).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if rows != 0 || events != 0 {
		t.Fatalf("the rejected Touch left tasks=%d events=%d; want 0 and 0", rows, events)
	}

	now := int64(1700000000000)
	newTask := func(id, eventID string) error {
		return d.Write(context.Background(), func(tx *Tx) error {
			return tx.Create(AggTask, id, []string{"title", "created_at", "updated_at"},
				[]any{"t", now, now},
				EventDraft{EventID: eventID, Type: "task_created", AggregateID: id})
		})
	}
	if err := newTask("task-c1", "c1-1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var version, evVersion int64
	if err := d.Read.QueryRow(`SELECT version FROM tasks WHERE id='task-c1'`).Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if err := d.Read.QueryRow(`SELECT aggregate_version FROM event_journal WHERE event_id='c1-1'`).Scan(&evVersion); err != nil {
		t.Fatalf("read event version: %v", err)
	}
	if version != 1 || evVersion != 1 {
		t.Fatalf("first version: row=%d event=%d; want 1 and 1", version, evVersion)
	}

	// The caller pre-generates id for idempotent retries, so a second Create with
	// the same id must collide rather than fork the aggregate.
	if err := newTask("task-c1", "c1-2"); err == nil {
		t.Fatal("Create accepted a duplicate id")
	} else {
		t.Logf("duplicate id refused: %v", err)
	}

	// id and version are Create's own to set.
	for _, cols := range [][]string{{"id", "title"}, {"version", "title"}} {
		err := d.Write(context.Background(), func(tx *Tx) error {
			return tx.Create(AggTask, "task-c2", cols, []any{"x", "y"},
				EventDraft{EventID: "c2-1", Type: "task_created", AggregateID: "task-c2"})
		})
		if err == nil {
			t.Fatalf("Create accepted columns %v", cols)
		}
		t.Logf("columns %v refused: %v", cols, err)
	}
}

// 契约 §1.1.7: multi-column setSQL is legal, one placeholder per column.
func TestTouchAcceptsMultipleColumns(t *testing.T) {
	d := newMigrated(t)
	createTask(t, d, "task-multi")

	var v int64
	err := d.Write(context.Background(), func(tx *Tx) error {
		var err error
		v, err = tx.Touch(AggTask, "task-multi", "status = ?, priority = ?, updated_at = ?",
			[]any{"RUNNING", "HIGH", int64(1700000000009)},
			EventDraft{EventID: "m-1", Type: "task_running", AggregateID: "task-multi"})
		return err
	})
	if err != nil {
		t.Fatalf("multi-column Touch: %v", err)
	}
	if v != 3 {
		t.Fatalf("version = %d; want 3", v)
	}
	var status, priority string
	if err := d.Read.QueryRow(`SELECT status, priority FROM tasks WHERE id='task-multi'`).
		Scan(&status, &priority); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "RUNNING" || priority != "HIGH" {
		t.Fatalf("stored %s/%s; want RUNNING/HIGH", status, priority)
	}
}
