package adapter

// T-402 ①'s door: the pump downgrades an Adapter's health by adapter_key. These cases are
// about the three columns moving together and about the door refusing what it is not
// entitled to say - which is the part a caller cannot check for itself.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

const healthEventsSQL = "SELECT count(*) FROM event_journal WHERE event_type='adapter_health_changed'"

// healthRow reads the trio every health write owes. Reading all three from one query is the
// point: a test that asked only for `health` could not see a writer that forgot a timestamp,
// and T-402 ① is exactly the "别只改一列" ruling.
func healthRow(t *testing.T, s *SQLiteStore, key string) (health string, checkedAt, updatedAt int64) {
	t.Helper()
	rows, err := s.d.Read.QueryContext(context.Background(),
		"SELECT health, last_healthcheck_at, updated_at FROM adapter_instances WHERE adapter_key = ?", key)
	if err != nil {
		t.Fatalf("read health of %s: %v", key, err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no adapter_instances row for %q", key)
	}
	if err := rows.Scan(&health, &checkedAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	return health, checkedAt, updatedAt
}

// TestDowngradeHealthMovesTheTrioInOneStatement is the pump's happy path on a real SQLite
// over the frozen schema: HEALTHY -> DEGRADED lands, both timestamps move to the same clock
// reading, and the transition is the one thing the journal is told about.
func TestDowngradeHealthMovesTheTrioInOneStatement(t *testing.T) {
	s := openMigrated(t)
	ctx := context.Background()
	probeOnce(t, s)
	// A fixed clock would make "did the timestamps move" unanswerable, so the test moves it.
	s.now = func() int64 { return 1789900600000 }

	before := countRows(t, s, healthEventsSQL)
	changed, err := s.DowngradeHealth(ctx, "fake", HealthDegraded)
	if err != nil {
		t.Fatalf("DowngradeHealth: %v", err)
	}
	if !changed {
		t.Error("changed=false on a HEALTHY -> DEGRADED transition: the caller cannot tell a write from a no-op")
	}
	health, checkedAt, updatedAt := healthRow(t, s, "fake")
	if health != HealthDegraded {
		t.Errorf("health = %q, want %q", health, HealthDegraded)
	}
	if checkedAt != 1789900600000 || updatedAt != 1789900600000 {
		t.Errorf("timestamps = last_healthcheck_at %d, updated_at %d, want both 1789900600000 (T-402 ①: 同笔三列)",
			checkedAt, updatedAt)
	}
	if got := countRows(t, s, healthEventsSQL); got != before+1 {
		t.Errorf("health_changed events = %d, want %d: a real transition has to be in the journal", got, before+1)
	}
	var payload string
	rows, err := s.d.Read.QueryContext(ctx,
		"SELECT payload_json FROM event_journal WHERE event_type='adapter_health_changed' ORDER BY seq DESC LIMIT 1")
	if err != nil {
		t.Fatalf("read the event payload: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no adapter_health_changed row to read")
	}
	if err := rows.Scan(&payload); err != nil {
		t.Fatal(err)
	}
	// §6 lists exactly three payload keys for this event; the test reads from/to back rather
	// than trusting the writer to have filled the map it was handed.
	for _, want := range []string{`"from":"HEALTHY"`, `"to":"DEGRADED"`, `"confidence":"HIGH"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload %q lacks %s", payload, want)
		}
	}
}

// TestDowngradeHealthRefusesWhatItIsNotEntitledToSays is the closed table. `setup` walks the
// row to the starting health through this same door, so every case begins from a state Core
// actually produced. A refused call must leave the row and the journal untouched - "no error"
// is not the claim, "nothing moved" is.
func TestDowngradeHealthRefusesWhatItIsNotEntitledToSays(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		setup  []string
		to     string
		refuse bool // true = an error naming the pair; false = a legitimate no-op
	}{
		{"same state is a no-op", nil, HealthHealthy, false},
		{"DEGRADED back to HEALTHY is a probe's claim", []string{HealthDegraded}, HealthHealthy, true},
		{"UNAVAILABLE back to DEGRADED is an upgrade in disguise", []string{HealthUnavailable}, HealthDegraded, true},
		{"UNAVAILABLE is terminal for this door", []string{HealthUnavailable}, HealthUnavailable, false},
		{"UNKNOWN is not a status a caller chooses", nil, HealthUnknown, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openMigrated(t)
			probeOnce(t, s) // the row starts HEALTHY
			for _, step := range tc.setup {
				if _, err := s.DowngradeHealth(ctx, "fake", step); err != nil {
					t.Fatalf("setup to %s: %v", step, err)
				}
			}
			wantHealth, wantChecked, wantUpdated := healthRow(t, s, "fake")
			before := countRows(t, s, healthEventsSQL)

			changed, err := s.DowngradeHealth(ctx, "fake", tc.to)
			if tc.refuse {
				if err == nil {
					t.Fatalf("%s -> %s was accepted; the row would say something no probe proved", wantHealth, tc.to)
				}
				var e *Error
				if !errors.As(err, &e) || e.Code != ErrInvalidParams {
					t.Errorf("refusal came back as %v, want an *adapter.Error with INVALID_PARAMS", err)
				}
			} else if err != nil {
				t.Fatalf("%s -> %s: %v, want a silent no-op", wantHealth, tc.to, err)
			}
			if changed {
				t.Error("changed=true on a call that was not allowed to write")
			}
			gotHealth, gotChecked, gotUpdated := healthRow(t, s, "fake")
			if gotHealth != wantHealth || gotChecked != wantChecked || gotUpdated != wantUpdated {
				t.Errorf("row moved to %s/%d/%d on a refused call, want %s/%d/%d",
					gotHealth, gotChecked, gotUpdated, wantHealth, wantChecked, wantUpdated)
			}
			if got := countRows(t, s, healthEventsSQL); got != before {
				t.Errorf("health_changed events went %d -> %d on a call that changed nothing (§10: 别把没变化写成变化)", before, got)
			}
		})
	}
}

// TestDowngradeHealthFromAnUnprovedRowOnlyAcceptsFailure covers the one starting state this
// door reads that no writer of ours sets: adapter_instances.health defaults to UNKNOWN
// (000001:93), and the schema's CHECK list also admits STARTING and STOPPED, which nothing in
// the tree writes. UNKNOWN gives way to UNAVAILABLE (a dead process is evidence), never to
// DEGRADED (nothing observed says part of it works), and a row holding a value outside the
// protocol's four is refused rather than reasoned about.
func TestDowngradeHealthFromAnUnprovedRowOnlyAcceptsFailure(t *testing.T) {
	ctx := context.Background()
	// The fixture shape every other test in this tree uses: a direct row over the read pool.
	// It has to be a fixture because no door in this package writes UNKNOWN or STOPPED.
	insert := func(t *testing.T, s *SQLiteStore, key, health string) {
		t.Helper()
		if _, err := s.d.Read.Exec(
			"INSERT INTO adapter_instances (id, adapter_key, display_name, health, created_at, updated_at)"+
				" VALUES (?, ?, ?, ?, 1, 1)", "i-"+key, key, "fixture", health); err != nil {
			t.Fatalf("fixture insert %s=%s: %v", key, health, err)
		}
	}

	s := openMigrated(t)
	insert(t, s, "unknown-key", HealthUnknown)
	if _, err := s.DowngradeHealth(ctx, "unknown-key", HealthDegraded); err == nil {
		t.Error("UNKNOWN -> DEGRADED accepted: no observation says part of this Adapter still works")
	}
	changed, err := s.DowngradeHealth(ctx, "unknown-key", HealthUnavailable)
	if err != nil || !changed {
		t.Fatalf("UNKNOWN -> UNAVAILABLE = (%v, %v), want a write: the process we talk to died", err, changed)
	}
	if h, _, _ := healthRow(t, s, "unknown-key"); h != HealthUnavailable {
		t.Errorf("health = %q, want UNAVAILABLE", h)
	}

	stopped := openMigrated(t)
	insert(t, stopped, "stopped-key", "STOPPED")
	if _, err := stopped.DowngradeHealth(ctx, "stopped-key", HealthUnavailable); err == nil {
		t.Error("a row holding STOPPED was reasoned about; this package never writes that value, so it has no rule for it")
	}
}

// TestDowngradeHealthDoesNotRegisterAnAdapterItOnlyLost is ①'s boundary against the probe
// path: registration belongs to a probe, so a key nobody registered must come back as
// "not found" and must NOT gain a row - a pump that could create rows would turn a lost
// connection into an Adapter Core claims to have seen.
func TestDowngradeHealthDoesNotRegisterAnAdapterItOnlyLost(t *testing.T) {
	s := openMigrated(t)
	ctx := context.Background()
	before := countRows(t, s, "SELECT count(*) FROM adapter_instances")
	changed, err := s.DowngradeHealth(ctx, "never-probed", HealthUnavailable)
	if !errors.Is(err, db.ErrAggregateNotFound) {
		t.Fatalf("unknown key = %v, want an error that stays db.ErrAggregateNotFound", err)
	}
	if changed {
		t.Error("changed=true for a key with no row")
	}
	if got := countRows(t, s, "SELECT count(*) FROM adapter_instances"); got != before {
		t.Errorf("instance rows went %d -> %d: the pump invented a registration", before, got)
	}
	if got := countRows(t, s, healthEventsSQL); got != 0 {
		t.Errorf("health_changed events = %d for a key that was never seen", got)
	}
	// The message has to name both the key and the value it was asked for, or the caller
	// reading a log line cannot tell which adapter the pump lost.
	if msg := err.Error(); !strings.Contains(msg, "never-probed") || !strings.Contains(msg, HealthUnavailable) {
		t.Errorf("refusal %q does not name the key and the target health", msg)
	}
}

// TestBothHealthDoorsWriteTheSameTrio guards this round's refactor: MarkUnverified used to
// carry its own Touch text, and the two doors were merged into one writer. A regression here
// would be silent to the probe tests (they assert health and event counts, not timestamps),
// so it is asserted on the row.
func TestBothHealthDoorsWriteTheSameTrio(t *testing.T) {
	ctx := context.Background()

	s := openMigrated(t)
	probeOnce(t, s)
	s.now = func() int64 { return 1789900900000 }
	if _, err := s.MarkUnverified(ctx, "fake", "Fake Adapter", "launch: refused"); err != nil {
		t.Fatalf("MarkUnverified: %v", err)
	}
	probeHealth, probeChecked, probeUpdated := healthRow(t, s, "fake")
	if probeHealth != HealthUnavailable || probeChecked != 1789900900000 || probeUpdated != 1789900900000 {
		t.Fatalf("probe door wrote %s / %d / %d, want UNAVAILABLE with both timestamps at 1789900900000",
			probeHealth, probeChecked, probeUpdated)
	}
	// The probe door keeps its reason in metadata_json; the pump door must not lose that.
	var meta string
	rows, err := s.d.Read.QueryContext(ctx,
		"SELECT metadata_json FROM adapter_instances WHERE adapter_key = 'fake'")
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&meta); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(meta, "launch: refused") {
		t.Errorf("metadata_json = %q, want the probe reason still in it", meta)
	}

	s2 := openMigrated(t)
	probeOnce(t, s2)
	s2.now = func() int64 { return 1789900900000 }
	if _, err := s2.DowngradeHealth(ctx, "fake", HealthUnavailable); err != nil {
		t.Fatalf("DowngradeHealth: %v", err)
	}
	pumpHealth, pumpChecked, pumpUpdated := healthRow(t, s2, "fake")
	if pumpHealth != probeHealth || pumpChecked != probeChecked || pumpUpdated != probeUpdated {
		t.Errorf("the two doors disagree about the trio: pump %s/%d/%d vs probe %s/%d/%d",
			pumpHealth, pumpChecked, pumpUpdated, probeHealth, probeChecked, probeUpdated)
	}
}

// TestDowngradeHealthRejectsAValueOutsideTheProtocolEnum keeps a typo from reaching the
// table. The CHECK list is wider than the protocol's four values, so SQLite would accept
// STARTING and the write would land as far as the driver is concerned - the door has to be
// the place that says no.
func TestDowngradeHealthRejectsAValueOutsideTheProtocolEnum(t *testing.T) {
	s := openMigrated(t)
	ctx := context.Background()
	probeOnce(t, s)
	before := countRows(t, s, healthEventsSQL)
	for _, to := range []string{"", "STARTING", "STOPPED", "READY", "healthy"} {
		changed, err := s.DowngradeHealth(ctx, "fake", to)
		if err == nil {
			t.Errorf("%q accepted, want a refusal: health %q is not this package's to write", to, to)
			continue
		}
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrInvalidParams {
			t.Errorf("%q refused as %v, want INVALID_PARAMS", to, err)
		}
		if changed {
			t.Errorf("%q reported changed=true", to)
		}
	}
	if got := countRows(t, s, healthEventsSQL); got != before {
		t.Errorf("health_changed events = %d, want %d: refusals must not journal anything", got, before)
	}
	if h, _, _ := healthRow(t, s, "fake"); h != HealthHealthy {
		t.Errorf("health = %q after four refusals, want HEALTHY untouched", h)
	}
}
