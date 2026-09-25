package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// Writer 是写路径的最小子集，*db.Tx 天然满足。新建聚合一律走 Create（内部契约 §1.1 第 6 条），
// 不是 Append —— notifications 是聚合表，Append 会被平台层拒。
type Writer interface {
	Create(a db.AggregateKind, id string, columns []string, values []any, ev db.EventDraft) error
}

// ErrInvalidDraft 拦在 Service 入口：id 或 title 缺失、priority 不在词表里，
// 都不该变成一条坏通知（§9.2「不把 Secret 写进明文」之外，"发错通知"同样是事故）。
var ErrInvalidDraft = errors.New("notification: invalid draft")

type Service struct{ now func() int64 }

func NewService() *Service { return &Service{now: func() int64 { return time.Now().UnixMilli() }} }

// Notify 建一条 UNREAD 通知：一行 notifications + 恰一行 notification_created，同一个事务。
// 返回后由调用方在事务提交之后再推 WebSocket（ADR-008）；本包不碰任何推送通道。
func (s *Service) Notify(ctx context.Context, tx Writer, d Draft) (Notification, error) {
	switch {
	case d.ID == "":
		return Notification{}, fmt.Errorf("%w: 缺 id（必须由调用方预生成 UUIDv7，重试复用）", ErrInvalidDraft)
	case d.EventID == "":
		return Notification{}, fmt.Errorf("%w: 缺 event_id", ErrInvalidDraft)
	case d.Type == "":
		return Notification{}, fmt.Errorf("%w: 缺 notification_type", ErrInvalidDraft)
	case d.Title == "":
		return Notification{}, fmt.Errorf("%w: 缺 title", ErrInvalidDraft)
	}
	if d.Priority == "" {
		d.Priority = PriorityMedium
	}
	if !d.Priority.Valid() {
		return Notification{}, fmt.Errorf("%w: priority %q 不在词表里", ErrInvalidDraft, d.Priority)
	}

	n := Notification{
		ID: d.ID, Type: d.Type, Priority: d.Priority, State: StateUnread,
		Title: d.Title, Body: d.Body, ProjectID: d.ProjectID, TaskID: d.TaskID,
		ApprovalID: d.ApprovalID, ExpiresAt: d.ExpiresAt, CreatedAt: s.now(), Version: 1,
	}
	cols := []string{"notification_type", "priority", "state", "title", "created_at"}
	vals := []any{string(n.Type), string(n.Priority), string(n.State), n.Title, n.CreatedAt}
	// 可空列（尤其是三个外键）只在真有值时才进列表：
	// 传空串等于写入一个非 NULL 的 ''，会直接撞 approval_id / task_id 的外键。
	for _, opt := range []struct {
		col  string
		val  any
		used bool
	}{
		{"body", n.Body, n.Body != ""},
		{"project_id", n.ProjectID, n.ProjectID != ""},
		{"task_id", n.TaskID, n.TaskID != ""},
		{"approval_id", n.ApprovalID, n.ApprovalID != ""},
		{"expires_at", n.ExpiresAt, n.ExpiresAt != 0},
	} {
		if opt.used {
			cols = append(cols, opt.col)
			vals = append(vals, opt.val)
		}
	}

	payload, err := Payload(CreatedPayload{NotificationType: n.Type, Priority: n.Priority,
		TaskID: n.TaskID, ApprovalID: n.ApprovalID})
	if err != nil {
		return Notification{}, fmt.Errorf("notification: payload: %w", err)
	}
	if err := tx.Create(db.AggNotification, n.ID, cols, vals, db.EventDraft{
		EventID: d.EventID, Type: EventCreated, Aggregate: db.AggNotification, AggregateID: n.ID,
		CorrelationID: d.CorrelationID, CausationID: d.CausationID, PrincipalID: d.PrincipalID,
		Payload: payload,
	}); err != nil {
		return Notification{}, fmt.Errorf("notification: create %s: %w", n.ID, err)
	}
	return n, nil
}

// Toucher 是 `:read` 那一条状态边需要的最小写面（`*db.Tx` 天然满足）。
// 与上面的 `Writer` 分开声明、不合成一个"大 Writer"：凑签名而多出来的方法，读的人会以为是必需依赖。
type Toucher interface {
	Touch(a db.AggregateKind, id, setSQL string, args []any, ev db.EventDraft) (int64, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ErrIllegalTransition 是"这一条边今天不许走"。它映射成 §15.2 的 409 `illegal_transition`
// （那张表明写这一码"来自各 Service 而非 Adapter"，所以正是这里该用的）。
var ErrIllegalTransition = errors.New("notification: illegal state transition")

// ReadCommand 带 event_id 而不带版本号：yaml 这条路径的参数只有 `Idempotency-Key` 与 `Principal`，
// 没有 `If-Match`，所以这里不发明一道乐观锁（§15.2 的 `version_conflict` 也不该由这条路产生）。
type ReadCommand struct {
	EventID       string
	PrincipalID   string
	CorrelationID string
	CausationID   string
}

// HandleCommand 就是 `ReadCommand` 那四个字段 —— §6 里 `_read` 与 `_handled` 是三行**共用**同一列 payload
// 键的合并行，两路口径也一样（都不带版本号、都不要求 Idempotency-Key），所以这里用类型别名而不是复制一份：
// 抄一遍四个字段，早晚有一处改了另一处没改（CO-49 那族）。
type HandleCommand = ReadCommand

// MarkHandled 落契约① §1.1 第 21 条（09-25 裁定）那条边：`UNREAD|READ|PENDING → HANDLED`，
// 同一事务里一行状态改动 + 恰一行 `notification_handled`。
//
// 三格判据都是裁定给的，不是我挑的：
//   - `HANDLED → HANDLED` 是**合法 no-op**（200、版本不动、不覆写 `handled_at`、**不发事件**）—— 与 C36① 已认的
//     `:read` 重放同一口径；第二个返回值就是"这一次真改了吗"。
//   - `IGNORED`/`EXPIRED → HANDLED` 报 `ErrIllegalTransition`（409）：**一行都不许多写**，所以这里在任何 Touch 之前返回。
//     `IGNORED` 本身就是用户的处置、`EXPIRED` 是系统判定的时效已过，都不是另一个动作该覆盖掉的痕迹。
//   - `READ` 在允许集里是**有意的**（它记"看过"、不是"处置过"），且它与 `:read` 的不对称别来统一：
//     只放 `READ→HANDLED` 会逼面板在用户点"已处理"时先补发一次 `:read`，一次用户动作就在 journal 里留下两条边。
//
// `UNREAD → HANDLED` **只写 `handled_at`、不补 `read_at`**：把"处置"折成"也算读过"，就是替面板抹掉"未读但已处理"这个真实形状。
func (s *Service) MarkHandled(ctx context.Context, tx Toucher, id string, cmd HandleCommand) (Notification, bool, error) {
	if strings.TrimSpace(cmd.EventID) == "" {
		return Notification{}, false, fmt.Errorf("%w: 缺 event_id（重试要复用同一个，不然会发第二条事件）", ErrInvalidDraft)
	}
	cur, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return Notification{}, false, err
	}
	switch cur.State {
	case StateUnread, StateRead, StatePending:
	case StateHandled:
		return cur, false, nil
	default:
		return cur, false, fmt.Errorf("%w: %s→HANDLED（已处置/已失效的状态不再被一次「处理」盖掉）", ErrIllegalTransition, cur.State)
	}
	at := s.now()
	payload, err := Payload(HandledPayload{
		NotificationType: cur.Type, Priority: cur.Priority, TaskID: cur.TaskID, ApprovalID: cur.ApprovalID,
	})
	if err != nil {
		return Notification{}, false, fmt.Errorf("notification: handled payload: %w", err)
	}
	// 两列：`state` 与 `handled_at`。这张表**没有 updated_at**（同 MarkRead 那格），`Touch` 返回的是新版本号不是行数。
	if _, err := tx.Touch(db.AggNotification, id, "state = ?, handled_at = ?",
		[]any{string(StateHandled), at}, db.EventDraft{
			EventID: cmd.EventID, Type: EventHandled, Aggregate: db.AggNotification, AggregateID: id,
			CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID, PrincipalID: cmd.PrincipalID,
			Payload: payload,
		}); err != nil {
		return Notification{}, false, fmt.Errorf("notification: mark handled %s: %w", id, err)
	}
	updated, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return Notification{}, false, err
	}
	return updated, true, nil
}

// MarkRead 落 `UNREAD|PENDING → READ` 这一条边：一行状态改动 + 恰一行 `notification_read`，同一事务。
// 返回的第二个值是"这一次真改了吗"——重复点"读过"是**合法的 no-op**：200、版本不动、事件也不发
// （什么都没变还发一条更新，就是把事件写成仪式；与 `project.Patch` 的空 PATCH 同一条口径）。
// 已处置的三态（HANDLED/IGNORED/EXPIRED）报 ErrIllegalTransition：把它们重新标成"读过"是替面板抹掉处置痕迹。
// 这两格判据契约里都没给，是我自挑的，账上记 **C36**。
func (s *Service) MarkRead(ctx context.Context, tx Toucher, id string, cmd ReadCommand) (Notification, bool, error) {
	if strings.TrimSpace(cmd.EventID) == "" {
		return Notification{}, false, fmt.Errorf("%w: 缺 event_id（重试要复用同一个，不然会发第二条事件）", ErrInvalidDraft)
	}
	cur, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return Notification{}, false, err
	}
	switch cur.State {
	case StateUnread, StatePending:
	case StateRead:
		return cur, false, nil
	default:
		return cur, false, fmt.Errorf("%w: %s→READ（已处置的状态不再标成读过）", ErrIllegalTransition, cur.State)
	}
	at := s.now()
	payload, err := Payload(ReadPayload{
		NotificationType: cur.Type, Priority: cur.Priority, TaskID: cur.TaskID, ApprovalID: cur.ApprovalID,
	})
	if err != nil {
		return Notification{}, false, fmt.Errorf("notification: read payload: %w", err)
	}
	// 只有两列要动：notifications 表**没有 updated_at**（000001:580 那一张的列里没有它），
	// 所以这里不顺手写一个不存在的列 —— 那会是一条 SQL 逻辑错。
	// `Touch` 返回的是**新版本号**（`UPDATE … RETURNING version`），不是受影响行数：
	// 第一版我按"行数"判了个 `!= 1`，于是每一次合法的读都被我自己报成"行不在库里"。
	// 找不到行那一格由 Touch 自己回 `ErrAggregateNotFound`，不需要我在这里数行。
	if _, err := tx.Touch(db.AggNotification, id, "state = ?, read_at = ?",
		[]any{string(StateRead), at}, db.EventDraft{
			EventID: cmd.EventID, Type: EventRead, Aggregate: db.AggNotification, AggregateID: id,
			CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID, PrincipalID: cmd.PrincipalID,
			Payload: payload,
		}); err != nil {
		return Notification{}, false, fmt.Errorf("notification: mark read %s: %w", id, err)
	}
	updated, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return Notification{}, false, err
	}
	return updated, true, nil
}
