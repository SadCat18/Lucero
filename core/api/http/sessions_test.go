package httpapi_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
func newSyncHarness(t *testing.T, withSessions bool, withList ...bool) (*httptest.Server, *db.DB) {
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
	if len(withList) > 0 && withList[0] {
		cfg.SessionRead = session.NewRepository(d.Read)
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

// seedForList 摆三条 Session：s-l1 挂在项目上且有游标、s-l2 无项目无游标（NULL 两格）、
// s-l3 有游标。另建一条真 projects 行喂外键（不是"为了测试导出私有面"，是真外键要求）。
func seedForList(t *testing.T, d *db.DB) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO projects (id, name, created_at, updated_at)
		VALUES ('proj-1', '列表用的项目', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	rows := [][3]string{
		{"s-l1", "proj-1", "cur-11"},
		{"s-l2", "", ""},
		{"s-l3", "proj-1", "cur-33"},
	}
	for i, r := range rows {
		var proj, cursor any
		if r[1] != "" {
			proj = r[1]
		}
		if r[2] != "" {
			cursor = r[2]
		}
		if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, project_id, sync_state,
			sync_cursor, state_confidence, created_at, updated_at)
			VALUES (?, 'ad-1', ?, 'SYNCED', ?, 'LOW', ?, ?)`, r[0], proj, cursor, i+1, i+1); err != nil {
			t.Fatal(err)
		}
	}
}

func listGet(t *testing.T, srv *httptest.Server, q string) (int, map[string]any, string) {
	t.Helper()
	code, body := call(t, srv, "GET", "/api/v1/sessions"+q, "", "", "")
	var m map[string]any
	if code == 200 {
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("列表响应不是合法 JSON: %q (%v)", body, err)
		}
	}
	return code, m, body
}

// TestListSessionsShapeAndFilter 钉这条口的三件事：required 六键一个不许缺、
// **NULL 与空串分开**（sync_cursor / project_id 两格），以及契约给的 project 过滤位真在过滤。
func TestListSessionsShapeAndFilter(t *testing.T) {
	srv, d := newSyncHarness(t, false, true)
	defer srv.Close()
	// 先空表走一遍：`items` 必须是 []，不是 null（同一件事两种写法就是 §12 要收的形状）。
	_, empty, body := listGet(t, srv, "")
	if items, ok := empty["items"].([]any); !ok || items == nil {
		t.Fatalf("空表应回 items: []，实际 %s", body)
	}
	seedForList(t, d)
	code, m, body := listGet(t, srv, "?limit=10")
	if code != 200 {
		t.Fatalf("应 200，实际 %d %s", code, body)
	}
	items := m["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("无过滤应回三条，实际 %d: %s", len(items), body)
	}
	byID := map[string]map[string]any{}
	for _, it := range items {
		o := it.(map[string]any)
		byID[o["id"].(string)] = o
	}
	for _, k := range []string{"id", "adapter_instance_id", "managed", "sync_state", "state_confidence", "sync_cursor"} {
		if _, ok := byID["s-l1"][k]; !ok {
			t.Errorf("Session.required 里的 %q 在出口缺键", k)
		}
	}
	if v := byID["s-l2"]["sync_cursor"]; v != nil {
		t.Errorf("从未同步过该是 null，实际 %#v", v)
	}
	if v := byID["s-l1"]["sync_cursor"]; v != "cur-11" {
		t.Errorf("sync_cursor=%#v，该原样带出库里那个位置", v)
	}
	if v := byID["s-l2"]["project_id"]; v != nil {
		t.Errorf("库里 NULL 的 project_id 该是 null 而不是空串，实际 %#v", v)
	}
	// Repository 根本不读 metadata / provider_status_raw：视图里不许出现编出来的空对象。
	for _, k := range []string{"metadata", "provider_status_raw", "archived_at"} {
		if _, ok := byID["s-l1"][k]; ok {
			t.Errorf("出口出现了没读过的键 %q（把「没读」报成「是空的」）", k)
		}
	}
	code, m, body = listGet(t, srv, "?project=proj-1&limit=10")
	if code != 200 || len(m["items"].([]any)) != 2 {
		t.Errorf("契约给的 project 过滤位没生效：%d %s", code, body)
	}
}

// TestListSessionsPagesAndGates 钉翻页与入口有界检查（含跨出口游标互不吃）。
func TestListSessionsPagesAndGates(t *testing.T) {
	srv, d := newSyncHarness(t, false, true)
	defer srv.Close()
	seedForList(t, d)

	_, m, body := listGet(t, srv, "?limit=2")
	nc, ok := m["next_cursor"].(string)
	if !ok || nc == "" {
		t.Fatalf("取满一页必须有游标（分页口径里它是字符串）: %s", body)
	}
	got := []string{}
	for _, it := range m["items"].([]any) {
		got = append(got, it.(map[string]any)["id"].(string))
	}
	code2, m2, body2 := listGet(t, srv, "?limit=2&cursor="+nc)
	if code2 != 200 {
		t.Fatalf("第二页该 200，实际 %d %s", code2, body2)
	}
	for _, it := range m2["items"].([]any) {
		got = append(got, it.(map[string]any)["id"].(string))
	}
	if strings.Join(got, ",") != "s-l1,s-l2,s-l3" {
		t.Errorf("两页拼起来不对: %v（第二页 next_cursor=%q）", got, m2["next_cursor"])
	}
	if m2["next_cursor"] != "" {
		t.Errorf("短页的 next_cursor 该是空串（REQ-F-19 唯一写法），实际 %#v", m2["next_cursor"])
	}
	for _, q := range []string{"?limit=0", "?limit=201", "?limit=x", "?cursor=not-base64!!",
		"?cursor=" + base64.RawURLEncoding.EncodeToString([]byte("1|400|n-1"))} {
		code, _, b := listGet(t, srv, q)
		if code != 400 || codeOf(t, b) != "validation_failed" {
			t.Errorf("%s 应 400 validation_failed，实际 %d %s", q, code, b)
		}
	}
}

// TestListSessionsUnwiredLegs 钉两格"没接线要说实话"：
// ① 没注入 SessionRead ⇒ GET 走 catch-all 的 501（路由根本没挂，不给半套面）；
// ② 挂上了之后，这条路径不接受的 POST ⇒ 405（C28② 的另一边，不是 501）。
func TestListSessionsUnwiredLegs(t *testing.T) {
	srv, d := newSyncHarness(t, false)
	defer srv.Close()
	seedForList(t, d)

	if code, _, b := listGet(t, srv, ""); code != http.StatusNotImplemented || codeOf(t, b) != "not_implemented" {
		t.Errorf("没注入 SessionRead 的 GET /sessions 该 501，实际 %d %s", code, b)
	}

	wired, wd := newSyncHarness(t, false, true)
	defer wired.Close()
	seedForList(t, wd)
	code, b := call(t, wired, "POST", "/api/v1/sessions", "local-user", "", "")
	if code != http.StatusMethodNotAllowed || codeOf(t, b) != "method_not_allowed" {
		t.Errorf("挂上之后 POST /api/v1/sessions 该 405（这条路径只接受 GET），实际 %d %s", code, b)
	}
}

// TestGetSessionSingleMatchesListItemShape 钉单条读的两件事：required 六键齐、
// 且**同一条会话在列表出口与单条出口键集合一模一样**（同一件事两种写法就是要收的那类形状）。
func TestGetSessionSingleMatchesListItemShape(t *testing.T) {
	srv, d := newSyncHarness(t, false, true)
	defer srv.Close()
	seedForList(t, d)

	code, body := call(t, srv, "GET", "/api/v1/sessions/s-l1", "", "", "")
	if code != 200 {
		t.Fatalf("读一条已知会话该 200，实际 %d %s", code, body)
	}
	one := mustParseMap(t, body)
	for _, k := range []string{"id", "adapter_instance_id", "managed", "sync_state", "state_confidence", "sync_cursor"} {
		if _, ok := one[k]; !ok {
			t.Errorf("Session.required 里的 %q 在单条出口缺键: %s", k, body)
		}
	}
	if one["id"] != "s-l1" || one["sync_cursor"] != "cur-11" || one["project_id"] != "proj-1" {
		t.Errorf("单条读回来的身份/位置/项目不对: %v", one)
	}
	// NULL 那一条：游标必须是 null，不是空串、不是缺键。
	_, nilCursor := callOne(t, srv, "/api/v1/sessions/s-l2")
	v, ok := nilCursor["sync_cursor"]
	if !ok || v != nil {
		t.Errorf("从未同步过该回 null，实际 ok=%v %#v", ok, v)
	}
	// 与列表出口逐键对齐。
	_, listed := callOne(t, srv, "/api/v1/sessions?limit=10")
	items := listed["items"].([]any)
	var inList map[string]any
	for _, it := range items {
		if m := it.(map[string]any); m["id"] == "s-l1" {
			inList = m
		}
	}
	if inList == nil {
		t.Fatalf("列表里找不到 s-l1: %v", listed)
	}
	if keySet(inList) != keySet(one) {
		t.Errorf("同一条会话两条出口键集合不一样：单条=%s 列表=%s", keySet(one), keySet(inList))
	}
}

// TestGetSessionUnknownIs404 与 :sync 那条同码：库里没有就是 404 `session_not_found`，
// 不拿 501（"没接线"）也不拿 200 + 空对象糊过去。
func TestGetSessionUnknownIs404(t *testing.T) {
	srv, d := newSyncHarness(t, false, true)
	defer srv.Close()
	seedForList(t, d)

	code, body := call(t, srv, "GET", "/api/v1/sessions/s-none", "", "", "")
	if code != http.StatusNotFound || codeOf(t, body) != "session_not_found" {
		t.Fatalf("该 404 session_not_found，实际 %d %s", code, body)
	}
}

// TestGetSessionMethodAndHalfWiredGates 钉两格分流：
// ① 单条这条路径不接受 POST → 405；② 只装读通道、没装 :sync 的业务面 → POST :sync 是 501，
// 而不是因为"另一格装上了"就回 405 说这条路不接受 POST。
func TestGetSessionMethodAndHalfWiredGates(t *testing.T) {
	srv, d := newSyncHarness(t, false, true)
	defer srv.Close()
	seedForList(t, d)

	if code, body := call(t, srv, "POST", "/api/v1/sessions/s-l1", "local-user", "k-1", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("POST /sessions/{id} 该 405，实际 %d %s", code, body)
	}
	if code, body := call(t, srv, "POST", "/api/v1/sessions/s-l1:sync", "local-user", "k-2", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("只装读通道时 POST :sync 该 501（能力没接线），实际 %d %s", code, body)
	}

	// 畸形形状必须被形状守卫拦下（这里读通道已注入，所以 501 只能来自守卫本身）。
	// 这一格是补出来的：变异把守卫拆掉时原来一点不红 —— 因为那几条断言当时待在**只装了 :sync 的 harness** 里，
	// 501 其实来自 `SessionRead == nil` 那道闸，守卫拆了也没人发现（观察面选错了，不是断言弱）。
	for _, path := range []string{"/api/v1/sessions/:sync", "/api/v1/sessions/s-l1:", "/api/v1/sessions/a/b",
		"/api/v1/sessions/a/b:sync"} {
		c2, b2 := call(t, srv, "GET", path, "", "", "")
		if c2 != http.StatusNotImplemented || codeOf(t, b2) != "not_implemented" {
			t.Errorf("%s 该被形状守卫拦成 501，实际 %d %s", path, c2, b2)
		}
		c3, b3 := call(t, srv, "POST", path, "local-user", "k-shape", "")
		if c3 != http.StatusNotImplemented || codeOf(t, b3) != "not_implemented" {
			t.Errorf("POST %s 同样该 501（不许拿一个必然不存在的 id 去查库、回一个假合法的 404），实际 %d %s", path, c3, b3)
		}
	}
}

// callOne 只要"200 就给我 map"，用在同一条用例里连读几次的场合（列表与单条要一起看）。
func callOne(t *testing.T, srv *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	code, body := call(t, srv, "GET", path, "", "", "")
	if code != 200 {
		t.Fatalf("GET %s 应 200，实际 %d %s", path, code, body)
	}
	return code, mustParseMap(t, body)
}
