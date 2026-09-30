package main

// sendpipe 是 `POST /api/v1/executions/{id}/send` 的连接半边：REQ-S-1 之后它由 **`execution.Service.Deliver`
// 在写事务之外**调用（旧描述"在全部 DB 写之后、seal 之前调"是修之前的形状 —— 那一格正是把一次 send 变成
// 全 Core 的写停摆的地方，09-25 裁、09-27 落；见 `api/http/executions.go` 的三拍注释）。
//
// 三件事决定它长这样（与 discoverassembly 同一批口径）：
//   - **连接是常驻的，不归这一腿关**：REQ-TURN-1 裁「Ⅰ」（契约① 第 25 条 ④-b，09-28 21:4x）之后，
//     每个 adapter key 一条常驻连接由 `turnHub` 持有（`turnpump.go`），C42 随裁放开。
//     这里既不 `Connect` 也不 `Close`：拿一条、用完就还回泵手里。旧注释那句"一次 send 现起一条连接、
//     连完就关"是修之前的形状，它和"回执之后收尾帧没人收"是同一件事的两面。
//   - **必须走 CallTurn 而不是 Call**：§6 规则 5 按 turn_ref 归因证据帧，
//     CallTurn 在出帧前把 request id 绑到 ExecutionID 上；用 Call 就没有这个绑定，
//     turn/claimed 那一对（CO-8）会因归不上而被丢掉。
//   - **错误归一成 *adapter.Error**：transport 的 switch 只收哨兵与 adapter.Error
//     （契约③ §7）。Connect/Initialize/LostError/ctx deadline 等非 adapter 错误
//     一律包成 ADAPTER_UNRESPONSIVE ⇒ 503 —— 那正是"这个 Adapter 病了"的处置。
//
// 这里**不再收收尾帧**（③ 给的字：`sendreap.go` 那套"只收不写 + 三态分辨"整搬进泵、回执后那条收割腿删除）。
// 上一形状（甲，T-370①）在 `Close` 之前非阻塞扫一遍缓冲，因为那条连接马上就不在了；
// 现在帧要么已经到（泵早一步收走）、要么晚一点到（泵等着），两种都不需要 send 这一腿自己扫。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/session"
)

// sendAttemptDeadline 是 send 自己的请求腿期限（T-345③ 给的字：不再借 `probeAttemptDeadline`，
// 那是"一次探针尝试"的语义，与"用户等一次投递"不是一件事）。两句必须一起留着：
//   - **这个 10 秒是从 probe 借来的占位量级，不是从对端行为量出来的**；真 Adapter 接入后要按实测换数
//     （主控记在 Wave-3 之后，不排当前闸门）；换数时**注释与用例一起改**，不许只改数。
//   - **REQ-S-1 之后期限只管"这条请求腿愿意等多久"，它不再能停住全 Core 的写**：
//     停摆的病根是"投递待在写事务里"，那个已经由两笔短事务修掉（反例 M3 证的是它，不是这个数）。
//
// 这一格的存在理由（T-361④ 让引的那一款，按章节名引、别按编号引）：
// 契约①「REQ-S-1 裁定：写事务里不许有网络往返、起子进程」那一条新增的**第 ⑧ 款「调用方义务：
// 连接面上每一次调用必须自带期限」**，实测出处是 `adapter` 的 CO-53 —— 连接面**不检测也不自救**
// 不带期限的 ctx（孩子不再读 stdin 时一次 `request` 5 秒不返回、`writeMu` 被永久占住，只有 `Close` 解得开）。
// 所以下面那句"外层没期限才补"不是风格选择：**这里的 `cctx` 必须无条件带期限**，缺了就是一条能把连接楔死的腿。
const sendAttemptDeadline = 10 * time.Second

// sendPipe 满足 execution.TurnCaller。
type sendPipe struct {
	read  *sql.DB
	hub   *turnHub
	byKey map[string]adapterSpec
	logf  func(format string, args ...any)
}

// CallSendTurn 反查落点 → 取常驻连接 → CallTurn(session/send) → 校验回执。
// 返回 TurnReceipt{Accepted, Reason}；Reason 只给调用方 logf，yaml 没有出口。
// 握手与起孩子都在 `turnHub.connection` 里（那里用的是同一个 `cctx`：期限管这条请求腿愿意等多久，
// 管不到连接活多久 —— 那是「Ⅰ」之后的分工）。
func (p *sendPipe) CallSendTurn(ctx context.Context, call execution.TurnCall) (execution.TurnReceipt, error) {
	// 反查 adapter_key 与远端会话 id。session 没行 ⇒ 包成 session.ErrSessionNotFound（transport ⇒ 404）。
	// remote_session_id 为空 ⇒ ErrSessionNotLinked 家族（404 session_not_found：send 没有落点）。
	// 这一列可空，而库里"空"有两份现实：`NULL` 与 `''`。绑成普通 string 会让 NULL 变成 Scan 错误
	// ⇒ 一路冒到 transport 的 default 支就是 500，而它对调用方来说和"没落点"是同一件事（CO-44 同族：
	// 平台层不替你分，调用方要么合并处置、要么明确喊出来，不许让它变成 500）。
	var remoteSessionID, adapterKey sql.NullString
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
	if !remoteSessionID.Valid || remoteSessionID.String == "" {
		return execution.TurnReceipt{}, execution.ErrSessionNotLinked
	}
	if _, ok := p.byKey[adapterKey.String]; !ok {
		return execution.TurnReceipt{}, fmt.Errorf("%w: %s（要 -adapter %s=<可执行路径>）",
			httpapi.ErrAdapterNotLaunchable, adapterKey.String, adapterKey.String)
	}

	// send 这一跳必须有期限，否则一个挂住的 Adapter 拖住 handler 线程。
	// 量级是**占位**：见上面 `sendAttemptDeadline` 的两句注（借来的数 + 它现在只管请求腿）。
	cctx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, sendAttemptDeadline)
		defer cancel()
	}

	// 半装配的装配体（`hub` 为 nil）走域里那一条 `ErrTurnNotWired` ⇒ 501，与 `Deliver` 里
	// `s.turn == nil` 那一处同一个码：这里既没有连接面也没有泵，跟"这个 Adapter 病了"（503）是两件事。
	if p.hub == nil {
		return execution.TurnReceipt{}, execution.ErrTurnNotWired
	}
	conn, err := p.hub.connection(cctx, adapterKey.String)
	if err != nil {
		return execution.TurnReceipt{}, unresponsive("起连接", err)
	}

	// QueueStrategy 不发明：yaml SendResult 的 queue 走本地队列，不经过这条连接面；
	// start_now/guide 没有 strategy 字段（CO-7 的 strategy 只对 queue 有意义）。
	// attachments 不进协议：SendParams 没有该字段（§4 session/send 的参数表就这些），
	// 附件只在本地 content_json 里（RecordMessage 已落）。
	params := adapter.SendParams{
		RemoteSessionID: remoteSessionID.String, Text: call.Text,
		ExecutionID: call.ExecutionID, Delivery: call.Delivery,
		IdempotencyKey: call.IdempotencyKey,
	}
	// 在飞名单登记必须早于出帧（崩溃那一支的名单来源，见 `turnHub.watch`）。
	p.hub.watch(adapterKey.String, call.ExecutionID)
	f, err := conn.CallTurn(cctx, call.ExecutionID, "session/send", params)
	if err != nil {
		// 对端没了 ⇒ 这条常驻连接不再可用：退役它，下一次 send 重连（④ 那句"死亡即重连"）。
		// 不在这里重试 —— §7.2 把 v0.1 钉在"一次调用一次尝试"上，重连是下一发的事。
		if connectionLost(err) {
			p.hub.retire(adapterKey.String, "session/send 里对端没了")
		}
		// 非 *adapter.Error（连接丢失 / ctx 超时 / 写失败）⇒ 归一成 503。
		return execution.TurnReceipt{}, unresponsive("session/send", err)
	}
	if f.Error != nil {
		// Adapter 明确拒了（delivery_mode_unsupported / session_not_found / …）：
		// 原样上抛，transport 的 probeErrorOf 按 §15.2 表映射。这一轮没开 ⇒ 从在飞名单里注销。
		p.hub.dropFlight(call.ExecutionID)
		return execution.TurnReceipt{}, f.Error
	}
	var res adapter.SendResult
	if err := json.Unmarshal(f.Result, &res); err != nil {
		p.hub.dropFlight(call.ExecutionID)
		return execution.TurnReceipt{}, fmt.Errorf("%w: session/send 回执不是 §5 的形状: %v",
			&adapter.Error{Code: adapter.ErrInternal, Message: "send result decode"}, err)
	}
	if err := res.Validate(call.Delivery); err != nil {
		p.hub.dropFlight(call.ExecutionID)
		return execution.TurnReceipt{}, err
	}
	if !res.Accepted {
		// 没接 ⇒ 也没有轮次在飞（挂住/超时不走这一支，见 watch 的注释）。
		p.hub.dropFlight(call.ExecutionID)
	}
	if !res.Accepted && p.logf != nil {
		// accepted=false 的 reason 只有 logf 这一个出口（yaml SendResult 没有 reason 键）。
		p.logf("send %s: adapter 拒了 delivery=%s: %s", call.ExecutionID, call.Delivery, res.Reason)
	}
	// 回执就是回执：收尾帧不在这里收（③ 删掉的那条收割腿），也不随返回关闭连接（「Ⅰ」）。
	// 返回之后 `turn/finished` 什么时候到、落到哪，是 `turnpump.go` 那条泵的事。
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
