package httpapi_test

import (
	"net/http"
	"testing"
)

// TestTaskItemPathFollowsC28Ruling 是 T-195④ 的那处真 bug 的钉子（主控 03:5x 指出、我按字核过 yaml）：
// `GET /api/v1/tasks/{id}`（yaml `getTask`，09-22 LAG-1 写进 §3）与 `PATCH`（`updateTask`）都是**合法方法**，
// 而这两条今天没接线 ⇒ C28② 说这该回 **501**，不是 405。
// 原实现在子树第一句就 `r.Method != POST → 405`，等于叫客户去换方法——可它换成 GET 还是没接线，
// 这正是 §15.2 那格写死的"501 与 405 不许互换"。
func TestTaskItemPathFollowsC28Ruling(t *testing.T) {
	srv, _ := newTaskHarness(t)

	// declared、方法合法、能力没接线 ⇒ 501 + not_implemented。
	for _, method := range []string{"GET", "PATCH"} {
		if code, body := call(t, srv, method, "/api/v1/tasks/t-none", "local-user", "k-item", ""); code != http.StatusNotImplemented ||
			codeOf(t, body) != "not_implemented" {
			t.Errorf("%s /api/v1/tasks/{id} 该 501 not_implemented（declared 未接），实际 %d %s", method, code, body)
		}
	}
	// 这条路径**不接受**的方法 ⇒ 405（同一个路径上两种答案的分界就在这里）。
	for _, method := range []string{"PUT", "DELETE", "POST"} {
		if code, body := call(t, srv, method, "/api/v1/tasks/t-none", "local-user", "k-item", ""); code != http.StatusMethodNotAllowed ||
			codeOf(t, body) != "method_not_allowed" {
			t.Errorf("%s /api/v1/tasks/{id} 该方法不被接受，该 405，实际 %d %s", method, code, body)
		}
	}
	// custom verb 那一类只声明 POST ⇒ 非 POST 仍是 405（这一格不许被上面的修法带跑）。
	if code, body := call(t, srv, "GET", "/api/v1/tasks/t-none:queue", "", "", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("GET 到 :queue 该 405（那条路径只接受 POST），实际 %d %s", code, body)
	}
	// 形状不认识的 ⇒ 501，且**不许**因为"方法对不上"先被判成 405（修之前的顺序 bug 就长这样）。
	for _, bad := range []string{
		"/api/v1/tasks/:queue",    // 空 id
		"/api/v1/tasks/a/b",       // 多一段
		"/api/v1/tasks/a:b:queue", // id 里还带冒号
		"/api/v1/tasks/",          // 只有前缀
	} {
		for _, method := range []string{"GET", "PUT"} {
			if code, body := call(t, srv, method, bad, "local-user", "k-shape", ""); code != http.StatusNotImplemented ||
				codeOf(t, body) != "not_implemented" {
				t.Errorf("%s %s 是畸形形状，该 501（不猜形状），实际 %d %s", method, bad, code, body)
			}
		}
	}
}

// TestTaskListRouteStillAnswers 是"修一条分流不许弄坏邻居"的那格：精确路径 `/api/v1/tasks`
// 由另一条 handler 接（列表 GET / 建单 POST），子树重写之后它必须照旧。
func TestTaskListRouteStillAnswers(t *testing.T) {
	srv, _ := newTaskHarness(t)
	if code, _ := call(t, srv, "GET", "/api/v1/tasks", "", "", ""); code != http.StatusOK {
		t.Errorf("GET /api/v1/tasks 该还是 200，实际 %d", code)
	}
	if code, body := call(t, srv, "DELETE", "/api/v1/tasks", "local-user", "", ""); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("DELETE /api/v1/tasks 该 405，实际 %d %s", code, body)
	}
	// 建单那条不能被误判成"单条路径"：它是精确路径、POST 合法。
	code, body := call(t, srv, "POST", "/api/v1/tasks", "local-user", "k-new", `{"title":"新任务"}`)
	if code != http.StatusCreated {
		t.Errorf("POST /api/v1/tasks 该还是 201，实际 %d %s", code, body)
	}
}
