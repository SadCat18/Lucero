package main

// 常驻入站泵的判据（REQ-TURN-1 裁「Ⅰ」，契约① 第 25 条 ④-b）。
//
// 这一文件覆盖的是**泵的判决表**：一帧进来，写什么、喊什么、什么都不做，三种结局互不重叠。
// 它喂的是自己造的 `process.Inbound`（`*process.Connection` 我造不出来 —— 那是 adapter 的包，
// 而 `<-chan process.Inbound` 我造得出来），所以这一层有牙；**接线**那一层（真子进程、真连接、
// 回执之后才到的帧）在 `turnpump_late_test.go`，两条用例分工写死在那里，别拿一边冒充另一边。
//
// 上一形状（甲，T-370①）的同一块判据住在 `sendreap_test.go`，那张表里第一行是"根本没收到帧 ⇒ 不喊"。
// 「Ⅰ」之后那一行从判决表里消失了：泵是逐帧工作的，"还没到"不是它的输入，只是一个还没发生的时刻。
// 这正这条裁定要修的东西（契约原文："真 adapter 的收尾帧几乎必然后到"）。

import (
	"errors"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
)

func finishedFrame(code string) *adapter.StreamFrame {
	return &adapter.StreamFrame{Method: adapter.MethodTurnFinished, ResultCode: code}
}

// pumpFixture 装一条泵（真库、真 `execution.Service` + task 投影器，但没有真子进程）。
// logf 换成收集器：泵那几种结局里"只喊不写"的那一种**只能从日志看见**，不收集就断不到。
func pumpFixture(t *testing.T) (*turnHub, *logTap, *db.DB) {
	t.Helper()
	_, _, pipe, d := newBareDiscover(t,
		[]adapterSpec{{key: "fake", displayName: "fake", command: "用不上：泵的用例不真起子进程"}})
	tap := &logTap{}
	pipe.hub.logf = tap.f
	return pipe.hub, tap, d
}

// pumpSeed 播一张实例 + 一条会话 + 一个 RUNNING 的执行（挂在 RUNNING 的任务上）。
// 任务状态给参数是因为判决表里有一格要的正是"投影被拒"那一种。
func pumpSeed(t *testing.T, d *db.DB, execID, taskID, taskStatus string) {
	t.Helper()
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Read.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	must(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ai-pump', 'fake', 'fake', 1, 1)`)
	must(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, created_at, updated_at)
		VALUES ('sess-pump', 'ai-pump', 'remote-pump', 1, 1)`)
	must(`INSERT INTO tasks (id, title, status, created_at, updated_at) VALUES (?, '泵的这一格', ?, 1, 1)`,
		taskID, taskStatus)
	must(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, ?, 'ai-pump', 'sess-pump', 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`, execID, taskID)
}

func pumpStatus(t *testing.T, d *db.DB, q string, args ...any) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func eventsOf(t *testing.T, d *db.DB, typ string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type=?`, typ).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestPumpWritesOnlyTheTerminalFrame 是那块"**只收**"整搬进来后的形状：混序喂五种帧，
// 只有"执行号对得上的 `turn/finished`"这一种会写库。
// 反例（判决记账本）：把 `finishOf` 里 `IsFinished()` 那道筛摘掉 ⇒ delta 会被当成收尾码写进终态。
func TestPumpWritesOnlyTheTerminalFrame(t *testing.T) {
	hub, tap, d := pumpFixture(t)
	pumpSeed(t, d, "ex-p1", "task-p1", "RUNNING")
	before := pumpStatus(t, d, `SELECT status FROM executions WHERE id='ex-p1'`)
	rowsBefore := eventsOf(t, d, "execution_succeeded") + eventsOf(t, d, "task_agent_completed")

	// 顺序有意混：别人那条执行的收尾、进程死了的一声、一条 delta、归不到人的收尾、我们的收尾。
	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-other", Frame: finishedFrame("SUCCEEDED")})
	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-p1", Err: errors.New("no reply")})
	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-p1",
		Frame: &adapter.StreamFrame{Method: adapter.MethodStreamDelta, Text: "半句"}})
	hub.handle("fake", 1, process.Inbound{Frame: finishedFrame("SUCCEEDED")}) // 空执行号：不许替别人猜身份
	if st := pumpStatus(t, d, `SELECT status FROM executions WHERE id='ex-p1'`); st != before {
		t.Errorf("上面四种帧都不是这次执行的收尾，状态却从 %s 变 %s", before, st)
	}
	if n := tap.count("落终态") + tap.count("未裁"); n != 0 {
		t.Errorf("都不是收尾就不该有任何声音，实际 %d 句：%v", n, tap.lines)
	}

	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-p1", Frame: finishedFrame("SUCCEEDED")})
	if st := pumpStatus(t, d, `SELECT status FROM executions WHERE id='ex-p1'`); st != "SUCCEEDED" {
		t.Errorf("执行号对得上的收尾帧该把这一条落成 SUCCEEDED，实际 %s", st)
	}
	if n := eventsOf(t, d, "execution_succeeded") + eventsOf(t, d, "task_agent_completed"); n <= rowsBefore {
		t.Errorf("落终态必须同笔发事件（%d 行，之前 %d）⇒ 只改状态不发事件就是把 §6 那一格丢了", n, rowsBefore)
	}
	if n := tap.count("迟到收尾已落终态"); n != 1 {
		t.Errorf("落成之后该恰一声，实际 %d 句：%v", n, tap.lines)
	}
}

// TestPumpRefusesResultCodesNobodyRuled 是判决表第二行（契约② §6 第 6 条推论①：收到帧 ≠ 成功）。
// 表外的码 ⇒ **一行都不写**，只喊；这三格是"边界一"唯一能行为可见的形状。
func TestPumpRefusesResultCodesNobodyRuled(t *testing.T) {
	for _, code := range []string{"OK", "INTERRUPTED", ""} {
		t.Run("表外的码 "+code, func(t *testing.T) {
			hub, tap, d := pumpFixture(t)
			pumpSeed(t, d, "ex-p2", "task-p2", "RUNNING")
			hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-p2", Frame: finishedFrame(code)})

			if st := pumpStatus(t, d, `SELECT status FROM executions WHERE id='ex-p2'`); st != "RUNNING" {
				t.Errorf("码 %q 没人裁过 ⇒ 一行都不许多，实际状态 %s（写成终态就是伪造）", code, st)
			}
			if n := eventsOf(t, d, "execution_succeeded") + eventsOf(t, d, "execution_failed") +
				eventsOf(t, d, "task_agent_completed"); n != 0 {
				t.Errorf("表外的码不许留下任何终态事件，实际 %d 行", n)
			}
			if n := tap.count("未裁"); n != 1 {
				t.Errorf("这一格唯一出口就是那一声喊，实际 %d 句：%v", n, tap.lines)
			}
			if !strings.Contains(strings.Join(tap.lines, "\n"), code) {
				t.Errorf("喊里要能看见那个码本身（否则事后无从判断 producer 发了什么）：%v", tap.lines)
			}
		})
	}
}

// TestPumpReplayOfSameFrameIsNotSecondTerminal 是判决表第三行：同一份收尾到两次（重放、
// 或两条腿都收到了）⇒ 第二笔不写、不再发事件，只喊"不重复发事件"。
// 这一格反证的是"泵按帧数推进"那种写法：那样第二帧会把 execution 推成非法转移（409/回滚）。
func TestPumpReplayOfSameFrameIsNotSecondTerminal(t *testing.T) {
	hub, tap, d := pumpFixture(t)
	pumpSeed(t, d, "ex-p3", "task-p3", "RUNNING")
	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-p3", Frame: finishedFrame("CANCELLED")})
	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-p3", Frame: finishedFrame("CANCELLED")})

	if st := pumpStatus(t, d, `SELECT status FROM executions WHERE id='ex-p3'`); st != "CANCELLED" {
		t.Errorf("两帧同码该还是 CANCELLED，实际 %s", st)
	}
	if n := eventsOf(t, d, "execution_cancelled"); n != 1 {
		t.Errorf("同帧重放该只发一条 execution_cancelled，实际 %d 条", n)
	}
	if n := tap.count("不重复发事件"); n != 1 {
		t.Errorf("第二帧该被认成重放并喊一声，实际 %d 句：%v", n, tap.lines)
	}
}

// TestPumpLeavesTheRowAloneWhenProjectionIsRejected 钉"喊了但不写"那一格：task 不在 §5.3 要求的
// 起始态 ⇒ `FinishFromTurn` 整笔回滚，泵**不许**把"这一轮失败了"自己补成一个终态。
// 边界（T-391⑤）"不许 Core 在回执后替 Provider 编一个终态出来"在这条上最容易被违反：
// 落库失败时随手写 FAILED 是最顺手的做法，而那正是禁的。
func TestPumpLeavesTheRowAloneWhenProjectionIsRejected(t *testing.T) {
	hub, tap, d := pumpFixture(t)
	// 任务已经 ACCEPTED：§5.3 的 succeeded 边从这里不合法 ⇒ 投影被拒 ⇒ 整笔回滚。
	pumpSeed(t, d, "ex-p4", "task-p4", "ACCEPTED")
	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-p4", Frame: finishedFrame("SUCCEEDED")})

	if st := pumpStatus(t, d, `SELECT status FROM executions WHERE id='ex-p4'`); st != "RUNNING" {
		t.Errorf("投影被拒时 execution 该一行都不动（整笔回滚），实际 %s", st)
	}
	if n := eventsOf(t, d, "execution_succeeded"); n != 0 {
		t.Errorf("回滚了就不该留下 execution_succeeded，实际 %d 条", n)
	}
	if n := tap.count("没落成"); n != 1 {
		t.Errorf("这一格唯一正确出口是那一声喊（CO-44 同族：0 行/被拒不许静默），实际 %d 句：%v", n, tap.lines)
	}
}

// TestPumpInventsNothingWhenNoFrameArrives 是"不 sleep、不轮询、不编终态"三条边界的正面形状：
// 泵收不到帧就只是**没做事** —— 没有等待、没有超时折成的状态、没有替 Provider 写的结论。
// 上一形状里这一格是"扫一遍缓冲、没帧 ⇒ 决定不写终态"（一个时刻性的判断）；「Ⅰ」把它变成
// "还没有这件事"（一个还没发生的事件），这正是裁 Ⅰ 的全部理由。
func TestPumpInventsNothingWhenNoFrameArrives(t *testing.T) {
	// 这一格不需要泵动起来：它要断的正是"没有帧进来时，库与日志都是空的"。
	_, tap, d := pumpFixture(t)
	pumpSeed(t, d, "ex-p5", "task-p5", "RUNNING")

	// 泵没收到任何东西（这里不调 handle 就是它的常态：`Inbound()` 上没帧）。
	if st := pumpStatus(t, d, `SELECT status FROM executions WHERE id='ex-p5'`); st != "RUNNING" {
		t.Errorf("没有帧就不该有任何推进，实际 %s", st)
	}
	if n := pumpStatus(t, d, `SELECT status FROM tasks WHERE id='task-p5'`); n != "RUNNING" {
		t.Errorf("task 同样不许被动到，实际 %s", n)
	}
	if len(tap.lines) != 0 {
		t.Errorf("没发生任何事就不该有任何声音：%v", tap.lines)
	}
	// UNKNOWN 是恢复那两条腿的（REQ-RC-1/RC-2），本批同步路径上一格都不许造出来。
	if eventsOf(t, d, "execution_unknown") != 0 {
		t.Errorf("泵不许写 UNKNOWN（T-391⑤：同步路径不造 UNKNOWN）")
	}
}
