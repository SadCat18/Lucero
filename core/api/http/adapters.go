package httpapi

// 这条口是 §7.2（REQ-AD-1）裁给 backend 的那一段：`api/http` 只做薄投影
// （取 adapter_key → 调 `Registry.Probe` → 出 yaml 的 `AdapterInstance`），
// **写一行都不在这里** —— 健康行、能力行与那三个已登记的事件全在 `internal/adapter` 自己身上。
//
// "handler 里不许补字段"是这条口的硬约束，所以响应体是域包给的 `AdapterInstanceView`，
// 不是这里现编的 struct：一个required 字段如果 probe 拿不出来，该改的是 probe，不是让 transport 猜。
//
// C28② 在这里的应用：路由存在、方法合法、能力没接线 ⇒ 501；方法不被接受 ⇒ 405；
// 形状不认识 ⇒ 501 且**不碰域**（拿畸形路径去问 Registry 会回一个假装合法的 400）。

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// AdapterProber 是这条口唯一的依赖形状：拉起来一次、握手一次，把已经落库的那一行投影出来。
// 它不带事务通道 —— 这不是省事，是 §7.1 的归属：写发生在域包里。
type AdapterProber interface {
	Probe(ctx context.Context, adapterKey string) (*adapter.AdapterInstanceView, error)
}

// registerAdapterProbeRoute 认领子树 `/api/v1/adapters/`。
// 认领整棵子树是有代价的：`/api/v1/adapters/{id}` 与 `.../capabilities` 本来由 catch-all 回 501，
// 现在由这里回 —— 状态与码都没变（501 `not_implemented`），但**必须由自己的用例钉住**，
// 不然"接一条口顺手把邻居接成 404/405"就是静默的行为变更。
func registerAdapterProbeRoute(mux *http.ServeMux, c Config) {
	mux.HandleFunc("/api/v1/adapters/", func(w http.ResponseWriter, r *http.Request) {
		key, tail, ok := splitAdapterPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"adapters 这条路径只认 {key}/probe、{id} 与 {id}/capabilities；别的形状 Core 不猜", true)
			return
		}
		switch tail {
		case "probe":
			// custom verb 那类是 POST-only：GET 不是"能力没接线"，是"这条路径不接受它"。
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/adapters/{key}/probe 只有 POST（它会真去连一次）", false)
				return
			}
			if c.Adapters == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"probe 还没装配 Registry（cmd/core 没给出 Adapter 的起法）", true)
				return
			}
			if r.Header.Get("Idempotency-Key") == "" {
				// 与 POST /projects 同一格：码、状态、话术都不许分叉（同一件事两种写法）。
				writeError(w, http.StatusForbidden, "idempotency_key_required",
					"POST /api/v1/adapters/{key}/probe 是有副作用的写，必须带 Idempotency-Key", false)
				return
			}
			probeAdapter(w, r, c, key)
		case "":
			// GET /api/v1/adapters/{id} 与 GET /api/v1/adapters：declared 未接。
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"adapters 的单条路径只接受 GET", false)
				return
			}
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"GET /api/v1/adapters/{id} 还没装配读通道", true)
		default:
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/adapters/{id}/"+tail+" 只接受 GET", false)
				return
			}
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"/api/v1/adapters/{id}/"+tail+" 还没装配读通道", true)
		}
	})
}

// splitAdapterPath 把 `/api/v1/adapters/{key}/probe` 拆成 (key, "probe")。
// 第二段只认 `probe` 与 `capabilities` 两个已知形状；空 key、多一段、带冒号的一律不认（ok=false）。
func splitAdapterPath(path string) (key, tail string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/adapters/")
	if rest == "" || strings.Contains(rest, ":") {
		return "", "", false
	}
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return rest, "", rest != ""
	}
	key, tail = rest[:i], rest[i+1:]
	if key == "" || tail == "" || strings.Contains(tail, "/") {
		return "", "", false
	}
	if tail != "probe" && tail != "capabilities" {
		return "", "", false
	}
	return key, tail, true
}

func probeAdapter(w http.ResponseWriter, r *http.Request, c Config, key string) {
	view, err := c.Adapters.Probe(r.Context(), key)
	if err != nil {
		if status, code, retryable, mapped := probeErrorOf(err); mapped {
			writeError(w, status, code, err.Error(), retryable)
			return
		}
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// probeErrorOf 把 §15.1 的码映成 §15.2 的 transport 码与状态。**只列 probe 真能产生的那些**：
// 抄整张表就会抄出一份和契约各说一套的副本（同"validator 不许内嵌契约快照"那一族）。
// 认不出的码先经 `Degrade()` 落成 INTERNAL —— 拿别人没给过的数字现编一个名字，就是归因丢了。
func probeErrorOf(err error) (status int, code string, retryable bool, mapped bool) {
	var e *adapter.Error
	if !errors.As(err, &e) || e == nil {
		return 0, "", false, false
	}
	switch strings.ToLower(e.Degrade().Symbol()) {
	case "invalid_params": // 未知 adapter_key：这是请求错，不是连接不健康（§7.2 明写这一分岔）
		return http.StatusBadRequest, "validation_failed", false, true
	case "adapter_unresponsive":
		return http.StatusServiceUnavailable, "adapter_unresponsive", true, true
	case "protocol_version_mismatch":
		return http.StatusUpgradeRequired, "protocol_version_mismatch", false, true
	case "protocol_not_initialized":
		return http.StatusServiceUnavailable, "protocol_not_initialized", true, true
	case "capability_unavailable":
		return http.StatusServiceUnavailable, "capability_unavailable", true, true
	case "auth_required": // 401 但**不代登录**：能重试的是人，不是 Core
		return http.StatusUnauthorized, "auth_required", false, true
	case "rate_limited":
		return http.StatusTooManyRequests, "rate_limited", true, true
	case "timeout":
		return http.StatusGatewayTimeout, "timeout", true, true
	case "internal":
		return http.StatusInternalServerError, "internal", false, true
	default:
		return 0, "", false, false
	}
}
