package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"encoding/base64"
	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/project"
	"github.com/SadCat18/lucero/core/internal/task"
	"sort"
)

// newProjectHarness 真库真表 + 真 project.Service：这条口最有价值的证据是
// "canonicalize 后冲突判 409" 与"首次响应等于库里那一行"，两条都得跑真写路径。
// newProjectHarness 默认**不**注入 ProjectRead（那正是第二十九批钉的"缺装配⇒GET 501"那一格）；
// 传 withRead 才挂上读通道。用变参而不是复制整个 harness：那 30 行装配重复两份迟早会漂。
func newProjectHarness(t *testing.T, withRead ...bool) (*httptest.Server, *db.DB) {
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
	for _, pr := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
		if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, pr[0], pr[1], pr[0]); err != nil {
			t.Fatal(err)
		}
	}
	cfg := httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            task.NewService(nil),
		TaskList:         task.NewRepository(d.Read),
		Store:            d,
		Notifications:    notification.NewRepository(d.Read),
		Projects:         project.NewService(nil),
	}
	if len(withRead) > 0 && withRead[0] {
		cfg.ProjectRead = d.Read
	}
	srv := httptest.NewServer(httpapi.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv, d
}

func post(t *testing.T, srv *httptest.Server, key, body string) (int, string) {
	t.Helper()
	return call(t, srv, http.MethodPost, "/api/v1/projects", "local-user", key, body)
}

func TestCreateProjectOverHTTP(t *testing.T) {
	srv, d := newProjectHarness(t)

	code, body := post(t, srv, "k-p1", `{"name":"主仓","canonical_path":"d:/project/lucero","project_type":"CODE","metadata":{"tier":"a"}}`)
	if code != http.StatusCreated {
		t.Fatalf("应 201，实际 %d: %s", code, body)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "name", "status", "version", "created_at", "updated_at"} {
		if _, ok := v[k]; !ok {
			t.Errorf("Project.required 里的 %q 在响应里缺键: %s", k, body)
		}
	}
	if v["status"] != "ACTIVE" || fmt.Sprint(v["version"]) != "1" {
		t.Errorf("默认值不对: %s", body)
	}
	if got, want := v["canonical_path"], project.Canonicalize("d:/project/lucero"); got != want {
		t.Errorf("canonical_path 应是收好的键 %v，实际 %v", want, got)
	}
	if md, ok := v["metadata"].(map[string]any); !ok || md["tier"] != "a" {
		t.Errorf("metadata 没按 JsonMap 出对象: %v", v["metadata"])
	}
	for _, k := range []string{"trust_domain_id", "archived_at"} {
		if val, present := v[k]; !present || val != nil {
			t.Errorf("%s 应为显式 null（实际 present=%v val=%v）", k, present, val)
		}
	}
	// 库里那一行必须与响应一致（C18 裁定 ②：首次响应也回读库）。
	var stored string
	if err := d.Read.QueryRow(`SELECT canonical_path FROM projects WHERE id = ?`, v["id"]).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != v["canonical_path"] {
		t.Errorf("响应报 %v、库里是 %s —— 两者必须同一串", v["canonical_path"], stored)
	}
	if n := countQuery(t, d, `SELECT count(*) FROM event_journal WHERE event_type='project_created'`); n != 1 {
		t.Errorf("project_created 应恰有一行，实际 %d", n)
	}
}

// TestProjectPathConflictIs409 是 §3 那句话的端到端版：换一种写法（分隔符、大小写、尾斜杠）
// 也算同一个路径 ⇒ 409，且第二笔整体回滚（不多行、不多事件）。
func TestProjectPathConflictIs409(t *testing.T) {
	srv, d := newProjectHarness(t)

	code, body := post(t, srv, "k-c1", `{"name":"甲","canonical_path":"C:\\work\\same"}`)
	if code != http.StatusCreated {
		t.Fatalf("第一个项目应 201，实际 %d: %s", code, body)
	}
	code, body = post(t, srv, "k-c2", `{"name":"乙","canonical_path":"c:/work/same/"}`)
	if code != http.StatusConflict || codeOf(t, body) != "duplicate_project_path" {
		t.Fatalf("同路径第二个项目应 409 duplicate_project_path，实际 %d: %s", code, body)
	}
	if n := countQuery(t, d, `SELECT count(*) FROM projects`); n != 1 {
		t.Errorf("冲突那笔不该留下项目行，实际 %d 行", n)
	}
	if n := countQuery(t, d, `SELECT count(*) FROM event_journal WHERE event_type='project_created'`); n != 1 {
		t.Errorf("冲突那笔不该留下事件，实际 %d 条", n)
	}
	// 空路径写 NULL，因此可以有多个（那条部分唯一索引明确豁免 NULL）。
	for _, k := range []string{"k-n1", "k-n2"} {
		if c, b := post(t, srv, k, `{"name":"没有路径"}`); c != http.StatusCreated {
			t.Fatalf("%s 应 201，实际 %d: %s", k, c, b)
		}
	}
	var nulls int
	if err := d.Read.QueryRow(`SELECT count(*) FROM projects WHERE canonical_path IS NULL`).Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if nulls != 2 {
		t.Errorf("无路径项目应能有多行，实际 %d", nulls)
	}
	// 归档之后同一把键可以复用（那条部分唯一索引带 `archived_at IS NULL`）——
	// 但 :archive 这条口还没实现，所以这一格我**不在这里假装验过**，留给落那一口时补。
}

func TestProjectInputGates(t *testing.T) {
	srv, _ := newProjectHarness(t)

	for _, tc := range []struct{ why, key, body, want string }{
		{"缺 Idempotency-Key", "", `{"name":"甲"}`, "idempotency_key_required"},
		{"name 空白", "k-b1", `{"name":"   "}`, "validation_failed"},
		{"metadata 不是对象", "k-b2", `{"name":"甲","metadata":3}`, "validation_failed"},
	} {
		code, body := post(t, srv, tc.key, tc.body)
		if codeOf(t, body) != tc.want {
			t.Errorf("%s：应 %s，实际 %d %s", tc.why, tc.want, code, body)
		}
	}
	// 合法主体 + 合法体要真能建出来（上面三个都是失败路径，这里补一条成功路径防"全 400 也绿"）。
	if c, b := post(t, srv, "k-b3", `{"name":"甲"}`); c != http.StatusCreated {
		t.Errorf("合法请求应 201，实际 %d: %s", c, b)
	}
	// 没带口令 → 全局闸先挡住（401），不进业务码。
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/projects", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("没口令应 401，实际 %d", resp.StatusCode)
	}
	if c, b := call(t, srv, http.MethodGet, "/api/v1/projects", "", "", ""); c != http.StatusNotImplemented ||
		codeOf(t, b) != "not_implemented" {
		// 这个 harness 故意**不注入** ProjectRead：证的是 C28② 那一格 —— 装配缺件时 GET 回 501，
		// 而不是回 405 说"这条路径不接受 GET"（那是假话，下面 TestListProjectsOverHTTP 就让它 200）。
		t.Errorf("没装配读通道的 GET /projects 应 501 not_implemented，实际 %d %s", c, b)
	}
	if c, b := call(t, srv, http.MethodPut, "/api/v1/projects", "local-user", "", ""); c != http.StatusMethodNotAllowed ||
		codeOf(t, b) != "method_not_allowed" {
		t.Errorf("PUT /projects 该 405（这条路径根本不接受它），实际 %d %s", c, b)
	}
}

// TestProjectReplayAndRedaction 两件一起钉：同一 Idempotency-Key 重放必须逐字节回到首次响应；
// 而名字里混进密钥形状时，**线上、库里、事件里都不能出现原值**（脱敏在密封/写出之前，C18 裁定 ②）。
func TestProjectReplayAndRedaction(t *testing.T) {
	srv, d := newProjectHarness(t)

	body := `{"name":"粘了一个 key ` + testSecret + `","canonical_path":"R:\\x"}`
	code1, first := post(t, srv, "k-red", body)
	code2, replay := post(t, srv, "k-red", body)
	if code1 != http.StatusCreated || code2 != http.StatusCreated {
		t.Fatalf("两笔都该 201（第二笔是回放），实际 %d / %d", code1, code2)
	}
	if first != replay {
		t.Errorf("重放没回到首次响应:\n 首次 = %s\n 重放 = %s", first, replay)
	}
	if strings.Contains(first, testSecret) || strings.Contains(replay, testSecret) {
		t.Errorf("密钥从响应里出去了: %s", first)
	}
	if !strings.Contains(first, redactMarker) {
		t.Errorf("响应里没有 %s，说明脱敏没跑: %s", redactMarker, first)
	}
	var stored, payload string
	if err := d.Read.QueryRow(`SELECT name FROM projects WHERE canonical_path = 'r:\x'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE event_type='project_created' LIMIT 1`).
		Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, testSecret) || strings.Contains(payload, testSecret) {
		t.Errorf("原值进了库里或事件里：name=%q payload=%q", stored, payload)
	}
	if n := countQuery(t, d, `SELECT count(*) FROM projects`); n != 1 {
		t.Errorf("重放不该建出第二个项目，实际 %d 行", n)
	}
}

// countQuery 与本包已有的 countRows(table) 不是一把尺子：这里三条断言都要带 WHERE，
// 拼进那个 helper 会把它变成"既能传表名又能传整条语句"的两可函数，所以另起一个。
func countQuery(t *testing.T, d *db.DB, q string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// seedProjectRows 直插五行：created_at 1~5 定序，p-c 是 ARCHIVED（契约没给 status 过滤位，
// 所以它必须**留在列表里** —— 这条断言看着"不像 bug"，但把 status 过滤偷偷加回来正是下一个人的顺手活）。
func seedProjectRows(t *testing.T, d *db.DB) {
	t.Helper()
	for i, id := range []string{"p-a", "p-b", "p-c", "p-d", "p-e"} {
		status, archived := "ACTIVE", any(nil)
		if id == "p-c" {
			status, archived = "ARCHIVED", i+100
		}
		if _, err := d.Read.Exec(`INSERT INTO projects (id, name, canonical_path, project_type, status,
			metadata_json, created_at, updated_at, archived_at)
			VALUES (?, ?, ?, 'CODE', ?, '{}', ?, ?, ?)`, id, "项目"+id, "path-"+id, status, i+1, i+1, archived); err != nil {
			t.Fatal(err)
		}
	}
}

func listProjectsGet(t *testing.T, srv *httptest.Server, q string) (int, map[string]any, string) {
	t.Helper()
	code, body := call(t, srv, "GET", "/api/v1/projects"+q, "", "", "")
	if code != 200 {
		return code, nil, body
	}
	return code, mustParseMap(t, body), body
}

func pageIDs(items []any) []string {
	out := []string{}
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			id, _ := m["id"].(string)
			out = append(out, id)
		}
	}
	return out
}

// TestListProjectsPagesWithCursor 钉 GET 那一腿的三件事：keyset 翻页不重不漏、
// 短页/到底只回空串（REQ-F-19），以及**归档的那行不被过滤掉**。
func TestListProjectsPagesWithCursor(t *testing.T) {
	srv, d := newProjectHarness(t, true)
	defer srv.Close()

	// 空表：items 必须是 []，不是 null（同一份"两种表示法"的禁令）。
	_, m, body := listProjectsGet(t, srv, "")
	if items, ok := m["items"].([]any); !ok || items == nil {
		t.Fatalf("空表应回 items: []，实际 %s", body)
	}
	if m["next_cursor"] != "" {
		t.Errorf("空表不该有游标: %s", body)
	}

	seedProjectRows(t, d)
	var (
		got    []string
		cursor string
		pages  int
	)
	for pages < 5 {
		q := "?limit=2"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		code, m, body := listProjectsGet(t, srv, q)
		if code != 200 {
			t.Fatalf("第 %d 页应 200，实际 %d %s", pages+1, code, body)
		}
		nc, ok := m["next_cursor"].(string)
		if !ok {
			t.Fatalf("next_cursor 必须是字符串键: %s", body)
		}
		page := m["items"].([]any)
		pages++
		got = append(got, pageIDs(page)...)
		if len(page) < 2 {
			if nc != "" {
				t.Errorf("短页却给了非空 next_cursor: %q", nc)
			}
			break
		}
		if nc == "" {
			t.Fatalf("第 %d 页取满了却没有游标，翻不动了: %s", pages, body)
		}
		cursor = nc
	}
	if strings.Join(got, ",") != "p-a,p-b,p-c,p-d,p-e" {
		t.Errorf("翻页拼起来不对: %v", got)
	}
}

// TestListProjectsQueryGates 钉入口的有界检查，含**跨出口的游标互不吃**：
// inbox 那套三元组游标喂给 projects 必须 400（它比 tasks 的二元多吃一段）。
func TestListProjectsQueryGates(t *testing.T) {
	srv, d := newProjectHarness(t, true)
	defer srv.Close()
	seedProjectRows(t, d)

	for _, q := range []string{"?limit=0", "?limit=201", "?limit=abc", "?cursor=not-base64!!",
		"?cursor=" + base64.RawURLEncoding.EncodeToString([]byte("1|400|n-1"))} {
		code, _, body := listProjectsGet(t, srv, q)
		if code != 400 || codeOf(t, body) != "validation_failed" {
			t.Errorf("%s 应 400 validation_failed，实际 %d %s", q, code, body)
		}
	}
	// status 不是这条口的参数（契约只给 cursor/limit）：传了必须被**忽略**而不是被当成过滤，
	// 否则就是给一个契约里没有的语义开门。
	_, m, body := listProjectsGet(t, srv, "?status=ACTIVE&limit=10")
	if len(m["items"].([]any)) != 5 {
		t.Errorf("未知参数 status 不该改变结果（应仍是五行）: %s", body)
	}
}

// TestProjectViewKeysMatchBetweenLegs 是 §12/C28 那条"同一件事不许两种写法"的行为级版本：
// 同一个项目经 POST 读回与经 GET 列表读出，键集合必须一模一样。
func TestProjectViewKeysMatchBetweenLegs(t *testing.T) {
	srv, _ := newProjectHarness(t, true)
	defer srv.Close()

	code, created := post(t, srv, "idem-proj-list", `{"name":"对形状的项目"}`)
	if code != 201 {
		t.Fatalf("POST 建项目应 201，实际 %d %s", code, created)
	}
	_, list, body := listProjectsGet(t, srv, "")
	item := map[string]any{}
	for _, it := range list["items"].([]any) {
		m := it.(map[string]any)
		if m["name"] == "对形状的项目" {
			item = m
		}
	}
	if item["id"] == nil {
		t.Fatalf("列表里没找到刚建的那条: %s", body)
	}
	postKeys, listKeys := keySet(mustParseMap(t, created)), keySet(item)
	if postKeys != listKeys {
		t.Errorf("同一项目的两条出口键集合不一样：POST=%s GET=%s", postKeys, listKeys)
	}
	if item["canonical_path"] != nil {
		t.Errorf("同一条目在两条出口都应显式出 null：%#v", item["canonical_path"])
	}
}

func keySet(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
