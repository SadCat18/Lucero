package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// ExecutionReader 是 executions 两条读面需要的读口，由 internal/execution 的 *Repository 直接满足
// （依赖方向合法：transport → 业务包）。T-229 说这一格「差的只是薄投影 + mux 注册」，这里就只做这两件事。
//
// 为什么是这一个接口而不是照 `TaskGetter` 那样**另开一个字段**：任务那格是「列表早就好了、单条读后接」，
// 两个半边分批落地，合并会让「没接」与「接了但缺方法」混成同一种错；executions 这两格是**同一轮接的、
// 同一个 *Repository 注入**，`c.Executions == nil` 表达的就是「读面整个没接」，而「接了但缺 Get」
// 在编译期就红（接口有方法）—— 合并没有可混淆的中间态。
type ExecutionReader interface {
	Get(ctx context.Context, id string) (execution.Execution, error)
	ListByTask(ctx context.Context, taskID string) ([]execution.Execution, error)
}

// executionView 的必填七字段来自 yaml 的 Execution.required；其余按域里有什么出什么 ——
// **域里有的（＝库里有列的）一个都不许少**，这是第八十批把 yaml `Execution`（core-v1.yaml:1151~1174）
// 逐键对完账之后的全集：`model_id`、`result_code`/`error_code`/`error_message`、
// `started_at`/`heartbeat_at`/`finished_at`、`created_at`/`updated_at` 全部进视图，
// 单条与列表共用这一个 struct（`executionViewOf`）⇒ 键集**逐字相等**，不会长出第二种口径。
//
// **三份快照**（model_snapshot / permission_snapshot / environment_snapshot）是 yaml `Execution`
// 里的 JsonMap 三格，域模型 `execution.Execution` 与 repo 的扫描都读了（§7.2：
// transport 不许另开 SELECT 补字段 ⇒ 只能在域里读）。
//
// **可空列出口成 null，不折算**：可空的四个字符串列（`model_id`、`result_code`、`error_code`、
// `error_message`）与三个时刻列（`started_at`/`heartbeat_at`/`finished_at`）在库里可空，
// 域里按 task 同一套约定（空串/0 = NULL，domain.go:5），出口经 `optStr`/`nullable` 还原成 null；
// `created_at`/`updated_at` 是 NOT NULL 毫秒时钟 ⇒ 直接 int64。
// **列的写者（第八十一批起）**：`result_code` / `error_code` / `started_at` / `finished_at` 四列
// 由 `execution.Service.advance` 的同一笔 Touch 落（`columnWrites`，internal/execution/service.go:216），
// 出口读到什么出什么；`error_message`（§6 payload 与 turn 帧都没有这个信号）、`heartbeat_at`
// （乙档已裁：无心跳信号源）与 `provider_execution_id`（CO-8 故意不写）今天仍恒是 NULL ⇒ 出口如实 null
// （schema 那边 `type: [string, "null"]` / `NullableTimestamp` 本来就是这么声明的），**不预置假值**：
// 谁开始写这些列，出口自己就跟上，这里一个字都不用改。
// `model_config_json` 是编排侧的另一格（`domain.go` 顶注明写），schema 里也没有它 ⇒ 不发明。
type executionView struct {
	ID                     string          `json:"id"`
	TaskID                 string          `json:"task_id"`
	Status                 string          `json:"status"`
	AttemptNo              int64           `json:"attempt_no"`
	TriggerType            string          `json:"trigger_type"`
	StateConfidence        string          `json:"state_confidence"`
	Version                int64           `json:"version"`
	AdapterInstanceID      string          `json:"adapter_instance_id"`
	SessionID              *string         `json:"session_id"`
	ModelID                *string         `json:"model_id"`
	PredecessorExecutionID *string         `json:"predecessor_execution_id"`
	ProviderExecutionID    *string         `json:"provider_execution_id"`
	ModelSnapshot          json.RawMessage `json:"model_snapshot"`
	PermissionSnapshot     json.RawMessage `json:"permission_snapshot"`
	EnvironmentSnapshot    json.RawMessage `json:"environment_snapshot"`
	ResultCode             *string         `json:"result_code"`
	ErrorCode              *string         `json:"error_code"`
	ErrorMessage           *string         `json:"error_message"`
	StartedAt              *int64          `json:"started_at"`
	HeartbeatAt            *int64          `json:"heartbeat_at"`
	FinishedAt             *int64          `json:"finished_at"`
	CreatedAt              int64           `json:"created_at"`
	UpdatedAt              int64           `json:"updated_at"`
}

// optStr 与 nullable 同一件事的字符串版：域里用 "" 表示「库里没这一列」，出口必须是 null 而不是 ""。
func optStr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// jsonMap 把库里那列 *_json 原文出口成**对象**（yaml 三格都是 `JsonMap`，type: object）。
// 两条兜底各自说一件事：
//   - 空串 ⇒ 这一格从没被写过 ⇒ 出 `{}`（正是列的 NOT NULL DEFAULT 值），不是 null、更不是 ""；
//   - 不是合法的 JSON 对象 ⇒ 那列被写坏了 ⇒ **报错上抛**（500 internal_error），
//     不折算成 `{}`：`writeJSON` 忽略编码错误，坏原文会**带着 200** 流到面板 ——
//     那比 500 坏得多（不静默、不折算，同 §12 那条红线）。
//
// 走 Unmarshal 判形状但**出口用原文**：数组/标量被判掉、键序与大整数不被我这一层重排。
func jsonMap(s string) (json.RawMessage, error) {
	if s == "" {
		return json.RawMessage(`{}`), nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return nil, fmt.Errorf("executions: 快照列不是 JSON 对象: %w", err)
	}
	return json.RawMessage(s), nil
}

// executionViewOf 出错只可能是某份快照列坏了（见 jsonMap）⇒ 调用方按 500 走，不许半份视图上墙。
func executionViewOf(e execution.Execution) (executionView, error) {
	model, err := jsonMap(e.ModelSnapshot)
	if err != nil {
		return executionView{}, err
	}
	perm, err := jsonMap(e.PermissionSnapshot)
	if err != nil {
		return executionView{}, err
	}
	env, err := jsonMap(e.EnvironmentSnapshot)
	if err != nil {
		return executionView{}, err
	}
	return executionView{
		ID: e.ID, TaskID: e.TaskID, Status: string(e.Status), AttemptNo: e.AttemptNo,
		TriggerType: string(e.TriggerType), StateConfidence: string(e.StateConfidence), Version: e.Version,
		AdapterInstanceID: e.AdapterInstanceID, SessionID: optStr(e.SessionID),
		ModelID:                optStr(e.ModelID),
		PredecessorExecutionID: optStr(e.PredecessorExecutionID), ProviderExecutionID: optStr(e.ProviderExecutionID),
		ModelSnapshot: model, PermissionSnapshot: perm, EnvironmentSnapshot: env,
		ResultCode: optStr(e.ResultCode), ErrorCode: optStr(e.ErrorCode), ErrorMessage: optStr(e.ErrorMessage),
		StartedAt: nullable(e.StartedAt), HeartbeatAt: nullable(e.HeartbeatAt), FinishedAt: nullable(e.FinishedAt),
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}, nil
}

// registerExecutionRoutes 拆两条 pattern：`/api/v1/executions`（精确，列表读面）与
// `/api/v1/executions/`（前缀，单条读 + send）。两条各自闸门，不互相绑架 ——
// 只注入列表不该让 send 误挂，只注入 send 也不该让列表消失（C28② 的口径）。
//
// 前缀这条**读面或写面任一在场就登记**（以前是 `ExecutionSend == nil || Store == nil` 整条不登记）：
// 单条读是 §3 登记过的路径，读面单独在场时若不认领，它会落到 catch-all 那句
// 「这条路径 Core 没登记」—— T-370② 点名过的假话。所以改成**登记，但各分支各判各的 nil**，
// 缺哪半就按哪半的话答 501，不互相冒充。
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
	if c.ExecutionSend == nil && c.Store == nil && c.Executions == nil {
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
			// 方法对了但写面没装配（只注入读面的那种 Config）⇒ C28② 501，不是 404 也不是 500。
			if c.ExecutionSend == nil || c.Store == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"POST /api/v1/executions/{id}/send 还没装配（ExecutionSend 与事务通道 Store 要同时注入）", true)
				return
			}
			sendExecution(w, r, c, id)
		case verb == "":
			// yaml `getExecution`（:496）只声明 GET ⇒ 方法不对是 405，与接没接线无关。
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/executions/{id} 只声明 GET；send 是 {id}/send", false)
				return
			}
			// 方法对了但读面没装配 ⇒ 按 C28② 回 501（声明过、只是没接），
			// **不是** 404（那会说成「没有这条 execution」）、也不是 catch-all 那句「没登记」。
			if c.Executions == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"GET /api/v1/executions/{id} 还没装配读通道（读面 Executions 没注入）", true)
				return
			}
			getExecution(w, r, c, id)
		case verb == "output":
			// T-370② 那一格：`GET /executions/{id}/output` 是 §3 登记过的路径（yaml :505 `listExecutionOutput`），
			// 以前它连 `splitExecutionItem` 都过不去，于是拿到一句"这条路径的形状我不认识"——那是假话，
			// 和 sessions.go 里 `messages` 那格同族（修法也照它：方法不对才 405，声明过但没装配是 501）。
			// 为什么今天只能 501：这张表的**写者还不存在** —— delta 级输出由入站帧落
			// `execution_output_chunks`，而那条消费者正是 T-370① 裁的那一格；先修文案不为它铺路。
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
					"/api/v1/executions/{id}/output 只声明 GET（增量游标 after_seq）", false)
				return
			}
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"GET /api/v1/executions/{id}/output 在契约③ §3 登记过，但读通道没装配：这一次执行的流式批次今天还没有写者（写它的是连接面入站帧那个消费者，见 T-370①），不拿空页冒充", true)
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
	// `/output` 也是 §3 登记过的形状（yaml :505），必须过得去 splitter —— 否则它对上的是
	// "形状我不认识"那句假话（T-370②）。仍然只认这两条子路径：认多了就是替别人决定归属。
	if sub, found := strings.CutSuffix(rest, "/output"); found {
		if sub == "" || strings.Contains(sub, "/") || strings.Contains(sub, ":") {
			return "", "", false
		}
		return sub, "output", true
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
	Deliver(ctx context.Context, res execution.SendReservation) (execution.SendResult, execution.TurnFinish, error)
	// FinishFromTurn 是甲（T-370①）的 (a)：把 `Deliver` 收回来的那帧 `turn/finished` 落成 execution
	// 终态 + 同笔的 task 投影。它在**第二笔短事务里**调用，与密封同一个 tx ——
	// "投递 → 第二笔落结果"那句裁语里，落的就是这个。
	FinishFromTurn(ctx context.Context, tx *db.Tx, id, resultCode string,
		cmd execution.Command) (execution.Result, bool, error)
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
	out, fin, err := c.ExecutionSend.Deliver(r.Context(), res)
	if err != nil {
		writeSendError(w, r, err)
		return
	}
	// 收到了收尾帧、但那个码没人裁过 ⇒ 这一次**只投递、不写终态**（边界一），
	// 投递本身是成功的，答复照旧 202，喊那一句在 sendpipe 的 logf 里。
	// 这里用域导出的谓词问"可不可映射"，而不是自己比字符串 —— 表只有一份。
	writeTerminal := fin.Seen && execution.MappedTurnResultCode(fin.ResultCode)

	// ── 第二笔：落投递结果（有可映射的收尾帧时先落终态 + task 投影，再密封，同一个 tx）。
	//    0 行（并发里我输了）由 sealIdempotency 回 errReplay ⇒ 409，不是静默 202 —— 第 25 条③ 要的就是这声喊。
	if err := c.Store.Write(r.Context(), func(tx *db.Tx) error {
		if writeTerminal {
			// 三个事件 id 一起铸：execution 终态 + task 投影 + （仅当投影落定 AGENT_COMPLETED 时）
			// 契约④-c 链尾那条 task_waiting_acceptance。三条都是 event_journal 的 UNIQUE 身份，
			// 域不自己生成（§3 UUIDv7），所以缺哪个都得在这儿补上。
			if _, _, e := c.ExecutionSend.FinishFromTurn(r.Context(), tx, id, fin.ResultCode, execution.Command{
				EventID: newUUIDv7(), TaskEventID: newUUIDv7(), TaskWaitingAcceptanceEventID: newUUIDv7(),
				PrincipalID: principal, CorrelationID: correlation,
			}); e != nil {
				// 投影被拒一类的错 ⇒ 整笔回滚：既不写终态也不密封，同键下一发还能续跑。
				// 为什么不敢"只跳过终态、照样密封"：那会让库里出现"投递成功但没有任何收尾记录"的象，
				// 而密封之后的重放**永远不再补**这一格。
				return e
			}
		}
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
		v, err := executionViewOf(e)
		if err != nil {
			// 某一行的快照列坏了：整页不发（半份数据的 200 比 500 隐蔽）。
			serverError(w, r, err)
			return
		}
		views = append(views, v)
	}
	// REQ-F-19：没有下一页只有空串这一种写法（键必在、不许 null、不许省略）。
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": ""})
}

// getExecution 是 yaml `getExecution`（:496，GET /api/v1/executions/{id}）那一格：
// 读面已在 registerExecutionRoutes 里判过 nil，到这里只剩三岔 ——
// 没这条行 ⇒ 404 not_found（与 getTask 同一族，不是 405、不是 501）；快照列坏了 ⇒ 500；否则 200 + 与列表同形的视图。
// 「与列表同形」不是省事：两条口出两套键，面板就得写两套读法。
func getExecution(w http.ResponseWriter, r *http.Request, c Config, id string) {
	e, err := c.Executions.Get(r.Context(), id)
	if err != nil {
		// `Repository.Get` 把 sql.ErrNoRows 包了一层 ⇒ 必须 errors.Is，不能 ==（同 getTask）。
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, db.ErrAggregateNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "没有这条 Execution", false)
			return
		}
		serverError(w, r, err)
		return
	}
	v, err := executionViewOf(e)
	if err != nil {
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// 编译期就钉住"读面由 *Repository 满足"：签名漂了这里先红，而不是等运行时的类型断言。
var _ ExecutionReader = (*execution.Repository)(nil)
