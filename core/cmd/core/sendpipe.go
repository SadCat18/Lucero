package main

// sendpipe 是 `POST /api/v1/executions/{id}/send` 的连接半边：execution.Service.Send 在
// 全部 DB 写之后、seal 之前调 CallSendTurn，这里把一次 start_now/guide 真的送到 Adapter。
//
// 三件事决定它长这样（与 discoverassembly 同一批口径）：
//   - **一次 send 现起一条连接**：v0.1 不做连接池（C42 未裁），连完就关。
//     每次多起一个子进程是已知代价，"连接复用后置"记在这条注释里。
//   - **必须走 CallTurn 而不是 Call**：§6 规则 5 按 turn_ref 归因证据帧，
//     CallTurn 在出帧前把 request id 绑到 ExecutionID 上；用 Call 就没有这个绑定，
//     turn/claimed 那一对（CO-8）会因归不上而被丢掉。
//   - **错误归一成 *adapter.Error**：transport 的 switch 只收哨兵与 adapter.Error
//     （契约③ §7）。Connect/Initialize/LostError/ctx deadline 等非 adapter 错误
//     一律包成 ADAPTER_UNRESPONSIVE ⇒ 503 —— 那正是"这个 Adapter 病了"的处置。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/session"
)

// sendPipe 满足 execution.TurnCaller。
type sendPipe struct {
	read   *sql.DB
	launch *launcher
	byKey  map[string]adapterSpec
	logf   func(format string, args ...any)
}

// CallSendTurn 反查落点 → 起连接 → 握手 → CallTurn(session/send) → 校验回执。
// 返回 TurnReceipt{Accepted, Reason}；Reason 只给调用方 logf，yaml 没有出口。
func (p *sendPipe) CallSendTurn(ctx context.Context, call execution.TurnCall) (execution.TurnReceipt, error) {
	// 反查 adapter_key 与远端会话 id。session 没行 ⇒ 包成 session.ErrSessionNotFound（transport ⇒ 404）。
	// remote_session_id 为空 ⇒ ErrSessionNotLinked 家族（404 session_not_found：send 没有落点）。
	var remoteSessionID, adapterKey string
	err := p.read.QueryRowContext(ctx,
		`SELECT s.remote_session_id, ai.adapter_key
		 FROM sessions s JOIN adapter_instances ai ON ai.id = s.adapter_instance_id
		 WHERE s.id = ?`, call.SessionID).Scan(&remoteSessionID, &adapterKey)
	if errors.Is(err, sql.ErrNoRows) {
		return execution.TurnReceipt{}, session.ErrSessionNotFound
	}
	if err != nil {
		return execution.TurnReceipt{}, fmt.Errorf("sendpipe: 反查 session %s: %w", call.SessionID, err)
	}
	if remoteSessionID == "" {
		return execution.TurnReceipt{}, execution.ErrSessionNotLinked
	}
	if _, ok := p.byKey[adapterKey]; !ok {
		return execution.TurnReceipt{}, fmt.Errorf("%w: %s（要 -adapter %s=<可执行路径>）",
			httpapi.ErrAdapterNotLaunchable, adapterKey, adapterKey)
	}

	// 与 discover 同一条：连接这条路必须有期限，否则一个挂住的 Adapter 拖住 handler 线程。
	cctx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, probeAttemptDeadline)
		defer cancel()
	}

	conn, err := p.launch.supervisorFor(adapterKey).Connect(cctx, coreProtocolVersion())
	if err != nil {
		return execution.TurnReceipt{}, unresponsive("起连接", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			p.logf("send %s: 关闭连接失败: %v", call.ExecutionID, err)
		}
	}()
	if _, err := conn.Initialize(cctx); err != nil {
		return execution.TurnReceipt{}, unresponsive("握手", err)
	}

	// QueueStrategy 不发明：yaml SendResult 的 queue 走本地队列，不经过这条连接面；
	// start_now/guide 没有 strategy 字段（CO-7 的 strategy 只对 queue 有意义）。
	// attachments 不进协议：SendParams 没有该字段（§4 session/send 的参数表就这些），
	// 附件只在本地 content_json 里（RecordMessage 已落）。
	params := adapter.SendParams{
		RemoteSessionID: remoteSessionID, Text: call.Text,
		ExecutionID: call.ExecutionID, Delivery: call.Delivery,
		IdempotencyKey: call.IdempotencyKey,
	}
	f, err := conn.CallTurn(cctx, call.ExecutionID, "session/send", params)
	if err != nil {
		// 非 *adapter.Error（连接丢失 / ctx 超时 / 写失败）⇒ 归一成 503。
		return execution.TurnReceipt{}, unresponsive("session/send", err)
	}
	if f.Error != nil {
		// Adapter 明确拒了（delivery_mode_unsupported / session_not_found / …）：
		// 原样上抛，transport 的 probeErrorOf 按 §15.2 表映射。
		return execution.TurnReceipt{}, f.Error
	}
	var res adapter.SendResult
	if err := json.Unmarshal(f.Result, &res); err != nil {
		return execution.TurnReceipt{}, fmt.Errorf("%w: session/send 回执不是 §5 的形状: %v",
			&adapter.Error{Code: adapter.ErrInternal, Message: "send result decode"}, err)
	}
	if err := res.Validate(call.Delivery); err != nil {
		return execution.TurnReceipt{}, err
	}
	if !res.Accepted && p.logf != nil {
		// accepted=false 的 reason 只有 logf 这一个出口（yaml SendResult 没有 reason 键）。
		p.logf("send %s: adapter 拒了 delivery=%s: %s", call.ExecutionID, call.Delivery, res.Reason)
	}
	return execution.TurnReceipt{Accepted: res.Accepted, Reason: res.Reason}, nil
}

// unresponsive 把一切非 adapter 错误归一成 ADAPTER_UNRESPONSIVE ⇒ 503。
// 已经是 *adapter.Error 的原样透传（不重复包装，否则 §15.2 的映射会丢）。
func unresponsive(what string, err error) error {
	var e *adapter.Error
	if errors.As(err, &e) {
		return err
	}
	return &adapter.Error{Code: adapter.ErrAdapterUnresponsive,
		Message: fmt.Sprintf("send %s: %v", what, err)}
}
