package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// newSyncHarness 与 inbox 那两口的搭法相同（真库真表），但**只挂 sessions 这一口**：
// 最后那条"没注入就维持 501"的用例要把同一个开关关掉再走一遍。
func newSyncHarness(t *testing.T, withSessions bool) (*httptest.Server, *db.DB) {
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
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances
		(id, adapter_key, display_name, created_at, updated_at) VALUES ('ad-1', 'codex', 'Codex', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	cfg := httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            task.NewService(nil),
		Store:            d,
		Notifications:    notification.NewRepository(d.Read),
	}
	if withSessions {
		cfg.Sessions = session.NewService(session.NewRepository(d.Read))
	}
	srv := httptest.NewServer(httpapi.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv, d
}

// seedSyncSession 摆一条 Session：cursor/confidence 传空串就是写 NULL（从未同步过的那个语义），
// withMessage=false 则本地一条消息都没有。
func seedSyncSession(t *testing.T, d *db.DB, id, state, cursor, confidence string, withMessage bool) {
	t.Helper()
	var c any
	if cursor != "" {
		c = cursor
	}
	var conf any = confidence
	if conf == "" {
		conf = "UNKNOWN"
	}
	if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, sync_state, sync_cursor,
		state_confidence, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 1, 1)`,
		id, "ad-1", state, c, conf); err != nil {
		t.Fatal(err)
	}
	if withMessage {
		if _, err := d.Read.Exec(`INSERT INTO messages (id, session_id, role, kind, sequence_no,
			text_content, created_at) VALUES (?, ?, 'USER', 'MESSAGE', 1, '一句', 1)`, id+"-m", id); err != nil {
			t.Fatal(err)
		}
	}
}

func syncPost(t *testing.T, srv *httptest.Server, path string) (int, map[string]any, string) {
	t.Helper()
	code, body := call(t, srv, "POST", path, "local-user", "idem-sync-1", "")
	var m map[string]any
	if code == 200 {
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("响应不是合法 JSON: %q (%v)", body, err)
		}
	}
	return code, m, body
}

// TestSyncDegradesAndEchoesStoredPosition 是 C25"先试只读链"那一支的正证：
// 降级要**明写在响应里**（DEGRADED + LOW），位置原样回显，且这条口今天一行都不写。
func TestSyncDegradesAndEchoesStoredPosition(t *testing.T) {
	srv, d := newSyncHarness(t, true)
	seedSyncSession(t, d, "s-1", "SYNCED", "cur-77", "HIGH", true)
	before := countQuery(t, d, `SELECT version * 1000000 + updated_at FROM sessions WHERE id = 's-1'`)
	journalBefore := countQuery(t, d, `SELECT count(*) FROM event_journal`)

	code, m, body := syncPost(t, srv, "/api/v1/sessions/s-1:sync")
	if code != 200 {
		t.Fatalf("只读链能走就该 200，实际 %d %s", code, body)
	}
	if m["sync_state"] != "DEGRADED" {
		t.Errorf("降级没明写：sync_state=%#v", m["sync_state"])
	}
	// 库里存的 HIGH 不许端出去：这次回答只读了本地字节，不是今天的远端置信度。
	if m["state_confidence"] != "LOW" {
		t.Errorf("state_confidence=%#v，降级应答该是 LOW", m["state_confidence"])
	}
	if m["next_cursor"] != "cur-77" {
		t.Errorf("next_cursor=%#v，该原样回显库里的位置", m["next_cursor"])
	}
	// REQ-F-21 ②：这条口不知道新增几条，那就**不出这个键** —— 出 0 就是拿 0 冒充"不知道"。
	if _, ok := m["added_messages"]; ok {
		t.Errorf("响应里出现了 added_messages=%#v（§12 禁止用 0 表示不知道，而这里连 0 都不该有）", m["added_messages"])
	}
	if got := countQuery(t, d, `SELECT version * 1000000 + updated_at FROM sessions WHERE id = 's-1'`); got != before {
		t.Errorf("这条口今天不许写聚合行：version/updated_at 的合值从 %d 变成了 %d", before, got)
	}
	if n := countQuery(t, d, `SELECT count(*) FROM event_journal`); n != journalBefore {
		t.Errorf("没推进聚合就不许发事件：event_journal 从 %d 行变成 %d 行", journalBefore, n)
	}
}

// TestSyncNullCursorMeansNeverSynced 钉 REQ-F-21 ③④：库里 NULL ⇒ 出口必须是 `null` 这个键值，
// 既不是空串（那会被读成"一个合法位置"），也不是省略键（那凑出第三种"没有"）。
func TestSyncNullCursorMeansNeverSynced(t *testing.T) {
	srv, d := newSyncHarness(t, true)
	seedSyncSession(t, d, "s-2", "UNKNOWN", "", "", true)

	code, m, body := syncPost(t, srv, "/api/v1/sessions/s-2:sync")
	if code != 200 {
		t.Fatalf("有消息可给就该 200，实际 %d %s", code, body)
	}
	v, ok := m["next_cursor"]
	if !ok {
		t.Fatalf("next_cursor 缺键（required 里就有它）: %s", body)
	}
	if v != nil {
		t.Errorf("从未同步过该回 null，实际 %#v", v)
	}
}

// TestSyncWithoutAnythingLocalIs503 是 C25 的另一支：本地既没有能回放的消息，也没有位置可回显
// ⇒ "完全无 Provider"，回 503 `adapter_unresponsive`（§15.2 已绑好这个码，不新造）。
// 已知偏差：这支配额里还得同时发 `adapter_downgraded`，今天发不出去，证据与请求见 REQ-B-1。
func TestSyncWithoutAnythingLocalIs503(t *testing.T) {
	srv, d := newSyncHarness(t, true)
	seedSyncSession(t, d, "s-3", "UNKNOWN", "", "", false)

	code, _, body := syncPost(t, srv, "/api/v1/sessions/s-3:sync")
	if code != http.StatusServiceUnavailable || codeOf(t, body) != "adapter_unresponsive" {
		t.Fatalf("该 503 adapter_unresponsive，实际 %d %s", code, body)
	}
	if retryable := mustParseMap(t, body)["error"].(map[string]any)["retryable"]; retryable != true {
		t.Errorf("503 这一码 §15.2 写的是可重试（等恢复），retryable=%#v", retryable)
	}
}

// TestSyncUnknownSessionIs404 钉"对象不存在"与"没有内容可给"的分别：那条线就是 404 与 503 的分别。
func TestSyncUnknownSessionIs404(t *testing.T) {
	srv, d := newSyncHarness(t, true)
	seedSyncSession(t, d, "s-4", "UNKNOWN", "", "", true)

	code, _, body := syncPost(t, srv, "/api/v1/sessions/nope:sync")
	if code != http.StatusNotFound || codeOf(t, body) != "session_not_found" {
		t.Fatalf("该 404 session_not_found，实际 %d %s", code, body)
	}
}

// TestSyncMethodAndUnwiredVerbs 把 C28② 那句通用口径落成三种可观察形状：
// 路径只接受 POST ⇒ 405；路径存在但能力没接线（无 verb 的 GET、没听过的 verb）⇒ 501。
func TestSyncMethodAndUnwiredVerbs(t *testing.T) {
	srv, d := newSyncHarness(t, true)
	seedSyncSession(t, d, "s-5", "SYNCED", "cur-1", "LOW", true)

	code, body := call(t, srv, "GET", "/api/v1/sessions/s-5:sync", "", "", "")
	if code != http.StatusMethodNotAllowed || codeOf(t, body) != "method_not_allowed" {
		t.Errorf("GET :sync 该 405（这条路径不接受 GET），实际 %d %s", code, body)
	}
	code, body = call(t, srv, "GET", "/api/v1/sessions/s-5", "", "", "")
	if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
		t.Errorf("GET /sessions/{id} 该 501（以后会有这条腿，不许说成 405），实际 %d %s", code, body)
	}
	code, body = call(t, srv, "POST", "/api/v1/sessions/s-5:manage", "local-user", "", "")
	if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
		t.Errorf("没听过的 verb 该 501，实际 %d %s", code, body)
	}
	// 畸形形状：空 id、空 verb、多一段路径 —— 都不许摸到 Service（摸到了就是拿一个必然不存在的
	// id 去查库，回一个假装合法的 404）。
	for _, p := range []string{"/api/v1/sessions/:sync", "/api/v1/sessions/s-5:", "/api/v1/sessions/a/b:sync"} {
		code, body = call(t, srv, "POST", p, "local-user", "", "")
		if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
			t.Errorf("%s 该 501，实际 %d %s", p, code, body)
		}
	}
}

// TestSyncStaysUnimplementedWithoutInjection 与另两口同规矩：没注入 ⇒ 维持 catch-all 的 501，不给半套面。
func TestSyncStaysUnimplementedWithoutInjection(t *testing.T) {
	srv, d := newSyncHarness(t, false)
	seedSyncSession(t, d, "s-6", "SYNCED", "cur-6", "LOW", true)

	code, _, body := syncPost(t, srv, "/api/v1/sessions/s-6:sync")
	if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
		t.Fatalf("没注入 Sessions 还挂上口 = 给了前端半套端点，实际 %d %s", code, body)
	}
}
