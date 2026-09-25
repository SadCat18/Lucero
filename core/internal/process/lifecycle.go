package process

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/eventjournal"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// ErrNoLifecycleRow is an edge that has no row to move: the Supervisor observed an exit
// but this store never landed a start for that attempt. It is not a write failure and it
// is not a no-op either - it means "库里没有这个 pid 的行", which per CO-44 has no subject
// an aggregate event could belong to, so the caller's exit is audit (the RecordDrop family),
// never a fabricated process_exited row.
var ErrNoLifecycleRow = errors.New("no process_instances row for this lifecycle edge")

// ProcessRow is the assembly-time half of a process_instances row: the facts a Supervisor
// cannot observe about itself. It is handed in once, next to the Supervisor it belongs to,
// and the write cell stamps them onto every attempt.
type ProcessRow struct {
	Kind       Kind   // -> process_type; only the two kinds this package can supervise
	OwnerType  string // §1.1 第 18 条: v0.1 has exactly one value, "adapter_instance"
	OwnerID    string // -> adapter_instances.id when OwnerType is set
	Executable string // what got launched, as configured
	// RestartPolicy is the policy in force for this Supervisor, not one attempt's outcome.
	RestartPolicy RestartPolicy
}

// LifecycleStore lands one lifecycle observation as one row plus exactly one event, which
// is why it holds a *db.Tx per call rather than a *db.DB: the pairing has to be
// unskippable inside a single transaction (契约① §1.1 第 17 条), and "Append the row, then
// remember the event" is the T-168 shape this entry exists to make impossible.
//
// Ownership is the point of T-211: this semantics lives here, in the package that makes
// the observation. cmd/core only assembles - it hands over the Tx and shouts the error -
// so no table name from another domain appears there (W13).
//
// One Supervisor goroutine may call it (all emits come from Run), and it is deliberately
// unsynchronised: attempt/curID are the sequence of that one goroutine's own observations.
type LifecycleStore struct {
	row   ProcessRow
	newID func() (string, error)
	now   func() int64
	logf  func(string, ...any)

	attempt int
	prevID  string
	curID   string
	// landedStatus is the status this cell wrote when it created curID, and it is never
	// advanced. That is what makes the guard bite on a replay: if it tracked "current
	// status" instead, an exited row would sit in EXITED and a second exit would match its
	// own post-state, moving the row again and booking a ghost edge (measured - the replay
	// only failed at all because event_journal.event_id is UNIQUE).
	//
	// This is 契约① §1.1 第 20 条 ⑤, not a local style choice: one attempt is one row, so a
	// row takes exactly one terminal edge. Anyone who lands a second edge on a row must move
	// the guard to the state *they* last wrote and keep shouting on 0 rows - never retry with
	// the old guard, which is the branch CO-44 ① already rejected. Note the guard can never
	// be a status this package does not emit: nothing carries RUNNING, so guarding on
	// "observed running" would return 0 rows every time and silently read as "already landed".
	landedStatus Status
}

// NewLifecycleStore validates the row facts against the frozen schema rather than letting
// SQLite's CHECK reject them mid-transaction: these are configuration errors, and a
// Supervisor that cannot land a row must be known at assembly time.
func NewLifecycleStore(row ProcessRow, newID func() (string, error), now func() int64,
	logf func(string, ...any)) (*LifecycleStore, error) {
	if row.Kind != KindAdapter && row.Kind != KindWorker {
		return nil, fmt.Errorf("process row: process_type %q is not something this package supervises", row.Kind)
	}
	// §1.1 第 18 条 registers exactly one owner_type for v0.1. An empty owner is legal
	// (the column is nullable); a wrong one is not, so it is refused here instead of
	// registering a second vocabulary nobody produced.
	if row.OwnerType != "" && row.OwnerType != OwnerTypeAdapterInstance {
		return nil, fmt.Errorf("process row: owner_type %q is not registered for v0.1 (only %q or empty)",
			row.OwnerType, OwnerTypeAdapterInstance)
	}
	if row.OwnerType != "" && row.OwnerID == "" {
		return nil, errors.New("process row: owner_type without owner_id - a half-known owner is worse than none")
	}
	switch row.RestartPolicy {
	case RestartNever, RestartOnFailure, RestartAlways:
	default:
		return nil, fmt.Errorf("process row: restart_policy %q is outside 000001's CHECK list", row.RestartPolicy)
	}
	if newID == nil {
		return nil, errors.New("process row: no id source - CO-45 (a) makes a row per attempt, so minting is this cell's job")
	}
	if now == nil {
		return nil, errors.New("process row: no clock source; §14 stores Unix ms")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &LifecycleStore{row: row, newID: newID, now: now, logf: logf}, nil
}

// OwnerTypeAdapterInstance is 契约① §1.1 第 18 条 registered owner vocabulary for v0.1 -
// the only value `process_instances.owner_type` may take today. It is exported because this
// package is the one that writes that column, so the vocabulary has exactly one source;
// 装配层 reads it instead of re-typing the literal (T-248: cmd/core 不得含业务判断，值也一样).
const OwnerTypeAdapterInstance = "adapter_instance"

// ID returns the row this cell last landed, or "" before the first start. The injector
// needs it for logs and for the audit line that carries an unattributable edge.
func (s *LifecycleStore) ID() string { return s.curID }

// RecordProcessStart inserts one row for this attempt and books process_started beside it.
// It returns the id it minted, because CO-45 (a) makes each attempt its own row and the
// caller cannot otherwise know which pid-instantiation the audit trail is talking about.
func (s *LifecycleStore) RecordProcessStart(tx *db.Tx, ev Event) (string, error) {
	if ev.Kind != EventStarted {
		return "", fmt.Errorf("RecordProcessStart got a %s observation", ev.Kind)
	}
	if ev.Status == "" {
		return "", errors.New("RecordProcessStart got no status: the row would be a guess")
	}
	id, err := s.newID()
	if err != nil {
		return "", fmt.Errorf("mint process row id: %w", err)
	}
	at := ev.Timestamp
	if at == 0 {
		at = s.now()
	}
	// created_at/updated_at are the row's own birth and last write (this store's clock);
	// started_at is the observation. They are different facts, so they get different sources:
	// stamping both from the event would make a late-recorded start look like it happened
	// when the row was written.
	stamped := s.now()
	s.attempt++
	meta, err := json.Marshal(map[string]any{
		"attempt": s.attempt, "previous_instance_id": s.prevID, "pid": ev.PID,
	})
	if err != nil {
		s.attempt--
		return "", fmt.Errorf("encode metadata_json: %w", err)
	}
	cols := []string{"process_type", "owner_type", "owner_id", "pid", "executable",
		"status", "restart_policy", "started_at", "metadata_json", "created_at", "updated_at"}
	vals := []any{string(s.row.Kind), s.row.OwnerType, s.row.OwnerID, int64(ev.PID), s.row.Executable,
		string(ev.Status), string(s.row.RestartPolicy), at, string(meta), stamped, stamped}
	err = tx.CreateVersionless(db.AggProcess, id, cols, vals, db.EventDraft{
		EventID:     id + ".started",
		Type:        eventjournal.ProcessStarted,
		Aggregate:   db.AggProcess,
		AggregateID: id,
		Payload: map[string]any{
			"process_type": string(s.row.Kind), "pid": ev.PID, "status": string(ev.Status),
			"attempt": s.attempt,
		},
	})
	if err != nil {
		s.attempt--
		return "", fmt.Errorf("land process_started for pid %d: %w", ev.PID, err)
	}
	s.prevID, s.curID, s.landedStatus = id, id, ev.Status
	return id, nil
}

// RecordProcessExit books a clean exit (§14's EXITED) on the row the last start landed.
func (s *LifecycleStore) RecordProcessExit(tx *db.Tx, ev Event) error {
	return s.takeEdge(tx, ev, EventExited, eventjournal.ProcessExited)
}

// RecordProcessCrash books an abnormal exit. ev.Crashes has no column (000001 is frozen),
// so the running count rides in metadata_json: it is the one number §8's budget needs and
// Recovery cannot recompute after Core itself restarts.
func (s *LifecycleStore) RecordProcessCrash(tx *db.Tx, ev Event) error {
	return s.takeEdge(tx, ev, EventCrashed, eventjournal.ProcessCrashed)
}

// takeEdge moves exactly one status edge and books exactly one event. Two rulings decide
// what it does when nothing moves:
//
//   - CO-44 ①: 0 rows is 0 rows. No second guard attempt, no retry until it "lands", no
//     event. Trying the other from-state is turning one observation into two branches that
//     guess at ordering.
//   - CO-44 ③: 0 rows must not be silent. It is shouted with the row key and the state it
//     was expected in, because otherwise "already landed, idempotent" and "never worked"
//     are indistinguishable afterwards.
//
// The status written is the one the Supervisor observed (ev.Status), never a re-derivation
// of it, and the guard is the value this cell itself wrote last - so the from-state is
// something this cell knows rather than something it hopes for.
func (s *LifecycleStore) takeEdge(tx *db.Tx, ev Event, want EventKind, evType string) error {
	if ev.Kind != want {
		return fmt.Errorf("%s got a %s observation", evType, ev.Kind)
	}
	if ev.Status == "" {
		return fmt.Errorf("%s got no status", evType)
	}
	if s.curID == "" {
		s.logf("%s has no row to move: pid=%d exit=%d crashes=%d - 库里没有这个实例的行，走审计不走事件",
			evType, ev.PID, ev.ExitCode, ev.Crashes)
		return ErrNoLifecycleRow
	}
	at := ev.Timestamp
	if at == 0 {
		at = s.now()
	}
	meta, err := json.Marshal(map[string]any{
		"attempt": s.attempt, "previous_instance_id": s.prevID, "pid": ev.PID,
		"crashes": ev.Crashes, "exit_code": ev.ExitCode,
	})
	if err != nil {
		return fmt.Errorf("encode metadata_json: %w", err)
	}
	payload := map[string]any{
		"process_type": string(s.row.Kind), "pid": ev.PID, "status": string(ev.Status),
		"exit_code": ev.ExitCode, "attempt": s.attempt,
	}
	if evType == eventjournal.ProcessCrashed {
		payload["crashes"] = ev.Crashes
	}
	changed, err := tx.TouchTransition(db.AggProcess, s.curID, "status", string(s.landedStatus),
		"status = ?, exited_at = ?, exit_code = ?, metadata_json = ?, updated_at = ?",
		[]any{string(ev.Status), at, int64(ev.ExitCode), string(meta), s.now()}, db.EventDraft{
			EventID:     s.curID + "." + evType,
			Type:        evType,
			Aggregate:   db.AggProcess,
			AggregateID: s.curID,
			Payload:     payload,
		})
	if err != nil {
		return fmt.Errorf("land %s for %s: %w", evType, s.curID, err)
	}
	if changed == 0 {
		s.logf("%s moved no row for %s (expected status %s, pid %d): 这一格已落过或这行已走别条边，不补事件、不换 guard 再试",
			evType, s.curID, s.landedStatus, ev.PID)
	}
	return nil
}
