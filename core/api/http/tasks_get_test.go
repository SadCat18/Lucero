package httpapi_test

// `GET /api/v1/tasks/{id}`（yaml `getTask`）的验收半边 —— 判据形状按主控在 T-320 里那句
// "你要落哪一格就重发一次 REQ"之前就已批准的 §3 声明，外加我自己承诺的两发反例：
// ① 摘掉读面注册 ⇒ 正向那格必须红（证明真有东西在断它）；② 把 404 写成 501 ⇒ 404 那格必须红
// （证明"资源不在"与"能力没接"这两格没被人顺手合并）。两发都在 `scripts` 之外用一次性变异跑，判决写进台账。

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// getTaskJSON 只取这一批要断的键；不写全字段是因为全字段形状由"与列表项逐键相等"那条断言钉，
// 在这里再抄一份就是把同一个契约抄成两处（CO-49 那一族）。
type getTaskJSON struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	Version   int64  `json:"version"`
}

func createForGet(t *testing.T, srv *httptest.Server, key, body string) (string, string) {
	t.Helper()
	status, out := call(t, srv, http.MethodPost, "/api/v1/tasks", "local-user", key, body)
	if status != http.StatusCreated {
		t.Fatalf("建 task 该 201，实际 %d: %s", status, out)
	}
	id, _ := mustParseMap(t, out)["id"].(string)
	if id == "" {
		t.Fatalf("首攻响应里没有 id: %s", out)
	}
	return id, out
}

// callIfMatch 是本文件自带的小助手：`call` 不带 If-Match 这一格，而这一批要断的就是它。
// 复制而不是改 `call` 的签名，是为了不去动那 130 多个既有调用点。
func callIfMatch(t *testing.T, srv *httptest.Server, method, path, principal, idemKey, ifMatch, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	if principal != "" {
		req.Header.Set("X-Lucero-Principal", principal)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

// harnessWithSession 在真库 harness 上再种一行可挂的来源会话；withGet 决定接不接读面。
func harnessWithSession(t *testing.T, sessID string, withGet bool) (*httptest.Server, *db.DB) {
	t.Helper()
	srv, d := taskHarness(t, withGet)
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ai-get', 'get', 'get', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, created_at, updated_at)
		VALUES (?, 'ai-get', ?, 1, 1)`, sessID, "remote-"+sessID); err != nil {
		t.Fatal(err)
	}
	return srv, d
}

// TestGetTaskMatchesTheListItemShape 钉的是"同一个 task 在两个出口长得一样"：
// 逐键比（键集合 + 每个键的值）。单条读如果哪天自己攒出第二套字段，这一格先红。
func TestGetTaskMatchesTheListItemShape(t *testing.T) {
	srv, _ := taskHarness(t, true)
	id, created := createForGet(t, srv, "k-get-shape", `{"title":"读回同形","priority":"LOW"}`)

	status, got := call(t, srv, http.MethodGet, "/api/v1/tasks/"+id, "", "", "")
	if status != http.StatusOK {
		t.Fatalf("GET 单条该 200，实际 %d: %s", status, got)
	}
	lstStatus, lst := call(t, srv, http.MethodGet, "/api/v1/tasks", "", "", "")
	if lstStatus != http.StatusOK {
		t.Fatalf("GET 列表该 200，实际 %d: %s", lstStatus, lst)
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(lst), &page); err != nil {
		t.Fatal(err)
	}
	var listItem map[string]any
	for _, it := range page.Items {
		if it["id"] == id {
			listItem = it
		}
	}
	if listItem == nil {
		t.Fatalf("列表里没有刚建的那条 %s（items=%s）", id, lst)
	}
	single := mustParseMap(t, got)
	if keysOf(single) != keysOf(listItem) {
		t.Errorf("单条读与列表项的键集合不同：\n单条=%s\n列表=%s", keysOf(single), keysOf(listItem))
	}
	for k, v := range single {
		if lv, ok := listItem[k]; !ok || lv != v {
			t.Errorf("键 %q 两处不一致：单条=%v 列表=%v（首攻响应=%s）", k, v, lv, created)
		}
	}
}

// TestGetTaskVersionDrivesIfMatch 是 §7 那条 oracle 链的前置：链走到尾要靠这条读拿到当前版本，
// 再拿它当 `If-Match` 打状态动作。所以这里断的不是"版本号长得对"，而是"**它真能推动一次合法迁移**"。
func TestGetTaskVersionDrivesIfMatch(t *testing.T) {
	srv, d := taskHarness(t, true)
	id, _ := createForGet(t, srv, "k-get-ifmatch", `{"title":"版本能当 If-Match 用","priority":"LOW"}`)

	_, got := call(t, srv, http.MethodGet, "/api/v1/tasks/"+id, "", "", "")
	var one getTaskJSON
	if err := json.Unmarshal([]byte(got), &one); err != nil {
		t.Fatal(err)
	}
	if one.Version != 1 {
		t.Fatalf("新建的 task 该是 v1（GET 读回来的版本要可信，否则下面的 If-Match 没意义），实际 v%d: %s", one.Version, got)
	}
	status, out := callIfMatch(t, srv, "POST", "/api/v1/tasks/"+id+":queue", "local-user", "k-get-queue",
		strconv.FormatInt(one.Version, 10), `{"adapter_key":"fake"}`)
	if status != http.StatusOK {
		t.Fatalf("用 GET 读回的 v%d 当 If-Match 该把 :queue 跑成 200，实际 %d: %s", one.Version, status, out)
	}
	// 迁移真发生了 ⇒ 再读一次版本必须往前走（不然上面那个 200 可能只是没落库的空转）。
	_, after := call(t, srv, http.MethodGet, "/api/v1/tasks/"+id, "", "", "")
	var two getTaskJSON
	if err := json.Unmarshal([]byte(after), &two); err != nil {
		t.Fatal(err)
	}
	if two.Status != "PREPARING" || two.Version != 3 {
		// 分派腿把 Queue+Create+Prepare 收成一笔事务，对外只见终态 PREPARING v3（本包既有那条 :queue 用例同一口径）。
		// 这里断的是"**GET 读回来的 v1 真把版本推到了 v3**"——版本号不是装饰品，它得能驱动一次合法迁移。
		t.Errorf("用 GET 的 v%d 打完 :queue 之后该是 PREPARING v3，实际 %s v%d", one.Version, two.Status, two.Version)
	}
	if n := countRows(t, d, "tasks"); n != 1 {
		t.Errorf("全程该只有 1 行 task，实际 %d", n)
	}
	if n := countRows(t, d, "executions"); n != 1 {
		t.Errorf("这一次 :queue 该建 1 条 Execution（否则上面那个 200 是空转），实际 %d", n)
	}
}

// TestGetTaskUnknownIs404Not501 钉 §15.2 那条分界：能力已在（读面注入了）、资源不在 ⇒ 404 not_found。
// 这一格就是第二发反例的落点：把这条写成 501，这里必红。
func TestGetTaskUnknownIs404Not501(t *testing.T) {
	srv, _ := taskHarness(t, true)
	code, body := call(t, srv, http.MethodGet, "/api/v1/tasks/t-missing", "local-user", "", "")
	if code != http.StatusNotFound || codeOf(t, body) != "not_found" {
		t.Errorf("未知 id 该 404 not_found（不是 501：能力已经在了），实际 %d %s", code, body)
	}
}

// TestGetTaskWithoutReadFaceIs501 与上一条成对：**唯一区别是有没有注入读面**，答案就必须从 501 翻到 404。
// 只断 501 那一侧不够（那可能是路由根本没挂）；两格一起才挡住"把没接线伪装成没找到"。
func TestGetTaskWithoutReadFaceIs501(t *testing.T) {
	srv, _ := taskHarness(t, false)
	code, body := call(t, srv, http.MethodGet, "/api/v1/tasks/t-none", "local-user", "", "")
	if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
		t.Errorf("读面没注入时 GET 该 501 not_implemented（C28②），实际 %d %s", code, body)
	}
}

// TestGetTaskVerbSplitUnchanged 钉接 GET 进去之后邻居没被带跑：PATCH 仍 501（还是没接线），
// POST/PUT/DELETE 仍 405（这条路径不接受它们）。
func TestGetTaskVerbSplitUnchanged(t *testing.T) {
	srv, _ := taskHarness(t, true)
	// GET 那一行要用**真存在的 id**：拿不存在的 id 去断 200，红了也分不清是"方法没接"还是"资源不在"
	// —— 那正是这个文件里 404/501 两格要分开的同一条理由。
	id, _ := createForGet(t, srv, "k-get-split", `{"title":"分流钉子","priority":"LOW"}`)
	for _, tc := range []struct {
		method, path, wantCode string
		want                   int
	}{
		{http.MethodPatch, "/api/v1/tasks/" + id, "not_implemented", http.StatusNotImplemented},
		{http.MethodPost, "/api/v1/tasks/" + id, "method_not_allowed", http.StatusMethodNotAllowed},
		{http.MethodPut, "/api/v1/tasks/" + id, "method_not_allowed", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/api/v1/tasks/" + id, "method_not_allowed", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/tasks/" + id, "", http.StatusOK},
		// 读面接上之后，未知 id 仍然只能是 404，不许被上面的分流写成 405/501（同一条路径三种答案的分界）。
		{http.MethodGet, "/api/v1/tasks/t-split-none", "not_found", http.StatusNotFound},
	} {
		code, body := call(t, srv, tc.method, tc.path, "local-user", "k-split", `{}`)
		if code != tc.want {
			t.Errorf("%s %s 该 %d，实际 %d %s", tc.method, tc.path, tc.want, code, body)
			continue
		}
		// 码名逐格钉死，不借 `codeName(status)`：那个助手只认 501/405 两档，
		// 拿它比 404 会得到空串 —— 于是"码对、信封错"这一格会假绿。
		if tc.wantCode != "" && codeOf(t, body) != tc.wantCode {
			t.Errorf("%s %s 该 %d %s，实际码对但信封是 %s", tc.method, tc.path, tc.want, tc.wantCode, body)
		}
	}
}

// TestGetTaskReadsBackOriginSessionID 是 REQ-LK-1 那条判据里今天做不到的半条（第 268 条报过）：
// "建带 session 的 task 后 `GET /tasks/{id}` 的 `session_id` 非空"。getTask 接上之后它才第一次可执行。
func TestGetTaskReadsBackOriginSessionID(t *testing.T) {
	srv, d := harnessWithSession(t, "sess-get-origin", true)
	id, _ := createForGet(t, srv, "k-get-lk", `{"title":"带来源会话","priority":"LOW","session_id":"sess-get-origin"}`)

	_, got := call(t, srv, http.MethodGet, "/api/v1/tasks/"+id, "", "", "")
	var one getTaskJSON
	if err := json.Unmarshal([]byte(got), &one); err != nil {
		t.Fatal(err)
	}
	if one.SessionID != "sess-get-origin" {
		t.Errorf("单条读该带回 ORIGIN 会话（判据那半条），实际 %q: %s", one.SessionID, got)
	}
	if n, _, _ := linkRowsOn(t, d, id); n != 1 {
		t.Errorf("读不该改库：link 该还是 1 行，实际 %d", n)
	}
}

// TestGetTaskRejectsMalformedIDs 钉畸形路径仍走"形状不认识 ⇒ 501"，不许被单条读抢成 200/404。
func TestGetTaskRejectsMalformedIDs(t *testing.T) {
	srv, _ := taskHarness(t, true)
	for _, p := range []string{"/api/v1/tasks/", "/api/v1/tasks//", "/api/v1/tasks/a/b", "/api/v1/tasks/a:b:c"} {
		code, body := call(t, srv, http.MethodGet, p, "", "", "")
		if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
			t.Errorf("GET %s 该 501（形状不认识，Core 不猜），实际 %d %s", p, code, body)
		}
	}
	// 精确路径那条没被子树抢走：列表仍 200。
	if code, body := call(t, srv, http.MethodGet, "/api/v1/tasks", "", "", ""); code != http.StatusOK {
		t.Errorf("GET /api/v1/tasks 该还是 200，实际 %d %s", code, body)
	}
}

func keysOf(m map[string]any) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}
