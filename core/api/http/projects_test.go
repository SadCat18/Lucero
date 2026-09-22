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

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/project"
	"github.com/SadCat18/lucero/core/internal/task"
)

// newProjectHarness 真库真表 + 真 project.Service：这条口最有价值的证据是
// "canonicalize 后冲突判 409" 与"首次响应等于库里那一行"，两条都得跑真写路径。
func newProjectHarness(t *testing.T) (*httptest.Server, *db.DB) {
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
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            task.NewService(nil),
		TaskList:         task.NewRepository(d.Read),
		Store:            d,
		Notifications:    notification.NewRepository(d.Read),
		Projects:         project.NewService(nil),
	}))
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
		t.Errorf("GET /projects 还没接，应 501 not_implemented，实际 %d %s", c, b)
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
