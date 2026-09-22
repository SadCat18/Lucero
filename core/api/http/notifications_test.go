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
	"github.com/SadCat18/lucero/core/internal/task"
)

// newInboxHarness 与 newTaskHarness 同形状（真库真表、真 Repository），只多注入 Notifications 一口。
// 本包刻意不写"假 lister"：这条口最有价值的证据是分页游标与响应形状，那必须跑真读路径才算数。
func newInboxHarness(t *testing.T) (*httptest.Server, *db.DB) {
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
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            task.NewService(nil),
		TaskList:         task.NewRepository(d.Read),
		Store:            d,
		Notifications:    notification.NewRepository(d.Read),
	}))
	t.Cleanup(srv.Close)
	return srv, d
}

// seedInboxRows 摆五行 UNREAD + 一行 READ：
// 两行同 created_at（逼出 id 那条 tie-break）、一行带 read_at、一行带 expires_at、
// 其余那些可空列保持 NULL（出口必须是 null，不是 0、不是省略）。
func seedInboxRows(t *testing.T, d *db.DB) {
	t.Helper()
	rows := [][6]string{
		{"i1", "CRITICAL", "500", "UNREAD", "", ""},
		{"i2", "HIGH", "400", "UNREAD", "1234", ""},
		{"i3", "HIGH", "400", "UNREAD", "", ""},
		{"i4", "MEDIUM", "900", "UNREAD", "", "7000"},
		{"i5", "LOW", "100", "UNREAD", "", ""},
		{"i6", "CRITICAL", "999", "READ", "", ""},
	}
	for _, r := range rows {
		var readAt, expiresAt any
		if r[4] != "" {
			readAt = r[4]
		}
		if r[5] != "" {
			expiresAt = r[5]
		}
		if _, err := d.Read.Exec(`INSERT INTO notifications
			(id, notification_type, priority, state, title, created_at, read_at, expires_at, version)
			VALUES (?, 'PROJECTION_REJECTED', ?, ?, 't', ?, ?, ?, 1)`,
			r[0], r[1], r[3], r[2], readAt, expiresAt); err != nil {
			t.Fatal(err)
		}
	}
}

type inboxPage struct {
	Items      []map[string]any `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

func getInbox(t *testing.T, srv *httptest.Server, query string) (int, inboxPage, string) {
	t.Helper()
	code, body := call(t, srv, "GET", "/api/v1/notifications"+query, "", "", "")
	var p inboxPage
	if code == 200 {
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatalf("inbox 响应不是合法 JSON: %q (%v)", body, err)
		}
	}
	return code, p, body
}

// TestInboxPagesWithCursor 是这条口的正证：默认 state=UNREAD、按 (档, created_at DESC, id) 出页，
// 翻页只靠 next_cursor（REQ-F-19：它是字符串，空串是唯一合法的"没有下一页"，键不许消失）。
func TestInboxPagesWithCursor(t *testing.T) {
	srv, d := newInboxHarness(t)
	seedInboxRows(t, d)

	var (
		got    []string
		cursor string
		pages  int
	)
	for pages < 4 {
		q := "?limit=2"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		code, p, body := getInbox(t, srv, q)
		if code != 200 {
			t.Fatalf("第 %d 页应 200，实际 %d: %s", pages+1, code, body)
		}
		if p.NextCursor == nil {
			t.Fatalf("next_cursor 缺键或为 null（REQ-F-19 只承认空串这一种\"没有\"）: %s", body)
		}
		pages++
		for _, item := range p.Items {
			id, _ := item["id"].(string)
			got = append(got, id)
		}
		if len(p.Items) < 2 {
			if *p.NextCursor != "" {
				t.Errorf("短页却给了非空 next_cursor: %q", *p.NextCursor)
			}
			break
		}
		if *p.NextCursor == "" {
			t.Fatalf("第 %d 页取满了 2 行却没有游标，翻不动了: %s", pages, body)
		}
		cursor = *p.NextCursor
	}
	if strings.Join(got, ",") != "i1,i2,i3,i4,i5" {
		t.Fatalf("三页拼起来不对（序或翻页有问题）: %v", got)
	}
}

// TestInboxItemShapeKeepsNullApartFromZero 钉响应形状里最容易糊的一层：
// 必填六键必须在；三个可空时间戳必须是 null 而不是 0（§12"不许用 0 表示不知道"）。
func TestInboxItemShapeKeepsNullApartFromZero(t *testing.T) {
	srv, d := newInboxHarness(t)
	seedInboxRows(t, d)

	code, p, body := getInbox(t, srv, "?limit=5")
	if code != 200 {
		t.Fatal(body)
	}
	byID := map[string]map[string]any{}
	for _, item := range p.Items {
		id, _ := item["id"].(string)
		byID[id] = item
	}
	if len(byID) != 5 {
		t.Fatalf("UNREAD 应有五行，实际 %d（含被 state 谓词漏掉/多收的行？）: %v", len(byID), p.Items)
	}
	if _, ok := byID["i6"]; ok {
		t.Error("READ 那一行串进来了，state 谓词没起作用")
	}
	for _, k := range []string{"id", "notification_type", "priority", "state", "title", "created_at"} {
		if _, ok := byID["i1"][k]; !ok {
			t.Errorf("Notification.required 里的 %q 在响应里缺键", k)
		}
	}
	// i1：没读过、没处理、不过期 → 三个都必须是 null。
	for _, k := range []string{"read_at", "handled_at", "expires_at"} {
		if v, ok := byID["i1"][k]; !ok || v != nil {
			t.Errorf("i1.%s = %#v（应为 null，不能是 0，也不能缺键）", k, v)
		}
	}
	if v := byID["i2"]["read_at"]; v != float64(1234) {
		t.Errorf("i2.read_at = %#v，应为 1234", v)
	}
	if v := byID["i4"]["expires_at"]; v != float64(7000) {
		t.Errorf("i4.expires_at = %#v，应为 7000", v)
	}
	// body/project_id/task_id/approval_id 这四列在夹具里是 NULL，而 internal/notification 的
	// scan 一直把可空字符串折成空串（早于这条口就存在）。yaml 对它们是 `type: [string,"null"]`、
	// 没有 minLength，所以 "" 是合法值、出口形状不违约；但"NULL 与真空串分不开"这条缺口我如实钉在
	// 断言里（而不是断言成 null 假装已经分开），修法见 progress Remaining。
	for _, k := range []string{"body", "project_id", "task_id", "approval_id"} {
		if v, ok := byID["i1"][k]; !ok {
			t.Errorf("i1.%s 缺键（这几列虽然可空，但键必须出现在响应里）", k)
		} else if v != "" {
			t.Errorf("i1.%s = %#v，夹具里没写值，应为空串", k, v)
		}
	}
}

// TestInboxQueryValidation 钉入口的有界检查：state 六态、limit 1~200、cursor 不合法一律 400。
func TestInboxQueryValidation(t *testing.T) {
	srv, d := newInboxHarness(t)
	seedInboxRows(t, d)

	for _, q := range []string{"?state=BOGUS", "?limit=0", "?limit=201", "?limit=abc", "?cursor=not-base64!!"} {
		code, _, body := getInbox(t, srv, q)
		if code != 400 || codeOf(t, body) != "validation_failed" {
			t.Errorf("%s 应 400 validation_failed，实际 %d %s", q, code, body)
		}
	}
	// 状态大小写不敏感（前端可能传小写），但值必须在词表里。
	if code, _, body := getInbox(t, srv, "?state=read"); code != 200 {
		t.Errorf("小写 state=read 应被接受，实际 %d %s", code, body)
	}
}

// TestInboxRejectsTaskCursor 是 T-95 那句"两套游标口径不许合并"的行为级断言：
// 把 tasks 那个二元的分页游标喂给 inbox，必须判不合法，而不是"勉强能用"。
func TestInboxRejectsTaskCursor(t *testing.T) {
	srv, d := newInboxHarness(t)
	seedInboxRows(t, d)

	taskStyle := base64.RawURLEncoding.EncodeToString([]byte("1234|some-task-id"))
	code, _, body := getInbox(t, srv, "?cursor="+taskStyle)
	if code != 400 || codeOf(t, body) != "validation_failed" {
		t.Errorf("tasks 形状的游标喂给 inbox 应 400，实际 %d %s", code, body)
	}
	// 反过来也要挡：inbox 的三元组喂给 tasks（tasks 的游标只该有一个分隔符）。
	inboxStyle := base64.RawURLEncoding.EncodeToString([]byte("1|400|i3"))
	if c2, b2 := call(t, srv, "GET", "/api/v1/tasks?cursor="+inboxStyle, "", "", ""); c2 != 400 ||
		codeOf(t, b2) != "validation_failed" {
		t.Errorf("inbox 形状的游标喂给 tasks 应 400 validation_failed，实际 %d %s", c2, b2)
	}
}

// TestInboxMethodAndAuth 钉 405 与全局口令闸；顺带证 :read/:handle 那两口仍是 501（本批只开 GET）。
func TestInboxMethodAndAuth(t *testing.T) {
	srv, d := newInboxHarness(t)
	seedInboxRows(t, d)

	if code, body := call(t, srv, "POST", "/api/v1/notifications", "local-user", "", "{}"); code != 405 ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("POST /notifications 应 405，实际 %d %s", code, body)
	}
	req, err := http.NewRequest("GET", srv.URL+"/api/v1/notifications", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("没带口令应 401，实际 %d", resp.StatusCode)
	}
	for _, path := range []string{"/api/v1/notifications/i1:read", "/api/v1/notifications/i1:handle"} {
		if code, body := call(t, srv, "POST", path, "local-user", "k-1", "{}"); code != 501 ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("%s 本批不该开，应 501，实际 %d %s", path, code, body)
		}
	}
}

// TestInboxStaysUnimplementedWithoutInjection：没注入 lister 时维持 §3 的 501，不给半套面。
// newTaskHarness 恰好就是"只注入 tasks 那三件"的配置。
func TestInboxStaysUnimplementedWithoutInjection(t *testing.T) {
	srv, _ := newTaskHarness(t)
	if code, body := call(t, srv, "GET", "/api/v1/notifications", "", "", ""); code != 501 ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("未注入时应 501 not_implemented，实际 %d %s", code, body)
	}
}
