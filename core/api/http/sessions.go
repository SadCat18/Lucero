package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/SadCat18/lucero/core/internal/session"
)

// SessionSyncer 是 `POST /api/v1/sessions/{id}:sync` 需要的业务面（契约③ §7：transport 只做投影）。
// 判定全在 internal/session 的 Service 里，这一层只把三条哨兵错误换成 §15.2 的状态码。
type SessionSyncer interface {
	SyncFromLocal(ctx context.Context, id string) (session.SyncOutcome, error)
}

// syncResultView 是 yaml `SyncResult` 的 required 两键 + 可选的 state_confidence。
// **`added_messages` 故意不出键**：它在 yaml 里是可选整数，而这条口今天不知道"新增了几条"
// —— §12 与 REQ-F-21 ② 禁的正是拿 `0` 冒充"不知道"。要出这个键得等真同步链能数出增量的那天。
type syncResultView struct {
	SyncState  string  `json:"sync_state"`
	NextCursor *string `json:"next_cursor"`
	Confidence string  `json:"state_confidence"`
}

// registerSessionRoutes 与 notifications/projects 同规矩：没注入就维持 catch-all 的 501，不给前端半套面。
// 分流口径按 C28② 那一句写死：**方法不被这条路径接受 ⇒ 405；路由存在、能力没接线 ⇒ 501**。
// 所以 `:sync` 上的 GET 是 405，而 `/api/v1/sessions/{id}` 那种"以后会有 GET"的路径是 501 ——
// 回 405 等于说"这条路径永远不接受 GET"，那是假话。
func registerSessionRoutes(mux *http.ServeMux, c Config) {
	if c.Sessions == nil {
		return
	}
	mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		id, verb, ok := splitSessionVerb(r.URL.Path)
		if !ok || verb != "sync" {
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"sessions 这一腿只接了 :sync；GET 与别的 custom verb 都还没接线", true)
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				":sync 只能 POST", false)
			return
		}
		syncSession(w, r, c, id)
	})
}

// splitSessionVerb 认两种形状：`/api/v1/sessions/{id}:sync` 与 `/api/v1/sessions/{id}`（verb 空）。
// 不照抄 splitTaskAction：那条不认"没有 verb"的形状，而这条路径上"没有 verb"是要回 501 的合法输入。
func splitSessionVerb(path string) (id, verb string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/sessions/")
	if rest == "" || strings.Contains(rest, "/") {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, ':')
	if i < 0 {
		return rest, "", true
	}
	if i == 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

func syncSession(w http.ResponseWriter, r *http.Request, c Config, id string) {
	out, err := c.Sessions.SyncFromLocal(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, syncResultView{
			SyncState: string(out.State), NextCursor: out.Cursor, Confidence: string(out.Confidence)})
	case errors.Is(err, session.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, "session_not_found", "没有这条 Session", false)
	case errors.Is(err, session.ErrNoLocalData):
		// C25 裁的那一支：完全无 Provider 可问 ⇒ 503，且码用现成的 `adapter_unresponsive`（§15.2 已绑 503）。
		writeError(w, http.StatusServiceUnavailable, "adapter_unresponsive",
			"没有可用 Provider，本地也没有能回放的增量", true)
	default:
		serverError(w, r, err)
	}
}
