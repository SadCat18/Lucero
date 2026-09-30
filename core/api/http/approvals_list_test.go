package httpapi_test

// `GET /api/v1/approvals` 的接线判据（T-414 ③：handler 归 backend，读法仍在 internal/approval）。
//
// 这里刻意**不往 approvals 表里写一行**：那张表对 backend 是绝对禁区，连测试也不破例。
// 所以判据分两层，各证各的、谁也不冒充谁：
//   - 这一层（transport）判的是"注入的读法有没有被真的调用、参数怎么传、出口是什么形状、
//     没注入时说的是不是'没装配'而不是'今天没有审批'"—— 用一只替身（approvalStub）就判得完；
//   - 真读法那一层（状态过滤、keyset 顺序、PENDING 还要看 deadline）由 `internal/approval` 自己那批用例判
//     （T-372 的 96 条），我在替身里重抄一遍那些规则只会得到第二份会漂移的说法。
//
// 换句话说：这一包绿**不证明**库里能读出真行，它证明的是"接口接上了、且说的话是真话"。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/approval"
)

// approvalStub 是被注入的那把"读法"：它只记录自己收到的 ListFilter，并按吩咐返回行或错误。
type approvalStub struct {
	items []approval.Approval
	last  approval.ListPosition
	err   error
	seen  []approval.ListFilter
}

func (s *approvalStub) ListPage(_ context.Context, f approval.ListFilter) ([]approval.Approval, approval.ListPosition, error) {
	s.seen = append(s.seen, f)
	if s.err != nil {
		return nil, approval.ListPosition{}, s.err
	}
	return s.items, s.last, nil
}

func approvalsServer(lst httpapi.ApprovalLister) http.Handler {
	return httpapi.NewHandler(httpapi.Config{
		Token:            "sekret-token",
		Version:          "test-1",
		State:            func() string { return "READY" },
		MigrationVersion: func() int64 { return 3 },
		Approvals:        lst,
	})
}

func approvalsGet(t *testing.T, h http.Handler, query string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/v1/approvals"
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer sekret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// onePending 是一条"在等"的审批（TaskID/Resource 等可空列留空，正是要看出口怎么折 null）。
func onePending() approval.Approval {
	return approval.Approval{
		ID: "ap-1", ExecutionID: "ex-1", AdapterInstanceID: "ad-1",
		Type: "TOOL_USE", RiskLevel: "MEDIUM", Action: "write_file",
		RequestJSON: `{"schema":1,"principal_id":"local-user"}`,
		State:       approval.PENDING, RequestedAt: 1_700_000_000_000, ExpiresAt: 1_700_000_030_000,
		Version: 1,
	}
}

func itemKeys(t *testing.T, body string) []string {
	t.Helper()
	var page struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("解不开这一页：%v（原文 %s）", err, body)
	}
	if len(page.Items) != 1 {
		t.Fatalf("这一页该有 1 条，实际 %d", len(page.Items))
	}
	keys := make([]string, 0, len(page.Items[0]))
	for k := range page.Items[0] {
		keys = append(keys, k)
	}
	return keys
}

func hasKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// TestListApprovalsServesTheInjectedFace 判出口形状：yaml `Approval` 的 18 个键一个不多一个不少，
// 可空列出 null（不是 ""、不是 0），两份 *_json 出对象，而 `grant_id` **恒 null** ——
// 那一格今天没有读法出口它，编一个值比出 null 坏得多。
func TestListApprovalsServesTheInjectedFace(t *testing.T) {
	stub := &approvalStub{items: []approval.Approval{onePending()}}
	rec := approvalsGet(t, approvalsServer(stub), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("注入了读法就该 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	want := []string{"action", "adapter_instance_id", "approval_type", "execution_id", "expires_at",
		"grant_id", "id", "requested_at", "resolution", "resource", "risk_level", "session_id",
		"state", "task_id", "version"}
	got := itemKeys(t, rec.Body.String())
	if len(got) != 18 {
		t.Errorf("yaml `Approval` 是 18 键，出口 %d 键：%v", len(got), got)
	}
	for _, k := range want {
		if !hasKey(got, k) {
			t.Errorf("出口缺 %s（现读 %v）", k, got)
		}
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	item := body["items"].([]any)[0].(map[string]any)
	for _, k := range []string{"task_id", "session_id", "resource", "resolved_at", "resolved_by", "grant_id"} {
		if item[k] != nil {
			t.Errorf("%s 该是 null（库里没有这一格），实际 %v", k, item[k])
		}
	}
	if reqMap, ok := item["request"].(map[string]any); !ok || reqMap["principal_id"] != "local-user" {
		t.Errorf("request 该原样出成对象，实际 %v", item["request"])
	}
	if resMap, ok := item["resolution"].(map[string]any); !ok || len(resMap) != 0 {
		t.Errorf("没写过的 resolution 该出 {}（与 executions 那三格同一条兜底），实际 %v", item["resolution"])
	}
	// 不满一页 ⇒ next_cursor 必须是**空串**这个写法本身（REQ-F-19：键必在、不许 null、不许省略）。
	if _, has := body["next_cursor"]; !has {
		t.Error("next_cursor 这个键必须在")
	}
	if v, _ := body["next_cursor"].(string); v != "" {
		t.Errorf("不满一页时 next_cursor 该是空串，实际 %q", v)
	}
	if len(stub.seen) != 1 {
		t.Fatalf("只该问读法一次，实际 %d 次", len(stub.seen))
	}
	if stub.seen[0].State != nil || stub.seen[0].Limit != 50 || stub.seen[0].After != nil {
		t.Errorf(`什么参数都不传该是"不过滤、缺省一页"，实际 %+v`, stub.seen[0])
	}
}

// TestListApprovalsPassesFiltersThrough 判参数真到了读法那一侧：state **原样（大写化）传下去**、
// limit 传下去、满一页时 next_cursor 非空；再用那一枚指针打第二发，读法收到的 After 必须就是
// 第一发它自己交回来的那个位置（往返走真链路，不在测试里抄 base64 格式 —— 抄了就长成"两边各自成立"）。
func TestListApprovalsPassesFiltersThrough(t *testing.T) {
	row := onePending()
	stub := &approvalStub{
		items: []approval.Approval{row, row},
		last:  approval.ListPosition{RequestedAt: 1_700_000_000_005, ID: "ap-z"},
	}
	h := approvalsServer(stub)

	rec := approvalsGet(t, h, "state=pending&limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("该 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	f := stub.seen[0]
	if f.State == nil || *f.State != "PENDING" {
		t.Errorf("state 该被大写化后传下去（词表唯一的说法在 approval），实际 %+v", f.State)
	}
	if f.Limit != 2 {
		t.Errorf("limit 该传 2，实际 %d", f.Limit)
	}
	if f.After != nil {
		t.Errorf("第一次请求不该有 After，实际 %+v", f.After)
	}
	var body struct {
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.NextCursor == "" {
		t.Fatal("满了 limit 的那一页必须有下一页指针")
	}

	rec2 := approvalsGet(t, h, "limit=2&cursor="+body.NextCursor)
	if rec2.Code != http.StatusOK {
		t.Fatalf("带着游标的那一发该 200，实际 %d：%s", rec2.Code, rec2.Body.String())
	}
	if len(stub.seen) != 2 {
		t.Fatalf("第二发该问到读法第二次，实际 %d 次", len(stub.seen))
	}
	after := stub.seen[1].After
	if after == nil || *after != stub.last {
		t.Errorf("next_cursor 往返没回到同一个位置：出去 %q，解回 %+v（应是 %+v）", body.NextCursor, after, stub.last)
	}
	if after != nil && after.ID == "" {
		t.Error("解回来的位置必须有 id")
	}
}

// TestListApprovalsNotWiredIsNotAnEmptyPage 是这条口最容易犯的错：没装配就回 200 + 空 items。
// 空页说的是"今天确实没有审批"，与"这一层没接"是两件事 —— 混起来，面板就把"没接"当成"都批完了"。
func TestListApprovalsNotWiredIsNotAnEmptyPage(t *testing.T) {
	rec := approvalsGet(t, approvalsServer(nil), "")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("没注入读法必须 501，实际 %d：%s", rec.Code, rec.Body.String())
	}
	text := rec.Body.String()
	if !strings.Contains(text, `"not_implemented"`) || !strings.Contains(text, `"retryable":true`) {
		t.Errorf("501 的口径必须是 not_implemented + retryable=true，实际 %s", text)
	}
	if strings.Contains(text, `"items"`) {
		t.Errorf("没装配那一格不许出 items（那是冒充有面）：%s", text)
	}
	for _, must := range []string{"§3 登记过", "没装配"} {
		if !strings.Contains(text, must) {
			t.Errorf("文案里要点名 %q（让读的人知道是接线问题，不是数据问题），实际 %s", must, text)
		}
	}
	// 而这条口的动词形状不许被接线改掉：POST 仍是 405，不是 501。
	rec2 := approvalsPost(t, approvalsServer(nil), "/api/v1/approvals")
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /approvals 该 405，实际 %d", rec2.Code)
	}
}

func approvalsPost(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer sekret-token")
	// 写动词必须先过 `X-Lucero-Principal` 那道门（handler.go:163/235），否则这里量到的是 401 而不是 405：
	// 我要判的是"这条口的动词形状没被接线改掉"，不是鉴权。
	req.Header.Set("X-Lucero-Principal", "local-user")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestListApprovalsMapsDomainRejectionTo400 判两件相反的事：
//   - approval 那句"这不是 approvals.state 的值"必须翻成 **400**（用户打错了），
//     而不是 500，也不是"空的一页"（REQ-A8：打错的过滤器答"今天没有"最坏）；
//   - 别的错误（库坏了）必须还是 **500**，不许被一并翻成用户的错。
//
// 这里用的是替身返回真错误 —— 文案来自 `internal/approval` 的 validState，我不在本包重抄那份词表，
// 所以 transport 认的是它那一句。这一处文案耦合是有判据钉住的：**它坏了本用例就红**，
// 干净的解法是那一侧导出一个哨兵或 ValidState（已作为一格报给主控，改 approval 的面对我不开放）。
func TestListApprovalsMapsDomainRejectionTo400(t *testing.T) {
	stub := &approvalStub{err: errors.New(`approval: "BOGUS" is not an approvals.state value`)}
	rec := approvalsGet(t, approvalsServer(stub), "state=bogus")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态词表外的值该 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"validation_failed"`) {
		t.Errorf("400 的 error_code 该是 validation_failed，实际 %s", rec.Body.String())
	}
	// 但过滤条件仍然**原样传下去**了：transport 不自己判词表（那会长出第二份会漂移的说法）。
	if stub.seen[0].State == nil || *stub.seen[0].State != "BOGUS" {
		t.Errorf("transport 不该自己拦下词表外的值，应交给那一侧；实际 %+v", stub.seen[0].State)
	}

	stub2 := &approvalStub{err: errors.New("no such column: requested_at")}
	rec2 := approvalsGet(t, approvalsServer(stub2), "")
	if rec2.Code != http.StatusInternalServerError {
		t.Errorf("库坏了必须是 500，不许翻成用户的错，实际 %d：%s", rec2.Code, rec2.Body.String())
	}
}

// TestListApprovalsBadParamsAre400 判 transport 自己那一格：limit 与 cursor 是自己该管的，
// 打错了当场拦住，不去打扰读法。
func TestListApprovalsBadParamsAre400(t *testing.T) {
	stub := &approvalStub{}
	for _, q := range []string{"limit=0", "limit=-1", "limit=201", "limit=abc", "cursor=not-base64!!!"} {
		rec := approvalsGet(t, approvalsServer(stub), q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q 该 400，实际 %d：%s", q, rec.Code, rec.Body.String())
		}
	}
	if len(stub.seen) != 0 {
		t.Errorf("参数不合法就不该问读法，实际被问了 %d 次", len(stub.seen))
	}
}
