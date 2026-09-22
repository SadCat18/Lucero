package notification

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Queryer 是本包用到的读接口子集（理由见 internal/session/repository.go 顶部的可复现证据）。
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Repository 只做读：SELECT notifications 一张表。
type Repository struct{ q Queryer }

func NewRepository(q Queryer) *Repository { return &Repository{q: q} }

const cols = `id, notification_type, priority, state, title, body, project_id, task_id,
	approval_id, created_at, read_at, handled_at, expires_at, version`

func (r *Repository) Get(ctx context.Context, id string) (Notification, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+cols+` FROM notifications WHERE id = ?`, id)
	n, err := scan(row)
	if err != nil {
		return Notification{}, fmt.Errorf("notification: get %s: %w", id, err)
	}
	return n, nil
}

// priorityRank 是 inbox 的档位序，**只有一个来源**：这一句 SQL。
// Go 侧不抄第二份表 —— ListPage 把它 SELECT 出来回在 ListPosition 里，
// 所以 transport 编码的游标与这里的定义不可能各说各话。
const priorityRank = `CASE priority WHEN 'CRITICAL' THEN 0 WHEN 'HIGH' THEN 1 WHEN 'MEDIUM' THEN 2 ELSE 3 END`

// ListPosition 是一行在 (priority 档 ASC, created_at DESC, id ASC) 这套 keyset 序里的位置。
type ListPosition struct {
	Rank      int
	CreatedAt int64
	ID        string
}

// ListFilter 是 inbox 那一页的条件。State 为 nil = **不按状态过滤**（C26 裁定 ②：
// "不传"不许兼职成"只要 UNREAD"，否则面板再也表达不出"我要全部"）。After 为 nil = 第一页。
type ListFilter struct {
	State *State
	After *ListPosition
	Limit int
}

// ListPage 按 inbox 序出一页，并带回**本页最后一行**的位置（handler 拿它编 next_cursor）。
// 原名 ListByState：C26 之后状态过滤是可选的，继续叫那个名字就是骗下一个读代码的人。
// 契约③ §2 禁 offset，所以翻页是 keyset 谓词而不是 LIMIT/OFFSET；游标的编解码属 transport。
// 零行时 last 是零值，handler 据此回 `next_cursor: ""` —— 那是**分页**口径（REQ-F-19：空串唯一表示
// "没有下一页"）。REQ-F-21/22 的**同步**游标口径与它相反（空串非法、只有非空串或 null），两套不许共用编解码。
func (r *Repository) ListPage(ctx context.Context, f ListFilter) ([]Notification, ListPosition, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	var where []string
	var args []any
	if f.State != nil {
		where = append(where, "state = ?")
		args = append(args, string(*f.State))
	}
	if f.After != nil {
		// 展开成显式条件、不用行值比较：对齐 internal/task/repository.go 那条老决定。
		where = append(where, "("+priorityRank+" > ?"+
			" OR ("+priorityRank+" = ? AND created_at < ?)"+
			" OR ("+priorityRank+" = ? AND created_at = ? AND id > ?))")
		args = append(args, f.After.Rank,
			f.After.Rank, f.After.CreatedAt,
			f.After.Rank, f.After.CreatedAt, f.After.ID)
	}
	args = append(args, limit)
	// WHERE 整段是可省的：不过滤时拼出 "WHERE  ORDER BY" 就是语法错（第一次跑就是这么红的）。
	query := `SELECT ` + priorityRank + ` AS priority_rank, ` + cols + ` FROM notifications`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY priority_rank, created_at DESC, id LIMIT ?`
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, ListPosition{}, fmt.Errorf("notification: list page: %w", err)
	}
	defer rows.Close()
	var out []Notification
	var last ListPosition
	for rows.Next() {
		// Rows.Scan 要求一次吃满所有列（stdlib 会直接报 "expected N destination arguments"），
		// 所以多出来的 priority_rank 只能跟着一起交给 scan，不能先单独 Scan 一次。
		var rank int
		n, err := scan(rows, &rank)
		if err != nil {
			return nil, ListPosition{}, fmt.Errorf("notification: list page scan: %w", err)
		}
		out = append(out, n)
		last = ListPosition{Rank: rank, CreatedAt: n.CreatedAt, ID: n.ID}
	}
	return out, last, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

// scan 读 cols 那 14 列。列表查询在 cols 之前多带一列（priority_rank），
// 用 leading 传进去，而不是把那套可空列的处理复制第二份。
func scan(row rowScanner, leading ...any) (Notification, error) {
	var (
		n                             Notification
		body, project, task, approval sql.NullString
		readAt, handledAt, expiresAt  sql.NullInt64
	)
	dest := append(append([]any{}, leading...), &n.ID, &n.Type, &n.Priority, &n.State, &n.Title,
		&body, &project, &task, &approval, &n.CreatedAt, &readAt, &handledAt, &expiresAt, &n.Version)
	if err := row.Scan(dest...); err != nil {
		return Notification{}, err
	}
	n.Body, n.ProjectID, n.TaskID, n.ApprovalID = body.String, project.String, task.String, approval.String
	n.ReadAt, n.HandledAt, n.ExpiresAt = readAt.Int64, handledAt.Int64, expiresAt.Int64
	return n, nil
}
