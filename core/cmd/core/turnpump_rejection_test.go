package main

// §5.4 那两条记录在**泵这一腿**的落库判据（T-417 开单那一格；域那一侧的转发判据在
// `internal/execution/finish_rejection_test.go`，两条各判一段，谁也不冒充谁）。
//
// 装配刻意照 `main.go:185~208` 那一段来（`rejectionReporter` 注进 `task.Service`、
// `auditedStore` 带锚点计数器）：这一格要判的就是"生产那根线接上之后，收尾那一笔真能走完"。
// 为什么必须连锚点计数器一起给 —— 它对 nil 接收者**不安全**（`Advance` 第一句就读 `head.Seq` 之后上锁），
// 而测试夹具里那枚 `auditedStore{d: d}` 是没有它的；少这一步，这条判据会先 panic 再红，
// 红成一个读不出原因的样子（那种红不如不跑）。
//
// 形状：Task 停在 `QUEUED`（`execution_succeeded` 那条边只从 `RUNNING` 驱动它 ⇒ 必然被拒），
// 泵收到一帧 `turn/finished`。要同时成立的四件（§5.4 三条 + C12 更正后的精确口径）：
// 　① execution 的终态**落了**（被拒的是 task 投影，不是这条执行本身）；
// 　② Task 不推进版本、`aggregate_type=task` 零事件；
// 　③ 恰有一行 `notification_created`（独立聚合）+ 一行 `notifications`（type=projection_rejected）；
// 　④ Audit 走行哈希链（这里经 audit 自己的 `HeadQuery` 读链头，不直查 `audit_entries`）。

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

func TestPumpWritesThe54RecordsWhenProjectionRejected(t *testing.T) {
	_, _, _, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: "不需要孩子"}})
	now := func() int64 { return 4_000 }
	anchor := &audit.Anchor{Path: filepath.Join(t.TempDir(), "audit.head")}
	store := auditedStore{d: d, checkpoints: audit.NewCheckpointCounter(anchor, now)}
	svc := execution.NewService()
	// 与 main.go 同一对：reporter 进 task.Service，session 只当落消息那半格用（这一腿用不到它写）。
	svc.EnableSend(task.NewService(&rejectionReporter{notes: notification.NewService(), now: now}),
		session.NewService(session.NewRepository(d.Read), now))
	var tap logTap
	hub := newTurnHub(context.Background(), newLauncher(nil, time.Second, nil, store, nil), store, svc,
		nil, nil, tap.f)
	t.Cleanup(hub.closeAll)

	mustExec(t, d, `INSERT INTO adapter_instances (id, adapter_key, display_name, health, created_at, updated_at)
		VALUES ('ad-rj', 'fake', 'Fake Adapter', 'HEALTHY', 1, 1)`)
	mustExec(t, d, `INSERT INTO tasks (id, title, status, created_at, updated_at)
		VALUES ('task-rj', '泵上被拒这一格', 'QUEUED', 1, 1)`)
	mustExec(t, d, `INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no, status,
		state_confidence, trigger_type, created_at, updated_at)
		VALUES ('ex-rj', 'task-rj', 'ad-rj', 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`)
	headBefore, _ := chainHead(t, d)

	hub.watch("fake", "ex-rj")
	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-rj",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnFinished, ResultCode: "SUCCEEDED"}})

	if n := tap.count("收尾没落成"); n != 0 {
		t.Fatalf("泵喊了落不成 ⇒ §5.4 那一支把整笔终态写入拖下水了（%d 句）：%v", n, tap.lines)
	}
	// ① execution 的终态落了。
	var status, resultCode string
	mustQuery(t, d, `SELECT status, COALESCE(result_code,'') FROM executions WHERE id='ex-rj'`, &status, &resultCode)
	if status != "SUCCEEDED" {
		t.Errorf("收尾该落终态，实际 status=%s result_code=%s；泵的观察：%v", status, resultCode, tap.lines)
	}
	// ② Task 一行都不许多。
	var taskStatus string
	var taskVersion int64
	mustQuery(t, d, `SELECT status, version FROM tasks WHERE id='task-rj'`, &taskStatus, &taskVersion)
	if taskStatus != "QUEUED" || taskVersion != 1 {
		t.Errorf("被拒的投影不许推进 Task：status=%s version=%d（应 QUEUED/1）", taskStatus, taskVersion)
	}
	if n := journalCountOf(t, d, "task", "task-rj"); n != 0 {
		t.Errorf("§5.4 第 1 条：task 聚合零事件，实际 %d 行", n)
	}
	// ③ 恰一条通知 + 恰一行 notification_created（独立聚合，C12 更正点名的就是这一格）。
	if n := countOf(t, d, `SELECT count(*) FROM notifications WHERE notification_type='projection_rejected'`); n != 1 {
		t.Errorf("恰一行 projection_rejected 通知，实际 %d 行", n)
	}
	if n := journalCountOf(t, d, "notification", ""); n != 1 {
		t.Errorf("恰一行 notification_created（aggregate_type=notification），实际 %d 行", n)
	}
	// ④ Audit 上了链：链头往前走（读法用 audit 自己的 HeadQuery，不直查那张表）。
	headAfter, hashAfter := chainHead(t, d)
	if headAfter <= headBefore || hashAfter == "" {
		t.Errorf("§5.4 的 Audit 没上链（链头 %d→%d、hash=%q）；泵的观察：%v", headBefore, headAfter, hashAfter, tap.lines)
	}
	// 被拒这一支不许静默：泵那一腿正常只喊"迟到收尾已落终态"，通知那一行由 reporter 落，
	// 而链头能往前走就说明它走的是 audit 的通道，不是被谁偷偷吞了。
	if n := tap.count("迟到收尾已落终态"); n != 1 {
		t.Errorf("这一笔应当正常落了（恰一声收尾喊话），实际 %d：%v", n, tap.lines)
	}
}

// mustExec/mustQuery/countOf/chainHead 是这一格要的四件小工具；`chainHead` 刻意走 audit 的读口，
// 因为 `audit_entries` 那张表按契约③ §4 + C1 分家不归本包直查。
func mustExec(t *testing.T, d *db.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Read.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func mustQuery(t *testing.T, d *db.DB, q string, dest ...any) {
	t.Helper()
	if err := d.Read.QueryRow(q).Scan(dest...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func countOf(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func journalCountOf(t *testing.T, d *db.DB, aggType, aggID string) int {
	t.Helper()
	q := `SELECT count(*) FROM event_journal WHERE aggregate_type = ?`
	args := []any{aggType}
	if aggID != "" {
		q += ` AND aggregate_id = ?`
		args = append(args, aggID)
	}
	return countOf(t, d, q, args...)
}

func chainHead(t *testing.T, d *db.DB) (int64, string) {
	t.Helper()
	seq, hash, err := audit.HeadQuery(d.Read)(context.Background())
	if err != nil {
		t.Fatalf("读链头失败（这条判据的地基）：%v", err)
	}
	return seq, hash
}
