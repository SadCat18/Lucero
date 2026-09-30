package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
)

// 这一格是 §2.3 启动顺序里 Event Journal 之后的那个空位（T-391 / REQ-RC-1）：
// 恢复扫描器成为 Occasion=Recovery 的生产入口，并按 CO-45③ 的口径数活进程。
//
// 为什么这些查询在 cmd/core 而不在业务包里：process_instances 归 internal/process（契约③ §7），
// 而 §0 禁止业务包之间跨包 SQL ⇒ 事实只能由装配层取好交进去（§0 末句：依赖靠注入，不靠 import）。
// 与 audit 链头、principals 那两处同族：cmd/core 持有库、只做取数与接线，不做业务判断（§7）。
// 只 SELECT：归属扫描把这一处按读面记 INFO（不写字面表名、不走 .Exec(）。

// queryer 是本文件只读查询的窄接口（*db.DB 的读池与 *db.Tx 都满足）。
// 不复用 execution.Queryer：这里查的是 process 那一格的表，与 execution 那个接口没有从属关系。
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// runRecovery 是启动时那一格的正文，两件事、都只喊不中断：
//
//  1. 按 CO-45③ 登记的口径数一次活进程 —— 口径在契约① 第 20 条里，谁都不许发明第二个；
//  2. 跑一次恢复扫描（execution.Occasion=Recovery 今天唯一的生产调用点）。
//
// 不返回 error 是有意的：恢复扫描失败不许把 Core 打死（启动顺序后面还有 HTTP/Health/Ready），
// 但 CO-44② 那句"0 行与失败都不许静默"是日志的下限 ⇒ 每条分支都落一句可 grep 的话，
// 并把"库里一行未改（整笔回滚）"这句一起说出来。
func (c *Core) runRecovery(ctx context.Context) {
	c.reportActiveProcesses(ctx)

	var rep execution.RecoveryReport
	err := c.db.Write(ctx, func(tx *db.Tx) error {
		var e error
		rep, e = c.executions.ScanRecovery(ctx, tx)
		return e
	})
	log.Printf("core: 恢复扫描（REQ-RC-1）: 候选 %d、查不到进程行 %d、改判 %d、受阻 %d",
		len(rep.Candidates), len(rep.Unattributed), len(rep.Marked), len(rep.Blocked))
	if len(rep.Candidates) == 0 {
		log.Printf("core: 恢复扫描: 没有在飞的 Execution，本次没有候选")
	}
	if len(rep.Unattributed) > 0 {
		// CO-44②：0 行不许静默 —— "查不到主体"与"改判了"必须是两句话。
		log.Printf("core: 恢复扫描: 这 %d 条查不到该台的进程行 ⇒ 一行都不改判（CO-44②）: %v",
			len(rep.Unattributed), rep.Unattributed)
	}
	for _, b := range rep.Blocked {
		log.Printf("core: 恢复扫描: 有证据却改判不了 %s: %v", b.ID, b.Err)
	}
	if err != nil {
		log.Printf("core: 恢复扫描没落库（整笔回滚，库里一行未改）: %v", err)
	}
}

// recoveryAudit 造 EnableRecovery 那只 audit 写口：把 §6 里 Audit 列 ✓（execution_interrupted）
// 要的那一行写进**与改判同一笔事务** —— 事后补记在崩溃之后会变成"改判有、证据没有"。
//
// 为什么这一半住在 cmd/core：链头、脱敏执行、归因行都归装配层（C14：业务包不 import audit；
// dropaudit.go 那句"`Tx.Append(\"audit_entries\"` 字面量即 W13-2 红"说的是同一条规矩 ——
// 表名与链都不许旁落）。域侧只负责两件事：这条事件要不要审计（RequiresAudit，§6 说了算）
// 与"为什么改判"的原文（AuditRecord.Reason）。形状照 cmd/core/rejections.go 里 §5.4 那条既有写法：
//
//   - audit_id 从事件 id 派生（与 §5.4 同形：同一事实的归因行可回放、可指回事件那一行）；
//   - details_json 只带 event_id 与 reason 两键，reason 是域侧拼好的原文，不在这里改写；
//   - 脱敏命中照样写归因行（RecordRedaction）：没有命中时它自己返回 (0, nil)，不添空行。
func recoveryAudit(now func() int64) execution.AuditFunc {
	return func(ctx context.Context, tx *db.Tx, rec execution.AuditRecord) error {
		details, err := json.Marshal(map[string]any{"event_id": rec.EventID, "reason": rec.Reason})
		if err != nil {
			return fmt.Errorf("恢复扫描的 audit details_json 编不出来: %w", err)
		}
		auditID := "audit-" + rec.EventID
		ch := &audit.Chain{Head: audit.HeadQuery(tx), Insert: tx, Now: now, Scrub: auditScrub}
		if _, err := ch.Append(ctx, audit.Entry{
			AuditID: auditID, PrincipalID: rec.PrincipalID,
			Action: execution.EventInterrupted, ResourceType: "execution", ResourceID: rec.ExecutionID,
			TaskID: rec.TaskID, ExecutionID: rec.ExecutionID, CorrelationID: rec.EventID,
			Outcome: audit.OutcomeSuccess, DetailsJSON: string(details),
		}); err != nil {
			return fmt.Errorf("恢复扫描的 audit 行没写出去: %w", err)
		}
		if _, err := ch.RecordRedaction(ctx, audit.Entry{
			AuditID: "redaction." + auditID, PrincipalID: rec.PrincipalID,
			Action: "redaction." + execution.EventInterrupted, ResourceType: "execution",
			ResourceID: rec.ExecutionID, TaskID: rec.TaskID, ExecutionID: rec.ExecutionID,
			CorrelationID: rec.EventID,
		}, redactionHits(tx, ch)); err != nil {
			return fmt.Errorf("恢复扫描的脱敏归因行没写出去: %w", err)
		}
		return nil
	}
}

// reportActiveProcesses 把 CO-45③ 那个口径数出来的活进程喊进日志。
// 0 行按 CO-44② 出声，且明写"按这个口径数出来 0 行"——不许读成"这台机器没有进程在跑"。
func (c *Core) reportActiveProcesses(ctx context.Context) {
	counts, err := activeProcessCounts(ctx, c.db.Read)
	if err != nil {
		log.Printf("core: 活进程计数读不出来（CO-45③）: %v —— 读不出来不等于 0", err)
		return
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		log.Printf("core: 活进程计数（CO-45③ 口径: status IN STARTING/RUNNING/STOPPING，按 process_type 分组）" +
			"= 0 行 —— 0 行按 CO-44② 出声，不等于没有进程在跑")
		return
	}
	log.Printf("core: 活进程计数（CO-45③ 口径: status IN STARTING/RUNNING/STOPPING，按 process_type 分组）: %d 行 %v",
		total, counts)
}

// activeProcessCounts 是 CO-45③ 登记的**唯一**口径：`status IN ('STARTING','RUNNING','STOPPING')`
// 按 process_type 分组数行数。三个状态值取 internal/process 的常量（CO-49：词表只有一处定义，
// 不在这里抄第二遍字面量），集合本身不许增减 —— 要改（例如把 STOPPING 算死）按 CO-45③ 末句
// 写 REQ 改那一条，不改这段代码注释。
func activeProcessCounts(ctx context.Context, q queryer) (map[string]int, error) {
	rows, err := q.QueryContext(ctx, `SELECT process_type, count(*)
		FROM process_instances
		WHERE status IN (?, ?, ?)
		GROUP BY process_type
		ORDER BY process_type`,
		string(process.StatusStarting), string(process.StatusRunning), string(process.StatusStopping))
	if err != nil {
		return nil, fmt.Errorf("活进程计数: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, fmt.Errorf("活进程计数 scan: %w", err)
		}
		out[kind] = n
	}
	return out, rows.Err()
}

// processFactsOf 造 EnableRecovery 那只只读口：按 adapter_instance_id 交出该台登记过的全部进程行
// （含已退出的 —— "上一代跑过什么"正是崩溃恢复要问的事）。只 SELECT，不带第二处表名知识。
func processFactsOf(q queryer) execution.ProcessFacts {
	return func(ctx context.Context, adapterInstanceID string) ([]execution.ProcessFact, error) {
		rows, err := q.QueryContext(ctx, `SELECT id, pid, status, metadata_json
			FROM process_instances
			WHERE owner_type = ? AND owner_id = ?
			ORDER BY created_at, id`, process.OwnerTypeAdapterInstance, adapterInstanceID)
		if err != nil {
			return nil, fmt.Errorf("查 %s 的进程行: %w", adapterInstanceID, err)
		}
		defer rows.Close()
		var out []execution.ProcessFact
		for rows.Next() {
			var f execution.ProcessFact
			var pid sql.NullInt64
			var meta string
			if err := rows.Scan(&f.ID, &pid, &f.Status, &meta); err != nil {
				return nil, fmt.Errorf("查 %s 的进程行 scan: %w", adapterInstanceID, err)
			}
			f.PID, f.Attempt = pid.Int64, attemptOf(meta)
			out = append(out, f)
		}
		return out, rows.Err()
	}
}

// attemptOf 只取 metadata_json 的 attempt 序号（CO-45②：那一格记 attempt + 上一条 instance id，
// 不开新列）。key 与 internal/process 的写口逐字同源（lifecycle.go 落行时写的就是 "attempt"）。
// 解析不出来就是 0 —— 0 与"这行没写这个键"不可分，这是已知的口径限制，记进 REQ-RC-2 的缺口里，
// 不在这里拿一次解析失败去编一个数。
func attemptOf(metadataJSON string) int {
	var m struct {
		Attempt int `json:"attempt"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &m); err != nil {
		return 0
	}
	return m.Attempt
}
