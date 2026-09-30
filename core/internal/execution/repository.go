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

// execCols 是三条只读路径共用的那一列清单（Get / ListByTask / 恢复扫描）——
// 单条读与列表读**共用它**，所以视图里出现的键两边必然同形（不许一边有一边没有）。
// 三份快照列（*_snapshot_json）是 yaml `Execution` 的三格，读面要出口给面板。
// 第八十批按 yaml `Execution`（core-v1.yaml:1151~1174）补的九列排在末尾：model_id、
// result_code/error_code/error_message、started_at/heartbeat_at/finished_at、created_at/updated_at
// —— 与 `scanExecution` 的目的参数逐位对齐，加列必须两处同批改。
// 写者（第八十一批甲档）不在这里：`columnWrites`（service.go:216）跟 status 同笔落；
// error_message / heartbeat_at / provider_execution_id 今天仍无写者（NULL 是真值）。
const execCols = `id, task_id, adapter_instance_id, session_id, model_id, predecessor_execution_id,
	attempt_no, status, state_confidence, trigger_type, provider_execution_id, version,
	model_snapshot_json, permission_snapshot_json, environment_snapshot_json,
	result_code, error_code, error_message, started_at, heartbeat_at, finished_at,
	created_at, updated_at`

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
// Core 重启后要看的就是这五个状态。改判的入口在 recovery.go 的 ScanRecovery（REQ-RC-1，
// 生产调用方），判定 reconnect / resume / interrupt 仍归 Recovery Engine，本包不做。
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
		model       sql.NullString
		predecessor sql.NullString
		provider    sql.NullString
		resultCode  sql.NullString
		errorCode   sql.NullString
		errorMsg    sql.NullString
		started     sql.NullInt64
		heartbeat   sql.NullInt64
		finished    sql.NullInt64
	)
	// 三份快照列 NOT NULL DEFAULT '{}'（迁移 000001）⇒ 直接绑 string：
	// 真出现 NULL 是迁移被人改过，宁可 Scan 报错也不折算成"没有这一格"。
	// created_at/updated_at NOT NULL ⇒ 直接绑 int64；其余可空列（model_id/result_*/error_*/三个时刻）
	// 走 Null* —— 「NULL 折成零值」是域的约定（domain.go:5 同 task），那一步在下面显式做，不靠 Scan 悄悄吞。
	if err := row.Scan(&e.ID, &e.TaskID, &e.AdapterInstanceID, &session, &model, &predecessor,
		&e.AttemptNo, &e.Status, &e.StateConfidence, &e.TriggerType, &provider, &e.Version,
		&e.ModelSnapshot, &e.PermissionSnapshot, &e.EnvironmentSnapshot,
		&resultCode, &errorCode, &errorMsg, &started, &heartbeat, &finished,
		&e.CreatedAt, &e.UpdatedAt); err != nil {
		return Execution{}, err
	}
	e.SessionID, e.ModelID, e.PredecessorExecutionID, e.ProviderExecutionID =
		session.String, model.String, predecessor.String, provider.String
	e.ResultCode, e.ErrorCode, e.ErrorMessage = resultCode.String, errorCode.String, errorMsg.String
	e.StartedAt, e.HeartbeatAt, e.FinishedAt = started.Int64, heartbeat.Int64, finished.Int64
	return e, nil
}
