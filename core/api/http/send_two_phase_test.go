package httpapi_test

// REQ-S-1（一次 send 停掉全 Core 的写）的落地判据，按契约①「REQ-S-1 两段式的幂等语义」那一格 ⑤ 钉：
// 三条用例 —— (a) 崩在密封前 ⇒ 同键续跑、库里不许多算一次；(b) 双客户端同键并发 ⇒ 只一个封得上、
// 另一个走 0 行分支**并且喊出来**；(c) 挂住/超时 ⇒ execution 留 RUNNING + 503 `adapter_unresponsive` + 不多发事件。
// 外加这条红本来要修的那件事本身：**挂住的 send 不许再 hold 住别人的写**（第 23 条⑤ 那条 1 秒判据）。
//
// 两条反例（判决写在台账，跑法是 `.scratch` 里的一次性变异脚本）：
//   ① 把续跑改回 errReplay ⇒ (a) 必须红；② 把密封的 0 行分支摘掉 ⇒ (b) 必须红；
//   ③（本批补的第三条）把 `Deliver` 搬回第一笔事务里 ⇒ "挂住期间 POST /projects 1 秒内 201" 必须红。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// gateTurn 是能被"卡住"也能被"放行"的连接面替身。旧的 stubTurn 只会立即返回，
// 它证不到这一批要证的任何一件事 —— 那正是"单层能绿、生产路径有牙"缺口的一半成因（第 271 条）。
type gateTurn struct {
	mu      sync.Mutex
	calls   int
	lastErr error
	rec     execution.TurnReceipt

	entered chan struct{} // 非 nil ⇒ 每次进入 CallSendTurn 时敲一次（缓冲 4，测试用来等"确实到了投递这一步"）
	gate    chan struct{} // 非 nil ⇒ **只有第 1 次调用**等它被关闭（模拟那一个挂住的 Adapter）
}

func (g *gateTurn) CallSendTurn(ctx context.Context, _ execution.TurnCall) (execution.TurnReceipt, error) {
	g.mu.Lock()
	g.calls++
	n := g.calls
	err, rec := g.lastErr, g.rec
	g.mu.Unlock()
	if g.entered != nil {
		select {
		case g.entered <- struct{}{}:
		default: // 计数用途，满了就丢，不阻塞被试代码
		}
	}
	// 只卡第一发：替身若把每一发都卡住，并发那一格会锁死在替身上、测不到被测代码（我 12:29 那趟 600s 超时就是这么来的）。
	if g.gate != nil && n == 1 {
		select {
		case <-g.gate:
		case <-ctx.Done():
			return execution.TurnReceipt{}, ctx.Err()
		}
	}
	return rec, err
}

func (g *gateTurn) set(err error, rec execution.TurnReceipt) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastErr, g.rec = err, rec
}

func (g *gateTurn) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func unresponsiveErr() error {
	return &adapter.Error{Code: adapter.ErrAdapterUnresponsive, Message: "用例注入：连接面挂住"}
}

func eventTypes(t *testing.T, d *db.DB, aggID string) []string {
	t.Helper()
	rows, err := d.Read.Query(`SELECT event_type FROM event_journal WHERE aggregate_id = ? ORDER BY seq`, aggID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func idemRow(t *testing.T, d *db.DB, key string) (state, stored string) {
	t.Helper()
	if err := d.Read.QueryRow(`SELECT state, COALESCE(response_json, '') FROM idempotency_keys WHERE key = ?`, key).
		Scan(&state, &stored); err != nil {
		t.Fatal(err)
	}
	return state, stored
}

func execStatusVersion(t *testing.T, d *db.DB, id string) (string, int64) {
	t.Helper()
	var st string
	var v int64
	if err := d.Read.QueryRow(`SELECT status, version FROM executions WHERE id = ?`, id).Scan(&st, &v); err != nil {
		t.Fatal(err)
	}
	return st, v
}

// post 带超时地发一发，返回状态码、响应体与耗时（1 秒那格要的是耗时，不是"最后成功了"）。
func postTimed(t *testing.T, url, token, principal, key, body string) (int, string, time.Duration) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Lucero-Principal", principal)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	cli := &http.Client{Timeout: 8 * time.Second}
	start := time.Now()
	resp, err := cli.Do(req)
	if err != nil {
		t.Fatalf("发不出去 %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b), time.Since(start)
}

// readExecutionViaFace 经 HTTP 读面取那条 execution 的当前形状（② 的"回读库里那一行"就走这里，
// 不再往响应体里找一个 yaml 没有的键）。
func readExecutionViaFace(t *testing.T, srv *httptest.Server, taskID, execID string) map[string]any {
	t.Helper()
	code, body := call(t, srv, http.MethodGet, "/api/v1/executions?task_id="+taskID, "", "", "")
	if code != http.StatusOK {
		t.Fatalf("读面该 200，实际 %d %s", code, body)
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	for _, it := range page.Items {
		if it["id"] == execID {
			return it
		}
	}
	t.Fatalf("读面里没有那条 execution %s（items=%s）", execID, body)
	return nil
}

// TestSendResumesAfterLostSeal 是判据 (a)：第一次投递失败 ⇒ **不密封**（意图已提交是既成事实）；
// 同键第二次读到的就是"未密封 + 第一笔身份"⇒ 走续跑。这一格要防的事只有一个：**多算一次**。
func TestSendResumesAfterLostSeal(t *testing.T) {
	turn := &gateTurn{lastErr: unresponsiveErr()}
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-a", status: "CREATED", sessionID: "sess-a", seedSession: true, turn: turn,
	}, true)

	code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-a/send", "local-user", "k-a",
		`{"text":"续跑这一格","delivery":"start_now"}`)
	if code != http.StatusServiceUnavailable || codeOf(t, body) != "adapter_unresponsive" {
		t.Fatalf("投递失败该 503 adapter_unresponsive（丙），实际 %d %s", code, body)
	}
	// 第一笔已提交 ⇒ 消息落了一次、CREATED→STARTING→RUNNING 两条状态边都在。
	if n := countRows(t, d, "messages"); n != 1 {
		t.Fatalf("第一笔该恰落 1 行消息，实际 %d", n)
	}
	// 投递失败后"库里那一行"要**经读面**判（主控 09-27 ② 改的字：不再从响应体里找 version ——
	// yaml `SendResult` 只有四键，加 version 等于改对外契约，得另开 REQ 连面板一起推）。
	if st, _ := execStatusVersion(t, d, "ex-a"); st != "RUNNING" {
		t.Errorf("丙：投递失败之后 execution 该留在 RUNNING 不动（直接读库那份），实际 %s", st)
	}
	if view := readExecutionViaFace(t, srv, "task-send", "ex-a"); view["status"] != "RUNNING" {
		t.Errorf("同一件事经 `GET /api/v1/executions?task_id=` 回读该是 RUNNING，实际 %v", view)
	}
	if st, _ := idemRow(t, d, "k-a"); st != "IN_PROGRESS" {
		t.Errorf("没密封 ⇒ 幂等行该还是 IN_PROGRESS（这一次的中断是可续跑的，不是死态），实际 %s", st)
	}
	edges := eventTypes(t, d, "ex-a")
	if len(edges) != 2 {
		t.Fatalf("失败那一趟该只有第一笔的两条状态边，实际 %d 条 %v", len(edges), edges)
	}

	// 同键重试：这一次连接面通了 ⇒ 续跑 + 密封。
	turn.set(nil, execution.TurnReceipt{Accepted: true})
	code2, body2 := call(t, srv, http.MethodPost, "/api/v1/executions/ex-a/send", "local-user", "k-a",
		`{"text":"续跑这一格","delivery":"start_now"}`)
	if code2 != http.StatusAccepted {
		t.Fatalf("续跑该 202，实际 %d %s", code2, body2)
	}
	if n := countRows(t, d, "messages"); n != 1 {
		t.Errorf("续跑**不许**写第二行消息（这一格就是防它），实际 %d 行", n)
	}
	if v := readExecutionViaFace(t, srv, "task-send", "ex-a"); v["version"] != float64(3) {
		// 判据 (a) 的"version 等于当前库版本"现在落在这里：读面回读的 v3 == 库里那一行的 v3。
		t.Errorf("续跑之后读面回读该是 v3（库里那一行），实际 %v", v)
	}
	if e2 := eventTypes(t, d, "ex-a"); len(e2) != 2 {
		t.Errorf("状态边也不许多走一条，实际 %d 条 %v", len(e2), e2)
	}
	if st, stored := idemRow(t, d, "k-a"); st != "COMPLETED" || stored == "" {
		t.Errorf("续跑完成该把结果密封，实际 state=%s body=%q", st, stored)
	}
	if turn.count() != 2 {
		t.Errorf("两趟各投一次连接面（首攻失败 + 续跑成功），实际 %d 次", turn.count())
	}
	// 密封之后再同键来一发 ⇒ 回放，第三次投递不该发生。
	code3, body3 := call(t, srv, http.MethodPost, "/api/v1/executions/ex-a/send", "local-user", "k-a",
		`{"text":"续跑这一格","delivery":"start_now"}`)
	if code3 != http.StatusAccepted || body3 != body2 {
		t.Errorf("密封后的重放该逐字节等于密封那份，实际 %d %q vs %q", code3, body3, body2)
	}
	if n := turn.count(); n != 2 {
		t.Errorf("回放不许再投一次连接面，实际 %d 次", n)
	}
	var view sendResultViewJSON
	if err := json.Unmarshal([]byte(body2), &view); err != nil {
		t.Fatal(err)
	}
	if view.Delivery != "start_now" || !view.Accepted {
		t.Errorf("续跑的答案该是请求态 + 回执里的 accepted，实际 %+v", view)
	}
}

type sendResultViewJSON struct {
	Delivery    string  `json:"delivery"`
	Accepted    bool    `json:"accepted"`
	MessageID   *string `json:"message_id"`
	ExecutionID *string `json:"execution_id"`
}

// TestSendConcurrentSealLoserShouts 是判据 (b)：两个客户端同一个键。
// 先到的卡在投递里（真挂住），后到的读到凭证 ⇒ 也走投递并先密封；
// 先到的回来密封时 guard 挡下 ⇒ **0 行必须喊**（409），不许静默当成自己封上了。
func TestSendConcurrentSealLoserShouts(t *testing.T) {
	entered := make(chan struct{}, 4)
	gate := make(chan struct{})
	turn := &gateTurn{entered: entered, gate: gate}
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-b", status: "RUNNING", sessionID: "sess-b", seedSession: true, turn: turn,
	}, true)

	first := make(chan string, 1)
	go func() {
		code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-b/send", "local-user", "k-b",
			`{"text":"并发双续跑","delivery":"start_now"}`)
		first <- strconv.Itoa(code) + " " + body
	}()

	// 等第一趟确实进了投递（第一笔已提交 ⇒ 凭证在库里），再放第二趟进来。
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("第一趟没进投递")
	}
	code2, body2 := call(t, srv, http.MethodPost, "/api/v1/executions/ex-b/send", "local-user", "k-b",
		`{"text":"并发双续跑","delivery":"start_now"}`)
	if code2 != http.StatusAccepted {
		t.Fatalf("第二趟续跑并先密封该 202，实际 %d %s", code2, body2)
	}

	close(gate) // 放第一趟回来密封
	var got string
	select {
	case got = <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("第一趟没回来")
	}
	if !strings.HasPrefix(got, "409") || !strings.Contains(got, "idempotency_replay") {
		t.Errorf("输掉密封的那条必须喊（409 idempotency_replay），实际 %q", got)
	}
	if n := countRows(t, d, "messages"); n != 1 {
		t.Errorf("两趟合计只该有一行消息（续跑不许多写），实际 %d", n)
	}
	var completed int
	if err := d.Read.QueryRow(`SELECT count(*) FROM idempotency_keys WHERE key='k-b' AND state='COMPLETED'`).
		Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Errorf("密封只能有一次，实际 %d 行 COMPLETED", completed)
	}
}

// TestSendDoesNotHoldTheWriteConnection 才是 REQ-S-1 的病根本身：send 挂在投递里时，
// 别的写必须在**一个明确上界内**答话（判据 N=1 秒）。旧形状下这一发会一直等到客户端超时。
func TestSendDoesNotHoldTheWriteConnection(t *testing.T) {
	entered := make(chan struct{}, 2)
	gate := make(chan struct{})
	defer close(gate) // 无论断言走到哪，都别把 goroutine 留成永久挂起
	turn := &gateTurn{entered: entered, gate: gate}
	srv, d := newSendHarness(t, sendSeed{
		execID: "ex-c", status: "RUNNING", sessionID: "sess-c", seedSession: true, turn: turn,
	}, true)

	done := make(chan error, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/executions/ex-c/send",
			strings.NewReader(`{"text":"挂住","delivery":"start_now"}`))
		if err != nil {
			done <- err
			return
		}
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("X-Lucero-Principal", "local-user")
		req.Header.Set("Idempotency-Key", "k-c")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("send 没进投递那一步")
	}

	code, body, took := postTimed(t, srv.URL+"/api/v1/projects", testToken, "local-user", "k-c-proj",
		`{"canonical_path":"/t73/during-hang","name":"挂住期间的写"}`)
	if code != http.StatusCreated {
		t.Errorf("send 挂住期间 POST /projects 该 201，实际 %d %s", code, body)
	}
	if took > time.Second {
		t.Errorf("判据是 1 秒内 201（不变量的意思是别的写**根本不该等**），实际等了 %s", took)
	}
	if n := countRows(t, d, "projects"); n != 1 {
		t.Errorf("那一发写该真落了库，实际 projects %d 行", n)
	}
}
