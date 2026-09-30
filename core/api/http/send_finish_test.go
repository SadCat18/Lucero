package httpapi_test

// 甲（T-370①）的端到端判据：走 handler 那一层，看"一帧 turn/finished 把 Execution 推到终态、
// 并同笔把 Task 投影成 AGENT_COMPLETED"是不是真的发生在生产路径上（域里绿不算，第 273 条同族口径）。
//
// 反例两发（判决记账本）：
//   ① 把 handler 里 `writeTerminal` 恒真/恒假 —— 恒假 ⇒ 终态那一格永不发生；恒真 ⇒ 未裁的码也会去写库；
//   ② 把 tx2 里对 FinishFromTurn 的调用摘掉（只留密封）⇒ 库里出现"投递成功但没有任何收尾记录"的象。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

func finishHarness(t *testing.T, code string) (*httptest.Server, *db.DB, *gateTurn) {
	t.Helper()
	turn := &gateTurn{}
	turn.set(nil, execution.TurnReceipt{Accepted: true,
		Finish: execution.TurnFinish{Seen: true, ResultCode: code}})
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-fin", status: "CREATED", sessionID: "sess-fin", seedSession: true, turn: turn,
	}, true)
	return srv, d, turn
}

// statusOf 从库里读一个状态值（读面那份判据在第 287 条走 HTTP，这里要的是"库里到底是什么"这一格）。
func statusOf(t *testing.T, d *db.DB, q string, args ...any) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func countEvent(t *testing.T, d *db.DB, evType, aggID string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type=? AND aggregate_id=?`,
		evType, aggID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestSendLandsTerminalFromTurnFrame 是 Wave-2 闸门上要的"第一次被观察到"那一格：
// 客户端只发一次 send，Task 却走到 AGENT_COMPLETED —— 驱动它的是帧里的 result_code，不是任何 Core 侧猜测。
func TestSendLandsTerminalFromTurnFrame(t *testing.T) {
	srv, d, turn := finishHarness(t, "SUCCEEDED")

	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-fin/send", "local-user", "k-fin",
		`{"text":"落终态这一格","delivery":"start_now"}`)
	if code != http.StatusAccepted {
		t.Fatalf("send 该 202，实际 %d %s", code, body)
	}
	if st := statusOf(t, d, `SELECT status FROM executions WHERE id='ex-fin'`); st != "SUCCEEDED" {
		t.Errorf("帧里是 SUCCEEDED ⇒ execution 该落成 SUCCEEDED，实际 %s", st)
	}
	if st := statusOf(t, d, `SELECT status FROM tasks WHERE id='task-send'`); st != "WAITING_ACCEPTANCE" {
		t.Errorf("§5.3 投影 AGENT_COMPLETED 之后契约④-c 同笔推 WAITING_ACCEPTANCE，实际 %s（这一步就是闸门要的那次观察）", st)
	}
	if n := countEvent(t, d, "execution_succeeded", "ex-fin"); n != 1 {
		t.Errorf("execution_succeeded 该恰一行，实际 %d", n)
	}
	if n := countEvent(t, d, "task_agent_completed", "task-send"); n != 1 {
		t.Errorf("task_agent_completed 该恰一行，实际 %d", n)
	}
	// 链尾那条（§7 相邻两步）：同笔发生、payload 点得出是哪一次执行。
	if n := countEvent(t, d, "task_waiting_acceptance", "task-send"); n != 1 {
		t.Errorf("task_waiting_acceptance 该恰一行，实际 %d", n)
	}
	var waitExec string
	if err := d.Read.QueryRow(`SELECT json_extract(payload_json,'$.execution_id') FROM event_journal
		WHERE event_type='task_waiting_acceptance' AND aggregate_id='task-send'`).Scan(&waitExec); err != nil {
		t.Fatal(err)
	}
	if waitExec != "ex-fin" {
		t.Errorf("task_waiting_acceptance.payload.execution_id 该是 ex-fin，实际 %q", waitExec)
	}
	if n := countRows(t, d, "messages"); n != 1 {
		t.Errorf("这一趟只该落一行消息，实际 %d", n)
	}
	if n := turn.count(); n != 1 {
		t.Errorf("连接面只该被打一次，实际 %d", n)
	}
	// 响应体仍是 yaml 那四键：收尾码不进对外形状（它是 Core 内部的推进依据）。
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 4 || m["delivery"] != "start_now" || m["accepted"] != true {
		t.Errorf("SendResult 该是 delivery/accepted/message_id/execution_id 四键，实际 %v", m)
	}
	if _, has := m["result_code"]; has {
		t.Errorf("不许把收尾码塞进对外响应（yaml 没有这个键）：%v", m)
	}

	// 同键重放：逐字节等于密封那份，且**一行都不许多**（终态不重写、事件不重发）。
	code2, body2 := call(t, srv, http.MethodPost, "/api/v1/executions/ex-fin/send", "local-user", "k-fin",
		`{"text":"落终态这一格","delivery":"start_now"}`)
	if code2 != http.StatusAccepted || body2 != body {
		t.Errorf("重放该逐字节回放 202，实际 %d %q vs %q", code2, body2, body)
	}
	if n := countEvent(t, d, "execution_succeeded", "ex-fin"); n != 1 {
		t.Errorf("重放后 execution_succeeded 仍该是 1 行，实际 %d", n)
	}
	if n := countEvent(t, d, "task_agent_completed", "task-send"); n != 1 {
		t.Errorf("重放后 task_agent_completed 仍该是 1 行，实际 %d", n)
	}
	if n := countEvent(t, d, "task_waiting_acceptance", "task-send"); n != 1 {
		t.Errorf("重放后 task_waiting_acceptance 仍该是 1 行（第二条链尾事件不许重发），实际 %d", n)
	}
	if n := turn.count(); n != 1 {
		t.Errorf("密封后的重放不许再投一次连接面，实际 %d", n)
	}
}

// TestSendKeepsRunningOnUnmappedTurnResultCode 钉边界一那一手：**收到帧不等于成功**。
// 现仓 fake-adapter 发的正是 "OK"（`cmd/fake-adapter/main.go:474`），它不在取值表里 ⇒
// execution 留 RUNNING、Task 不动、一行终态事件都不发；但投递确实成功了 ⇒ 202 且照常密封。
func TestSendKeepsRunningOnUnmappedTurnResultCode(t *testing.T) {
	srv, d, _ := finishHarness(t, "OK")

	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-fin/send", "local-user", "k-ok",
		`{"text":"帧来了但码没人裁","delivery":"start_now"}`)
	if code != http.StatusAccepted {
		t.Fatalf("投递本身是成功的 ⇒ 该 202，实际 %d %s", code, body)
	}
	if st := statusOf(t, d, `SELECT status FROM executions WHERE id='ex-fin'`); st != "RUNNING" {
		t.Errorf("未裁的 result_code 不许推进 execution（尤其不许折成成功），实际 %s", st)
	}
	if st := statusOf(t, d, `SELECT status FROM tasks WHERE id='task-send'`); st != "RUNNING" {
		t.Errorf("Task 该停在 RUNNING（投影没被驱动），实际 %s", st)
	}
	for _, ev := range []string{"execution_succeeded", "execution_failed", "execution_cancelled", "task_agent_completed"} {
		if n := countEvent(t, d, ev, "ex-fin") + countEvent(t, d, ev, "task-send"); n != 0 {
			t.Errorf("未裁的码不该发任何终态事件，%s 却有 %d 行", ev, n)
		}
	}
	if st, _ := idemRow(t, d, "k-ok"); st != "COMPLETED" {
		t.Errorf("投递成功就该密封（同键下一发不必再投），实际 state=%s", st)
	}
}
