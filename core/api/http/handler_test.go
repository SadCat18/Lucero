package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
)

type harness struct {
	h       http.Handler
	tickets *httpapi.Tickets
	state   string
	now     time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	hs := &harness{state: httpapi.StateReady}
	hs.now = time.UnixMilli(1_700_000_000_000)
	// 注入时钟：30 秒过期这种事不能靠 sleep 测。
	clock := func() time.Time { return hs.now }
	hs.tickets = httpapi.NewTickets(30*time.Second, clock)
	hs.h = httpapi.NewHandler(httpapi.Config{
		Token:            "sekret-token",
		Version:          "test-1",
		State:            func() string { return hs.state },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          hs.tickets,
	})
	return hs
}

func (hs *harness) do(method, path, auth, principal, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	if principal != "" {
		req.Header.Set("X-Lucero-Principal", principal)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	return rec
}

func TestHealthIsTheOnlyUnauthenticatedEndpoint(t *testing.T) {
	hs := newHarness(t)

	rec := hs.do(http.MethodGet, "/api/v1/health", "", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("health 应免鉴权，实际 %d", rec.Code)
	}
	var h httpapi.Health
	if err := json.Unmarshal(rec.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if h.State != httpapi.StateReady || !h.Ready || h.Version != "test-1" || h.MigrationVersion != 2 {
		t.Errorf("health 四字段不对: %+v", h)
	}
	if strings.Contains(rec.Body.String(), "sekret-token") {
		t.Error("health 响应里漏了口令")
	}

	// 还没 Ready 时 ready 必须是 false：前端只看这一格，别看 state 猜。
	hs.state = httpapi.StateBooting
	rec = hs.do(http.MethodGet, "/api/v1/health", "", "", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if h.Ready {
		t.Errorf("BOOTING 阶段 ready 不能是 true: %+v", h)
	}

	for _, tc := range []struct{ name, auth string }{
		{"缺口令", ""}, {"错口令", "wrong"}, {"只写 Bearer 没值", "  "},
	} {
		rec = hs.do(http.MethodGet, "/api/v1/tasks", tc.auth, "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s：应 401，实际 %d", tc.name, rec.Code)
		}
		var body httpapi.ErrorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("错误体形状不符契约③ §2：%v", err)
		}
		if body.Error.Code == "" {
			t.Errorf("%s：错误体缺 code", tc.name)
		}
	}
}

// 带 Origin 的外部页面一律 403（契约③ §1.3）；回环来源放行，因为前端就坐在回环上。
func TestOriginGuard(t *testing.T) {
	hs := newHarness(t)
	for _, bad := range []string{"https://evil.example", "http://10.0.0.5:5173", "null"} {
		if rec := hs.do(http.MethodGet, "/api/v1/tasks", "sekret-token", "", bad); rec.Code != http.StatusForbidden {
			t.Errorf("Origin %q 应 403，实际 %d", bad, rec.Code)
		}
	}
	for _, ok := range []string{"http://127.0.0.1:5173", "http://localhost:3000", "http://[::1]:8080"} {
		if rec := hs.do(http.MethodGet, "/api/v1/tasks", "sekret-token", "", ok); rec.Code == http.StatusForbidden {
			t.Errorf("回环来源 %q 被自己的 CORS 闸挡了", ok)
		}
	}
}

// 写操作必须带 X-Lucero-Principal: local-user；system / adapter 不接受外部声明（§1.4）。
func TestWritesNeedLocalUserPrincipal(t *testing.T) {
	hs := newHarness(t)
	rec := hs.do(http.MethodPost, "/api/v1/tasks", "sekret-token", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("缺主体的写应 401，实际 %d", rec.Code)
	}
	for _, who := range []string{"system", "adapter-codex", "local-user"} {
		rec = hs.do(http.MethodPost, "/api/v1/tasks", "sekret-token", who, "")
		// 主体合法不等于端点已实现：到 handler 那儿是 501，而不是 401。
		if who == "local-user" && rec.Code != http.StatusNotImplemented {
			t.Errorf("local-user 的写应走到 501，实际 %d", rec.Code)
		}
		if who != "local-user" && rec.Code == http.StatusNotImplemented {
			t.Errorf("%s 不该被当成合法主体放行", who)
		}
	}
}

// 还没接的端点必须 501 并带 code，不许假成功（契约③ §8）。
func TestUnimplementedEndpointsAreNotSilentSuccess(t *testing.T) {
	hs := newHarness(t)
	for _, p := range []string{"/api/v1/projects", "/api/v1/executions/ex-1", "/ws/v1/events"} {
		rec := hs.do(http.MethodGet, p, "sekret-token", "", "")
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s 应 501，实际 %d", p, rec.Code)
		}
		var body httpapi.ErrorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Code != "not_implemented" || !body.Error.Retryable {
			t.Errorf("%s 错误体不对: %+v", p, body.Error)
		}
	}
}

// REQ-F-11 的反向用例（契约③ §2 明令要有一条）：同一请求两次必须拿到两张不同的有效票。
// 票签发端点还额外豁免于"写操作要带主体"，因为它不改变业务状态。
func TestWsTicketIsNotIdempotencyCached(t *testing.T) {
	hs := newHarness(t)

	first := hs.issueTicket(t)
	second := hs.issueTicket(t)
	if first == second {
		t.Fatal("两次签票拿到同一张：幂等缓存把一次性凭证变成可预测的了")
	}
	if !hs.tickets.Redeem(first) {
		t.Error("第一张票应有效")
	}
	if !hs.tickets.Redeem(second) {
		t.Error("第二张票也必须有效，不能因为第一张被用了就作废")
	}
}

// §1.2：单次使用 + 30 秒过期，且校验即作废。
func TestWsTicketIsSingleUseAndExpires(t *testing.T) {
	hs := newHarness(t)
	ticket := hs.issueTicket(t)

	if hs.tickets.Redeem("guess-the-ticket") {
		t.Error("随便猜一张也能过？")
	}
	if !hs.tickets.Redeem(ticket) {
		t.Fatal("第一次校验应通过")
	}
	if hs.tickets.Redeem(ticket) {
		t.Error("同一张票第二次校验必须失败（校验即作废）")
	}

	fresh := hs.issueTicket(t)
	hs.now = hs.now.Add(30 * time.Second)
	if hs.tickets.Redeem(fresh) {
		t.Error("到点的票必须作废")
	}
}

func (hs *harness) issueTicket(t *testing.T) string {
	t.Helper()
	rec := hs.do(http.MethodPost, "/api/v1/ws-ticket", "sekret-token", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("签票应 200（不要求主体头，因为它不改变业务状态），实际 %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Ticket    string `json:"ticket"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Ticket == "" || body.ExpiresIn != 30 {
		t.Fatalf("票响应不对: %s", rec.Body)
	}
	return body.Ticket
}
