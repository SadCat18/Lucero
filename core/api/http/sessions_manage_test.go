package httpapi_test

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

// newManageHarness 与 newSyncHarness 的区别只有一件：**它把写侧那一格也注入**，
// 所以"未装配 ⇒ 501"那格必须由 `wired=false` 单独跑一遍。
// 这条规矩是第三十五批那处 SILENT 换来的：断言如果待在"另一道 nil 闸就会给出同样答案"的 harness 里，
// 变异不咬不是因为判据对，是因为它压根没在判这一格。
func newManageHarness(t *testing.T, wired bool) (*httptest.Server, *db.DB) {
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
	repo := session.NewRepository(d.Read)
	cfg := httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            task.NewService(nil),
		TaskList:         task.NewRepository(d.Read),
		Store:            d,
		Sessions:         session.NewService(repo),
		SessionRead:      repo,
	}
	if wired {
		cfg.SessionManage = session.NewService(repo)
	}
	srv := httptest.NewServer(httpapi.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv, d
}

func seedLiveSession(t *testing.T, d *db.DB, id string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES (?, ?, ?, 1, 1)`, "ai-"+id, "key-"+id, id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, managed, sync_state, state_confidence,
		created_at, updated_at) VALUES (?, ?, 0, 'UNKNOWN', 'UNKNOWN', 1, 1)`, id, "ai-"+id); err != nil {
		t.Fatal(err)
	}
}

func sessionRow(t *testing.T, d *db.DB, id string) (managed int, project any, version int64) {
	t.Helper()
	if err := d.Read.QueryRow(`SELECT managed, project_id, version FROM sessions WHERE id = ?`, id).
		Scan(&managed, &project, &version); err != nil {
		t.Fatal(err)
	}
	return
}

func managedEventRows(t *testing.T, d *db.DB, id string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type = 'session_managed' AND aggregate_id = ?`, id).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSessionManageOverHTTP(t *testing.T) {
	srv, d := newManageHarness(t, true)
	seedLiveSession(t, d, "sh-1")

	code, body := call(t, srv, "POST", "/api/v1/sessions/sh-1:manage", "local-user", "k-1", `{"managed":true}`)
	if code != http.StatusOK {
		t.Fatalf("该 200，实际 %d %s", code, body)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	if v["id"] != "sh-1" || v["managed"] != true {
		t.Errorf("响应该是读回的那行（managed=true），实际 %s", body)
	}
	// yaml 的 Session **required 六格**（`:982` 那一张的 required 列，我一开始写成"看着眼熟的八个"、被原文纠正）：
	// `sync_cursor` 必须在键里且可以是 null —— REQ-F-21 说的是"不许省略"，不是"不许为空"。
	for _, k := range []string{"id", "adapter_instance_id", "managed", "sync_state", "state_confidence", "sync_cursor"} {
		if _, ok := v[k]; !ok {
			t.Errorf("Session 的必填键 %s 不在响应里: %s", k, body)
		}
	}
	if managed, _, version := sessionRow(t, d, "sh-1"); managed != 1 || version != 2 {
		t.Errorf("库里该是 managed=1 v2，实际 %d v%d", managed, version)
	}
	if n := managedEventRows(t, d, "sh-1"); n != 1 {
		t.Errorf("恰一行 session_managed，实际 %d", n)
	}

	// 同值再打：还是 200，但库里一切不动（幂等点击不是错误）。
	code2, body2 := call(t, srv, "POST", "/api/v1/sessions/sh-1:manage", "local-user", "k-2", `{"managed":true}`)
	if code2 != http.StatusOK {
		t.Errorf("重复 :manage 该 200，实际 %d %s", code2, body2)
	}
	if managed, _, version := sessionRow(t, d, "sh-1"); managed != 1 || version != 2 {
		t.Errorf("重复调用该什么都不动，实际 %d v%d", managed, version)
	}
	if n := managedEventRows(t, d, "sh-1"); n != 1 {
		t.Errorf("第二次不许再发事件，实际 %d 行", n)
	}
}

func TestSessionManageRequiresManaged(t *testing.T) {
	srv, d := newManageHarness(t, true)
	seedLiveSession(t, d, "sh-2")
	for _, b := range []string{`{}`, `{"project_id":"p1"}`, `{"managed":"yes"}`} {
		code, body := call(t, srv, "POST", "/api/v1/sessions/sh-2:manage", "local-user", "k", b)
		if code != http.StatusBadRequest || codeOf(t, body) != "validation_failed" {
			t.Errorf("请求体 %s 该 400 validation_failed，实际 %d %s", b, code, body)
		}
	}
	if managed, _, version := sessionRow(t, d, "sh-2"); managed != 0 || version != 1 {
		t.Errorf("被拒的三发都不该动库，实际 managed=%d v%d", managed, version)
	}
}

// TestSessionManageRejectsUnknownKeys 是 C35 那条口径在这条口上的落点：
// 未知键**静默丢弃**会让调用方以为改动生效了（比如它写了 `{"managed":true,"status":"SYNCED"}`）。
func TestSessionManageRejectsUnknownKeys(t *testing.T) {
	srv, d := newManageHarness(t, true)
	seedLiveSession(t, d, "sh-3")
	code, body := call(t, srv, "POST", "/api/v1/sessions/sh-3:manage", "local-user", "k",
		`{"managed":true,"status":"SYNCED"}`)
	if code != http.StatusBadRequest || codeOf(t, body) != "validation_failed" {
		t.Errorf("未知键该 400（不静默丢），实际 %d %s", code, body)
	}
	if managed, _, version := sessionRow(t, d, "sh-3"); managed != 0 || version != 1 {
		t.Errorf("400 这一发不该留下半次改动，实际 managed=%d v%d", managed, version)
	}
}

func TestSessionManageUnknownSessionIs404(t *testing.T) {
	srv, d := newManageHarness(t, true)
	code, body := call(t, srv, "POST", "/api/v1/sessions/sh-none:manage", "local-user", "k", `{"managed":true}`)
	if code != http.StatusNotFound || codeOf(t, body) != "session_not_found" {
		t.Errorf("未知会话该 404 session_not_found，实际 %d %s", code, body)
	}
	if n := managedEventRows(t, d, "sh-none"); n != 0 {
		t.Errorf("404 这一发不该留事件，实际 %d", n)
	}
}

func TestSessionManageProjectIDFlow(t *testing.T) {
	srv, d := newManageHarness(t, true)
	seedLiveSession(t, d, "sh-4")
	if _, err := d.Read.Exec(`INSERT INTO projects (id, name, canonical_path, project_type, status,
		metadata_json, created_at, updated_at) VALUES ('prj-4','p','d:\\p\\4','CODE','ACTIVE','{}',1,1)`); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, srv, "POST", "/api/v1/sessions/sh-4:manage", "local-user", "k",
		`{"managed":true,"project_id":"prj-4"}`)
	if code != http.StatusOK {
		t.Fatalf("挂上真项目该 200，实际 %d %s", code, body)
	}
	if _, project, _ := sessionRow(t, d, "sh-4"); project != "prj-4" {
		t.Errorf("库里 project_id 该是 prj-4，实际 %#v", project)
	}
	// 不存在的项目：400，而且**一整发都不写**（预检在写之前，不是写一半再回滚）。
	code2, body2 := call(t, srv, "POST", "/api/v1/sessions/sh-4:manage", "local-user", "k2",
		`{"managed":false,"project_id":"prj-none"}`)
	if code2 != http.StatusBadRequest || codeOf(t, body2) != "validation_failed" {
		t.Errorf("未知项目该 400，实际 %d %s", code2, body2)
	}
	if managed, project, version := sessionRow(t, d, "sh-4"); managed != 1 || project != "prj-4" || version != 2 {
		t.Errorf("被拒的那发不该动任何东西（该停在 managed=1/prj-4/v2），实际 %d/%#v/v%d", managed, project, version)
	}
}

// TestSessionManageVerbRouting 钉"接进分流不许弄坏邻居"：:sync / 单条读 / 未知 verb 各回各的。
func TestSessionManageVerbRouting(t *testing.T) {
	srv, d := newManageHarness(t, true)
	seedLiveSession(t, d, "sh-5")

	// 未装配那一支：同一条路径该 501（这是 SessionManage 那道闸给的，不是别人的 nil 闸）。
	unwired, du := newManageHarness(t, false)
	seedLiveSession(t, du, "sh-u")
	if code, body := call(t, unwired, "POST", "/api/v1/sessions/sh-u:manage", "local-user", "k", `{"managed":true}`); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("没装配写侧时 :manage 该 501，实际 %d %s", code, body)
	}
	// 方法不被接受 ⇒ 405。
	if code, body := call(t, srv, "GET", "/api/v1/sessions/sh-5:manage", "", "", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("GET :manage 该 405，实际 %d %s", code, body)
	}
	// 未知 verb 仍走 default 的 501（新增一支不许把兜底挤掉）。
	if code, body := call(t, srv, "POST", "/api/v1/sessions/sh-5:bogus", "local-user", "k", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("未知 verb 该 501，实际 %d %s", code, body)
	}
	// 单条读那条不能被挤坏：GET /sessions/{id} 该 200。
	if code, body := call(t, srv, "GET", "/api/v1/sessions/sh-5", "", "", ""); code != http.StatusOK {
		t.Errorf("GET 单条读该还是 200，实际 %d %s", code, body)
	}
	// :sync 那条也照旧（主控 T-195① 刚纠正过：它早就通到业务层，别当 501 报）。
	if code, body := call(t, srv, "POST", "/api/v1/sessions/sh-5:sync", "local-user", "k-s", ""); code != http.StatusOK &&
		code != http.StatusServiceUnavailable {
		t.Errorf(":sync 该走业务层（200 降级或 503），实际 %d %s", code, body)
	}
}

// TestSessionManageProjectIDThreeStates 钉 C41 裁定里最容易塌的那一层：**"没给这个键"与"给了 null"是两件事**。
// 如果 transport 用 `*string` 解，两者都变成 nil ⇒ "解绑"在 HTTP 面上根本无法表达（而裁定明写 null = 这次没绑/解绑）。
func TestSessionManageProjectIDThreeStates(t *testing.T) {
	srv, d := newManageHarness(t, true)
	seedLiveSession(t, d, "sh-8")
	if _, err := d.Read.Exec(`INSERT INTO projects (id, name, canonical_path, project_type, status,
		metadata_json, created_at, updated_at) VALUES ('prj-8','p','d:\\p\\8','CODE','ACTIVE','{}',1,1)`); err != nil {
		t.Fatal(err)
	}

	// ① 只给 managed：归属这一列不许动，事件里 project_id 该是 null（键在、值可空）。
	if code, body := call(t, srv, "POST", "/api/v1/sessions/sh-8:manage", "local-user", "k1", `{"managed":true}`); code != http.StatusOK {
		t.Fatalf("第一发该 200，实际 %d %s", code, body)
	}
	if _, project, _ := sessionRow(t, d, "sh-8"); project != nil {
		t.Errorf("没提 project_id 就不该动归属，实际 %#v", project)
	}
	if p := lastManagedPayload(t, d, "sh-8"); p["project_id"] != nil {
		t.Errorf("没绑归属时 payload 该发 null，实际 %#v", p)
	}

	// ② 给了值：绑上，payload 里也是那个值。
	if code, body := call(t, srv, "POST", "/api/v1/sessions/sh-8:manage", "local-user", "k2",
		`{"managed":true,"project_id":"prj-8"}`); code != http.StatusOK {
		t.Fatalf("第二发该 200，实际 %d %s", code, body)
	}
	if _, project, _ := sessionRow(t, d, "sh-8"); project != "prj-8" {
		t.Errorf("库里该绑上 prj-8，实际 %#v", project)
	}

	// ③ **再发一次一模一样的**：这必须是 no-op —— 版本不动、事件不多一行。
	// 它同时是"没提 project_id"与"提了 null"的分界探针：如果 transport 把 absent 当成解绑，
	// 这一发就会既改归属又多推一次版本，两个断言当场都红。
	_, _, vBefore := sessionRow(t, d, "sh-8")
	codeN, bodyN := call(t, srv, "POST", "/api/v1/sessions/sh-8:manage", "local-user", "k3", `{"managed":true}`)
	if codeN != http.StatusOK {
		t.Fatalf("同值再打该 200，实际 %d %s", codeN, bodyN)
	}
	managedAfter, projectAfter, vAfter := sessionRow(t, d, "sh-8")
	if vAfter != vBefore {
		t.Errorf("no-op 不该推版本（v%d→v%d），而且归属 %#v", vBefore, vAfter, projectAfter)
	}
	if managedAfter != 1 || projectAfter != "prj-8" {
		t.Errorf("请求里没提 project_id ⇒ 归属不许被动（absent ≠ null），实际 managed=%d project=%#v", managedAfter, projectAfter)
	}
	if n := managedEventRows(t, d, "sh-8"); n != 2 {
		t.Errorf("①②各一次真改动、③是 no-op ⇒ 事件该停在 2 行，实际 %d", n)
	}

	// ④ **显式 null**：这一发才是"解绑"，必须与③区分开（同样的 managed 值，只差那一个键）。
	code3, body3 := call(t, srv, "POST", "/api/v1/sessions/sh-8:manage", "local-user", "k4",
		`{"managed":true,"project_id":null}`)
	if code3 != http.StatusOK {
		t.Fatalf("显式 null 该被当成解绑并接受，实际 %d %s", code3, body3)
	}
	if _, project, _ := sessionRow(t, d, "sh-8"); project != nil {
		t.Errorf("显式 null 该把归属清成 NULL，实际 %#v", project)
	}
	if n := managedEventRows(t, d, "sh-8"); n != 3 {
		t.Errorf("④ 是真改动 ⇒ 事件该到 3 行，实际 %d", n)
	}
	if p := lastManagedPayload(t, d, "sh-8"); p["project_id"] != nil {
		t.Errorf("解绑那发的 payload 该发 null，实际 %#v", p)
	}

	// ⑤ 给了一个非字符串（数字）：400，不许"顺手换算成字符串"。
	if code, body := call(t, srv, "POST", "/api/v1/sessions/sh-8:manage", "local-user", "k5",
		`{"managed":true,"project_id":7}`); code != http.StatusBadRequest || codeOf(t, body) != "validation_failed" {
		t.Errorf("project_id 给数字该 400，实际 %d %s", code, body)
	}
}

func lastManagedPayload(t *testing.T, d *db.DB, id string) map[string]any {
	t.Helper()
	var raw string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal
		WHERE event_type = 'session_managed' AND aggregate_id = ? ORDER BY seq DESC LIMIT 1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("payload 不是合法 JSON: %v (%s)", err, raw)
	}
	if _, ok := m["project_id"]; !ok {
		t.Errorf("payload 省掉了 project_id 这一键（C41 裁定要求两键都必须在）: %s", raw)
	}
	return m
}

func TestSessionManageIsNotAuthFree(t *testing.T) {
	srv, d := newManageHarness(t, true)
	seedLiveSession(t, d, "sh-6")

	req, err := http.NewRequest("POST", srv.URL+"/api/v1/sessions/sh-6:manage", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("缺口令的 :manage 该 401，实际 %d", resp.StatusCode)
	}
	if managed, _, version := sessionRow(t, d, "sh-6"); managed != 0 || version != 1 {
		t.Errorf("被鉴权挡住的那发不该动库，实际 %d v%d", managed, version)
	}
}

// TestMessagesPathIsRecognizedButUnwired 钉巡检 07:20②那一格：`GET /api/v1/sessions/{id}/messages` 是契约③ §3
// 声明过的路径（yaml 的 `listSessionMessages`），而它以前连 `splitSessionVerb` 都过不去（那条规则是"含斜杠就不认形状"），
// 于是拿到一句「这条路径的形状我不认识」的 501 —— 那是**假话**，同族的旧错在 tasks.go 上是 T-195④ 修的。
// 现在三格分开：认得但没接线 ⇒ 501 `not_implemented`；方法不对 ⇒ 405；**别的**多段形状仍然不认（只放开这一条）。
func TestMessagesPathIsRecognizedButUnwired(t *testing.T) {
	srv, _ := newManageHarness(t, true)
	// 这一格的**实质就是那句话**：状态码与 error.code 在"形状不认识"和"没接线"两格里一模一样（都是 501/not_implemented），
	// 所以只断码的判据对这条缺陷没有牙 —— 要断的是"它说的是不是实话"。
	code, body := call(t, srv, "GET", "/api/v1/sessions/s-1/messages", "", "", "")
	if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
		t.Errorf("messages 这条声明过的路径该 501 not_implemented，实际 %d %s", code, body)
	}
	if m := msgOf(t, body); !strings.Contains(m, "还没装配") || strings.Contains(m, "形状") {
		t.Errorf("理由必须是「读通道没接」而不是「形状不认识」，实际 %q", m)
	}
	for _, m := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		if code, body := call(t, srv, m, "/api/v1/sessions/s-1/messages", "local-user", "k-m", ""); code != http.StatusMethodNotAllowed ||
			codeOf(t, body) != "method_not_allowed" {
			t.Errorf("%s messages 该 405（它只声明 GET），实际 %d %s", m, code, body)
		}
	}
	// 别的多段/畸形形状必须仍然「不认」——认多了就是替别人决定这条路径归谁。
	// （不列 `//messages` 那种：ServeMux 会先清洗路径，守卫看不见双斜杠，那条在 handler_test.go 里另有断言。）
	for _, p := range []string{"/api/v1/sessions/a/b/messages", "/api/v1/sessions/a:sync/messages",
		"/api/v1/sessions/a/messages/1", "/api/v1/sessions/a/messagez"} {
		if code, body := call(t, srv, "GET", p, "", "", ""); code != http.StatusNotImplemented ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("畸形 %s 该 501，实际 %d %s", p, code, body)
		} else if m := msgOf(t, body); !strings.Contains(m, "形状") {
			t.Errorf("畸形 %s 的理由该是「形状不认识」那一格，实际 %q", p, m)
		}
	}
}
