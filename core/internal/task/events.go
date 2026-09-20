package task

import "encoding/json"

// 事件类型 = §6 目录中触发者为 task 的行。task_state_corrected 的触发者是 recovery，
// 不在本包定义。
const (
	EventCreated           = "task_created"
	EventQueued            = "task_queued"
	EventPreparing         = "task_preparing"
	EventRunning           = "task_running"
	EventWaitingApproval   = "task_waiting_approval"
	EventAgentCompleted    = "task_agent_completed"
	EventVerifying         = "task_verifying"
	EventWaitingAcceptance = "task_waiting_acceptance"
	EventAccepted          = "task_accepted"
	EventFailed            = "task_failed"
	EventCancelled         = "task_cancelled"
	EventInterrupted       = "task_interrupted"
	EventPaused            = "task_paused"
	EventBlocked           = "task_blocked"
	EventResumed           = "task_resumed"
	EventTransferring      = "task_transferring"
)

// auditRequired = §6 目录的 Audit 列 ✓：这些事件必须同事务带一条 audit_entries（ADR-012）。
var auditRequired = map[string]bool{
	EventWaitingApproval: true, EventAccepted: true, EventFailed: true,
	EventCancelled: true, EventInterrupted: true,
}

func RequiresAudit(eventType string) bool { return auditRequired[eventType] }

// Payload 把 payload 结构体转成 db.EventDraft.Payload 要的 map。
// 这里故意不管 "schema":1 —— 平台层在 appendEvent 里缺键就补（db 已确认，内部契约 §1.1 第 8~9 条同处理），
// 补在业务包等于把同一个保证写两遍。真表断言见 repository_test.go 的 payload_json 检查。
func Payload(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// 事件目录 §6 的任务域 payload，一个事件类型一个结构体。
type CreatedPayload struct {
	Title     string `json:"title"`
	Priority  string `json:"priority"`
	Mode      string `json:"mode"`
	RiskLevel string `json:"risk_level"`
	ProjectID string `json:"project_id,omitempty"`
}

type QueuedPayload struct {
	Priority string `json:"priority"`
}

type PreparingPayload struct {
	ExecutionID string `json:"execution_id"`
}

type RunningPayload struct {
	ExecutionID string `json:"execution_id"`
}

type WaitingApprovalPayload struct {
	ApprovalID string `json:"approval_id"`
}

type AgentCompletedPayload struct {
	ExecutionID string `json:"execution_id"`
	ResultCode  string `json:"result_code"`
}

type VerifyingPayload struct {
	CheckKind string `json:"check_kind"`
}

type WaitingAcceptancePayload struct {
	ExecutionID string `json:"execution_id"`
}

type AcceptedPayload struct {
	AcceptedBy string `json:"accepted_by"`
}

type FailedPayload struct {
	ErrorCode string `json:"error_code"`
}

type CancelledPayload struct {
	Reason string `json:"reason"`
}

type InterruptedPayload struct {
	ExecutionID string `json:"execution_id"`
	LeaseState  string `json:"lease_state"`
}

type PausedPayload struct {
	Reason string `json:"reason"`
}

type BlockedPayload struct {
	Reason    string `json:"reason"`
	BlockedOn string `json:"blocked_on"`
}

// ResumedPayload 的 from 同时覆盖 PAUSED→RUNNING 与 BLOCKED→RUNNING
// （§6 明确靠 from 区分，不拆成两个事件类型）。
type ResumedPayload struct {
	From State `json:"from"`
}

type TransferringPayload struct {
	HandoffID string `json:"handoff_id"`
}
