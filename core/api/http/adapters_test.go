package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/adapter"
)

// stubProber 只回答两件事：被叫了几次（形状守卫与缺键那几格必须**一次都没被叫**），
// 以及该回什么（域包的错误是 §15.2 那些码的唯一来源）。
// 它不碰库，所以这条口的用例与真库那条（cmd/core/probeassembly_test.go）分工清楚：
// 这里判"transport 有没有补字段、有没有把 501 回成 404"，那边判"握手与落库"。
type stubProber struct {
	calls []string
	view  *adapter.AdapterInstanceView
	err   error
}

func (s *stubProber) Probe(_ context.Context, adapterKey string) (*adapter.AdapterInstanceView, error) {
	s.calls = append(s.calls, adapterKey)
	return s.view, s.err
}

// newProbeHarness 的 `p` 是接口本身：没装配那一格要传**字面 nil**（`newProbeHarness(t, nil)`）。
// 传一个值为 nil 的 *stubProber 会得到"非 nil 接口装着 nil 指针"，501 那格就测不到了 ——
// 这正是 cmd/core 那边 adaptersField() 存在的原因。
func newProbeHarness(t *testing.T, p httpapi.AdapterProber) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Token: testToken, Version: "test-1", State: func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 2 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Adapters:         p,
	}))
	t.Cleanup(srv.Close)
	return srv
}

func requireUntouched(t *testing.T, s *stubProber, what string) {
	t.Helper()
	if len(s.calls) != 0 {
		t.Errorf("%s：形状/键位守卫该在碰到域之前就把请求拦下，实际域被调用了 %v", what, s.calls)
	}
}

// TestAdapterProbeRouteShapesAndGates 钉四格：未装配 ⇒ 501；方法不对 ⇒ 405；
// 形状不认识 ⇒ 501 且不碰域；带冒号/多一段/空 key 都不许变成 400（那会把"路径写错"报成"key 写错"）。
func TestAdapterProbeRouteShapesAndGates(t *testing.T) {
	unwired := newProbeHarness(t, nil)
	if code, body := call(t, unwired, "POST", "/api/v1/adapters/fake/probe", "local-user", "k-1", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("没装配 Registry 时 probe 该 501，实际 %d %s", code, body)
	}

	s := &stubProber{view: &adapter.AdapterInstanceView{ID: "i-1", AdapterKey: "fake", DisplayName: "fake", Enabled: true, Health: "HEALTHY"}}
	srv := newProbeHarness(t, s)
	if code, body := call(t, srv, "GET", "/api/v1/adapters/fake/probe", "", "", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("GET 到一条只接受的 POST 的 custom-verb 路径该 405，实际 %d %s", code, body)
	}
	requireUntouched(t, s, "GET 该在调域之前就被 405 拦下")

	for _, bad := range []string{
		"/api/v1/adapters/fake/probe/extra", // 多一段
		"/api/v1/adapters/fake:x/probe",     // 带冒号
		"/api/v1/adapters/fake/whatver",     // 不认识的动词段
		"/api/v1/adapters/",                 // 只有前缀（空 key 的唯一洗不掉的写法）
	} {
		code, body := call(t, srv, "POST", bad, "local-user", "k-2", "")
		if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
			t.Errorf("畸形形状 %q 该 501（不猜形状），实际 %d %s", bad, code, body)
		}
	}
	requireUntouched(t, s, "畸形形状该一次都不落到域")

	// `/api/v1/adapters//probe` **到不了这条口的形状守卫**：Go 的 ServeMux 先做 clean-redirect，
	// 非 GET 用的是 307（保方法），洗完就是单段 `{id}` ⇒ 那条路径对 POST 的合法答案是 405。
	// 这一格单独钉，是因为不钉的话下一个人会以为"空 key 守卫漏了 `//`"，而真正的解释在 mux 那一层。
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest("POST", srv.URL+"/api/v1/adapters//probe", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Lucero-Principal", "local-user")
	req.Header.Set("Idempotency-Key", "k-2b")
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || resp.Header.Get("Location") != "/api/v1/adapters/probe" {
		t.Errorf(`双斜杠该由 mux 先 307 到 "/api/v1/adapters/probe"，实际 %d location=%q`,
			resp.StatusCode, resp.Header.Get("Location"))
	}
	if code, body := call(t, srv, "POST", "/api/v1/adapters/probe", "local-user", "k-2c", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf(`洗完之后的 "/api/v1/adapters/probe" 是单条 {id}，POST 该 405，实际 %d %s`, code, body)
	}
}

// TestAdapterProbeNeighborsStayUnwired 是"认领整棵子树的代价"那一句的机械版：
// 这些路径本来由 catch-all 回 501，现在由这条口回 —— 状态与码都不许变。
func TestAdapterProbeNeighborsStayUnwired(t *testing.T) {
	s := &stubProber{}
	srv := newProbeHarness(t, s)
	for _, p := range []struct{ method, path string }{
		{"GET", "/api/v1/adapters"},
		{"GET", "/api/v1/adapters/a-1"},
		{"GET", "/api/v1/adapters/a-1/capabilities"},
	} {
		if code, body := call(t, srv, p.method, p.path, "", "", ""); code != http.StatusNotImplemented ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("%s %s 是 declared 未接，该 501，实际 %d %s", p.method, p.path, code, body)
		}
	}
	for _, p := range []struct{ method, path string }{
		{"POST", "/api/v1/adapters/a-1"},
		{"POST", "/api/v1/adapters/a-1/capabilities"},
	} {
		if code, body := call(t, srv, p.method, p.path, "local-user", "k-3", ""); code != http.StatusMethodNotAllowed ||
			codeOf(t, body) != "method_not_allowed" {
			t.Errorf("%s %s 方法不被那条路径接受，该 405，实际 %d %s", p.method, p.path, code, body)
		}
	}
	requireUntouched(t, s, "邻居路径不该走到 probe")
}

// TestAdapterProbeRequiresIdempotencyKey：yaml 把这条口写成"有副作用 → Idempotency-Key"。
// 缺键必须是 403 `idempotency_key_required`（与 POST /projects 同一个码同一个状态），而且**一次都不许真去连**。
func TestAdapterProbeRequiresIdempotencyKey(t *testing.T) {
	s := &stubProber{}
	srv := newProbeHarness(t, s)
	if code, body := call(t, srv, "POST", "/api/v1/adapters/fake/probe", "local-user", "", ""); code != http.StatusForbidden ||
		codeOf(t, body) != "idempotency_key_required" {
		t.Errorf("缺 Idempotency-Key 该 403 idempotency_key_required，实际 %d %s", code, body)
	}
	requireUntouched(t, s, "缺幂等键该在拉起子进程之前就被拦下")

	req, err := http.NewRequest("POST", srv.URL+"/api/v1/adapters/fake/probe", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Idempotency-Key", "k-9")
	req.Header.Set("X-Lucero-Principal", "local-user")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("probe 不是免鉴权的口子，缺口令该 401，实际 %d", resp.StatusCode)
	}
}

// TestAdapterProbeProjectsTheDomainView 判"一个字都不补"：响应体就是域包那份视图，
// 必填五格（id/adapter_key/display_name/health/enabled）逐字段对回去，可空的那几格必须是 null 而不是被省略。
func TestAdapterProbeProjectsTheDomainView(t *testing.T) {
	view := &adapter.AdapterInstanceView{
		ID: "inst-1", AdapterKey: "fake", DisplayName: "Fake Adapter",
		AdapterVersion: nil, ProtocolVersion: ptr("1.0"), SoftwareVersion: ptr("9.9.9"),
		Enabled: true, Health: "HEALTHY", EffectiveBackend: ptr("cli"),
		LastHeartbeatAt: nil, LastHealthcheckAt: ptr(int64(1234)),
		Metadata: map[string]any{"schema": 1}, Version: 3, CreatedAt: 100, UpdatedAt: 200,
	}
	s := &stubProber{view: view}
	srv := newProbeHarness(t, s)
	code, body := call(t, srv, "POST", "/api/v1/adapters/fake/probe", "local-user", "k-4", "")
	if code != http.StatusOK {
		t.Fatalf("该 200，实际 %d %s", code, body)
	}
	if len(s.calls) != 1 || s.calls[0] != "fake" {
		t.Fatalf("probe 该恰好被调一次、参数是路径里那个 key，实际 %v", s.calls)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	// required 五格逐一对回（少任何一格，前端的 generated client 就会拒这个响应）。
	for k, want := range map[string]any{
		"id": "inst-1", "adapter_key": "fake", "display_name": "Fake Adapter",
		"health": "HEALTHY", "enabled": true,
	} {
		if got[k] != want {
			t.Errorf("%s 该是 %v（视图原样投出去），实际 %#v", k, want, got[k])
		}
	}
	for _, k := range []string{"adapter_version", "last_heartbeat_at"} {
		v, ok := got[k]
		if !ok {
			t.Errorf("%s 该出现为 null（可空≠省略，两套游标那件事就是这里学的）", k)
			continue
		}
		if v != nil {
			t.Errorf("%s 该是 null，实际 %#v", k, v)
		}
	}
	if got["protocol_version"] != "1.0" || got["software_version"] != "9.9.9" || got["effective_backend"] != "cli" {
		t.Errorf("可空但有值的那几格该照搬，实际 %s", body)
	}
	if got["last_healthcheck_at"] != float64(1234) {
		t.Errorf("last_healthcheck_at 该是 verified_at（1234），实际 %#v", got["last_healthcheck_at"])
	}
	// §1.1 第 16 条（09-23 02:3x 扩到"纯代码侧集合比对"那一类）的三条腿，正好落在"一个字都不补"这格上：
	// ①解码后的键集合 == 域包那个 struct 能发出的 json 标签集合（少一格 = 吞字段，多一格 = 补字段，都红）；
	// ②原始 body 里**逐次出现**的顶层键 token 数 == 去重后的数（重复键在 JSON→map 里会被静默吸收）；
	// ③合成反例证明 ② 有牙（不许只对着干净基线自证）。
	requireExactViewKeys(t, body)
}

// requireExactViewKeys 是"transport 不许补字段"的机械版。用 `json.Decoder.Token()` 而不是 `Unmarshal`
// 是为了**留住重复键**（Unmarshal 会悄悄后写覆盖前写，那正是第 16 条要防的那种吸收）。
func requireExactViewKeys(t *testing.T, body string) {
	t.Helper()
	tokens := topLevelJSONKeys(t, body)
	uniq := map[string]bool{}
	for _, k := range tokens {
		uniq[k] = true
	}
	if len(tokens) != len(uniq) {
		t.Errorf("响应里有重复的顶层键（%d 个 token / %d 个去重）：JSON→map 会把它吸收掉，判据必须自己看见", len(tokens), len(uniq))
	}
	// 反例：把同一个顶层键**再写一遍**（合法 JSON，只是重复成员），这条必须把 token 数数出 +1 ——
	// 不这样做的话 ② 只是对着干净样本空转：`Unmarshal` 会把重复键后写覆盖前写，谁都不会响。
	dupBody := strings.TrimSuffix(strings.TrimSpace(body), "}") + ",\"id\":\"dup\"}"
	if dup := topLevelJSONKeys(t, dupBody); len(dup) != len(tokens)+1 {
		t.Errorf("合成反例没被数出来（原 %d 个 token，加一个重复键之后 %d 个）⇒ 那条去重判据是空的",
			len(tokens), len(dup))
	}

	want := map[string]bool{}
	rt := reflect.TypeOf(adapter.AdapterInstanceView{})
	for i := 0; i < rt.NumField(); i++ {
		tag := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			want[tag] = true
		}
	}
	if len(want) != rt.NumField() {
		t.Fatalf("视图结构里有重复的 json 标签（%d 字段 / %d 去重标签）—— 上面那条去重判据会被这种漂移骗过", rt.NumField(), len(want))
	}
	for _, k := range tokens {
		if !want[k] {
			t.Errorf("响应出现了 `AdapterInstanceView` 之外的键 %q ⇒ transport 在补字段（§7.2 禁止）", k)
		}
		delete(want, k)
	}
	for k := range want {
		t.Errorf("视图里的 %q 没出现在响应里 ⇒ 投影吞了字段", k)
	}
}

func topLevelJSONKeys(t *testing.T, body string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(body))
	var keys []string
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return keys
		}
		if err != nil {
			t.Fatalf("响应不是合法 JSON（%v）body=%s", err, body)
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		case string:
			if depth == 1 {
				keys = append(keys, v)
			}
		}
	}
}

// TestAdapterProbeMapsDomainErrors 是 §7.2 那句"未知 key 是请求错、不是连接不健康"的机械版，
// 也是 §15.2 那张表在这条口上的全部落点。最后一行是**没有码的普通 error** ⇒ 走 transport 自己的 500。
func TestAdapterProbeMapsDomainErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		code string
	}{
		{"未知 adapter_key", &adapter.Error{Code: adapter.ErrInvalidParams, Message: "not registered"}, http.StatusBadRequest, "validation_failed"},
		{"拉不起来", &adapter.Error{Code: adapter.ErrAdapterUnresponsive, Message: "launch failed"}, http.StatusServiceUnavailable, "adapter_unresponsive"},
		{"major 对不上", &adapter.Error{Code: adapter.ErrProtocolVersionMismatch, Message: "foreign major"}, http.StatusUpgradeRequired, "protocol_version_mismatch"},
		{"握手前就调", &adapter.Error{Code: adapter.ErrProtocolNotInitialized, Message: "pre-handshake"}, http.StatusServiceUnavailable, "protocol_not_initialized"},
		{"能力没有", &adapter.Error{Code: adapter.ErrCapabilityUnavailable, Message: "no capability"}, http.StatusServiceUnavailable, "capability_unavailable"},
		{"要登录", &adapter.Error{Code: -32007, Message: "auth required"}, http.StatusUnauthorized, "auth_required"},
		{"限流", &adapter.Error{Code: -32008, Message: "rate limited"}, http.StatusTooManyRequests, "rate_limited"},
		{"超期", &adapter.Error{Code: -32011, Message: "deadline"}, http.StatusGatewayTimeout, "timeout"},
		// 别人给了一个 Core 没见过的数字：Degrade 成 INTERNAL，**不许现编一个名字**（归因翻不回来）。
		{"没见过的码", &adapter.Error{Code: -32999, Message: "what"}, http.StatusInternalServerError, "internal"},
		{"没有码的 error", errors.New("库都没开"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &stubProber{err: tc.err}
			srv := newProbeHarness(t, s)
			code, body := call(t, srv, "POST", "/api/v1/adapters/fake/probe", "local-user", "k-5", "")
			if code != tc.want || codeOf(t, body) != tc.code {
				t.Errorf("该 %d %s，实际 %d %s", tc.want, tc.code, code, body)
			}
			if len(s.calls) != 1 {
				t.Errorf("这一格是错误映射，域该被调一次，实际 %v", s.calls)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
