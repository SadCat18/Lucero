package task

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Queryer 是本包用到的读接口子集（窄接口便于注入）。为什么不直接用 sql.QueryerContext：
// database/sql 不导出查询接口，driver.QueryerContext 是驱动侧 SPI、参数类型不同、*sql.DB 不满足它。
// 完整版见 internal/session/repository.go。
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Repository 只读：SELECT tasks 一张表。写路径唯一入口是 db.Create / db.Touch。
type Repository struct{ q Queryer }

func NewRepository(q Queryer) *Repository { return &Repository{q: q} }

// taskCols 末列是 REQ-LK-1 的来源会话：link 表是**关系行**（不发事件、不推版本），
// 所以读模型用相关子查询把它带回来，而不是给 tasks 加一列 —— 加列会造出第二个真相来源。
// 取法要写死：`UNIQUE(task_id, session_id, role)` 只禁"同一会话挂两次"，**不禁**一个 task 挂多个 ORIGIN
// （裁定没说不许多挂，我不替它发明），所以这里按 `ordinal, id` 取确定的一条并留 `LIMIT 1` ——
// 否则真多挂起来时读侧是 500，而不是"看见第一个"。多挂本身由写侧那条 UNIQUE 用例反证（第 268 条）。
const taskCols = `id, project_id, parent_task_id, title, description, task_type, mode, priority,
	status, risk_level, requested_by, created_at, updated_at, accepted_at, archived_at, version,
	(SELECT l.session_id FROM task_session_links l
	   WHERE l.task_id = tasks.id AND l.role = 'ORIGIN' ORDER BY l.ordinal, l.id LIMIT 1)`

// Get 是写侧 Service 取"库里的当前状态"的唯一来源：状态机的 from 不许由调用方口头给。
func (r *Repository) Get(ctx context.Context, id string) (Task, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+taskCols+` FROM tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if err != nil {
		return Task{}, fmt.Errorf("task: get %s: %w", id, err)
	}
	return t, nil
}

// Filter 是 GET /api/v1/tasks 的查询条件。游标用 (created_at, id) 的 keyset（契约③ §2 禁 offset），
// 由 handler 负责编解码，本层只吃解好的两列。
type Filter struct {
	Status         State
	ProjectID      string
	AfterCreatedAt int64
	AfterID        string
	Limit          int
}

// List 按 created_at, id 升序出队。Scheduler 也走这条：§18 明说 priority 不是唯一执行顺序，
// 排序判定留给它，这里不预设。
func (r *Repository) List(ctx context.Context, f Filter) ([]Task, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(f.Status))
	}
	if f.ProjectID != "" {
		where = append(where, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.AfterID != "" {
		// 不用行值比较，展开成显式条件：SQLite 各版本都吃，且能走 created_at 索引。
		where = append(where, "(created_at > ? OR (created_at = ? AND id > ?))")
		args = append(args, f.AfterCreatedAt, f.AfterCreatedAt, f.AfterID)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+taskCols+` FROM tasks WHERE `+strings.Join(where, " AND ")+
			` ORDER BY created_at, id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("task: list: %w", err)
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("task: list scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// StatusCounts 一次 GROUP BY 出面板要的状态分布，不把行拉回来数。
func (r *Repository) StatusCounts(ctx context.Context) (map[State]int64, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT status, count(*) FROM tasks GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("task: status counts: %w", err)
	}
	defer rows.Close()
	out := map[State]int64{}
	for rows.Next() {
		var s State
		var n int64
		if err := rows.Scan(&s, &n); err != nil {
			return nil, fmt.Errorf("task: status counts scan: %w", err)
		}
		out[s] = n
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanTask(row rowScanner) (Task, error) {
	var (
		t                    Task
		project, parent      sql.NullString
		desc, requestedBy    sql.NullString
		acceptedAt, archived sql.NullInt64
		origin               sql.NullString // 没有 ORIGIN 行时子查询给 NULL，不是空串
	)
	if err := row.Scan(&t.ID, &project, &parent, &t.Title, &desc,
		&t.TaskType, &t.Mode, &t.Priority, &t.Status, &t.RiskLevel, &requestedBy,
		&t.CreatedAt, &t.UpdatedAt, &acceptedAt, &archived, &t.Version, &origin); err != nil {
		return Task{}, err
	}
	t.ProjectID, t.ParentTaskID, t.Description, t.RequestedBy =
		project.String, parent.String, desc.String, requestedBy.String
	t.SessionID = origin.String
	t.AcceptedAt = acceptedAt.Int64
	t.Archived = archived.Valid
	return t, nil
}
