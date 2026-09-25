package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/SadCat18/lucero/core/internal/platform/db"
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
	if c.Sessions == nil && c.SessionRead == nil && c.SessionManage == nil && c.SessionMessages == nil {
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
					"/api/v1/sessions/{id} 只有 GET；:sync 与 :manage 是带冒号的那两条", false)
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
		case verb == "manage":
			// T-195②：这一格以前掉进下面的 `default:`（一句 501，理由写着"不猜它该走哪条腿"），
			// 修法是**把它接进分流**而不是另挂一条 mux pattern —— 两条 pattern 抢同一条前缀迟早分叉。
			if c.SessionManage == nil || c.Store == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					":manage 还没装配业务面（写侧要 Service + 事务通道）", true)
				return
			}
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", ":manage 只能 POST", false)
				return
			}
			manageSession(w, r, c, id)
		case verb == "messages":
			// 巡检 07:20② 点的就是这一格：`GET /api/v1/sessions/{id}/messages` 是契约③ §3 声明过的路径
			// （`listSessionMessages`），以前它连 `splitSessionVerb` 都过不去（见那函数的注释），
			// 于是拿到的是"这条路径的形状我不认识"——那是一句假话。今天的真相是**路由认得、读通道没接**，
			// 按 C28② 就该回 501 且把理由说成"还没装配"；方法不对才是 405。
			// 第六十二批：生产者 RecordMessage 已落地（api-contract :113~119 的接线条件满足），
			// SessionMessages 注入后走真读路径；nil 仍 501（"还没装配"，文案不含"形状"）。
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/sessions/{id}/messages 只声明 GET", false)
				return
			}
			if c.SessionMessages == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"GET /api/v1/sessions/{id}/messages 还没装配读通道（增量游标 after_seq/cursor 都在契约里，实现没接）", true)
				return
			}
			listSessionMessages(w, r, c, id)
		default:
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"没听过的 custom verb 一律 501，不猜它该走哪条腿", true)
		}
	})
}

// splitSessionVerb 认三种形状：`/api/v1/sessions/{id}:sync`、`/api/v1/sessions/{id}`（verb 空）、
// 与声明过的子路径 `/api/v1/sessions/{id}/messages`。
// 不照抄 splitTaskAction：那条不认"没有 verb"的形状，而这条路径上"没有 verb"是要回 501 的合法输入。
func splitSessionVerb(path string) (id, verb string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/sessions/")
	if rest == "" {
		return "", "", false
	}
	// 子路径今天只有一条：`{id}/messages`（契约③ §3 声明的 `listSessionMessages`）。
	// 不认它 = 对一条声明过的路径说"形状我不认识"，那是假话（巡检 07:20②；同族旧错见 T-195④ 的 tasks.go）。
	// 只认这一条，别的多段形状仍然不认 —— 认多了就是替别人决定"这条路径归谁"。
	if sub, found := strings.CutSuffix(rest, "/messages"); found {
		if sub == "" || strings.Contains(sub, "/") || strings.Contains(sub, ":") {
			return "", "", false
		}
		return sub, "messages", true
	}
	if strings.Contains(rest, "/") {
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

// SessionMessageReader 是 GET /sessions/{id}/messages 的读面：
// Get 先过 404 闸（session 不存在与"没消息"是两件事），ListMessagesPage 出一页。
// 由 *session.Repository 直接满足。
type SessionMessageReader interface {
	Get(ctx context.Context, id string) (session.Session, error)
	ListMessagesPage(ctx context.Context, f session.MessageFilter) ([]session.Message, error)
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

// messageView 是 yaml Message（:1055）：required [id, session_id, role, kind, created_at]。
// **`content` / `provider_raw` 不出键**：域模型不读那两列，"handler 不许补字段"（§7.2）。
// sequence_no=0 / remote_message_id="" / text_content="" / synced_at=0 折成 null
// （REQ-F-21 ④ 同族：NULL 与空串是两件事，出口只发 null）。
type messageView struct {
	ID              string  `json:"id"`
	SessionID       string  `json:"session_id"`
	RemoteMessageID *string `json:"remote_message_id"`
	Role            string  `json:"role"`
	Kind            string  `json:"kind"`
	SequenceNo      *int64  `json:"sequence_no"`
	TextContent     *string `json:"text_content"`
	CreatedAt       int64   `json:"created_at"`
	SyncedAt        *int64  `json:"synced_at"`
}

func messageViewOf(m session.Message) messageView {
	return messageView{
		ID: m.ID, SessionID: m.SessionID,
		RemoteMessageID: nullableString(m.RemoteMessageID),
		Role:            string(m.Role), Kind: string(m.Kind),
		// sequence_no 从 1 起（本地未编号是 NULL → 域里 0）：0 不冒充"知道是第 0 条"。
		SequenceNo:  nullableSeq(m.SequenceNo),
		TextContent: nullableString(m.TextContent),
		CreatedAt:   m.CreatedAt, SyncedAt: nullableMillis(m.SyncedAt),
	}
}

// nullableSeq 把域里的 0 折成 null：sequence_no 可 NULL 且编号从 1 起，
// 0 只可能是"库里是 NULL"的折算，不是合法序号（§12：不拿 0 冒充不知道）。
func nullableSeq(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// listSessionMessages 是 `GET /api/v1/sessions/{id}/messages`：405 → 501 已在分流里判过，
// 这里只做 404 闸 → 参数 → 投影。limit 1~200 默认 50；游标复用 encodeCursor/decodeCursor。
func listSessionMessages(w http.ResponseWriter, r *http.Request, c Config, id string) {
	if _, err := c.SessionMessages.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session_not_found", "没有这条 Session", false)
			return
		}
		serverError(w, r, err)
		return
	}
	q := r.URL.Query()
	f := session.MessageFilter{SessionID: id, Limit: 50}
	if v := q.Get("after_seq"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "validation_failed", "after_seq 必须是非负整数", false)
			return
		}
		f.AfterSeq = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 200 {
			writeError(w, http.StatusBadRequest, "validation_failed", "limit 必须是 1~200", false)
			return
		}
		f.Limit = n
	}
	if cur := q.Get("cursor"); cur != "" {
		at, msgID, err := decodeCursor(cur)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "cursor 不合法", false)
			return
		}
		f.AfterCreatedAt, f.AfterID = at, msgID
	}
	items, err := c.SessionMessages.ListMessagesPage(r.Context(), f)
	if err != nil {
		serverError(w, r, err)
		return
	}
	views := make([]messageView, 0, len(items))
	for _, m := range items {
		views = append(views, messageViewOf(m))
	}
	next := ""
	if len(items) == f.Limit && len(items) > 0 {
		last := items[len(items)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	// 空页 items 必须是 [] 不是 null（与 inbox/discover 同口径）。
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": next})
}

// SessionManager 是 `POST /api/v1/sessions/{id}:manage` 的业务面。它与 SessionSyncer 分成两个字段：
// 一个要事务通道（写）、一个只要读 —— 合成一个接口就会逼 cmd/core 造一个只为凑签名的转发类型。
type SessionManager interface {
	Manage(ctx context.Context, tx session.Writer, id string,
		cmd session.ManageCommand) (session.Session, bool, error)
}

// manageBody 是 yaml `ManageSessionRequest`：`managed` required、`project_id` 可空。
// `ProjectID` 收 `json.RawMessage` 而不是 `*string`，因为**"没给这一格"与"给了 null"是三态里的两态**：
// `*string` 会把两者都解码成 nil，于是"解绑"在 HTTP 面上根本无法表达（而 C41 裁定明写 `null` = 这次没绑/解绑）。
type manageBody struct {
	Managed   *bool           `json:"managed"`
	ProjectID json.RawMessage `json:"project_id"`
}

// projectIDFromRaw 把上面那三态翻成 ManageCommand 的指针语义。
func projectIDFromRaw(raw json.RawMessage) (*string, error) {
	trimmed := strings.TrimSpace(string(raw))
	switch {
	case trimmed == "":
		return nil, nil // 请求里没这个键：不动归属
	case trimmed == "null":
		empty := ""
		return &empty, nil // 显式 null：解绑（领域侧空串 = 没有归属）
	default:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return &s, nil
	}
}

func manageSession(w http.ResponseWriter, r *http.Request, c Config, id string) {
	var body manageBody
	if err := decodeStrict(w, r, &body); err != nil {
		return // decodeStrict 已经把 400 写出去了
	}
	if body.Managed == nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "managed 是必填的布尔位", false)
		return
	}
	projectID, err := projectIDFromRaw(body.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "project_id 必须是字符串或 null: "+err.Error(), false)
		return
	}
	cmd := session.ManageCommand{
		Managed: *body.Managed, ProjectID: projectID,
		EventID: newUUIDv7(), PrincipalID: r.Header.Get("X-Lucero-Principal"),
	}
	cmd.CorrelationID, cmd.CausationID = cmd.EventID, cmd.EventID

	var out session.Session
	err = c.Store.Write(r.Context(), func(tx *db.Tx) error {
		var err error
		// 第二个返回值"真改了吗"只决定库里有没有那条事件；两种情况的响应形状必须一致，所以这里不接它。
		out, _, err = c.SessionManage.Manage(r.Context(), tx, id, cmd)
		return err
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, sessionViewOf(out))
	case errors.Is(err, session.ErrSessionNotFound), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "session_not_found", "没有这条 Session", false)
	case errors.Is(err, session.ErrProjectNotFound), errors.Is(err, session.ErrInvalidCommand):
		// 请求指向不存在的项目 / 命令本身不合法：都是**写错了的请求**，不是 Core 病了
		// （域侧已预检存在性，就是为了让 FK 报错不要冒到 transport 变成 500）。
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), false)
	default:
		serverError(w, r, err)
	}
}

// decodeStrict 是"读请求体 + 不认的键一律 400"这一套的唯一落点（C35 那条口径）。
// 抽出来是因为这段一旦被抄第二遍，两遍迟早分叉；静默丢弃未知键更糟——那会让调用方以为改动生效了。
func decodeStrict(w http.ResponseWriter, r *http.Request, into any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "请求体不合法: "+err.Error(), false)
		return err
	}
	return nil
}
