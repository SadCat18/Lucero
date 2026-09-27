package httpapi

// approvals 一族在契约③ §3 与 openapi 里都登记过（`specs/api-contract-v0.1.md:168`；
// `core-v1.yaml:567` listApprovals、`:583` getApproval、`:595` resolveApproval），但 Core 这边
// **今天只有路由、没有业务面**：读它的那一半不在 backend 的 SCOPE 里（`approvals` 表是我这边的红线，
// 绝对禁止读写），主控 T-370② 把 200 面立到另一家（T-372）。
//
// 这一格修的是一句**假话**：这些路径以前落到 `handler.go` 的兜底 501，回的是"这条路径 Core 没登记"，
// 而 §3 明明登记了 —— 主控原话「误导文案今天已经让我误判过一次归因」。所以这里只登记两件事：
//   - §3 声明过的动词（列表/详情 GET、`{id}:resolve` POST）⇒ 501 `not_implemented` + retryable=true，
//     理由写成"路由认得、业务面没装配，且它不归本包"；
//   - 没声明的动词 ⇒ 405。501 说的是"没装配"，405 说的是"方法不对"，两件事不混（与 sessions/notifications 同口径）。
//
// 这个文件里不许出现任何 SQL、也不许出现"读一个空页返回 200"那种冒充：没装配就回没装配。

import (
	"net/http"
	"strings"
)

const notWiredOwner = "读 approvals 业务面归 approval/audit 那一侧（T-372），不在 backend 的 SCOPE 内"

func registerApprovalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/approvals", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"/api/v1/approvals 只声明 GET（列表带 state/cursor）；处理审批是 /api/v1/approvals/{id}:resolve", false)
			return
		}
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"GET /api/v1/approvals 在契约③ §3 登记过（listApprovals），但 Core 侧没装配读面："+notWiredOwner, true)
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
				"POST /api/v1/approvals/"+id+":resolve 在契约③ §3 登记过（resolveApproval），但 Core 侧没装配写面："+notWiredOwner, true)
		case verb == "":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/approvals/{id} 只声明 GET", false)
				return
			}
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"GET /api/v1/approvals/{id} 在契约③ §3 登记过（getApproval，F-8 批准 · LAG-1 回写那条），但 Core 侧没装配读面："+notWiredOwner, true)
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
