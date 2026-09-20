package notification

import (
	"context"
	"database/sql"
	"fmt"
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

// ListByState 给 GET /api/v1/notifications?state= 用，按 inbox 索引序（priority, created_at DESC）。
// ponytail: 只有 limit、没有契约③ §2 的 keyset cursor，等 transport 落地时统一加。
func (r *Repository) ListByState(ctx context.Context, state State, limit int) ([]Notification, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+cols+` FROM notifications WHERE state = ?
		 ORDER BY CASE priority WHEN 'CRITICAL' THEN 0 WHEN 'HIGH' THEN 1 WHEN 'MEDIUM' THEN 2 ELSE 3 END,
		          created_at DESC, id
		 LIMIT ?`, string(state), limit)
	if err != nil {
		return nil, fmt.Errorf("notification: list %s: %w", state, err)
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("notification: list %s scan: %w", state, err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scan(row rowScanner) (Notification, error) {
	var (
		n                             Notification
		body, project, task, approval sql.NullString
		readAt, handledAt, expiresAt  sql.NullInt64
	)
	if err := row.Scan(&n.ID, &n.Type, &n.Priority, &n.State, &n.Title, &body, &project,
		&task, &approval, &n.CreatedAt, &readAt, &handledAt, &expiresAt, &n.Version); err != nil {
		return Notification{}, err
	}
	n.Body, n.ProjectID, n.TaskID, n.ApprovalID = body.String, project.String, task.String, approval.String
	n.ReadAt, n.HandledAt, n.ExpiresAt = readAt.Int64, handledAt.Int64, expiresAt.Int64
	return n, nil
}
