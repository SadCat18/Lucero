package httpapi_test

// §61 垂直链的最后一格：`Task → ACCEPTED` 今天停在哪、由谁推（契约④-c 裁「甲」之后的答案）。
//
//	格一（链通）：一帧 SUCCEEDED 把 execution 推到终态 ⇒ task 投影 AGENT_COMPLETED 之后**同一笔事务**
//	       继续落 `WAITING_ACCEPTANCE`（§7 那对相邻事件 task_agent_completed → task_waiting_acceptance
//	       都在，后者 payload 点得出 execution_id），随后 `POST /tasks/{id}:accept` ⇒ 200 真落 `ACCEPTED`。
//	格二（错态）：帧里是 FAILED ⇒ task 落 `FAILED`，**半格不发生** —— 既不推 WAITING_ACCEPTANCE 也不发
//	       那条事件；这时 `:accept` 必须 409 且一行事件都不写。"错态不推"与"错态不验收"一起钉。
//	格三（终态）：`ACCEPTED` 之后再 `:accept` ⇒ 409 且事件不增（矩阵里 Accepted 没有出边）。
//
// 历史（为什么这一格原来靠手工摆态）：旧判据是「先 `UPDATE tasks SET status='WAITING_ACCEPTANCE'`
// 再 `:accept`」，那把"链跑通"偷换成了"末端那一跳跑通" —— 缺的是 `AGENT_COMPLETED → WAITING_ACCEPTANCE`
// 那一步的驱动方，而驱动方补上之后**状态是 send 自己走出来的**，手工 UPDATE 已删（契约④-c 的判据本体）。
// **本批（T-391「Ⅰ」）改的是"谁触发那一笔收尾"**：以前是 handler 在第二笔里按回执带来的帧写，
// 现在收归 `cmd/core` 的常驻入站泵 ⇒ 本包只能代打那一笔（见 `driveFrame`，在 `send_finish_test.go`），
// 三条格子的判据本体（状态是走出来的、不是 UPDATE 摆的）一字未动。
// 函数名 `TestChainStopsBeforeAcceptanceNotAtAccept` 是 W2-8 闸门与契约④-c 按名字点的锚，**保留不动**；
// 旧的"甲格/乙格"分法随裁作废（甲格的前提 AGENT_COMPLETED 不再是可观测的停留态，它成了同一笔事务里的中间态）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// chainHarness 用 send 那套完整装配（真库真表 + 连接面替身）。
// 替身回执**不再带收尾证据**：REQ-TURN-1 裁「Ⅰ」之后 handler 不看帧、也不写终态，
// `TurnReceipt` 里那个字段已删（`internal/execution/send.go` 的注释记着为什么）。
func chainHarness(t *testing.T) (*httptest.Server, *db.DB) {
	t.Helper()
	turn := &gateTurn{}
	turn.set(nil, execution.TurnReceipt{Accepted: true})
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-chain", status: "CREATED", sessionID: "sess-chain", seedSession: true, turn: turn,
	}, true)
	return srv, d
}

// chainSend 打那一发 start_now，只认 202（投递那一拍到此为止，链的下一步不由它负责）。
func chainSend(t *testing.T, srv *httptest.Server) {
	t.Helper()
	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-chain/send", "local-user", "k-chain",
		`{"text":"把链推到链尾","delivery":"start_now"}`)
	if code != http.StatusAccepted {
		t.Fatalf("send 该 202，实际 %d %s", code, body)
	}
}

func TestChainStopsBeforeAcceptanceNotAtAccept(t *testing.T) {
	// ── 格一：send 之后**自然**落 WAITING_ACCEPTANCE（这一格以前是手工 UPDATE 摆出来的）──
	srv, d := chainHarness(t)
	chainSend(t, srv)
	driveFrame(t, d, "ex-chain", "SUCCEEDED")

	if st := chainTaskStatus(t, d, "task-send"); st != "WAITING_ACCEPTANCE" {
		t.Fatalf("投影落定 AGENT_COMPLETED 后该同笔推到 WAITING_ACCEPTANCE，实际 %s（前提没成立，下面的 200 是空转）", st)
	}
	if st, _ := chainExecStatus(t, d, "ex-chain"); st != "SUCCEEDED" {
		t.Fatalf("execution 该落 SUCCEEDED，实际 %s", st)
	}
	// §7 相邻两步都在，且链尾那条点得出是哪一次执行。
	if n := chainEventRows(t, d, "task_agent_completed"); n != 1 {
		t.Errorf("task_agent_completed 该恰一行，实际 %d", n)
	}
	if n := chainEventRows(t, d, "task_waiting_acceptance"); n != 1 {
		t.Errorf("task_waiting_acceptance 该恰一行，实际 %d", n)
	}
	if ex := chainPayload(t, d, "task_waiting_acceptance", "execution_id"); ex != "ex-chain" {
		t.Errorf("task_waiting_acceptance.payload.execution_id 该是 ex-chain，实际 %q", ex)
	}

	aCode, aBody := call(t, srv, http.MethodPost, "/api/v1/tasks/task-send:accept", "local-user", "", "")
	if aCode != http.StatusOK {
		t.Fatalf("WAITING_ACCEPTANCE 上 `:accept` 该 200，实际 %d %s —— 那说明「停点」判断错了，要重查", aCode, aBody)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(aBody), &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ACCEPTED" {
		t.Errorf("答案里的状态该是 ACCEPTED，实际 %v", out)
	}
	if st := chainTaskStatus(t, d, "task-send"); st != "ACCEPTED" {
		t.Errorf("库里该落成 ACCEPTED，实际 %s", st)
	}
	if n := chainEventRows(t, d, "task_accepted"); n != 1 {
		t.Errorf("task_accepted 该恰一行，实际 %d", n)
	}
	by, _ := chainAcceptedBy(t, d)
	if by != "local-user" {
		t.Errorf("accepted_by 必须是写主体（契约③ §1.4：不是请求体自报字段），实际 %q", by)
	}

	// ── 格三：ACCEPTED 是终态，重复验收该 409 且不多发事件（没带 key ⇒ 走状态机这一条路）──
	reCode, reBody := call(t, srv, http.MethodPost, "/api/v1/tasks/task-send:accept", "local-user", "", "")
	if reCode != http.StatusConflict {
		t.Errorf("终态上重复验收该 409，实际 %d %s", reCode, reBody)
	}
	if n := chainEventRows(t, d, "task_accepted"); n != 1 {
		t.Errorf("重复验收后 task_accepted 仍该 1 行，实际 %d", n)
	}
	if st := chainTaskStatus(t, d, "task-send"); st != "ACCEPTED" {
		t.Errorf("被拒的验收不许改动状态，实际 %s", st)
	}
}

// TestAcceptWrongStateNeitherPushesNorAccepts 是"错态"那一半的判据：
// 投影结果不是 AGENT_COMPLETED（FAILED 帧 ⇒ task FAILED）⇒ 半格不发生；
// 这时 `:accept` 仍 409，且 task_waiting_acceptance / task_accepted 一行都没有。
func TestAcceptWrongStateNeitherPushesNorAccepts(t *testing.T) {
	srv, d := chainHarness(t)
	chainSend(t, srv)
	driveFrame(t, d, "ex-chain", "FAILED")

	if st := chainTaskStatus(t, d, "task-send"); st != "FAILED" {
		t.Fatalf("FAILED 帧该把 task 投影成 FAILED，实际 %s（错态那一格的前提没成立）", st)
	}
	if n := chainEventRows(t, d, "task_failed"); n != 1 {
		t.Errorf("task_failed 该恰一行，实际 %d", n)
	}
	if n := chainEventRows(t, d, "task_waiting_acceptance"); n != 0 {
		t.Errorf("投影不是 AGENT_COMPLETED ⇒ 半格不发生，实际发了 %d 行 task_waiting_acceptance", n)
	}
	if n := chainEventRows(t, d, "task_agent_completed"); n != 0 {
		t.Errorf("FAILED 那一格不该发 task_agent_completed，实际 %d 行", n)
	}

	aCode, aBody := call(t, srv, http.MethodPost, "/api/v1/tasks/task-send:accept", "local-user", "", "")
	if aCode != http.StatusConflict {
		t.Fatalf("FAILED 不是 WAITING_ACCEPTANCE ⇒ `:accept` 该 409，实际 %d %s", aCode, aBody)
	}
	if c := codeOf(t, aBody); c != "illegal_transition" {
		t.Errorf("错态验收该 409 illegal_transition，实际 %q", c)
	}
	if st := chainTaskStatus(t, d, "task-send"); st != "FAILED" {
		t.Errorf("被拒的验收不许改动状态，实际 %s", st)
	}
	if n := chainEventRows(t, d, "task_accepted") + chainEventRows(t, d, "task_waiting_acceptance"); n != 0 {
		t.Errorf("被拒的验收不该发任何链尾事件，实际 %d 行", n)
	}
}

// TestAcceptIdempotencyKeyReplaysFirstResponse 钉第七十八批给 `:accept` 补的幂等那半格
// （yaml :445 把 Idempotency-Key 声明成 required，实现此前没接；本批口径：带了就走、没带放行 ——
// 没带的那条路由上面两条用例覆盖）。三件事一起断：
//
//	① 同键重放 = 逐字节等于首次响应，且版本不动、事件不增（"HTTP 200"不等于"又执行了一次"）；
//	② 幂等行已密封（COMPLETED）；
//	③ 同一把键换**另一条任务** ⇒ 409 idempotency_mismatch 喊出来，而不是回别人的 200
//	   —— 这就是 request_hash 取路径而不取空 body 的那条牙（body 恒空 ⇒ 换任务也判不出）。
func TestAcceptIdempotencyKeyReplaysFirstResponse(t *testing.T) {
	srv, d := chainHarness(t)
	chainSend(t, srv)
	driveFrame(t, d, "ex-chain", "SUCCEEDED")

	// 另造一条任务，用作"同键换任务"的那一发的落点（它不必到 WAITING_ACCEPTANCE：
	// 幂等认领在动作之前，hash 不合当场就喊，压根碰不到状态机）。
	code, body := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-other", `{"title":"另一条"}`)
	if code != http.StatusCreated {
		t.Fatalf("建第二条任务该 201，实际 %d %s", code, body)
	}
	var other taskJSON
	if err := json.Unmarshal([]byte(body), &other); err != nil {
		t.Fatal(err)
	}

	firstCode, firstBody := call(t, srv, http.MethodPost, "/api/v1/tasks/task-send:accept",
		"local-user", "k-acc", "")
	if firstCode != http.StatusOK {
		t.Fatalf("首击该 200，实际 %d %s", firstCode, firstBody)
	}
	journal := countRows(t, d, "event_journal")
	ver := chainVersion(t, d, "task-send")

	// ① 同键重放。
	repCode, repBody := call(t, srv, http.MethodPost, "/api/v1/tasks/task-send:accept",
		"local-user", "k-acc", "")
	if repCode != http.StatusOK {
		t.Fatalf("同键重放该 200，实际 %d %s", repCode, repBody)
	}
	if repBody != firstBody {
		t.Errorf("重放该逐字节等于首次响应：\n首 %s\n重 %s", firstBody, repBody)
	}
	if n := countRows(t, d, "event_journal"); n != journal {
		t.Errorf("重放不许多写事件：前 %d 后 %d", journal, n)
	}
	if v := chainVersion(t, d, "task-send"); v != ver {
		t.Errorf("重放不许推版本：首 v%d，重放后 v%d", ver, v)
	}
	if n := chainEventRows(t, d, "task_accepted"); n != 1 {
		t.Errorf("重放后 task_accepted 仍该 1 行，实际 %d", n)
	}
	// ② 幂等行已密封。
	if st, _ := idemRow(t, d, "k-acc"); st != "COMPLETED" {
		t.Errorf("首次 `:accept` 之后该密封（COMPLETED），实际 %s", st)
	}
	// ③ 同键换另一条任务 ⇒ 喊出来，而不是回上面那份 200。
	misCode, misBody := call(t, srv, http.MethodPost, "/api/v1/tasks/"+other.ID+":accept",
		"local-user", "k-acc", "")
	if misCode != http.StatusConflict || codeOf(t, misBody) != "idempotency_mismatch" {
		t.Errorf("同一把键换一条任务该 409 idempotency_mismatch，实际 %d %s", misCode, misBody)
	}
	if st := chainTaskStatus(t, d, other.ID); st != "CREATED" {
		t.Errorf("被 mismatch 挡下的那一发不该动第二条任务，实际 %s", st)
	}
}

func chainTaskStatus(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(`SELECT status FROM tasks WHERE id=?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func chainVersion(t *testing.T, d *db.DB, id string) int64 {
	t.Helper()
	var v int64
	if err := d.Read.QueryRow(`SELECT version FROM tasks WHERE id=?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func chainExecStatus(t *testing.T, d *db.DB, id string) (string, int64) {
	t.Helper()
	var s string
	var v int64
	if err := d.Read.QueryRow(`SELECT status, COALESCE((SELECT aggregate_version FROM event_journal
		WHERE aggregate_type='execution' AND aggregate_id=? ORDER BY aggregate_version DESC LIMIT 1),0)
		FROM executions WHERE id=?`, id, id).Scan(&s, &v); err != nil {
		t.Fatal(err)
	}
	return s, v
}

func chainEventRows(t *testing.T, d *db.DB, evType string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type=?`, evType).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// chainPayload 读一条事件 payload 里的某个键（断言"这条事件点得出是哪一次执行"这一类）。
func chainPayload(t *testing.T, d *db.DB, evType, key string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(`SELECT COALESCE(json_extract(payload_json, '$.' || ?), '') FROM event_journal
		WHERE event_type=? ORDER BY seq LIMIT 1`, key, evType).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func chainAcceptedBy(t *testing.T, d *db.DB) (string, int64) {
	t.Helper()
	var by string
	var seq int64
	if err := d.Read.QueryRow(`SELECT json_extract(payload_json,'$.accepted_by'), seq FROM event_journal
		WHERE event_type='task_accepted' ORDER BY seq LIMIT 1`).Scan(&by, &seq); err != nil {
		t.Fatal(err)
	}
	return by, seq
}
