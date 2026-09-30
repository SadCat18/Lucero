package httpapi_test

// send 的**回执那一拍**长什么样（REQ-TURN-1 裁「Ⅰ」之后的分工，判据 (c) 零外溢的 handler 面）。
//
// 本文件的判据本体上一批叫 `TestSendLandsTerminalFromTurnFrame`（甲，T-370①）：它让 handler 在第二笔里
// 按回执带回的收尾帧写终态。「Ⅰ」之后那件事不归 handler 管了 —— 收尾帧由 `cmd/core` 的常驻入站泵收，
// 终态与 §5.3 投影在泵自己那一笔短事务里写，`TurnReceipt` 里那个字段已删（`internal/execution/send.go`
// 的注释记着为什么）。所以：
//   - **搬走的那几条**（帧 ⇒ 终态成套、未裁的码只喊不写、同帧重放不重发）归
//     `cmd/core/turnpump_test.go`（判决表）与 `cmd/core/turnpump_late_test.go`（真进程 + 迟到帧）；
//     T-381③ⅰ 那句"把 `TestSendLandsTerminalFromTurnFrame` 的替身帧换成**真进程那一跑**"随本批落地，
//     接住它的就是后面那一条用例。
//   - **留在这里的**是判据 (c) 点名的三样里 handler 管得到的那两样：`SendResult` 四键与 202 语义
//     （§15.2 映射与 CO-25 排水点不在这一层）；外加一条新的正面形状 ——
//     **投递这一拍自己不写终态**，所以 202 之后库里那一行必须还是 RUNNING。
//     这一条是有牙的：谁把"按帧写终态"塞回 handler，它当场红。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// shapeHarness 装 send 那条链的完整装配（真库真表 + 连接面替身）。替身只回"接没接"，不带收尾证据
// ——「Ⅰ」之后回执上压根没有收尾证据这个字段，替身想带也带不出来。
// 执行号由调用方给（本文件的用例与 `send_columns_test.go` 各用各的行名，共用同一套装配）。
func shapeHarness(t *testing.T, execID string) (*httptest.Server, *db.DB, *gateTurn) {
	t.Helper()
	turn := &gateTurn{}
	turn.set(nil, execution.TurnReceipt{Accepted: true})
	srv, d := newSendHarness(t, sendSeed{
		execID: execID, status: "CREATED", sessionID: "sess-" + execID, seedSession: true, turn: turn,
	}, true)
	return srv, d, turn
}

// driveFrame 代打"泵收到一帧 `turn/finished` 之后那一笔"（同 `accept_chain_test.go` 的理由：
// 真做这件事的是 cmd/core 的泵，本包 import 不到装配层）。
// 调用的是同一个导出动作、同一组三个事件身份、同一个 `system` 主体 ⇒ 代的是"谁触发"，不代"那一笔长什么样"。
func driveFrame(t *testing.T, d *db.DB, execID, resultCode string) {
	t.Helper()
	svc := execution.NewService()
	svc.EnableSend(task.NewService(nil), session.NewService(session.NewRepository(d.Read)))
	ctx := context.Background()
	mint := func() string { return adapter.NewRequestID().String() } // 与 cmd/core 的泵同一个铸法
	correlation := mint()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, _, err := svc.FinishFromTurn(ctx, tx, execID, resultCode, execution.Command{
			EventID: mint(), TaskEventID: mint(), TaskWaitingAcceptanceEventID: mint(),
			PrincipalID: "system", CorrelationID: correlation, CausationID: correlation,
		})
		return err
	}); err != nil {
		t.Fatalf("代泵打那一笔收尾（result_code=%s）失败：%v", resultCode, err)
	}
}

// statusOf 从库里读一个状态值（读面判据走 HTTP，这里要的是"库里到底是什么"那一格）。
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

// TestSendReceiptSealsButTerminalIsThePumps 是判据 (c) 里 handler 那两样 + 「Ⅰ」的分工本身。
func TestSendReceiptSealsButTerminalIsThePumps(t *testing.T) {
	srv, d, turn := shapeHarness(t, "ex-shape")

	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-shape/send", "local-user", "k-shape",
		`{"text":"回执那一拍","delivery":"start_now"}`)
	if code != http.StatusAccepted {
		t.Fatalf("send 该 202，实际 %d %s", code, body)
	}

	// 对外形状：yaml `SendResult` 那四键，一个不多一个不少；收尾码不许出现在响应里。
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

	// 「Ⅰ」的分工：投递这一拍只负责"送到 + 密封"，**不写终态**。
	if st := statusOf(t, d, `SELECT status FROM executions WHERE id='ex-shape'`); st != "RUNNING" {
		t.Errorf("终态归泵写 ⇒ handler 这一拍之后库里该还是 RUNNING，实际 %s（写成终态就是把两条落库路径并回一条）", st)
	}
	for _, ev := range []string{"execution_succeeded", "execution_failed", "execution_cancelled", "task_agent_completed"} {
		if n := countEvent(t, d, ev, "ex-shape") + countEvent(t, d, ev, "task-send"); n != 0 {
			t.Errorf("投递这一拍不该发任何终态事件，%s 却有 %d 行", ev, n)
		}
	}
	if n := countRows(t, d, "messages"); n != 1 {
		t.Errorf("这一趟只该落一行消息，实际 %d", n)
	}
	if n := turn.count(); n != 1 {
		t.Errorf("连接面只该被打一次，实际 %d", n)
	}
	if st, _ := idemRow(t, d, "k-shape"); st != "COMPLETED" {
		t.Errorf("投递成功就该密封（同键下一发不必再投），实际 state=%s", st)
	}

	// 同键重放：逐字节等于密封那份，且一行都不许多（泵那一腿与这里无关，重放不会再投连接面）。
	code2, body2 := call(t, srv, http.MethodPost, "/api/v1/executions/ex-shape/send", "local-user", "k-shape",
		`{"text":"回执那一拍","delivery":"start_now"}`)
	if code2 != http.StatusAccepted || body2 != body {
		t.Errorf("重放该逐字节回放 202，实际 %d %q vs %q", code2, body2, body)
	}
	if n := turn.count(); n != 1 {
		t.Errorf("密封后的重放不许再投一次连接面，实际 %d", n)
	}
	if st := statusOf(t, d, `SELECT status FROM executions WHERE id='ex-shape'`); st != "RUNNING" {
		t.Errorf("重放也不该动状态，实际 %s", st)
	}

	// 代泵补那一帧 ⇒ 终态与链尾从**另一笔事务**落进来，密封与响应形状一行都没被它动过。
	driveFrame(t, d, "ex-shape", "SUCCEEDED")
	if st := statusOf(t, d, `SELECT status FROM executions WHERE id='ex-shape'`); st != "SUCCEEDED" {
		t.Errorf("收尾那一笔该把 execution 落成 SUCCEEDED，实际 %s", st)
	}
	if _, body3 := call(t, srv, http.MethodPost, "/api/v1/executions/ex-shape/send", "local-user", "k-shape",
		`{"text":"回执那一拍","delivery":"start_now"}`); body3 != body {
		t.Errorf("泵落了终态之后，重放的响应仍该逐字节等于密封那份（回放的是当时那份答案）：\n%s\n%s", body, body3)
	}
}
