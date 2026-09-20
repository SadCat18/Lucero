package task_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/task"
)

// allTaskStates 是从 §5.1 状态清单独立手抄的第三份表（task_test 包看不到实现里的表）。
var allTaskStates = []task.State{task.Created, task.Queued, task.Preparing, task.Running,
	task.WaitingApproval, task.WaitingInput, task.Blocked, task.Paused, task.Transferring,
	task.AgentCompleted, task.Verifying, task.WaitingAcceptance, task.Accepted,
	task.Failed, task.Cancelled, task.Interrupted, task.Unknown}

// §5.3 投影表逐行：每条投影出的迁移必须同时落在 §5.1 矩阵里，否则投影在造非法状态。
func TestProjectionMatchesContract53(t *testing.T) {
	cases := []struct {
		from         task.State
		edge         string
		handoffReady bool
		approved     bool
		want         task.State
	}{
		{task.Preparing, task.EdgeStarted, false, false, task.Running},
		{task.Running, task.EdgeApprovalRequested, false, false, task.WaitingApproval},
		{task.WaitingApproval, task.EdgeApprovalResolved, false, true, task.Running},
		{task.Running, task.EdgeSucceeded, false, false, task.AgentCompleted},
		{task.Running, task.EdgeFailed, false, false, task.Failed},
		{task.Running, task.EdgeFailed, true, false, task.Transferring},
		{task.Interrupted, task.EdgeFailed, true, false, task.Transferring},
		{task.Running, task.EdgeInterrupted, false, false, task.Interrupted},
		{task.Running, task.EdgeCancelled, false, false, task.Cancelled},
	}
	for _, c := range cases {
		got, ok, rej := task.ProjectTaskState(c.from, c.edge, c.handoffReady, c.approved)
		if !ok || got != c.want {
			t.Errorf("%s --%s--> 期望 %s，实际 %s (ok=%v rej=%v)", c.from, c.edge, c.want, got, ok, rej)
			continue
		}
		if rej.Reason != "" {
			t.Errorf("放行的投影却带了解释：%+v", rej)
		}
		// §5.1 是权威：投影出的目标必须在矩阵内。execution_interrupted 对应的
		// RUNNING→INTERRUPTED 只对 Recovery 开放（INTERRUPTED 只能由恢复扫描写入）。
		if err := task.Check(c.from, got, task.Recovery, task.Evidence{}); err != nil {
			t.Errorf("投影出的 %s→%s 不在 §5.1 矩阵里: %v", c.from, got, err)
		}
		if c.edge != task.EdgeInterrupted {
			if err := task.Check(c.from, got, task.Normal, task.Evidence{}); err != nil {
				t.Errorf("运行期边沿 %s→%s 应可用 Normal 场合: %v", c.from, got, err)
			}
		}
	}
}

// Task 不镜像 Execution：§6 目录里没有 execution_waiting_input 事件，
// WAITING_INPUT→RUNNING 虽有 execution_resumed 事件，但 §5.3 不含它，
// 所以 RUNNING→WAITING_INPUT→RUNNING 全程 Task 一步都不动。
func TestExecutionWaitingInputChurnProducesNoTaskEvent(t *testing.T) {
	churn := [][2]execution.State{{execution.Running, execution.WaitingInput}, {execution.WaitingInput, execution.Running}}
	cur := task.Running
	for _, h := range churn {
		if err := execution.Check(h[0], h[1], execution.Normal, execution.Evidence{}); err != nil {
			t.Fatalf("Execution %s→%s 自身应当合法: %v", h[0], h[1], err)
		}
		edge := execution.EventOf(h[0], h[1])
		if edge == execution.EventResumed {
			// 有事件，但不是 §5.3 的边沿 —— 恰好证明"有事件 ≠ 驱动 Task"。
			t.Logf("%s→%s 产生了 %s，继续验证它不驱动 Task", h[0], h[1], edge)
		}
		to, ok, _ := task.ProjectTaskState(cur, edge, false, false)
		if ok {
			t.Fatalf("Execution %s→%s（事件 %q）把 Task 从 %s 驱动到了 %s", h[0], h[1], edge, cur, to)
		}
	}
	for _, edge := range []string{"", execution.EventCreated, execution.EventOutputFlushed,
		execution.EventPaused, execution.EventResumed, "message_recorded", "notification_created", "session_synced"} {
		if to, ok, _ := task.ProjectTaskState(task.Running, edge, false, false); ok {
			t.Errorf("%q 不是 §5.3 的边沿，却把 Task 驱动到了 %s", edge, to)
		}
	}
}

// 只有批准的决议才推进 Task；拒绝/过期/取消留在原态（§5.3 括号里的"批准"）。
func TestUnapprovedResolutionDoesNotAdvanceTask(t *testing.T) {
	to, ok, rej := task.ProjectTaskState(task.WaitingApproval, task.EdgeApprovalResolved, false, false)
	if ok {
		t.Errorf("未批准的决议把 Task 推进到了 %s", to)
	}
	if !strings.Contains(rej.Reason, "批准") {
		t.Errorf("拒绝理由没说明原因: %q", rej.Reason)
	}
}

// §5.4：投影无合法目标时 Task 停在原态、不产生事件，但必须交出 Notification + Audit 的内容。
func TestProjectionRejectionCarriesAuditDetails(t *testing.T) {
	// PREPARING 没有进 TRANSFERRING 的边，所以 Handoff 已就绪也不能投影出 TRANSFERRING。
	to, ok, rej := task.ProjectTaskState(task.Preparing, task.EdgeFailed, true, true)
	if ok {
		t.Fatalf("PREPARING 投影出了 §5.1 不允许的 %s", to)
	}
	if to != "" || rej.TaskState != task.Preparing {
		t.Errorf("被拒绝的投影必须让 Task 停在原态: to=%q rej=%+v", to, rej)
	}
	if rej.ExecutionEvent != task.EdgeFailed || rej.Reason == "" {
		t.Errorf("§5.4 要求 details 带 execution_event 与非空 reason: %+v", rej)
	}
	b, err := json.Marshal(rej)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"task_state", "execution_event", "reason"} {
		if _, ok := got[k]; !ok {
			t.Errorf("§5.4 的 details_json 缺键 %s，实际 %s", k, b)
		}
	}

	// 全表扫描：任何被拒绝的投影都不得改状态，且必须给出解释。
	edges := []string{task.EdgeStarted, task.EdgeApprovalRequested, task.EdgeApprovalResolved,
		task.EdgeSucceeded, task.EdgeInterrupted, task.EdgeCancelled, task.EdgeFailed}
	for _, from := range allTaskStates {
		for _, edge := range edges {
			for _, handoff := range []bool{false, true} {
				to, ok, rej := task.ProjectTaskState(from, edge, handoff, true)
				if !ok {
					if to != "" || rej.Reason == "" || rej.TaskState != from || rej.ExecutionEvent != edge {
						t.Errorf("%s --%s(handoff=%v) 被拒但记录不完整: to=%q rej=%+v", from, edge, handoff, to, rej)
					}
					continue
				}
				if err := task.Check(from, to, task.Recovery, task.Evidence{}); err != nil {
					t.Errorf("%s --%s--> %s 绕过了 §5.1: %v", from, edge, to, err)
				}
			}
		}
	}
}

// §5.4 自己举的三个例子必须真的走拒绝路径（不得退化成 FAILED 去"顺手修正"）。
func TestContract54ExamplesAreRejected(t *testing.T) {
	for _, from := range []task.State{task.Queued, task.Paused, task.Blocked} {
		to, ok, rej := task.ProjectTaskState(from, task.EdgeFailed, true, true)
		if ok {
			t.Errorf("%s 收到 execution_failed(Handoff 已 READY) 竟投影出了 %s", from, to)
			continue
		}
		if rej.TaskState != from || rej.Reason == "" {
			t.Errorf("%s 的拒绝记录不完整: %+v", from, rej)
		}
	}
}

// §7 垂直链的 Task 半边：按事件顺序走一遍，投影与矩阵必须一致。
func TestVerticalChainTaskHalf(t *testing.T) {
	steps := []struct {
		edge string
		want task.State
	}{
		{"", task.Queued}, // task_queued 由 task 包自己驱动
		{"", task.Preparing},
		{task.EdgeStarted, task.Running},
		{task.EdgeApprovalRequested, task.WaitingApproval},
		{task.EdgeApprovalResolved, task.Running},
		{task.EdgeSucceeded, task.AgentCompleted},
		{"", task.WaitingAcceptance}, // task_waiting_acceptance
		{"", task.Accepted},          // task_accepted
	}
	cur := task.Created
	for i, s := range steps {
		if s.edge != "" {
			to, ok, rej := task.ProjectTaskState(cur, s.edge, false, true)
			if !ok || to != s.want {
				t.Fatalf("第 %d 步投影 %s --%s--> 期望 %s，实际 %s (ok=%v rej=%v)", i, cur, s.edge, s.want, to, ok, rej)
			}
		}
		if err := task.Check(cur, s.want, task.Normal, task.Evidence{}); err != nil {
			t.Fatalf("第 %d 步 %s→%s 不在 §5.1 矩阵里: %v", i, cur, s.want, err)
		}
		cur = s.want
	}
	if !task.Terminal(cur) {
		t.Fatalf("链尾 %s 应为终态", cur)
	}
	// AGENT_COMPLETED != ACCEPTED：AI 自称完成只能停在 AGENT_COMPLETED。
	if err := task.Check(task.AgentCompleted, task.Accepted, task.Normal, task.Evidence{}); err == nil {
		t.Error("AGENT_COMPLETED 直接跳到 ACCEPTED 被放行了")
	}
}

// C2 补登的三条 task_* 事件必须真的能驱动对应的迁移。
func TestPauseBlockResumeEventsMatchMatrix(t *testing.T) {
	for _, hop := range [][2]task.State{
		{task.Running, task.Paused}, {task.WaitingApproval, task.Paused}, {task.WaitingInput, task.Paused},
		{task.Blocked, task.Paused}, {task.WaitingAcceptance, task.Paused},
		{task.Created, task.Blocked}, {task.Queued, task.Blocked}, {task.Running, task.Blocked},
		{task.Preparing, task.Blocked}, {task.WaitingApproval, task.Blocked}, {task.WaitingInput, task.Blocked},
		{task.Transferring, task.Blocked},
	} {
		if err := task.Check(hop[0], hop[1], task.Normal, task.Evidence{}); err != nil {
			t.Errorf("%s→%s 应合法: %v", hop[0], hop[1], err)
		}
	}
	// resumed 的 from 只允许 PAUSED / BLOCKED（§6 那行的括号限定）。
	for _, from := range []task.State{task.Paused, task.Blocked} {
		if err := task.Check(from, task.Running, task.Normal, task.Evidence{}); err != nil {
			t.Errorf("%s→RUNNING（task_resumed）应合法: %v", from, err)
		}
		if err := task.Check(from, task.Queued, task.Normal, task.Evidence{}); err != nil {
			t.Errorf("%s→QUEUED（task_resumed）应合法: %v", from, err)
		}
	}
}
