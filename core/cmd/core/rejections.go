package main

// §5.4 那条"投影被拒不许静默吞"在这里落地：两条记录（Notification + Audit）由 rejectionReporter 写，
// 而"提交成功之后才推进锚点"由 auditedStore 收口。分工是有意的：
// transport 不该知道 audit 存在，业务包不该知道锚点存在，两边都不该知道彼此的顺序 —— 装配层知道。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

type rejectionReporter struct {
	notes *notification.Service
	now   func() int64
}

// Report 在**调用方那个事务里**写两条：Notification 走自己的聚合（必然落一行 notification_created，
// C12 更正后的口径），Audit 走行哈希链、不进 event_journal。
// action=execution_failed、outcome=UNKNOWN、details_json 带 §5.4 点名的三键 ——
// 三键直接 marshal rec.Rejection，因为它字段上的 json 名字就是从 §5.4 抄的，再抄一遍只会多一处真相。
func (r *rejectionReporter) Report(ctx context.Context, tx *db.Tx, rec task.RejectionReport) error {
	if _, err := r.notes.Notify(ctx, tx, notification.Draft{
		ID: rec.NotificationID, EventID: rec.EventID,
		Type: notification.TypeProjectionRejected, Priority: notification.PriorityHigh,
		Title: "Task 投影无合法目标", Body: rec.Reason, TaskID: rec.TaskID,
		PrincipalID: rec.PrincipalID, CorrelationID: rec.CorrelationID, CausationID: rec.CausationID,
	}); err != nil {
		return fmt.Errorf("§5.4 的 Notification 没写出去: %w", err)
	}
	details, err := json.Marshal(rec.Rejection)
	if err != nil {
		return fmt.Errorf("§5.4 的 audit details_json 编不出来: %w", err)
	}
	// §2.2.1 的装配半边（T-109 + T-114）：链在密封前要过唯一那份脱敏执行（db 的），
	// 拿到的命中再由**我这个调用者**写成一条 redaction 行 —— 两边缺一个就分别是
	// "哈希盖在改之前的字节上"（链永久重算不出来）与"字节对了但归因静默缩水"。
	// audit 不 import platform/db（§0），所以两者的类型搬运放在这里（`auditScrub`/`toAuditHits`）。
	ch := &audit.Chain{Head: audit.HeadQuery(tx), Insert: tx, Now: r.now, Scrub: auditScrub}
	if _, err := ch.Append(ctx, audit.Entry{
		AuditID: rec.AuditID, PrincipalID: rec.PrincipalID,
		// §5.4 指定的这一对：动作是 execution_failed（执行侧没能推进），结果判不出来说 UNKNOWN。
		Action: "execution_failed", ResourceType: "task", ResourceID: rec.TaskID, TaskID: rec.TaskID,
		Outcome: "UNKNOWN", DetailsJSON: string(details), CorrelationID: rec.CorrelationID,
	}); err != nil {
		return fmt.Errorf("§5.4 的 Audit 没写出去: %w", err)
	}
	// 没有命中时 `RecordRedaction` 自己返回 (0, nil)，所以诚实的写入不会多出空行（空归因是链上噪音，§2.2）。
	// audit_id 由 §5.4 那行的 id **派生**，刻意不用 UUIDv7 —— 但别把派生当"挡重放"的手段：
	// 我把下面这个 id 换成非派生形状做过变异，重放仍然只落一行归因，因为整笔事务在**更早的** notification id
	// 上就撞 UNIQUE 回滚了（§8 让重试复用同一批 id）。所以派生的真实收益只有两条、都不依赖 UNIQUE：
	// ① 同一事实的归因行 id 可回放、可直接归因到那一行 §5.4；② 决定论路径里少引一个随机源。
	// "重放不多行"由 TestRedactionRowIsWrittenOncePerRejection 钉 —— 它钉的是 §8 的整批 id 复用，不是这里。
	if _, err := ch.RecordRedaction(ctx, audit.Entry{
		AuditID: "redaction." + rec.AuditID, PrincipalID: rec.PrincipalID,
		Action: "redaction.execution_failed", ResourceType: "task", ResourceID: rec.TaskID, TaskID: rec.TaskID,
		CorrelationID: rec.CorrelationID,
	}, redactionHits(tx, ch)); err != nil {
		return fmt.Errorf("§5.4 的脱敏归因行没写出去: %w", err)
	}
	return nil
}

// auditScrub 是 `db.Scrub`（三值，T-114 的新签名）与 `audit.Chain.Scrub`（要 audit 自己的命中类型）
// 之间唯一的搬运。它**不执行任何脱敏** —— 执行仍只有 db 那一份（§2.2 只允许一份）。
func auditScrub(table string, cols []string, args []any) ([]any, []audit.RedactionHit, error) {
	values, hits, err := db.Scrub(table, cols, args)
	return values, toAuditHits(hits), err
}

func toAuditHits(hits []db.RedactionHit) []audit.RedactionHit {
	if len(hits) == 0 {
		return nil
	}
	out := make([]audit.RedactionHit, len(hits))
	for i, h := range hits {
		// 两边同名字段、Kind 都是 "key"/"pattern" 的字符串枚举（audit 与 db 各留一份是为了 §0 的方向）。
		out[i] = audit.RedactionHit{Column: h.Column, Kind: audit.HitKind(h.Kind), Match: h.Match, Count: h.Count}
	}
	return out
}

// redactionHits 是一笔事务里全部的脱敏归因：db 在聚合行上命中的（`notifications` 等），
// 加上密封前那次在审计行上命中的（`ch.ScrubHits()`）。两侧**直接拼接、不去重、不合并**：
// "这两个是不是同一次命中"的规则只住在强制点那侧，我这里再抄一份就是 §2.2.1 当初要终结的那种漂移
// （形状照 `internal/approval/store_sqlite.go` 的 `redactionHits`，那是同一条契约的既成写法）。
func redactionHits(tx *db.Tx, ch *audit.Chain) []audit.RedactionHit {
	return append(toAuditHits(tx.RedactionHits()), ch.ScrubHits()...)
}

// auditedStore 是交给 transport 的唯一写入口：一笔事务 + 一次锚点交账。
// 顺序是硬要求 —— 只有 db 那层确认 COMMIT 回来才允许 Advance；提交前推进会让锚点领先库内状态，
// 崩溃之后一次普通的回滚就会被读成"篡改"，而误报篡改比不报锚点更糟（ADR-018）。
//
// 为什么这里再来一次交账**不是** approval 那条腿的重复（巡检 11:59 提醒过双计风险，值得钉死）：
//   - approval 的链挂了自己的计数器（`store_sqlite.go` 的 `chain()` 里 `Checkpoints: s.cp`），
//     它写的那些行由它的 `write()` 在提交后 `ch.Checkpoint()` 交账；
//   - 我这个 reporter 的链**故意不挂** `Checkpoints`（见 Report 里那句 `&audit.Chain{…}`），
//     所以 §5.4 的审计行若不由这里交账，就永远没人交 —— 那正是 T-79 指出的"② 空转"。
//   - 两条腿会重叠的唯一条件是：有人在我的 `Write` 回调里再去调 approval store。
//     而那本身就不合法 —— approval 的 `write()` 自己会开一次 `d.Write`，写通道是 `SetMaxOpenConns(1)`，
//     嵌套会先把事务卡死，轮不到双计。这一条我**当场测过**（临时用例跑完后按文件名删除，不留仓库）：
//     在 `store.Write` 的回调里再调一次 `db.Write`，5s 不返回。
//     所以"双计"在今天的形状下不可达；可达的是**挂死**，那是所有角色都要守的一条接线约束：
//     不要在一次 `Store.Write` 事务里再去调任何自己会开事务的 Service（approval / 别的 store 都一样）。
//     万一以后形状变了，第一个会 notice 的是 `TestAnchorAdvancesOnlyAfterCommit`
//     里"一笔提交后 Counted() 必须是 1"这条断言。
type auditedStore struct {
	d           *db.DB
	checkpoints *audit.CheckpointCounter
}

func (s auditedStore) Write(ctx context.Context, fn func(tx *db.Tx) error) error {
	before, err := auditHead(ctx, s.d)
	if err != nil {
		return err
	}
	if err := s.d.Write(ctx, fn); err != nil {
		return err
	}
	after, err := auditHead(ctx, s.d)
	if err != nil {
		return err
	}
	if after.Seq == before.Seq {
		return nil // 这笔事务没写审计行，计数器一次都不该加
	}
	return s.checkpoints.Advance(int(after.Seq-before.Seq), after)
}

// auditHead 读链头。**为什么可以拿"提交前后两次读"当 append 条数**：
// platform/db 的写通道是 `write.SetMaxOpenConns(1)`（db.go:21~25 自己写着"the serialized write channel"），
// 所以这两次读之间不会插进另一个提交者，delta 就是本笔事务写进去的审计行数。
// 一旦哪天写通道不再串行，这个计数就会重复累加 —— 锚点本身仍然有效（Commit 只认递增 seq、
// 内容是真提交过的行），错的是"第 1000 条"这个时机，不会造成误报篡改。
func auditHead(ctx context.Context, d *db.DB) (audit.Head, error) {
	seq, hash, err := audit.HeadQuery(d.Read)(ctx)
	if err != nil {
		return audit.Head{}, fmt.Errorf("读审计链头: %w", err)
	}
	return audit.Head{Seq: seq, Hash: hash}, nil
}
