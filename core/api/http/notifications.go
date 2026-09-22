package httpapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// NotificationLister 是 GET /api/v1/notifications 需要的读面，由 internal/notification 的
// *Repository 直接满足（依赖方向合法：transport → 业务包）。这里只做解析/校验/状态码。
type NotificationLister interface {
	ListPage(ctx context.Context, f notification.ListFilter) ([]notification.Notification, notification.ListPosition, error)
}

// notificationView 的必填六字段来自 core-v1.yaml 的 Notification.required；
// 三个可空时间戳一律显式出键（0 在库里就是 NULL，见下面 nullable 那一句）。
type notificationView struct {
	ID         string `json:"id"`
	Type       string `json:"notification_type"`
	Priority   string `json:"priority"`
	State      string `json:"state"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	ProjectID  string `json:"project_id"`
	TaskID     string `json:"task_id"`
	ApprovalID string `json:"approval_id"`
	CreatedAt  int64  `json:"created_at"`
	ReadAt     *int64 `json:"read_at"`
	HandledAt  *int64 `json:"handled_at"`
	ExpiresAt  *int64 `json:"expires_at"`
}

// nullable 把 domain 里"0 = 库里没这一列"的约定还原成 schema 要的 null。
// 三个时间戳都是毫秒时钟，真实值不可能落在 0，所以这一折不产生歧义；
// 想把 NULL 与 0 彻底分开，得改 internal/notification 的字段类型（见 progress Remaining）。
func nullable(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func notificationViewOf(n notification.Notification) notificationView {
	return notificationView{
		ID: n.ID, Type: string(n.Type), Priority: string(n.Priority), State: string(n.State),
		Title: n.Title, Body: n.Body, ProjectID: n.ProjectID, TaskID: n.TaskID, ApprovalID: n.ApprovalID,
		CreatedAt: n.CreatedAt, ReadAt: nullable(n.ReadAt), HandledAt: nullable(n.HandledAt),
		ExpiresAt: nullable(n.ExpiresAt),
	}
}

// registerNotificationRoutes 与 tasks 那条同规矩：依赖没注入就维持 §3 的 501，不给前端半套面。
func registerNotificationRoutes(mux *http.ServeMux, c Config) {
	if c.Notifications == nil {
		return
	}
	mux.HandleFunc("/api/v1/notifications", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"notifications 这一腿只有 GET；:read / :handle 是另外的口", false)
			return
		}
		listNotifications(w, r, c)
	})
}

func listNotifications(w http.ResponseWriter, r *http.Request, c Config) {
	q := r.URL.Query()
	// C26 裁定 ②：**不传 state = 不过滤**。原来那两行"缺省取 UNREAD"已删 —— 一个字段不许兼职两种语义，
	// 否则面板就再也表达不出"我要全部"；而面板的 inbox 调用本来就显式带 state（主控核过），缺省零收益。
	var state *notification.State
	if v := strings.ToUpper(q.Get("state")); v != "" {
		s := notification.State(v)
		if !s.Valid() {
			writeError(w, http.StatusBadRequest, "validation_failed",
				"state 不在 notifications 的六态里: "+v, false)
			return
		}
		state = &s
	}
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 200 {
			writeError(w, http.StatusBadRequest, "validation_failed", "limit 必须是 1~200", false)
			return
		}
		limit = n
	}
	f := notification.ListFilter{State: state, Limit: limit}
	if cur := q.Get("cursor"); cur != "" {
		pos, err := decodeInboxCursor(cur)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "cursor 不合法", false)
			return
		}
		f.After = &pos
	}
	items, last, err := c.Notifications.ListPage(r.Context(), f)
	if err != nil {
		serverError(w, r, err)
		return
	}
	views := make([]notificationView, 0, len(items))
	for _, n := range items {
		views = append(views, notificationViewOf(n))
	}
	// REQ-F-19：分页出口的"没有下一页"只有空串这一种写法（键必在、不许 null、不许省略）。
	next := ""
	if len(items) == limit && len(items) > 0 {
		next = encodeInboxCursor(last)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": next})
}

// NotificationReader 是 `:read` 这条口的写面。它与 NotificationLister 分成两个字段而不是合成一个接口：
// 一个挂在 *Repository、一个挂在 *Service，合并就会逼 cmd/core 造一个只为凑签名的转发类型（同 sessions 那两个）。
type NotificationReader interface {
	MarkRead(ctx context.Context, tx notification.Toucher, id string,
		cmd notification.ReadCommand) (notification.Notification, bool, error)
}

// registerNotificationItemRoute 认领子树 `/api/v1/notifications/`（`:read` 这条口）。
// 认领整棵子树就要负责邻居：`{id}:handle` 与"别的形状"必须**保持它们今天的 501**，
// 不许因为多了这段代码而变成 404/405 —— 那是静默的行为变更（第四十一批的 adapters 同一条规矩）。
func registerNotificationItemRoute(mux *http.ServeMux, c Config) {
	mux.HandleFunc("/api/v1/notifications/", func(w http.ResponseWriter, r *http.Request) {
		id, verb, ok := splitNotificationVerb(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"notifications 的单条路径只认 {id}:read 与 {id}:handle；别的形状 Core 不猜", true)
			return
		}
		switch verb {
		case "read":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/notifications/{id}:read 只有 POST", false)
				return
			}
			if c.NotifRead == nil || c.Store == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"POST /api/v1/notifications/{id}:read 还没装配（写通道或 Service 没注入）", true)
				return
			}
			readNotification(w, r, c, id)
		case "handle":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/notifications/{id}:handle 只有 POST", false)
				return
			}
			// declared、方法合法、能力没接线 ⇒ 501（C28②）。今天不许为它编一个假 200，
			// 也不许把它回成 405 让客户去换方法。
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"POST /api/v1/notifications/{id}:handle 还没接线（`notification_handled` 已登记，缺的是状态边由谁裁）", true)
		default:
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"notifications 的单条路径只认 {id}:read 与 {id}:handle", true)
		}
	})
}

// splitNotificationVerb 只认 `{id}:read` / `{id}:handle` 两种形状；空 id、多一段、第二个冒号都算不认识。
func splitNotificationVerb(path string) (id, verb string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/notifications/")
	if rest == "" || strings.Contains(rest, "/") {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, ':')
	if i < 0 {
		// 没有动词的那条（GET /notifications/{id}）契约里根本没 declared ⇒ 不认。
		return "", "", false
	}
	id, verb = rest[:i], rest[i+1:]
	if id == "" || verb == "" || strings.Contains(id, ":") {
		return "", "", false
	}
	if verb != "read" && verb != "handle" {
		return "", "", false
	}
	return id, verb, true
}

func readNotification(w http.ResponseWriter, r *http.Request, c Config, id string) {
	principal := r.Header.Get("X-Lucero-Principal")
	correlation := newUUIDv7()
	var out notification.Notification
	err := c.Store.Write(r.Context(), func(tx *db.Tx) error {
		var err error
		// 第二个返回值"真改了吗"只决定库里有没有第二条事件；两种情况的响应形状必须一致，
		// 所以这里不接它（接了又只在注释里用，就是死代码）。
		out, _, err = c.NotifRead.MarkRead(r.Context(), tx, id, notification.ReadCommand{
			EventID: newUUIDv7(), PrincipalID: principal, CorrelationID: correlation, CausationID: correlation,
		})
		return err
	})
	switch {
	case err == nil:
		// C18 裁定 ②：两种情况都回"库里那一行"，不回显请求。
		writeJSON(w, http.StatusOK, notificationViewOf(out))
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, db.ErrAggregateNotFound):
		writeError(w, http.StatusNotFound, "not_found", "没有这条通知", false)
	case errors.Is(err, notification.ErrIllegalTransition):
		writeError(w, http.StatusConflict, "illegal_transition", err.Error(), false)
	case errors.Is(err, notification.ErrInvalidDraft):
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), false)
	default:
		serverError(w, r, err)
	}
}

// encodeInboxCursor / decodeInboxCursor 是 **分页** 游标（inbox 三元组：档 + created_at + id）。
// 与 tasks 那两个函数分开写，是因为它们装的字段数就不同；而同步游标（REQ-F-21/22）
// 连"空串合法"这一点都相反，将来 `:sync` 必须另写第三个，不许复用这里任何一个。
func encodeInboxCursor(p notification.ListPosition) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(strconv.Itoa(p.Rank) + "|" + strconv.FormatInt(p.CreatedAt, 10) + "|" + p.ID))
}

func decodeInboxCursor(s string) (notification.ListPosition, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return notification.ListPosition{}, err
	}
	rank, rest, ok := strings.Cut(string(b), "|")
	if !ok {
		return notification.ListPosition{}, errors.New("inbox cursor: 缺档位")
	}
	at, id, ok := strings.Cut(rest, "|")
	if !ok || id == "" {
		return notification.ListPosition{}, errors.New("inbox cursor: 缺 id")
	}
	r, err := strconv.Atoi(rank)
	if err != nil {
		return notification.ListPosition{}, err
	}
	millis, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return notification.ListPosition{}, err
	}
	return notification.ListPosition{Rank: r, CreatedAt: millis, ID: id}, nil
}
