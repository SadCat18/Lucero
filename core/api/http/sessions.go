package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
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

// registerSessionRoutes 与 notifications/projects 同规矩：两格依赖都没注入就维持 catch-all 的 501，不给前端半套面。
// 分流口径按 C28② 写死：**方法不被这条路径接受 ⇒ 405；路由存在、能力没接线 ⇒ 501**。
// 同一条前缀路径上今天有两格能力：`{id}` 的单条读（GET，要 SessionRead）与 `{id}:sync`（POST，要 Sessions）。
// 各自的"没接线"分支分别回 501 —— 不许因为另一格装上了就拿 405 说"这条路不接受这个方法"，那是假话。
func registerSessionRoutes(mux *http.ServeMux, c Config) {
	if c.Sessions == nil && c.SessionRead == nil {
		return
	}
	mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		id, verb, ok := splitSessionVerb(r.URL.Path)
		switch {
		case !ok:
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"这条路径的形状我不认识（空 id、空 verb 或多一段都不摸业务面）", true)
		case verb == "":
			if c.SessionRead == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"GET /api/v1/sessions/{id} 还没装配读通道", true)
				return
			}
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/sessions/{id} 只有 GET；:sync 是带冒号的那条", false)
				return
			}
			getSession(w, r, c, id)
		case verb == "sync":
			if c.Sessions == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented", ":sync 还没装配业务面", true)
				return
			}
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", ":sync 只能 POST", false)
				return
			}
			syncSession(w, r, c, id)
		default:
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"没听过的 custom verb 一律 501，不猜它该走哪条腿", true)
		}
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

// SessionReader 是 sessions 这一腿的读面（列表 + 单条），由 *session.Repository 直接满足。
// 它与 SessionSyncer 是两个字段而不是一个接口：一个挂在 Repository、一个挂在 Service，
// 合并成一个接口就会逼 cmd/core 造一个只为凑签名的转发类型。
type SessionReader interface {
	Get(ctx context.Context, id string) (session.Session, error)
	ListPage(ctx context.Context, f session.ListFilter) ([]session.Session, error)
}

// sessionView 出的是 yaml `Session` 里"必填六个 + 域模型真带着的那几个可空项"。
// 三个可空字符串走 nullableString：域模型把库里的 NULL 折成了空串，出口再还原成 null，
// 免得前端拿到 ""（同一件事两种写法，正是 §12/REQ-F-22 要收的形状）。
// `provider_status_raw` / `metadata` **不出键**：Repository 根本不读那两列，
// 在这里编一个 `{}` 填上就是把"没读"报成"是空的"；`archived_at` 同理（域模型只留了个 bool）。
type sessionView struct {
	ID                string  `json:"id"`
	AdapterInstanceID string  `json:"adapter_instance_id"`
	ProjectID         *string `json:"project_id"`
	RemoteSessionID   *string `json:"remote_session_id"`
	Title             *string `json:"title"`
	Managed           bool    `json:"managed"`
	SyncState         string  `json:"sync_state"`
	SyncCursor        *string `json:"sync_cursor"`
	RemoteRevision    *string `json:"remote_revision"`
	StateConfidence   string  `json:"state_confidence"`
	LastMessageAt     *int64  `json:"last_message_at"`
	LastSyncedAt      *int64  `json:"last_synced_at"`
	Version           int64   `json:"version"`
	CreatedAt         int64   `json:"created_at"`
	UpdatedAt         int64   `json:"updated_at"`
}

func sessionViewOf(s session.Session) sessionView {
	return sessionView{
		ID: s.ID, AdapterInstanceID: s.AdapterInstanceID,
		ProjectID: nullableString(s.ProjectID), RemoteSessionID: nullableString(s.RemoteSessionID),
		Title: nullableString(s.Title), Managed: s.Managed,
		SyncState: string(s.SyncState), SyncCursor: s.SyncCursor,
		RemoteRevision: nullableString(s.RemoteRevision), StateConfidence: string(s.StateConfidence),
		LastMessageAt: nullableMillis(s.LastMessageAt), LastSyncedAt: nullableMillis(s.LastSyncedAt),
		Version: s.Version, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

// registerSessionListRoute 单开一条：它只依赖读通道，不要求 Sessions（Service）也在位。
// 与 :sync 那条同规矩 —— 没注入就维持 catch-all 的 501，不给前端半套面。
func registerSessionListRoute(mux *http.ServeMux, c Config) {
	if c.SessionRead == nil {
		return
	}
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			// 这条路径今天只接受 GET：:manage 之类是另外的路径，POST 到这里是方法不被接受 ⇒ 405（C28②）。
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"/api/v1/sessions 只有 GET；:sync / :manage 是带 id 的另外的路径", false)
			return
		}
		listSessions(w, r, c)
	})
}

// listSessions 只做解析/校验/投影：过滤与翻页住在 session.Repository.ListPage（契约③ §7）。
// 游标复用 tasks 那对二元编解码（同为**分页**口径：`next_cursor: ""` 是唯一合法的"没有下一页"）。
func listSessions(w http.ResponseWriter, r *http.Request, c Config) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 200 {
			writeError(w, http.StatusBadRequest, "validation_failed", "limit 必须是 1~200", false)
			return
		}
		limit = n
	}
	f := session.ListFilter{ProjectID: q.Get("project"), Limit: limit}
	if cur := q.Get("cursor"); cur != "" {
		at, id, err := decodeCursor(cur)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "cursor 不合法", false)
			return
		}
		f.AfterCreatedAt, f.AfterID = at, id
	}
	items, err := c.SessionRead.ListPage(r.Context(), f)
	if err != nil {
		serverError(w, r, err)
		return
	}
	views := make([]sessionView, 0, len(items))
	for _, s := range items {
		views = append(views, sessionViewOf(s))
	}
	next := ""
	if len(items) == limit && len(items) > 0 {
		last := items[len(items)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": next})
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

// getSession 是 `GET /api/v1/sessions/{id}` 那一条腿：读那一行、原样投影成 yaml 的 `Session`。
// 读不到就说读不到（404 `session_not_found`，§15.2 已绑 404），不拿 501 或空对象糊过去；
// 归档的会话照样能按 id 读到 —— 契约在这条路上没给任何过滤位，我不替它加。
func getSession(w http.ResponseWriter, r *http.Request, c Config, id string) {
	s, err := c.SessionRead.Get(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, sessionViewOf(s))
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "session_not_found", "没有这条 Session", false)
	default:
		serverError(w, r, err)
	}
}
