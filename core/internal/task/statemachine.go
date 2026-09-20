// Package task 拥有 Task 状态机。矩阵逐行拄自
// specs/events/event-contract-v0.1.md §5.1，不在矩阵内的迁移一律非法。
package task

import "errors"

type State string

const (
	Created           State = "CREATED"
	Queued            State = "QUEUED"
	Preparing         State = "PREPARING"
	Running           State = "RUNNING"
	WaitingApproval   State = "WAITING_APPROVAL"
	WaitingInput      State = "WAITING_INPUT"
	Blocked           State = "BLOCKED"
	Paused            State = "PAUSED"
	Transferring      State = "TRANSFERRING"
	AgentCompleted    State = "AGENT_COMPLETED"
	Verifying         State = "VERIFYING"
	WaitingAcceptance State = "WAITING_ACCEPTANCE"
	Accepted          State = "ACCEPTED"
	Failed            State = "FAILED"
	Cancelled         State = "CANCELLED"
	Interrupted       State = "INTERRUPTED"
	Unknown           State = "UNKNOWN"
)

// ErrIllegalTransition 命中时调用方不得"顺手修正"状态，只能写 Audit 后放弃本次迁移。
var ErrIllegalTransition = errors.New("task: illegal state transition")

// Occasion 区分正常运行与 Core 重启扫描（领域模型「Crash Recovery Query」）。
type Occasion int

const (
	Normal Occasion = iota
	Recovery
)

// Evidence 是 Recovery 进入或离开 Unknown 时必须携带的观测依据。
type Evidence struct{ Reason string }

// successors[from][to] 的 bool = true 表示该边只对 Recovery 开放（§5.1 recovery 列）。
// Unknown 的出边不在这里：它由 Check 单独按"仅 Recovery + 证据 → 任意确定态"处理。
var successors = map[State]map[State]bool{
	Created:           {Queued: false, Cancelled: false, Blocked: false},
	Queued:            {Preparing: false, Paused: false, Blocked: false, Cancelled: false, Failed: false, Created: true},
	Preparing:         {Running: false, WaitingApproval: false, Blocked: false, Paused: false, Cancelled: false, Failed: false, Queued: true, Interrupted: true},
	Running:           {WaitingApproval: false, WaitingInput: false, Blocked: false, Paused: false, Transferring: false, AgentCompleted: false, Failed: false, Cancelled: false, Interrupted: true},
	WaitingApproval:   {Running: false, Blocked: false, Paused: false, Cancelled: false, Failed: false, Interrupted: true},
	WaitingInput:      {Running: false, Blocked: false, Paused: false, Cancelled: false, Failed: false, Interrupted: true},
	Blocked:           {Queued: false, Running: false, Paused: false, Cancelled: false, Failed: false, Interrupted: true},
	Paused:            {Queued: false, Running: false, Cancelled: false},
	Transferring:      {Running: false, Queued: false, Blocked: false, Failed: false, Cancelled: false, Interrupted: true},
	AgentCompleted:    {Verifying: false, WaitingAcceptance: false, Running: false, Cancelled: false, Interrupted: true},
	Verifying:         {WaitingAcceptance: false, Running: false, Failed: false, Cancelled: false, Interrupted: true},
	WaitingAcceptance: {Accepted: false, Running: false, Verifying: false, Paused: false, Cancelled: false},
	Interrupted:       {Queued: false, Running: false, Transferring: false, Cancelled: false, Failed: false},
	Failed:            {Queued: false, Cancelled: false},
	Accepted:          {},
	Cancelled:         {},
	Unknown:           {},
}

// Terminal 报告该状态是否已无出边（§5.1 终态 = ACCEPTED、CANCELLED）。
func Terminal(s State) bool {
	edges, ok := successors[s]
	return ok && len(edges) == 0 && s != Unknown
}

// Check 是唯一的迁移裁决点。
func Check(from, to State, occ Occasion, ev Evidence) error {
	if from == Unknown {
		if occ != Recovery || ev.Reason == "" {
			return ErrIllegalTransition
		}
		if _, known := successors[to]; !known || to == Unknown {
			return ErrIllegalTransition
		}
		return nil
	}
	edges, ok := successors[from]
	if !ok {
		return ErrIllegalTransition
	}
	if to == Unknown {
		// 已定局的状态不得被改判成 UNKNOWN，否则等于丢历史（§12 红线）。
		if occ == Recovery && ev.Reason != "" && !Terminal(from) {
			return nil
		}
		return ErrIllegalTransition
	}
	recoveryOnly, ok := edges[to]
	if !ok || (recoveryOnly && occ != Recovery) {
		return ErrIllegalTransition
	}
	return nil
}
