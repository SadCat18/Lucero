package execution

// `POST /api/v1/executions/{id}/send` 的 execution 半边：yaml :544 的事件序列
// execution_started → task_running → message_recorded 在同一事务里走完。
//
// 依赖方向：execution → task（投影）与 execution → session（落消息）都无环；
// 反向注入靠 consumer-side 接口（EnableSend），NewService() 签名不变。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// ErrSessionNotLinked：execution.session_id 为 NULL（queue 时 links 没有、或 session 被 SET NULL），
// 或 session 行的 remote_session_id 为空 —— 两种都是"send 没有落点"。
// §15.2 绑 404 `session_not_found` —— send 没有会话就没有落点，这不是"执行不存在"（那是 404 not_found）。
var ErrSessionNotLinked = errors.New("execution: execution has no session_id")

// ErrTurnNotWired：delivery=start_now/guide 要真开一轮/中转插话，连接面却没装配
// （EnableTurn 没调过）。C28②：能力没接线 ⇒ 501 not_implemented，不是静默降级成 queue
// （那正是 yaml :554 禁的"报告按你说的做了"）。
var ErrTurnNotWired = errors.New("execution: send 的连接面未装配（EnableTurn 未调用）")

// Delivery 三态与 yaml DeliveryMode 逐字一致（:853）。SendResult.delivery 是**实际生效的态**：
// start_now/guide 接上了连接面就回请求的那个态；queue 回 queue；accepted=false 时保持请求态
// （不改写成 queue —— 改写才是 §5 静默降级）。
const (
	DeliveryStartNow = "start_now"
	DeliveryGuide    = "guide"
	DeliveryQueue    = "queue"
)

// TaskProjector 是 §5.3 投影的最小面，由 *task.Service.ApplyExecutionEvent 直接满足。
type TaskProjector interface {
	ApplyExecutionEvent(ctx context.Context, tx *db.Tx, taskID, edge string,
		handoffReady, approved bool, cmd task.Command) (task.Result, error)
}

// MessageRecorder 是 session 落消息的最小面，由 *session.Service.RecordMessage 直接满足。
type MessageRecorder interface {
	RecordMessage(ctx context.Context, tx session.Writer, sessionID string,
		cmd session.RecordMessageCommand) (session.Message, error)
}

// TurnCall 是一次 start_now/guide 连接调用的全部外部输入。
type TurnCall struct {
	ExecutionID    string
	SessionID      string
	Text           string
	Delivery       string
	IdempotencyKey string
}

// TurnReceipt 是连接面的回执：只回答"接没接"与"没接时的理由"。
// 刻意不带 provider_execution_id —— CO-8 禁止拿 send 回执当 started 证据
// （证据要走 turn/claimed + turn/dispatched 那一对）。
type TurnReceipt struct {
	Accepted bool
	Reason   string // 只进 sendpipe logf；yaml SendResult 没有 reason 出口，不发明
}

// TurnCaller 是 execution → 连接面的 consumer-side 接口，由 cmd/core 的 *sendPipe 满足。
// queue 不走它（drain 推进器后置），只有 start_now/guide 会调。
type TurnCaller interface {
	CallSendTurn(ctx context.Context, call TurnCall) (TurnReceipt, error)
}

// SendCommand 是一次 send 的全部外部输入。事件 ID 与 message_id 由调用方预生成
// （§3 要 UUIDv7；幂等重试要复用同一个才谈得上"只落一条"）。
type SendCommand struct {
	Text        string
	Delivery    string
	Attachments []string

	// IdempotencyKey 从 HTTP 头透传给 adapter 做重试幂等（§4 的 Idempotency-Key 必填）。
	IdempotencyKey string

	// 三个事件 ID：CREATED 起步链用 StartingEventID + StartedEventID；
	// STARTING 起步只用 StartedEventID。TaskEventID / MessageEventID 各对应一行事件。
	StartingEventID string
	StartedEventID  string
	TaskEventID     string
	MessageEventID  string
	MessageID       string

	PrincipalID   string
	CorrelationID string
	CausationID   string

	// 三个被拒投影的预生成 id（task.Command.Notification/RejEvent/RejAudit）：
	// 注入了 RejectionReporter 时 Report 要写 §5.4 两条记录，缺 id 会变成 Report 失败（500），
	// 而那本该是 409 illegal_transition。handler 与上面五个 id 一起 newUUIDv7()。
	RejNotificationID string
	RejEventID        string
	RejAuditID        string
}

// SendResult 是 yaml SendResult 的域形状（:1186）。
type SendResult struct {
	Delivery    string
	Accepted    bool
	MessageID   string
	ExecutionID string
}

// SendReservation 是第一笔（意图）落库之后、投递之前的凭证，也是**续跑的入场券**：
// 同一 Idempotency-Key 若在密封前中断，重试拿这一份再打一次连接面，而不是重写第二行消息（第 25 条①）。
// 它被 json 化存进 `idempotency_keys.response_json`（state=IN_PROGRESS 那一格 ⇒ 这一列此刻是"身份"），
// 所以字段只能是"再打一次"所需的最小集，别顺手塞进只有第一次才有的东西。
type SendReservation struct {
	ExecutionID    string `json:"execution_id"`
	SessionID      string `json:"session_id"`
	MessageID      string `json:"message_id"`
	Text           string `json:"text"`
	Delivery       string `json:"delivery"`
	IdempotencyKey string `json:"idempotency_key"`
	NeedsTurn      bool   `json:"needs_turn"`
	Accepted       bool   `json:"accepted"`
}

// Result 把凭证折算成 yaml `SendResult` 的形状：delivery 保持请求态（不改写成 queue），
// accepted 在 queue 那一支是"已落库待送达"=true，在 start_now/guide 由 Deliver 覆盖成回执里的值。
func (r SendReservation) Result() SendResult {
	return SendResult{Delivery: r.Delivery, Accepted: r.Accepted,
		MessageID: r.MessageID, ExecutionID: r.ExecutionID}
}

// Reserve 是**第一笔短事务**里做的那半：校验 → 状态边 → task 投影 → 落消息，返回投递凭证。
// 它不在里面打网络：写面只有 `db.go:48` 那一条串行连接，起子进程/握手/等回执一旦关进写事务，
// 一次 send 就能停掉全 Core 的写（REQ-S-1，09-25 裁）。投递是 `Deliver`，跑在事务之外。
//
// 允许的起始态：
//   - CREATED：全链 CREATED→STARTING(execution_starting)→RUNNING(execution_started) + task_running + message_recorded
//   - STARTING：STARTING→RUNNING + 同上两格
//   - RUNNING：只补 message_recorded（已在跑，不重发 started）
//
// 终态与其它中间态 → ErrIllegalTransition（409）。execution 没挂 session → ErrSessionNotLinked（404）。
//
// **判定顺序有意如此**：validate(400) → Get(404) → session linked(404) → 状态前置守卫(409)
// → turn 未接线(501) → projector/recorder 未装配(500) → 全部 DB 写。
// 409 先于 501 是硬要求：状态机拒绝是"这次请求在业务上不合法"，比"能力没接线"更该先答。
//
// delivery=queue 不连 adapter（drain 推进器后置），凭证里 NeedsTurn=false、Accepted=true（已落库待送达）。
// start_now/guide：连接面未装配 ⇒ ErrTurnNotWired（501，不静默降级 —— yaml :554 禁的正是那个）。
func (s *Service) Reserve(ctx context.Context, tx *db.Tx, id string, cmd SendCommand) (SendReservation, error) {
	if err := validateSend(cmd); err != nil {
		return SendReservation{}, err
	}

	cur, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return SendReservation{}, err // sql.ErrNoRows → transport 404 not_found
	}
	if cur.SessionID == "" {
		return SendReservation{}, ErrSessionNotLinked
	}

	// 状态前置守卫：只放行 CREATED/STARTING/RUNNING，其余在碰任何装配位之前答 409。
	switch cur.Status {
	case Created, Starting, Running:
	default:
		return SendReservation{}, fmt.Errorf("execution: send 在 %s 不合法: %w", cur.Status, ErrIllegalTransition)
	}

	// turn 未接线只拦 start_now/guide：queue 本来就不走连接面。
	// 放在 409 之后：状态机拒绝比"能力没接线"更该先答。
	if cmd.Delivery != DeliveryQueue && s.turn == nil {
		return SendReservation{}, ErrTurnNotWired
	}
	if s.projector == nil || s.recorder == nil {
		return SendReservation{}, fmt.Errorf("execution: Send 未 EnableSend（装配缺口）")
	}

	base := Command{
		PrincipalID: cmd.PrincipalID, CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
	}

	// 起步子链：只在 CREATED / STARTING 时跑。RUNNING 已经走过了。
	// 前置守卫已保证 default 不可达（cur.Status ∈ 三态）。
	switch cur.Status {
	case Created:
		start := base
		start.EventID = cmd.StartingEventID
		if _, err := s.advance(ctx, tx, id, Starting, start); err != nil {
			return SendReservation{}, err
		}
		fallthrough
	case Starting:
		started := base
		started.EventID = cmd.StartedEventID
		// provider_execution_id 留空（omitempty 键不出现）、快照空串：
		// send 回执不是 started 证据（CO-8），证据走 turn/claimed + turn/dispatched。
		if _, err := s.advance(ctx, tx, id, Running, started); err != nil {
			return SendReservation{}, err
		}
		// §5.3 投影：execution_started → task_running。EdgeStarted 只从 PREPARING 驱动。
		tres, err := s.projector.ApplyExecutionEvent(ctx, tx, cur.TaskID, task.EdgeStarted,
			false, false, task.Command{
				EventID: cmd.TaskEventID, ExecutionID: id,
				Notification: cmd.RejNotificationID, RejEvent: cmd.RejEventID, RejAudit: cmd.RejAuditID,
				PrincipalID: cmd.PrincipalID, CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
			})
		if err != nil {
			// 没注入 reporter 的拒绝：能识别成状态机拒绝（409），不是 500 装配错。
			// Report 写失败 / 库错误等仍原样上抛（那才是 500）。
			if errors.Is(err, task.ErrProjectionRejected) {
				return SendReservation{}, fmt.Errorf("execution: send 的 task_running 投影被拒: %w", ErrIllegalTransition)
			}
			return SendReservation{}, err
		}
		if tres.Rejection.Reason != "" || !tres.Advanced {
			// 注入了 reporter：§5.4 两条记录已在同事务写下，但 send 的事件序列断了
			// （task 不在 PREPARING）⇒ 整笔回滚 + 409。回滚连那两条一起丢是有意的：
			// send 是同步 API，409 本身是对外答案；后台事件投影那条路的 §5.4 持久化不受影响。
			return SendReservation{}, fmt.Errorf("execution: send 的 task_running 投影被拒（%s）: %w",
				tres.Rejection.Reason, ErrIllegalTransition)
		}
	case Running:
		// 已在跑：不重发 execution_started / task_running，只补 message_recorded。
	}

	if _, err := s.recorder.RecordMessage(ctx, tx, cur.SessionID, session.RecordMessageCommand{
		ID: cmd.MessageID, EventID: cmd.MessageEventID, Text: cmd.Text, Attachments: cmd.Attachments,
		PrincipalID: cmd.PrincipalID, CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
	}); err != nil {
		return SendReservation{}, err
	}

	// queue：不连 adapter，已落库待送达 ⇒ 凭证里就把答案给全（NeedsTurn=false）。
	if cmd.Delivery == DeliveryQueue {
		return SendReservation{ExecutionID: id, SessionID: cur.SessionID, MessageID: cmd.MessageID,
			Text: cmd.Text, Delivery: DeliveryQueue, IdempotencyKey: cmd.IdempotencyKey, Accepted: true}, nil
	}
	// start_now/guide：意图已经落库，投递留给 Deliver —— 那一跳跑在这笔事务**之外**。
	// accepted 在这里还是 false（占位），真值只由 Deliver 从回执里填。
	return SendReservation{ExecutionID: id, SessionID: cur.SessionID, MessageID: cmd.MessageID,
		Text: cmd.Text, Delivery: cmd.Delivery, IdempotencyKey: cmd.IdempotencyKey, NeedsTurn: true}, nil
}

// Deliver 打连接面那一跳，**必须在任何事务之外调用**（REQ-S-1 的不变量就在这一句落地）：
// 写面只有 `platform/db` 那一条串行连接（`db.go:48`），起子进程 / 协议握手 / 等对端回执
// 一旦关在写事务里，一次 send 就能停掉整个 Core 的写（09-25 那条红）。
//
// 失败 ⇒ 原样上抛：调用者**不密封**、execution 留在 RUNNING 不动（契约①「REQ-S-1 两段式的幂等语义」那一格 ④=丙），
// HTTP 答案走已绑 503 的 `adapter_unresponsive`；v0.1 同步路径上不新造 `UNKNOWN` 边。
// 不密封 ⇒ 同一 `Idempotency-Key` 的下一次请求读到的仍是"未密封 + 第一笔身份"⇒ 续跑。
//
// `res.NeedsTurn=false`（queue）时不发帧，直接回第一笔折算出的结果。
func (s *Service) Deliver(ctx context.Context, res SendReservation) (SendResult, error) {
	if !res.NeedsTurn {
		return res.Result(), nil
	}
	// 这里的 nil 检查与 Reserve 里那一处**不是重复**：Reserve 挡的是"首攻时就没装配"，
	// 这一处挡的是"凭证是上一趟写的、这一趟进程重新起来后连接面没接上"（续跑路径）。
	if s.turn == nil {
		return SendResult{}, ErrTurnNotWired
	}
	rec, err := s.turn.CallSendTurn(ctx, TurnCall{
		ExecutionID: res.ExecutionID, SessionID: res.SessionID, Text: res.Text,
		Delivery: res.Delivery, IdempotencyKey: res.IdempotencyKey,
	})
	if err != nil {
		return SendResult{}, err
	}
	// accepted=false 仍算跑通：delivery 保持请求态 —— 改写成 queue 才是 §5 禁的静默降级。
	// reason 只进 sendpipe 的 logf，yaml `SendResult` 没有这个出口，不发明。
	out := res.Result()
	out.Accepted = rec.Accepted
	return out, nil
}

// validateSend 在碰库之前拦住：text 必须非空、delivery ∈ 三枚举、四个事件/消息 id 必须在场。
// 缺 id 意味着这一次写没有可复用的身份（幂等重试会发第二条事件）。
// IdempotencyKey 只对 start_now/guide 要求：它要透给 adapter（§4 必填），
// 空键会让 adapter 侧的重试幂等失效；queue 不走连接面，HTTP 头仍由 handler 在更外层挡 403。
func validateSend(cmd SendCommand) error {
	switch {
	case strings.TrimSpace(cmd.Text) == "":
		return fmt.Errorf("%w: text 不能为空", ErrInvalidExecution)
	case cmd.Delivery != DeliveryStartNow && cmd.Delivery != DeliveryGuide && cmd.Delivery != DeliveryQueue:
		return fmt.Errorf("%w: delivery %q 不在 [start_now guide queue]", ErrInvalidExecution, cmd.Delivery)
	case cmd.Delivery != DeliveryQueue && cmd.IdempotencyKey == "":
		return fmt.Errorf("%w: %s 要透 idempotency_key 给 adapter，不能空", ErrInvalidExecution, cmd.Delivery)
	case cmd.MessageID == "":
		return fmt.Errorf("%w: 缺 message_id", ErrInvalidExecution)
	case cmd.MessageEventID == "":
		return fmt.Errorf("%w: 缺 message_event_id", ErrInvalidExecution)
	case cmd.TaskEventID == "":
		return fmt.Errorf("%w: 缺 task_event_id", ErrInvalidExecution)
	case cmd.StartedEventID == "":
		return fmt.Errorf("%w: 缺 started_event_id", ErrInvalidExecution)
	case cmd.StartingEventID == "":
		return fmt.Errorf("%w: 缺 starting_event_id", ErrInvalidExecution)
	}
	return nil
}
