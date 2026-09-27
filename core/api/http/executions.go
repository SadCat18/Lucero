package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// ExecutionLister 是 GET /api/v1/executions 需要的读面，由 internal/execution 的 *Repository 直接满足
// （依赖方向合法：transport → 业务包）。T-229 说这一格"差的只是薄投影 + mux 注册"，这里就只做这两件事。
type ExecutionLister interface {
	ListByTask(ctx context.Context, taskID string) ([]execution.Execution, error)
}

// executionView 的必填七字段来自 yaml 的 Execution.required；其余按域里有什么出什么。
//
// **刻意不出的那些键**（model_id / 三份 snapshot / started_at / finished_at / heartbeat_at / created_at / updated_at /
// result_code / error_code / error_message）不是漏了：`execution.Execution` 这个域模型里没有它们
// （快照与时间戳在 repo 的扫描里就不取），transport 也不许去库里另开一条 SELECT ——
// "handler 里不许补字段"在 projects/adapters 那两格是同一条规矩（§7.2）。要出它们得先扩域模型，那是下一格的事。
type executionView struct {
	ID                     string  `json:"id"`
	TaskID                 string  `json:"task_id"`
	Status                 string  `json:"status"`
	AttemptNo              int64   `json:"attempt_no"`
	TriggerType            string  `json:"trigger_type"`
	StateConfidence        string  `json:"state_confidence"`
	Version                int64   `json:"version"`
	AdapterInstanceID      string  `json:"adapter_instance_id"`
	SessionID              *string `json:"session_id"`
	PredecessorExecutionID *string `json:"predecessor_execution_id"`
	ProviderExecutionID    *string `json:"provider_execution_id"`
}

// optStr 与 nullable 同一件事的字符串版：域里用 "" 表示"库里没这一列"，出口必须是 null 而不是 ""。
func optStr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func executionViewOf(e execution.Execution) executionView {
	return executionView{
		ID: e.ID, TaskID: e.TaskID, Status: string(e.Status), AttemptNo: e.AttemptNo,
		TriggerType: string(e.TriggerType), StateConfidence: string(e.StateConfidence), Version: e.Version,
		AdapterInstanceID: e.AdapterInstanceID, SessionID: optStr(e.SessionID),
		PredecessorExecutionID: optStr(e.PredecessorExecutionID), ProviderExecutionID: optStr(e.ProviderExecutionID),
	}
}

// registerExecutionRoutes 拆两条 pattern：`/api/v1/executions`（精确，列表读面）与
// `/api/v1/executions/`（前缀，单条/send）。两条各自闸门，不互相绑架 ——
// 只注入列表不该让 send 误挂，只注入 send 也不该让列表消失（C28② 的口径）。
func registerExecutionRoutes(mux *http.ServeMux, c Config) {
	if c.Executions != nil {
		mux.HandleFunc("/api/v1/executions", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"executions 这条只声明 GET（建 Execution 是编排腿的事，不是这条口）", false)
				return
			}
			listExecutions(w, r, c)
		})
	}
	if c.ExecutionSend == nil || c.Store == nil {
		return
	}
	mux.HandleFunc("/api/v1/executions/", func(w http.ResponseWriter, r *http.Request) {
		id, verb, ok := splitExecutionItem(r.URL.Path)
		switch {
		case !ok:
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"这条路径的形状我不认识（空 id、多一段或 id 里带冒号都不摸业务面）", true)
		case verb == "send":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/executions/{id}/send 只能 POST", false)
				return
			}
			sendExecution(w, r, c, id)
		case verb == "":
			// yaml 声明了 GET /executions/{id} 但实现没接 ⇒ 按 C28② 回 501，不是 405。
			if r.Method == http.MethodGet {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"GET /api/v1/executions/{id} 还没装配读通道", true)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"/api/v1/executions/{id} 只声明 GET；send 是 {id}/send", false)
		default:
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"没听过的 execution 子路径一律 501，不猜它该走哪条腿", true)
		}
	})
}

// splitExecutionItem 认两种形状：`/api/v1/executions/{id}/send` 与 `/api/v1/executions/{id}`。
// 不照抄 splitSessionVerb 的冒号分支：yaml 这条路径上没有 `:verb` 形状，认多了就是替别人决定归属。
func splitExecutionItem(path string) (id, verb string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/executions/")
	if rest == "" {
		return "", "", false
	}
	if sub, found := strings.CutSuffix(rest, "/send"); found {
		if sub == "" || strings.Contains(sub, "/") || strings.Contains(sub, ":") {
			return "", "", false
		}
		return sub, "send", true
	}
	if strings.Contains(rest, "/") || strings.Contains(rest, ":") {
		return "", "", false
	}
	return rest, "", true
}

// ExecutionSender 是 POST /api/v1/executions/{id}/send 的业务面，由 *execution.Service 满足。
// 两个方法而不是一个：REQ-S-1 的不变量要求**投递跑在任何事务之外**（写面只有一条串行连接，
// 起子进程/握手/等回执关进写事务 ⇒ 一次 send 停掉全 Core 的写）。
// `Reserve` 落"意图"（消息 + 状态边 + 事件），`Deliver` 打那一跳 —— transport 负责安排顺序，域负责各自那半。
type ExecutionSender interface {
	Reserve(ctx context.Context, tx *db.Tx, id string, cmd execution.SendCommand) (execution.SendReservation, error)
	Deliver(ctx context.Context, res execution.SendReservation) (execution.SendResult, error)
}

// sendBody 是 yaml SendMessageRequest（:1175）：required [text, delivery]，attachments 可选。
type sendBody struct {
	Text        string   `json:"text"`
	Delivery    string   `json:"delivery"`
	Attachments []string `json:"attachments"`
}

// sendResultView 是 yaml SendResult（:1186）：required [delivery, accepted]，两个 id 可 null。
type sendResultView struct {
	Delivery    string  `json:"delivery"`
	Accepted    bool    `json:"accepted"`
	MessageID   *string `json:"message_id"`
	ExecutionID *string `json:"execution_id"`
}

// sendExecution 是 REQ-S-1 的落点：原来这三步全挤在同一笔写事务里（旧形状见台账第 271 条），
// 于是 起子进程 / 协议握手 / 等对端回执 都占着 `platform/db` 那**一条**串行写连接（db.go:48），
// 一次挂住的 send 就停掉整个 Core 的写 —— 读面与 /health 走另一条池所以照答，这正是那条红难发现的原因。
//
// 现在拆成三拍：**第一笔（预留，事务内）→ 投递（事务外）→ 第二笔（落结果，事务内）**。
// 幂等语义按契约①「REQ-S-1 两段式的幂等语义」那一格：
//
//	第一笔提交即成为既成事实 ⇒ 同键重试**不重做意图**（一行消息都不许多写）；
//	投递失败/挂住 ⇒ 不密封 ⇒ execution 留 RUNNING、答案走已绑 503 的 `adapter_unresponsive`（④=丙）；
//	密封那一步的 guard 是 `state != COMPLETED` ⇒ 并发双续跑只有一个封得上，另一个 0 行 ⇒ 409 喊出来（③）。
func sendExecution(w http.ResponseWriter, r *http.Request, c Config, id string) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		// api-contract :142-145：send 是高风险写，Idempotency-Key 必带。先于读 body（同 createTask 顺序）。
		writeError(w, http.StatusForbidden, "idempotency_key_required",
			"POST /api/v1/executions/{id}/send 必须带 Idempotency-Key", false)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "读请求体失败", true)
		return
	}
	var body sendBody
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "请求体不合法: "+err.Error(), false)
		return
	}
	principal := r.Header.Get("X-Lucero-Principal")
	correlation := newUUIDv7()

	// 这批 id 只有**首攻**会用；续跑读的是上一趟写进凭证里的那一套（同键两次请求共用同一行消息靠的就是它）。
	cmd := execution.SendCommand{
		Text: body.Text, Delivery: body.Delivery, Attachments: body.Attachments,
		// HTTP 头透给域 → sendpipe → adapter（§4 的 Idempotency-Key；start_now/guide 的重试幂等靠它）。
		IdempotencyKey:  key,
		StartingEventID: newUUIDv7(), StartedEventID: newUUIDv7(),
		TaskEventID: newUUIDv7(), MessageEventID: newUUIDv7(), MessageID: newUUIDv7(),
		// §5.4 两条记录的 id：投影被拒时 Report 要用；缺它会 500 而不是 409。
		RejNotificationID: newUUIDv7(), RejEventID: newUUIDv7(), RejAuditID: newUUIDv7(),
		PrincipalID: principal, CorrelationID: correlation, CausationID: correlation,
	}

	var (
		res    execution.SendReservation
		sealed string
	)
	hash := hashOf(raw)

	// ── 第一笔：认领 + 意图 + 投递凭证。这一笔里一次网络都不发。
	if err := c.Store.Write(r.Context(), func(tx *db.Tx) error {
		state, payload, err := claimIdempotencyRow(r.Context(), tx, key, principal, "execution_send", hash)
		if err != nil {
			return err
		}
		switch state {
		case claimReplay:
			// 已密封 ⇒ 原样回放，业务行一行都不再写。
			sealed = payload
			return nil
		case claimResume:
			// 上一趟把意图提交了、没来得及密封 ⇒ 这次只续投递。
			// 解不出来就是这一格被别的口写过 ⇒ 原样上抛，不猜、不"重写一遍算了"。
			return json.Unmarshal([]byte(payload), &res)
		}
		out, err := c.ExecutionSend.Reserve(r.Context(), tx, id, cmd)
		if err != nil {
			return err
		}
		res = out
		identity, err := json.Marshal(res)
		if err != nil {
			return err
		}
		if err := recordSendIdentity(r.Context(), tx, key, string(identity)); err != nil {
			return err
		}
		if !res.NeedsTurn {
			// queue 没有投递这一跳 ⇒ 当场密封；凭证随后被真响应覆盖（读取条件始终是 state，两义不分叉）。
			sealed, err = sealIdempotency(r.Context(), tx, key, viewOfSend(out.Result()))
			return err
		}
		return nil
	}); err != nil {
		writeSendError(w, r, err)
		return
	}
	if sealed != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(sealed))
		return
	}

	// ── 投递：这一拍**不在任何事务里**。挂住 / 连不上 ⇒ 不密封 ⇒ 同键还能续跑。
	out, err := c.ExecutionSend.Deliver(r.Context(), res)
	if err != nil {
		writeSendError(w, r, err)
		return
	}

	// ── 第二笔：只落投递结果。0 行（并发里我输了）由 sealIdempotency 回 errReplay ⇒ 409，
	//    不是静默 202 —— 第 25 条③ 要的就是这声喊。
	if err := c.Store.Write(r.Context(), func(tx *db.Tx) error {
		var e error
		sealed, e = sealIdempotency(r.Context(), tx, key, viewOfSend(out))
		return e
	}); err != nil {
		writeSendError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(sealed))
}

// viewOfSend 把域的结果折算成 yaml `SendResult`（:1186）：required 两键 delivery/accepted，两个 id 可 null。
func viewOfSend(r execution.SendResult) sendResultView {
	return sendResultView{Delivery: r.Delivery, Accepted: r.Accepted,
		MessageID: optStr(r.MessageID), ExecutionID: optStr(r.ExecutionID)}
}

// writeSendError 是三拍共用的错误映射（预留里失败、投递里失败、密封里失败，答案必须是同一套码；
// 写三份迟早一份漂）。
func writeSendError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errReplay):
		// 两种真事实都到这里：首次还在飞（没凭证可读）、或密封那一步 0 行 = 并发续跑里我输了。
		// 都不静默当成功（CO-44 那条口径在这里的声音就是 409）。
		writeError(w, http.StatusConflict, "idempotency_replay",
			"同一 Idempotency-Key 的首次请求还没完成，暂时没有可回放的响应", true)
	case errors.Is(err, errKeyMismatch):
		writeError(w, http.StatusConflict, "idempotency_mismatch",
			"同一 Idempotency-Key 带了不同的请求体（键含 request_hash）", false)
	case errors.Is(err, execution.ErrInvalidExecution):
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), false)
	case errors.Is(err, execution.ErrSessionNotLinked), errors.Is(err, session.ErrSessionNotFound):
		// execution 没挂 session / 指向的 session 已不在：send 没有落点 ⇒ 404 session_not_found。
		writeError(w, http.StatusNotFound, "session_not_found", "这条 Execution 没有可用的 Session", false)
	case errors.Is(err, execution.ErrIllegalTransition), errors.Is(err, task.ErrIllegalTransition),
		errors.Is(err, task.ErrProjectionRejected):
		// 执行侧状态机拒绝 / task 投影拒绝（含没注入 reporter 的那条哨兵）都归 409。
		writeError(w, http.StatusConflict, "illegal_transition", err.Error(), false)
	case errors.Is(err, db.ErrAggregateNotFound), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "not_found", "没有这条 Execution", false)
	case errors.Is(err, execution.ErrTurnNotWired):
		// start_now/guide 要连接面，EnableTurn 没调过（-adapter 一个都没配）⇒ C28② 能力没接线 501。
		// 不静默降级成 queue —— yaml :554 禁的正是"报告按你说的做了"。
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"start_now/guide 的连接面还没装配（-adapter 没配，或 EnableTurn 没接线）", true)
	case errors.Is(err, ErrAdapterNotLaunchable):
		// send 反查到的 adapter_key 本机没配起法 ⇒ 同 501（这台机器上从来没有过这条能力）。
		writeError(w, http.StatusNotImplemented, "not_implemented", err.Error(), true)
	default:
		// Adapter 协议错误（delivery_mode_unsupported / provider_error / adapter_unresponsive …）
		// 经 probeErrorOf 按 §15.2 表映射；认不出的才落 serverError。两份映射表不许分叉（同 probe 那条口径）。
		if status, code, retryable, mapped := probeErrorOf(err); mapped {
			writeError(w, status, code, err.Error(), retryable)
			return
		}
		serverError(w, r, err)
	}
}

func listExecutions(w http.ResponseWriter, r *http.Request, c Config) {
	q := r.URL.Query()
	taskID := q.Get("task_id")
	if taskID == "" {
		// yaml 把 task_id 标成 required: true ⇒ 缺它就是请求不合法，不是"给你全部 execution"。
		writeError(w, http.StatusBadRequest, "validation_failed", "task_id 是必填的查询参数", false)
		return
	}
	// 这两个参数 yaml 里声明了，但 `ListByTask` 一次给全、没有可续的游标。
	// **拒掉而不是默默收下不用**：收了不用比不收更坏（调用方会以为自己限了 50 条）。
	for _, p := range []string{"cursor", "limit"} {
		if q.Get(p) != "" {
			writeError(w, http.StatusBadRequest, "validation_failed",
				"这条口今天不分页（清单一次给全），不接受 "+p+"；要分页得先给 executions 建读游标那一格", false)
			return
		}
	}
	items, err := c.Executions.ListByTask(r.Context(), taskID)
	if err != nil {
		serverError(w, r, err)
		return
	}
	views := make([]executionView, 0, len(items))
	for _, e := range items {
		views = append(views, executionViewOf(e))
	}
	// REQ-F-19：没有下一页只有空串这一种写法（键必在、不许 null、不许省略）。
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": ""})
}

// 编译期就钉住"读面由 *Repository 满足"：签名漂了这里先红，而不是等运行时的类型断言。
var _ ExecutionLister = (*execution.Repository)(nil)
