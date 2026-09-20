package execution

import (
	"sort"
	"strings"
	"testing"
)

// §6 目录里触发者为 execution 的 9 行，独立手抄第二份。与包内常量双向对账：
// 少了 = 事件没实现；多了 = §9.1 的 P0（未登记事件入库）。
// execution_approval_requested / _resolved 的触发者是 approval，不在此列。
var catalog6 = []struct {
	eventType string
	audit     bool
	keys      []string
	payload   any
}{
	{EventCreated, false, []string{"task_id", "adapter_instance_id", "model_id", "attempt_no", "trigger_type"},
		CreatedPayload{TaskID: "t", AdapterInstanceID: "a", ModelID: "m", AttemptNo: 1, TriggerType: TriggerInitial}},
	{EventStarted, false, []string{"provider_execution_id", "model_snapshot_sha256"},
		StartedPayload{ProviderExecutionID: "p", ModelSnapshotSHA256: "sha"}},
	{EventOutputFlushed, false, []string{"batch_first_seq", "batch_last_seq", "char_count", "channel"},
		OutputFlushedPayload{BatchFirstSeq: 1, BatchLastSeq: 2, CharCount: 3, Channel: "stdout"}},
	{EventPaused, false, []string{"reason"}, PausedPayload{Reason: "用户暂停"}},
	{EventResumed, false, []string{"from"}, ResumedPayload{From: Paused}},
	{EventSucceeded, false, []string{"result_code", "error_code", "usage"},
		ResultPayload{ResultCode: "OK", ErrorCode: "", Usage: map[string]any{"tokens": 1}}},
	{EventFailed, true, []string{"result_code", "error_code", "usage"},
		ResultPayload{ResultCode: "", ErrorCode: "E", Usage: map[string]any{"tokens": 1}}},
	{EventCancelled, false, []string{"result_code", "error_code", "usage"},
		ResultPayload{ResultCode: "CANCELLED", Usage: map[string]any{"tokens": 1}}},
	{EventInterrupted, true, []string{"result_code", "error_code", "usage"},
		ResultPayload{ErrorCode: "LEASE_EXPIRED", Usage: map[string]any{"tokens": 1}}},
}

func TestEventCatalogMatchesContract6(t *testing.T) {
	if len(catalog6) != 9 {
		t.Fatalf("§6 的 execution 行应有 9 条，实际 %d", len(catalog6))
	}
	mine := []string{EventCreated, EventStarted, EventOutputFlushed, EventPaused, EventResumed,
		EventSucceeded, EventFailed, EventCancelled, EventInterrupted}
	if len(mine) != len(catalog6) {
		t.Fatalf("包内声明 %d 个事件类型，目录 %d 行", len(mine), len(catalog6))
	}
	byType := map[string]struct{ audit bool }{}
	for _, c := range catalog6 {
		if _, dup := byType[c.eventType]; dup {
			t.Fatalf("测试表重复登记 %s", c.eventType)
		}
		byType[c.eventType] = struct{ audit bool }{c.audit}
	}
	for _, e := range mine {
		row, ok := byType[e]
		if !ok {
			t.Errorf("%s 没在 §6 登记（§9.1 P0：未登记事件不得入库）", e)
			continue
		}
		if !strings.HasPrefix(e, "execution_") {
			t.Errorf("%s 不符合 §1 的 <domain>_<action> 命名", e)
		}
		if RequiresAudit(e) != row.audit {
			t.Errorf("%s 的 Audit 标记与 §6 的 ✓ 列不符（实现=%v 目录=%v）", e, RequiresAudit(e), row.audit)
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
		// 不含 schema：那个键由平台层 appendEvent 补，不在业务包重复保证。
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s payload 键 = %v，§6 要求 %v", c.eventType, got, want)
		}
	}
}

func TestEventOfMatchesCatalog(t *testing.T) {
	cases := []struct {
		from, to State
		want     string
	}{
		{Created, Starting, ""}, // execution_created 属于建行，不属于状态迁移
		{Starting, Running, EventStarted},
		{Running, Succeeded, EventSucceeded},
		{Running, Failed, EventFailed},
		{WaitingInput, Failed, EventFailed},
		{Transferring, Cancelled, EventCancelled},
		{Running, Interrupted, EventInterrupted},
		{Running, Paused, EventPaused},
		{Paused, Running, EventResumed},
		{WaitingInput, Running, EventResumed},
		{Interrupted, Running, EventResumed},
		{Running, WaitingInput, ""}, // §6 目录里没有 waiting_input 事件
		{Running, Transferring, ""},
		{WaitingApproval, Running, ""}, // 推进事件由 approval 包发，不是本包
	}
	for _, c := range cases {
		if got := EventOf(c.from, c.to); got != c.want {
			t.Errorf("EventOf(%s,%s) = %q，应为 %q", c.from, c.to, got, c.want)
		}
	}
}
