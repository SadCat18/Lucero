package adapter

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// openMigrated builds a real SQLite store over the frozen migrations, because the whole
// point of this file is that the SQL and the CHECK constraints are what they are: a fake
// store cannot tell me adapter_instances.health rejects a value outside its CHECK list,
// and that failure would otherwise surface as a 500 in production.
func openMigrated(t *testing.T) *SQLiteStore {
	t.Helper()
	// Redaction must be initialised from audit's own rule set before any write;
	// platform/db fails closed otherwise, so the store's SQL is not the only thing a
	// probe write has to satisfy.
	r := audit.Rules()
	if err := db.InitRedaction(r.DenyKeys, r.Patterns, r.Marker); err != nil {
		t.Fatalf("InitRedaction: %v", err)
	}
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "probe.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s, err := NewSQLiteStore(d, func() int64 { return 1789900000000 })
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return s
}

func countRows(t *testing.T, s *SQLiteStore, q string) int {
	t.Helper()
	rows, err := s.d.Read.Query(q)
	if err != nil {
		t.Fatalf("count %s: %v", q, err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("count %s returned no row", q)
	}
	var n int
	if err := rows.Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestSQLiteStoreRecordsOneProbeEndToEnd asserts the write §7.2 asks for actually lands:
// one instance row, one capability row per verified capability, and one registered event
// per fact - and that a second probe of the same key does not create a second row, since
// adapter_key is UNIQUE and a duplicate would surface as a 500 rather than a clean re-check.
func TestSQLiteStoreRecordsOneProbeEndToEnd(t *testing.T) {
	s := openMigrated(t)
	ctx := context.Background()
	res := ProbeResult{
		AdapterKey: "fake", DisplayName: "Fake Adapter", SoftwareVersion: "26.1.0",
		AdapterVersion: "0.1.0", ProtocolVersion: "1.0", VerifiedAt: 1789900000000,
		HealthState: HealthHealthy, EffectiveBackend: BackendNativeAPI,
		Capabilities: []Capability{{
			Name: "session.send", SupportLevel: LevelNative, EffectiveBackend: BackendNativeAPI,
			Confidence: ConfidenceHigh, LastVerifiedAt: 1789900000000,
		}},
	}
	id, err := s.RecordProbe(ctx, res)
	if err != nil {
		t.Fatalf("first RecordProbe: %v", err)
	}
	if id == "" {
		t.Fatal("RecordProbe returned no instance id")
	}
	if got := countRows(t, s, "SELECT count(*) FROM adapter_instances"); got != 1 {
		t.Errorf("instance rows = %d, want 1", got)
	}
	if got := countRows(t, s, "SELECT count(*) FROM adapter_capabilities"); got != 1 {
		t.Errorf("capability rows = %d, want one per verified capability", got)
	}
	if got := countRows(t, s, "SELECT count(*) FROM event_journal"); got != 2 {
		t.Errorf("journal rows = %d, want adapter_registered + adapter_capability_verified", got)
	}

	// A second probe, same health: the instance row must be reused. The capability row is
	// re-verified, which is the one thing that legitimately repeats.
	res2 := res
	res2.Capabilities[0].LastVerifiedAt = 1789900001000
	id2, err := s.RecordProbe(ctx, res2)
	if err != nil {
		t.Fatalf("second RecordProbe: %v", err)
	}
	if id2 != id {
		t.Errorf("second probe minted instance %q over %q: adapter_key is UNIQUE and a second row is a crash, not a refresh", id2, id)
	}
	if got := countRows(t, s, "SELECT count(*) FROM adapter_instances"); got != 1 {
		t.Errorf("instance rows after re-probe = %d, want still 1", got)
	}
	if got := countRows(t, s, "SELECT count(*) FROM adapter_capabilities"); got != 1 {
		t.Errorf("capability rows after re-probe = %d, want still 1 (upsert, not append)", got)
	}
}

// TestSQLiteStoreHealthChangeIsTheOnlyRepeatedEvent pins the discipline behind §7.2's
// "只记 health_state 变化": a probe that learned nothing new writes no adapter_health_changed,
// because a journal full of HEALTHY-to-HEALTHY transitions is how an audit trail stops
// meaning anything.
func TestSQLiteStoreHealthChangeIsTheOnlyRepeatedEvent(t *testing.T) {
	s := openMigrated(t)
	ctx := context.Background()
	res := ProbeResult{AdapterKey: "fake", DisplayName: "d", VerifiedAt: 1, HealthState: HealthHealthy}
	if _, err := s.RecordProbe(ctx, res); err != nil {
		t.Fatalf("first: %v", err)
	}
	before := countRows(t, s, "SELECT count(*) FROM event_journal WHERE event_type='adapter_health_changed'")
	if _, err := s.RecordProbe(ctx, res); err != nil {
		t.Fatalf("second: %v", err)
	}
	if got := countRows(t, s, "SELECT count(*) FROM event_journal WHERE event_type='adapter_health_changed'"); got != before {
		t.Errorf("health_changed events went %d -> %d for a probe that saw no change", before, got)
	}
	if _, err := s.MarkUnverified(ctx, "fake", "d", "launch: refused"); err != nil {
		t.Fatalf("MarkUnverified: %v", err)
	}
	if got := countRows(t, s, "SELECT count(*) FROM event_journal WHERE event_type='adapter_health_changed'"); got != before+1 {
		t.Errorf("health_changed events = %d, want %d: a real HEALTHY to UNAVAILABLE transition must be recorded", got, before+1)
	}
	// The failure path may not touch the capability offer at all.
	if got := countRows(t, s, "SELECT count(*) FROM adapter_capabilities"); got != 0 {
		t.Errorf("capability rows = %d after a failed probe, want 0 (§7.2: 不伪造 capability 行)", got)
	}
	// Two failure probes in a row: the second has nothing new to say either.
	if _, err := s.MarkUnverified(ctx, "fake", "d", "launch: refused again"); err != nil {
		t.Fatalf("second MarkUnverified: %v", err)
	}
	if got := countRows(t, s, "SELECT count(*) FROM adapter_instances"); got != 1 {
		t.Errorf("instance rows = %d, want 1", got)
	}
}

// T-263's read door: the dispatch leg resolves adapter_key -> instance id through here and
// nowhere else, so these three cases are the whole contract of that one method.
func probeOnce(t *testing.T, s *SQLiteStore) string {
	t.Helper()
	id, err := s.RecordProbe(context.Background(), ProbeResult{
		AdapterKey: "fake", DisplayName: "Fake Adapter", VerifiedAt: 1789900000000,
		HealthState: HealthHealthy, EffectiveBackend: BackendNativeAPI, ProtocolVersion: "1.0",
	})
	if err != nil {
		t.Fatalf("RecordProbe: %v", err)
	}
	return id
}

func TestResolveInstanceNamesTheRowProbeWrote(t *testing.T) {
	s := openMigrated(t)
	want := probeOnce(t, s)
	got, err := s.ResolveInstance(context.Background(), "fake")
	if err != nil {
		t.Fatalf("ResolveInstance: %v", err)
	}
	if got != want {
		t.Errorf("resolved %q, want the id RecordProbe wrote (%q) - a resolver that returns any other string is a second source of truth", got, want)
	}
}

// The miss has to be a sentinel transport can recognise (adapters.go maps
// db.ErrAggregateNotFound / sql.ErrNoRows to 404), and it has to name the key: "not found"
// without the key is the reading that later turns into "the database is broken".
func TestResolveInstanceSaysNotFoundForAnUnknownKey(t *testing.T) {
	s := openMigrated(t)
	probeOnce(t, s) // a row exists for some other key, so an empty result is really a miss
	_, err := s.ResolveInstance(context.Background(), "ghost")
	if !errors.Is(err, db.ErrAggregateNotFound) {
		t.Fatalf("err = %v, want errors.Is(err, db.ErrAggregateNotFound) so the transport arm stays a 404 and not a 500", err)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("err = %v, want the message to name the key that was not found", err)
	}
	// An empty key is not a special case: it matches no row, so it is the same answer.
	if _, err := s.ResolveInstance(context.Background(), ""); !errors.Is(err, db.ErrAggregateNotFound) {
		t.Errorf("empty key err = %v, want the same not-found sentinel", err)
	}
}

// A resolver that writes would be the quiet version of the T-168 shape: the read that
// "just makes sure the row exists" ends up owning half the write path.
func TestResolveInstanceWritesNothing(t *testing.T) {
	s := openMigrated(t)
	id := probeOnce(t, s)
	instances := countRows(t, s, "SELECT count(*) FROM adapter_instances")
	events := countRows(t, s, "SELECT count(*) FROM event_journal")
	for _, key := range []string{"fake", "ghost"} {
		if _, err := s.ResolveInstance(context.Background(), key); err != nil && key == "fake" {
			t.Fatalf("resolve %s: %v", key, err)
		}
	}
	if got := countRows(t, s, "SELECT count(*) FROM adapter_instances"); got != instances {
		t.Errorf("instance rows went %d -> %d: a read may not create or refresh rows", instances, got)
	}
	if got := countRows(t, s, "SELECT count(*) FROM event_journal"); got != events {
		t.Errorf("journal rows went %d -> %d: a read books no event (the id %q proves the hit path found the row)", events, got, id)
	}
}
