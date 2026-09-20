package task

import (
	"sort"
	"strings"
	"testing"
)

// §6 目录里触发者为 task 的 16 行，独立手抄第三份。与包内常量双向对账：
// 少了 = 事件没实现；多了 = §9.1 的 P0（未登记事件入库）。
var catalog6 = []struct {
	eventType string
	audit     bool
	keys      []string
	payload   any
}{
	{EventCreated, false, []string{"title", "priority", "mode", "risk_level", "project_id"},
		CreatedPayload{Title: "t", Priority: "NORMAL", Mode: "EXECUTE", RiskLevel: "LOW", ProjectID: "p"}},
	{EventQueued, false, []string{"priority"}, QueuedPayload{Priority: "HIGH"}},
	{EventPreparing, false, []string{"execution_id"}, PreparingPayload{ExecutionID: "e"}},
	{EventRunning, false, []string{"execution_id"}, RunningPayload{ExecutionID: "e"}},
	{EventWaitingApproval, true, []string{"approval_id"}, WaitingApprovalPayload{ApprovalID: "a"}},
	{EventAgentCompleted, false, []string{"execution_id", "result_code"},
		AgentCompletedPayload{ExecutionID: "e", ResultCode: "OK"}},
	{EventVerifying, false, []string{"check_kind"}, VerifyingPayload{CheckKind: "tests"}},
	{EventWaitingAcceptance, false, []string{"execution_id"}, WaitingAcceptancePayload{ExecutionID: "e"}},
	{EventAccepted, true, []string{"accepted_by"}, AcceptedPayload{AcceptedBy: "local-user"}},
	{EventFailed, true, []string{"error_code"}, FailedPayload{ErrorCode: "E"}},
	{EventCancelled, true, []string{"reason"}, CancelledPayload{Reason: "用户取消"}},
	{EventInterrupted, true, []string{"execution_id", "lease_state"},
		InterruptedPayload{ExecutionID: "e", LeaseState: "EXPIRED"}},
	{EventPaused, false, []string{"reason"}, PausedPayload{Reason: "用户暂停"}},
	{EventBlocked, false, []string{"reason", "blocked_on"}, BlockedPayload{Reason: "等依赖", BlockedOn: "task-dep"}},
	{EventResumed, false, []string{"from"}, ResumedPayload{From: Paused}},
	{EventTransferring, false, []string{"handoff_id"}, TransferringPayload{HandoffID: "h"}},
}

func declared() map[string]bool {
	out := map[string]bool{}
	for _, c := range catalog6 {
		if out[c.eventType] {
			panic("测试表里重复登记了 " + c.eventType)
		}
		out[c.eventType] = true
	}
	return out
}

func TestEventCatalogMatchesContract6(t *testing.T) {
	want := declared()
	if len(want) != 16 {
		t.Fatalf("§6 的 task 行应有 16 条，实际 %d", len(want))
	}
	mine := map[string]bool{
		EventCreated: true, EventQueued: true, EventPreparing: true, EventRunning: true,
		EventWaitingApproval: true, EventAgentCompleted: true, EventVerifying: true,
		EventWaitingAcceptance: true, EventAccepted: true, EventFailed: true, EventCancelled: true,
		EventInterrupted: true, EventPaused: true, EventBlocked: true, EventResumed: true,
		EventTransferring: true,
	}
	for e := range mine {
		if !strings.HasPrefix(e, "task_") {
			t.Errorf("%s 不符合 §1 的 <domain>_<action> 命名", e)
		}
		if !want[e] {
			t.Errorf("%s 没在 §6 登记（§9.1 P0：未登记事件不得入库）", e)
		}
	}
	for e := range want {
		if !mine[e] {
			t.Errorf("§6 登记了 %s，包里却没有", e)
		}
	}
}

// TestPayloadKeysMatchCatalog 证明每个事件类型的 payload 键与 §6 的"payload 关键字段"列一致。
func TestPayloadKeysMatchCatalog(t *testing.T) {
	for _, c := range catalog6 {
		m, err := Payload(c.payload)
		if err != nil {
			t.Fatalf("%s: %v", c.eventType, err)
		}
		got := []string{}
		for k := range m {
			got = append(got, k)
		}
		sort.Strings(got)
		want := append([]string{}, c.keys...)
		sort.Strings(want)
		// 不含 schema：那个键由平台层 appendEvent 补，见 repository_test.go 的 payload_json 断言。
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s payload 键 = %v，§6 要求 %v", c.eventType, got, want)
		}
		if RequiresAudit(c.eventType) != c.audit {
			t.Errorf("%s 的 Audit 标记与 §6 的 ✓ 列不符（实现=%v 目录=%v）",
				c.eventType, RequiresAudit(c.eventType), c.audit)
		}
	}
}
