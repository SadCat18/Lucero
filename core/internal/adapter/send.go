package adapter

import (
	"encoding/json"
	"fmt"
)

// SendResult is the session/send reply (contract §5 and CO-7). A synchronous success
// response alone does not prove delivery: CO-7's red line is that only the fields
// that can evidence the state may be recorded, and a reply that cannot evidence it
// must push the Execution to EXECUTION_STATE_UNKNOWN instead of being read as
// "queued".
type SendResult struct {
	// Accepted is mandatory and is the only field that may turn a reply into a
	// recorded success.
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason"`
	// QueuePosition belongs to delivery=queue only; set anywhere else it is the
	// silent downgrade §5 forbids.
	QueuePosition *int64 `json:"queue_position"`
	TurnID        string `json:"turn_id"`
	// ProviderExecutionID is what CO-7 asks start_now to return. Core does not
	// enforce it non-empty: CO-13 ruled that a Provider with no handle sends the
	// empty string, and that must never be read back as "never ran".
	ProviderExecutionID string          `json:"provider_execution_id"`
	IdempotencyHit      bool            `json:"idempotency_hit"`
	ProviderRaw         json.RawMessage `json:"provider_raw,omitempty"`
}

// Queue strategies (CO-7, 2026-09-19) decide what a queued send waits for.
// An absent strategy means interrupt_and_run, which is how old clients are read.
const (
	QueueInterruptAndRun = "interrupt_and_run"
	QueueAfterTurn       = "queue_after_turn"
)

var queueStrategies = []string{QueueInterruptAndRun, QueueAfterTurn}

// SendParams is the session/send request (§5 tri-state, §7.1 strategy, §4's
// mandatory idempotency key). Text is not validated here: an attachment-only send
// is legal and only the Adapter knows what its Provider accepts.
type SendParams struct {
	RemoteSessionID string `json:"remote_session_id"`
	Text            string `json:"text,omitempty"`
	// ExecutionID is Core's own id for the run this send starts. CO-8 keys the
	// turn/claimed + turn/dispatched evidence pair by it, and an Adapter can only
	// echo what it was given - §4's session/send params list has no such field,
	// which is CO-24. Attribution still runs by turn_ref on every frame (§4.2 裁定 ①),
	// so this is the one place the identity crosses: execution_id is ours, and a
	// Provider must not end up logging it on the other four notifications.
	// Left optional so an empty value keeps the honest outcome: no evidence frames,
	// and CO-8 leaves the Execution UNKNOWN rather than let Core infer a start from
	// the send reply alone.
	ExecutionID   string `json:"execution_id,omitempty"`
	Delivery      string `json:"delivery"`
	QueueStrategy string `json:"queue_strategy,omitempty"`
	// IdempotencyKey is §4's "Idempotency-Key 必填". The contract spells it
	// header-style but an ndjson frame has no headers, so it travels in params -
	// see CO-15 if 主控 wants it frame-level next to request_id instead.
	IdempotencyKey string `json:"idempotency_key"`
}

// Validate refuses the shapes that would lose work: no idempotency key (a retry
// then double-sends), an undeclared delivery state, and a queue strategy attached
// to a request that is not queuing - which is how a downgrade gets hidden.
func (p SendParams) Validate() error {
	if p.RemoteSessionID == "" {
		return &Error{Code: ErrInvalidParams, Message: "session/send has no remote_session_id"}
	}
	if !contains(deliveryModes, p.Delivery) {
		return &Error{Code: ErrDeliveryModeUnsupported,
			Message: fmt.Sprintf("session/send delivery %q is not one of %v (§5)", p.Delivery, deliveryModes)}
	}
	if p.IdempotencyKey == "" {
		return &Error{Code: ErrInvalidParams,
			Message: "session/send without an idempotency key: §4 makes it mandatory on this high-risk write, " +
				"a retry would otherwise send the message twice"}
	}
	if p.QueueStrategy != "" {
		if !contains(queueStrategies, p.QueueStrategy) {
			return &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("queue_strategy %q is not one of %v (CO-7)", p.QueueStrategy, queueStrategies)}
		}
		if p.Delivery != DeliveryQueue {
			return &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("queue_strategy %q with delivery %s: the strategy only says what a queue waits for (CO-7)",
					p.QueueStrategy, p.Delivery)}
		}
	}
	return nil
}

// Strategy is the CO-7 default made explicit, so no caller has to remember that an
// empty string means interrupt_and_run.
func (p SendParams) Strategy() string {
	if p.QueueStrategy == "" {
		return QueueInterruptAndRun
	}
	return p.QueueStrategy
}

// CreateSessionParams is the session/create request (CO-5). ClientRequestID is
// Core-generated UUIDv7 and mandatory: the call is non-idempotent by nature, so
// without it a retry creates a second remote session.
type CreateSessionParams struct {
	ClientRequestID string `json:"client_request_id"`
	ProjectID       string `json:"project_id,omitempty"`
	Title           string `json:"title,omitempty"`
	// BaseSHA is optional; CO-5 leaves the branch undefined for a Provider that
	// cannot align to a commit (CO-14).
	BaseSHA string `json:"base_sha,omitempty"`
}

// Validate refuses an empty client_request_id, and refuses a base_sha that is
// present but malformed. A Provider-side commit mismatch is a business answer, not
// a request error, so it is not checked here.
func (p CreateSessionParams) Validate() error {
	if p.ClientRequestID == "" {
		return &Error{Code: ErrInvalidParams,
			Message: "session/create without client_request_id: CO-5 makes the call idempotent by this key, " +
				"a retry would otherwise create a second session"}
	}
	if p.BaseSHA != "" && len(p.BaseSHA) != 40 {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("session/create base_sha %q is not a 40-hex git sha", p.BaseSHA)}
	}
	return nil
}

// CreateSessionResult is the CO-5 reply. Created false is not a failure: it is the
// idempotent hit that returns the session the first call already made.
type CreateSessionResult struct {
	RemoteSessionID string          `json:"remote_session_id"`
	Created         bool            `json:"created"`
	ProviderRaw     json.RawMessage `json:"provider_raw,omitempty"`
}

func (r CreateSessionResult) Validate() error {
	if r.RemoteSessionID == "" {
		return &Error{Code: ErrInvalidParams,
			Message: "session/create answered without remote_session_id; CO-5 has no third shape, " +
				"so this reply cannot be recorded as either created or hit"}
	}
	return nil
}

// ModelSelectParams picks up where §4's model/select row stops: that row names the
// behaviour ("对已有会话切模型；不支持必须显式报错") and no payload fields at all
// (CO-36), so the request keys off the only vocabulary this Adapter already
// publishes - model/list's model_key - instead of inventing a second name for the
// same thing.
type ModelSelectParams struct {
	RemoteSessionID string `json:"remote_session_id"`
	ModelKey        string `json:"model_key"`
}

// ModelSelectResult echoes the pair it applied. It carries no "switched" boolean:
// §4 has no such field, and an error reply is already the negative answer.
type ModelSelectResult struct {
	RemoteSessionID string `json:"remote_session_id"`
	ModelKey        string `json:"model_key"`
}

// Validate refuses the two shapes §4's model/select cannot answer with: no session to
// switch, or a switch with no model named.
func (p ModelSelectParams) Validate() error {
	if p.RemoteSessionID == "" || p.ModelKey == "" {
		return &Error{Code: ErrInvalidParams,
			Message: "model/select needs both remote_session_id and model_key (§4)"}
	}
	return nil
}

// Validate checks a reply against the delivery mode Core actually asked for.
// Running it against the *requested* mode rather than a reported one is the point:
// an Adapter that quietly queued a guide request is exactly the mis-delivery §5
// calls a P0-class accident, and the user must not be told their interruption
// happened.
func (s SendResult) Validate(delivery string) error {
	if !contains(deliveryModes, delivery) {
		return &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("delivery %q is not one of %v (§5)", delivery, deliveryModes)}
	}
	if !s.Accepted {
		if s.Reason == "" {
			return &Error{Code: ErrInvalidParams,
				Message: "session/send refused without a reason; CO-7 requires accepted=false to say why, " +
					"otherwise Core has nothing to record and the turn goes to EXECUTION_STATE_UNKNOWN"}
		}
		if s.QueuePosition != nil {
			return &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("session/send says accepted=false but holds queue_position %d (CO-7)",
					*s.QueuePosition)}
		}
		return nil
	}
	switch delivery {
	case DeliveryStartNow, DeliveryGuide:
		if s.QueuePosition != nil {
			return &Error{Code: ErrDeliveryModeUnsupported,
				Message: fmt.Sprintf("delivery %s was answered with queue_position %d: that is §5's silent downgrade to queue, "+
					"which must fail rather than be reported as success", delivery, *s.QueuePosition)}
		}
		if s.TurnID == "" {
			return &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("delivery %s accepted without a turn_id; nothing proves the turn exists (CO-7)", delivery)}
		}
	case DeliveryQueue:
		// A queued reply may legitimately carry a turn_id if the Provider assigns one
		// early; Core must still not project "started" from it.
		if s.QueuePosition == nil {
			return &Error{Code: ErrInvalidParams,
				Message: "delivery queue accepted without queue_position: CO-7 lets queue claim only \"queued + position\""}
		}
		if *s.QueuePosition < 1 {
			return &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("queue_position %d is not a position; 0 must not stand in for \"unknown\" (CO-7)",
					*s.QueuePosition)}
		}
	}
	return nil
}
