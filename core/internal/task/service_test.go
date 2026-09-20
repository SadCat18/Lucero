package task_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

// reporter 就是将来 cmd/core 要绑的那个适配器：Notification 走真 Service，
// Audit 这一路现在只有计数器（audit 的哈希链入口需要 Entry 映射，属接线工作）。
type reporter struct {
	svc     *notification.Service
	audits  int
	lastRec task.RejectionReport
}

func (r *reporter) Report(ctx context.Context, tx *db.Tx, rec task.RejectionReport) error {
	if r.svc != nil {
		if _, err := r.svc.Notify(ctx, tx, notification.Draft{
			ID: rec.NotificationID, EventID: rec.EventID,
			Type: notification.TypeProjectionRejected, Priority: notification.PriorityHigh,
			Title: "Task 投影无合法目标", Body: rec.Reason, TaskID: rec.TaskID,
			PrincipalID: rec.PrincipalID, CorrelationID: rec.CorrelationID,
		}); err != nil {
			return err
		}
	}
	r.audits++
	r.lastRec = rec
	return nil
}

// newTaskAt 用平台层把一条 Task 建在指定状态（Create 建首行 + 版本 1 + 一行事件）。
func newTaskAt(t *testing.T, d *db.DB, ctx context.Context, id string, status task.State) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		// event_journal.principal_id 有外键：两种主体都得先在 principals 里存在。
		for _, p := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
			if _, err := tx.Append("principals",
				[]string{"id", "principal_type", "display_name", "created_at", "updated_at"},
				[]any{p[0], p[1], p[0], now, now}); err != nil {
				return err
			}
		}
		return tx.Create(db.AggTask, id,
			[]string{"title", "status", "created_at", "updated_at"},
			[]any{"跑通垂直链", string(status), now, now},
			db.EventDraft{EventID: "ev-new-" + id, Type: task.EventCreated,
				Aggregate: db.AggTask, AggregateID: id, PrincipalID: "system"})
	}); err != nil {
		t.Fatal(err)
	}
}

func countByAggType(t *testing.T, d *db.DB, aggType, id string) int64 {
	t.Helper()
	var n int64
	if err := d.Read.QueryRow(
		`SELECT count(*) FROM event_journal WHERE aggregate_type = ? AND aggregate_id = ?`, aggType, id).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestApplyExecutionEventAdvancesTask 是写侧主路径：同事务读当前状态 → §5.3 投影 → Touch，
// 一个版本一行事件，事件类型按 §6 换成 task_waiting_approval。
func TestApplyExecutionEventAdvancesTask(t *testing.T) {
	ctx := context.Background()
	rep := &reporter{}
	svc := task.NewService(rep)
	d := openDB(t)
	newTaskAt(t, d, ctx, "t-run", task.Running)

	var res task.Result
	err := d.Write(ctx, func(tx *db.Tx) error {
		var err error
		res, err = svc.ApplyExecutionEvent(ctx, tx, "t-run", task.EdgeApprovalRequested, false, false,
			task.Command{EventID: "ev-1", PrincipalID: "local-user", CorrelationID: "c-1",
				ApprovalID: "ap-1", WantVersion: 1})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Advanced || res.To != task.WaitingApproval || res.Version != 2 {
		t.Errorf("推进结果 = %+v，应为 → WAITING_APPROVAL v2", res)
	}
	if n := countByAggType(t, d, "task", "t-run"); n != 2 {
		t.Errorf("task 聚合应恰好两行事件（created + waiting_approval），实际 %d", n)
	}
	if n := countWhere(t, d, `SELECT count(*) FROM event_journal
		WHERE event_type = 'task_waiting_approval' AND payload_json LIKE '%"approval_id":"ap-1"%'`); n != 1 {
		t.Errorf("task_waiting_approval 的 payload 没带 approval_id，计数 %d", n)
	}
	if rep.audits != 0 {
		t.Errorf("放行的投影不该产生 §5.4 记录，audits=%d", rep.audits)
	}
}

// TestApplyExecutionEventIfMatch：库里版本已经前进，带旧 If-Match 的请求必须 409 且不 Touch。
func TestApplyExecutionEventIfMatch(t *testing.T) {
	ctx := context.Background()
	svc := task.NewService(&reporter{})
	d := openDB(t)
	newTaskAt(t, d, ctx, "t-im", task.Running)

	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.ApplyExecutionEvent(ctx, tx, "t-im", task.EdgeApprovalRequested, false, false,
			task.Command{EventID: "ev-a", PrincipalID: "system", ApprovalID: "ap-1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.ApplyExecutionEvent(ctx, tx, "t-im", task.EdgeApprovalRequested, false, false,
			task.Command{EventID: "ev-b", PrincipalID: "system", ApprovalID: "ap-2", WantVersion: 1})
		return err
	})
	if !errors.Is(err, task.ErrVersionConflict) {
		t.Fatalf("If-Match 不符应返回 ErrVersionConflict，实际 %v", err)
	}
	if n := countByAggType(t, d, "task", "t-im"); n != 2 {
		t.Errorf("被 409 拒掉的请求不该推进事件行数，实际 %d", n)
	}
	if s, v := getTask(t, d, "t-im"); s != task.WaitingApproval || v != 2 {
		t.Errorf("被拒的请求不该改库里状态，实际 %s v%d", s, v)
	}
}

// TestApplyExecutionEventRejection 是 §5.4 的完整闭环（更正后的精确口径）：
// Task 聚合零新增版本、零事件；恰有一行 notification_created（aggregate_type=notification）；
// Audit 一次。Notification 走的是真 notification.Service + 真表。
func TestApplyExecutionEventRejection(t *testing.T) {
	ctx := context.Background()
	rep := &reporter{svc: notification.NewService()}
	svc := task.NewService(rep)
	d := openDB(t)
	newTaskAt(t, d, ctx, "t-rej", task.Preparing)

	var res task.Result
	err := d.Write(ctx, func(tx *db.Tx) error {
		var err error
		res, err = svc.ApplyExecutionEvent(ctx, tx, "t-rej", task.EdgeFailed, true, true,
			task.Command{EventID: "ev-x", PrincipalID: "system", CorrelationID: "c-9",
				Notification: "n-rej-1", RejEvent: "ev-rej-1"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Advanced || res.To != task.Preparing || res.Version != 1 {
		t.Errorf("被拒的投影必须让 Task 停在原态原版本: %+v", res)
	}
	if res.Rejection.Reason == "" || res.Rejection.ExecutionEvent != task.EdgeFailed {
		t.Errorf("被拒的投影必须交出 §5.4 的解释: %+v", res.Rejection)
	}
	if n := countByAggType(t, d, "task", "t-rej"); n != 1 {
		t.Errorf("§5.4：aggregate_type=task 的事件必须零新增（只剩建行那条），实际 %d", n)
	}
	if n := countByAggType(t, d, "notification", "n-rej-1"); n != 1 {
		t.Errorf("§5.4 第 2 条要求恰有一行 notification_created，实际 %d", n)
	}
	if rep.audits != 1 {
		t.Errorf("Audit 必须恰有一条（不进 event_journal），实际 %d", rep.audits)
	}
	got, err := notification.NewRepository(d.Read).Get(ctx, "n-rej-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != notification.TypeProjectionRejected || got.TaskID != "t-rej" || got.State != notification.StateUnread {
		t.Errorf("通知内容不对: %+v", got)
	}
	if rep.lastRec.Reason == "" || rep.lastRec.TaskID != "t-rej" {
		t.Errorf("交给 reporter 的记录不完整: %+v", rep.lastRec)
	}
}

// 没注入 reporter 时必须报错而不是静默吞（§5.4 第 3 条）。
func TestRejectionWithoutReporterFails(t *testing.T) {
	ctx := context.Background()
	svc := task.NewService(nil)
	d := openDB(t)
	newTaskAt(t, d, ctx, "t-nil", task.Preparing)

	err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.ApplyExecutionEvent(ctx, tx, "t-nil", task.EdgeFailed, true, true,
			task.Command{EventID: "ev-nil", PrincipalID: "system"})
		return err
	})
	if err == nil {
		t.Fatal("静默吞掉了 §5.4 的拒绝：没注入 reporter 应该报错")
	}
	if n := countByAggType(t, d, "notification", "t-nil"); n != 0 {
		t.Errorf("这条路径不该有通知，实际 %d", n)
	}
}

func countWhere(t *testing.T, d *db.DB, query string) int64 {
	t.Helper()
	var n int64
	if err := d.Read.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
