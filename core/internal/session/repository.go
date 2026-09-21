package session

import (
	"context"
	"database/sql"
	"fmt"
)

// Queryer 是本包用到的读接口子集：consumer-side 窄接口，*sql.DB / *sql.Conn / *sql.Stmt 直接满足。
//
// 为什么自己声明而不是引用标准库的同名接口：database/sql 根本不导出"查询接口"这一类东西，
// QueryerContext 只存在于 database/sql/driver，而且是**驱动侧 SPI**
// （QueryContext(ctx, query, []driver.NamedValue) (driver.Rows, error)），参数类型不同，
// *sql.DB 并不满足它。这与工具链版本无关 —— 上游 Go 从来如此（T-32 撤回的那句"Go 1.8 起
// database/sql 导出了它"是错的，实测 `go doc database/sql` 里没有任何 Queryer*）。
// 注：这个窄接口不是这台机器的 workaround，标准做法本来就是调用方自定义小接口。
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Repository 只做读：SELECT 本模块自己的 sessions / messages / task_session_links。
// 写路径唯一入口是 internal/platform/db 的 Create/Touch（ADR-004 禁全局仓储，
// 内部契约 §1 规则 3：业务包各自持有 Repository，但拿不到别的表的写权限）。
type Repository struct{ q Queryer }

func NewRepository(q Queryer) *Repository { return &Repository{q: q} }

const sessionCols = `id, adapter_instance_id, project_id, remote_session_id, title, managed,
	sync_state, sync_cursor, remote_revision, state_confidence,
	last_message_at, last_synced_at, created_at, updated_at, archived_at, version`

type scanner interface{ Scan(dest ...any) error }

func (r *Repository) Get(ctx context.Context, id string) (Session, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id)
	s, err := scanSession(row)
	if err != nil {
		return Session{}, fmt.Errorf("session: get %s: %w", id, err)
	}
	return s, nil
}

// ForTask 是内部契约 §4 里"某个 task 关联了哪些会话"的唯一走法：一个 Task 可以同时挂着
// 多家软件的多个 Session，按 ordinal 排。
// ponytail: 先取 link 再逐个取 Session 是 N+1，一个 task 的 link 是个位数。
// 必须先把游标关掉再发第二条查询：SQLite 单连接池下边持游标边查询会死锁（实测卡满 600s 测试超时）。
func (r *Repository) ForTask(ctx context.Context, taskID string) ([]TaskSession, error) {
	var links []TaskSession
	err := func() error {
		rows, err := r.q.QueryContext(ctx,
			`SELECT id, session_id, role, ordinal FROM task_session_links
			 WHERE task_id = ? ORDER BY ordinal, created_at`, taskID)
		if err != nil {
			return fmt.Errorf("session: links of task %s: %w", taskID, err)
		}
		defer rows.Close()
		for rows.Next() {
			var ls TaskSession
			if err := rows.Scan(&ls.LinkID, &ls.Session.ID, &ls.Role, &ls.Ordinal); err != nil {
				return fmt.Errorf("session: links of task %s scan: %w", taskID, err)
			}
			links = append(links, ls)
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, err
	}
	out := make([]TaskSession, 0, len(links))
	for _, ls := range links {
		ls.Session, err = r.Get(ctx, ls.Session.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, ls)
	}
	return out, nil
}

// ListMessages 按 (sequence_no, created_at) 升序返回一个 Session 的消息，最多 limit 条。
func (r *Repository) ListMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, session_id, remote_message_id, role, kind, sequence_no,
			text_content, created_at, synced_at
		 FROM messages WHERE session_id = ?
		 ORDER BY COALESCE(sequence_no, created_at), created_at LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("session: messages of %s: %w", sessionID, err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var (
			m      Message
			remote sql.NullString
			seq    sql.NullInt64
			text   sql.NullString
			synced sql.NullInt64
		)
		if err := rows.Scan(&m.ID, &m.SessionID, &remote, &m.Role, &m.Kind, &seq,
			&text, &m.CreatedAt, &synced); err != nil {
			return nil, fmt.Errorf("session: messages of %s scan: %w", sessionID, err)
		}
		m.RemoteMessageID, m.SequenceNo, m.TextContent, m.SyncedAt =
			remote.String, seq.Int64, text.String, synced.Int64
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanSession(row scanner) (Session, error) {
	var (
		s       Session
		proj    sql.NullString
		remote  sql.NullString
		title   sql.NullString
		cursor  sql.NullString
		rev     sql.NullString
		managed int64
		lastMsg sql.NullInt64
		lastSyn sql.NullInt64
		arch    sql.NullInt64
	)
	if err := row.Scan(&s.ID, &s.AdapterInstanceID, &proj, &remote, &title, &managed,
		&s.SyncState, &cursor, &rev, &s.StateConfidence,
		&lastMsg, &lastSyn, &s.CreatedAt, &s.UpdatedAt, &arch, &s.Version); err != nil {
		return Session{}, err
	}
	s.ProjectID, s.RemoteSessionID, s.Title, s.RemoteRevision =
		proj.String, remote.String, title.String, rev.String
	// 游标单独赋值：NULL 与非空是两个语义，不能像其它列那样用 .String 折成空串（REQ-F-21 ④）。
	if cursor.Valid && cursor.String != "" {
		v := cursor.String
		s.SyncCursor = &v
	} else {
		s.SyncCursor = nil
	}
	s.Managed, s.LastMessageAt, s.LastSyncedAt, s.Archived =
		managed == 1, lastMsg.Int64, lastSyn.Int64, arch.Valid
	return s, nil
}
