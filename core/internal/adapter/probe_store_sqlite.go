package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/eventjournal"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// probe_store_sqlite.go is the SQLite store for §7.2's registration domain, in the shape
// internal contract §3 rule 1 allows: the table names live in the owning domain package
// and nowhere else, and every write goes through platform/db so version-taking, the
// mandatory redaction and the event-journal row cannot be skipped.
//
// This file is the only one in the package allowed to import a Core-internal package
// other than the standard library - see TestOnlyStdlibImports, which was scoped to keep
// the protocol files DB-free instead of being deleted when §7.2 added the domain half.
// os/exec stays denied everywhere: starting a process is internal/process's job, and
// probe reaches it through the injected Launcher rather than by importing it (§7.2's
// REQ-T12 note).

// SQLiteStore records what a probe learned.
type SQLiteStore struct {
	d   *db.DB
	now func() int64
	// newID mints row ids. It is a field so a test can pin "the instance row and its
	// capability rows are written under the id the store chose", not whatever a random
	// generator produced.
	newID func() string
}

var _ ProbeStore = (*SQLiteStore)(nil)

// NewSQLiteStore binds the store to an opened, migrated DB. A nil clock is a bug in the
// caller, so it is refused rather than defaulted to wall time: probe rows are compared
// against each other (health "from → to"), and an uninjectable clock makes that
// comparison untestable.
func NewSQLiteStore(d *db.DB, now func() int64) (*SQLiteStore, error) {
	if d == nil {
		return nil, &Error{Code: ErrInvalidParams, Message: "adapter store needs an opened db.DB"}
	}
	if now == nil {
		return nil, &Error{Code: ErrInvalidParams, Message: "adapter store requires a clock"}
	}
	return &SQLiteStore{d: d, now: now, newID: func() string { return NewRequestID().String() }}, nil
}

// instanceRow is the slice of adapter_instances this store reads.
type instanceRow struct {
	id     string
	health string
}

// lookupInstance finds the row for an adapter_key. QueryContext rather than
// QueryRowContext + sql.ErrNoRows keeps database/sql out of this package entirely: the
// store's only DB vocabulary is what platform/db exposes.
func lookupInstance(ctx context.Context, tx *db.Tx, adapterKey string) (instanceRow, bool, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT id, health FROM adapter_instances WHERE adapter_key = ?", adapterKey)
	if err != nil {
		return instanceRow{}, false, fmt.Errorf("look up adapter %s: %w", adapterKey, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return instanceRow{}, false, rows.Err()
	}
	var got instanceRow
	if err := rows.Scan(&got.id, &got.health); err != nil {
		return instanceRow{}, false, fmt.Errorf("scan adapter %s: %w", adapterKey, err)
	}
	return got, true, nil
}

// ResolveInstance is the read door for adapter_key -> adapter_instances.id (T-263).
// The dispatch leg needs that mapping and this store is the only place allowed to read
// it: returning the id alone (not a row struct) keeps the ownership question out of
// cmd/core, which per §7 must not carry business decisions - not even which column to pick.
//
// A key with no row reports db.ErrAggregateNotFound, wrapped so the message still names
// the key: errors.Is stays true, which is what transport's 404 arm matches on
// (api/http/adapters.go). It reads over d.Read like every other read in the tree
// (approval/permission do the same) - taking the serialized write channel for a SELECT
// would make a lookup wait behind unrelated writes.
func (s *SQLiteStore) ResolveInstance(ctx context.Context, adapterKey string) (string, error) {
	rows, err := s.d.Read.QueryContext(ctx,
		"SELECT id FROM adapter_instances WHERE adapter_key = ?", adapterKey)
	if err != nil {
		return "", fmt.Errorf("resolve adapter %s: %w", adapterKey, err)
	}
	defer rows.Close()
	if !rows.Next() {
		// rows.Err() first, and only then "not found": a driver error that surfaces on the
		// first Next() must not be answered with a 404. This branch has no mutant - a normal
		// single-row SELECT over real SQLite cannot reach it in-process - so it is kept
		// because getting the attribution wrong is worse than carrying three lines.
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("resolve adapter %s: %w", adapterKey, err)
		}
		return "", fmt.Errorf("resolve adapter %s: %w", adapterKey, db.ErrAggregateNotFound)
	}
	var id string
	if err := rows.Scan(&id); err != nil {
		return "", fmt.Errorf("scan adapter %s: %w", adapterKey, err)
	}
	// No trailing rows.Err() here: adapter_key is UNIQUE and the row is already in hand, so
	// that check is one nobody can make fail.
	return id, nil
}

// RecordProbe writes the instance row and one row per verified capability in a single
// transaction. Atomicity is the point: an Adapter that landed its health but not its
// capabilities would be advertised as HEALTHY with an empty offer, and §3's effective
// capability computation would read that as "this product can do nothing".
func (s *SQLiteStore) RecordProbe(ctx context.Context, res ProbeResult) (string, error) {
	at := s.now()
	written := ""
	err := s.d.Write(ctx, func(tx *db.Tx) error {
		existing, found, err := lookupInstance(ctx, tx, res.AdapterKey)
		if err != nil {
			return err
		}
		id := existing.id
		if !found {
			id = s.newID()
			written = id
			if err := tx.Create(db.AggAdapter, id, []string{
				"adapter_key", "display_name", "adapter_version", "protocol_version", "software_version",
				"enabled", "health", "effective_backend", "last_healthcheck_at",
				"metadata_json", "created_at", "updated_at",
			}, []any{
				res.AdapterKey, res.DisplayName, res.AdapterVersion, res.ProtocolVersion, res.SoftwareVersion,
				true, res.HealthState, nullStr(res.EffectiveBackend), res.VerifiedAt,
				`{"schema":1}`, at, at,
			}, db.EventDraft{
				EventID: s.newID(), Type: eventjournal.AdapterRegistered, Aggregate: db.AggAdapter,
				AggregateID: id,
				Payload:     map[string]any{"adapter_key": res.AdapterKey, "protocol_version": res.ProtocolVersion},
			}); err != nil {
				return fmt.Errorf("register adapter %s: %w", res.AdapterKey, err)
			}
			written = id
		} else if existing.health != res.HealthState {
			// Only a real change earns the event. Touching the row on every probe with the
			// same health would fill the journal with "changed from HEALTHY to HEALTHY",
			// which is the noise §10's "别把没变化写成变化" rule is about.
			if _, err := tx.Touch(db.AggAdapter, id,
				"health = ?, adapter_version = ?, protocol_version = ?, software_version = ?, effective_backend = ?, last_healthcheck_at = ?, updated_at = ?",
				[]any{res.HealthState, res.AdapterVersion, res.ProtocolVersion, res.SoftwareVersion,
					nullStr(res.EffectiveBackend), res.VerifiedAt, at},
				db.EventDraft{
					EventID: s.newID(), Type: eventjournal.AdapterHealthChanged, Aggregate: db.AggAdapter,
					AggregateID: id,
					Payload:     map[string]any{"from": existing.health, "to": res.HealthState, "confidence": "HIGH"},
				}); err != nil {
				return fmt.Errorf("update adapter %s: %w", res.AdapterKey, err)
			}
		} else {
			// Health unchanged. There is deliberately no "just refresh
			// last_healthcheck_at" write here: platform/db's Touch requires an event with
			// every aggregate write, and adapter_health_changed from==to would put a
			// change in the journal that did not happen. CO-42 asks 主控 whether v0.1
			// wants a no-event refresh path instead of a stale check time.
			written = id
		}
		for _, c := range res.Capabilities {
			if err := s.recordCapability(ctx, tx, id, c, at); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return written, nil
}

// recordCapability upserts one capability row and verifies it with the one registered
// event for that fact (event contract §6's adapter_capability_verified).
func (s *SQLiteStore) recordCapability(ctx context.Context, tx *db.Tx, instanceID string, c Capability, at int64) error {
	rows, err := tx.QueryContext(ctx,
		"SELECT id FROM adapter_capabilities WHERE adapter_instance_id = ? AND capability_name = ?",
		instanceID, c.Name)
	if err != nil {
		return fmt.Errorf("look up capability %s: %w", c.Name, err)
	}
	var existing string
	if rows.Next() {
		if err := rows.Scan(&existing); err != nil {
			rows.Close()
			return fmt.Errorf("scan capability %s: %w", c.Name, err)
		}
	}
	rows.Close()

	payload := map[string]any{"capability_name": c.Name, "support_level": c.SupportLevel}
	if existing != "" {
		if _, err := tx.Touch(db.AggAdapterCapability, existing,
			"support_level = ?, effective_backend = ?, health = ?, last_verified_at = ?, updated_at = ?",
			[]any{c.SupportLevel, nullStr(c.EffectiveBackend), healthOf(c), c.LastVerifiedAt, at},
			db.EventDraft{EventID: s.newID(), Type: eventjournal.AdapterCapabilityVerified,
				Aggregate: db.AggAdapterCapability, AggregateID: existing, Payload: payload}); err != nil {
			return fmt.Errorf("verify capability %s: %w", c.Name, err)
		}
		return nil
	}
	id := s.newID()
	if err := tx.Create(db.AggAdapterCapability, id, []string{
		"adapter_instance_id", "capability_name", "support_level", "effective_backend",
		"health", "last_verified_at", "metadata_json", "created_at", "updated_at",
	}, []any{
		instanceID, c.Name, c.SupportLevel, nullStr(c.EffectiveBackend),
		healthOf(c), c.LastVerifiedAt, `{"schema":1}`, at, at,
	}, db.EventDraft{EventID: s.newID(), Type: eventjournal.AdapterCapabilityVerified,
		Aggregate: db.AggAdapterCapability, AggregateID: id, Payload: payload}); err != nil {
		return fmt.Errorf("record capability %s: %w", c.Name, err)
	}
	return nil
}

// MarkUnverified is the failure path §7.2 allows: a health row and nothing else. No
// capability rows are written, because a probe that could not talk to the Adapter has not
// learned anything about its offer, and inventing UNSUPPORTED rows would be the
// "看起来一切正常" failure in the other direction.
func (s *SQLiteStore) MarkUnverified(ctx context.Context, adapterKey, displayName, reason string) (string, error) {
	at := s.now()
	written := ""
	err := s.d.Write(ctx, func(tx *db.Tx) error {
		existing, found, err := lookupInstance(ctx, tx, adapterKey)
		if err != nil {
			return err
		}
		if !found {
			id := s.newID()
			written = id
			return tx.Create(db.AggAdapter, id, []string{
				"adapter_key", "display_name", "enabled", "health",
				"last_healthcheck_at", "metadata_json", "created_at", "updated_at",
			}, []any{adapterKey, displayName, true, HealthUnavailable, at,
				reasonJSON(reason), at, at}, db.EventDraft{
				EventID: s.newID(), Type: eventjournal.AdapterRegistered, Aggregate: db.AggAdapter,
				AggregateID: id,
				Payload:     map[string]any{"adapter_key": adapterKey, "protocol_version": ""},
			})
		}
		written = existing.id
		if existing.health == HealthUnavailable {
			// Already UNAVAILABLE: same reason as above, no write and no event. The row
			// says what it said; a second "changed to UNAVAILABLE" would be a fabricated
			// transition.
			return nil
		}
		return s.healthWrite(ctx, tx, existing, adapterKey, HealthUnavailable, at, reason)
	})
	if err != nil {
		return "", fmt.Errorf("record failed probe of %s: %w", adapterKey, err)
	}
	return written, nil
}

// DowngradeHealth is the pump's door for §7.2's health column (T-402 ①): lower one Adapter
// by `adapter_key`, writing `health`, `last_healthcheck_at` and `updated_at` in the same
// statement. It reports whether a row actually moved, so a caller that fires on every
// reconnect attempt learns "already down here" without reading the table back.
//
// Why it sits beside MarkUnverified instead of being the same call: MarkUnverified answers
// "a probe could not reach this Adapter", and a probe *is* the registration act, so it may
// create the row and it carries the failure reason. The pump answers "the process we were
// talking to died" - it must never register an Adapter it only lost, and its honest value can
// be DEGRADED rather than UNAVAILABLE. Both go through `healthWrite`, so the trio is
// assembled in exactly one place and this package does not grow a second writer of one fact.
//
// There is no reason parameter on purpose: the event contract gives `adapter_health_changed`
// three payload keys - `from` `to` `confidence` (specs/events/event-contract-v0.1.md:185) -
// and a fourth would be a contract change smuggled in through a signature.
func (s *SQLiteStore) DowngradeHealth(ctx context.Context, adapterKey, to string) (bool, error) {
	if !contains(healthValues, to) {
		return false, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("adapter %s: health %q is not one of %v", adapterKey, to, healthValues)}
	}
	at := s.now()
	changed := false
	err := s.d.Write(ctx, func(tx *db.Tx) error {
		existing, found, err := lookupInstance(ctx, tx, adapterKey)
		if err != nil {
			return err
		}
		if !found {
			// Wrapped, not invented: the caller matches on errors.Is(err, db.ErrAggregateNotFound),
			// and the sentence names why a pump may not register what it only lost.
			return fmt.Errorf("downgrade adapter %s to %s: %w (no instance row; registration belongs to a probe)",
				adapterKey, to, db.ErrAggregateNotFound)
		}
		if existing.health == to {
			return nil // already there: no write, no journal row for a change that did not happen
		}
		if !isDowngrade(existing.health, to) {
			return &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("adapter %s is %s: %s is not a downgrade - health going back up is probe evidence, not the absence of a death signal",
					adapterKey, existing.health, to)}
		}
		if err := s.healthWrite(ctx, tx, existing, adapterKey, to, at, ""); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("downgrade adapter %s: %w", adapterKey, err)
	}
	return changed, nil
}

// healthDowngrades is the closed set this door takes. Note the column's CHECK list is wider
// than this table: 000001:93-96 also allows STARTING and STOPPED, which nothing in this tree
// writes (the protocol enum in healthValues has four values, and handshake.go:185 refuses any
// other). A row that somehow holds one of those is refused here rather than guessed about.
//
// The pairs it does take: UNKNOWN -> UNAVAILABLE is in because
// a process we were talking to dying is evidence, while UNKNOWN -> DEGRADED is not: nothing
// observed says part of the Adapter still works. Anything raising health is refused - that is
// what a successful probe is for (RecordProbe) - and ->UNKNOWN is refused because §8's
// "UNKNOWN 不得折成 UNAVAILABLE" cuts both ways: UNKNOWN is the absence of evidence, not a
// status a caller may choose.
var healthDowngrades = map[string][]string{
	HealthHealthy:     {HealthDegraded, HealthUnavailable},
	HealthDegraded:    {HealthUnavailable},
	HealthUnknown:     {HealthUnavailable},
	HealthUnavailable: nil,
}

func isDowngrade(from, to string) bool {
	for _, allow := range healthDowngrades[from] {
		if allow == to {
			return true
		}
	}
	return false
}

// healthWrite is the one place that puts a health transition on the table. The trio is the
// point (T-402 ① "同笔把 updated_at／last_healthcheck_at 一起走，别只改一列"): a row that says
// UNAVAILABLE with yesterday's check time is unreadable, and a row whose updated_at lags the
// health cannot be told apart from a stuck writer. `reason` is non-empty only on the probe
// path, where §7.2 allows the reason into metadata_json (the table has no reason column and
// 000001 is frozen). The event is mandatory with every aggregate write in this tree, and
// `from`/`to`/`confidence` are exactly the payload keys §6 lists.
func (s *SQLiteStore) healthWrite(ctx context.Context, tx *db.Tx, existing instanceRow, adapterKey, to string, at int64, reason string) error {
	set := "health = ?, last_healthcheck_at = ?, updated_at = ?"
	args := []any{to, at, at}
	if reason != "" {
		set = "health = ?, last_healthcheck_at = ?, metadata_json = ?, updated_at = ?"
		args = []any{to, at, reasonJSON(reason), at}
	}
	_, err := tx.Touch(db.AggAdapter, existing.id, set, args, db.EventDraft{
		EventID: s.newID(), Type: eventjournal.AdapterHealthChanged, Aggregate: db.AggAdapter,
		AggregateID: existing.id,
		Payload:     map[string]any{"from": existing.health, "to": to, "confidence": "HIGH"},
	})
	return err
}

// healthOf maps §3's capability row to its own health column. A capability's health is
// derived from its level rather than reported separately, because §2's InitializeResult
// carries no per-capability health field and inventing one would be a second source.
func healthOf(c Capability) string {
	if c.SupportLevel == LevelUnsupported {
		return HealthUnavailable
	}
	return HealthHealthy
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// reasonJSON keeps the failure reason inside metadata_json: adapter_instances has no
// reason column, 000001 is frozen, and dropping the reason would leave a row that says
// UNAVAILABLE with nothing to explain it.
func reasonJSON(reason string) string {
	return `{"schema":1,"last_probe_reason":` + quote(reason) + `}`
}

func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
