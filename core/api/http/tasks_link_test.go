package httpapi_test

// REQ-LK-1（09-25 主控裁「甲」）的验收半边。裁定给的判据形状是三样：
// 正例（建带 session 的 task ⇒ session_id 看得见 + link 表 1 行 role=ORIGIN）、
// 反例（同一发 POST 用同一把 Idempotency-Key 重放 ⇒ **不许多出第二行 link**）、
// 缺省那发（session_id 为空 ⇒ link 表 0 行）。
// 另钉第②条（404 `session_not_found`，且**不建孤儿**）与第④条（不发事件、不给 payload 加键）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// newHarnessWithSession 复用真库 harness，再种一行能挂的来源会话
// （`sessions.adapter_instance_id` 是外键，所以父行也得插 —— 跨包写在 `_test.go` 里按 §1.3 合法）。
func newHarnessWithSession(t *testing.T, sessID string) (*httptest.Server, *db.DB) {
	t.Helper()
	srv, d := newTaskHarness(t)
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ai-lk', 'lk', 'lk', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, created_at, updated_at)
		VALUES (?, 'ai-lk', ?, 1, 1)`, sessID, "remote-"+sessID); err != nil {
		t.Fatal(err)
	}
	return srv, d
}

func linkRowsOn(t *testing.T, d *db.DB, taskID string) (int, []string, []int64) {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM task_session_links WHERE task_id = ?`, taskID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	rows, err := d.Read.Query(`SELECT role, ordinal FROM task_session_links WHERE task_id = ? ORDER BY ordinal, id`, taskID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var roles []string
	var ords []int64
	for rows.Next() {
		var r string
		var o int64
		if err := rows.Scan(&r, &o); err != nil {
			t.Fatal(err)
		}
		roles, ords = append(roles, r), append(ords, o)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return n, roles, ords
}

func taskCreatedPayloadOn(t *testing.T, d *db.DB, taskID string) map[string]any {
	t.Helper()
	var raw string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal
		WHERE event_type = 'task_created' AND aggregate_id = ?`, taskID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func hasAnyKey(m map[string]any, keys ...string) []string {
	var hit []string
	for _, k := range keys {
		if _, ok := m[k]; ok {
			hit = append(hit, k)
		}
	}
	return hit
}

// TestCreateTaskWithOriginSessionIsOneClosure 正例：一次 POST 要么都有、要么都没有。
// 主控那半条「用 `GET /tasks/{id}` 读回 session_id」今天落不了地（那条口仍是 501，第 268 条里报出去），
// 所以这里按同一份真相的**两个活着的出口**断：响应体回显 + 直接读库那一行 link；列表读面另有用例。
func TestCreateTaskWithOriginSessionIsOneClosure(t *testing.T) {
	srv, d := newHarnessWithSession(t, "sess-origin")

	status, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "lk-1",
		`{"title":"带来源会话","priority":"LOW","session_id":"sess-origin"}`)
	if status != http.StatusCreated {
		t.Fatalf("建带会话的任务该 201，实际 %d: %s", status, out)
	}
	m := mustParseMap(t, out)
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatal("响应里没有 id")
	}
	if m["session_id"] != "sess-origin" {
		t.Errorf("响应该回显 session_id，实际 %v", m["session_id"])
	}

	n, roles, ords := linkRowsOn(t, d, id)
	if n != 1 {
		t.Fatalf("link 表该恰好 1 行，实际 %d", n)
	}
	if len(roles) != 1 || roles[0] != "ORIGIN" {
		t.Errorf("role 必须是 ORIGIN（这是来源会话，不是执行会话），实际 %v", roles)
	}
	if len(ords) != 1 || ords[0] != 0 {
		t.Errorf("ordinal 必须是 0，实际 %v", ords)
	}

	// 第④条：link 是关系行 —— 不多发事件，也不给 task_created 加键。
	if ev := countEventsOfType(t, d, "task_created"); ev != 1 {
		t.Errorf("这一发该只留一条 task_created，实际 %d", ev)
	}
	if total := countRows(t, d, "event_journal"); total != 1 {
		t.Errorf("link 不许多写一行事件（§1.1 第 12 条：关系行故意不发事件），实际 event_journal 共 %d 行", total)
	}
	if hit := hasAnyKey(taskCreatedPayloadOn(t, d, id), "session_id", "session", "origin_session_id"); len(hit) > 0 {
		t.Errorf("task_created 的 payload 不许出现会话键（§6 加键是登记变更，要另开 REQ）：%v", hit)
	}
}

// TestCreateTaskOriginSessionReplayAddsNoSecondLink 是主控点名的反例。
// 这一条不是凑数：幂等记录封的是"响应"，link 是第二张表 —— 只在 `Append` 前加一句"查过了"的写法
// 会让首攻与重放都回 201，而库里长出两行关系。
func TestCreateTaskOriginSessionReplayAddsNoSecondLink(t *testing.T) {
	srv, d := newHarnessWithSession(t, "sess-replay")
	body := `{"title":"重放不许多挂","priority":"LOW","session_id":"sess-replay"}`

	firstStatus, first := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "lk-replay", body)
	if firstStatus != http.StatusCreated {
		t.Fatalf("首攻该 201，实际 %d: %s", firstStatus, first)
	}
	id := mustParseMap(t, first)["id"].(string)

	againStatus, again := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "lk-replay", body)
	if againStatus != http.StatusCreated || again != first {
		t.Errorf("重放该原样回首次响应，实际 %d；首次=%s 重放=%s", againStatus, first, again)
	}
	if n, _, _ := linkRowsOn(t, d, id); n != 1 {
		t.Errorf("重放之后 link 是 %d 行（必须恰好 1 行）", n)
	}
	if n := countRows(t, d, "tasks"); n != 1 {
		t.Errorf("重放多建了任务：%d 行", n)
	}
}

// TestCreateTaskWithoutOriginSessionWritesNoLink 第③条：缺省 / `null` / 空串都是合法状态。
func TestCreateTaskWithoutOriginSessionWritesNoLink(t *testing.T) {
	cases := []struct{ name, key, body string }{
		{"不带 session_id", "nokey", `{"title":"没有来源会话","priority":"LOW"}`},
		{"session_id 是 null", "null", `{"title":"null 也算没给","priority":"LOW","session_id":null}`},
		{"session_id 是空串", "empty", `{"title":"空串也算没给","priority":"LOW","session_id":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, d := newHarnessWithSession(t, "sess-x")
			status, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "lk-"+tc.key, tc.body)
			if status != http.StatusCreated {
				t.Fatalf("该 201，实际 %d: %s", status, out)
			}
			m := mustParseMap(t, out)
			if v, ok := m["session_id"]; ok && v != "" {
				t.Errorf("没给来源会话就不该回显 session_id：%v", v)
			}
			id, _ := m["id"].(string)
			if n, _, _ := linkRowsOn(t, d, id); n != 0 {
				t.Errorf("这一发不许挂任何来源会话行，实际 %d", n)
			}
		})
	}
}

// TestCreateTaskUnknownSessionIs404AndBuildsNoOrphan 第②条：404 且整笔回滚。
// 码要按字面是 `session_not_found`（与 send 腿同一个码）—— 泛型 `not_found` 会让面板把
// "会话不存在"读成"任务不存在"，那条链后面就没法自动重试了。
func TestCreateTaskUnknownSessionIs404AndBuildsNoOrphan(t *testing.T) {
	srv, d := newHarnessWithSession(t, "sess-real")

	status, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "lk-404",
		`{"title":"挂在不存在的会话上","priority":"LOW","session_id":"sess-nope"}`)
	if status != http.StatusNotFound {
		t.Fatalf("不存在的来源会话该 404，实际 %d: %s", status, out)
	}
	if c := codeOf(t, out); c != "session_not_found" {
		t.Errorf("错误码该是 session_not_found（不用泛型 not_found），实际 %q: %s", c, out)
	}
	if n := countRows(t, d, "tasks"); n != 0 {
		t.Errorf("被拒的这一发建出了 %d 行孤儿任务", n)
	}
	if n := countRows(t, d, "task_session_links"); n != 0 {
		t.Errorf("被拒的这一发挂出了 %d 行 link", n)
	}
	if n := countRows(t, d, "event_journal"); n != 0 {
		t.Errorf("被拒的这一发写出了 %d 行事件（整笔事务该回滚）", n)
	}
}

// TestListTasksEchoesOriginSession 钉读模型那一半：`taskCols` 末尾那个相关子查询真把 link 带回来了。
// 没有它，`session_id` 只在首攻那一发里出现过一次 —— 之后任何读面都看不见，等于写了个只有写侧知道的事实。
func TestListTasksEchoesOriginSession(t *testing.T) {
	srv, _ := newHarnessWithSession(t, "sess-list")
	status, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", "lk-list",
		`{"title":"列表也该带出来","priority":"LOW","session_id":"sess-list"}`)
	if status != http.StatusCreated {
		t.Fatalf("建任务该 201，实际 %d: %s", status, out)
	}
	status, page := call(t, srv, http.MethodGet, "/api/v1/tasks", "", "", "")
	if status != http.StatusOK {
		t.Fatalf("列表该 200，实际 %d: %s", status, page)
	}
	if !strings.Contains(page, `"session_id":"sess-list"`) {
		t.Errorf("列表那一行该带回来源会话（读侧子查询没生效？）：%s", page)
	}
	if !strings.Contains(page, `"title":"列表也该带出来"`) {
		t.Errorf("列表里找不到刚建的那条任务：%s", page)
	}
}
