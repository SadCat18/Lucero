package execution_test

// §5.4 在收尾那一腿的转发判据（T-417：这一格正式开单之前没人接，因为契约里它写着"尚无 owner"）。
//
// 判的是**这一层唯一能判的事**：`FinishFromTurn` 把调用方铸好的三枚 id 原样交给 `task.Service`。
// 被拒那一支要写的两条记录（Notification `projection_rejected` + Audit）住在装配层
// （`cmd/core/rejections.go`），它的真实落库由 `cmd/core/turnpump_rejection_test.go` 判 ——
// 这里用一只替身 reporter，只钉"id 有没有被转过去"，不重抄那两条记录的形状（那会长出第二份说法）。
//
// 为什么这一格值得单独一条：空 id 不是"少写一条通知"，是**整笔连坐** ——
// `Tx.Create` 拿空主键建 notification 行会失败 ⇒ 回滚 ⇒ execution 的终态也写不进去，
// 泵只能喊一句"收尾没落成"。所以转发断了必须在**这一层**就能红，而不是等装配层的日志。

import (
	"context"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// rejectionSpy 是被注入的 reporter：它只记账，不写库（写库那一半在 cmd/core 的同一接口实现上判）。
type rejectionSpy struct {
	calls []task.RejectionReport
}

func (s *rejectionSpy) Report(_ context.Context, _ *db.Tx, r task.RejectionReport) error {
	s.calls = append(s.calls, r)
	return nil
}

// rejectFixture 播一对必然被拒的形状：Task 在 `QUEUED`、Execution 在 `RUNNING`，
// 而收尾帧是 `SUCCEEDED` ⇒ §5.3 那条边只从 RUNNING 驱动 Task ⇒ 走 §5.4 那一支。
func rejectFixture(t *testing.T, d *db.DB, taskID, execID string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, status, created_at, updated_at)
		VALUES (?, '投影被拒这一格', 'QUEUED', 1, 1)`, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, ?, 'ad-codex', 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`, execID, taskID); err != nil {
		t.Fatal(err)
	}
}

func taskRowOf(t *testing.T, d *db.DB, id string) (string, int64) {
	t.Helper()
	var status string
	var v int64
	if err := d.Read.QueryRow(`SELECT status, version FROM tasks WHERE id = ?`, id).Scan(&status, &v); err != nil {
		t.Fatal(err)
	}
	return status, v
}

func journalRowsOf(t *testing.T, d *db.DB, aggType, id string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE aggregate_type = ? AND aggregate_id = ?`,
		aggType, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestFinishFromTurnForwardsTheRejectionIDs 判三件事：被拒那一支**被走到**（不是静默、不是报错回滚）、
// 三枚 id 逐字转发（非空且互不相同）、Task 一侧一行都不许多（§5.4 第 1 条 + C12 更正的精确口径）。
func TestFinishFromTurnForwardsTheRejectionIDs(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	rejectFixture(t, d, "task-rej", "ex-rej")

	spy := &rejectionSpy{}
	svc := execution.NewService()
	svc.EnableSend(task.NewService(spy), session.NewService(session.NewRepository(d.Read), func() int64 { return 4_000 }))

	ctx := context.Background()
	cmd := rejectionCmd()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, _, err := svc.FinishFromTurn(ctx, tx, "ex-rej", "SUCCEEDED", cmd)
		return err
	}); err != nil {
		t.Fatalf("被拒的投影不该把整笔收尾拖下水（§5.4 走通知+审计，Task 停在原态）：%v", err)
	}

	if len(spy.calls) != 1 {
		t.Fatalf("§5.4 那一支该被走到恰一次，实际 %d 次", len(spy.calls))
	}
	got := spy.calls[0]
	if got.TaskID != "task-rej" || got.TaskState != task.Queued || got.ExecutionEvent != "execution_succeeded" {
		t.Errorf("报告里的现场不对（TaskID/TaskState/ExecutionEvent）：%+v", got)
	}
	if got.Reason == "" {
		t.Error(`Reason 是 details_json 里唯一说得出"为什么拒"的一格，不许为空`)
	}
	ids := []string{got.NotificationID, got.EventID, got.AuditID}
	for i, v := range ids {
		if v == "" {
			t.Errorf("三枚 id 里第 %d 枚是空串 ⇒ 装配层那一支会被 notification 的入参校验先拒、整笔回滚：%+v", i, got)
		}
	}
	if got.NotificationID == got.EventID || got.EventID == got.AuditID || got.NotificationID == got.AuditID {
		t.Errorf("三枚 id 必须互不相同（两个 UNIQUE 命名空间共用一个 id 会撞）：%+v", got)
	}

	// Task 一侧：状态与版本逐字不变，且 `aggregate_type=task` 零事件（C12 更正后的精确口径）。
	if st, v := taskRowOf(t, d, "task-rej"); st != "QUEUED" || v != 1 {
		t.Errorf("被拒的投影不许推进 Task：status=%s version=%d（应为 QUEUED/1）", st, v)
	}
	if n := journalRowsOf(t, d, "task", "task-rej"); n != 0 {
		t.Errorf("§5.4 第 1 条：被拒时 task 聚合零事件，实际 %d 行", n)
	}
	// 而 execution 那一侧的终态是真的落了（否则这一支就成了"静默吞 + 丢终态"）。
	var status, resultCode string
	if err := d.Read.QueryRow(`SELECT status, COALESCE(result_code,'') FROM executions WHERE id='ex-rej'`).
		Scan(&status, &resultCode); err != nil {
		t.Fatal(err)
	}
	if status != "SUCCEEDED" || resultCode != "SUCCEEDED" {
		t.Errorf("收尾该落终态（被拒的是 task 投影，不是 execution），实际 status=%s result_code=%s", status, resultCode)
	}
}

// TestFinishFromTurnWithoutTheIDsReachesTheReporterWithEmptyOnes 把"这一层的责任边界"钉死：
// 域**不铸身份**（§3），调用方不给就是空 —— 所以"给不给"是调用方的合同，红要红在转发这一格。
// （真实后果由 cmd/core 那条装配判据兜：空 id 让整笔回滚，那里能跑真 reporter。）
func TestFinishFromTurnWithoutTheIDsReachesTheReporterWithEmptyOnes(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	rejectFixture(t, d, "task-rej2", "ex-rej2")

	spy := &rejectionSpy{}
	svc := execution.NewService()
	svc.EnableSend(task.NewService(spy), session.NewService(session.NewRepository(d.Read), func() int64 { return 4_000 }))

	ctx := context.Background()
	cmd := rejectionCmd()
	cmd.RejNotificationID, cmd.RejEventID, cmd.RejAuditID = "", "", ""
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, _, err := svc.FinishFromTurn(ctx, tx, "ex-rej2", "SUCCEEDED", cmd)
		return err
	}); err != nil {
		t.Fatalf("这一格只判转发，替身 reporter 不该失败：%v", err)
	}
	if len(spy.calls) != 1 || spy.calls[0].NotificationID != "" || spy.calls[0].AuditID != "" {
		t.Fatalf("调用方没给 id 时，域**不许自己补一枚**（§3 身份由调用方铸）：%+v", spy.calls)
	}
}

func rejectionCmd() execution.Command {
	return execution.Command{
		EventID: "ev-rj-1", TaskEventID: "ev-rj-2", TaskWaitingAcceptanceEventID: "ev-rj-3",
		RejNotificationID: "ntf-rj-1", RejEventID: "ev-rj-4", RejAuditID: "aud-rj-1",
		PrincipalID: "system", CorrelationID: "corr-rj", CausationID: "corr-rj",
	}
}
