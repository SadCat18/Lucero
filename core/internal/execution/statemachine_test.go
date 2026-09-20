package execution

import (
	"errors"
	"testing"
)

type specRow struct{ normal, recovery []State }

// spec52 是从 event-contract-v0.1.md §5.2 独立手抄的第二份表，用来和 successors 对账。
var spec52 = map[State]specRow{
	Created:         {normal: []State{Starting, Cancelled, Failed}},
	Starting:        {normal: []State{Running, WaitingApproval, Failed, Cancelled}, recovery: []State{Interrupted}},
	Running:         {normal: []State{WaitingApproval, WaitingInput, Paused, Transferring, Succeeded, Failed, Cancelled}, recovery: []State{Interrupted}},
	WaitingApproval: {normal: []State{Running, Paused, Failed, Cancelled}, recovery: []State{Interrupted}},
	WaitingInput:    {normal: []State{Running, Paused, Failed, Cancelled}, recovery: []State{Interrupted}},
	Paused:          {normal: []State{Running, Cancelled}, recovery: []State{Running, Interrupted}},
	Transferring:    {normal: []State{Failed, Cancelled}, recovery: []State{Interrupted}},
	Interrupted:     {normal: []State{Running, Failed, Cancelled}},
	Succeeded:       {},
	Failed:          {},
	Cancelled:       {},
	Unknown:         {},
}

var allStates = []State{Created, Starting, Running, WaitingApproval, WaitingInput, Paused,
	Transferring, Succeeded, Failed, Cancelled, Interrupted, Unknown}

func TestMatrixMatchesContract52(t *testing.T) {
	if len(successors) != len(allStates) {
		t.Fatalf("successors %d 行，§5.2 %d 个状态", len(successors), len(allStates))
	}
	legal := 0
	for _, from := range allStates {
		edges, ok := successors[from]
		if !ok {
			t.Fatalf("successors 缺 %s 行", from)
		}
		row := spec52[from]
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
	t.Logf("§5.2 合法迁移覆盖 %d 条", legal)
}

func TestIllegalTransitions(t *testing.T) {
	cases := []struct {
		from, to State
		occ      Occasion
		why      string
	}{
		{Succeeded, Running, Normal, "SUCCEEDED 是终态"},
		{Succeeded, Failed, Normal, "SUCCEEDED 是终态"},
		{Succeeded, Interrupted, Recovery, "恢复期也不能改判已成功"},
		{Failed, Running, Normal, "Execution 的 FAILED 是终态，重来靠新 Execution"},
		{Failed, Starting, Recovery, "同上"},
		{Cancelled, Running, Normal, "CANCELLED 是终态"},
		{Cancelled, Created, Normal, "CANCELLED 是终态"},
		{Created, Running, Normal, "必须先 STARTING"},
		{Created, Succeeded, Normal, "没跑过不可能成功"},
		{Running, Created, Normal, "不能退回创建前"},
		{Paused, Failed, Normal, "PAUSED 只能回 RUNNING 或 CANCELLED"},
		{Transferring, Running, Normal, "转交目标端不是 RUNNING"},
		{WaitingApproval, Succeeded, Normal, "等审批期间不可能成功"},
		{WaitingInput, Succeeded, Normal, "等输入期间不可能成功"},
		{Interrupted, WaitingApproval, Normal, "INTERRUPTED 只能去 RUNNING/FAILED/CANCELLED"},
		{Starting, Interrupted, Normal, "INTERRUPTED 只能由恢复扫描写入"},
		{Running, Running, Normal, "自环不是迁移"},
		{Failed, Failed, Recovery, "终态自环也不行"},
		{State("QUOTA_EXHAUSTED"), Failed, Normal, "那是 result_code，不是状态"},
		{Running, State("DONE"), Normal, "表外终点"},
	}
	for _, c := range cases {
		if err := Check(c.from, c.to, c.occ, Evidence{Reason: "伪造的证据也不该管用"}); !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("%s→%s (occ=%d) 应非法（%s），实际 %v", c.from, c.to, c.occ, c.why, err)
		}
	}
}

func TestUnknownIsNotCoercible(t *testing.T) {
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
		// §56 Crash Recovery Query 扫的五个状态都在这个循环里。
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
	// 红线：UNKNOWN 不得被转成 FAILED/0/Offline。这里断言包里根本没有这种目标取值。
	for _, bogus := range []State{"FAILED", "0", "Offline", "OFFLINE", ""} {
		if err := Check(Unknown, bogus, Normal, Evidence{}); !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("UNKNOWN 被自动转成了 %q，实际 %v", bogus, err)
		}
	}
}

func TestTerminalStates(t *testing.T) {
	for _, s := range []State{Succeeded, Failed, Cancelled} {
		if !Terminal(s) {
			t.Errorf("%s 应为终态", s)
		}
	}
	for _, s := range allStates {
		if s == Succeeded || s == Failed || s == Cancelled {
			continue
		}
		if Terminal(s) {
			t.Errorf("%s 不该被判成终态", s)
		}
	}
}

// TestProviderSwitchKeepsTask 覆盖 ADR-013/ADR-015 的结构性保证：
// 同一个 Task 先后挂在三家软件的三个 Session 上，TaskID 不变、AttemptNo 递增、
// 前驱链可追；Task 侧不需要（也不允许）持有 provider/session 身份。
func TestProviderSwitchKeepsTask(t *testing.T) {
	first := Execution{ID: "exec-1", TaskID: "task-1", AdapterInstanceID: "codex-1",
		SessionID: "sess-codex-a", AttemptNo: 1, Status: Created, TriggerType: TriggerInitial}

	second := first.NewAttempt(TriggerManualSwitch, "qoder-1", "sess-qoder-b")
	second.ID = "exec-2"
	third := second.NewAttempt(TriggerHandoff, "opencode-1", "sess-opencode-c")

	if third.TaskID != first.TaskID || second.TaskID != first.TaskID {
		t.Fatalf("换 Provider 产生了新 Task: %q %q %q", first.TaskID, second.TaskID, third.TaskID)
	}
	if second.AttemptNo != 2 || third.AttemptNo != 3 {
		t.Errorf("attempt_no 没递增: %d %d", second.AttemptNo, third.AttemptNo)
	}
	if second.PredecessorExecutionID != "exec-1" || third.PredecessorExecutionID != "exec-2" {
		t.Errorf("前驱链断了: %q %q", second.PredecessorExecutionID, third.PredecessorExecutionID)
	}
	sessions := map[string]bool{"sess-codex-a": true, second.SessionID: true, third.SessionID: true}
	if len(sessions) != 3 {
		t.Errorf("三个 Session 应有三个不同值，实际 %v", sessions)
	}
	if third.TriggerType != TriggerHandoff || !third.TriggerType.Valid() {
		t.Errorf("trigger_type 丢失或非法: %q", third.TriggerType)
	}
	if !first.TriggerType.Valid() || TriggerType("NOPE").Valid() {
		t.Error("TriggerType 词表校验失效（§23 八个值）")
	}
}
