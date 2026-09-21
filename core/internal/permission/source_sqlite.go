package permission

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// SettingEmergencyStop is the GLOBAL settings key the first rung of the chain
// reads; value_json is {"enabled":true}.
//
// Read-only here, and deliberately so (T-65① / REQ-DB-2): settings has no
// legitimate write path today - Tx.UpdateOnce's narrowTables holds only
// idempotency_keys - and a write entry point with no consumer is the same
// speculative feature T-28③ refused for policy_rules. When the panel grows an
// emergency-stop button, that whitelist extension goes through a REQ to the
// orchestrator. Verified repo-wide, all file types (not one file, not one dir):
// the only INSERT INTO settings anywhere is this package's own test fixture, and
// no migration seeds the row - so a fresh database always reads "not stopped".
//
// A missing row means NOT stopped. "Cannot read the switch" is not "stop": the
// asymmetry is intentional, because failing this direction on an absent row
// would halt every Execution for a setting nobody ever set. A value that exists
// but cannot be parsed is the opposite case - it is returned as an error, which
// Engine turns into DENY, so a corrupt stop flag cannot read as "clear to
// proceed".
const SettingEmergencyStop = "emergency_stop"

// SQLiteSource reads policy and grants through the platform's read pool.
// It never writes: policy_rules and temporary_grants belong to the write paths
// in internal/approval and whoever seeds policy_rules.
type SQLiteSource struct{ d *db.DB }

func NewSQLiteSource(d *db.DB) *SQLiteSource { return &SQLiteSource{d: d} }

func (s *SQLiteSource) EmergencyStopped(ctx context.Context) (bool, error) {
	var raw string
	err := s.d.Read.QueryRowContext(ctx,
		`SELECT value_json FROM settings
		 WHERE scope_type = 'GLOBAL' AND scope_id IS NULL AND setting_key = ?`,
		SettingEmergencyStop).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("permission: read %s: %w", SettingEmergencyStop, err)
	}
	var v struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return false, fmt.Errorf("permission: settings.%s value_json is not {\"enabled\":bool}: %w", SettingEmergencyStop, err)
	}
	return v.Enabled, nil
}

// Rules returns every enabled policy rule.
//
// ponytail: no scope filtering in SQL — the Engine filters by scope, id and
// pattern anyway, and this table is single-operator sized. If it ever grows,
// push the relevance test into the query and keep the semantics identical.
func (s *SQLiteSource) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := s.d.Read.QueryContext(ctx,
		`SELECT id, scope_type, COALESCE(scope_id, ''), action_pattern, COALESCE(resource_pattern, ''),
			effect, priority FROM policy_rules WHERE enabled = 1`)
	if err != nil {
		return nil, fmt.Errorf("permission: read policy_rules: %w", err)
	}
	defer rows.Close()

	var out []Rule
	for rows.Next() {
		var (
			ru  Rule
			eff string
			sc  string
		)
		if err := rows.Scan(&ru.ID, &sc, &ru.ScopeID, &ru.ActionPattern, &ru.ResourcePattern, &eff, &ru.Priority); err != nil {
			return nil, err
		}
		// scope_type and effect arrive as plain strings because both columns are
		// CHECKed by 000001; re-validating them here would be unreachable code.
		ru.ScopeType, ru.Effect = Scope(sc), Effect(eff)
		out = append(out, ru)
	}
	return out, rows.Err()
}

// Grants returns the principal's grant rows, live and spent alike. Expiry and
// revocation are decided by Grant.Active on every read, so a missed sweep
// cannot leave a privilege usable; filtering here would only duplicate that.
func (s *SQLiteSource) Grants(ctx context.Context, principalID string) ([]Grant, error) {
	rows, err := s.d.Read.QueryContext(ctx,
		`SELECT id, principal_id, scope_type, COALESCE(scope_id, ''), action,
			COALESCE(resource_pattern, ''), effect, starts_at, COALESCE(expires_at, 0),
			COALESCE(revoked_at, 0)
		 FROM temporary_grants WHERE principal_id = ?`, principalID)
	if err != nil {
		return nil, fmt.Errorf("permission: read temporary_grants: %w", err)
	}
	defer rows.Close()

	var out []Grant
	for rows.Next() {
		var (
			g       Grant
			sc, eff string
		)
		if err := rows.Scan(&g.ID, &g.PrincipalID, &sc, &g.ScopeID, &g.Action,
			&g.ResourcePattern, &eff, &g.StartsAt, &g.ExpiresAt, &g.RevokedAt); err != nil {
			return nil, err
		}
		g.ScopeType, g.Effect = Scope(sc), Effect(eff)
		out = append(out, g)
	}
	return out, rows.Err()
}
