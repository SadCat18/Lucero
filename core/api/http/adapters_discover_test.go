package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/session"
)

// stubDiscover 记录被叫了几次：形状守卫与"没装配"那几格必须一次都没落到域。
type stubDiscover struct {
	calls []string
	items []session.Discovered
	err   error
}

func (s *stubDiscover) DiscoverSessions(_ context.Context, instanceID string) ([]session.Discovered, int, error) {
	s.calls = append(s.calls, instanceID)
	if s.err != nil {
		return nil, 0, s.err
	}
	return s.items, len(s.items), nil
}

// newDiscoverHarness 把 adapters 那棵子树整个装起来（probe 与 discover 都注入），
// 所以"未装配"那一支必须单独用 `discover=false` 跑一遍 —— 否则 501 可能来自别人的 nil 闸。
func newDiscoverHarness(t *testing.T, d *stubDiscover, discover bool) *httptest.Server {
	t.Helper()
	cfg := httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Adapters: &stubProber{view: &adapter.AdapterInstanceView{
			ID: "ai-1", AdapterKey: "fake", DisplayName: "fake", Enabled: true, Health: "HEALTHY",
		}},
	}
	if discover {
		cfg.SessionDiscover = d
	}
	srv := httptest.NewServer(httpapi.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv
}

// TestDiscoverRouteShapeIsRecognized 是 T-195③ 的机械版：这一格以前"见冒号就不认形状"，
// 于是它拿到的是"形状不认识"的 501，客户与"这个口还没接线"完全分不开。
// 现在扩了形状 ⇒ 方法不对就是 405，未装配才是 501。
func TestDiscoverRouteShapeIsRecognized(t *testing.T) {
	s := &stubDiscover{items: []session.Discovered{{SessionID: "sess-1", RemoteSessionID: "rsn-1"}}}
	srv := newDiscoverHarness(t, s, true)

	code, body := call(t, srv, "POST", "/api/v1/adapters/ai-1/sessions:discover", "local-user", "k-d1", "")
	if code != http.StatusOK {
		t.Fatalf("接好了就该 200，实际 %d %s", code, body)
	}
	var v struct {
		Items []struct {
			SessionID       string `json:"session_id"`
			RemoteSessionID string `json:"remote_session_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 1 || v.Items[0].SessionID != "sess-1" || v.Items[0].RemoteSessionID != "rsn-1" {
		t.Errorf("items 该是 (session_id, remote_session_id) 成对，实际 %s", body)
	}
	if len(s.calls) != 1 || s.calls[0] != "ai-1" {
		t.Fatalf("路径参数该原样传成实例 id，实际 %v", s.calls)
	}

	// 空清单也要出键、出空数组（`items` 是 required）。
	empty := newDiscoverHarness(t, &stubDiscover{}, true)
	if code, body := call(t, empty, "POST", "/api/v1/adapters/ai-9/sessions:discover", "local-user", "k-d2", ""); code != http.StatusOK {
		t.Errorf("远端零会话该 200 + 空数组，实际 %d %s", code, body)
	} else if !strings.Contains(body, `"items":[]`) {
		t.Errorf("空页必须是 `items: []` 而不是 null/省略，实际 %s", body)
	}

	// 方法不被接受 ⇒ 405（不是 501）。
	if code, body := call(t, srv, "GET", "/api/v1/adapters/ai-1/sessions:discover", "", "", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("GET discover 该 405，实际 %d %s", code, body)
	}
}

func TestDiscoverUnknownInstanceAndUnconfiguredKey(t *testing.T) {
	// 实例不存在 ⇒ 404（对象不存在）
	srv := newDiscoverHarness(t, &stubDiscover{err: sql.ErrNoRows}, true)
	if code, body := call(t, srv, "POST", "/api/v1/adapters/nope/sessions:discover", "local-user", "k", ""); code != http.StatusNotFound ||
		codeOf(t, body) != "not_found" {
		t.Errorf("未知实例该 404 not_found，实际 %d %s", code, body)
	}
	// 有实例但本机没配起法 ⇒ 501（这台机器上从来没这条能力），**不许与 503 混**
	srv3 := newDiscoverHarness(t, &stubDiscover{err: httpapi.ErrAdapterNotLaunchable}, true)
	if code, body := call(t, srv3, "POST", "/api/v1/adapters/ai-x/sessions:discover", "local-user", "k", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("没配起法该 501，实际 %d %s", code, body)
	}
	// 那个 Adapter 病了 ⇒ 503 adapter_unresponsive（与上面两格分岔，同一个函数三种答案）
	srv4 := newDiscoverHarness(t, &stubDiscover{err: &adapter.Error{Code: adapter.ErrAdapterUnresponsive, Message: "起不来"}}, true)
	if code, body := call(t, srv4, "POST", "/api/v1/adapters/ai-y/sessions:discover", "local-user", "k", ""); code != http.StatusServiceUnavailable ||
		codeOf(t, body) != "adapter_unresponsive" {
		t.Errorf("连不上该 503 adapter_unresponsive，实际 %d %s", code, body)
	}
	// 没有码的普通错误 ⇒ transport 自己的 500
	srv5 := newDiscoverHarness(t, &stubDiscover{err: errors.New("库里没开成")}, true)
	if code, body := call(t, srv5, "POST", "/api/v1/adapters/ai-z/sessions:discover", "local-user", "k", ""); code != http.StatusInternalServerError ||
		codeOf(t, body) != "internal_error" {
		t.Errorf("无码错误该 internal_error/500，实际 %d %s", code, body)
	}
	// T-204③：远端清单自己畸形（同页两条同样的 remote_session_id）⇒ 整批拒，那一格定 **400 `validation_failed`**。
	// 这里**故意带一层 %w 包装**（装配方 `DiscoverSessions` 原样抛域错，但 transport 认的是 `errors.Is`）：
	// 写成 `err == session.ErrInvalidCommand` 这种直接比较就要红 —— 那是这条判据的牙。
	srv6 := newDiscoverHarness(t, &stubDiscover{
		err: fmt.Errorf("%w: 同一页里 \"rsn-dup\" 出现了两次，宁可整批拒", session.ErrInvalidCommand),
	}, true)
	if code, body := call(t, srv6, "POST", "/api/v1/adapters/ai-w/sessions:discover", "local-user", "k", ""); code != http.StatusBadRequest ||
		codeOf(t, body) != "validation_failed" {
		t.Errorf("坏清单该 400 validation_failed，实际 %d %s", code, body)
	}
}

// TestDiscoverUnwiredAndNeighbors 钉两件事：未装配那一支是**这道闸**给的 501；
// 以及扩形状没把邻居改坏（probe 仍 200、capabilities/单条仍 501、别的畸形仍 501）。
func TestDiscoverUnwiredAndNeighbors(t *testing.T) {
	s := &stubDiscover{}
	unwired := newDiscoverHarness(t, s, false)
	if code, body := call(t, unwired, "POST", "/api/v1/adapters/ai-1/sessions:discover", "local-user", "k", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("没注入 SessionDiscover 时该 501，实际 %d %s", code, body)
	}
	if len(s.calls) != 0 {
		t.Errorf("未装配那一支不该落到域，实际 %v", s.calls)
	}

	wired := newDiscoverHarness(t, s, true)
	if code, _ := call(t, wired, "POST", "/api/v1/adapters/fake/probe", "local-user", "k-p", ""); code != http.StatusOK {
		t.Errorf("probe 那条不该被 discover 弄坏，实际 %d", code)
	}
	// declared、方法合法、没接线 ⇒ 501（这两条走 GET）
	for _, p := range []string{"/api/v1/adapters/a-1", "/api/v1/adapters/a-1/capabilities"} {
		if code, body := call(t, wired, "GET", p, "", "", ""); code != http.StatusNotImplemented ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("GET %s 该 501（declared 未接），实际 %d %s", p, code, body)
		}
		// 同一个路径的 POST 是 405（它只声明 GET）—— 这一对不许互换，第四十一批就钉过。
		if code, body := call(t, wired, "POST", p, "local-user", "k", ""); code != http.StatusMethodNotAllowed ||
			codeOf(t, body) != "method_not_allowed" {
			t.Errorf("POST %s 该 405，实际 %d %s", p, code, body)
		}
	}
	// 形状不认识 ⇒ 501，且**一次都不落到域**（discover 会被叫到就说明守卫漏了）
	for _, p := range []string{
		"/api/v1/adapters//sessions:discover", // 空实例 id
		"/api/v1/adapters/a/b/sessions:discover",
		"/api/v1/adapters/a:x/sessions:discover",
		"/api/v1/adapters/a-1/sessions:discoverx",
		"/api/v1/adapters/a-1/capabilities:discover",
		"/api/v1/adapters/sessions:discover",
	} {
		if code, body := call(t, wired, "POST", p, "local-user", "k", ""); code != http.StatusNotImplemented ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("畸形形状 %s 该 501，实际 %d %s", p, code, body)
		}
	}
	if len(s.calls) != 0 {
		t.Errorf("上面这些格都不该落到域，实际被叫了 %v", s.calls)
	}
}

// TestDiscoverRequiresIdempotencyKey 是 T-204④ 那一格：yaml `core-v1.yaml:217` 本来就挂了
// `parameters/IdempotencyKey`（那个参数 `required: true`，`:741`），所以缺这道检查是**代码的缺口**、不是契约没写。
// 三条都要钉：① 缺键 ⇒ 403 `idempotency_key_required` 且**一次都不落到域**（不许先把 Provider 拉起来再说键不对）；
// ② 补上同一个 harness 的键 ⇒ 正常 200（证明 403 不是别的东西给的）；
// ③ 未装配 + 缺键 ⇒ 仍是 501 —— 闸的顺序与上面 probe 那一格完全一致，同一件事不许有两种写法。
func TestDiscoverRequiresIdempotencyKey(t *testing.T) {
	s := &stubDiscover{items: []session.Discovered{{SessionID: "sess-1", RemoteSessionID: "rsn-1"}}}
	srv := newDiscoverHarness(t, s, true)
	if code, body := call(t, srv, "POST", "/api/v1/adapters/ai-1/sessions:discover", "local-user", "", ""); code != http.StatusForbidden ||
		codeOf(t, body) != "idempotency_key_required" {
		t.Errorf("discover 缺 Idempotency-Key 该 403 idempotency_key_required，实际 %d %s", code, body)
	}
	if len(s.calls) != 0 {
		t.Errorf("缺键该在起连接之前就被拦下，实际落到了域 %v", s.calls)
	}
	if code, body := call(t, srv, "POST", "/api/v1/adapters/ai-1/sessions:discover", "local-user", "k-403", ""); code != http.StatusOK {
		t.Errorf("同一个 harness 补上键就该 200（证明 403 只差那一个头），实际 %d %s", code, body)
	}
	unwired := newDiscoverHarness(t, &stubDiscover{}, false)
	if code, body := call(t, unwired, "POST", "/api/v1/adapters/ai-1/sessions:discover", "local-user", "", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("未装配时缺键也该先回 501（装配闸在前），实际 %d %s", code, body)
	}
}
