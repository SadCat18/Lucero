package httpapi_test

// GET /api/v1/sessions/{id}/messages 的读面用例（第六十二批格2）。
// 接线依据是 api-contract-v0.1.md:113~119 原话 —— 生产者 RecordMessage 已落地。
// 专用 harness 注入 SessionMessages：现有 newManageHarness 不注入它，
// 所以 TestMessagesPathIsRecognizedButUnwired（501 文案）继续靠那条绿。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// newMessagesHarness 注入 SessionMessages（与 SessionRead 同一个 Repository 值、两个字段）。
// wired=false 时故意不注入 ⇒ 501 那格由这条口自己给，不借别人的 nil 闸。
func newMessagesHarness(t *testing.T, wired bool) (*httptest.Server, *db.DB) {
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
		VALUES ('ad-m', 'fake', 'Fake', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	repo := session.NewRepository(d.Read)
	cfg := httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            task.NewService(nil),
		TaskList:         task.NewRepository(d.Read),
		Store:            d,
		// SessionRead 无条件注入：让 /sessions/ 前缀被认领，
		// 这样 unwired=false 时 messages 分支的 501 是 SessionMessages 那道 nil 闸给的，
		// 不是"整条路由没挂、落 catch-all"（那样断不出这一格的牙）。
		SessionRead: repo,
	}
	if wired {
		cfg.SessionMessages = repo
	}
	srv := httptest.NewServer(httpapi.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv, d
}

// seedMessagesRow 摆一条 session + n 条消息（id=m-1..n、seq=1..n、created_at=100*i）。
func seedMessagesRow(t *testing.T, d *db.DB, sessID string, n int) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id,
		created_at, updated_at) VALUES (?, 'ad-m', ?, 1, 1)`, sessID, "remote-"+sessID); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		if _, err := d.Read.Exec(`INSERT INTO messages (id, session_id, role, kind, sequence_no,
			text_content, created_at) VALUES (?, ?, 'USER', 'MESSAGE', ?, ?, ?)`,
			sessID+"-m"+string(rune('0'+i)), sessID, i, "第"+string(rune('0'+i))+"句", 100*i); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSessionMessagesListHappyPath 钉 200 形状：items 顺序、必填键、nullable 还原、next_cursor。
func TestSessionMessagesListHappyPath(t *testing.T) {
	srv, d := newMessagesHarness(t, true)
	seedMessagesRow(t, d, "s-ok", 3)

	code, body := call(t, srv, http.MethodGet, "/api/v1/sessions/s-ok/messages", "", "", "")
	if code != http.StatusOK {
		t.Fatalf("该 200，实际 %d %s", code, body)
	}
	var page struct {
		Items []map[string]any `json:"items"`
		Next  string           `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("该 3 条，实际 %d：%s", len(page.Items), body)
	}
	// 顺序按 (created_at, id)：100/200/300。
	for i, m := range page.Items {
		want := float64((i + 1) * 100)
		if m["created_at"] != want {
			t.Errorf("第 %d 条 created_at 该 %v，实际 %v", i, want, m["created_at"])
		}
	}
	// yaml Message required 五键 + nullable 还原。
	first := page.Items[0]
	for _, k := range []string{"id", "session_id", "role", "kind", "created_at",
		"remote_message_id", "sequence_no", "text_content", "synced_at"} {
		if _, ok := first[k]; !ok {
			t.Errorf("Message 缺键 %s：%v", k, first)
		}
	}
	if first["role"] != "USER" || first["kind"] != "MESSAGE" {
		t.Errorf("role/kind 读错: %v", first)
	}
	if first["sequence_no"] != float64(1) {
		t.Errorf("sequence_no 该 1，实际 %v", first["sequence_no"])
	}
	// content / provider_raw 不出键（域不读那两列）。
	if _, ok := first["content"]; ok {
		t.Errorf("content 不该出键（域模型没有它）: %v", first)
	}
	if _, ok := first["provider_raw"]; ok {
		t.Errorf("provider_raw 不该出键: %v", first)
	}
	// 3 < limit 50 ⇒ next_cursor 空串。
	if page.Next != "" {
		t.Errorf("不足一页该 next_cursor=\"\"，实际 %q", page.Next)
	}
}

// TestSessionMessagesEmptyPageIsArray 钉空页 items 必须是 [] 不是 null。
func TestSessionMessagesEmptyPageIsArray(t *testing.T) {
	srv, d := newMessagesHarness(t, true)
	seedMessagesRow(t, d, "s-empty", 0) // 只有 session、零消息

	code, body := call(t, srv, http.MethodGet, "/api/v1/sessions/s-empty/messages", "", "", "")
	if code != http.StatusOK {
		t.Fatalf("该 200，实际 %d %s", code, body)
	}
	if !strings.Contains(body, `"items":[]`) {
		t.Errorf("空页该 items:[]（不是 null、不是省略），实际 %s", body)
	}
	if m := mustParseMap(t, body); m["next_cursor"] != "" {
		t.Errorf("空页 next_cursor 该 \"\"，实际 %v", m["next_cursor"])
	}
}

// TestSessionMessagesCursorPaging 钉 keyset 翻页：limit=2 第一页两条、带 next_cursor，
// 第二页从游标之后接着出。
func TestSessionMessagesCursorPaging(t *testing.T) {
	srv, d := newMessagesHarness(t, true)
	seedMessagesRow(t, d, "s-page", 5)

	code, body := call(t, srv, http.MethodGet, "/api/v1/sessions/s-page/messages?limit=2", "", "", "")
	if code != http.StatusOK {
		t.Fatalf("第一页该 200，实际 %d %s", code, body)
	}
	var page1 struct {
		Items []map[string]any `json:"items"`
		Next  string           `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body), &page1); err != nil {
		t.Fatal(err)
	}
	if len(page1.Items) != 2 || page1.Next == "" {
		t.Fatalf("limit=2 该出 2 条 + 非空游标，实际 %d 条 next=%q：%s", len(page1.Items), page1.Next, body)
	}
	code2, body2 := call(t, srv, http.MethodGet,
		"/api/v1/sessions/s-page/messages?limit=2&cursor="+page1.Next, "", "", "")
	if code2 != http.StatusOK {
		t.Fatalf("第二页该 200，实际 %d %s", code2, body2)
	}
	var page2 struct {
		Items []map[string]any `json:"items"`
		Next  string           `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body2), &page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("第二页该 2 条，实际 %d：%s", len(page2.Items), body2)
	}
	// 不与第一页重叠：第一页最后是 m-2(created 200)，第二页首条应是 300。
	if page2.Items[0]["created_at"] != float64(300) {
		t.Errorf("游标后该从 300 起，实际 %v", page2.Items[0]["created_at"])
	}
}

// TestSessionMessagesAfterSeqFilter 钉 yaml 的增量位 after_seq：sequence_no > ?。
func TestSessionMessagesAfterSeqFilter(t *testing.T) {
	srv, d := newMessagesHarness(t, true)
	seedMessagesRow(t, d, "s-inc", 4)

	code, body := call(t, srv, http.MethodGet, "/api/v1/sessions/s-inc/messages?after_seq=2", "", "", "")
	if code != http.StatusOK {
		t.Fatalf("该 200，实际 %d %s", code, body)
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("after_seq=2 该剩 2 条（seq 3,4），实际 %d：%s", len(page.Items), body)
	}
	if page.Items[0]["sequence_no"] != float64(3) {
		t.Errorf("首条该 seq=3，实际 %v", page.Items[0]["sequence_no"])
	}
}

// TestSessionMessagesValidationGates 钉 400：after_seq 非法、limit 越界、游标畸形。
func TestSessionMessagesValidationGates(t *testing.T) {
	srv, d := newMessagesHarness(t, true)
	seedMessagesRow(t, d, "s-v", 1)

	for _, q := range []string{"?after_seq=-1", "?after_seq=abc", "?limit=0", "?limit=201",
		"?limit=abc", "?cursor=!!!not-base64!!!", "?cursor=" + "YWJj"} { // 最后一个缺 |
		code, body := call(t, srv, http.MethodGet, "/api/v1/sessions/s-v/messages"+q, "", "", "")
		if code != http.StatusBadRequest || codeOf(t, body) != "validation_failed" {
			t.Errorf("%s 该 400 validation_failed，实际 %d %s", q, code, body)
		}
	}
}

// TestSessionMessagesUnknownSessionIs404：session 不存在与"没消息"是两件事。
func TestSessionMessagesUnknownSessionIs404(t *testing.T) {
	srv, _ := newMessagesHarness(t, true)
	code, body := call(t, srv, http.MethodGet, "/api/v1/sessions/s-none/messages", "", "", "")
	if code != http.StatusNotFound || codeOf(t, body) != "session_not_found" {
		t.Errorf("未知 session 该 404 session_not_found，实际 %d %s", code, body)
	}
}

// TestSessionMessagesUnwiredAndNeighbors 钉三格分工：
// 未注入 ⇒ 501（文案含"还没装配"）；POST ⇒ 405；畸形多段形状 ⇒ 501（"形状不认识"）。
func TestSessionMessagesUnwiredAndNeighbors(t *testing.T) {
	// 未注入：同一条 GET 路径回 501 且话术是"还没装配"。
	unwired, _ := newMessagesHarness(t, false)
	if code, body := call(t, unwired, http.MethodGet, "/api/v1/sessions/s-x/messages", "", "", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("未注入 SessionMessages 该 501，实际 %d %s", code, body)
	} else if m := msgOf(t, body); !strings.Contains(m, "还没装配") || strings.Contains(m, "形状") {
		t.Errorf("501 文案该是「还没装配」而不是「形状不认识」，实际 %q", m)
	}

	// 已注入：方法不对 ⇒ 405（能力在、这条路不接受 POST）。
	wired, d := newMessagesHarness(t, true)
	seedMessagesRow(t, d, "s-n", 1)
	if code, body := call(t, wired, http.MethodPost, "/api/v1/sessions/s-n/messages", "local-user", "k", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("POST messages 该 405，实际 %d %s", code, body)
	}

	// 畸形多段形状仍不认（只放开 {id}/messages 这一条）。
	for _, p := range []string{"/api/v1/sessions/a/b/messages", "/api/v1/sessions/a:sync/messages"} {
		if code, body := call(t, wired, http.MethodGet, p, "", "", ""); code != http.StatusNotImplemented ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("畸形 %s 该 501，实际 %d %s", p, code, body)
		} else if m := msgOf(t, body); !strings.Contains(m, "形状") {
			t.Errorf("畸形 %s 文案该是「形状不认识」，实际 %q", p, m)
		}
	}
}
