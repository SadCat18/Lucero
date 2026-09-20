package execution

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

// Repository 只读：SELECT executions 一张表。写路径唯一入口是 db.Create / db.Touch。
type Repository struct{ q Queryer }

func NewRepository(q Queryer) *Repository { return &Repository{q: q} }

const execCols = `id, task_id, adapter_instance_id, session_id, predecessor_execution_id,
	attempt_no, status, state_confidence, trigger_type, provider_execution_id, version`

func (r *Repository) Get(ctx context.Context, id string) (Execution, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+execCols+` FROM executions WHERE id = ?`, id)
	e, err := scanExecution(row)
	if err != nil {
		return Execution{}, fmt.Errorf("execution: get %s: %w", id, err)
	}
	return e, nil
}

// ListByTask 给 GET /executions?task_id= 用（契约③ §3），按 created_at 出，
// 于是 attempt_no / predecessor_execution_id 的链条在时间线上天然可读。
func (r *Repository) ListByTask(ctx context.Context, taskID string) ([]Execution, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+execCols+` FROM executions WHERE task_id = ? ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("execution: list of task %s: %w", taskID, err)
	}
	defer rows.Close()
	var out []Execution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, fmt.Errorf("execution: list of task %s scan: %w", taskID, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListCrashRecoveryCandidates 就是领域模型「Crash Recovery Query」那条 SELECT 的原文范围：
// Core 重启后要看的就是这五个状态，判定（reconnect / resume / interrupt）归 Recovery Engine，
// 本包只负责把它们读出来。
func (r *Repository) ListCrashRecoveryCandidates(ctx context.Context) ([]Execution, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+execCols+` FROM executions
		 WHERE status IN ('STARTING','RUNNING','WAITING_APPROVAL','WAITING_INPUT','TRANSFERRING')
		 ORDER BY heartbeat_at, id`)
	if err != nil {
		return nil, fmt.Errorf("execution: recovery scan: %w", err)
	}
	defer rows.Close()
	var out []Execution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, fmt.Errorf("execution: recovery scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanExecution(row rowScanner) (Execution, error) {
	var (
		e           Execution
		session     sql.NullString
		predecessor sql.NullString
		provider    sql.NullString
	)
	if err := row.Scan(&e.ID, &e.TaskID, &e.AdapterInstanceID, &session, &predecessor,
		&e.AttemptNo, &e.Status, &e.StateConfidence, &e.TriggerType, &provider, &e.Version); err != nil {
		return Execution{}, err
	}
	e.SessionID, e.PredecessorExecutionID, e.ProviderExecutionID =
		session.String, predecessor.String, provider.String
	return e, nil
}
