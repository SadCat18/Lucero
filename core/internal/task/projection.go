package task

import "fmt"

// §5.3 中会驱动 Task 的 Execution 边沿。取值就是 event_type 原文，不另立词汇。
// 不在这张表里的 Execution 事件（execution_created、execution_output_flushed、
// execution_paused、execution_resumed，以及 §6 根本没有事件的 RUNNING→WAITING_INPUT）
// 一律不产生 Task 事件。
const (
	EdgeStarted           = "execution_started"
	EdgeApprovalRequested = "execution_approval_requested"
	EdgeApprovalResolved  = "execution_approval_resolved"
	EdgeSucceeded         = "execution_succeeded"
	EdgeFailed            = "execution_failed"
	EdgeInterrupted       = "execution_interrupted"
	EdgeCancelled         = "execution_cancelled"
)

// Rejection 是 §5.4 要求的那两条记录（Notification projection_rejected + Audit）的全部内容，
// 字段名与 details_json 的键一一对应。零值 = 没有被拒绝。
// Notification 与 Audit 走各自通道，不进 event_journal。
type Rejection struct {
	TaskState      State  `json:"task_state"`
	ExecutionEvent string `json:"execution_event"`
	Reason         string `json:"reason"`
}

// ProjectTaskState 返回该 Execution 边沿应驱动的 Task 迁移。
// ok=false 时 rej 非空，调用方必须按 §5.4 发 Notification + Audit，不得静默吞。
// handoffReady = 本次 Execution 的 Handoff 已 READY/STARTING_TARGET，是 execution_failed 的唯一分支。
// approved 仅对 EdgeApprovalResolved 有意义：拒绝/过期/取消的决议不推进 Task。
func ProjectTaskState(from State, edge string, handoffReady, approved bool) (State, bool, Rejection) {
	switch edge {
	case EdgeStarted:
		return guard(from, Preparing, Running, edge)
	case EdgeApprovalRequested:
		return guard(from, Running, WaitingApproval, edge)
	case EdgeApprovalResolved:
		if !approved {
			return "", false, reject(from, edge, "决议不是批准，不推进 Task")
		}
		return guard(from, WaitingApproval, Running, edge)
	case EdgeSucceeded:
		return guard(from, Running, AgentCompleted, edge)
	case EdgeInterrupted:
		return guard(from, Running, Interrupted, edge)
	case EdgeCancelled:
		return guard(from, Running, Cancelled, edge)
	case EdgeFailed:
		to := Failed
		if handoffReady {
			to = Transferring
		}
		return guardAny(from, to, edge)
	}
	return "", false, reject(from, edge, "不是 §5.3 的边沿")
}

// guard 要求当前状态正是 §5.3 指定的那一个。矩阵校验用 Recovery 场合：§5.3 的边沿
// 既可能来自正常运行，也可能来自重启扫描（execution_interrupted 对应的
// RUNNING→INTERRUPTED 在 §5.1 里只对 Recovery 开放），而 Recovery 是 Normal 的超集。
func guard(from, want, to State, edge string) (State, bool, Rejection) {
	if from != want {
		return "", false, reject(from, edge, fmt.Sprintf("该边沿只从 %s 驱动 Task", want))
	}
	return guardAny(from, to, edge)
}

func guardAny(from, to State, edge string) (State, bool, Rejection) {
	if Check(from, to, Recovery, Evidence{}) != nil {
		return "", false, reject(from, edge, fmt.Sprintf("§5.1 没有 %s→%s 这条边", from, to))
	}
	return to, true, Rejection{}
}

func reject(from State, edge, reason string) Rejection {
	return Rejection{TaskState: from, ExecutionEvent: edge, Reason: reason}
}
