package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/eventjournal"
)

// ErrAggregateNotFound is returned when Touch's WHERE id = ? matched no row.
// Touch never creates the row (契约 §1.1.6) — that is Create's job.
var ErrAggregateNotFound = errors.New("aggregate row not found")

// AggregateKind is the event aggregate_type and, through aggTable, the table
// that owns the row's version counter (ADR-017).
type AggregateKind string

const (
	AggProject      AggregateKind = "project"
	AggAdapter      AggregateKind = "adapter"
	AggModel        AggregateKind = "model"
	AggSession      AggregateKind = "session"
	AggTask         AggregateKind = "task"
	AggExecution    AggregateKind = "execution"
	AggApproval     AggregateKind = "approval"
	AggQuota        AggregateKind = "quota"
	AggNotification AggregateKind = "notification"
	AggHandoff      AggregateKind = "handoff"
	// R-002 重开（T-12）：这三张表可变但没有进 ADR-017 的聚合表清单，
	// 000003 才补上 version，否则它们的 UPDATE 在平台层无处表达。
	AggGrant             AggregateKind = "grant"
	AggPolicy            AggregateKind = "policy"
	AggAdapterCapability AggregateKind = "adapter_capability"
)

var aggTable = map[AggregateKind]string{
	AggProject: "projects", AggAdapter: "adapter_instances", AggModel: "models",
	AggSession: "sessions", AggTask: "tasks", AggExecution: "executions",
	AggApproval: "approvals", AggQuota: "quota_accounts",
	AggNotification: "notifications", AggHandoff: "handoffs",
	AggGrant: "temporary_grants", AggPolicy: "policy_rules",
	AggAdapterCapability: "adapter_capabilities",
}

// aggTables is the inverse: Append must not be able to write an aggregate row
// without taking a version.
var aggTables = func() map[string]bool {
	m := make(map[string]bool, len(aggTable))
	for _, t := range aggTable {
		m[t] = true
	}
	return m
}()

var (
	// setPair accepts only "column = ?" so no caller-supplied text reaches SQL.
	setPair      = regexp.MustCompile(`^([a-z_][a-z0-9_]*)[ \t]*=[ \t]*\?$`)
	identifierRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
)

// platformOwned columns belong to the platform, never to a caller's SET/INSERT
// list. id is absent because Create must name it; Touch rejects it separately.
var platformOwned = map[string]bool{"version": true, "seq": true}

// EventDraft is the pre-commit event row (契约 §1).
type EventDraft struct {
	EventID       string
	Type          string
	Aggregate     AggregateKind
	AggregateID   string
	CorrelationID string
	CausationID   string
	PrincipalID   string
	Payload       map[string]any
}

// Tx is one open BEGIN IMMEDIATE transaction on the single write connection.
// It cannot publish anything: publishing happens after Write returns (ADR-008).
type Tx struct {
	ctx  context.Context
	conn *sql.Conn
	db   *DB
	hits *hits
}

// Write runs fn inside BEGIN IMMEDIATE on the one-connection write pool, which
// is what serializes writers.
func (d *DB) Write(ctx context.Context, fn func(tx *Tx) error) error {
	conn, err := d.write.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire write channel: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin immediate: %w", err)
	}
	tx := &Tx{ctx: ctx, conn: conn, db: d, hits: newHits()}
	done := false
	defer func() {
		if !done {
			// A cancelled ctx must not prevent the rollback, or the single writer
			// stays parked inside an open transaction.
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	done = true
	return nil
}

// RedactionHits reports what was scrubbed in this transaction: rule names and
// counts only. Recording them is audit's call, not this package's.
func (tx *Tx) RedactionHits() []RedactionHit { return tx.hits.all() }

// QueryRowContext and QueryContext pass reads through to the transaction's own
// connection (契约 §1.1.11). "read version → compare → Touch" has to happen in one
// transaction; reading the version outside Write leaves a race where another
// transaction has already advanced it.
//
// Deliberately read-only: no ExecContext here, or a caller could write an
// aggregate row without taking a version. Which tables a caller may SELECT stays
// the caller's responsibility (domain model "Repository Boundary").
func (tx *Tx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return tx.conn.QueryRowContext(ctx, q, args...)
}

func (tx *Tx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return tx.conn.QueryContext(ctx, q, args...)
}

// Touch updates the aggregate, takes its next version, and appends the event in
// one transaction. setSQL is a comma-separated list of "column = ?" pairs whose
// placeholders line up with args.
func (tx *Tx) Touch(a AggregateKind, id, setSQL string, args []any, ev EventDraft) (int64, error) {
	table, err := tx.db.aggregateTable(a)
	if err != nil {
		return 0, err
	}
	if id == "" {
		return 0, fmt.Errorf("touch %s: empty aggregate id", a)
	}
	cols, err := parseSet(setSQL)
	if err != nil {
		return 0, err
	}
	if len(cols) != len(args) {
		return 0, fmt.Errorf("touch %s: %d columns but %d args", a, len(cols), len(args))
	}
	for _, c := range cols {
		if c == "id" {
			return 0, errors.New("touch: id is not settable")
		}
		if err := tx.db.checkColumn(table, c); err != nil {
			return 0, err
		}
	}
	scrubbed, err := tx.scrubArgs(table, cols, args)
	if err != nil {
		return 0, err
	}

	set := make([]string, 0, len(cols))
	for _, c := range cols {
		set = append(set, c+" = ?")
	}
	// Taking the version and changing state is one statement, so there is no
	// window for two callers to be handed the same version (ADR-017).
	q := "UPDATE " + table + " SET " + strings.Join(set, ", ") +
		", version = version + 1 WHERE id = ? RETURNING version"

	var version int64
	args2 := append(append([]any{}, scrubbed...), id)
	if err := tx.conn.QueryRowContext(tx.ctx, q, args2...).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("touch %s %s: %w", a, id, ErrAggregateNotFound)
		}
		return 0, fmt.Errorf("touch %s %s: %w", a, id, err)
	}
	if err := tx.appendEvent(ev, a, id, version); err != nil {
		return 0, err
	}
	return version, nil
}

// Create inserts the first row of an aggregate at version 1 and appends its
// first event in the same transaction (契约 §1.1.6). The caller pre-generates id
// as a UUIDv7 so an idempotent retry reuses the same one.
func (tx *Tx) Create(a AggregateKind, id string, columns []string, values []any, ev EventDraft) error {
	table, err := tx.db.aggregateTable(a)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("create %s: empty id", a)
	}
	if len(columns) != len(values) {
		return fmt.Errorf("create %s: %d columns but %d values", a, len(columns), len(values))
	}
	if err := tx.db.checkInsert(a, table, columns); err != nil {
		return err
	}
	if ev.AggregateID != "" && ev.AggregateID != id {
		return fmt.Errorf("create %s: id %q but the event is about %q", a, id, ev.AggregateID)
	}
	scrubbed, err := tx.scrubArgs(table, columns, values)
	if err != nil {
		return err
	}
	// version is written out rather than left to ADR-017's DEFAULT 1, so the row
	// and the event's aggregate_version cannot disagree.
	cols := append(append([]string{}, columns...), "id", "version")
	args := append(append([]any{}, scrubbed...), id, 1)
	q := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" +
		placeholders(len(cols)) + ")"
	if _, err := tx.conn.ExecContext(tx.ctx, q, args...); err != nil {
		return fmt.Errorf("create %s %s: %w", a, id, err)
	}
	return tx.appendEvent(ev, a, id, 1)
}

// Append inserts an append-only row: no version, no event. Aggregate tables are
// refused so their writes cannot skip Touch; schema_migrations belongs to the
// migrator and event_journal to appendEvent.
func (tx *Tx) Append(table string, columns []string, values []any) (int64, error) {
	if !isIdentifier(table) {
		return 0, fmt.Errorf("table %q is not an identifier", table)
	}
	if aggTables[table] {
		return 0, fmt.Errorf("append to %s: aggregate rows must go through Touch or Create", table)
	}
	switch table {
	case "schema_migrations", "event_journal":
		return 0, fmt.Errorf("append to %s is not allowed", table)
	}
	if len(columns) == 0 || len(columns) != len(values) {
		return 0, fmt.Errorf("append %s: %d columns and %d values", table, len(columns), len(values))
	}
	for _, c := range columns {
		if err := tx.db.checkColumn(table, c); err != nil {
			return 0, err
		}
	}
	scrubbed, err := tx.scrubArgs(table, columns, values)
	if err != nil {
		return 0, err
	}
	q := "INSERT INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" +
		placeholders(len(columns)) + ")"
	res, err := tx.conn.ExecContext(tx.ctx, q, scrubbed...)
	if err != nil {
		return 0, fmt.Errorf("append to %s: %w", table, err)
	}
	return res.LastInsertId()
}

const insertEventSQL = `INSERT INTO event_journal
	(event_id, event_type, aggregate_type, aggregate_id, aggregate_version,
	 correlation_id, causation_id, principal_id, payload_json, created_at)
	VALUES (?,?,?,?,?,?,?,?,?,?)`

func (tx *Tx) appendEvent(ev EventDraft, a AggregateKind, id string, version int64) error {
	if !eventjournal.Registered(ev.Type) {
		return fmt.Errorf("event type %q is not in the event journal catalogue", ev.Type)
	}
	if ev.EventID == "" {
		return fmt.Errorf("event %s: empty event_id", ev.Type)
	}
	if ev.Aggregate != "" && ev.Aggregate != a {
		return fmt.Errorf("event %s: aggregate_type %q but touching %q", ev.Type, ev.Aggregate, a)
	}
	payload := ev.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["schema"]; !ok {
		payload["schema"] = 1
	}
	raw, err := redactValue("payload_json", payload, tx.hits)
	if err != nil {
		return fmt.Errorf("event %s: %w", ev.Type, err)
	}
	_, err = tx.conn.ExecContext(tx.ctx, insertEventSQL,
		ev.EventID, ev.Type, string(a), id, version,
		nullable(ev.CorrelationID), nullable(ev.CausationID), nullable(ev.PrincipalID),
		raw, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("append event %s for %s v%d: %w", ev.Type, id, version, err)
	}
	return nil
}

func (tx *Tx) scrubArgs(table string, cols []string, args []any) ([]any, error) {
	out := make([]any, len(args))
	for i, v := range args {
		s, err := redactValue(table+"."+cols[i], v, tx.hits)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func (d *DB) aggregateTable(a AggregateKind) (string, error) {
	t, ok := aggTable[a]
	if !ok {
		return "", fmt.Errorf("unknown aggregate %q", a)
	}
	return t, nil
}

// checkInsert validates a Create column list. id and version are excluded here
// because Create supplies both itself.
func (d *DB) checkInsert(a AggregateKind, table string, columns []string) error {
	seen := map[string]bool{}
	for _, c := range columns {
		if seen[c] {
			return fmt.Errorf("create %s: column %s listed twice", a, c)
		}
		seen[c] = true
		if c == "id" {
			return fmt.Errorf("create %s: id is a Create argument, not a column", a)
		}
		if err := d.checkColumn(table, c); err != nil {
			return err
		}
	}
	return nil
}

// checkColumn validates an identifier against the live table, so the whitelist
// cannot drift from a later migration.
func (d *DB) checkColumn(table, col string) error {
	if !isIdentifier(col) {
		return fmt.Errorf("column %q is not an identifier", col)
	}
	if platformOwned[col] {
		return fmt.Errorf("column %s belongs to the platform, not to a caller", col)
	}
	cols, err := d.columns(table)
	if err != nil {
		return err
	}
	if !cols[col] {
		return fmt.Errorf("no such column %s.%s", table, col)
	}
	return nil
}

// columns reads a table's column set from SQLite itself and caches it, so the
// write whitelist follows the schema instead of duplicating it in Go.
func (d *DB) columns(table string) (map[string]bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.cols[table]; ok {
		return c, nil
	}
	rows, err := d.Read.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no such table %s", table)
	}
	d.cols[table] = out
	return out, nil
}

// parseSet splits a "a = ?, b = ?" list into validated column names.
func parseSet(setSQL string) ([]string, error) {
	trimmed := strings.TrimSpace(setSQL)
	if trimmed == "" {
		return nil, errors.New("empty SET list")
	}
	items := strings.Split(trimmed, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		m := setPair.FindStringSubmatch(strings.TrimSpace(item))
		if m == nil {
			return nil, fmt.Errorf("set item %q is not \"column = ?\"", item)
		}
		out = append(out, m[1])
	}
	return out, nil
}

func isIdentifier(s string) bool { return identifierRe.MatchString(s) }

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
