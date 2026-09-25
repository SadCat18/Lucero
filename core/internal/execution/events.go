package execution

import (
	"encoding/json"
)

// 事件类型 = §6 目录中触发者为 execution 的行。execution_approval_requested /
// execution_approval_resolved 的触发者是 approval（audit 角色），不在本包定义。
const (
	EventCreated       = "execution_created"
	EventStarted       = "execution_started"
	EventOutputFlushed = "execution_output_flushed"
	EventPaused        = "execution_paused"
	EventResumed       = "execution_resumed"
	EventStarting      = "execution_starting" // REQ-DB-4（09-24）：CREATED→STARTING 由它表达，§6 那行 payload 只有 `from`
	EventSucceeded     = "execution_succeeded"
	EventFailed        = "execution_failed"
	EventCancelled     = "execution_cancelled"
	EventInterrupted   = "execution_interrupted"
)

// EventOf 返回该迁移对应的事件类型；"" 表示该迁移不由本包发事件
// （WAITING_APPROVAL→RUNNING 由 approval 包发 execution_approval_resolved；
// RUNNING→WAITING_INPUT 在 §6 目录里根本没有事件）。
// 事件存在与否与"是否驱动 Task"是两件事：§5.3 之外的事件一律不驱动 Task。
func EventOf(from, to State) string {
	switch to {
	case Starting:
		return EventStarting
	case Succeeded:
		return EventSucceeded
	case Failed:
		return EventFailed
	case Cancelled:
		return EventCancelled
	case Interrupted:
		return EventInterrupted
	case Paused:
		return EventPaused
	case Running:
		switch from {
		case Starting:
			return EventStarted
		case Paused, WaitingInput, Interrupted:
			return EventResumed
		}
	}
	return ""
}

// auditRequired = §6 目录的 Audit 列 ✓(failed/interrupted)：这两个事件必须同事务带一条 audit_entries。
var auditRequired = map[string]bool{EventFailed: true, EventInterrupted: true}

func RequiresAudit(eventType string) bool { return auditRequired[eventType] }

// Payload 把 payload 结构体转成 db.EventDraft.Payload 要的 map。
// "schema":1 由平台层在 appendEvent 里缺键时补（db 已确认），本包不再重复保证。
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

// 事件目录 §6 的执行域 payload。一个事件类型一个结构体。
type CreatedPayload struct {
	TaskID            string      `json:"task_id"`
	AdapterInstanceID string      `json:"adapter_instance_id"`
	ModelID           string      `json:"model_id,omitempty"`
	AttemptNo         int64       `json:"attempt_no"`
	TriggerType       TriggerType `json:"trigger_type"`
}

type StartedPayload struct {
	ProviderExecutionID string `json:"provider_execution_id,omitempty"`
	ModelSnapshotSHA256 string `json:"model_snapshot_sha256"`
}

type OutputFlushedPayload struct {
	BatchFirstSeq int64  `json:"batch_first_seq"`
	BatchLastSeq  int64  `json:"batch_last_seq"`
	CharCount     int64  `json:"char_count"`
	Channel       string `json:"channel"`
}

type PausedPayload struct {
	Reason string `json:"reason"`
}

// ResumedPayload 的 from 区分 PAUSED / WAITING_INPUT / INTERRUPTED 三种恢复来源。
type ResumedPayload struct {
	From State `json:"from"`
}

// StartingPayload 是 §6 的 `execution_starting` 那一行：目录里只登记了 `from` 一个键。
// 不复用 ResumedPayload（键集今天相同是巧合，§6 是两行，以后改一行不该牵动另一行）。
type StartingPayload struct {
	From State `json:"from"`
}

// ResultPayload 同用于 succeeded / failed / cancelled / interrupted；
// 区分靠 event_type，不靠压缩事件类型（§6 表下注）。三个键恒在场：
// 空串表示"没有"，与 omitempty 的"键不存在"是两回事，别让读端两种都要防。
type ResultPayload struct {
	ResultCode string         `json:"result_code"`
	ErrorCode  string         `json:"error_code"`
	Usage      map[string]any `json:"usage"`
}
