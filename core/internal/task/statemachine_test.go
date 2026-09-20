package task

import (
	"errors"
	"testing"
)

type specRow struct{ normal, recovery []State }

// spec51 是从 event-contract-v0.1.md §5.1 独立手抄的第二份表，用来和 successors
// 逐行对账。两张表任何一处不一致都算缺陷。
var spec51 = map[State]specRow{
	Created:           {normal: []State{Queued, Cancelled, Blocked}},
	Queued:            {normal: []State{Preparing, Paused, Blocked, Cancelled, Failed}, recovery: []State{Created}},
	Preparing:         {normal: []State{Running, WaitingApproval, Blocked, Paused, Cancelled, Failed}, recovery: []State{Queued, Interrupted}},
	Running:           {normal: []State{WaitingApproval, WaitingInput, Blocked, Paused, Transferring, AgentCompleted, Failed, Cancelled}, recovery: []State{Interrupted}},
	WaitingApproval:   {normal: []State{Running, Blocked, Paused, Cancelled, Failed}, recovery: []State{Interrupted}},
	WaitingInput:      {normal: []State{Running, Blocked, Paused, Cancelled, Failed}, recovery: []State{Interrupted}},
	Blocked:           {normal: []State{Queued, Running, Paused, Cancelled, Failed}, recovery: []State{Queued, Interrupted}},
	Paused:            {normal: []State{Queued, Running, Cancelled}, recovery: []State{Queued}},
	Transferring:      {normal: []State{Running, Queued, Blocked, Failed, Cancelled}, recovery: []State{Interrupted}},
	AgentCompleted:    {normal: []State{Verifying, WaitingAcceptance, Running, Cancelled}, recovery: []State{Interrupted}},
	Verifying:         {normal: []State{WaitingAcceptance, Running, Failed, Cancelled}, recovery: []State{Interrupted}},
	WaitingAcceptance: {normal: []State{Accepted, Running, Verifying, Paused, Cancelled}},
	Interrupted:       {normal: []State{Queued, Running, Transferring, Cancelled, Failed}},
	Failed:            {normal: []State{Queued, Cancelled}},
	Accepted:          {},
	Cancelled:         {},
	Unknown:           {},
}

var allStates = []State{Created, Queued, Preparing, Running, WaitingApproval, WaitingInput,
	Blocked, Paused, Transferring, AgentCompleted, Verifying, WaitingAcceptance,
	Accepted, Failed, Cancelled, Interrupted, Unknown}

// TestMatrixMatchesContract51 为 §5.1 的每一条合法迁移各跑一次用例：
// 正常边在 Normal 与 Recovery 两种场合都必须放行（Recovery 是正常运行之上追加的边集，
// 否则恢复扫描无法把 INTERRUPTED 推回 QUEUED/RUNNING），recovery-only 边在 Normal 下必须拒绝。
func TestMatrixMatchesContract51(t *testing.T) {
	if len(successors) != len(allStates) {
		t.Fatalf("successors %d 行，§5.1 %d 个状态", len(successors), len(allStates))
	}
	legal := 0
	for _, from := range allStates {
		edges, ok := successors[from]
		if !ok {
			t.Fatalf("successors 缺 %s 行", from)
		}
		row := spec51[from]
		want := map[State]bool{}
		for _, to := range row.normal {
			want[to] = false
		}
		for _, to := range row.recovery {
			if _, already := want[to]; !already {
				want[to] = true
			}
		}
		if from == Unknown {
			continue // UNKNOWN 的出边由 Check 特判，见 TestUnknownIsNotCoercible
		}
		if len(want) != len(edges) {
			t.Errorf("%s: 边数 期望 %d 实际 %d (%v vs %v)", from, len(want), len(edges), want, edges)
		}
		for to, recoveryOnly := range want {
			legal++
			got, ok := edges[to]
			if !ok || got != recoveryOnly {
				t.Errorf("%s→%s: 应为 recoveryOnly=%v，实际 ok=%v recoveryOnly=%v", from, to, recoveryOnly, ok, got)
			}
			if err := Check(from, to, Recovery, Evidence{Reason: "重启扫描"}); err != nil {
				t.Errorf("%s→%s Recovery 被拒: %v", from, to, err)
			}
			if recoveryOnly {
				if err := Check(from, to, Normal, Evidence{}); !errors.Is(err, ErrIllegalTransition) {
					t.Errorf("%s→%s 只对 Recovery 开放，Normal 却放行: %v", from, to, err)
				}
				continue
			}
			if err := Check(from, to, Normal, Evidence{}); err != nil {
				t.Errorf("%s→%s Normal 被拒: %v", from, to, err)
			}
		}
	}
	t.Logf("§5.1 合法迁移覆盖 %d 条", legal)
}

func TestIllegalTransitions(t *testing.T) {
	cases := []struct {
		from, to State
		occ      Occasion
		why      string
	}{
		{Accepted, Running, Normal, "ACCEPTED 是终态"},
		{Accepted, Queued, Normal, "ACCEPTED 是终态"},
		{Accepted, Failed, Recovery, "恢复期也不能把已验收的任务改回失败"},
		{Cancelled, Created, Normal, "CANCELLED 是终态"},
		{Cancelled, Running, Recovery, "CANCELLED 是终态"},
		{Failed, Running, Normal, "FAILED 只能回 QUEUED 或 CANCELLED"},
		{Failed, Accepted, Normal, "失败的任务不能被直接验收"},
		{Running, Accepted, Normal, "必须经 AGENT_COMPLETED→(VERIFYING)→WAITING_ACCEPTANCE"},
		{AgentCompleted, Accepted, Normal, "AGENT_COMPLETED != ACCEPTED"},
		{Created, Running, Normal, "CREATED 不能跳过 QUEUED"},
		{Created, Accepted, Normal, "刚建的任务不可能已验收"},
		{Queued, Running, Normal, "QUEUED 必须先 PREPARING"},
		{Queued, Created, Normal, "QUEUED→CREATED 只对 Recovery 开放"},
		{WaitingApproval, AgentCompleted, Normal, "等审批期间不得宣布完成"},
		{WaitingInput, WaitingApproval, Normal, "矩阵未授权"},
		{Paused, Failed, Normal, "PAUSED 没有 FAILED 出边"},
		{Interrupted, Accepted, Normal, "中断后必须先重新跑"},
		{Running, Interrupted, Normal, "INTERRUPTED 只能由恢复扫描写入"},
		{Running, State("SUCCEEDED"), Normal, "Execution 的状态不是 Task 的状态"},
		{Created, Created, Normal, "自环不是迁移"},
		{Running, Running, Normal, "自环不是迁移"},
		{Accepted, Accepted, Recovery, "终态自环也不行"},
		{State("BROKEN"), Running, Normal, "表外起点"},
		{Running, State("DONE"), Normal, "表外终点"},
	}
	for _, c := range cases {
		if err := Check(c.from, c.to, c.occ, Evidence{Reason: "伪造的证据也不该管用"}); !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("%s→%s (occ=%d) 应非法（%s），实际 %v", c.from, c.to, c.occ, c.why, err)
		}
	}
}

// TestUnknownIsNotCoercible 守住 §5.1 UNKNOWN 行与 §12 红线"无法确认状态时却显示确定值"。
func TestUnknownIsNotCoercible(t *testing.T) {
	// UNKNOWN 不在任何状态的正常或 recovery 出边里：正常运行不会"顺手"把任务标成未知。
	for from, edges := range successors {
		if _, ok := edges[Unknown]; ok {
			t.Errorf("%s 的出边里出现了 UNKNOWN", from)
		}
		if from == Unknown {
			continue
		}
		if err := Check(from, Unknown, Normal, Evidence{}); !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("%s→UNKNOWN Normal 必须非法，实际 %v", from, err)
		}
		if err := Check(from, Unknown, Recovery, Evidence{}); !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("%s→UNKNOWN 无证据时必须非法，实际 %v", from, err)
		}
		err := Check(from, Unknown, Recovery, Evidence{Reason: "lease 过期且 process 已退出"})
		if Terminal(from) {
			if !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("%s 是终态，不得被改判成 UNKNOWN（等于丢历史）", from)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s→UNKNOWN 带证据的 Recovery 应放行，实际 %v", from, err)
		}
	}
	// 离开 UNKNOWN 同样只有 Recovery + 证据一条路，且目标必须是确定态。
	for _, to := range allStates {
		if err := Check(Unknown, to, Normal, Evidence{}); !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("UNKNOWN→%s Normal 必须非法，实际 %v", to, err)
		}
		if err := Check(Unknown, to, Recovery, Evidence{}); !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("UNKNOWN→%s 无证据时必须非法，实际 %v", to, err)
		}
		err := Check(Unknown, to, Recovery, Evidence{Reason: "provider_execution_id 仍在跑"})
		if to == Unknown {
			if !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("UNKNOWN→UNKNOWN 应非法，实际 %v", err)
			}
			continue
		}
		if err != nil {
			t.Errorf("UNKNOWN→%s 带证据的 Recovery 应放行，实际 %v", to, err)
		}
	}
	if Terminal(Unknown) {
		t.Error("UNKNOWN 不是终态，恢复引擎必须能带证据离开它")
	}
	// UNKNOWN 的字面值就是 UNKNOWN，包内没有任何把它转成 FAILED/0/Offline 的入口。
	if string(Unknown) != "UNKNOWN" {
		t.Errorf("UNKNOWN 被改写成了 %q", string(Unknown))
	}
}

func TestTerminalStates(t *testing.T) {
	for _, s := range []State{Accepted, Cancelled} {
		if !Terminal(s) {
			t.Errorf("%s 应为终态", s)
		}
	}
	for _, s := range allStates {
		if s == Accepted || s == Cancelled {
			continue
		}
		if Terminal(s) {
			t.Errorf("%s 不该被判成终态", s)
		}
	}
}
