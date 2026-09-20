package quota

import (
	"context"
	"database/sql"
	"fmt"
)

// Queryer 是本包用到的读接口子集（窄接口便于注入）。为什么不直接用 sql.QueryerContext：
// database/sql 不导出查询接口，driver.QueryerContext 是驱动侧 SPI、参数类型不同、*sql.DB 不满足它。
// 完整版见 internal/session/repository.go。
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Repository 只做读：SELECT 本模块自己的 quota_accounts / quota_snapshots。
type Repository struct{ q Queryer }

func NewRepository(q Queryer) *Repository { return &Repository{q: q} }

const accountCols = `id, adapter_instance_id, external_account_ref, display_label,
	quota_source, created_at, updated_at, version`

type rowScanner interface{ Scan(dest ...any) error }

func (r *Repository) Account(ctx context.Context, id string) (Account, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+accountCols+` FROM quota_accounts WHERE id = ?`, id)
	a, err := scanAccount(row)
	if err != nil {
		return Account{}, fmt.Errorf("quota: account %s: %w", id, err)
	}
	return a, nil
}

// AccountsForAdapter 列出某 adapter 实例名下所有额度账户（同一软件多账号是常态）。
func (r *Repository) AccountsForAdapter(ctx context.Context, adapterInstanceID string) ([]Account, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+accountCols+` FROM quota_accounts WHERE adapter_instance_id = ? ORDER BY created_at, id`,
		adapterInstanceID)
	if err != nil {
		return nil, fmt.Errorf("quota: accounts of %s: %w", adapterInstanceID, err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("quota: accounts of %s scan: %w", adapterInstanceID, err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LatestSnapshots 返回该账户每个 quota_key 的最新一条读数（窗口额度并存：5H/WEEKLY/MONTHLY…）。
// 同一 key 出现 observed_at 完全相同的两条时靠 id 定序，保证一个 key 只出一条。
func (r *Repository) LatestSnapshots(ctx context.Context, accountID string) ([]Snapshot, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, quota_account_id, quota_type, quota_key, raw_value_json, used_value, remaining_value,
			percentage_used, percentage_remaining, window_start, window_end, reset_at,
			observation_kind, source, confidence, observed_at
		 FROM quota_snapshots s
		 WHERE s.quota_account_id = ? AND NOT EXISTS (
			SELECT 1 FROM quota_snapshots n
			WHERE n.quota_account_id = s.quota_account_id AND n.quota_key = s.quota_key
			  AND (n.observed_at > s.observed_at
			       OR (n.observed_at = s.observed_at AND n.id > s.id)))
		 ORDER BY s.quota_key`, accountID)
	if err != nil {
		return nil, fmt.Errorf("quota: snapshots of %s: %w", accountID, err)
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var (
			s                               Snapshot
			source                          sql.NullString
			used, remaining                 sql.NullFloat64
			pctUsed, pctRemaining           sql.NullFloat64
			windowStart, windowEnd, resetAt sql.NullInt64
		)
		if err := rows.Scan(&s.ID, &s.QuotaAccountID, &s.Type, &s.Key, &s.RawValueJSON,
			&used, &remaining, &pctUsed, &pctRemaining,
			&windowStart, &windowEnd, &resetAt,
			&s.ObservationKind, &source, &s.Confidence, &s.ObservedAt); err != nil {
			return nil, fmt.Errorf("quota: snapshots of %s scan: %w", accountID, err)
		}
		s.Source = source.String
		s.UsedValue, s.RemainingValue = optFloat(used), optFloat(remaining)
		s.PercentageUsed, s.PercentageRemaining = optFloat(pctUsed), optFloat(pctRemaining)
		s.WindowStart, s.WindowEnd, s.ResetAt = optInt(windowStart), optInt(windowEnd), optInt(resetAt)
		out = append(out, s)
	}
	return out, rows.Err()
}

func optFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func optInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func scanAccount(row rowScanner) (Account, error) {
	var (
		a                       Account
		external, label, source sql.NullString
	)
	if err := row.Scan(&a.ID, &a.AdapterInstanceID, &external, &label, &source,
		&a.CreatedAt, &a.UpdatedAt, &a.Version); err != nil {
		return Account{}, err
	}
	a.ExternalAccountRef, a.DisplayLabel, a.QuotaSource = external.String, label.String, source.String
	return a, nil
}
