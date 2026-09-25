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
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
)

// AdapterProber 是这条口唯一的依赖形状：拉起来一次、握手一次，把已经落库的那一行投影出来。
// 它不带事务通道 —— 这不是省事，是 §7.1 的归属：写发生在域包里。
type AdapterProber interface {
	Probe(ctx context.Context, adapterKey string) (*adapter.AdapterInstanceView, error)
}

// ErrAdapterNotLaunchable 是"这个实例的 key 在本机没配起法"（`-adapter key=路径` 没给过它）。
// 它必须是**能被包外认出的哨兵**：装配方在 `cmd/core` 里判这件事，而"该回 501 还是 503"是 transport 的口径。
// 回 501 的理由：这台机器上从来没有过这条能力，不是那个 Adapter 病了（后者是 `adapter_unresponsive`/503）。
var ErrAdapterNotLaunchable = errors.New("httpapi: 这个 adapter_key 没配起法")

// SessionDiscoverer 是 discover 那条腿的业务面（yaml 的 `discoverAdapterSessions`）：
// 拿实例 id → 起一条连接、拉一次 §4 的 `session/list` → 按 `(adapter_instance_id, remote_session_id)` 去重 upsert
// （T-204①：契约③ 原来那句"按 `remote_session_id` 去重"措辞不严，已按 `idx_sessions_remote_unique` 的两列改准；
// 这条腿本来就按实例作用域查，所以两列口径与实现本来就一致）。
// 这里不出现协议、也不出现事务通道：那是装配方（cmd/core）与 `internal/session` 之间的事。
type SessionDiscoverer interface {
	DiscoverSessions(ctx context.Context, adapterInstanceID string) ([]session.Discovered, int, error)
}

// discoveredSessionView 是 yaml `DiscoveredSession`（required: session_id + remote_session_id）。
type discoveredSessionView struct {
	SessionID       string `json:"session_id"`
	RemoteSessionID string `json:"remote_session_id"`
}

func discoverAdapterSessions(w http.ResponseWriter, r *http.Request, c Config, instanceID string) {
	items, _, err := c.SessionDiscover.DiscoverSessions(r.Context(), instanceID)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows), errors.Is(err, db.ErrAggregateNotFound):
			writeError(w, http.StatusNotFound, "not_found", "没有这个 Adapter 实例（先 probe 一次让它登记）", false)
		case errors.Is(err, ErrAdapterNotLaunchable):
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"这个实例的 adapter_key 在本机没配起法（-adapter key=可执行路径 没给过它）", true)
		case errors.Is(err, session.ErrInvalidCommand):
			// T-204③ 裁定："同页重复 remote_session_id 整批拒"这一格定 **400 `validation_failed`**（码早已登记，不新登记）。
			// 语义要说清楚：这一支里发坏清单的是 **Provider**（§4 那一页给了两行同样的 remote_session_id，或给了空 id），
			// 不是调用方 —— 但 §15.2 没有"上游数据畸形"这一格，而 500 会把"这份清单畸形"冒充成"Core 自己坏了"，
			// 所以按主控指定的那一个落，不复用 serverError。
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), false)
		default:
			if status, code, retryable, mapped := probeErrorOf(err); mapped {
				// 连接与握手那一串错与 probe 是同一批（同一个 Adapter 在同一条腿上），
				// 所以复用 probeErrorOf，不另写第二份映射表 —— 两份迟早分叉。
				writeError(w, status, code, err.Error(), retryable)
				return
			}
			serverError(w, r, err)
		}
		return
	}
	views := make([]discoveredSessionView, 0, len(items))
	for _, it := range items {
		views = append(views, discoveredSessionView{SessionID: it.SessionID, RemoteSessionID: it.RemoteSessionID})
	}
	// `items` 是 required：空页也要出键、出空数组（同 inbox 那条口径）。
	if views == nil {
		views = []discoveredSessionView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

// registerAdapterProbeRoute 认领子树 `/api/v1/adapters/`。
// 认领整棵子树是有代价的：`/api/v1/adapters/{id}` 与 `.../capabilities` 本来由 catch-all 回 501，
// 现在由这里回 —— 状态与码都没变（501 `not_implemented`），但**必须由自己的用例钉住**，
// 不然"接一条口顺手把邻居接成 404/405"就是静默的行为变更。
func registerAdapterProbeRoute(mux *http.ServeMux, c Config) {
	mux.HandleFunc("/api/v1/adapters/", func(w http.ResponseWriter, r *http.Request) {
		seg, tail, ok := splitAdapterPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"adapters 这条路径只认 {key}/probe、{id}/sessions:discover、{id} 与 {id}/capabilities；别的形状 Core 不猜", true)
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
			// 这一格里 `seg` 就是 **adapter_key**（yaml 的 `probeAdapter` 那一格明写"路径参数 `key` 是 `adapter_key`（非实例 id）"）。
			probeAdapter(w, r, c, seg)
		case "sessions:discover":
			// T-195③/T-198：这一格以前到不了这里 —— `splitAdapterPath` 见整条路径里有冒号就判"不认形状"，
			// 于是它回的是"形状不认识"的 501 而不是 405；修法按主控说的**扩它认识的形状**，不是改 405 文案。
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/adapters/{id}/sessions:discover 只有 POST（它会真去连一次 Provider）", false)
				return
			}
			if c.SessionDiscover == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"sessions:discover 还没装配（cmd/core 没给出 Adapter 的起法）", true)
				return
			}
			if r.Header.Get("Idempotency-Key") == "" {
				// T-204④：yaml 这条腿本来就带 `parameters/IdempotencyKey`（`discoverAdapterSessions` 那一格，而那个参数是 `required: true`），
				// §2 的通则也是"有副作用的 POST 必填"——缺这道检查是**代码的缺口**，不是契约没写。
				// 顺序与话术都照上面那格 probe（405 → 501 → 403），同一件事不许有两种写法。
				writeError(w, http.StatusForbidden, "idempotency_key_required",
					"POST /api/v1/adapters/{id}/sessions:discover 是有副作用的写，必须带 Idempotency-Key", false)
				return
			}
			// 这一格里 `seg` 换了一件事：**它是 `adapter_instances.id`，不是 adapter_key**
			// （yaml 的 `discoverAdapterSessions` 与 `probeAdapter` 分开登记，T-204② 复核的结论就是"`{id}` 是对的，别改 yaml"）。
			discoverAdapterSessions(w, r, c, seg)
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

// splitAdapterPath 把 `/api/v1/adapters/<那一段>/<尾巴>` 拆成 (seg, tail)。
// 第一个返回值**故意不叫 key 也不叫 id**：同一棵子树里两条路的路径参数不是同一个东西，
// 而 yaml 把它们分开登记 —— `probeAdapter` 那一格是 `adapter_key`（`parameters/Key`，明写"非实例 id"），
// `discoverAdapterSessions` 与 capabilities 那两格是 `adapter_instances.id`（`parameters/Id`，明写"不是 adapter_key"）。
// T-204② 点的就是这个：调用方拿到的 `seg` 该按所在那一格自己决定叫什么。
// 已知形状三种：`{key}/probe`、`{id}/capabilities`、`{id}/sessions:discover`（T-195③ 点名的那一格：
// 它整条路径里有冒号，所以"见冒号就不认"的旧写法把它挡在了分流之前）。
// 其余一律不认（空段、多一段、别的 custom verb、第二个冒号）。
func splitAdapterPath(path string) (seg, tail string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/adapters/")
	if rest == "" {
		return "", "", false
	}
	// discover 那一条先单独认：它是"段/动词:动词"的形状，通配规则会把它判成畸形。
	if s, found := strings.CutSuffix(rest, "/sessions:discover"); found {
		if s == "" || strings.Contains(s, "/") || strings.Contains(s, ":") {
			return "", "", false
		}
		return s, "sessions:discover", true
	}
	if strings.Contains(rest, ":") {
		return "", "", false
	}
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return rest, "", rest != ""
	}
	seg, tail = rest[:i], rest[i+1:]
	if seg == "" || tail == "" || strings.Contains(tail, "/") {
		return "", "", false
	}
	if tail != "probe" && tail != "capabilities" {
		return "", "", false
	}
	return seg, tail, true
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

// probeErrorOf 把 §15.1 的码映成 §15.2 的 transport 码与状态。**只列 probe 与 send 真能产生的那些**：
// 抄整张表就会抄出一份和契约各说一套的副本（同"validator 不许内嵌契约快照"那一族）。
// 认不出的码先经 `Degrade()` 落成 INTERNAL —— 拿别人没给过的数字现编一个名字，就是归因丢了。
// §15.2 表（adapter-protocol-v0.1.md:504 起）的三行是本批 send 接上后新可达的：
// delivery_mode_unsupported→422、session_not_found→404、provider_error→502。
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
	// §15.2 表：send 接上连接面后新可达的三行。
	case "delivery_mode_unsupported": // Adapter 明确拒了这个 delivery 态（§5 红线）
		return http.StatusUnprocessableEntity, "delivery_mode_unsupported", false, true
	case "session_not_found": // Adapter 侧没有这个远端会话（与 Core transport 的 404 同码不同生产者）
		return http.StatusNotFound, "session_not_found", false, true
	case "provider_error": // Provider 真的坏了：502 不是 503（能重试的不是 Core 这一侧）
		return http.StatusBadGateway, "provider_error", false, true
	default:
		return 0, "", false, false
	}
}
