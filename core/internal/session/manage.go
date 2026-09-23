package session

// `POST /api/v1/sessions/{id}:manage` 的判定半边（T-192② 派的、T-195② 改过修法形状的那一口）。
// 契约③ §3 那格的原话是"它是**纯 session 聚合的一次写**（置 managed + 发 `session_managed`），
// 不依赖 probe/Provider" ⇒ 这条路不需要连接层，也不等 `:sync`。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// Writer 是写侧需要的最小面（`*db.Tx` 天然满足）：`:manage` 只要 `Touch`，discover 还要 `Create` 建行。
// 两个能力合成一个接口而不是拆两个：它们是同一个事务上的两种写，拆开只会多一层"哪个方法要哪个接口"的疑问。
// 刻意不与读侧的 `Queryer` 合成"大 Store"：凑签名多出来的方法会让读的人以为是必需依赖。
type Writer interface {
	Touch(a db.AggregateKind, id, setSQL string, args []any, ev db.EventDraft) (int64, error)
	Create(a db.AggregateKind, id string, columns []string, values []any, ev db.EventDraft) error
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ErrInvalidCommand 拦在入口：event_id 缺失意味着这一次写没有可复用的身份（重试就会发第二条事件）。
var ErrInvalidCommand = errors.New("session: 命令不合法")

// ErrProjectNotFound 是"`project_id` 指向一个不存在的项目"。它**不**是 FK 报错冒到 transport：
// 那样客户拿到的是 500（`internal_error`），而事实是请求写错了 ⇒ 这里先预检，映射成 400 `validation_failed`。
var ErrProjectNotFound = errors.New("session: 那个项目不存在")

// ManageCommand 是一次 `:manage`。`ProjectID` 用指针区分**"请求没给这一格"**与"给了 null（清空归属）"：
// 折成 string 就分不开，与 `Session.SyncCursor` 为什么是指针是同一条理由（REQ-F-21 第 ④ 条）。
// **三态由 transport 喂对**：请求里没这个键 → nil（不动归属）；显式 `null` → 指着空串的指针（解绑）；给了值 → 指着那个 id。
type ManageCommand struct {
	Managed bool
	// nil = 本次不动归属；非 nil = 要把 `project_id` 改成它（空串 = 清空）。
	ProjectID     *string
	EventID       string
	PrincipalID   string
	CorrelationID string
	CausationID   string
}

// projectOrNil 把领域里"空串 = 没有归属"翻成事件 payload 要的 `null`。
// C41 裁定那句是"两键都必须在、`project_id` 允许显式 null"⇒ 这里**不能省键**（`*string` 不带 omitempty 正好），
// 也不能拿 `""` 顶替 null —— 那是"绑到一个空串命名的项目"，外键列上不存在这种值。
func projectOrNil(project string) *string {
	if project == "" {
		return nil
	}
	return &project
}

// Manage 落这一条写：改 `managed`（和可选的 `project_id`）+ 恰一行 `session_managed`，同一事务。
// 返回的第二个值是"这一次真改了吗"：目标值与库里现值相同时是**合法 no-op**（200、版本不动、事件不发）——
// 面板重复点"受管理"不该产出一条新事实（与 `project.Patch` 的空 PATCH、`:read` 的重复读同一条口径）。
//
// 事件 payload 装的是**生效后**的那两格（不是请求体的回声）：§6 那一格的键就是 `managed` `project_id`，
// 若我把没改动的 `project_id` 留空发出去，事件就在说"这条会话没有归属"，而库里那一行明明有 —— 那是事件替请求撒谎。
func (s *Service) Manage(ctx context.Context, tx Writer, id string, cmd ManageCommand) (Session, bool, error) {
	if strings.TrimSpace(cmd.EventID) == "" {
		return Session{}, false, fmt.Errorf("%w: 缺 event_id（重试要复用同一个）", ErrInvalidCommand)
	}
	cur, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, false, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
		}
		return Session{}, false, err
	}

	project := cur.ProjectID
	projectChanged := false
	if cmd.ProjectID != nil {
		project = *cmd.ProjectID
		projectChanged = project != cur.ProjectID
		if project != "" {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id = ?`, project).Scan(&n); err != nil {
				return Session{}, false, fmt.Errorf("session: 查项目 %s: %w", project, err)
			}
			if n == 0 {
				return Session{}, false, fmt.Errorf("%w: %s", ErrProjectNotFound, project)
			}
		}
	}
	if cmd.Managed == cur.Managed && !projectChanged {
		return cur, false, nil
	}

	// `managed` 是 `INTEGER NOT NULL CHECK (managed IN (0,1))`。这里显式写 0/1 而不是传布尔，
	// 但**别把这件事当成"不写 0/1 就会坏"**：实测驱动会换算（变异 V4b 写 `true`、V4d 写 `"1"` 都照样落 1、用例没红），
	// 真正兜住这一列的是 CHECK 本身（V4c 写 2 当场被拒）。写 0/1 只是为了让读代码的人不必先去查驱动换算规则。
	set := []string{"managed = ?"}
	args := []any{0}
	if cmd.Managed {
		args[0] = 1
	}
	if projectChanged {
		set = append(set, "project_id = ?")
		if project == "" {
			args = append(args, nil) // 清空归属要的是 NULL，不是空串（外键列上 '' 会去匹配一个不存在的项目）
		} else {
			args = append(args, project)
		}
	}
	set = append(set, "updated_at = ?")
	args = append(args, s.now())

	payload, err := payloadOf(ManagedPayload{Managed: cmd.Managed, ProjectID: projectOrNil(project)})
	if err != nil {
		return Session{}, false, fmt.Errorf("session: managed payload: %w", err)
	}
	if _, err := tx.Touch(db.AggSession, id, strings.Join(set, ", "), args, db.EventDraft{
		EventID: cmd.EventID, Type: EventManaged, Aggregate: db.AggSession, AggregateID: id,
		CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID, PrincipalID: cmd.PrincipalID,
		Payload: payload,
	}); err != nil {
		return Session{}, false, fmt.Errorf("session: manage %s: %w", id, err)
	}
	// C18 裁定 ②：回读库里那一行，不回显请求。
	updated, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return Session{}, false, err
	}
	return updated, true, nil
}

// payloadOf 把 struct 编成 §6 要求的 map（`"schema":1` 由平台层在 appendEvent 里补，这里不重复注入 —— 见 events.go 顶部那段）。
func payloadOf(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// nowMillisNow 是 NewService 缺省用的时钟；装配方（cmd/core）会给它自己那份共享时钟，
// 所以这里不宣称"全仓只有一个时间源"，只保证**没给的时候也不至于写个零**。
func nowMillisNow() int64 { return time.Now().UnixMilli() }
