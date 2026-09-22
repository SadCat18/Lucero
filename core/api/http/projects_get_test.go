package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestGetProjectSingleMatchesListAndPatchShapes 钉单条读的三件事：
// ① 读回来的就是库里那一行（含 canonicalize 后的路径与 NULL 那几格）；
// ② **归档的项目照样读得到** —— 契约在这条路上没给过滤位，藏起来就是我自己加的语义（C26 同一课）；
// ③ 同一个项目在 POST 响应、列表项、单条读、PATCH 响应四个出口上**键集合一模一样**。
func TestGetProjectSingleMatchesListAndPatchShapes(t *testing.T) {
	srv, d := newProjectHarness(t, true)
	defer srv.Close()

	id := createdID(t, srv, "k-get-1", `{"name":"单条读的项目","canonical_path":"D:\\get\\one\\"}`)
	// 归档一条（直接改库，因为 :archive 那条 verb 还没接）：它必须仍可按 id 读到。
	if _, err := d.Read.Exec(`UPDATE projects SET status = 'ARCHIVED', archived_at = 5000 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}

	code, body := call(t, srv, "GET", "/api/v1/projects/"+id, "", "", "")
	if code != http.StatusOK {
		t.Fatalf("归档不该让单条读变 404，实际 %d %s", code, body)
	}
	one := mustParseMap(t, body)
	if one["id"] != id || one["name"] != "单条读的项目" || one["canonical_path"] != "d:\\get\\one" {
		t.Errorf("读回来的不是库里那一行: %s", body)
	}
	if one["status"] != "ARCHIVED" || one["archived_at"] != float64(5000) {
		t.Errorf("归档状态没如实带出: %v / %v", one["status"], one["archived_at"])
	}
	if one["trust_domain_id"] != nil {
		t.Errorf("库里是 NULL 的列该出 null，实际 %#v", one["trust_domain_id"])
	}

	_, listed, listBody := listProjectsGet(t, srv, "?limit=10")
	var inList map[string]any
	for _, it := range listed["items"].([]any) {
		if m := it.(map[string]any); m["id"] == id {
			inList = m
		}
	}
	if inList == nil {
		t.Fatalf("列表里找不到同一条: %s", listBody)
	}
	if keySet(inList) != keySet(one) {
		t.Errorf("单条与列表两条出口键集合不一样：单条=%s 列表=%s", keySet(one), keySet(inList))
	}
	patchCode, patchBody := patchProject(t, srv, "/api/v1/projects/"+id, "1", `{"name":"改一次名"}`)
	if patchCode != http.StatusOK {
		t.Fatalf("PATCH 应 200，实际 %d %s", patchCode, patchBody)
	}
	if k := keySet(mustParseMap(t, patchBody)); k != keySet(one) {
		t.Errorf("PATCH 与单条读键集合不一样：%s vs %s", k, keySet(one))
	}
}

// TestGetProjectUnknownIs404 与 PATCH 同码同词：库里没有就是 404 `not_found`，
// 不拿 501 说"没接线"、也不拿 200 + 空对象糊过去。
func TestGetProjectUnknownIs404(t *testing.T) {
	srv, _ := newProjectHarness(t, true)
	defer srv.Close()

	code, body := call(t, srv, "GET", "/api/v1/projects/does-not-exist", "", "", "")
	if code != http.StatusNotFound || codeOf(t, body) != "not_found" {
		t.Errorf("该 404 not_found，实际 %d %s", code, body)
	}
}

// TestGetProjectIsNotAuthFree 顺手钉一件容易漏的：单条读走的是同一条全局 Bearer 闸（GET 不要求 Principal 头，
// 但缺口令必须 401 —— 别让新接的一条口成了免鉴权的读口）。
func TestGetProjectIsNotAuthFree(t *testing.T) {
	srv, _ := newProjectHarness(t, true)
	defer srv.Close()
	id := createdID(t, srv, "k-get-2", `{"name":"要口令的读口"}`)

	req, err := http.NewRequest("GET", srv.URL+"/api/v1/projects/"+id, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("没带口令的单条读该 401，实际 %d", resp.StatusCode)
	}
}
