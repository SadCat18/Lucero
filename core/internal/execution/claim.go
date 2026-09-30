package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// RecordClaim 是 `executions.provider_execution_id` 那一列的**唯一写者**，走的是 09-30 主控裁的
// 通道 (i)（契约① 第 25 条 ④-a :741）：聚合行的 UPDATE 只能配一条 §6 事件，而 (ii)「让 db 开一道
// 无事件的列写口子」被明拒 —— 理由原文是"每次 `Touch` 带一条事件是审计链地基，我不为省一行拆它"。
//
// 三条硬时机按 :741 那句逐字办，一条都不放宽：
//
//   - **只在 `turn/claimed` 那一刻**：调用方是 cmd/core 那条常驻入站泵，它在认出 claimed 帧的那一笔里
//     调本函数（`turnpump.go` 的 `recordClaim`）。本函数自己认不出帧，所以这一条靠"只有一个调用点"守着，
//     而那条判据在 `turnpump_claim_test.go`：dispatched 帧必须一次都不打到这里。
//   - **不许在 `dispatched` 补写**：这里不接受"从别的时机带进来的把手"这一说 —— 借口子的写法是把
//     `advance` 的 `execution_started` 那一笔顺手加上这一列（`columnWrites` 老注释里那条"乙档已裁不动"
//     说的就是它），现在裁了 (i)，那一列的写者就是本函数，别处一律不许碰。
//   - **不许借这一笔把状态显示成 running**：SET 里只有 `provider_execution_id` 与 `updated_at`，
//     **没有 `status`**，事件也不推任何一边（`From == To`，所以它不进 `EventOf`）。
//     契约② §6 规则 1 那句"开始证明仍只认 `dispatched`"因此不被这一笔绕开。
//
// 为什么要留这一列而不是只记在内存里：claimed 与 dispatched 之间有一段"Provider 已经接手、Core 还不知道
// 结果"的窗口，Core 在这一段里死掉之后，重启的那一代**只能从库里**读出那个把手去问 Provider
// （:741 引的三条规则里第 3 条说的就是这个时机）。内存里那份（泵的 `flights`）活不过这趟重启。
//
// 幂等的形状与 `FinishFromTurn` 不同，值得说清：同值重放 ⇒ `(false, nil)`，**不写库、不发第二条事件**；
// 异值 ⇒ 报错且一行都不写。后者不是防御性啰嗦：一个 Execution 对应一次 Provider 侧的执行
// （ADR-013/015 换 Provider 只产生**新** Execution），所以"把手变了"只有两种真相 ——
// 对端在撒谎，或者 Core 把两轮的帧归到了一行上。两种都不许用 UPDATE 抹平。
func (s *Service) RecordClaim(ctx context.Context, tx *db.Tx, id string, cmd Command) (bool, error) {
	switch {
	case cmd.ProviderExecutionID == "":
		return false, fmt.Errorf("%w: claimed 帧没带 provider_execution_id ⇒ 空串不是把手，也不许替 Provider 编一个",
			ErrInvalidExecution)
	case cmd.EventID == "":
		return false, fmt.Errorf("%w: 缺 event_id（§3 的 UUIDv7 由调用方铸，域不生成身份）", ErrInvalidExecution)
	}
	cur, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return false, err
	}
	if cur.ProviderExecutionID != "" {
		if cur.ProviderExecutionID == cmd.ProviderExecutionID {
			return false, nil // 同一把手重放：库里已经是这个事实，第二条事件是假历史
		}
		return false, fmt.Errorf("%w: %s 上已是 %q，帧里带来 %q", ErrClaimConflict, id,
			cur.ProviderExecutionID, cmd.ProviderExecutionID)
	}
	// 已定局的行不接受迟到的 claimed：在那之后发一条 `execution_claimed` 会把"接手"这件事排到"结束"之后，
	// 事件序列就成了假的。宁可喊出来、不写（迟到帧该怎么处置归协议，不归这一格替它决定）。
	if Terminal(cur.Status) {
		return false, fmt.Errorf("execution: %s 已定局（%s），迟到的 claimed 不回写把手: %w",
			id, cur.Status, ErrIllegalTransition)
	}
	payload, err := Payload(ClaimedPayload{ProviderExecutionID: cmd.ProviderExecutionID})
	if err != nil {
		return false, fmt.Errorf("execution: execution_claimed payload: %w", err)
	}
	now := time.Now().UnixMilli()
	// 这一行是全部边界所在：`status` 不在 SET 里。多写一个 `status = ?` 就把裁掉的 (ii) 换了个形状请回来了。
	_, err = tx.Touch(db.AggExecution, id, "provider_execution_id = ?, updated_at = ?",
		[]any{cmd.ProviderExecutionID, now},
		db.EventDraft{EventID: cmd.EventID, Type: EventClaimed, Aggregate: db.AggExecution, AggregateID: id,
			CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
			PrincipalID: cmd.PrincipalID, Payload: payload})
	if err != nil {
		return false, fmt.Errorf("execution: touch %s（execution_claimed，只写把手那一列）: %w", id, err)
	}
	return true, nil
}

// ErrClaimConflict 是"这一行的把手与帧里的不是同一个"。见上面那段：它不是重试的对手，
// 是一次归因错了，所以调用方必须喊，不许"以库里那份为准"悄悄丢掉帧里那个。
var ErrClaimConflict = errors.New("execution: provider_execution_id 已有值，不许覆盖")
