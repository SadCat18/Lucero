package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/SadCat18/lucero/core/internal/task"
)

const testToken = "handler-token"

// redactMarker 与 secretShape 是本包测试自带的最小脱敏规则：权威是 `audit.Rules()`，
// 但业务包（含其测试）不 import 别的规则包拿配置（C14 的教训：audit 在途改动会把我的包带红）。
// 这条 pattern 与 `audit.Rules().Patterns` 里 `"sk-[A-Za-z0-9_-]{16,}"` 同形，够 §2.2 的断言用。
const (
	redactMarker = "[REDACTED]"
	secretShape  = `sk-[A-Za-z0-9_-]{16,}`
	testSecret   = "sk-abcdefghijklmnop0123456789"
)

// queueBody 是 `:queue` 的请求体。C43 裁「乙」（T-262）之后 `adapter_key` 必填。
// harness 会种一行 `adapter_instances(adapter_key='fake')`，所以这个 key 解析得出实例 id。
const queueBody = `{"adapter_key":"fake"}`

// taskJSON 是响应体的读侧镜像（handler 侧的类型不外露，测试只按 JSON 形状断言）。
type taskJSON struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Version int64  `json:"version"`
}

// newTaskHarness 挂上真 SQLite + 真 task.Service/Repository：
// handler 层最有价值的证据就是"错误码、状态码、幂等、游标"这四件事对不对，
// 而这些只有跑真库才作数（假 tx 类型我根本造不出来，platform/db 不供 ExecContext）。
func newTaskHarness(t *testing.T) (*httptest.Server, *db.DB) {
	t.Helper()
	// 默认**不**注入读面：`TestTaskItemPathFollowsC28Ruling` 钉的就是"declared、方法合法、能力没接线 ⇒ 501"，
	// 注入进去那条钉子会静默变成"404"，那是另一种对。
	return taskHarness(t, false)
}

// taskHarness 的两个开关只差一件事：`GET /api/v1/tasks/{id}` 的读面注不注入。
// 写成参数而不是复制第二个 harness：那 40 行迁移与夹具一旦分两份，迟早一处改了另一处没改。
func taskHarness(t *testing.T, withGet bool) (*httptest.Server, *db.DB) {
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
	// 分派腿的外部门：`queueBody` 里的 adapter_key='fake' 必须解析得出实例，
	// 否则 :queue 全走 404、后面所有正向断言都测不到三步状态推进。
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ad-fake', 'fake', 'Fake', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	pstore, err := adapter.NewSQLiteStore(d, func() int64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	ready := func() string { return httpapi.StateReady }
	cfg := httpapi.Config{
		Token: testToken, Version: "test-1", State: ready,
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            task.NewService(nil),
		TaskList:         task.NewRepository(d.Read),
		Store:            d,
		Instances:        pstore,
		ExecutionCreate:  execution.NewService(),
	}
	if withGet {
		cfg.TaskGet = task.NewRepository(d.Read)
	}
	srv := httptest.NewServer(httpapi.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv, d
}

type errEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

func call(t *testing.T, srv *httptest.Server, method, path, principal, idemKey, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	if principal != "" {
		req.Header.Set("X-Lucero-Principal", principal)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func codeOf(t *testing.T, body string) string {
	t.Helper()
	var e errEnvelope
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("错误体形状不符契约③ §2: %q", body)
	}
	if e.Error.Code == "" {
		t.Fatalf("错误体缺 code: %q", body)
	}
	return e.Error.Code
}

// msgOf 取错误体里的 message。只有一种情况该断它：**缺陷本身就是一句话**
// （比如对一条声明过的路径回「形状我不认识」）—— 那种格子只断状态码与 code 对缺陷没有牙。
func msgOf(t *testing.T, body string) string {
	t.Helper()
	var e errEnvelope
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("错误体形状不符契约③ §2: %q", body)
	}
	return e.Error.Message
}

// mustParseMap 把成功响应读成 map，供"两次响应语义相同"这类断言用（键序不参与比较）。
func mustParseMap(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("响应不是合法 JSON 对象: %q (%v)", body, err)
	}
	return m
}

func countRows(t *testing.T, d *db.DB, table string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// countEventsOfType 数 `event_journal` 里某一类事件落了几条（表名写死、类型走参数）。
func countEventsOfType(t *testing.T, d *db.DB, evType string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type = ?`, evType).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestCreateThenQueueThenReplay 覆盖垂直链前两步的 handler 面，
// 并钉住 §40 那条：同一 Idempotency-Key 重放**绝不能**产生第二个 Task。
func TestCreateThenQueueThenReplay(t *testing.T) {
	srv, d := newTaskHarness(t)
	body := `{"title":"跑通垂直链","priority":"HIGH","mode":"EXECUTE","risk_level":"LOW"}`

	status, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-1", body)
	if status != http.StatusCreated {
		t.Fatalf("建任务应 201，实际 %d: %s", status, out)
	}
	var created taskJSON
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "CREATED" || created.Version != 1 || created.ID == "" {
		t.Fatalf("响应形状不对: %+v", created)
	}
	// §3 要 UUIDv7：版本位 7、variant 10xx，前端与恢复都要靠这个形状。
	if len(created.ID) != 36 || created.ID[14] != '7' || !strings.ContainsRune("89ab", rune(created.ID[19])) {
		t.Errorf("id 不是 UUIDv7 形状: %q", created.ID)
	}

	// 同一 key 同一 body 重放：**原样返回首次响应**（T-57，契约③ §2"命中已完成记录时返回首次的响应"），
	// 并且一条副作用都不许多：任务 1 条、事件 1 行、版本不许被推高。
	firstOut := out
	if status, out = call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-1", body); status != http.StatusCreated {
		t.Fatalf("重放应 201（返回首次响应），实际 %d: %s", status, out)
	}
	// 内部契约 §1.1 第 13 条"响应体口径"（C18 裁定 ②）：首次与重放**逐字节相等**，
	// 并且那一份必须**就是库里存的 response_json** —— 不接受"允许键序不同"的特例断言。
	// 这两条一起挡住一类真 bug：返回自己 Marshal 的那份而落库的是 scrub 后的另一份，
	// 平时只差键序，一旦脱敏命中就是"未打码的值发到线上、库里存打码版"。
	if out != firstOut {
		t.Errorf("重放没有逐字节返回首次响应：\n  首次 = %s\n  重放 = %s", firstOut, out)
	}
	var stored string
	if err := d.Read.QueryRow(`SELECT response_json FROM idempotency_keys WHERE key='k-1'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != firstOut || stored != out {
		t.Errorf("wire 与存储不是同一份：\n  库里 = %s\n  首次 = %s\n  重放 = %s", stored, firstOut, out)
	}
	if n := countRows(t, d, "tasks"); n != 1 {
		t.Errorf("重放产生了第 %d 条任务（§40 要避免的高风险动作重复执行）", n)
	}
	if n := countRows(t, d, "event_journal"); n != 1 {
		t.Errorf("重放多写了 %d 行事件", n)
	}

	// 同一 key 换 body：键含 request_hash，必须拒掉且同样不落库。
	if status, out = call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-1",
		`{"title":"换个内容","priority":"LOW"}`); status != http.StatusConflict {
		t.Fatalf("同 key 不同 body 应 409，实际 %d: %s", status, out)
	} else if c := codeOf(t, out); c != "idempotency_mismatch" {
		t.Errorf("错误码应为 idempotency_mismatch，实际 %q", c)
	}
	if n := countRows(t, d, "tasks"); n != 1 {
		t.Errorf("同 key 换 body 竟然建出了第 %d 条任务", n)
	}

	// Queue 走 If-Match：带对版本 200，带错版本 409，重复 Queue 409 illegal_transition。
	id := created.ID
	if status, out = call(t, srv, http.MethodPost, "/api/v1/tasks/"+id+":queue", "local-user", "", queueBody); status != http.StatusOK {
		t.Fatalf("queue 应 200，实际 %d: %s", status, out)
	}
	var q struct {
		Status  string `json:"status"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		t.Fatal(err)
	}
	// 分派腿把 Queue+Create+Prepare 收成一次 :queue：CREATED v1 →（Queue v2 → Prepare v3）
	// 整笔事务原子推进，对外只见终态 PREPARING v3。
	if q.Status != "PREPARING" || q.Version != 3 {
		t.Errorf("queue 后应 PREPARING v3（分派腿三步一笔事务），实际 %+v", q)
	}
	if n := countRows(t, d, "executions"); n != 1 {
		t.Errorf(":queue 应恰建 1 条 Execution，实际 %d", n)
	}
	if status, out = call(t, srv, http.MethodPost, "/api/v1/tasks/"+id+":queue", "local-user", "", queueBody); status != http.StatusConflict {
		t.Errorf("重复 queue 应 409，实际 %d: %s", status, out)
	} else if c := codeOf(t, out); c != "illegal_transition" {
		t.Errorf("错误码应为 illegal_transition，实际 %q", c)
	}
	// 双 :queue 的事务回滚半边：第二次在 Tasks.Queue 就 illegal ⇒ 整笔回滚 ⇒
	// executions 不许多第二行（backend.md Next action 点名的用例）。
	if n := countRows(t, d, "executions"); n != 1 {
		t.Errorf("被拒的第二次 queue 不该多建 Execution，实际 %d 行", n)
	}

	stale := `{"title":"并发","priority":"NORMAL"}`
	if status, out = call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-2", stale); status != http.StatusCreated {
		t.Fatalf("建第二条应 201，实际 %d: %s", status, out)
	}
	var second taskJSON
	if err := json.Unmarshal([]byte(out), &second); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/tasks/"+second.ID+":queue", strings.NewReader(queueBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Lucero-Principal", "local-user")
	req.Header.Set("If-Match", "99")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("If-Match 不符应 409，实际 %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if c := codeOf(t, string(b)); c != "version_conflict" {
		t.Errorf("错误码应为 version_conflict（契约③ §2），实际 %q", c)
	}

	// 分派腿之后 :queue 落到 PREPARING（不是停在 QUEUED），列表过滤跟着改。
	if status, out = call(t, srv, http.MethodGet, "/api/v1/tasks?status=PREPARING", "", "", ""); status != http.StatusOK {
		t.Fatalf("列表应 200，实际 %d: %s", status, out)
	} else {
		var page struct {
			Items      []taskJSON `json:"items"`
			NextCursor string     `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(out), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].ID != id || page.NextCursor != "" {
			t.Errorf("status=PREPARING 列表不对: %s", out)
		}
	}
}

// T-57 的另一半：首次请求还挂在 IN_PROGRESS 时**确实没有响应可回**，此刻才许 409。
// 这条不是补剧情——没有它，"一律返回首次响应"可以退化成"未封存时回个 201 + 空 body"还全绿。
func TestReplayWhileFirstRequestInProgress(t *testing.T) {
	srv, d := newTaskHarness(t)
	body := `{"title":"首攻未完","priority":"NORMAL"}`
	sum := sha256.Sum256([]byte(body))
	if _, err := d.Read.Exec(`INSERT INTO idempotency_keys
		(key, operation, request_hash, state, created_at, updated_at)
		VALUES (?, 'task_created', ?, 'IN_PROGRESS', 1, 1)`,
		"k-busy", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	status, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-busy", body)
	if status != http.StatusConflict || codeOf(t, out) != "idempotency_replay" {
		t.Fatalf("首次未完应 409 idempotency_replay，实际 %d: %s", status, out)
	}
	if n := countRows(t, d, "tasks"); n != 0 {
		t.Errorf("IN_PROGRESS 占坑期间竟然建出了 %d 条任务", n)
	}
}

// TestTaskVerbRoutingAndBoundedChecks 钉住 :pause/:resume 里**不依赖事件注册表**的那几条：
// 没听过的 verb 仍 501（不假成功）、:resume 必须点名目标、If-Match 必须是正整数、
// 矩阵不允许的动作为 409、不存在的任务为 404。这几条现在就是终态，不是"等 db 之后才有效"。
func TestTaskVerbRoutingAndBoundedChecks(t *testing.T) {
	srv, d := newTaskHarness(t)
	_, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-v",
		`{"title":"暂停边界","priority":"NORMAL"}`)
	var created taskJSON
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	vr := "/api/v1/tasks/" + created.ID

	// CREATED 没有到 PAUSED 的边（§5.1）→ 409，且不写任何一行。
	if status, out2 := call(t, srv, http.MethodPost, vr+":pause", "local-user", "", `{"reason":"还没排队"}`); status != http.StatusConflict {
		t.Errorf("CREATED→PAUSED 应 409，实际 %d: %s", status, out2)
	} else if c := codeOf(t, out2); c != "illegal_transition" {
		t.Errorf("错误码应为 illegal_transition，实际 %q", c)
	}
	// :resume 不点名目标 → 400（两条合法出边，替客户端选等于替它决定要不要有 Execution）。
	for _, body := range []string{"", `{}`, `{"to":"CANCELLED"}`, `{"to":"paused"}`, `not json`} {
		if status, out2 := call(t, srv, http.MethodPost, vr+":resume", "local-user", "", body); status != http.StatusBadRequest {
			t.Errorf(":resume body %q 应 400，实际 %d: %s", body, status, out2)
		} else if c := codeOf(t, out2); c != "validation_failed" {
			t.Errorf(":resume body %q 错误码应为 validation_failed，实际 %q", body, c)
		}
	}
	// 坏 If-Match 在碰库之前就挡掉。
	badReq, _ := http.NewRequest(http.MethodPost, srv.URL+vr+":pause", strings.NewReader(`{}`))
	badReq.Header.Set("Authorization", "Bearer "+testToken)
	badReq.Header.Set("X-Lucero-Principal", "local-user")
	badReq.Header.Set("If-Match", "abc")
	if resp, err := http.DefaultClient.Do(badReq); err != nil {
		t.Fatal(err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("If-Match=abc 应 400，实际 %d", resp.StatusCode)
		}
	}
	// 没听过的 verb：仍然 501，不假成功也不 404 混过去。
	if status, out2 := call(t, srv, http.MethodPost, vr+":teleport", "local-user", "", ""); status != http.StatusNotImplemented {
		t.Errorf("未知 verb 应 501，实际 %d: %s", status, out2)
	} else if c := codeOf(t, out2); c != "not_implemented" {
		t.Errorf("错误码应为 not_implemented，实际 %q", c)
	}
	// 不存在的任务：404，且不是 500。
	if status, _ := call(t, srv, http.MethodPost, "/api/v1/tasks/nope:pause", "local-user", "", `{}`); status != http.StatusNotFound {
		t.Errorf("不存在的 Task 应 404，实际 %d", status)
	}
	if n := countRows(t, d, "tasks"); n != 1 {
		t.Errorf("上面这些被挡掉的调用竟然建出了 %d 条任务", n)
	}
	if n := countRows(t, d, "event_journal"); n != 1 {
		t.Errorf("上面这些被挡掉的调用写出了 %d 行事件", n)
	}

	// 合法暂停的真实行为：`task_paused` / `task_resumed` 09-24 已在注册集合内（`events.go` 的 Registered 那一行），
	// 所以这一支就是**成功路径** —— 以前这里写着"REQ-DB-3 未落地 ⇒ 只能 500"，那是过期前提（T-253② 点名要改的那处）。
	if _, out2 := call(t, srv, http.MethodPost, vr+":queue", "local-user", "", queueBody); out2 == "" {
		t.Fatal("queue 没返回内容")
	}
	status, out3 := call(t, srv, http.MethodPost, vr+":pause", "local-user", "", `{"reason":"注册表已补"}`)
	if status != http.StatusOK {
		t.Errorf("pause 该走成功路径（200 + Task 体），实际 %d: %s", status, out3)
	}
	if !strings.Contains(out3, `"PAUSED"`) {
		t.Errorf("暂停之后该是 PAUSED（§5.1 那条边），实际 %s", out3)
	}
}

// TestPauseResumeWritesBothEvents 是同一件事的事件半边：成功路径要留下**两条**事件，
// 而不是只把状态改了（第 17 条"行与事件同生同死"在 tasks 这条竖线上的对应物）。
func TestPauseResumeWritesBothEvents(t *testing.T) {
	srv, d := newTaskHarness(t)
	_, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-ev", `{"title":"要留两条事件"}`)
	var created taskJSON
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	vr := "/api/v1/tasks/" + created.ID
	if status, out2 := call(t, srv, http.MethodPost, vr+":queue", "local-user", "", queueBody); status != http.StatusOK {
		t.Fatalf("queue 该 200，实际 %d: %s", status, out2)
	}
	for _, reason := range []string{"先看暂停", "再看恢复"} {
		if status, ev := call(t, srv, http.MethodPost, vr+":pause", "local-user", "", `{"reason":"`+reason+`"}`); status != http.StatusOK {
			t.Fatalf("pause 该 200，实际 %d: %s", status, ev)
		}
		// :resume 要的是 `{"to": …}`（§5.1 的目标态），不是 reason；两轮都走矩阵里那条 PAUSED→QUEUED。
		if status, ev := call(t, srv, http.MethodPost, vr+":resume", "local-user", "", `{"to":"QUEUED"}`); status != http.StatusOK {
			t.Fatalf("resume 该 200，实际 %d: %s", status, ev)
		}
	}
	for _, evType := range []string{"task_paused", "task_resumed"} {
		if n := countEventsOfType(t, d, evType); n < 2 {
			t.Errorf("%s 该至少落两条（两次 pause/resume），实际 %d 条", evType, n)
		}
	}
}

// TestPauseResumeRoundTripOverHTTP 是 :pause→:resume 的端到端一条。
// `task_paused` / `task_resumed` 09-24 已在注册集合内（T-253② 量的那一行 `events.go`），
// 所以这一条**不再需要 Skip 门面** —— 以前那句"这一段现在会 Skip"是过期前提，跟着 `requireCatalogued` 一起撤掉。
func TestPauseResumeRoundTripOverHTTP(t *testing.T) {
	srv, _ := newTaskHarness(t)
	_, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-p", `{"title":"暂停一下"}`)
	var created taskJSON
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	vr := "/api/v1/tasks/" + created.ID
	if status, out2 := call(t, srv, http.MethodPost, vr+":queue", "local-user", "", queueBody); status != http.StatusOK {
		t.Fatalf("queue 应 200，实际 %d: %s", status, out2)
	}
	// 分派腿之后 :queue 已到 PREPARING v3；PREPARING→PAUSED 合法（§5.1）。
	// 版本链：Create v1 →（Queue v2 + Prepare v3）→ pause v4 → resume v5。
	if status, out2 := call(t, srv, http.MethodPost, vr+":pause", "local-user", "", `{"reason":"先歇会儿"}`); status != http.StatusOK {
		t.Fatalf("pause 应 200，实际 %d: %s", status, out2)
	} else if m := mustParseMap(t, out2); m["status"] != "PAUSED" || m["version"] != float64(4) {
		t.Errorf("pause 响应不对: %v", m)
	}
	if status, out2 := call(t, srv, http.MethodPost, vr+":resume", "local-user", "", `{"to":"QUEUED"}`); status != http.StatusOK {
		t.Fatalf("resume 应 200，实际 %d: %s", status, out2)
	} else if m := mustParseMap(t, out2); m["status"] != "QUEUED" || m["version"] != float64(5) {
		t.Errorf("resume 响应不对: %v", m)
	}
}

// TestIdempotentFirstResponseIsRedacted 补 U13-7 点出的那半句缺口：
// 原来我只证到"首次与重放一致"，而 §1.1 第 13 条响应体口径的风险点是"**库里对、发出去的那份错**"。
// 所以三处必须同时成立、且都不含明文：① 首次 201 body；② 重放 body；③ 库里的 response_json。
// 顺带证第四处 —— 写进 `tasks.title` 的那一列也不留明文（否则打码只发生在响应层，库还是漏的）。
func TestIdempotentFirstResponseIsRedacted(t *testing.T) {
	srv, d := newTaskHarness(t)
	body := `{"title":"标题里贴了密钥 ` + testSecret + `","priority":"NORMAL"}`

	status, first := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-secret", body)
	if status != http.StatusCreated {
		t.Fatalf("建任务应 201，实际 %d: %s", status, first)
	}
	if strings.Contains(first, testSecret) {
		t.Errorf("§2.2 被绕开：首次 201 body 把未脱敏的密钥形状值发出去了: %s", first)
	}
	if !strings.Contains(first, redactMarker) {
		t.Errorf("首次 body 里没有打码标记，说明这条 pattern 根本没生效（用例就成了空断言）: %s", first)
	}

	status, replay := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-secret", body)
	if status != http.StatusCreated || replay != first {
		t.Errorf("重放与首次不是同一份: %d\n  首次=%s\n  重放=%s", status, first, replay)
	}
	if strings.Contains(replay, testSecret) {
		t.Errorf("重放把明文发出去了: %s", replay)
	}

	var stored string
	if err := d.Read.QueryRow(`SELECT response_json FROM idempotency_keys WHERE key='k-secret'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != first || stored != replay {
		t.Errorf("wire 与存储分叉（C18 那一格）：\n  库里 = %s\n  首次 = %s\n  重放 = %s", stored, first, replay)
	}
	var rowTitle string
	if err := d.Read.QueryRow(`SELECT title FROM tasks`).Scan(&rowTitle); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rowTitle, testSecret) {
		t.Errorf("明文密钥进了 tasks.title —— 打码不能只在响应层做: %s", rowTitle)
	}
}

// TestCancelOverHTTPReachesTerminal 是 :cancel 的端到端一条（`task_cancelled` 已在 db 注册表里，
// 所以这条今天就是硬断言，不像 pause/resume 要等 REQ-DB-3）：
// CREATED→QUEUED→CANCELLED 三个版本三行事件，终态之后再动一律 409。
func TestCancelOverHTTPReachesTerminal(t *testing.T) {
	srv, d := newTaskHarness(t)
	_, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-can", `{"title":"取消我"}`)
	var created taskJSON
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	vr := "/api/v1/tasks/" + created.ID
	if status, out2 := call(t, srv, http.MethodPost, vr+":queue", "local-user", "", queueBody); status != http.StatusOK {
		t.Fatalf("queue 应 200，实际 %d: %s", status, out2)
	}
	// 分派腿后 :queue 已到 PREPARING v3；PREPARING→CANCELLED 合法，cancel 落 v4。
	// 事件行数：task_created + task_queued + execution_created + task_preparing + task_cancelled = 5。
	if status, out2 := call(t, srv, http.MethodPost, vr+":cancel", "local-user", "", `{"reason":"不做了"}`); status != http.StatusOK {
		t.Fatalf("cancel 应 200，实际 %d: %s", status, out2)
	} else if m := mustParseMap(t, out2); m["status"] != "CANCELLED" || m["version"] != float64(4) {
		t.Errorf("cancel 响应不对: %v", m)
	}
	// 终态之后：:accept 与 :cancel 都是 409 illegal_transition（不是 501、不是 500）。
	for _, verb := range []string{":accept", ":cancel"} {
		status, out2 := call(t, srv, http.MethodPost, vr+verb, "local-user", "", `{}`)
		if status != http.StatusConflict {
			t.Errorf("%s 在终态后应 409，实际 %d: %s", verb, status, out2)
			continue
		}
		if c := codeOf(t, out2); c != "illegal_transition" {
			t.Errorf("%s 错误码应为 illegal_transition，实际 %q", verb, c)
		}
	}
	if n := countRows(t, d, "tasks"); n != 1 {
		t.Errorf("多了任务：%d", n)
	}
	if n := countRows(t, d, "event_journal"); n != 5 {
		t.Errorf("分派腿后一条链应 5 行事件（created+queued+execution_created+preparing+cancelled），实际 %d", n)
	}
}

// 校验分支必须在写库之前挡住，并给出可控的错误码（不能把 SQL 约束文本透出去）。
func TestTaskInputValidation(t *testing.T) {
	srv, d := newTaskHarness(t)

	cases := []struct {
		name, method, path, principal, key, body string
		wantStatus                               int
		wantCode                                 string
	}{
		{"缺 Idempotency-Key", http.MethodPost, "/api/v1/tasks", "local-user", "", `{"title":"x"}`,
			http.StatusForbidden, "idempotency_key_required"},
		{"空标题", http.MethodPost, "/api/v1/tasks", "local-user", "k-a", `{"title":"   "}`,
			http.StatusBadRequest, "validation_failed"},
		{"priority 不在词表", http.MethodPost, "/api/v1/tasks", "local-user", "k-b",
			`{"title":"x","priority":"URGENT"}`, http.StatusBadRequest, "validation_failed"},
		{"body 不是 JSON", http.MethodPost, "/api/v1/tasks", "local-user", "k-c", `not json`,
			http.StatusBadRequest, "validation_failed"},
		// C43 裁乙（T-262）之后 `:queue` 必带 `adapter_key` ⇒ 这两行的 body 必须**合法**，
		// 否则它们测的就变成"缺 key"这一格（同样是 400 的 `queue 的 If-Match 不是数字` 最容易骗人）。
		{"queue 不存在的任务", http.MethodPost, "/api/v1/tasks/nope:queue", "local-user", "", queueBody,
			http.StatusNotFound, "not_found"},
		{"queue 的 If-Match 不是数字", http.MethodPost, "/api/v1/tasks/x:queue", "local-user", "", queueBody,
			http.StatusBadRequest, "validation_failed"},
		// 分派腿解析半边：adapter_key 合法形状但库里没有 ⇒ 404（在碰任何聚合之前）。
		// 用 nope 任务 id 是为了证"解析先于查任务"：若顺序反了这里会回任务的 404 而不是实例的 404，
		// 但错误码相同；真正区分在下面 TestQueueUnknownAdapterKeyIs404BeforeTouchingTask。
		{"queue 的 adapter_key 库里没有", http.MethodPost, "/api/v1/tasks/nope:queue", "local-user", "",
			`{"adapter_key":"no-such-key"}`, http.StatusNotFound, "not_found"},
		// 下面三行是这一格新立的闸：用**不存在的任务 id** 打，是为了证"闸在读库之前"——
		// 若闸排在查询后面，这里会回 404 而不是 400。
		{"queue 没带请求体", http.MethodPost, "/api/v1/tasks/nope:queue", "local-user", "", "",
			http.StatusBadRequest, "validation_failed"},
		{"queue 的 body 里没 adapter_key", http.MethodPost, "/api/v1/tasks/nope:queue", "local-user", "", `{}`,
			http.StatusBadRequest, "validation_failed"},
		{"queue 的 adapter_key 是空白", http.MethodPost, "/api/v1/tasks/nope:queue", "local-user", "",
			`{"adapter_key":"   "}`, http.StatusBadRequest, "validation_failed"},
		{"还没接的动作保持 501", http.MethodPost, "/api/v1/tasks/any:transfer", "local-user", "", "",
			http.StatusNotImplemented, "not_implemented"},
		{"status 不在状态集合", http.MethodGet, "/api/v1/tasks?status=NOPE", "", "", "",
			http.StatusBadRequest, "validation_failed"},
		{"cursor 不合法", http.MethodGet, "/api/v1/tasks?cursor=%21%23", "", "", "",
			http.StatusBadRequest, "validation_failed"},
		{"limit 越界", http.MethodGet, "/api/v1/tasks?limit=9999", "", "", "",
			http.StatusBadRequest, "validation_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader(c.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+testToken)
			if c.principal != "" {
				req.Header.Set("X-Lucero-Principal", c.principal)
			}
			if c.key != "" {
				req.Header.Set("Idempotency-Key", c.key)
			}
			if c.name == "queue 的 If-Match 不是数字" {
				req.Header.Set("If-Match", "abc")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != c.wantStatus {
				t.Fatalf("应 %d，实际 %d: %s", c.wantStatus, resp.StatusCode, b)
			}
			if got := codeOf(t, string(b)); got != c.wantCode {
				t.Errorf("错误码应为 %q，实际 %q", c.wantCode, got)
			}
			if strings.Contains(string(b), "SQLITE") || strings.Contains(string(b), "CONSTRAINT") {
				t.Errorf("把存储层细节透给客户端了: %s", b)
			}
		})
	}
	for _, tbl := range []string{"tasks", "event_journal", "idempotency_keys"} {
		if n := countRows(t, d, tbl); n != 0 {
			t.Errorf("%s 被坏输入写了 %d 行", tbl, n)
		}
	}
}

// TestQueueGateLeavesARealTaskUntouched 是 C43 那一闸的"对真任务也不许做半件事"半边。
// 上面那张表只证到"任务不存在时回 400 而不是 404"（⇒ 闸在查询之前），
// 而这一条证的才是主控要的结局：**任务存在、请求没声明调度对象 ⇒ 状态与版本一步都没走、事件一行都没多**。
// （少这一条，"先跑 Queue 再校验 body 然后回 400"那种写法能骗过整张表。）
func TestQueueGateLeavesARealTaskUntouched(t *testing.T) {
	srv, d := newTaskHarness(t)
	_, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-ak", `{"title":"缺 key 也别动我"}`)
	var created taskJSON
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	vr := "/api/v1/tasks/" + created.ID

	for _, body := range []string{"", `{}`, `{"adapter_key":"   "}`, "not json"} {
		status, resp := call(t, srv, http.MethodPost, vr+":queue", "local-user", "", body)
		if status != http.StatusBadRequest {
			t.Errorf("body=%q 该 400（C43：adapter_key 必填），实际 %d: %s", body, status, resp)
		} else if c := codeOf(t, resp); c != "validation_failed" {
			t.Errorf("body=%q 错误码该 validation_failed，实际 %q", body, c)
		}
	}
	// `GET /tasks/{id}` 还没接线（501），所以直接读库看这一行有没有被动过 —— 版本没涨是关键那半：
	// 只回 400 但状态已经走了的写法，光看响应是看不出来的。
	var status string
	var version int64
	if err := d.Read.QueryRow(`SELECT status, version FROM tasks WHERE id = ?`, created.ID).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "CREATED" || version != 1 {
		t.Errorf("四次被拒的 queue 之后任务竟然动了: %s v%d", status, version)
	}
	if n := countEventsOfType(t, d, "task_queued"); n != 0 {
		t.Errorf("被挡掉的调用写出了 %d 行 task_queued", n)
	}
	if n := countRows(t, d, "event_journal"); n != 1 {
		t.Errorf("除了建任务那一行，被挡掉的调用又写了事件: %d 行", n)
	}

	// 正向对照：带上 key 就走得通（否则上面那四次"全 400"也可以靠"这个端点永远 400"骗过去）。
	// 分派腿一笔事务落三行事件：task_queued + execution_created + task_preparing。
	if status, resp := call(t, srv, http.MethodPost, vr+":queue", "local-user", "", queueBody); status != http.StatusOK {
		t.Fatalf("带 adapter_key 的 queue 该 200，实际 %d: %s", status, resp)
	}
	if n := countRows(t, d, "event_journal"); n != 4 {
		t.Errorf("成功那一跳该恰好多三行事件（共 4），实际 %d 行", n)
	}
	if n := countRows(t, d, "executions"); n != 1 {
		t.Errorf("成功 queue 应恰建 1 条 Execution，实际 %d", n)
	}
}

// TestQueueUnknownAdapterKeyIs404BeforeTouchingTask 钉分派腿解析的失败格：
// adapter_key 库里没有 ⇒ 404，且在碰任何聚合之前返回 —— 任务状态/版本/事件一行都不许动。
// （与 TestQueueGateLeavesARealTaskUntouched 的 400 闸是两格：那个是"没声明调度对象"，
// 这个是"声明了但解析不出实例"。）
func TestQueueUnknownAdapterKeyIs404BeforeTouchingTask(t *testing.T) {
	srv, d := newTaskHarness(t)
	_, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-ak404", `{"title":"解析不到别动我"}`)
	var created taskJSON
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	vr := "/api/v1/tasks/" + created.ID

	status, resp := call(t, srv, http.MethodPost, vr+":queue", "local-user", "",
		`{"adapter_key":"no-such-key"}`)
	if status != http.StatusNotFound {
		t.Fatalf("库里没有的 adapter_key 该 404，实际 %d: %s", status, resp)
	} else if c := codeOf(t, resp); c != "not_found" {
		t.Errorf("错误码该 not_found，实际 %q", c)
	}
	var st string
	var version int64
	if err := d.Read.QueryRow(`SELECT status, version FROM tasks WHERE id = ?`, created.ID).Scan(&st, &version); err != nil {
		t.Fatal(err)
	}
	if st != "CREATED" || version != 1 {
		t.Errorf("被 404 挡掉的 queue 不该动任务: %s v%d", st, version)
	}
	if n := countEventsOfType(t, d, "task_queued"); n != 0 {
		t.Errorf("被挡的调用写出了 %d 行 task_queued", n)
	}
	if n := countRows(t, d, "executions"); n != 0 {
		t.Errorf("被挡的调用建出了 %d 条 Execution", n)
	}
	if n := countRows(t, d, "event_journal"); n != 1 {
		t.Errorf("除建任务那一行外不该多事件，实际 %d 行", n)
	}
}

// 翻页不重不漏（契约③ §2：游标是 keyset，不许用 offset）。
func TestTaskListCursorNoGapsNoDupes(t *testing.T) {
	srv, _ := newTaskHarness(t)
	for i := 1; i <= 5; i++ {
		body := fmt.Sprintf(`{"title":"任务%d","priority":"NORMAL"}`, i)
		if status, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user",
			fmt.Sprintf("page-k%d", i), body); status != http.StatusCreated {
			t.Fatalf("建第 %d 条失败：%d %s", i, status, out)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 6; page++ {
		path := "/api/v1/tasks?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		status, out := call(t, srv, http.MethodGet, path, "", "", "")
		if status != http.StatusOK {
			t.Fatalf("第 %d 页应 200，实际 %d: %s", page, status, out)
		}
		var result struct {
			Items      []taskJSON `json:"items"`
			NextCursor string     `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Items) > 2 {
			t.Fatalf("limit 没生效：%d 条", len(result.Items))
		}
		for _, it := range result.Items {
			if seen[it.ID] {
				t.Fatalf("游标翻页把 %s 重复送出来了", it.ID)
			}
			seen[it.ID] = true
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	if len(seen) != 5 {
		t.Errorf("五页内应取完 5 条，实际 %d：%v", len(seen), seen)
	}
}
