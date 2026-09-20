package test

import (
	"testing"

	"github.com/SadCat18/lucero/core/internal/task"
)

// §5.3 投影规则的独立转录：Execution 边沿 → Task 迁移。
// "from" 为空表示 §5.3 未钉死起点（execution_failed 只写目标态）。
type projection struct {
	edge        string
	from        string
	to          string
	handoffRead bool
	approved    bool
}

var projections = []projection{
	{"execution_started", "PREPARING", "RUNNING", false, false},
	{"execution_approval_requested", "RUNNING", "WAITING_APPROVAL", false, false},
	{"execution_approval_resolved", "WAITING_APPROVAL", "RUNNING", false, true},
	{"execution_succeeded", "RUNNING", "AGENT_COMPLETED", false, false},
	{"execution_failed", "RUNNING", "FAILED", false, false},
	{"execution_failed", "RUNNING", "TRANSFERRING", true, false},
	{"execution_interrupted", "RUNNING", "INTERRUPTED", false, false},
	{"execution_cancelled", "RUNNING", "CANCELLED", false, false},
}

// §5.3 末句：Execution 的非边沿变化不产生 Task 事件。
const notAnEdge = "execution_output_flushed"

// TestProjectionDrivesDeclaredEdges 正向：§5.3 每一行在其指定起点上都必须产出声明的目标态。
func TestProjectionDrivesDeclaredEdges(t *testing.T) {
	for _, p := range projections {
		to, ok, rej := task.ProjectTaskState(task.State(p.from), p.edge, p.handoffRead, p.approved)
		if !ok {
			t.Errorf("%s 从 %s 应投影到 %s，却被拒: %s", p.edge, p.from, p.to, rej.Reason)
			continue
		}
		if string(to) != p.to {
			t.Errorf("%s 从 %s 投影到 %s，期望 %s", p.edge, p.from, to, p.to)
		}
		if rej != (task.Rejection{}) {
			t.Errorf("%s 成功投影却带回 Rejection: %+v", p.edge, rej)
		}
	}
}

// TestProjectionNeverLeavesMatrix 反向（§5.4 的第一半）：投影只能沿 §5.1 的边走，
// 任何一次成功投影的目标态都必须被状态机 itself 认可；起点不对就必须拒且不改判。
func TestProjectionNeverLeavesMatrix(t *testing.T) {
	edges := []string{"execution_started", "execution_approval_requested", "execution_approval_resolved",
		"execution_succeeded", "execution_failed", "execution_interrupted", "execution_cancelled", notAnEdge}
	for _, m := range matrices() {
		if m.name != "Task" {
			continue
		}
		for _, from := range m.states {
			for _, edge := range edges {
				for _, handoff := range []bool{false, true} {
					to, ok, rej := task.ProjectTaskState(task.State(from), edge, handoff, true)
					if !ok {
						if to != "" {
							t.Errorf("%s 从 %s 被拒但仍返回目标态 %s", edge, from, to)
						}
						if rej.ExecutionEvent != edge || rej.TaskState != task.State(from) || rej.Reason == "" {
							t.Errorf("%s 从 %s 被拒但 Rejection 不完整: %+v", edge, from, rej)
						}
						continue
					}
					// 成功：这条边必须在 §5.1 里存在（Recovery 是 Normal 的超集，故按 Recovery 校验）。
					if err := task.Check(task.State(from), to, task.Recovery, task.Evidence{}); err != nil {
						t.Errorf("投影 %s（%s→%s）产出了 §5.1 之外的边: %v", edge, from, to, err)
					}
				}
			}
		}
	}
}

// TestTerminalsCannotBeProjected §5.4 的典型现场 + 终态保护。
//
// 注意一处容易读错的地方：§5.4 那句"典型：QUEUED/PAUSED/BLOCKED 时收到 execution_failed
// 且 Handoff 已 READY"限定的是 **Handoff 已 READY** 的分支（目标态 TRANSFERRING）。
// handoffReady=false 时 §5.3 的目标是 FAILED，而 §5.1 明确允许 QUEUED→FAILED、BLOCKED→FAILED，
// 所以那种投影**不**该被拒——本用例按这个口径钉住，若主控改判 §5.3/§5.4 需连带改这里。
func TestTerminalsCannotBeProjected(t *testing.T) {
	for _, from := range []string{"ACCEPTED", "CANCELLED"} {
		for _, handoff := range []bool{false, true} {
			to, ok, rej := task.ProjectTaskState(task.State(from), "execution_failed", handoff, false)
			if ok {
				t.Errorf("终态 %s 收到 execution_failed 被投影到 %s（handoffReady=%v）", from, to, handoff)
			}
			if rej.TaskState != task.State(from) || rej.Reason == "" {
				t.Errorf("终态 %s 的拒绝记录不完整: %+v", from, rej)
			}
		}
	}
	// §5.4 字面例子：Handoff 已 READY 时这三个状态都没有 →TRANSFERRING 这条边。
	for _, from := range []string{"QUEUED", "PAUSED", "BLOCKED"} {
		if to, ok, _ := task.ProjectTaskState(task.State(from), "execution_failed", true, false); ok {
			t.Errorf("%s 收到 execution_failed(handoffReady) 却投影到 %s，§5.4 要求停在原态", from, to)
		}
	}
	// PAUSED 连 →FAILED 也没有，所以不READY 时同样必须被拒。
	if _, ok, _ := task.ProjectTaskState(task.Paused, "execution_failed", false, false); ok {
		t.Error("PAUSED→FAILED 不在 §5.1，却通过了投影")
	}
	// 对照：QUEUED/BLOCKED 在非 READY 分支下走 §5.1 的合法边 FAILED，必须放行。
	for _, from := range []string{"QUEUED", "BLOCKED"} {
		to, ok, rej := task.ProjectTaskState(task.State(from), "execution_failed", false, false)
		if !ok || to != task.Failed {
			t.Errorf("%s 收到 execution_failed(非 READY) 应走 §5.1 的 %s→FAILED，实得 ok=%v to=%s rej=%+v",
				from, from, ok, to, rej)
		}
	}
}

// TestApprovalRejectionDoesNotAdvance §5.3 括注：只有"批准"的决议才推进 Task。
func TestApprovalRejectionDoesNotAdvance(t *testing.T) {
	for _, from := range []string{"WAITING_APPROVAL", "RUNNING", "QUEUED"} {
		if _, ok, _ := task.ProjectTaskState(task.State(from), "execution_approval_resolved", false, false); ok {
			t.Errorf("%s 收到未批准的 execution_approval_resolved 却推进了 Task", from)
		}
	}
}

// TestProjectionRejectedJournalAndAudit §5.4 的完整断言：被拒时 event_journal 零新增、
// Task 未变、Notification(projection_rejected) 与 Audit 各一条。
//
// 前半（Task 未变 / 不产出状态）已由上面的纯函数测试钉住。
// 后半需要真实接线：全仓 grep "projection_rejected" 目前只命中 projection.go 的一条注释，
// 没有任何 Service 把 Rejection 变成 Notification + Audit 行，也没有测试可用的 task Service。
//
// 断言口径按 T-21 更正过一遍：**不是**"event_journal 零新增"——被拒时 Notification 自身
// 就是一行 Durable Event（aggregate_type=notification），按旧措辞写会把正确实现判成失败。
func TestProjectionRejectedJournalAndAudit(t *testing.T) {
	t.Skip("blocked: 需要 backend 提供写库的 task Service（§5.4 的 Notification+Audit 落点尚无 owner）；" +
		"接线完成后本用例按 §5.4 + T-21 断言四条：" +
		"① event_journal 里 aggregate_type=task 零新增版本、且零 task_* 事件；" +
		"② tasks.status 与 tasks.version 与投影前逐字节相同（Task 停在原态）；" +
		"③ 恰有一行 notification_created（aggregate_type=notification，payload.notification_type=projection_rejected）；" +
		"④ Audit 走哈希链、不进 journal，且恰有一条 action=execution_failed / outcome=UNKNOWN、" +
		"details_json 带 task_state+execution_event+reason")
}
