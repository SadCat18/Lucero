package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

// 甲（T-370① 裁的那一支）的落点 (a)：把一帧 `turn/finished` 落成 Execution 的终态，并同笔投影到 Task。
// 谁调它随 REQ-TURN-1 裁「Ⅰ」变了：上一形状由 handler 在第二笔短事务里调（收尾随 send 回执同步交回），
// 现在是 cmd/core 那条**常驻入站泵**在收到帧的那一笔里调 —— 落库这一格的内容一个字没改，改的是触发时机。
//
// 为什么需要单独一个导出方法，而不是让调用方直接调 `advance`：`advance` 是所有推进共用的四步
// （读行 → If-Match → §5.2 裁决 → Touch + 事件），它**接收目标态**；而"帧里那个字符串算哪种终态"是新的
// 解释职责 —— 混进调用方就会长出第二份映射（边界一禁的正是这个）。
//
// **取值表只有一份，就是本包的状态常量**：`SUCCEEDED` / `FAILED` / `CANCELLED`（边界一点名的那三个）。
// 这里不引入第四个字面、也不给任何别名 —— 协议规范 `specs/adapter-protocol/adapter-protocol-v0.1.md:279`
// 只写了"`turn/finished` 带 `result_code`，是唯一的强制收尾信号"，**没有取值表**；
// 现仓唯一的生产者 `cmd/fake-adapter` 今天发两处：`"SUCCEEDED"`（`main.go:490`，常量在 `:117`）与 `"CANCELLED"`
// （`service.go:458`）；它曾发过的 `"OK"`（队列排干）与 `"INTERRUPTED"`（进程重启）按 T-382②④ 已经不发了。
// ⇒ 所以表里没有的字面一律**不写终态并喊出来**；这一格的判据用**测试自己造的字面**（`"OK"` 只是其中一个样本，
//
//	不代表 producer 现状），把 `"OK"` 折成成功就是「伪造终态」—— 取值表本身已由 `契约② §6` 第 6 条裁死。
var ErrUnmappedResultCode = errors.New("execution: turn/finished 的 result_code 不在已裁的取值表里")

// turnFinishedState 是那份唯一的映射。
func turnFinishedState(resultCode string) (State, bool) {
	switch s := State(resultCode); s {
	case Succeeded, Failed, Cancelled:
		return s, true
	}
	return "", false
}

// MappedTurnResultCode 是给装配层问的那一句："这个收尾码在不在已裁的取值表里"。
// 它和 `FinishFromTurn` 读**同一份** `turnFinishedState`（边界一：不许长出第二份映射），
// 存在的唯一理由是收帧的那一条腿（REQ-TURN-1「Ⅰ」的入站泵）要能在**开写事务之前**问一句：
// 表外的码一行都不写，而写面只有 `platform/db` 那一条串行连接，不该为注定回滚的一笔去占它。
func MappedTurnResultCode(resultCode string) bool {
	_, ok := turnFinishedState(resultCode)
	return ok
}

// FinishFromTurn 把一次收尾落成同一笔事务里的几件事：execution 终态边（含 §6 事件）+ §5.3 的 task 投影
// + 投影落定 AGENT_COMPLETED 时契约④-c 那半格 WAITING_ACCEPTANCE（见下面那段）。
// 事件 ID 由调用方铸（`cmd.EventID` 给 execution、`cmd.TaskEventID` 给 task 投影、
// `cmd.TaskWaitingAcceptanceEventID` 给链尾那条），平台口径如此，域不自己生成身份。
//
// 三种结局互不重叠：
//  1. 表里没有这个 `resultCode` ⇒ `ErrUnmappedResultCode`，**一行都不写**（边界一）；
//  2. 库里已经是这个终态（同帧重放、或两条腿都收到了同一份收尾）⇒ `alreadyFinished=true`、`Advanced=false`，
//     **不重复发事件**（幂等：调用方据此什么都不补，只喊一声）；
//  3. 正常落终态 ⇒ 走 `advance`（`Occasion=Normal`，与 `Recovery` 无关：这是同步腿收到的真帧）。
//
// 投影被拒（Task 不在 §5.3 要求的那个态）⇒ 整笔回滚并上抛 `ErrIllegalTransition`，
// 这一趟**连终态那一行都不写**（泵因此只喊、不改库）；§5.4 那两条记录此刻没有 reporter（与 send 腿同一形状的取舍），要就裁。
// 幂等密封不归这里：那是 handler 第二笔自己的 guard（REQ-TURN-1「Ⅰ」之后两笔不再是同一笔）。
func (s *Service) FinishFromTurn(ctx context.Context, tx *db.Tx, id, resultCode string,
	cmd Command) (res Result, alreadyFinished bool, err error) {
	to, ok := turnFinishedState(resultCode)
	if !ok {
		return Result{}, false, fmt.Errorf("%w: 帧里是 %q，已裁的只有 SUCCEEDED/FAILED/CANCELLED（收到帧不等于成功）",
			ErrUnmappedResultCode, resultCode)
	}
	if s.projector == nil {
		return Result{}, false, fmt.Errorf("execution: FinishFromTurn 需要 task 投影器，现在没装配（EnableSend 没调过）")
	}
	cur, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return Result{}, false, err
	}
	if cur.Status == to {
		return Result{From: cur.Status, To: cur.Status, Version: cur.Version}, true, nil
	}

	finish := cmd
	finish.ResultCode = resultCode
	res, err = s.advance(ctx, tx, id, to, finish)
	if err != nil {
		return res, false, err
	}

	// §5.3 三条边都在 task/projection.go 里（succeeded→AGENT_COMPLETED、failed→Failed/Transferring、
	// cancelled→Cancelled），边集说什么就做什么；handoff 今天没有生产者 ⇒ handoffReady 恒 false，
	// 不替 §5.3 那个 failed 分支编一个"也许在转移"的判断。
	edge := task.EdgeSucceeded
	switch to {
	case Failed:
		edge = task.EdgeFailed
	case Cancelled:
		edge = task.EdgeCancelled
	}
	pres, err := s.projector.ApplyExecutionEvent(ctx, tx, cur.TaskID, edge, false, false, task.Command{
		EventID: cmd.TaskEventID, ExecutionID: id, ResultCode: resultCode,
		PrincipalID: cmd.PrincipalID, CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
	})
	if err != nil {
		if errors.Is(err, task.ErrProjectionRejected) {
			return res, false, fmt.Errorf("execution: %s 的 task 投影被拒: %w", edge, ErrIllegalTransition)
		}
		return res, false, err
	}

	// 契约④-c（REQ-ACCEPT-1 裁「甲」）：在任务投影落定 AGENT_COMPLETED 的**同一笔事务**里接着推
	// WAITING_ACCEPTANCE —— §7 垂直链的 `task_agent_completed → task_waiting_acceptance` 两步，
	// 也是 `:accept`（只认 WAITING_ACCEPTANCE 起始态）今天唯一缺的那个驱动方。
	//
	// 判据**以投影返回为准**，不在这里另立第二套映射：不是这一格就一步都不多走 ——
	// failed/cancelled（→Failed/Transferring/Cancelled）、投影被拒、或压根没推进 ⇒ 半格不发生。
	// 半格（只发 task_agent_completed、停在 AGENT_COMPLETED ⇒ `:accept` 永远 409）比不发更坏。
	// 同帧重放走不到这里：上面 `cur.Status == to` 已经以 alreadyFinished 提前返回，第二条事件不会发。
	if pres.Advanced && pres.To == task.AgentCompleted {
		if cmd.TaskWaitingAcceptanceEventID == "" {
			return res, false, fmt.Errorf("execution: 这一帧要同笔推 WAITING_ACCEPTANCE，却没有对应的 event_id（调用方铸 UUIDv7）")
		}
		if _, err := s.projector.AdvanceToWaitingAcceptance(ctx, tx, cur.TaskID, task.Command{
			EventID: cmd.TaskWaitingAcceptanceEventID, ExecutionID: id,
			PrincipalID: cmd.PrincipalID, CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
		}); err != nil {
			// 起始态/矩阵/缺 id 一类的拒绝：整笔回滚（既不写终态也不密封），与投影被拒同一形状。
			return res, false, fmt.Errorf("execution: AGENT_COMPLETED→WAITING_ACCEPTANCE 没推成: %w", err)
		}
	}
	return res, false, nil
}
