package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// 这三个口都由 internal/task 与 internal/platform/db 的实现满足（依赖方向合法：
// transport → 业务包 → platform/db）。handler 里只做解析/校验/状态码，
// 状态迁移与事件写入全在 Service 里（契约③ §7：薄层不得含业务判断）。
type TaskService interface {
	Create(ctx context.Context, tx *db.Tx, in task.NewTask) (task.Task, error)
	Queue(ctx context.Context, tx *db.Tx, id string, cmd task.Command) (task.Result, error)
	// Prepare 是分派腿第三步（QUEUED→PREPARING），缺 execution_id 在碰库之前就失败（§6）。
	Prepare(ctx context.Context, tx *db.Tx, id string, cmd task.Command) (task.Result, error)
	Pause(ctx context.Context, tx *db.Tx, id string, cmd task.Command) (task.Result, error)
	// Resume 的目标态由 handler 传进来：§5.1 有两条合法出边，选哪条是请求里的信息，不是这里该猜的。
	Resume(ctx context.Context, tx *db.Tx, id string, to task.State, cmd task.Command) (task.Result, error)
	Cancel(ctx context.Context, tx *db.Tx, id string, cmd task.Command) (task.Result, error)
	Accept(ctx context.Context, tx *db.Tx, id string, cmd task.Command) (task.Result, error)
}

// AdapterResolver 是分派腿的只读门（T-263）：adapter_key → adapter_instances.id。
// 由 *adapter.SQLiteStore 直接满足；返回 id 而不是整行，把"选哪一列"的归属留在域包里。
type AdapterResolver interface {
	ResolveInstance(ctx context.Context, adapterKey string) (string, error)
}

// ExecutionCreator 是分派腿第二步的写面：在同一笔事务里建 Execution（§6 的 execution_created）。
// 由 *execution.Service 直接满足。
type ExecutionCreator interface {
	Create(ctx context.Context, tx *db.Tx, in execution.NewExecution) (execution.Execution, error)
}

type TaskLister interface {
	List(ctx context.Context, f task.Filter) ([]task.Task, error)
}

type TaskStore interface {
	Write(ctx context.Context, fn func(tx *db.Tx) error) error
}

// newUUIDv7 生成领域模型「ID 规范」要的 UUIDv7（48bit 毫秒时间戳 + ver 7 + 61bit 随机）。
// 不引第三方库：go.mod 归主控独占，而这里只要一个形如 v7 的唯一 id。
func newUUIDv7() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("httpapi: 熵源不可用: " + err.Error())
	}
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type createTaskBody struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Priority    string `json:"priority"`
	RiskLevel   string `json:"risk_level"`
	ProjectID   string `json:"project_id"`
	// SessionID 是 REQ-LK-1（甲）那格：yaml `CreateTaskRequest` 里它早就在（可空），
	// 非空时同事务挂一行 `task_session_links(role=ORIGIN)`；JSON `null` 与缺省都落成 ""（同一处置）。
	SessionID string `json:"session_id"`
}

type taskView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Mode        string `json:"mode"`
	RiskLevel   string `json:"risk_level"`
	ProjectID   string `json:"project_id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	RequestedBy string `json:"requested_by,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	Version     int64  `json:"version"` // 契约③ §2：聚合读必须带版本，供 If-Match
}

func viewOf(t task.Task) taskView {
	return taskView{ID: t.ID, Title: t.Title, Description: t.Description, Status: string(t.Status),
		Priority: t.Priority, Mode: t.Mode, RiskLevel: t.RiskLevel, ProjectID: t.ProjectID,
		SessionID:   t.SessionID,
		RequestedBy: t.RequestedBy, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Version: t.Version}
}

// registerTaskRoutes 只在三个依赖都注入时才挂；没注入时保持 §3 里那条"未实现 501"，
// 不返回半套端点骗前端。分派腿的两个额外依赖（Instances/ExecutionCreate）不进这道总闸 ——
// 它们只 gate :queue 这一格（缺了回 501），GET 列表与其它状态动作不受影响。
func registerTaskRoutes(mux *http.ServeMux, c Config) {
	if c.Tasks == nil || c.TaskList == nil || c.Store == nil {
		return
	}
	mux.HandleFunc("/api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listTasks(w, r, c)
		case http.MethodPost:
			createTask(w, r, c)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "tasks 只支持 GET 与 POST", false)
		}
	})
	mux.HandleFunc("/api/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		id, action, ok := splitTaskAction(r.URL.Path)
		if !ok {
			// 形状不认识：501 且不猜它该走哪条腿（原来这里排在方法判断后面，所以畸形路径会被回成 405）。
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"tasks 的单条路径只认 {id} 与 {id}:状态动作；别的形状 Core 不猜", true)
			return
		}
		if action == "" {
			// **T-195④ 的那处真 bug 就修在这里**：`GET /api/v1/tasks/{id}`（yaml `getTask`，09-22 LAG-1 写进 §3）
			// 与 `PATCH /api/v1/tasks/{id}`（`updateTask`）都是**合法方法**，而这两条今天没接线 ⇒
			// 按 C28② 该回 501，不是 405。原来第一句就 `r.Method != POST → 405`，等于叫客户去换方法，
			// 而它换哪个方法都一样没接线 —— 那正是 §15.2 那格禁止的互换。
			switch r.Method {
			case http.MethodGet, http.MethodPatch:
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"GET / PATCH /api/v1/tasks/{id} 都还没接线（declared 在 yaml 里，缺的是实现）", true)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/tasks/{id} 只接受 GET 与 PATCH", false)
			}
			return
		}
		// custom verb 那一类：yaml 里每条都只声明 POST ⇒ 别的动词确实是"方法不被接受"，405 站得住。
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"状态动作必须用 POST …:"+action, false)
			return
		}
		switch action {
		case "queue":
			queueTask(w, r, c, id)
		case "pause":
			pauseTask(w, r, c, id)
		case "resume":
			resumeTask(w, r, c, id)
		case "cancel":
			cancelTask(w, r, c, id)
		case "accept":
			acceptTask(w, r, c, id)
		default:
			// 没听过的 verb（以及 §3 里还没排到 :transfer 一类）：明确 501，不假成功。
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"该状态动作尚未实现", true)
		}
	})
}

// splitTaskAction 解析契约③ §3 的两种形状：`/api/v1/tasks/{id}`（action 返回空串）与
// `/api/v1/tasks/{id}:queue`。**没动词那条以前直接判 not-ok**，所以 `GET /tasks/{id}` 连"合法方法"这一步都到不了
// （T-195④）。现在空 action 是合法结果，由调用方分流；畸形形状（空 id、多一段、id 里还带冒号）才算不认。
func splitTaskAction(path string) (id, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/tasks/")
	if rest == "" || strings.Contains(rest, "/") {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, ':')
	if i < 0 {
		return rest, "", true
	}
	id, action = rest[:i], rest[i+1:]
	if id == "" || action == "" || strings.Contains(id, ":") {
		return "", "", false
	}
	return id, action, true
}

func createTask(w http.ResponseWriter, r *http.Request, c Config) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		// §40 + 契约③ §2：建任务属于必须带 Idempotency-Key 的高风险写。
		writeError(w, http.StatusForbidden, "idempotency_key_required",
			"POST /api/v1/tasks 必须带 Idempotency-Key", false)
		return
	}
	var body createTaskBody
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "读请求体失败", true)
		return
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "请求体不是合法 JSON: "+err.Error(), false)
		return
	}
	principal := r.Header.Get("X-Lucero-Principal")
	correlation := newUUIDv7() // 一条命令链共享同一个 correlation_id（契约① §4）

	id := newUUIDv7()
	var sealed string // 首次响应体：本轮刚造的，或命中已封存的那一份 —— 对外没有区别
	err = c.Store.Write(r.Context(), func(tx *db.Tx) error {
		// 幂等记录与副作用在同一事务里：主键冲突 = 这条 key 已经有人干过，整个事务回滚，
		// 高风险动作一次都不会重复执行（§40 的目的）。
		s, err := claimIdempotency(r.Context(), tx, key, principal, "task_created", hashOf(raw))
		if err != nil {
			return err
		}
		if s != "" {
			sealed = s // 已封存 → 原样回首次响应，不再碰任何聚合
			return nil
		}
		// REQ-LK-1（甲）：link 那一行的 id 由调用方铸（与 task id 同规矩，§3），
		// 只有真给了来源会话才铸 —— 没给就不该凭空多一个 UUID。
		linkID := ""
		if body.SessionID != "" {
			linkID = newUUIDv7()
		}
		created, err := c.Tasks.Create(r.Context(), tx, task.NewTask{
			ID: id, EventID: newUUIDv7(), Title: body.Title, Description: body.Description,
			Mode: body.Mode, Priority: body.Priority, RiskLevel: body.RiskLevel,
			ProjectID: body.ProjectID, SessionID: body.SessionID, SessionLinkID: linkID,
			RequestedBy: principal,
			PrincipalID: principal, CorrelationID: correlation, CausationID: correlation,
		})
		if err != nil {
			return err
		}
		// 封存的是"当时那份响应"：任务后来被 queue 掉了，重放仍回 CREATED/v1，
		// 因为契约③ §2 要的是首次响应，不是当前状态（当前状态请 GET）。
		sealed, err = sealIdempotency(r.Context(), tx, key, viewOf(created))
		return err
	})
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(sealed))
	case errors.Is(err, errReplay):
		writeError(w, http.StatusConflict, "idempotency_replay",
			"同一 Idempotency-Key 的首次请求还没完成，暂时没有可回放的响应", true)
	case errors.Is(err, errKeyMismatch):
		writeError(w, http.StatusConflict, "idempotency_mismatch",
			"同一 Idempotency-Key 带了不同的请求体（键含 request_hash）", false)
	case errors.Is(err, task.ErrInvalidTask):
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), false)
	case errors.Is(err, session.ErrSessionNotFound):
		// REQ-LK-1 第②条：**不用泛型 `not_found`** —— 与 send 腿「没有会话就没有落点」同一格口径、同一个码。
		writeError(w, http.StatusNotFound, "session_not_found", err.Error(), false)
	default:
		serverError(w, r, err)
	}
}

func listTasks(w http.ResponseWriter, r *http.Request, c Config) {
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
	f := task.Filter{Status: task.State(q.Get("status")), ProjectID: q.Get("project"), Limit: limit}
	if f.Status != "" && !knownState(f.Status) {
		writeError(w, http.StatusBadRequest, "validation_failed",
			"status 不在 §5.1 的状态集合里: "+string(f.Status), false)
		return
	}
	if cur := q.Get("cursor"); cur != "" {
		at, id, err := decodeCursor(cur)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "cursor 不合法", false)
			return
		}
		f.AfterCreatedAt, f.AfterID = at, id
	}
	items, err := c.TaskList.List(r.Context(), f)
	if err != nil {
		serverError(w, r, err)
		return
	}
	views := make([]taskView, 0, len(items))
	for _, t := range items {
		views = append(views, viewOf(t))
	}
	next := ""
	if len(items) == limit && len(items) > 0 {
		last := items[len(items)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": next})
}

// taskAction 是 :queue / :pause / :resume 共用的外壳：解析 If-Match → 一个事务里跑那一步 → 统一映射错误码。
// 三个动作只在"跑哪一步、命令里多带什么"上不同；把错误码映射写三份迟早会漂，所以只写一份。
// handler 层零业务判断（契约③ §7）：状态与合法性全在 task.Service 里判。
func taskAction(w http.ResponseWriter, r *http.Request, c Config, id string, base task.Command,
	run func(context.Context, *db.Tx, task.Command) (task.Result, error)) {

	var (
		wantVersion int64
		res         task.Result
	)
	if v := r.Header.Get("If-Match"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "validation_failed", "If-Match 必须是正整数版本", false)
			return
		}
		wantVersion = n
	}
	correlation := newUUIDv7()
	cmd := base
	cmd.EventID = newUUIDv7()
	cmd.PrincipalID = r.Header.Get("X-Lucero-Principal")
	cmd.CorrelationID, cmd.CausationID = correlation, correlation
	cmd.WantVersion = wantVersion

	err := c.Store.Write(r.Context(), func(tx *db.Tx) error {
		var err error
		res, err = run(r.Context(), tx, cmd)
		return err
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": string(res.To), "version": res.Version})
	case errors.Is(err, task.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error(), true)
	case errors.Is(err, task.ErrIllegalTransition):
		writeError(w, http.StatusConflict, "illegal_transition", err.Error(), false)
	case errors.Is(err, task.ErrInvalidTask):
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), false)
	case errors.Is(err, db.ErrAggregateNotFound) || errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "not_found", "没有这条 Task", false)
	default:
		serverError(w, r, err)
	}
}

// queueTask 是 C43 裁定的完整落地：前半格由调用方声明 adapter_key（readAdapterKey），
// 后半格把它解析成 adapter_instances.id 再在同一笔事务里走三步 ——
// Tasks.Queue（CREATED→QUEUED）→ Executions.Create（恰一行 execution_created）→
// Tasks.Prepare（QUEUED→PREPARING、task_preparing）—— 成功回 PREPARING v3。
//
// 分派依赖（Instances + ExecutionCreate）没注入时回 501（C28②）：路由已在、能力没接线，
// 不给"能 queue 但建不出 Execution"的半套面。解析走 d.Read（先解析后开事务，别把 SELECT
// 压在串行的写通道里）；adapter_key 不存在 ⇒ 404，且在碰任何聚合之前返回。
//
// 明确不做（backend 第五十八批 Next action 原文）：(a) 不推 execution_starting —— §7 序列
// 只到 execution_created → task_preparing，且 Started 是 STARTING→RUNNING、要 provider_execution_id，
// 没有进程起来就写 STARTING 是给日志记一件没发生的事；(b) 501「本机没配起法」/503「有实例连不上」
// 两臂要接 launcher 的运行时答案，拿不到就不先写。
//
// 重复 :queue 不需要幂等键：任务已在 PREPARING ⇒ Tasks.Queue 回 illegal_transition ⇒ 409
// 且整笔事务回滚 ⇒ execution 不会多第二行（有用例钉这一条）。
func queueTask(w http.ResponseWriter, r *http.Request, c Config, id string) {
	adapterKey, ok := readAdapterKey(w, r)
	if !ok {
		return
	}
	if c.Instances == nil || c.ExecutionCreate == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented",
			":queue 的分派腿还没装配（缺 AdapterResolver 或 ExecutionCreator）", true)
		return
	}
	// 先解析后开事务：读走 d.Read，写事务里只做那三步状态推进。
	instanceID, err := c.Instances.ResolveInstance(r.Context(), adapterKey)
	if errors.Is(err, db.ErrAggregateNotFound) {
		writeError(w, http.StatusNotFound, "not_found",
			"没有这个 adapter_key 对应的实例（先 probe 一次让它登记）", false)
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	executionID := newUUIDv7()
	taskAction(w, r, c, id, task.Command{}, func(ctx context.Context, tx *db.Tx, cmd task.Command) (task.Result, error) {
		// ① CREATED→QUEUED：cmd 带着调用方的 If-Match（WantVersion），在这一步生效。
		if _, err := c.Tasks.Queue(ctx, tx, id, cmd); err != nil {
			return task.Result{}, err
		}
		// ② 同一笔事务里建 Execution。EventID/Correlation/Causation 复用 cmd 的命令链
		// （契约① §4：一条命令链共享同一个 correlation_id）；AttemptNo=1、TriggerInitial 由 Create 默认。
		//
		// session_id 装配（第六十一批）：links 已有则抄第一条（按 ordinal）；没有则留空不发明 ——
		// 创建侧 POST /tasks 无 session_id、今天也没有生产写入者建 links（第 264 条上报的缺口）。
		// send 腿对空 session_id 回 404 session_not_found，不静默造一个会话。
		sessionID := ""
		if links, err := session.NewRepository(tx).ForTask(ctx, id); err != nil {
			return task.Result{}, err
		} else if len(links) > 0 {
			sessionID = links[0].Session.ID
		}
		if _, err := c.ExecutionCreate.Create(ctx, tx, execution.NewExecution{
			ID:                executionID,
			TaskID:            id,
			AdapterInstanceID: instanceID,
			SessionID:         sessionID,
			EventID:           newUUIDv7(),
			PrincipalID:       cmd.PrincipalID,
			CorrelationID:     cmd.CorrelationID,
			CausationID:       cmd.CausationID,
		}); err != nil {
			return task.Result{}, err
		}
		// ③ QUEUED→PREPARING：execution_id 是这条事件的语义核心（§6）。
		// WantVersion 清零 —— If-Match 只校验调用方看到的那个起始版本（v1），
		// 后续两步是同一笔原子操作的内部推进，不能再拿 v1 去撞中间态 v2。
		prepCmd := cmd
		prepCmd.WantVersion = 0
		prepCmd.EventID = newUUIDv7()
		prepCmd.ExecutionID = executionID
		return c.Tasks.Prepare(ctx, tx, id, prepCmd)
	})
}

// readAdapterKey 读 `:queue` 的请求体。C43 把 `adapter_key` 定成**必填**，所以这里的口径是：
// 没有体、体不是 JSON、字段缺省、值是空白 —— 四种都算"没声明调度对象"，一律 400 `validation_failed`，
// 且在碰任何聚合之前返回（与 `readReasonBody` 那三个"reason 可选"的动作不同，那三个的 body 契约里没定义）。
func readAdapterKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "读请求体失败", true)
		return "", false
	}
	var body struct {
		AdapterKey string `json:"adapter_key"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "请求体不是合法 JSON: "+err.Error(), false)
		return "", false
	}
	if strings.TrimSpace(body.AdapterKey) == "" {
		writeError(w, http.StatusBadRequest, "validation_failed",
			"POST …:queue 必须带 adapter_key（C43：调度对象由调用方声明，Core 不猜默认、不代选实例）", false)
		return "", false
	}
	return body.AdapterKey, true
}

// readReasonBody 读 :pause / :cancel 那一类"只有一个可选 reason 字段"的请求体。
// reason 允许缺省/空串：§3 没给这两个动作定义 body，"条款查不到即作废"，我不替契约发明必填要求。
// 返回 ok=false 时已经写好 400，调用方直接 return。
func readReasonBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "读请求体失败", true)
		return "", false
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if strings.TrimSpace(string(raw)) != "" {
		if err := json.Unmarshal(raw, &body); err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "请求体不是合法 JSON: "+err.Error(), false)
			return "", false
		}
	}
	return body.Reason, true
}

func pauseTask(w http.ResponseWriter, r *http.Request, c Config, id string) {
	reason, ok := readReasonBody(w, r)
	if !ok {
		return
	}
	taskAction(w, r, c, id, task.Command{Reason: reason},
		func(ctx context.Context, tx *db.Tx, cmd task.Command) (task.Result, error) {
			return c.Tasks.Pause(ctx, tx, id, cmd)
		})
}

func cancelTask(w http.ResponseWriter, r *http.Request, c Config, id string) {
	reason, ok := readReasonBody(w, r)
	if !ok {
		return
	}
	taskAction(w, r, c, id, task.Command{Reason: reason},
		func(ctx context.Context, tx *db.Tx, cmd task.Command) (task.Result, error) {
			return c.Tasks.Cancel(ctx, tx, id, cmd)
		})
}

// acceptTask 没有请求体：验收人是 `X-Lucero-Principal` 那个写主体，不是 body 里自报的字段
// （契约③ §1.4）。§5.4 之外它也是第一个"动作的语义全在起始态+主体"上、不需要任何参数的面。
func acceptTask(w http.ResponseWriter, r *http.Request, c Config, id string) {
	taskAction(w, r, c, id, task.Command{},
		func(ctx context.Context, tx *db.Tx, cmd task.Command) (task.Result, error) {
			return c.Tasks.Accept(ctx, tx, id, cmd)
		})
}

// resumeBody 的 to **必填**：§5.1 里 PAUSED→QUEUED 与 PAUSED→RUNNING 两条边都合法，
// 而 tasks 表没有"暂停之前是什么"这一列 —— 替客户端选等于替它决定要不要有一个 Execution。
type resumeBody struct {
	To string `json:"to"`
}

func resumeTask(w http.ResponseWriter, r *http.Request, c Config, id string) {
	var body resumeBody
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "读请求体失败", true)
		return
	}
	if err := json.Unmarshal(raw, &body); err != nil || (body.To != "QUEUED" && body.To != "RUNNING") {
		writeError(w, http.StatusBadRequest, "validation_failed",
			`:resume 需要一个 JSON 体 {"to":"QUEUED"|"RUNNING"}`, false)
		return
	}
	to := task.State(body.To)
	taskAction(w, r, c, id, task.Command{}, func(ctx context.Context, tx *db.Tx, cmd task.Command) (task.Result, error) {
		return c.Tasks.Resume(ctx, tx, id, to, cmd)
	})
}

// serverError 不回传内部错误文本：里面可能带 SQL、列名甚至路径。
// 也不在这里打日志 —— §2.2 要求日志过同一个 redactor，那一步归 cmd/core 装 slog handler 时接，
// 在那之前宁可把错误吞在这里，也不写一条未脱敏的日志出去（§12 红线）。
func serverError(w http.ResponseWriter, _ *http.Request, _ error) {
	writeError(w, http.StatusInternalServerError, "internal_error", "Core 没能完成这次操作", true)
}

// --- 幂等 ---

var (
	errReplay      = errors.New("httpapi: 重复的 Idempotency-Key")
	errKeyMismatch = errors.New("httpapi: Idempotency-Key 对应的请求体不一致")
)

// claimIdempotency 决定这一次 POST 归谁干。三种结果互斥：
//   - ("", nil)     → 这条 key 由本次占用（已写 IN_PROGRESS 行），调用方去执行副作用；
//   - (body, nil)   → 命中已封存的首次响应，调用方把 body 原样发出去，什么都不用再写；
//   - ("", errReplay) → 首次请求还挂着（IN_PROGRESS），此刻确实没有响应可回。
//
// §1.1 第 12 条：这张表是"故意不发事件的可变表"，所以占坑用 Append、封存用 UpdateOnce，都不占聚合版本。
func claimIdempotency(ctx context.Context, tx *db.Tx, key, principal, operation, hash string) (string, error) {
	var (
		existingHash, state string
		stored              sql.NullString
	)
	err := tx.QueryRowContext(ctx,
		`SELECT request_hash, state, response_json FROM idempotency_keys WHERE key = ?`, key).
		Scan(&existingHash, &state, &stored)
	switch {
	case err == nil:
		if existingHash != hash {
			return "", errKeyMismatch
		}
		if state == "COMPLETED" && stored.String != "" {
			return stored.String, nil
		}
		return "", errReplay
	case errors.Is(err, sql.ErrNoRows):
		// 没记录，往下 INSERT
	case err != nil:
		// 读不出来的其它错误也别蒙：让 INSERT 去撞唯一约束，语义更清楚。
		return "", err
	}
	now := time.Now().UnixMilli()
	var p any
	if principal != "" {
		p = principal
	}
	if _, err := tx.Append("idempotency_keys",
		[]string{"key", "principal_id", "operation", "request_hash", "state", "created_at", "updated_at"},
		[]any{key, p, operation, hash, "IN_PROGRESS", now, now}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: idempotency_keys.key") {
			return "", errReplay
		}
		return "", err
	}
	return "", nil
}

// sealIdempotency 把首次响应封进 idempotency_keys，返回**封存后从库里读回来的那一份**。
// guard 用 state != 'COMPLETED'：这是"只封存第一次"的强制点，不靠调用方自觉。
// 0 行 = 这一格已经被别人封了，本次就不该由我来宣称首次响应 → 按重放处理（事务回滚，副作用不留）。
//
// 为什么非要回读而不是 return string(body)：内部契约 §1.1 第 13 条的"响应体口径"（C18 裁定 ②）。
// body 是我自己 json.Marshal 的，落库的是 Tx.scrubArgs 处理后的另一份；没被脱敏命中时只差键序，
// 一旦被命中就是**真实内容分叉** —— 首次响应把未打码的值发到线上、库里存打码版，正好绕开 §2.2 要守的线。
// 回读之后：首次与重放逐字节相等，且 wire 与存储按构造一致，脱敏规则以后怎么改都不会漏。
// sealIdempotency 把首次响应封存进幂等记录。v 是 any：task 与 project 两条 POST 共用同一份封存逻辑，
// 复制第二份迟早会和这里的"先封存后写出"的顺序漂移（C18 裁定 ② 依赖那个顺序）。
func sealIdempotency(ctx context.Context, tx *db.Tx, key string, v any) (string, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	n, err := tx.UpdateOnce("idempotency_keys", key, "state", "COMPLETED",
		"state = ?, response_json = ?, updated_at = ?",
		[]any{"COMPLETED", string(body), time.Now().UnixMilli()})
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", errReplay
	}
	var stored string
	if err := tx.QueryRowContext(ctx,
		`SELECT response_json FROM idempotency_keys WHERE key = ?`, key).Scan(&stored); err != nil {
		return "", err
	}
	return stored, nil
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// encodeCursor 只服务**分页**游标：REQ-F-19 定下 `next_cursor == ""` 是"没有下一页"的唯一合法值。
// REQ-F-21/22 的**同步**游标口径与它相反（空串非法，只有非空串或 null），所以 `:sync` 落地时
// 另写一个编码函数，不许复用这两个函数、也不许在 validator 里把同名两套合并成一套。
func encodeCursor(createdAt int64, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d|%s", createdAt, id)))
}

func decodeCursor(s string) (int64, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, "", err
	}
	at, id, ok := strings.Cut(string(b), "|")
	if !ok || id == "" {
		return 0, "", errors.New("bad cursor")
	}
	// 只此一个分隔符：多吃一段就是吃下了别人的游标形状（inbox 那条口是三元组）。
	// 不挡的话一个 tasks/inbox 互换的游标会安静地返回空页，比 400 难查得多。
	if strings.Contains(id, "|") {
		return 0, "", errors.New("bad cursor")
	}
	n, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return 0, "", err
	}
	return n, id, nil
}

func knownState(s task.State) bool {
	return s == task.Created || s == task.Queued || s == task.Preparing || s == task.Running ||
		s == task.WaitingApproval || s == task.WaitingInput || s == task.Blocked || s == task.Paused ||
		s == task.Transferring || s == task.AgentCompleted || s == task.Verifying ||
		s == task.WaitingAcceptance || s == task.Accepted || s == task.Failed ||
		s == task.Cancelled || s == task.Interrupted || s == task.Unknown
}
