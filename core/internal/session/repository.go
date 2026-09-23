package session

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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

// RemoteIndex 是 discover 的去重依据：这台 Adapter 实例已经落过哪些 `remote_session_id` → 本地行 id。
// 只取两列、不扫整行 —— 一次发现可能几十条，把整行都读回来只是把 §14 那半行浪费一遍。
// 先把游标关干净再返回：单连接池下边持游标边发下一条语句会死锁（实测卡满 600s 测试超时，
// 而 discover 的 upsert 紧接着就要写，这一格必须严格）。
func (r *Repository) RemoteIndex(ctx context.Context, adapterInstanceID string) (map[string]string, error) {
	out := map[string]string{}
	err := func() error {
		rows, err := r.q.QueryContext(ctx,
			`SELECT remote_session_id, id FROM sessions WHERE adapter_instance_id = ?`, adapterInstanceID)
		if err != nil {
			return fmt.Errorf("session: remote index of %s: %w", adapterInstanceID, err)
		}
		defer rows.Close()
		for rows.Next() {
			var remote, id string
			if err := rows.Scan(&remote, &id); err != nil {
				return fmt.Errorf("session: remote index scan: %w", err)
			}
			// 库里要是混进两条同 remote id 的行（外键与唯一索引都没拦，只有约定拦），
			// 保留先出现的那条并**不静默覆盖**：覆盖会让下一次 discover 又建一行，行数就一直涨。
			if _, dup := out[remote]; !dup {
				out[remote] = id
			}
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, err
	}
	return out, nil
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

// ListFilter 是 GET /api/v1/sessions 的条件。ProjectID 是**契约里明写的过滤位**
// （yaml 的 `project` 参数，"F-8 批准"），不是我自己加的（对照 C26：没有契约来源的字段不许兼职）。
// 三个字段全空的 ProjectID = 不过滤；After* 走 keyset，没有 OFFSET（契约③ §2）。
type ListFilter struct {
	ProjectID      string
	AfterCreatedAt int64
	AfterID        string
	Limit          int
}

// ListPage 按 (created_at, id) 升序出一页 —— 与 task/project 那两条 list 同一个序、同一套二元游标形状，
// 所以 transport 复用现成的 encodeCursor/decodeCursor，本批零新增编解码函数。
func (r *Repository) ListPage(ctx context.Context, f ListFilter) ([]Session, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	where := []string{"1=1"}
	args := []any{}
	if f.ProjectID != "" {
		where = append(where, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.AfterID != "" {
		where = append(where, "(created_at > ? OR (created_at = ? AND id > ?))")
		args = append(args, f.AfterCreatedAt, f.AfterCreatedAt, f.AfterID)
	}
	args = append(args, limit)
	rows, err := r.q.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE `+
		strings.Join(where, " AND ")+` ORDER BY created_at, id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("session: list: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("session: list scan: %w", err)
		}
		out = append(out, s)
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
