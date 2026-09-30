package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

// newExecutionHarness 走真库真表：这条口最有价值的证据就是「出口的值是不是库里那一行的」——
// 有值的出值、没写者的出 null、schema 没声明的不发明，三样都必须跑真读路径才算数
// （与 inbox 那格同一个口径）。
func newExecutionHarness(t *testing.T, wired bool) (*httptest.Server, *db.DB) {
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
	// 父行先建：`executions` 对 tasks / adapter_instances / sessions 都有外键（这库开了 FK）。
	for _, tk := range []string{"task-1", "task-2"} {
		if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, created_at, updated_at) VALUES (?, '编排腿要建的那次执行', 1, 1)`, tk); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ad-codex', 'codex', 'Codex', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, created_at, updated_at)
		VALUES ('sess-q', 'ad-codex', 'remote-q', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO models (id, adapter_instance_id, remote_model_id, canonical_key,
		display_name, discovered_at, updated_at) VALUES ('m1', 'ad-codex', 'r-m1', 'ck-m1', 'Model 1', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	// 三行：ex-1（task-1，可选列都有值）、ex-2（task-1，另一组值）、ex-3（task-2，证明过滤不是巧合）。
	// 时间戳按行递增，读面的顺序才是可断的（`ListByTask` 是 ORDER BY created_at, id）。
	type seed struct {
		id, task, session, predecessor, provider, model string
		attempt                                         int64
		status, conf, trigger                           string
		result, errCode, errMsg                         string
		started, finished                               int64
	}
	for i, s := range []seed{
		{id: "ex-1", task: "task-1", provider: "p1", model: "m1", attempt: 1,
			status: "SUCCEEDED", conf: "HIGH", trigger: "INITIAL", result: "OK", started: 10, finished: 20},
		{id: "ex-2", task: "task-1", session: "sess-q", predecessor: "ex-1", attempt: 2,
			status: "STARTING", conf: "UNKNOWN", trigger: "MANUAL_SWITCH", started: 30},
		{id: "ex-3", task: "task-2", attempt: 1, status: "WAITING_INPUT", conf: "MEDIUM", trigger: "HANDOFF"},
	} {
		if _, err := d.Read.Exec(
			`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, predecessor_execution_id,
			    attempt_no, status, state_confidence, trigger_type, provider_execution_id, model_id,
			    result_code, error_code, error_message, started_at, finished_at, created_at, updated_at)
			 VALUES (?, ?, 'ad-codex', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.id, s.task, anyStr(s.session), anyStr(s.predecessor), s.attempt, s.status, s.conf, s.trigger,
			anyStr(s.provider), anyStr(s.model), anyStr(s.result), anyStr(s.errCode), anyStr(s.errMsg),
			anyInt(s.started), anyInt(s.finished), int64(100+i), int64(100+i)); err != nil {
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
	}
	if wired {
		cfg.Executions = execution.NewRepository(d.Read)
	}
	srv := httptest.NewServer(httpapi.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv, d
}

// anyStr / anyInt 把"夹具里没给"写成库里的 NULL（不是空串、不是 0）——出口该出 null 就得出 null。
func anyStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func anyInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func TestListExecutionsByTask(t *testing.T) {
	srv, _ := newExecutionHarness(t, true)

	code, body := call(t, srv, "GET", "/api/v1/executions?task_id=task-1", "", "", "")
	if code != http.StatusOK {
		t.Fatalf("带 task_id 该 200，实际 %d %s", code, body)
	}
	var page struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("task-1 该两行，实际 %s", body)
	}
	if page.NextCursor == nil || *page.NextCursor != "" {
		t.Errorf("REQ-F-19：不分页时 next_cursor 必须是空串这一种写法，实际 %v", page.NextCursor)
	}
	// 顺序按 created_at,id：ex-1 在前。
	first, second := page.Items[0], page.Items[1]
	if first["id"] != "ex-1" || second["id"] != "ex-2" {
		t.Errorf("顺序该是 ex-1,ex-2，实际 %v,%v", first["id"], second["id"])
	}
	// yaml 的 Execution.required 七键：两行都得齐（少一个就是给面板一个不合法的响应体）。
	for _, item := range page.Items {
		for _, k := range []string{"id", "task_id", "status", "attempt_no", "trigger_type", "state_confidence", "version"} {
			if _, ok := item[k]; !ok {
				t.Errorf("%v 缺必填键 %s：%v", item["id"], k, item)
			}
		}
	}
	// 有值的那行出值、没值的那行出 null —— 两种都不许被折算成 ""、0 或省略键。
	if first["session_id"] != nil {
		t.Errorf("ex-1 的 session_id 该是 null，实际 %v", first["session_id"])
	}
	if s, ok := second["session_id"].(string); !ok || s != "sess-q" {
		t.Errorf("ex-2 的 session_id 该是夹具里那个 sess-q，实际 %v", second["session_id"])
	}
	// model_id：第八十批起域里有了这一列（yaml `Execution` 有它）⇒ **有值出值、没值出 null**，
	// 折算成 "" 或凭空编一个都红。上一批那条「域里没有 model_id 不许出现」的反例按 Next ① 的
	// 「九键名单随之收窄」删掉，边界改由 executions_get_test 的 23 键键集断言钉住。
	if got, want := first["model_id"], "m1"; got != want {
		t.Errorf("ex-1 的 model_id 该是库里那行的 %#v，实际 %#v", want, got)
	}
	if got := second["model_id"]; got != nil {
		t.Errorf("ex-2 的 model_id 库里是 NULL，出口该 null，实际 %#v", got)
	}
	if v, ok := first["version"].(float64); !ok || v == 0 {
		t.Errorf("version 该是真版本号（If-Match 的比较基准），实际 %v", first["version"])
	}

	// 过滤不是巧合：task-2 只有一行。
	if _, body := call(t, srv, "GET", "/api/v1/executions?task_id=task-2", "", "", ""); body == "" {
		t.Fatal("空响应体")
	}
	// 查无此 Task ⇒ 空数组（列表口的"没有"是空页，不是 404；单条读才是 404 那一族）。
	code, body = call(t, srv, "GET", "/api/v1/executions?task_id=nope", "", "", "")
	if code != http.StatusOK || !containsEmptyItems(body) {
		t.Errorf("未知 task_id 该 200 + 空 items，实际 %d %s", code, body)
	}
}

func containsEmptyItems(body string) bool {
	var page struct {
		Items []any `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		return false
	}
	return page.Items != nil && len(page.Items) == 0
}

// TestExecutionsRequestGates 钉四种"没说谎"的分工：缺必填参数是 400、方法不对是 405、
// 收了实现不了的参数是 400（**不是默默收下**），而这条口没装配才是 501。
func TestExecutionsRequestGates(t *testing.T) {
	srv, _ := newExecutionHarness(t, true)
	for _, tc := range []struct{ why, path, want string }{
		{"缺 task_id", "/api/v1/executions", "validation_failed"},
		{"task_id 空串", "/api/v1/executions?task_id=", "validation_failed"},
		{"带 cursor", "/api/v1/executions?task_id=task-1&cursor=abc", "validation_failed"},
		{"带 limit", "/api/v1/executions?task_id=task-1&limit=10", "validation_failed"},
	} {
		if code, body := call(t, srv, "GET", tc.path, "local-user", "", ""); code != http.StatusBadRequest ||
			codeOf(t, body) != tc.want {
			t.Errorf("%s 该 400 %s，实际 %d %s", tc.why, tc.want, code, body)
		}
	}
	if code, body := call(t, srv, "POST", "/api/v1/executions?task_id=task-1", "local-user", "k-1", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("POST executions 该 405，实际 %d %s", code, body)
	}

	// 没装配那一格：这条路径根本不认领 ⇒ 由 catch-all 回 501（不是 405、也不是 400）。
	bare, _ := newExecutionHarness(t, false)
	if code, body := call(t, bare, "GET", "/api/v1/executions?task_id=task-1", "local-user", "", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("没注入读面该 501，实际 %d %s", code, body)
	}
}
