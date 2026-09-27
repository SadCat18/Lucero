package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/project"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// newSendHarness 走真库真表 + 完整装配（ExecutionSend + Store + EnableSend 的两格）。
// seeded 决定夹具里那条 execution 的初始状态与 session 挂接。
// seed.turn 非 nil 时再 EnableTurn（连接面注入变体）。
type sendSeed struct {
	execID      string
	status      string
	sessionID   string // "" = session_id 为 NULL
	taskStatus  string
	taskID      string
	seedSession bool
	turn        execution.TurnCaller // nil = 不接连接面（start_now/guide 应 501）
}

func newSendHarness(t *testing.T, seed sendSeed, wired bool) (*httptest.Server, *db.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := db.InitRedaction([]string{"token"}, []string{secretShape}, redactMarker); err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
		if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, p[0], p[1], p[0]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ad-fake', 'fake', 'Fake', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	taskID := seed.taskID
	if taskID == "" {
		taskID = "task-send"
	}
	taskStatus := seed.taskStatus
	if taskStatus == "" {
		taskStatus = "PREPARING"
	}
	if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, status, created_at, updated_at)
		VALUES (?, 'send 腿', ?, 1, 1)`, taskID, taskStatus); err != nil {
		t.Fatal(err)
	}
	// session_id 非空就必须有父行（这库开了 FK）；seedSession 开关只服务"故意多建一条"的场景。
	if seed.seedSession || seed.sessionID != "" {
		if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id,
			created_at, updated_at) VALUES (?, 'ad-fake', ?, 1, 1)`, seed.sessionID, "remote-"+seed.execID); err != nil {
			t.Fatal(err)
		}
	}
	var sessArg any
	if seed.sessionID != "" {
		sessArg = seed.sessionID
	}
	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id,
		attempt_no, status, state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, ?, 'ad-fake', ?, 1, ?, 'UNKNOWN', 'INITIAL', 1, 1)`,
		seed.execID, taskID, sessArg, seed.status); err != nil {
		t.Fatal(err)
	}

	execSvc := execution.NewService()
	taskSvc := task.NewService(nil)
	sessSvc := session.NewService(session.NewRepository(d.Read))
	if wired {
		execSvc.EnableSend(taskSvc, sessSvc)
		if seed.turn != nil {
			execSvc.EnableTurn(seed.turn)
		}
	}
	cfg := httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            taskSvc,
		Projects:         project.NewService(nil), // 只为 send 那格判据：挂住期间要能另发一发写
		TaskList:         task.NewRepository(d.Read),
		Store:            d,
		Executions:       execution.NewRepository(d.Read),
	}
	if wired {
		cfg.ExecutionSend = execSvc
	}
	srv := httptest.NewServer(httpapi.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv, d
}

const sendBodyOK = `{"text":"继续","delivery":"queue"}`

// TestSendHappyPath 钉 yaml :552 的事件序列与 202 形状：
// execution_starting → execution_started → task_running → message_recorded，状态推进到 RUNNING。
func TestSendHappyPath(t *testing.T) {
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-send", status: "CREATED", sessionID: "sess-send", seedSession: true,
	}, true)

	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-send/send", "local-user", "k-send-1", sendBodyOK)
	if code != http.StatusAccepted {
		t.Fatalf("send 该 202，实际 %d %s", code, body)
	}
	m := mustParseMap(t, body)
	if m["delivery"] != "queue" || m["accepted"] != true {
		t.Errorf("SendResult 该 delivery=queue accepted=true（实际生效态，不回显请求），实际 %v", m)
	}
	if m["message_id"] == nil || m["execution_id"] == nil {
		t.Errorf("message_id / execution_id 该非 null，实际 %v", m)
	}

	// 状态：execution CREATED→RUNNING（v3），task PREPARING→RUNNING。
	var execStatus string
	var execVer int64
	if err := d.Read.QueryRow(`SELECT status, version FROM executions WHERE id='ex-send'`).
		Scan(&execStatus, &execVer); err != nil {
		t.Fatal(err)
	}
	if execStatus != "RUNNING" || execVer != 3 {
		t.Errorf("execution 该 RUNNING v3，实际 %s v%d", execStatus, execVer)
	}
	var taskStatus string
	if err := d.Read.QueryRow(`SELECT status FROM tasks WHERE id='task-send'`).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "RUNNING" {
		t.Errorf("task 该被投影到 RUNNING，实际 %s", taskStatus)
	}
	// 事件序列四行。
	for _, ev := range []string{"execution_starting", "execution_started", "task_running", "message_recorded"} {
		if n := countEventsOfType(t, d, ev); n != 1 {
			t.Errorf("%s 该恰 1 行，实际 %d", ev, n)
		}
	}
	// messages 一行，sequence_no=1，role=USER。
	var seq int64
	var role string
	if err := d.Read.QueryRow(`SELECT sequence_no, role FROM messages WHERE session_id='sess-send'`).
		Scan(&seq, &role); err != nil {
		t.Fatal(err)
	}
	if seq != 1 || role != "USER" {
		t.Errorf("message 该 seq=1 role=USER，实际 seq=%d role=%s", seq, role)
	}
	// 幂等重放：同 key 同 body → 逐字节同响应，副作用零新增。
	if code2, body2 := call(t, srv, http.MethodPost, "/api/v1/executions/ex-send/send",
		"local-user", "k-send-1", sendBodyOK); code2 != http.StatusAccepted || body2 != body {
		t.Errorf("幂等重放该同 202 同体，实际 %d %s", code2, body2)
	}
	if n := countRows(t, d, "messages"); n != 1 {
		t.Errorf("重放不该多落消息，实际 %d 行", n)
	}
	if n := countEventsOfType(t, d, "message_recorded"); n != 1 {
		t.Errorf("重放不该多发事件，实际 %d", n)
	}
}

// TestSendRunningOnlyRecordsMessage：已在 RUNNING 的 execution 只补 message_recorded，
// 不重发 execution_started / task_running。
func TestSendRunningOnlyRecordsMessage(t *testing.T) {
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-run", status: "RUNNING", sessionID: "sess-send", seedSession: true,
		taskStatus: "RUNNING",
	}, true)

	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-run/send", "local-user", "k-r1", sendBodyOK)
	if code != http.StatusAccepted {
		t.Fatalf("RUNNING 上 send 该 202，实际 %d %s", code, body)
	}
	if n := countEventsOfType(t, d, "message_recorded"); n != 1 {
		t.Errorf("message_recorded 该 1，实际 %d", n)
	}
	if n := countEventsOfType(t, d, "execution_started"); n != 0 {
		t.Errorf("RUNNING 上不该重发 execution_started，实际 %d", n)
	}
	if n := countEventsOfType(t, d, "task_running"); n != 0 {
		t.Errorf("RUNNING 上不该重发 task_running，实际 %d", n)
	}
	var execVer int64
	if err := d.Read.QueryRow(`SELECT version FROM executions WHERE id='ex-run'`).Scan(&execVer); err != nil {
		t.Fatal(err)
	}
	if execVer != 1 {
		t.Errorf("RUNNING 上 send 不该动 execution 版本，实际 v%d", execVer)
	}
}

// TestSendGates 钉四条"没说谎"的分工：缺键 403、坏 delivery 400、终态 409、没挂 session 404 session_not_found。
func TestSendGates(t *testing.T) {
	// 缺 Idempotency-Key。
	srv, _ := newSendHarness(t, sendSeed{execID: "ex-g", status: "RUNNING", sessionID: "sess-s", seedSession: true}, true)
	if code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-g/send", "local-user", "", sendBodyOK); code != http.StatusForbidden ||
		codeOf(t, body) != "idempotency_key_required" {
		t.Errorf("缺 Idempotency-Key 该 403 idempotency_key_required，实际 %d %s", code, body)
	}

	// 坏 delivery（不在三枚举）。
	if code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-g/send", "local-user", "k-bad",
		`{"text":"hi","delivery":"telepathy"}`); code != http.StatusBadRequest ||
		codeOf(t, body) != "validation_failed" {
		t.Errorf("坏 delivery 该 400 validation_failed，实际 %d %s", code, body)
	}

	// 未知键（DisallowUnknownFields）。
	if code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-g/send", "local-user", "k-unk",
		`{"text":"hi","delivery":"queue","model_id":"m"}`); code != http.StatusBadRequest ||
		codeOf(t, body) != "validation_failed" {
		t.Errorf("未知键该 400 validation_failed，实际 %d %s", code, body)
	}

	// 终态 409。
	term, _ := newSendHarness(t, sendSeed{execID: "ex-done", status: "SUCCEEDED", sessionID: "sess-s", seedSession: true}, true)
	if code, body := call(t, term, http.MethodPost, "/api/v1/executions/ex-done/send", "local-user", "k-t", sendBodyOK); code != http.StatusConflict ||
		codeOf(t, body) != "illegal_transition" {
		t.Errorf("终态该 409 illegal_transition，实际 %d %s", code, body)
	}

	// 没挂 session（session_id NULL）。
	noSess, _ := newSendHarness(t, sendSeed{execID: "ex-ns", status: "RUNNING", sessionID: ""}, true)
	if code, body := call(t, noSess, http.MethodPost, "/api/v1/executions/ex-ns/send", "local-user", "k-ns", sendBodyOK); code != http.StatusNotFound ||
		codeOf(t, body) != "session_not_found" {
		t.Errorf("无 session 该 404 session_not_found，实际 %d %s", code, body)
	}

	// 执行不存在。
	if code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-nope/send", "local-user", "k-404", sendBodyOK); code != http.StatusNotFound ||
		codeOf(t, body) != "not_found" {
		t.Errorf("不存在的 execution 该 404 not_found，实际 %d %s", code, body)
	}
}

// TestSendMethodAndWiringGates：405（方法不对）、501（没接线）、405（GET item 不是 send）。
func TestSendMethodAndWiringGates(t *testing.T) {
	srv, _ := newSendHarness(t, sendSeed{execID: "ex-m", status: "RUNNING", sessionID: "sess-s", seedSession: true}, true)

	// GET 到 send 路径 → 405。
	if code, body := call(t, srv, http.MethodGet, "/api/v1/executions/ex-m/send", "", "", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("GET send 该 405，实际 %d %s", code, body)
	}
	// GET item（yaml 声明但没实现）→ 501。
	if code, body := call(t, srv, http.MethodGet, "/api/v1/executions/ex-m", "", "", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("GET {id} 该 501 not_implemented，实际 %d %s", code, body)
	}

	// 没接线：ExecutionSend=nil ⇒ 前缀路径不认领 ⇒ catch-all 501。
	bare, _ := newSendHarness(t, sendSeed{execID: "ex-b", status: "RUNNING", sessionID: "sess-s", seedSession: true}, false)
	if code, body := call(t, bare, http.MethodPost, "/api/v1/executions/ex-b/send", "local-user", "k-b", sendBodyOK); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("没接线该 501，实际 %d %s", code, body)
	}
}

// TestSendIdempotencyMismatch：同 key 不同 body → 409 idempotency_mismatch。
func TestSendIdempotencyMismatch(t *testing.T) {
	srv, _ := newSendHarness(t, sendSeed{execID: "ex-mm", status: "RUNNING", sessionID: "sess-s", seedSession: true}, true)
	if code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-mm/send", "local-user", "k-mm", sendBodyOK); code != http.StatusAccepted {
		t.Fatalf("首击该 202，实际 %d %s", code, body)
	}
	if code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-mm/send", "local-user", "k-mm",
		`{"text":"另一句","delivery":"queue"}`); code != http.StatusConflict ||
		codeOf(t, body) != "idempotency_mismatch" {
		t.Errorf("同 key 不同 body 该 409 idempotency_mismatch，实际 %d %s", code, body)
	}
}

// TestQueueCopiesSessionIDFromLinks 钉 Step A：links 已有则抄进 executions.session_id；
// 没有 links 则留 NULL，不发明。
func TestQueueCopiesSessionIDFromLinks(t *testing.T) {
	srv, d := newTaskHarness(t)
	// 建任务。
	code, body := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-q1",
		`{"title":"带 session 的 queue"}`)
	if code != http.StatusCreated {
		t.Fatalf("建任务该 201，实际 %d %s", code, body)
	}
	var created taskJSON
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	// 无 links：queue 后 session_id 应为 NULL。
	if code, body = call(t, srv, http.MethodPost, "/api/v1/tasks/"+created.ID+":queue",
		"local-user", "k-q1", queueBody); code != http.StatusOK {
		t.Fatalf("queue 该 200，实际 %d %s", code, body)
	}
	var sessNull bool
	if err := d.Read.QueryRow(`SELECT session_id IS NULL FROM executions WHERE task_id=?`, created.ID).
		Scan(&sessNull); err != nil {
		t.Fatal(err)
	}
	if !sessNull {
		t.Errorf("没有 links 时 session_id 该 NULL，实际非 NULL")
	}

	// 有 links：再建一个任务 + session + link，queue 后 session_id 应抄进去。
	code, body = call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-q2",
		`{"title":"带 session 的 queue 2"}`)
	if code != http.StatusCreated {
		t.Fatalf("建任务该 201，实际 %d %s", code, body)
	}
	var created2 taskJSON
	if err := json.Unmarshal([]byte(body), &created2); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id,
		created_at, updated_at) VALUES ('sess-q2', 'ad-fake', 'remote-q2', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO task_session_links (id, task_id, session_id, role, ordinal, created_at)
		VALUES ('lnk-1', ?, 'sess-q2', 'ORIGIN', 0, 1)`, created2.ID); err != nil {
		t.Fatal(err)
	}
	if code, body = call(t, srv, http.MethodPost, "/api/v1/tasks/"+created2.ID+":queue",
		"local-user", "k-q2", queueBody); code != http.StatusOK {
		t.Fatalf("queue 该 200，实际 %d %s", code, body)
	}
	var sessID string
	if err := d.Read.QueryRow(`SELECT session_id FROM executions WHERE task_id=?`, created2.ID).
		Scan(&sessID); err != nil {
		t.Fatal(err)
	}
	if sessID != "sess-q2" {
		t.Errorf("有 links 时 session_id 该抄 sess-q2，实际 %q", sessID)
	}
}

// TestSendTaskNotPreparingIs409：execution CREATED、task 不在 PREPARING（EdgeStarted 被拒）
// ⇒ 整笔回滚、409 illegal_transition —— 不是 202 假成功，也不是 500 装配错。
func TestSendTaskNotPreparingIs409(t *testing.T) {
	// nil reporter（本 harness 的 task.NewService(nil)）：走 ErrProjectionRejected 哨兵。
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-np", status: "CREATED", sessionID: "sess-np",
		taskStatus: "RUNNING", taskID: "task-np",
	}, true)
	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-np/send", "local-user", "k-np", sendBodyOK)
	if code != http.StatusConflict || codeOf(t, body) != "illegal_transition" {
		t.Fatalf("task 不在 PREPARING 该 409 illegal_transition，实际 %d %s", code, body)
	}
	// 整笔回滚：execution 仍 CREATED、无新事件、无 message。
	var st string
	var ver int64
	if err := d.Read.QueryRow(`SELECT status, version FROM executions WHERE id='ex-np'`).Scan(&st, &ver); err != nil {
		t.Fatal(err)
	}
	if st != "CREATED" || ver != 1 {
		t.Errorf("被拒的 send 不该动 execution，实际 %s v%d", st, ver)
	}
	if n := countRows(t, d, "messages"); n != 0 {
		t.Errorf("被拒的 send 不该落 message，实际 %d 行", n)
	}
	if n := countEventsOfType(t, d, "execution_starting"); n != 0 {
		t.Errorf("被拒的 send 不该写事件，实际 execution_starting %d", n)
	}
	// task 原样。
	var tsk string
	if err := d.Read.QueryRow(`SELECT status FROM tasks WHERE id='task-np'`).Scan(&tsk); err != nil {
		t.Fatal(err)
	}
	if tsk != "RUNNING" {
		t.Errorf("被拒的 send 不该动 task，实际 %s", tsk)
	}
}

// TestSendBodyShapeInResponse：sendResultView 的键集与 yaml SendResult.required 对齐。
func TestSendBodyShapeInResponse(t *testing.T) {
	srv, _ := newSendHarness(t, sendSeed{
		execID: "ex-shape", status: "RUNNING", sessionID: "sess-s", seedSession: true,
	}, true)
	_, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-shape/send", "local-user", "k-sh", sendBodyOK)
	m := mustParseMap(t, body)
	for _, k := range []string{"delivery", "accepted", "message_id", "execution_id"} {
		if _, ok := m[k]; !ok {
			t.Errorf("SendResult 缺键 %s：%v", k, m)
		}
	}
	// delivery 必须是三枚举之一（实际生效态 = queue）。
	if d, _ := m["delivery"].(string); !strings.Contains("start_now guide queue", d) {
		t.Errorf("delivery %q 不在三枚举", d)
	}
}

// stubTurn 是 TurnCaller 的测试替身：记录调用、按脚本回回执或错误。
type stubTurn struct {
	rec   execution.TurnReceipt
	err   error
	calls []execution.TurnCall
}

func (s *stubTurn) CallSendTurn(_ context.Context, call execution.TurnCall) (execution.TurnReceipt, error) {
	s.calls = append(s.calls, call)
	return s.rec, s.err
}

// TestSendStartNowUnwiredIs501 钉 C28②：start_now 要连接面，EnableTurn 没调过 ⇒ 501 not_implemented，
// 不静默降级成 queue（yaml :554 禁的正是"报告按你说的做了"）。queue 照常 202（它不走连接面）。
func TestSendStartNowUnwiredIs501(t *testing.T) {
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-unwired", status: "RUNNING", sessionID: "sess-u", seedSession: true,
	}, true) // wired=true 但 seed.turn=nil ⇒ EnableSend 有、EnableTurn 无

	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-unwired/send",
		"local-user", "k-un", `{"text":"hi","delivery":"start_now"}`)
	if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
		t.Errorf("未接连接面的 start_now 该 501 not_implemented，实际 %d %s", code, body)
	}
	// 整笔回滚：一条消息都没落。
	if n := countRows(t, d, "messages"); n != 0 {
		t.Errorf("501 的 send 不该落 message，实际 %d 行", n)
	}

	// 同一 harness 上 queue 照常 202。
	if code2, body2 := call(t, srv, http.MethodPost, "/api/v1/executions/ex-unwired/send",
		"local-user", "k-q", sendBodyOK); code2 != http.StatusAccepted {
		t.Errorf("queue 不走连接面，该 202，实际 %d %s", code2, body2)
	}
}

// TestSendTurnWiredStartNowAccepted 钉接上线后的 202 形状：delivery 回请求态 start_now、
// accepted=true、IdempotencyKey 透传给连接面。
func TestSendTurnWiredStartNowAccepted(t *testing.T) {
	turn := &stubTurn{rec: execution.TurnReceipt{Accepted: true}}
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-now", status: "RUNNING", sessionID: "sess-n", seedSession: true, turn: turn,
	}, true)

	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-now/send",
		"local-user", "k-now", `{"text":"立刻跑","delivery":"start_now"}`)
	if code != http.StatusAccepted {
		t.Fatalf("接线后的 start_now 该 202，实际 %d %s", code, body)
	}
	m := mustParseMap(t, body)
	if m["delivery"] != "start_now" || m["accepted"] != true {
		t.Errorf("该 delivery=start_now accepted=true（实际生效态），实际 %v", m)
	}
	if len(turn.calls) != 1 {
		t.Fatalf("该恰调一次连接面，实际 %d 次", len(turn.calls))
	}
	got := turn.calls[0]
	if got.ExecutionID != "ex-now" || got.SessionID != "sess-n" || got.Delivery != "start_now" {
		t.Errorf("TurnCall 形状不对: %+v", got)
	}
	if got.IdempotencyKey != "k-now" {
		t.Errorf("Idempotency-Key 该透传，实际 %q", got.IdempotencyKey)
	}
	// 消息已落。
	if n := countRows(t, d, "messages"); n != 1 {
		t.Errorf("start_now 成功该落 1 条 message，实际 %d", n)
	}
}

// TestSendTurnAcceptedFalseStill202 钉 accepted=false 的口径：仍 202 落库，
// delivery 保持请求态（不改写成 queue —— 改写才是 §5 静默降级），accepted=false。
func TestSendTurnAcceptedFalseStill202(t *testing.T) {
	turn := &stubTurn{rec: execution.TurnReceipt{Accepted: false, Reason: "provider busy"}}
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-rej", status: "RUNNING", sessionID: "sess-r", seedSession: true, turn: turn,
	}, true)

	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-rej/send",
		"local-user", "k-rej", `{"text":"插一句","delivery":"guide"}`)
	if code != http.StatusAccepted {
		t.Fatalf("accepted=false 仍该 202，实际 %d %s", code, body)
	}
	m := mustParseMap(t, body)
	if m["delivery"] != "guide" {
		t.Errorf("delivery 该保持请求态 guide（不改写成 queue），实际 %v", m["delivery"])
	}
	if m["accepted"] != false {
		t.Errorf("accepted 该 false，实际 %v", m["accepted"])
	}
	if n := countRows(t, d, "messages"); n != 1 {
		t.Errorf("accepted=false 仍该落 message（已记账），实际 %d 行", n)
	}
}

// TestSendTurnAdapterErrorMaps 钉 §15.2 表的三行经 probeErrorOf 上墙：
// delivery_mode_unsupported→422、session_not_found→404（Adapter 侧）、provider_error→502；
// 以及 turn 未装配的另一个入口 ErrAdapterNotLaunchable→501。
// 连接丢失类（非 *adapter.Error）归一成 adapter_unresponsive→503。
func TestSendTurnAdapterErrorMaps(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{"delivery_mode_unsupported", &adapter.Error{Code: adapter.ErrDeliveryModeUnsupported, Message: "nope"},
			http.StatusUnprocessableEntity, "delivery_mode_unsupported"},
		{"session_not_found", &adapter.Error{Code: -32006, Message: "gone"},
			http.StatusNotFound, "session_not_found"},
		{"provider_error", &adapter.Error{Code: -32012, Message: "down"},
			http.StatusBadGateway, "provider_error"},
		{"not_launchable", fmt.Errorf("%w: fake", httpapi.ErrAdapterNotLaunchable),
			http.StatusNotImplemented, "not_implemented"},
		{"unresponsive", &adapter.Error{Code: adapter.ErrAdapterUnresponsive, Message: "dead"},
			http.StatusServiceUnavailable, "adapter_unresponsive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turn := &stubTurn{err: tc.err}
			srv, _ := newSendHarness(t, sendSeed{
				execID: "ex-e", status: "RUNNING", sessionID: "sess-e", seedSession: true, turn: turn,
			}, true)
			code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-e/send",
				"local-user", "k-e", `{"text":"hi","delivery":"start_now"}`)
			if code != tc.want || codeOf(t, body) != tc.code {
				t.Errorf("%s 该 %d %s，实际 %d %s", tc.name, tc.want, tc.code, code, body)
			}
		})
	}
}

// TestSendStatusGuardBeforeTurn 钉 409 先于 501：终态上打 start_now，
// 状态机拒绝比"能力没接线"更该先答（即使连接面也没接）。
func TestSendStatusGuardBeforeTurn(t *testing.T) {
	srv, _ := newSendHarness(t, sendSeed{
		execID: "ex-term", status: "SUCCEEDED", sessionID: "sess-t", seedSession: true,
		// turn=nil：若顺序错了会先撞 501 而不是 409。
	}, true)
	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-term/send",
		"local-user", "k-t2", `{"text":"hi","delivery":"start_now"}`)
	if code != http.StatusConflict || codeOf(t, body) != "illegal_transition" {
		t.Errorf("终态 + start_now 该 409（先于 501），实际 %d %s", code, body)
	}
}
