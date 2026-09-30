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
