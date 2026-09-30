package main

// 死亡观察与崩溃那一支的判据（T-404 ①②）。**形状是注入驱动的**：用 fake-adapter 的
// `debug/hold`（把收尾扣住，制造"在飞"）与 `debug/crash`（§10 的崩溃注入，先回执再退、退出码 2）
// 造出"Core 活着、对端进程死"这一形 —— 契约① 第 25 条 ④-a :713 那句「触发点 = Supervisor 观察到子进程死亡」
// 今天就是这么被走到的。它**不证**真 Provider 的进程会怎么死，只证 Core 收到"死了"这个观察之后做的那几件事。
//
// 三条线各自有主：
//   - 在飞名单（`turnHub.watch`）⇒ 崩溃改判只覆盖**我自己发过帧的那些执行**，库里别的 RUNNING 行不算；
//   - §7.2 健康列 ⇒ 走 `internal/adapter` 那唯一写口 `DowngradeHealth`（T-402 ①，泵只调用）；
//   - 崩溃改判那一笔 ⇒ `execution_unknown` 已在 §6 与平台目录里（09-30 三处同改那一批），所以今天
//     真落得下去。这里的断言仍是**配对的**：喊成功就必须与库里一致、喊失败就必须一行没动 ——
//     两种都不许悄悄，也不必因为"墙拆了"而改测试（分支由喊话决定，不由开关决定）。

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
)

func pumpHealth(t *testing.T, d *db.DB, adapterKey string) string {
	t.Helper()
	var h string
	if err := d.Read.QueryRow(`SELECT health FROM adapter_instances WHERE adapter_key = ?`, adapterKey).Scan(&h); err != nil {
		t.Fatalf("读 %s 的 health: %v", adapterKey, err)
	}
	return h
}

func pumpExecStatus(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(`SELECT status FROM executions WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatalf("读 %s: %v", id, err)
	}
	return s
}

func pumpEventsOf(t *testing.T, d *db.DB, evType string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type = ?`, evType).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// deathFixture：probe → discover 先留下实例行与会话行（健康列也在这两步里落到 HEALTHY），
// 返回的 hub 带着真 svc / 健康写口 / 过程事实 —— `newBareDiscover` 现在就是按真装配给的，
// 所以这几条用例打的是生产接缝，不是替身。
func deathFixture(t *testing.T) (*sendPipe, *logTap, *db.DB, string) {
	t.Helper()
	if os.Getenv("LUCERO_TEST_FAKE_ADAPTER") != "1" {
		t.Skip("未开 LUCERO_TEST_FAKE_ADAPTER=1：不 build、不起真子进程（这几条判据必须开着跑）")
	}
	exe := buildFakeAdapterHere(t)
	probe, discover, pipe, d := newBareDiscover(t,
		[]adapterSpec{{key: "fake", displayName: "Fake Adapter", command: exe}})
	tap := &logTap{}
	pipe.hub.logf = tap.f
	pipe.logf = tap.f

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	view, err := probe.Probe(ctx, "fake")
	if err != nil {
		t.Fatalf("probe 失败：%v", err)
	}
	items, _, err := discover.DiscoverSessions(ctx, view.ID)
	if err != nil || len(items) == 0 {
		t.Fatalf("discover 该留下真会话：%v（%d 条）", err, len(items))
	}
	return pipe, tap, d, items[0].SessionID
}

// insertInFlightRow 播一条"在飞"的执行（task + execution 两行，RUNNING / HIGH）。
// 拆出来只为一件新判据（T-414 ②）需要在**占住写面之前**把行备好、把 hold 武装上，然后再出帧。
func insertInFlightRow(t *testing.T, d *db.DB, execID, taskID, sess string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, status, created_at, updated_at)
		VALUES (?, '崩溃这一格', 'RUNNING', 1, 1)`, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, ?, (SELECT id FROM adapter_instances WHERE adapter_key='fake'), ?, 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`,
		execID, taskID, sess); err != nil {
		t.Fatal(err)
	}
}

// armHold 起常驻连接并武装 debug/hold（§10 那条注入：收尾被扣住，这一轮就留在飞）。
func armHold(t *testing.T, pipe *sendPipe) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pipe.hub.connection(ctx, "fake")
	if err != nil {
		t.Fatalf("起常驻连接失败：%v", err)
	}
	if _, err := conn.Call(ctx, "debug/hold", map[string]any{"on": true}); err != nil {
		t.Fatalf("武装 debug/hold 失败：%v", err)
	}
}

// sendHeldTurn 打一发 start_now：这一发被接下、收尾被 hold 扣住 ⇒ 它是一条在飞的执行。
func sendHeldTurn(t *testing.T, pipe *sendPipe, execID, sess string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pipe.CallSendTurn(ctx, execution.TurnCall{
		ExecutionID: execID, SessionID: sess, Text: "留在飞", Delivery: "start_now",
		IdempotencyKey: "k-die-" + execID,
	}); err != nil {
		t.Fatalf("这一发 send 该被接下：%v", err)
	}
}

// releaseHeldTurn 解除 hold ⇒ 对端把那一轮的 `turn/finished` 交出来（不带 code 时默认 SUCCEEDED，
// `core/cmd/fake-adapter/main.go:341~343` 那句）。这一帧**需要写库**，所以它也是"泵还能不能干活"的探针。
func releaseHeldTurn(t *testing.T, pipe *sendPipe) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pipe.hub.connection(ctx, "fake")
	if err != nil {
		t.Fatalf("取常驻连接失败：%v", err)
	}
	if _, err := conn.Call(ctx, "debug/hold", map[string]any{"on": false}); err != nil {
		t.Fatalf("解除 debug/hold 失败：%v", err)
	}
}

// armHoldAndSend 武装 hold 再打一发 start_now：这一发被接下、收尾被扣住 ⇒ 它就是一条"在飞"的执行。
func armHoldAndSend(t *testing.T, pipe *sendPipe, d *db.DB, execID, taskID, sess string) {
	t.Helper()
	insertInFlightRow(t, d, execID, taskID, sess)
	armHold(t, pipe)
	sendHeldTurn(t, pipe, execID, sess)
}

// crashIt 让真孩子当场死掉（debug/crash 先回执、后 os.Exit(2)）。
func crashIt(t *testing.T, pipe *sendPipe) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := pipe.hub.connection(ctx, "fake")
	if err != nil {
		t.Fatalf("取连接失败：%v", err)
	}
	if _, err := conn.Call(ctx, adapter.MethodDebugCrash, nil); err != nil {
		t.Fatalf("debug/crash 该先回执：%v", err)
	}
}

func TestPumpDeathMarksInFlightAndDowngradesHealth(t *testing.T) {
	pipe, tap, d, sess := deathFixture(t)
	armHoldAndSend(t, pipe, d, "ex-die", "task-die", sess)
	before := pumpHealth(t, d, "fake")
	if st := pumpExecStatus(t, d, "ex-die"); st != "RUNNING" {
		t.Fatalf("前提：hold 之下这一条该还是 RUNNING，实际 %s", st)
	}

	crashIt(t, pipe)
	if !tap.wait(t, "崩溃处理收尾", 20*time.Second) {
		t.Fatalf("没等到崩溃处理的收尾一声 ⇒ 死亡观察没跑（日志：%v）", tap.lines)
	}

	if n := tap.count("观察到对端进程终止"); n != 1 {
		t.Errorf("死亡观察该恰一声，实际 %d：%v", n, tap.lines)
	}
	if n := tap.count("连接退役（对端进程已终止）"); n != 1 {
		t.Errorf("这条连接该被退役恰一次，实际 %d：%v", n, tap.lines)
	}
	// 在飞名单是本批接线的正身：**必须正好 1 条**（0 条 = watch 没接上，2 条 = 名单是从库里猜的）。
	if summary := tap.lineWith("崩溃处理收尾"); !strings.Contains(summary, "在飞 1 条") {
		t.Fatalf("在飞名单该认得这一条 send（它出帧前就登记了），实际那一声是 %q", summary)
	}
	// 配对断言：改判成不成，都要与库和喊话一致。两分支由**喊话**选，不由开关选 ⇒ 墙拆了不必改这里。
	landed, blocked := tap.count("判为 UNKNOWN"), tap.count("崩溃改判没落成")
	status := pumpExecStatus(t, d, "ex-die")
	switch {
	case landed == 1 && blocked == 0:
		if status != "UNKNOWN" {
			t.Errorf("喊了判为 UNKNOWN 而库里是 %s ⇒ 半真历史", status)
		}
		if n := pumpEventsOf(t, d, "execution_unknown"); n != 1 {
			t.Errorf("改判该配一条 execution_unknown，实际 %d 行", n)
		}
	case landed == 0 && blocked == 1:
		if status != "RUNNING" {
			t.Errorf("改判没成却把状态改了（%s）⇒ 那是「只改状态不发事件」那一形", status)
		}
		if n := pumpEventsOf(t, d, "execution_unknown"); n != 0 {
			t.Errorf("改判没成却留下事件（%d 行）⇒ 状态与事件得同生同死", n)
		}
	default:
		t.Fatalf("改判的喊话必须恰一条、且只在一侧：成功 %d、失败 %d（日志：%v）", landed, blocked, tap.lines)
	}
	// 健康列走 adapter 那唯一写口：值真变了，而且它自己那条事件也在（CO-58 的耦合就钉在这两行上）。
	if got := pumpHealth(t, d, "fake"); got != adapter.HealthUnavailable {
		t.Errorf("对端死了之后 §7.2 那列该从 %s 降到 UNAVAILABLE（写口在 internal/adapter），实际 %s；泵的观察：%v", before, got, tap.lines)
	}
	if n := pumpEventsOf(t, d, "adapter_health_changed"); n == 0 {
		t.Errorf("健康是 adapter 的 store 写的，它那条 adapter_health_changed 该留下；没有就说明泵绕过了写口。泵的观察：%v", tap.lines)
	}
	// provider 把手的**观察面**：claimed 那一刻被泵记进在飞名单。库里那一腿的配对断言在
	// `turnpump_claim_test.go` —— 两腿各判各的，谁也不替谁担保（内存那份活不过重启，库里那份才是交接）。
	if n := tap.count("claimed 带回 provider_execution_id"); n != 1 {
		t.Errorf("claimed 那帧该被记恰一次，实际 %d 句：%v", n, tap.lines)
	}
}

// TestPumpDeathLeavesOtherRunningRowsAlone 钉"别扩大改判面"：库里有一条**没经过这条连接**的 RUNNING 执行，
// 它不许被顺手判成 UNKNOWN。⇒ 在飞名单只能是"我自己出过帧的那些"，不能是 `SELECT ... WHERE status='RUNNING'`。
func TestPumpDeathLeavesOtherRunningRowsAlone(t *testing.T) {
	pipe, tap, d, _ := deathFixture(t)
	if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, status, created_at, updated_at)
		VALUES ('task-idle', '没经过这条连接的', 'RUNNING', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES ('ex-idle', 'task-idle', (SELECT id FROM adapter_instances WHERE adapter_key='fake'), 1,
		'RUNNING', 'HIGH', 'INITIAL', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	crashIt(t, pipe)
	if !tap.wait(t, "崩溃处理收尾", 20*time.Second) {
		t.Fatalf("没等到崩溃处理的收尾一声：%v", tap.lines)
	}
	if n := tap.count("在飞 0 条"); n != 1 {
		t.Errorf("这台一条 send 都没在飞，那一声该说 0 条：%v", tap.lines)
	}
	if st := pumpExecStatus(t, d, "ex-idle"); st != "RUNNING" {
		t.Errorf("没经过这条连接的执行被顺手改了（%s）⇒ 名单不能从库里猜", st)
	}
	if n := pumpEventsOf(t, d, "execution_unknown"); n != 0 {
		t.Errorf("一条都没改判就不该有 execution_unknown，实际 %d 行", n)
	}
	if got := pumpHealth(t, d, "fake"); got != adapter.HealthUnavailable {
		t.Errorf("没有在飞执行也要降健康，实际 %s；泵的观察：%v", got, tap.lines)
	}
}

// TestShutdownIsNotACrash 钉的是"关停不是崩溃"那一格：Core 自己关停也会"看到"孩子终止
// （`Close` 就是杀它），但那不是对端死了 ⇒ 不改判、不降健康、不走崩溃处理。
// 守住这一格的是 `watchDeath` 里 `werr != nil` 那一句（`closeAll` 先按 cancel、再退役，所以主动关停
// 那一趟 `Wait` 必然带 err）。**这条用例就是那一句的全部理由**：摘掉它，本用例当场红（本批变异 N-g）。
// 原来这里还有一面 `closing atomic.Bool`，T-408 ③ 二选一之后按"无人能观察的状态不留"摘了 ——
// 它与"cancel 已按"永远同真，而反过来还能把一次紧邻关停的真死亡读成"不是崩溃"。
//
// 等的是观察者自己那声"不作崩溃处理"（就在 return 前一句），**不是**等"连接退役"：上一版等后者，
// 判据就成了"看观察者跑没跑完"的赛跑 —— N-g 第一次跑正是这样被蒙过去的（第 306 条记这个反例）。
func TestShutdownIsNotACrash(t *testing.T) {
	pipe, tap, d, sess := deathFixture(t)
	armHoldAndSend(t, pipe, d, "ex-shut", "task-shut", sess)
	before := pumpHealth(t, d, "fake")

	pipe.hub.closeAll()
	if !tap.wait(t, "不作崩溃处理", 15*time.Second) {
		t.Fatalf("死亡观察者没给出它的决定 ⇒ 这一格又变成靠运气等的了：%v", tap.lines)
	}
	if n := tap.count("不作崩溃处理"); n != 1 {
		t.Errorf("这一代连接只有一个观察者，决定该恰一声，实际 %d：%v", n, tap.lines)
	}
	if n := tap.count("崩溃处理收尾"); n != 0 {
		t.Errorf("Core 主动关停不许走崩溃处理，实际 %d 声：%v", n, tap.lines)
	}
	if st := pumpExecStatus(t, d, "ex-shut"); st != "RUNNING" {
		t.Errorf("关停不许改判任何执行，实际 %s", st)
	}
	if got := pumpHealth(t, d, "fake"); got != before {
		t.Errorf("关停不许动健康列：%s → %s", before, got)
	}
	if n := pumpEventsOf(t, d, "execution_unknown"); n != 0 {
		t.Errorf("关停路上凭空发了 %d 条 execution_unknown", n)
	}
}

// TestPumpFlightsTrackOnlyWhatWasSent 不打真进程，专钉那份名单与 claimed 的时机（它决定崩溃时改判谁）。
func TestPumpFlightsTrackOnlyWhatWasSent(t *testing.T) {
	var tap logTap
	hub := newTurnHub(context.Background(), newLauncher(nil, time.Second, nil, auditedStore{}, nil),
		auditedStore{}, nil, nil, nil, tap.f)

	hub.watch("k1", "ex-a")
	hub.watch("k2", "ex-b")
	// claimed 走真通道：只有已登记的那条会被记上，且只认 claimed 那一帧。
	hub.handle("k1", 1, process.Inbound{ExecutionID: "ex-a",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnClaimed, ProviderExecutionID: "prov-a"}})
	hub.handle("k1", 1, process.Inbound{ExecutionID: "ex-never-watched",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnClaimed, ProviderExecutionID: "prov-x"}})
	hub.handle("k1", 1, process.Inbound{ExecutionID: "ex-a",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnDispatch, ProviderExecutionID: "prov-late"}})

	got := hub.takeFlights("k1")
	if len(got) != 1 {
		t.Fatalf("k1 上只该有 ex-a 一条，实际 %v", got)
	}
	if got["ex-a"] != "prov-a" {
		t.Errorf("provider 把手该来自 claimed 那一刻（dispatch 上那个不许覆盖，契约② §6 规则 3），实际 %q", got["ex-a"])
	}
	if second := hub.takeFlights("k1"); len(second) != 0 {
		t.Errorf("一次取走之后该空（否则同一场崩溃被处理两遍）：%v", second)
	}
	hub.dropFlight("ex-b")
	if k2 := hub.takeFlights("k2"); len(k2) != 0 {
		t.Errorf("注销之后不该再被算进崩溃面：%v", k2)
	}
	// 收尾一到就注销：这一条与"重连之后又被改判一次"是同一个事故的两面。
	hub.watch("k3", "ex-c")
	hub.handle("k3", 1, process.Inbound{ExecutionID: "ex-c",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnFinished, ResultCode: "SUCCEEDED"}})
	if left := hub.takeFlights("k3"); len(left) != 0 {
		t.Errorf("收到收尾的执行不该还留在飞名单里：%v", left)
	}
	// claimed 那两帧在**落库面**那一腿也必须出声：这台 hub 只接了观察面（svc==nil），
	// 两帧 ⇒ 两声"没接落库面"；而上面那条 dispatched 帧一声都不该有（:741 第二条硬时机）。
	if n := tap.count("没接落库面"); n != 2 {
		t.Errorf("两条 claimed 帧各该喊一声没装配齐，实际 %d 句：%v", n, tap.lines)
	}
	// 没带把手的 claimed：不写、不猜，但要说出来。
	hub.handle("k1", 1, process.Inbound{ExecutionID: "ex-a",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnClaimed}})
	if n := tap.count("没带 provider_execution_id"); n != 1 {
		t.Errorf("空把手该被点出来，实际 %d 句：%v", n, tap.lines)
	}

	// 那一笔写必然失败（这台 hub 没接库通道），失败要喊出来、不许静默跳过。
	if n := tap.count("收尾没落成"); n != 1 {
		t.Errorf("泵落不了库时必须出声，实际 %d 句：%v", n, tap.lines)
	}
}
