package httpapi

// approvals 一族在契约③ §3 与 openapi 里都登记过（`specs/api-contract-v0.1.md:168`；
// `core-v1.yaml:567` listApprovals、`:583` getApproval、`:595` resolveApproval）。
// 09-30 21:2x 主控把三格的归属重新对了一遍（T-414 ③）：**列表这条口的 handler 接线归 backend**，
// 因为读法本身早就在 `internal/approval`（T-372 落的只读分页，`Service.ListPage`）——
// 缺的从来不是"有没有读法"，是"transport 有没有把它接进来"。
//
// 红线一条没动：**本文件不碰那张表**。它只调注入进来的一个方法，拿不到任何入口符号，
// 所以这里出现不了任何 SQL、也出现不了那一层的读写口（`TestApprovalsFileNeverTouchesTheTable`
// 钉的就是这一句，它看字面不看意图）。
//
// 三条口现在各是什么形状，逐条说清（"没装配"必须是一句真话）：
//   - `GET /api/v1/approvals` —— **注入了读法就真读**：状态过滤 / keyset 游标 / limit 都在读法那一侧有唯一说法；
//     没注入 ⇒ 501 `not_implemented` + retryable，理由写"本进程没把 approval 的读面接进来"，
//     绝不回"空的一页"（空页是"今天确实没有审批"，与"这一层没接"是两件事，混起来会把归因带跑）。
//   - `GET /api/v1/approvals/{id}` —— 仍然 501，但理由换成了**真话的新版本**：那一侧的读法今天只有分页与
//     按 execution 两种，**没有 by-id 的那一个**（`Service` 面上没有 Get）。要接它得先在 `internal/approval`
//     加一个方法 —— 那是 audit 的面包，我不越过去自己造一个第二份读法。
//   - `POST /api/v1/approvals/{id}:resolve` —— 仍然 501：决议是**写**那张表（还带 temporary grant），
//     归属在 approval/audit，且 `approvals` 表对 backend 是绝对禁区。
//
// 动词的形状照旧两分：§3 声明过的动词 ⇒ 501，没声明的动词 ⇒ 405（501 说"没装配"、405 说"方法不对"，
// 与 sessions/notifications 同口径）。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/SadCat18/lucero/core/internal/approval"
)

// ApprovalLister 是这条列表唯一需要的读法（consumer-side 接口，与 NotificationLister 同族）。
// 它刻意只有一口：by-id 与 resolve 今天都不在 transport 这一侧能办到的事里（见文件头），
// 多塞一个方法进去就会逼 cmd/core 造一个只为凑签名的转发类型。
type ApprovalLister interface {
	ListPage(ctx context.Context, f approval.ListFilter) ([]approval.Approval, approval.ListPosition, error)
}

const notWiredOwner = "approval 的读法在 internal/approval（T-372 的只读分页），本进程没把它接进来"

func registerApprovalRoutes(mux *http.ServeMux, c Config) {
	mux.HandleFunc("/api/v1/approvals", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"/api/v1/approvals 只声明 GET（列表带 state/cursor）；处理审批是 /api/v1/approvals/{id}:resolve", false)
			return
		}
		listApprovals(w, r, c)
	})

	mux.HandleFunc("/api/v1/approvals/", func(w http.ResponseWriter, r *http.Request) {
		id, verb, ok := splitApprovalItem(r.URL.Path)
		switch {
		case !ok:
			// 真正认得的只有 `{id}` 与 `{id}:resolve` 两形（照 yaml，不自扩）。这一支是"从没声明过的形状"，
			// 与兜底那条同口径，所以这里的"没登记"是真话。
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"这条路径 Core 没登记（approvals 名下只声明过 /{id} 与 /{id}:resolve）；已登记的端点看契约③ §3", true)
		case verb == "resolve":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/approvals/{id}:resolve 只声明 POST", false)
				return
			}
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"POST /api/v1/approvals/"+id+":resolve 在契约③ §3 登记过（resolveApproval），但那是写那张表的一腿，"+notWiredOwner, true)
		case verb == "":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/approvals/{id} 只声明 GET", false)
				return
			}
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"GET /api/v1/approvals/{id} 在契约③ §3 登记过（getApproval，F-8 批准 · LAG-1 回写那条），但那一侧今天没有 by-id 的读法"+
					"（T-372 落的是只读分页与按 execution 两种）⇒ 加方法是 approval 的活，我不在这一层自己造第二份读法", true)
		default:
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"没听过的 approval 子动词一律 501，不猜它该走哪条腿", true)
		}
	})
}

// splitApprovalItem 认两种形状：`/api/v1/approvals/{id}` 与 `/api/v1/approvals/{id}:resolve`。
// 形状与 `splitSessionVerb` 的冒号分支相同，但不共用函数：那条硬写着 sessions 前缀，
// 为两处调用去改它的签名会把别的分支一起拖进来（同一格第 289 条那种"别替别人决定归属"）。
func splitApprovalItem(path string) (id, verb string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/approvals/")
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

// approvalView 是 yaml `Approval`（`core-v1.yaml:1206~1226`）那 18 个键的出口，一个不多一个不少：
// 可空列走指针（域里 ""/0 = 库里 NULL ⇒ 出口必须是 null 而不是 ""），两份 *_json 列走 `jsonMap`
// （空 ⇒ `{}`、坏了 ⇒ 500，与 executions 那三格同一条规矩）。
//
// `grant_id` 恒为 null —— **这不是偷懒**：grant 住在另一张表（temporary_grants），
// approval 的只读分页没把它交出来。编一个值出来比 null 坏得多，所以这里出 null 并在判据里钉住"它不许被猜"。
type approvalView struct {
	ID                string          `json:"id"`
	TaskID            *string         `json:"task_id"`
	ExecutionID       *string         `json:"execution_id"`
	SessionID         *string         `json:"session_id"`
	AdapterInstanceID *string         `json:"adapter_instance_id"`
	ApprovalType      string          `json:"approval_type"`
	Action            string          `json:"action"`
	Resource          *string         `json:"resource"`
	RiskLevel         string          `json:"risk_level"`
	State             string          `json:"state"`
	Request           json.RawMessage `json:"request"`
	Resolution        json.RawMessage `json:"resolution"`
	GrantID           *string         `json:"grant_id"`
	RequestedAt       int64           `json:"requested_at"`
	ExpiresAt         *int64          `json:"expires_at"`
	ResolvedAt        *int64          `json:"resolved_at"`
	ResolvedBy        *string         `json:"resolved_by"`
	Version           int64           `json:"version"`
}

func approvalViewOf(a approval.Approval) (approvalView, error) {
	req, err := jsonMap(a.RequestJSON)
	if err != nil {
		return approvalView{}, err
	}
	res, err := jsonMap(a.ResolutionJSON)
	if err != nil {
		return approvalView{}, err
	}
	return approvalView{
		ID: a.ID, TaskID: optStr(a.TaskID), ExecutionID: optStr(a.ExecutionID),
		SessionID: optStr(a.SessionID), AdapterInstanceID: optStr(a.AdapterInstanceID),
		ApprovalType: a.Type, Action: a.Action, Resource: optStr(a.Resource),
		RiskLevel: a.RiskLevel, State: a.State,
		Request: req, Resolution: res, GrantID: nil,
		RequestedAt: a.RequestedAt, ExpiresAt: nullable(a.ExpiresAt),
		ResolvedAt: nullable(a.ResolvedAt), ResolvedBy: optStr(a.ResolvedBy),
		Version: a.Version,
	}, nil
}

// listApprovals 是 `GET /api/v1/approvals`（yaml listApprovals：state + Cursor + Limit）。
// 三件参数一件都不在这里发明：状态词表、keyset 顺序、"PENDING 还要看 deadline"那一条全在
// `internal/approval`（REQ-A8 的归属），transport 只做"要不要过滤"与"翻页"这两件 transport 的事。
func listApprovals(w http.ResponseWriter, r *http.Request, c Config) {
	if c.Approvals == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"GET /api/v1/approvals 在契约③ §3 登记过（listApprovals），但 Core 侧没装配："+notWiredOwner, true)
		return
	}
	q := r.URL.Query()
	// 缺省 50、上限 200 与 inbox 同一条 transport 口径（那一侧的 `defaultListLimit` 没导出，
	// 也不该导出：一页出多少行是出口的事，过滤与顺序才是领域的事）。
	f := approval.ListFilter{Limit: 50}
	if v := strings.ToUpper(q.Get("state")); v != "" {
		state := v
		f.State = &state
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
		pos, err := decodeApprovalCursor(cur)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "cursor 不合法", false)
			return
		}
		f.After = &pos
	}
	items, last, err := c.Approvals.ListPage(r.Context(), f)
	if err != nil {
		// 状态词表那一格的错误**不在这里重做一份**（那是 approval 的唯一说法）：认它那句原话翻成 400，
		// 其余一律 500。文案耦合是暂时的、且有判据钉住（`TestListApprovalsRejectsUnknownState`），
		// 它坏了会响，不会静默地把打错的过滤答成"今天没有审批"。干净的解法是那一侧导出一个哨兵或 ValidState，
		// 已作为一格报给主控（改 approval 的面对我不开放）。
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			serverError(w, r, err)
			return
		}
		if strings.Contains(err.Error(), "is not an approvals.state value") {
			writeError(w, http.StatusBadRequest, "validation_failed",
				"state 不在 approvals 的状态词表里（词表唯一的说法在 approval 那一侧）: "+
					q.Get("state"), false)
			return
		}
		serverError(w, r, err)
		return
	}
	views := make([]approvalView, 0, len(items))
	for _, a := range items {
		v, err := approvalViewOf(a)
		if err != nil {
			serverError(w, r, err)
			return
		}
		views = append(views, v)
	}
	// REQ-F-19：分页出口的"没有下一页"只有空串这一种写法（键必在、不许 null、不许省略）。
	next := ""
	if len(items) == f.Limit && len(items) > 0 {
		next = encodeApprovalCursor(last)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": next})
}

// encodeApprovalCursor / decodeApprovalCursor 是 approvals 的**分页**游标：keyset 两元组
// （requested_at, id），与 inbox 的三元组、同步游标各自独立（REQ-F-21/22 那条"不许复用这里任何一个"）。
func encodeApprovalCursor(p approval.ListPosition) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(strconv.FormatInt(p.RequestedAt, 10) + "|" + p.ID))
}

func decodeApprovalCursor(s string) (approval.ListPosition, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return approval.ListPosition{}, err
	}
	at, id, ok := strings.Cut(string(b), "|")
	if !ok || id == "" {
		return approval.ListPosition{}, errors.New("approvals cursor: 缺 id")
	}
	millis, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return approval.ListPosition{}, err
	}
	return approval.ListPosition{RequestedAt: millis, ID: id}, nil
}
