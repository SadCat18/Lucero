package httpapi_test

import (
	"bytes"
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
	"github.com/SadCat18/lucero/core/internal/eventjournal"
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
	ready := func() string { return httpapi.StateReady }
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Token: testToken, Version: "test-1", State: ready,
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            task.NewService(nil),
		TaskList:         task.NewRepository(d.Read),
		Store:            d,
	}))
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

// mustParseMap 把成功响应读成 map，供"两次响应语义相同"这类断言用（键序不参与比较）。
func mustParseMap(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("响应不是合法 JSON 对象: %q (%v)", body, err)
	}
	return m
}

// requireCatalogued 与 internal/task 的 write_test.go 里那份是同一句话，故意各写一遍：
// 项目明令禁止 internal/common、shared、utils 这类口袋包（内部契约 §1.1），
// 所以跨包的测试小工具只能复制，不能建第三个包。
// 缺的是 C2 那五个事件类型没进 db 的注册表 → REQ-DB-3；补齐后这些 Skip 自动变成真跑。
func requireCatalogued(t *testing.T, types ...string) {
	t.Helper()
	for _, s := range types {
		if !eventjournal.Registered(s) {
			t.Skipf("%q 还没进 internal/eventjournal 的注册表（REQ-DB-3 等 db），写它会被 §9.1 判 P0", s)
		}
	}
}

func countRows(t *testing.T, d *db.DB, table string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
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
	if status, out = call(t, srv, http.MethodPost, "/api/v1/tasks/"+id+":queue", "local-user", "", ""); status != http.StatusOK {
		t.Fatalf("queue 应 200，实际 %d: %s", status, out)
	}
	var q struct {
		Status  string `json:"status"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		t.Fatal(err)
	}
	if q.Status != "QUEUED" || q.Version != 2 {
		t.Errorf("queue 后应 QUEUED v2，实际 %+v", q)
	}
	if status, out = call(t, srv, http.MethodPost, "/api/v1/tasks/"+id+":queue", "local-user", "", ""); status != http.StatusConflict {
		t.Errorf("重复 queue 应 409，实际 %d: %s", status, out)
	} else if c := codeOf(t, out); c != "illegal_transition" {
		t.Errorf("错误码应为 illegal_transition，实际 %q", c)
	}

	stale := `{"title":"并发","priority":"NORMAL"}`
	if status, out = call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-2", stale); status != http.StatusCreated {
		t.Fatalf("建第二条应 201，实际 %d: %s", status, out)
	}
	var second taskJSON
	if err := json.Unmarshal([]byte(out), &second); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/tasks/"+second.ID+":queue", bytes.NewReader(nil))
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

	if status, out = call(t, srv, http.MethodGet, "/api/v1/tasks?status=QUEUED", "", "", ""); status != http.StatusOK {
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
			t.Errorf("status=QUEUED 列表不对: %s", out)
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

	// 合法暂停的真实行为也钉一条，免得"接好了"与"接好了但被平台层拒"这两种状态在账上长得一样。
	// task_paused 还没进 db 的注册表（REQ-DB-3）→ 只能是 500 internal_error：
	// 既不假装成功，也不假装成 501 未实现。db 补齐后这一支自动翻成 200 的断言。
	if _, out2 := call(t, srv, http.MethodPost, vr+":queue", "local-user", "", ""); out2 == "" {
		t.Fatal("queue 没返回内容")
	}
	if eventjournal.Registered(task.EventPaused) {
		if status, out3 := call(t, srv, http.MethodPost, vr+":pause", "local-user", "", `{"reason":"注册表已补"}`); status != http.StatusOK {
			t.Errorf("注册表已补，pause 应 200，实际 %d: %s", status, out3)
		}
	} else {
		status, out3 := call(t, srv, http.MethodPost, vr+":pause", "local-user", "", `{"reason":"注册表已补"}`)
		if status != http.StatusInternalServerError {
			t.Errorf("REQ-DB-3 未落地时 pause 应老实报 500，实际 %d: %s", status, out3)
		}
		if c := codeOf(t, out3); c != "internal_error" {
			t.Errorf("错误码应为 internal_error，实际 %q", c)
		}
		if strings.Contains(out3, "eventjournal") || strings.Contains(out3, "catalogue") || strings.Contains(out3, "SQLITE") {
			t.Errorf("把平台层细节透给客户端了: %s", out3)
		}
	}
}

// TestPauseResumeRoundTripOverHTTP 是 :pause→:resume 的端到端一条。
// 与 internal/task 那边同一句话：task_paused / task_resumed 还没进注册表（REQ-DB-3），
// 所以这一段现在会 Skip；db 补齐后自动开始跑。跳过时**不**算交付完成，见 progress 第 78 条。
func TestPauseResumeRoundTripOverHTTP(t *testing.T) {
	requireCatalogued(t, task.EventPaused, task.EventResumed)
	srv, _ := newTaskHarness(t)
	_, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "k-p", `{"title":"暂停一下"}`)
	var created taskJSON
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	vr := "/api/v1/tasks/" + created.ID
	if status, out2 := call(t, srv, http.MethodPost, vr+":queue", "local-user", "", ""); status != http.StatusOK {
		t.Fatalf("queue 应 200，实际 %d: %s", status, out2)
	}
	// QUEUED→PAUSED 合法（§5.1 有这条边）。
	if status, out2 := call(t, srv, http.MethodPost, vr+":pause", "local-user", "", `{"reason":"先歇会儿"}`); status != http.StatusOK {
		t.Fatalf("pause 应 200，实际 %d: %s", status, out2)
	} else if m := mustParseMap(t, out2); m["status"] != "PAUSED" || m["version"] != float64(3) {
		t.Errorf("pause 响应不对: %v", m)
	}
	if status, out2 := call(t, srv, http.MethodPost, vr+":resume", "local-user", "", `{"to":"QUEUED"}`); status != http.StatusOK {
		t.Fatalf("resume 应 200，实际 %d: %s", status, out2)
	} else if m := mustParseMap(t, out2); m["status"] != "QUEUED" || m["version"] != float64(4) {
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
	if status, out2 := call(t, srv, http.MethodPost, vr+":queue", "local-user", "", ""); status != http.StatusOK {
		t.Fatalf("queue 应 200，实际 %d: %s", status, out2)
	}
	if status, out2 := call(t, srv, http.MethodPost, vr+":cancel", "local-user", "", `{"reason":"不做了"}`); status != http.StatusOK {
		t.Fatalf("cancel 应 200，实际 %d: %s", status, out2)
	} else if m := mustParseMap(t, out2); m["status"] != "CANCELLED" || m["version"] != float64(3) {
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
	if n := countRows(t, d, "event_journal"); n != 3 {
		t.Errorf("一个版本一行事件，应为 3 行，实际 %d", n)
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
		{"queue 不存在的任务", http.MethodPost, "/api/v1/tasks/nope:queue", "local-user", "", "",
			http.StatusNotFound, "not_found"},
		{"queue 的 If-Match 不是数字", http.MethodPost, "/api/v1/tasks/x:queue", "local-user", "", "",
			http.StatusBadRequest, "validation_failed"},
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
