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

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

// 这三个口都由 internal/task 与 internal/platform/db 的实现满足（依赖方向合法：
// transport → 业务包 → platform/db）。handler 里只做解析/校验/状态码，
// 状态迁移与事件写入全在 Service 里（契约③ §7：薄层不得含业务判断）。
type TaskService interface {
	Create(ctx context.Context, tx *db.Tx, in task.NewTask) (task.Task, error)
	Queue(ctx context.Context, tx *db.Tx, id string, cmd task.Command) (task.Result, error)
	Pause(ctx context.Context, tx *db.Tx, id string, cmd task.Command) (task.Result, error)
	// Resume 的目标态由 handler 传进来：§5.1 有两条合法出边，选哪条是请求里的信息，不是这里该猜的。
	Resume(ctx context.Context, tx *db.Tx, id string, to task.State, cmd task.Command) (task.Result, error)
	Cancel(ctx context.Context, tx *db.Tx, id string, cmd task.Command) (task.Result, error)
	Accept(ctx context.Context, tx *db.Tx, id string, cmd task.Command) (task.Result, error)
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
	RequestedBy string `json:"requested_by,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	Version     int64  `json:"version"` // 契约③ §2：聚合读必须带版本，供 If-Match
}

func viewOf(t task.Task) taskView {
	return taskView{ID: t.ID, Title: t.Title, Description: t.Description, Status: string(t.Status),
		Priority: t.Priority, Mode: t.Mode, RiskLevel: t.RiskLevel, ProjectID: t.ProjectID,
		RequestedBy: t.RequestedBy, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Version: t.Version}
}

// registerTaskRoutes 只在三个依赖都注入时才挂；没注入时保持 §3 里那条"未实现 501"，
// 不返回半套端点骗前端。
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
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"状态动作必须用 POST …:queue", false)
			return
		}
		id, action, ok := splitTaskAction(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"该状态动作尚未实现", true)
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

// splitTaskAction 解析契约③ §3 的 custom verb 形状：/api/v1/tasks/{id}:queue
func splitTaskAction(path string) (id, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/tasks/")
	i := strings.LastIndexByte(rest, ':')
	if i <= 0 || rest == "" {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
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
		created, err := c.Tasks.Create(r.Context(), tx, task.NewTask{
			ID: id, EventID: newUUIDv7(), Title: body.Title, Description: body.Description,
			Mode: body.Mode, Priority: body.Priority, RiskLevel: body.RiskLevel,
			ProjectID: body.ProjectID, RequestedBy: principal,
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

func queueTask(w http.ResponseWriter, r *http.Request, c Config, id string) {
	taskAction(w, r, c, id, task.Command{}, func(ctx context.Context, tx *db.Tx, cmd task.Command) (task.Result, error) {
		return c.Tasks.Queue(ctx, tx, id, cmd)
	})
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
func sealIdempotency(ctx context.Context, tx *db.Tx, key string, v taskView) (string, error) {
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
