package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
)

// 这里补上 T84-7b 等的那半格：`process.Recorder` 在装配侧的实现（契约③ §7.2 第一次把四方法形状写进契约）。
//
// 今天四格里只有一格能真写（RecordDrop），生命周期那三格只能出声（依据见 RecordStart 的注释与 T-168）。
//   - **RecordDrop** 落一行审计，走 `audit` 自己的出口，`event_journal` 零新增（§7.2:310~311 断的就是这一对；
//     这里出现 `Tx.Append("audit_entries"` 字面量即 W13-2 红）。
//
// 生命周期那三格今天**一格都不写**：§1.1 第 12 条把 `process_instances` 列进"故意不发事件的可变表"，
// 但那是"**除** `process_started`/`process_exited`/`process_crashed` 三个外"—— 排除，不是包含（T-168 收回 T-162①，
// 我 01:0x 把它读反过一次）。这三件是要发事件的事实，而 `Append` 与 `cmd/core/main.go` 里
// `tx.Append("principals", …)` 那条通道一样**不写事件**（判据 U13-4），拿它建行 = 行写了、事件静默丢掉，
// 正是 §1.1 第 13 条 00:41 裁死的形状；退出/崩溃那两格还要**改**同一行，而第 13 条裁定它不进 `narrowTables`。
// 真正的通道挂在 **REQ-DB-2** 上（"无版本、但要发事件"的表没有写路径，与 `core_ready` 同源）—— 主控明写"不算 backend 欠账"。
// 那三格是 void 的（T-62 冻的形状，契约③ §7.2 今天照抄了它），写不进去**没法向上报失败**，
// 所以这里把"这格没落库"喊到日志里，而不是静默 no-op —— 静默归因正是 §2.2.1/REQ-A11 在终结的那种假账。

// dropRecorder 一个 adapter 一个（`cmd/core` 的构造点认识 adapter_key，`DroppedEvent` 里没有任何身份字段：
// T-155 把 `ProcessRole` 判成零生产者零消费者并删了它）。
type dropRecorder struct {
	store      auditedStore
	adapterKey string
	now        func() int64
	logf       func(format string, args ...any)
}

func newDropRecorder(store auditedStore, adapterKey string, now func() int64, logf func(string, ...any)) *dropRecorder {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &dropRecorder{store: store, adapterKey: adapterKey, now: now, logf: logf}
}

// RecordStart 今天**只出声、不写库**（T-168 收回了 T-162① 那句"`RecordStart` 走 `Append` 不受影响"）。
//
// 我 01:0x 那版读错了 §1.1 第 12 条：那句是"`process_instances` 属故意不发事件的可变表，
// **除** `process_started`/`process_exited`/`process_crashed` 三个外" —— "除…外"是**排除**，
// 也就是这三件是**要发事件的事实**，而 `Append` 恰恰不写事件（判据 U13-4 同一形状）。
// 所以拿 `Append` 建行 = 行写了、`process_started` 静默丢掉 ⇒ 正是 §1.1 第 13 条禁止的那个形状。
// 三格从今天起同形：等 REQ-DB-2 那条"无版本、但要发事件"的通道，一起走它。
// 要改成真写需要的是**一条说清"这张表的写归谁"的 REQ**，不是白名单例外，也不是口头豁免（§7.1 尾句）。
func (r *dropRecorder) RecordStart(ev process.Event) {
	r.logf("REQ-DB-2 未落：生命周期那格没建行 action=started adapter_key=%q pid=%d", r.adapterKey, ev.PID)
}

func (r *dropRecorder) RecordExit(ev process.Event) {
	r.logf("REQ-DB-2 未落：生命周期那格没改库 action=exited adapter_key=%q pid=%d exit=%d",
		r.adapterKey, ev.PID, ev.ExitCode)
}

func (r *dropRecorder) RecordCrash(ev process.Event) {
	r.logf("REQ-DB-2 未落：生命周期那格没改库 action=crashed adapter_key=%q pid=%d exit=%d crashes=%d",
		r.adapterKey, ev.PID, ev.ExitCode, ev.Crashes)
}

// RecordDrop 把一次"帧没能归属"落成一行审计。
//
// 身份在 `details_json` 里（T84-7b 00:2x 新增那条断言：审计行必须能指出是哪个 Adapter，
// **不许留空串冒充"知道"**）⇒ adapter_key 没装配就是装配漏件，这一行宁可不写也要喊出来：
// 写一行"不知道是谁丢的"审计，比少一行更难事后翻回来。
func (r *dropRecorder) RecordDrop(ev process.DroppedEvent) {
	if r.adapterKey == "" {
		r.logf("丢弃没有归属，这一行不写（拿空串冒充知道比不写更坏）: turn_ref=%q method=%q", ev.TurnRef, ev.Method)
		return
	}
	details, err := json.Marshal(map[string]any{
		"adapter_key":  r.adapterKey,
		"process_kind": string(ev.Kind),
		"method":       ev.Method,
		"turn_ref":     ev.TurnRef,
		// 两个 execution id 按"当时收到的样子"各记一份、不互相校正（T84-7）：
		// 它们不一致本身就是这一行要留住的事实，谁替谁对齐都是把事实擦掉。
		"frame_execution_id": ev.FrameExecutionID,
		"bound_execution_id": ev.BoundExecutionID,
		"reason":             ev.Reason,
		"dropped_at":         ev.Timestamp,
	})
	if err != nil {
		r.logf("丢弃审计编不出来，这一行没落库: %v", err)
		return
	}
	ctx := context.Background()
	id, err := newAuditID()
	if err != nil {
		r.logf("丢弃审计的 id 编不出来，这一行没落库: %v", err)
		return
	}
	if err := r.store.Write(ctx, func(tx *db.Tx) error {
		// 与 §5.4 那条腿同一套顺序：密封前过唯一那份脱敏（哈希才盖得住落库字节）→ append → 调用者写归因行。
		ch := &audit.Chain{Head: audit.HeadQuery(tx), Insert: tx, Now: r.now, Scrub: auditScrub}
		if _, err := ch.Append(ctx, audit.Entry{
			AuditID: id, PrincipalID: "system",
			Action: "frame_dropped", ResourceType: "adapter", ResourceID: r.adapterKey,
			// 丢弃没有"成功"可言（这帧就是没被处理），也不是"判不出来"（§5.4 那种 UNKNOWN）⇒ FAILURE。
			Outcome: "FAILURE", DetailsJSON: string(details), CorrelationID: ev.TurnRef,
		}); err != nil {
			return fmt.Errorf("丢弃的 Audit 没写出去: %w", err)
		}
		if _, err := ch.RecordRedaction(ctx, audit.Entry{
			AuditID: "redaction." + id, PrincipalID: "system",
			Action: "redaction.frame_dropped", ResourceType: "adapter", ResourceID: r.adapterKey,
			CorrelationID: ev.TurnRef,
		}, redactionHits(tx, ch)); err != nil {
			return fmt.Errorf("丢弃的脱敏归因行没写出去: %w", err)
		}
		return nil
	}); err != nil {
		// void 接口报不上去，只能喊：这一行的失败必须是能在日志里看见的事实。
		r.logf("丢弃审计没落库: %v", err)
	}
}

// newAuditID 给"没有上游 id 可复用"的审计行编一个 id。§5.4 那条腿的 id 是命令带进来的（重试要复用同一批），
// 丢弃不是由谁发起的命令，没有可复用的东西 ⇒ 只能现编；这里不宣称 v7，因为没有任何时钟序要保住。
func newAuditID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("audit id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
