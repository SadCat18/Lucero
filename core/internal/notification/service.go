package notification

import (
	"context"
	"errors"
	"fmt"
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
