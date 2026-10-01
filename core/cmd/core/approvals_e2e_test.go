package main

// `GET /api/v1/approvals` 走真装配那一格（T-414 ③）。
//
// `api/http` 那包用的是替身读法，判的是出口形状与文案；这里判的是**装配真接上了**：
// 真 boot 出来的 Core、真库（空的 approvals 表）、真 `approval.Service.ListPage`。
// 两件只有在这条路上才证得到：
//   - 空表答 200 + `items: []` —— 这是真话（"今天确实没有审批"），与"没接线答 501"是两种结局，
//     替身测不出这两者会不会被混起来；
//   - 词表外的 state 在这里会得到 **400**，而那句判定住在 `internal/approval` ——
//     它响就说明 transport 真把请求交到了那一侧手里，不是在我这一层自己猜了一份词表。
//
// 这一包**一行都不往 approvals 表里写**（那张表对 backend 是绝对禁区，连测试也不破例），
// 所以它只判"读得到空页 + 过滤条件真到了那一侧"，不判"读得出某一行"（那由 approval 自己那批用例判）。

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestApprovalsListIsAssembledInRealCore(t *testing.T) {
	core := bootAndServe(t)
	base := "http://" + core.endpoint + "/api/v1/approvals"

	var page struct {
		Items      *[]map[string]any `json:"items"`
		NextCursor *string           `json:"next_cursor"`
	}
	if err := getJSON(t, base, core.token, &page); err != nil {
		t.Fatalf("真装配下的这条口没走通：%v", err)
	}
	if page.Items == nil {
		t.Fatal("items 这个键必须在（空页也要出数组，不是 null、不是省略）")
	}
	if len(*page.Items) != 0 {
		t.Errorf("空表该出空数组，实际 %d 条", len(*page.Items))
	}
	if page.NextCursor == nil || *page.NextCursor != "" {
		t.Errorf(`next_cursor 必须是空串这个写法（REQ-F-19：键必在、不许 null），实际 %v`, page.NextCursor)
	}

	// 词表内的过滤条件也得走通（PENDING 那一支在 approval 里还要多叠一条 deadline 判定）。
	var pending struct {
		Items []map[string]any `json:"items"`
	}
	if err := getJSON(t, base+"?state=PENDING&limit=10", core.token, &pending); err != nil {
		t.Fatalf("state=PENDING 那一发没走通：%v", err)
	}

	// 词表外：400，且理由点名"approval 那一侧"——这一发同时证明 400 是**那一侧**判的，
	// 不是 transport 自己抄了一份词表（抄的那份会漂移，REQ-A8 就是为这个立的）。
	code, body := statusOf(t, http.MethodGet, base+"?state=definitely_not_a_state", core.token)
	if code != http.StatusBadRequest {
		t.Errorf("词表外的 state 该 400（判定住在 approval），实际 %d：%s", code, body)
	}

	// 而没接线那一格仍然是 501 —— 由 handler 自己那包判（TestListApprovalsNotWiredIsNotAnEmptyPage），
	// 这里补的是反向那一半：**真装配里它不该是 501**，否则接线等于没接。
	if code, body = statusOf(t, http.MethodGet, base, core.token); code == http.StatusNotImplemented {
		t.Errorf("真装配下的这条口还在 501 ⇒ cmd/core 那根线没接上：%s", body)
	}
}

// TestApprovalByIdAndResolveAreAssembledInRealCore 判的是 T-426 ① 那一对接进了**真装配**。
// 两发都不需要库里有一行审批，所以这张表对 backend 的绝对禁区一条没破：
//   - `GET /approvals/{id}` ⇒ **404**，不是 501。404 说的是"没有这一单"——那一侧真去查了才知道没有；
//     501 才说"没接线"。这一发同时证明 by-id 那半走的是 `Service.GetByID`（`requireID` 先于取行、
//     not-found 用自己的哨兵），不是 transport 里另拼的一份读法；
//   - `POST /approvals/{id}:resolve` ⇒ 对不存在的单回 **404**，且**下一发再读仍是 404**
//     （决议那一腿真到了库里、没写出不存在的一行、也没把"没找到"折成 200 或 501）。
func TestApprovalByIdAndResolveAreAssembledInRealCore(t *testing.T) {
	core := bootAndServe(t)
	const missing = "ap-not-here"

	code, body := statusOf(t, http.MethodGet, "http://"+core.endpoint+"/api/v1/approvals/"+missing, core.token)
	if code == http.StatusNotImplemented {
		t.Fatalf("真装配下 by-id 还在 501 ⇒ 这一对没接进来：%s", body)
	}
	if code != http.StatusNotFound || !strings.Contains(body, "没有这条 Approval") {
		t.Errorf("不存在的单该 404 并说清，实际 %d：%s", code, body)
	}

	rcode, rbody := postJSON(t, "http://"+core.endpoint+"/api/v1/approvals/"+missing+":resolve",
		core.token, `{"state":"APPROVED","note":"e2e 只问不存在的单"}`, "k-e2e-resolve-1")
	if rcode == http.StatusNotImplemented {
		t.Fatalf("真装配下 :resolve 还在 501 ⇒ 裁决那一半没接进来：%s", rbody)
	}
	if rcode != http.StatusNotFound {
		t.Errorf("对不存在的单下决议该 404（判定住在 approval 那侧），实际 %d：%s", rcode, rbody)
	}
	// 反向那一半：被拒的决议不许留下任何"看起来成了"的痕迹。
	if code, body = statusOf(t, http.MethodGet, "http://"+core.endpoint+"/api/v1/approvals/"+missing, core.token); code != http.StatusNotFound {
		t.Errorf("一发失败的决议之后这一单还是该 404，实际 %d：%s", code, body)
	}
	// 空 id 那一格：`{id}` 段为空是另一条路径形状，不能被 by-id 那条口悄悄吞掉（这里只确认真装配的话也是真的）。
	if code, body = statusOf(t, http.MethodGet, "http://"+core.endpoint+"/api/v1/approvals/", core.token); code == http.StatusOK {
		t.Errorf("空 id 不该被当成一次正常的详情读，实际 200：%s", body)
	}
}

// postJSON 是给真装配那一头发写请求用的小助手：带 Bearer、带写主体（v0.1 的 401 闸在中间件里），
// 目的地只有本机回环上那一台真 Core。
func postJSON(t *testing.T, url, token, body, idem string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Lucero-Principal", "local-user")
	req.Header.Set("Content-Type", "application/json")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("这一发没打到本机 Core（%s）：%v", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw)
}

// statusOf 发一发并原样带回状态码与正文（上面两条反向判据都要看正文，不能只看码）。
// 目的地只有本机回环上那一台真 Core（`core.endpoint` 由 boot 写进临时目录），不是外部地址。
func statusOf(t *testing.T, method, url, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("这一发没打到本机 Core（%s）：%v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}
