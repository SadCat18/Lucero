// Package httpapi 是契约③ 的 transport 薄层：鉴权、参数形状、状态码与错误体。
// 这里不许出现任何业务判断或状态迁移（契约③ §7），一切走 internal/* 的 Service 与
// 事件提交之后的推送（ADR-008）。
package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/SadCat18/lucero/core/internal/project"
)

// Health 是 GET /api/v1/health 的响应体（契约③ §3 的四字段）。
// State 的词表契约里没给（见 progress 的 REQ-T-1），先按启动阶段实现：
// BOOTING / READY / STOPPING；ready 才是前端该看的权威闸门。
const (
	StateBooting  = "BOOTING"
	StateReady    = "READY"
	StateStopping = "STOPPING"
)

type Health struct {
	State            string `json:"state"`
	Version          string `json:"version"`
	MigrationVersion int64  `json:"migration_version"`
	Ready            bool   `json:"ready"`
}

// ErrorBody 是契约③ §2 的错误体形状。
type ErrorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
		Details   any    `json:"details,omitempty"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string, retryable bool) {
	var b ErrorBody
	b.Error.Code, b.Error.Message, b.Error.Retryable = code, msg, retryable
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(b)
}

// Tickets 是一次性短票（契约③ §1.2 REQ-F-2）：单次使用、校验即作废、默认 30 秒过期、
// 只在内存里，不写库也不写日志。
type Tickets struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time
	exp map[string]time.Time
}

func NewTickets(ttl time.Duration, now func() time.Time) *Tickets {
	if now == nil {
		now = time.Now
	}
	return &Tickets{ttl: ttl, now: now, exp: map[string]time.Time{}}
}

// Issue 每次都生成一张新票。它**必须**不进任何幂等缓存（REQ-F-11）：
// 命中缓存会把同一张票或其"已作废"结论重放出去。
func (t *Tickets) Issue() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败意味着系统熵源坏了，签发出来的票不可信；宁可不服务。
		panic("httpapi: 熵源不可用: " + err.Error())
	}
	ticket := base64.RawURLEncoding.EncodeToString(buf)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.exp[ticket] = t.now().Add(t.ttl)
	return ticket
}

// Redeem 校验即作废：无论成功失败都不留原票。
func (t *Tickets) Redeem(ticket string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	exp, ok := t.exp[ticket]
	if ok {
		delete(t.exp, ticket)
	}
	return ok && t.now().Before(exp)
}

// Config 是 transport 层需要的全部依赖，由 cmd/core 注入。
type Config struct {
	Token   string
	Version string
	// Health 读当前运行态；MigrationVersion 由调用方从真库里取，不在这里猜。
	State            func() string
	MigrationVersion func() int64
	Tickets          *Tickets

	// 三个都注入时才挂 /api/v1/tasks；缺任何一个就维持"未实现 501"，不给前端半套端点。
	Tasks    TaskService
	TaskList TaskLister
	// TaskGet 是 `GET /api/v1/tasks/{id}` 的读面（yaml `getTask`）。单列一个字段而不是塞进
	// TaskLister：列表那半早就装配好了，今天才接读面，合并接口会把"没接"与"接了但缺方法"混成同一种错。
	TaskGet TaskGetter
	Store   TaskStore

	// 注入时才挂 GET /api/v1/notifications（读侧一口，不需要 Store）。
	Notifications NotificationLister
	// NotifRead + Store 都在时才挂 POST /api/v1/notifications/{id}:read（写侧要事务通道）。
	NotifRead NotificationReader
	// NotifHandle + Store 都在时才挂 POST /api/v1/notifications/{id}:handle（契约① §1.1 第 21 条裁的边集）。
	// 与 NotifRead 分开两个字段：缺任何一个，那一条口就维持 501，不给前端半套端点。
	NotifHandle NotificationHandler

	// Projects + Store 都在时才挂 POST /api/v1/projects（写侧要事务通道）。
	Projects ProjectService
	// ProjectRead 在时才挂 GET /api/v1/projects；缺它就回 501 而不是 405（C28② 的口径）。
	ProjectRead project.Queryer

	// 注入时才挂 POST /api/v1/sessions/{id}:sync（今天只有只读链这一格能力）。
	Sessions SessionSyncer
	// 注入时才挂 GET /api/v1/sessions（面板今天就在调这条，还吃着 501）。
	SessionRead SessionReader
	// SessionManage + Store 都在时才挂 POST /api/v1/sessions/{id}:manage（写侧要事务通道）。
	SessionManage SessionManager
	// SessionMessages 注入时才挂 GET /api/v1/sessions/{id}/messages（读面；
	// RecordMessage 落地后生产者已在，api-contract :113~119 的禁令条件满足）。nil ⇒ 501。
	SessionMessages SessionMessageReader

	// 注入时才挂 POST /api/v1/adapters/{key}/probe（§7.2 的 backend 半边：只投影，不写）。
	Adapters AdapterProber
	// SessionDiscover 在时才挂 POST /api/v1/adapters/{id}/sessions:discover
	// （它是面板链第 2/3/4 格共用的那把锁：Core 侧今天没有第二条路能建出 sessions 行）。
	SessionDiscover SessionDiscoverer

	// 注入时才挂 GET /api/v1/executions?task_id=（T-229 的读面那一格：Wave-2 要看 execution 真建出来了）
	// 与 GET /api/v1/executions/{id}（yaml `getExecution` :496，第七十九批脱的 501）。
	// 两条读面共用这一个 *Repository 注入：要么整面在、要么整面不在，没有中间态。
	Executions ExecutionReader

	// Approvals 是 `GET /api/v1/approvals` 的读面（T-414 ③：handler 接线归 backend，
	// 而那一行的读法本身住在 `internal/approval` —— 本包只调它，拿不到任何入口符号）。
	// 没注入时这条口维持 501「没装配」，**不回空的一页**：空页说的是"今天确实没有审批"。
	Approvals ApprovalLister

	// ExecutionSend + Store 都在时才挂 POST /api/v1/executions/{id}/send（yaml :544；写侧要事务通道）。
	// 缺任一个则该路径落 catch-all 的 501（C28②：能力没接线不给半套面）。
	ExecutionSend ExecutionSender

	// 分派腿（T-263 + C43 后半格）：Instances 解析 adapter_key → adapter_instances.id；
	// ExecutionCreate 在同一笔事务里建 Execution。两个都注入时 :queue 才走完整分派到 PREPARING v3；
	// 缺任一个则 :queue 维持 501（C28②：能力没接线不给半套面）。
	Instances       AdapterResolver
	ExecutionCreate ExecutionCreator
}

// NewHandler 装出 /api/v1 面：除 /health 外全部要求 Bearer；带 Origin 的外部请求 403；
// 写操作（POST/PATCH/DELETE，签发票的 /ws-ticket 除外）必须带 X-Lucero-Principal: local-user。
func NewHandler(c Config) http.Handler {
	mux := http.NewServeMux()
	registerTaskRoutes(mux, c)
	registerNotificationRoutes(mux, c)
	registerNotificationItemRoute(mux, c)
	registerProjectRoutes(mux, c)
	registerProjectItemRoute(mux, c)
	registerSessionRoutes(mux, c)
	registerSessionListRoute(mux, c)
	registerAdapterProbeRoute(mux, c)
	registerExecutionRoutes(mux, c)
	registerApprovalRoutes(mux, c)

	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "health 只支持 GET", false)
			return
		}
		writeJSON(w, http.StatusOK, Health{
			State: c.State(), Version: c.Version,
			MigrationVersion: c.MigrationVersion(), Ready: c.State() == StateReady,
		})
	})

	mux.HandleFunc("/api/v1/ws-ticket", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "签发票必须用 POST", false)
			return
		}
		// 反向用例钉着这条豁免：票不进幂等缓存，两次请求一定拿到两张不同的有效票。
		writeJSON(w, http.StatusOK, struct {
			Ticket    string `json:"ticket"`
			ExpiresIn int    `json:"expires_in"`
		}{Ticket: c.Tickets.Issue(), ExpiresIn: int(c.Tickets.ttl.Seconds())})
	})

	// 其余 /api/v1 与 /ws/v1 都还没接：明确 501，不假装成功（契约③ §8 不做的事之一）。
	// T-229 顺手那一格：**这里不列举清单** —— 列一次就要有人维护一次，过期比缺更坏
	// （上一版写着"只提供 /api/v1/health 与 /api/v1/ws-ticket"，现盘早已不止这两条）。
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"这条路径 Core 没登记；已登记的端点看契约③ §3（specs/api-contract-v0.1.md）", true)
	})

	return decorate(mux, c)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decorate 是全局中间件：CORS → Bearer → Principal。顺序有意如此：
// 先挡浏览器跨源，再验共享口令，最后才看主体声明。
func decorate(next http.Handler, c Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !LoopbackOrigin(origin) {
			writeError(w, http.StatusForbidden, "origin_not_allowed", "Core 不接受该来源的跨源请求", false)
			return
		}
		if r.URL.Path != "/api/v1/health" {
			want := "Bearer " + c.Token
			if got := r.Header.Get("Authorization"); subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized", "缺少或错误的共享口令", false)
				return
			}
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			// 签发票不改变业务状态，所以不要求主体声明（REQ-F-11 同族豁免）；
			// 其余写操作一律要 local-user，system/adapter 不接受外部声明（契约③ §1.4）。
			if r.URL.Path != "/api/v1/ws-ticket" && r.Header.Get("X-Lucero-Principal") != "local-user" {
				writeError(w, http.StatusUnauthorized, "unauthorized",
					"写操作必须带 X-Lucero-Principal: local-user", false)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// LoopbackOrigin：默认只放行 127.0.0.1 / localhost / ::1 的任意端口。
// 依据是契约③ §1.1 的威胁模型本来就是"防非本机"，而桌面 WebView 与 Vite 开发服都跑在回环上。
// 真正的边界仍是 Bearer 口令，这里只是不给自己添堵。放宽/收紧都请主控裁定。
func LoopbackOrigin(origin string) bool {
	rest, ok := strings.CutPrefix(origin, "http://")
	if !ok {
		rest, ok = strings.CutPrefix(origin, "https://")
	}
	if !ok {
		return false
	}
	host := rest
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}
