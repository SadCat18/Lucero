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
//   - `GET /api/v1/approvals/{id}` 与 `POST /api/v1/approvals/{id}:resolve` —— 这两条从第九十三批起
//     接的是**同一对能力**（`ApprovalDecider`：`Service.GetByID` 回读 + `Service.Resolve` 裁决）。
//     为什么是一对而不是两个开关：yaml `resolveApproval:609~611` 的 200 体就是 `Approval` 那条 schema ⇒
//     一次成功的决议**必须**能把那份审批回读出来；只有写没有读的那半接不上，就不会假装接了一半。
//     audit 那两格都在 10-01 交出（by-id 是 T-427，`Resolve` 一直在），所以缺的确实只剩 transport ⇒
//     没注入这一对时仍回 501（理由改写成"这一对没接进来"，不再是"那一侧没有读法"——那是过期真话）。
//
// 决议这一腿的红线：**本层一行都不判"该不该批"**（yaml `:603` 明写"面板只发决议，裁决权在 Core 的
// Permission/Approval Engine"），也不含任何 SQL/读写口符号 —— 谁能批、重复决议给什么码、`resolved_by`
// 从哪来，三件全在 `internal/approval` 那侧（T-426 ①：主控要 audit 用三行交出它实际校验了什么，
// 契约 §3/§4 由主控落；在那之前我这一层不补任何一条策略，只转发与翻译它已经给出的答案）。
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

// ApprovalDecider 是 `{id}` 与 `{id}:resolve` 那两条口共用的一对能力：回读 + 裁决。
// 两个方法都由 `*approval.Service` 一家满足（by-id 是 audit 10-01 T-427 交的 `GetByID`，
// 裁决是它一直有的 `Resolve`），**这里只列 transport 用得上的那两条签名**，
// 一张口都不开给"顺手把表交出去"的那种设计：本文件里出现不了任何读写口符号与 SQL
// （`TestApprovalsFileNeverTouchesTheTable` 按字面钉着它，看字面不看意图 —— 连解释性注释都不能写那一串）。
//
// 为什么 `Resolve` 不在这里收进 `ApprovalLister`：那会把"能列"与"能判"并成一个开关，
// 于是只接读法的进程会假装它也接了写面 —— 三格各留各的 501 真话才是要守的形状。
type ApprovalDecider interface {
	GetByID(ctx context.Context, id string) (approval.Approval, error)
	Resolve(ctx context.Context, id string, res approval.Resolution) error
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
			resolveApproval(w, r, c, id)
		case verb == "":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/approvals/{id} 只声明 GET", false)
				return
			}
			getApproval(w, r, c, id)
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
// `grant_id`（T-440）不再恒 null：它是域层**已经交给我**的那份决议记录里的一个键 ——
// `internal/approval/approval.go:398` 的 `GrantID string json:"grant_id"` 就写进 `resolution_json` 那一列，
// 所以这里只做只读映射，一行 SQL 都不写、不去碰 `temporary_grants`（那是 audit 的表，边界①）。
// 取不到（没开授权、或那份记录里没有这个键）就出 null —— **null 是"没开这扇门"，不是"我猜不到"**；
// 编一个值出来才是坏（这条与上一版同一个理由，只是上一版把结论下错了：它说"读法没交出来"，实际交了）。
// 为什么要露这一格：面板点了"批准并临时允许"之后必须看得见自己刚开了一扇**有时限**的门
// （契约① §3 规则 3 的反向半：不能只改 UI 状态，也得让 UI 看得见它）。
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
		Request: req, Resolution: res, GrantID: resolutionGrantID(a.ResolutionJSON),
		RequestedAt: a.RequestedAt, ExpiresAt: nullable(a.ExpiresAt),
		ResolvedAt: nullable(a.ResolvedAt), ResolvedBy: optStr(a.ResolvedBy),
		Version: a.Version,
	}, nil
}

// getApproval 是 `GET /api/v1/approvals/{id}`（yaml getApproval:583~593，F-8 批准 · LAG-1 回写那条）。
// 读法只有一个入口：`Service.GetByID` —— 它内部 `requireID` 先于取行（audit 那句"你没点名任何审批"
// 不能回成"没有这条审批"，因为那是两种不同的真话）。
// 为什么这里不判 `sql.ErrNoRows`：那一层的 not-found 有自己的哨兵 `approval.ErrNotFound`，
// 而本文件连 `database/sql` 都不许出现（红线按字面钉，见 `TestApprovalsFileNeverTouchesTheTable`）——
// 拿别人的哨兵当参数、而不是自己再拼一份"读不出来就算 404"，才是这一格该有的形状。
func getApproval(w http.ResponseWriter, r *http.Request, c Config, id string) {
	if c.ApprovalDecision == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"GET /api/v1/approvals/"+id+" 在契约③ §3 登记过（getApproval），但"+notWiredDecision+
				"；by-id 那一半是 audit 10-01 T-427 交的 `Service.GetByID`", true)
		return
	}
	got, err := c.ApprovalDecision.GetByID(r.Context(), id)
	switch {
	case err == nil:
		// 视图与列表那一条**共用同一个函数**：同一行从两条口出去必须逐键同形
		// （sessions / tasks / executions 三家同这条规矩，漂移过一次就再也并不回来了）。
		v, verr := approvalViewOf(got)
		if verr != nil {
			serverError(w, r, verr)
			return
		}
		writeJSON(w, http.StatusOK, v)
	case errors.Is(err, approval.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "没有这条 Approval", false)
	default:
		serverError(w, r, err)
	}
}

// resolutionGrantID 只读地把决议记录里的 `grant_id` 取出来（T-440 边界①：来源只有域层交出来的那一列）。
// 空串/没有这个键/那份 JSON 读不动 ⇒ 一律 null，**不猜**：`Service.Request` 那句"never claim a risk level
// we were not told"是同一个规矩的另一种写法，没被告知就是没有。
func resolutionGrantID(resolutionJSON string) *string {
	if strings.TrimSpace(resolutionJSON) == "" {
		return nil
	}
	var rec struct {
		GrantID string `json:"grant_id"`
	}
	if err := json.Unmarshal([]byte(resolutionJSON), &rec); err != nil {
		return nil
	}
	if rec.GrantID == "" {
		return nil
	}
	return &rec.GrantID
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
