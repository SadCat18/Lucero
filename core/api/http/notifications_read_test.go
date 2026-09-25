package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

// newReadHarness 只多注入 `:read` 这一格需要的东西（NotifRead + Store）。
// `wired=false` 那一支是**给 501 那格用的**：其它依赖照给，所以 501 只能来自 `NotifRead == nil` 这道闸，
// 不会像第三十五批那样押到别人家的 nil 上、变异不咬还以为断言有牙。
func newReadHarness(t *testing.T, wired bool) (*httptest.Server, *db.DB) {
	t.Helper()
	return newNotifHarness(t, wired, false)
}

// newNotifHarness 是同一套真库装配，把"接哪几条写口"分开给：
// `:read` 与 `:handle` 是两个字段，所以"只接一条"这一格能被钉住（没接的那条必须仍是 501，
// 且 501 只能来自那道 nil 闸 —— 别的家照给，第三十五批那种"变异不咬还以为有牙"就不复现）。
func newNotifHarness(t *testing.T, readWired, handleWired bool) (*httptest.Server, *db.DB) {
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
	cfg := httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Tasks:            task.NewService(nil),
		TaskList:         task.NewRepository(d.Read),
		Store:            d,
		Notifications:    notification.NewRepository(d.Read),
	}
	if readWired {
		cfg.NotifRead = notification.NewService()
	}
	if handleWired {
		cfg.NotifHandle = notification.NewService()
	}
	srv := httptest.NewServer(httpapi.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv, d
}

// seedOne 走真写路径建一条 UNREAD（transport 不自己 INSERT notifications，那是域侧的形状）。
func seedOne(t *testing.T, d *db.DB, id string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, created_at, updated_at)
		VALUES (?, '被拒投影的那条', 1, 1)`, id+"-task"); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(context.Background(), func(tx *db.Tx) error {
		_, err := notification.NewService().Notify(context.Background(), tx, notification.Draft{
			ID: id, EventID: "ev-" + id, Type: notification.TypeProjectionRejected,
			Priority: notification.PriorityHigh, Title: "被拒投影", TaskID: id + "-task", PrincipalID: "system",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func readBack(t *testing.T, d *db.DB, id string) (state string, readAt int64, version int64) {
	t.Helper()
	if err := d.Read.QueryRow(`SELECT state, COALESCE(read_at, 0), version FROM notifications WHERE id = ?`, id).
		Scan(&state, &readAt, &version); err != nil {
		t.Fatal(err)
	}
	return
}

func eventCount(t *testing.T, d *db.DB, typ string) int64 {
	t.Helper()
	var n int64
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type = ?`, typ).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestNotificationReadOverHTTP 判三件：200 的形状与 inbox 那条**同一个视图**（少一格面板就得改两份）、
// 状态真的推进了、重复调用是 200 no-op（不推版本、不再发事件、连 read_at 都不刷新）。
func TestNotificationReadOverHTTP(t *testing.T) {
	srv, d := newReadHarness(t, true)
	seedOne(t, d, "n-http-1")

	code, body := call(t, srv, "POST", "/api/v1/notifications/n-http-1:read", "local-user", "k-read-1", "")
	if code != http.StatusOK {
		t.Fatalf("该 200，实际 %d %s", code, body)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	if v["state"] != "READ" || v["id"] != "n-http-1" {
		t.Errorf("响应该是读回的那行（READ），实际 %s", body)
	}
	if ra, ok := v["read_at"]; !ok || ra == nil {
		t.Errorf("read_at 必须有值（null 就是没读过），实际 %s", body)
	}
	// 必填六格与 GET /notifications 那份一致：这里逐键点，不写"看着一样"。
	for _, k := range []string{"id", "notification_type", "priority", "state", "title", "created_at"} {
		if _, ok := v[k]; !ok {
			t.Errorf("Notification 的必填键 %s 不在响应里: %s", k, body)
		}
	}
	if st, ra, ver := readBack(t, d, "n-http-1"); st != "READ" || ra == 0 || ver != 2 {
		t.Errorf("库里该是 READ + read_at + v2，实际 %s/%d/v%d", st, ra, ver)
	}
	if n := eventCount(t, d, "notification_read"); n != 1 {
		t.Errorf("恰一行 notification_read，实际 %d", n)
	}

	_, firstAt, firstVer := readBack(t, d, "n-http-1")
	code2, _ := call(t, srv, "POST", "/api/v1/notifications/n-http-1:read", "local-user", "k-read-2", "")
	if code2 != http.StatusOK {
		t.Errorf("重复 :read 该还是 200（幂等点击不是错误），实际 %d", code2)
	}
	if st, ra, ver := readBack(t, d, "n-http-1"); st != "READ" || ra != firstAt || ver != firstVer {
		t.Errorf("重复调用该什么都不动（时间戳 %d→%d / 版本 v%d→v%d）", firstAt, ra, firstVer, ver)
	}
	if n := eventCount(t, d, "notification_read"); n != 1 {
		t.Errorf("第二次不许再发一条事件（什么都没变还发更新=把事件写成仪式），实际 %d 行", n)
	}
}

func TestNotificationReadUnknownIs404(t *testing.T) {
	srv, d := newReadHarness(t, true)
	code, body := call(t, srv, "POST", "/api/v1/notifications/nope:read", "local-user", "k-1", "")
	if code != http.StatusNotFound || codeOf(t, body) != "not_found" {
		t.Errorf("未知 id 该 404 not_found，实际 %d %s", code, body)
	}
	if n := eventCount(t, d, "notification_read"); n != 0 {
		t.Errorf("404 这一发不该留事件，实际 %d", n)
	}
}

// TestNotificationReadRefusesHandledWith409 把 C36② 那一格抬到 HTTP 面：409 `illegal_transition`
// （§15.2 明写这一码"来自各 Service 而非 Adapter"，正是这条路的归属）。
func TestNotificationReadRefusesHandledWith409(t *testing.T) {
	srv, d := newReadHarness(t, true)
	seedOne(t, d, "n-handled")
	if _, err := d.Read.Exec(`UPDATE notifications SET state = 'HANDLED' WHERE id = ?`, "n-handled"); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, srv, "POST", "/api/v1/notifications/n-handled:read", "local-user", "k-1", "")
	if code != http.StatusConflict || codeOf(t, body) != "illegal_transition" {
		t.Errorf("已处置的该 409 illegal_transition，实际 %d %s", code, body)
	}
	if st, _, ver := readBack(t, d, "n-handled"); st != "HANDLED" || ver != 1 {
		t.Errorf("被拒之后库里那行不该动，实际 %s v%d", st, ver)
	}
}

// TestNotificationReadVerbRouting 钉"认领整棵子树的代价"：`:handle` 与邻居的 501/405 一个字都不许变。
// 少钉一条，就是接一条口顺手把邻居接成 404/405 那种静默行为变更（第四十一批同一条规矩）。
// 09-25 起 `:handle` 已经有实现（契约① §1.1 第 21 条裁了边集），**这里的 501 只能来自 harness 没注入
// `NotifHandle` 那道闸**（readWired=true、handleWired=false 就是这一支）；接通之后那六格行为由
// `notifications_handle_test.go` 钉，别拿这条 501 当"`:handle` 还没做"。
func TestNotificationReadVerbRouting(t *testing.T) {
	srv, d := newNotifHarness(t, true, false)
	seedOne(t, d, "n-route")

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		// declared、方法合法、**这一格没装配**（harness 故意不给 NotifHandle）⇒ 501。
		{"POST", "/api/v1/notifications/n-route:handle", http.StatusNotImplemented},
		// 没有动词的那条（GET /notifications/{id}）契约里根本没 declared ⇒ 形状不认，也是 501。
		{"GET", "/api/v1/notifications/n-route", http.StatusNotImplemented},
		// 方法不被这条路径接受 ⇒ 405。
		{"GET", "/api/v1/notifications/n-route:read", http.StatusMethodNotAllowed},
		{"GET", "/api/v1/notifications/n-route:handle", http.StatusMethodNotAllowed},
	} {
		code, body := call(t, srv, tc.method, tc.path, "local-user", "k-route", "")
		if code != tc.want || codeOf(t, body) != codeName(tc.want) {
			t.Errorf("%s %s 该 %d %s，实际 %d %s", tc.method, tc.path, tc.want, codeName(tc.want), code, body)
		}
	}
	// 精确路径那条（GET/POST /api/v1/notifications）没被子树抢走：列表仍是 200、POST 仍是 405。
	if code, body := call(t, srv, "GET", "/api/v1/notifications", "", "", ""); code != http.StatusOK {
		t.Errorf("GET /notifications 该还是 200（列表那条口没被认领走），实际 %d %s", code, body)
	}
	if code, body := call(t, srv, "POST", "/api/v1/notifications", "local-user", "k-list", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("POST /notifications 该 405，实际 %d %s", code, body)
	}
	// 形状不认识的那些 ⇒ 501 且**一次都不落到库**（库里那条 n-route 该还是 UNREAD v1）。
	for _, bad := range []string{
		"/api/v1/notifications//read",
		"/api/v1/notifications/n-route/extra:read",
		"/api/v1/notifications/a:b:read",
		"/api/v1/notifications/n-route:nonsense",
		"/api/v1/notifications/:read",
		"/api/v1/notifications/",
	} {
		if code, body := call(t, srv, "POST", bad, "local-user", "k-shape", ""); code != http.StatusNotImplemented ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("畸形形状 %q 该 501（不猜形状），实际 %d %s", bad, code, body)
		}
	}
	if st, _, ver := readBack(t, d, "n-route"); st != "UNREAD" || ver != 1 {
		t.Errorf("上面所有守卫都该在碰到域之前拦住，库里那条却被改成了 %s v%d", st, ver)
	}
}

// codeName 只是把"状态码 → 期望的 code 字符串"这一对照写成一处，免得五个断言各抄一份口径。
func codeName(status int) string {
	switch status {
	case http.StatusNotImplemented:
		return "not_implemented"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	}
	return ""
}

// TestNotificationReadIsNotAuthFree 钉一条容易漏的：新接的写口走的是同一套全局闸（Bearer + Principal）。
func TestNotificationReadIsNotAuthFree(t *testing.T) {
	srv, d := newReadHarness(t, true)
	seedOne(t, d, "n-auth")

	req, err := http.NewRequest("POST", srv.URL+"/api/v1/notifications/n-auth:read", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("没带口令的 :read 该 401，实际 %d", resp.StatusCode)
	}
	// 口令对了但没声明主体（写操作必须带 local-user）⇒ 也是 401，不是 200 也不是 403。
	req2, err := http.NewRequest("POST", srv.URL+"/api/v1/notifications/n-auth:read", nil)
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Authorization", "Bearer "+testToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("写操作缺 X-Lucero-Principal 该 401，实际 %d", resp2.StatusCode)
	}
	if st, _, ver := readBack(t, d, "n-auth"); st != "UNREAD" || ver != 1 {
		t.Errorf("两发被鉴权挡住的请求都不该动库，实际 %s v%d", st, ver)
	}
}

// TestNotificationReadWithoutWiringStays501 是"半接线闸门"那一格。
func TestNotificationReadWithoutWiringStays501(t *testing.T) {
	srv, _ := newReadHarness(t, false)
	code, body := call(t, srv, "POST", "/api/v1/notifications/n-none:read", "local-user", "k-1", "")
	if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
		t.Errorf("没装配 Service 时 :read 该 501，实际 %d %s", code, body)
	}
}
