package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// patchProject 发一次 PATCH。If-Match 可以不传（yaml 里它不是必填）。
func patchProject(t *testing.T, srv *httptest.Server, path, ifMatch, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("PATCH", srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Lucero-Principal", "local-user")
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func createdID(t *testing.T, srv *httptest.Server, key, body string) string {
	t.Helper()
	code, resp := post(t, srv, key, body)
	if code != http.StatusCreated {
		t.Fatalf("建项目应 201，实际 %d %s", code, resp)
	}
	return mustParseMap(t, resp)["id"].(string)
}

// TestUpdateProjectWritesOneEventAndReadsBack 是这条口的正证：响应必须等于**读回库里那一行**
// （C18 裁定 ②：不回显请求体），版本推到 2，且恰有一行 `project_updated`、不多发 `project_created`。
func TestUpdateProjectWritesOneEventAndReadsBack(t *testing.T) {
	srv, d := newProjectHarness(t, true)
	defer srv.Close()
	id := createdID(t, srv, "k-upd-1", `{"name":"待改的项目","canonical_path":"d:\\upd\\"}`)

	code, body := patchProject(t, srv, "/api/v1/projects/"+id, "1", `{"name":"改过名的项目","metadata":{"k":"v"}}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH 应 200，实际 %d %s", code, body)
	}
	m := mustParseMap(t, body)
	if m["name"] != "改过名的项目" || m["version"] != float64(2) {
		t.Errorf("响应不对（版本该推到 2）: %s", body)
	}
	if md, ok := m["metadata"].(map[string]any); !ok || md["k"] != "v" {
		t.Errorf("metadata 没带回来: %v", m["metadata"])
	}
	var stored string
	if err := d.Read.QueryRow(`SELECT name FROM projects WHERE id = ? AND version = 2`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "改过名的项目" {
		t.Errorf("库里是 %q，响应却说 %v —— 响应必须是读回来的", stored, m["name"])
	}
	if n := countQuery(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'project_updated'`); n != 1 {
		t.Errorf("project_updated 应恰一行，实际 %d", n)
	}
	if n := countQuery(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'project_created'`); n != 1 {
		t.Errorf("更新不该再造一条 created，实际 %d", n)
	}
	var payload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE event_type = 'project_updated'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, "name") || !strings.Contains(payload, "metadata") {
		t.Errorf("payload 缺 §6 登记的那两键: %s", payload)
	}
}

// TestUpdateProjectIfMatchGate 钉 If-Match 三格：不符 ⇒ 409 且一行都不写；不带 ⇒ 照改；非正整数 ⇒ 400。
func TestUpdateProjectIfMatchGate(t *testing.T) {
	srv, d := newProjectHarness(t, true)
	defer srv.Close()
	id := createdID(t, srv, "k-upd-2", `{"name":"带门的项目"}`)

	if code, body := patchProject(t, srv, "/api/v1/projects/"+id, "7", `{"name":"不该落地"}`); code != http.StatusConflict ||
		codeOf(t, body) != "version_conflict" {
		t.Errorf("版本不符该 409 version_conflict，实际 %d %s", code, body)
	}
	if n := countQuery(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'project_updated'`); n != 0 {
		t.Errorf("被拒的更新不许留下事件，实际 %d", n)
	}
	if code, body := patchProject(t, srv, "/api/v1/projects/"+id, "abc", `{}`); code != http.StatusBadRequest ||
		codeOf(t, body) != "validation_failed" {
		t.Errorf("If-Match 非正整数该 400，实际 %d %s", code, body)
	}
	if code, body := patchProject(t, srv, "/api/v1/projects/"+id, "", `{"name":"没带门也能改"}`); code != http.StatusOK {
		t.Errorf("不带 If-Match 该照改（yaml 里它不必填），实际 %d %s", code, body)
	}
}

// TestUpdateProjectCannotTouchStatus 是这条口的存在理由：状态不是跳过来改的。
// 而且**不能静默丢掉**客户端写的 status（那样它会以为改成功了）⇒ 不认识的键一律 400。
func TestUpdateProjectCannotTouchStatus(t *testing.T) {
	srv, d := newProjectHarness(t, true)
	defer srv.Close()
	id := createdID(t, srv, "k-upd-3", `{"name":"状态守门员"}`)

	if code, body := patchProject(t, srv, "/api/v1/projects/"+id, "1", `{"status":"ARCHIVED"}`); code != http.StatusBadRequest ||
		codeOf(t, body) != "validation_failed" {
		t.Errorf("带 status 的 PATCH 该 400，实际 %d %s", code, body)
	}
	var status string
	var version int64
	if err := d.Read.QueryRow(`SELECT status, version FROM projects WHERE id = ?`, id).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "ACTIVE" || version != 1 {
		t.Errorf("被拒的请求留下了痕：status=%q version=%d", status, version)
	}
}

// TestUpdateProjectEmptyPatchWritesNothing 钉空 PATCH：200，但版本不动、事件不发。
// "什么都没变还发一条 project_updated"就是把事件写成仪式，与 §9 那条同一条线。
func TestUpdateProjectEmptyPatchWritesNothing(t *testing.T) {
	srv, d := newProjectHarness(t, true)
	defer srv.Close()
	id := createdID(t, srv, "k-upd-4", `{"name":"空改动"}`)

	code, body := patchProject(t, srv, "/api/v1/projects/"+id, "1", `{}`)
	if code != http.StatusOK {
		t.Fatalf("空 PATCH 该 200，实际 %d %s", code, body)
	}
	if v := mustParseMap(t, body)["version"]; v != float64(1) {
		t.Errorf("空改动不许推版本，实际 %v", v)
	}
	if n := countQuery(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'project_updated'`); n != 0 {
		t.Errorf("没改任何东西却发了事件：%d 行", n)
	}
}

// TestProjectItemRouteShapes 钉子树路径上其它形状：未接的 GET ⇒ 501；不接受的方法 ⇒ 405；
// 形状不认识（空 id / 多一段 / 带冒号）⇒ 501，而且不许拿它去查库回一个假装合法的 404。
func TestProjectItemRouteShapes(t *testing.T) {
	srv, d := newProjectHarness(t, true)
	defer srv.Close()

	// 读通道已装配 ⇒ 这条子树上"没有这个项目"该老实 404（第五十批把 GET 单接通了）。
	if code, body := call(t, srv, "GET", "/api/v1/projects/p-none", "", "", ""); code != http.StatusNotFound ||
		codeOf(t, body) != "not_found" {
		t.Errorf("GET 不存在的项目该 404 not_found，实际 %d %s", code, body)
	}
	// 没装配读通道时，同一条腿必须退回 501（不是 404、也不是 200 + 空对象）。
	unwired, _ := newProjectHarness(t)
	defer unwired.Close()
	if code, body := call(t, unwired, "GET", "/api/v1/projects/p-none", "", "", ""); code != http.StatusNotImplemented ||
		codeOf(t, body) != "not_implemented" {
		t.Errorf("没注入 ProjectRead 时 GET 单条该 501，实际 %d %s", code, body)
	}
	if code, body := call(t, srv, "PUT", "/api/v1/projects/p-none", "local-user", "", "{}"); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("PUT 该 405，实际 %d %s", code, body)
	}
	for _, path := range []string{"/api/v1/projects/", "/api/v1/projects/a/b", "/api/v1/projects/p-none:archive"} {
		if code, body := call(t, srv, "GET", path, "", "", ""); code != http.StatusNotImplemented ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("%s 该 501（形状不认 / 没接的 verb），实际 %d %s", path, code, body)
		}
		// 同一批形状也要挡在 PATCH 前面：放它过去就是拿"必然不存在的 id"去查库、
		// 回一个假装合法的 404 —— 那 501 与 404 的分别是这条守卫的存在理由。
		if code, body := patchProject(t, srv, path, "", `{"name":"形状测试"}`); code != http.StatusNotImplemented ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("PATCH %s 该被形状守卫拦成 501，实际 %d %s", path, code, body)
		}
	}
	if code, body := patchProject(t, srv, "/api/v1/projects/nope", "", `{"name":"没这个项目"}`); code != http.StatusNotFound ||
		codeOf(t, body) != "not_found" {
		t.Errorf("不存在的项目该 404 not_found，实际 %d %s", code, body)
	}
	var projects int
	if err := d.Read.QueryRow(`SELECT count(*) FROM projects WHERE id = 'nope'`).Scan(&projects); err != nil {
		t.Fatal(err)
	}
}
