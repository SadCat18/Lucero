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
