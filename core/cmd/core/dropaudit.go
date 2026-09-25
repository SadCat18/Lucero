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
// 四格现在**都真写**（REQ-DB-2 那条通道 09-24 到位之后），但写的东西分两类：
//   - **RecordDrop** 落一行审计，走 `audit` 自己的出口，`event_journal` 零新增（§7.2 里那句"写审计不写事件"的断言管的就是这一对；
//     这里出现 `Tx.Append("audit_entries"` 字面量即 W13-2 红）。
//   - **生命周期那三格** 每格 = 一笔事务里把 `*db.Tx` 递给 `internal/process` 的写口
//     （契约① §1.1 第 17 条"落点与角色"：观察语义归 `internal/process`，cmd/core 只装配），
//     于是"行 + 恰一条事件"是同一笔提交里的事 —— 这正是 T-168 要消灭的那个形状（行写了、事件静默丢掉）的反面。
//     三格是 void 的（T-62 冻的形状），写不进去**没法向上报失败**，所以成败都喊到日志：
//     静默归因是 §2.2.1/REQ-A11 在终结的那种假账，而"没人挂桥"与"挂了但没落库"必须是两句话。
//
// 历史一句（免得下一个人再走一遍）：T-162① 说过"`RecordStart` 走 `Append` 不受影响"，T-168 收回；
// §1.1 第 12 条那句"`process_instances` 属故意不发事件的可变表"后面还跟着"**除** 那三件外"—— 是排除不是包含。

// dropRecorder 一个 adapter 一个（`cmd/core` 的构造点认识 adapter_key，`DroppedEvent` 里没有任何身份字段：
// T-155 把 `ProcessRole` 判成零生产者零消费者并删了它）。
type dropRecorder struct {
	store      auditedStore
	adapterKey string
	now        func() int64
	logf       func(format string, args ...any)
	// lifecycle 是 `internal/process` 那个写口（契约① §1.1 第 17 条"落点与角色"：语义住在观察方，
	// cmd/core 只递 `*db.Tx` 并把错误喊出来）。nil = 这一格没装配成（构造点报错等），三格退回"只出声"——
	// 那不是失败被吞，是**另一种诚实**：喊出来的句子会明写"没装配"，而不是假装落库了。
	lifecycle *process.LifecycleStore
}

func newDropRecorder(store auditedStore, adapterKey string, now func() int64, logf func(string, ...any),
	lifecycle *process.LifecycleStore) *dropRecorder {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &dropRecorder{store: store, adapterKey: adapterKey, now: now, logf: logf, lifecycle: lifecycle}
}

// writeLifecycle 是三格共用的那一条通道：一笔事务里交给写口落"一行 + 恰一条事件"，喊错不吞错。
// `process.Recorder` 三格是 void（T-62 冻的形状），报不上去只能喊 —— 但喊的是**真试过的证据**：
// 成功也喊（带行 id），失败也喊（带原因），这样"没人挂桥"与"挂了但没落库"在日志里是两句话。
func (r *dropRecorder) writeLifecycle(action string, fn func(tx *db.Tx) error) {
	if r.lifecycle == nil {
		r.logf("REQ-DB-2 写口没装配：生命周期那格没落库、只出声 action=%s adapter_key=%q", action, r.adapterKey)
		return
	}
	err := r.store.Write(context.Background(), fn)
	if err != nil {
		r.logf("生命周期那格没落库 action=%s adapter_key=%q: %v", action, r.adapterKey, err)
		return
	}
	r.logf("生命周期已落 action=%s adapter_key=%q row=%q", action, r.adapterKey, r.lifecycle.ID())
}

// RecordStart 落"这一次尝试的一行 + `process_started`"（同一笔事务，§1.1 第 17 条要的就是这个配对）。
//
// 历史：T-162① 曾说"`RecordStart` 走 `Append` 不受影响"，T-168 收回了它 —— 拿 `Append` 建行会把已登记的
// `process_started` 静默丢掉，正是 §1.1 第 13 条禁止的形状。今天有了一条**说清这张表写归谁**的通道
// （`CreateVersionless` + 写口在 `internal/process`），所以这里不再需要白名单例外或口头豁免（§7.1 尾句）。
func (r *dropRecorder) RecordStart(ev process.Event) {
	r.writeLifecycle("started", func(tx *db.Tx) error {
		_, err := r.lifecycle.RecordProcessStart(tx, ev)
		return err
	})
}

func (r *dropRecorder) RecordExit(ev process.Event) {
	r.writeLifecycle("exited", func(tx *db.Tx) error {
		return r.lifecycle.RecordProcessExit(tx, ev)
	})
}

func (r *dropRecorder) RecordCrash(ev process.Event) {
	r.writeLifecycle("crashed", func(tx *db.Tx) error {
		return r.lifecycle.RecordProcessCrash(tx, ev)
	})
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
