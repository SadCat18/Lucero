package main

// 审计链的读侧与三态判定（ADR-018 / T-64③）。链的**写**入口与哈希算法都在 internal/audit，
// 这里只做两件事：把 audit_entries 读成 []audit.Entry 交给 Check，以及把 Check 的结果诚实喊出来。

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/audit"
)

// auditEntryColumns 是 audit_entries 的全部列。清单取自 000001 迁移（全项目共用的 schema 真相），
// 不是抄 audit 包内部的私有常量。漂移仍是真风险，所以 TestAuditEntryColumnsMatchSchema
// 用 PRAGMA table_info 钉住"这张表恰好就是这些列"：
// 少读一列 → VerifyChain 重算不出同一个 entry_hash → UNVERIFIED（fail closed），
// 最坏的可能是"该说的没说出来"，不会是"误报校验通过"。
var auditEntryColumns = []string{
	"seq", "audit_id", "principal_id", "action", "resource_type", "resource_id",
	"project_id", "task_id", "execution_id", "correlation_id", "outcome",
	"details_json", "previous_hash", "entry_hash", "signature", "created_at",
}

func loadAuditEntries(ctx context.Context, q *sql.DB) ([]audit.Entry, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+joinColumns(auditEntryColumns)+` FROM audit_entries ORDER BY seq`)
	if err != nil {
		return nil, fmt.Errorf("读审计链: %w", err)
	}
	defer rows.Close()

	var out []audit.Entry
	for rows.Next() {
		var (
			e                                      audit.Entry
			auditID, principalID, action, rType    sql.NullString
			rID, projectID, taskID, execID, corrID sql.NullString
			outcome, details, prevHash             sql.NullString
			entryHash, signature                   sql.NullString
			created                                sql.NullInt64
		)
		if err := rows.Scan(&e.Seq, &auditID, &principalID, &action, &rType, &rID,
			&projectID, &taskID, &execID, &corrID, &outcome, &details,
			&prevHash, &entryHash, &signature, &created); err != nil {
			return nil, fmt.Errorf("读审计链 scan: %w", err)
		}
		e.AuditID, e.PrincipalID, e.Action = auditID.String, principalID.String, action.String
		e.ResourceType, e.ResourceID = rType.String, rID.String
		e.ProjectID, e.TaskID, e.ExecutionID, e.CorrelationID = projectID.String, taskID.String, execID.String, corrID.String
		e.Outcome, e.DetailsJSON, e.PreviousHash = outcome.String, details.String, prevHash.String
		e.EntryHash, e.Signature, e.CreatedAt = entryHash.String, signature.String, created.Int64
		out = append(out, e)
	}
	return out, rows.Err()
}

func joinColumns(cols []string) string {
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ", "
		}
		out += c
	}
	return out
}

// auditChainStatus 把整条链交给 audit.Check 判三态。
// 只有 Check 真的逐链重算过、并且撞到锚点，才可能返回 StatusVerifiedLocal；
// 读不动、列对不上、锚点缺失或链落后于锚点，一律落回 UNVERIFIED / UNKNOWN。
func (c *Core) auditChainStatus(ctx context.Context) (audit.ChainStatus, error) {
	entries, err := loadAuditEntries(ctx, c.db.Read)
	if err != nil {
		return "", err
	}
	return audit.Check(entries, c.anchor)
}
