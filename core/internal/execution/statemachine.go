// Package execution 拥有 Execution 状态机。矩阵逐行拄自
// specs/events/event-contract-v0.1.md §5.2。Execution 与 Task 是两套状态机，
// 不得互相镜像（ADR-014）。
package execution

import "errors"

type State string

const (
	Created         State = "CREATED"
	Starting        State = "STARTING"
	Running         State = "RUNNING"
	WaitingApproval State = "WAITING_APPROVAL"
	WaitingInput    State = "WAITING_INPUT"
	Paused          State = "PAUSED"
	Transferring    State = "TRANSFERRING"
	Succeeded       State = "SUCCEEDED"
	Failed          State = "FAILED"
	Cancelled       State = "CANCELLED"
	Interrupted     State = "INTERRUPTED"
	Unknown         State = "UNKNOWN"
)

var ErrIllegalTransition = errors.New("execution: illegal state transition")

type Occasion int

const (
	Normal Occasion = iota
	Recovery
)

type Evidence struct{ Reason string }

// TriggerType 记录为什么出现这次执行（领域模型「Execution Trigger Type」）。
type TriggerType string

const (
	TriggerInitial      TriggerType = "INITIAL"
	TriggerRetry        TriggerType = "RETRY"
	TriggerHandoff      TriggerType = "HANDOFF"
	TriggerResume       TriggerType = "RESUME"
	TriggerManualSwitch TriggerType = "MANUAL_SWITCH"
	TriggerAutoSwitch   TriggerType = "AUTO_SWITCH"
	TriggerReview       TriggerType = "REVIEW"
	TriggerRecovery     TriggerType = "RECOVERY"
)

func (t TriggerType) Valid() bool {
	switch t {
	case TriggerInitial, TriggerRetry, TriggerHandoff, TriggerResume,
		TriggerManualSwitch, TriggerAutoSwitch, TriggerReview, TriggerRecovery:
		return true
	}
	return false
}

var successors = map[State]map[State]bool{
	Created:         {Starting: false, Cancelled: false, Failed: false},
	Starting:        {Running: false, WaitingApproval: false, Failed: false, Cancelled: false, Interrupted: true},
	Running:         {WaitingApproval: false, WaitingInput: false, Paused: false, Transferring: false, Succeeded: false, Failed: false, Cancelled: false, Interrupted: true},
	WaitingApproval: {Running: false, Paused: false, Failed: false, Cancelled: false, Interrupted: true},
	WaitingInput:    {Running: false, Paused: false, Failed: false, Cancelled: false, Interrupted: true},
	Paused:          {Running: false, Cancelled: false, Interrupted: true},
	Transferring:    {Failed: false, Cancelled: false, Interrupted: true},
	Interrupted:     {Running: false, Failed: false, Cancelled: false},
	Succeeded:       {},
	Failed:          {},
	Cancelled:       {},
	Unknown:         {},
}

func Terminal(s State) bool {
	edges, ok := successors[s]
	return ok && len(edges) == 0 && s != Unknown
}

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
