package adapter

import (
	"strings"
	"testing"
)

// TestSendResultValidate pins CO-7: the synchronous reply only
// counts as delivered when it can evidence that state, and a downgrade from guide
// or start_now to queue must fail loudly instead of being reported as success.
func TestSendResultValidate(t *testing.T) {
	cases := []struct {
		name     string
		res      SendResult
		delivery string
		wantErr  ErrorCode // 0 means the reply is legal
		wantText string    // substring the refusal must contain
	}{
		{
			name:     "start_now with a turn",
			res:      SendResult{Accepted: true, TurnID: "turn-1", ProviderExecutionID: "exec-1"},
			delivery: DeliveryStartNow,
		},
		{
			name:     "start_now without any proof of a turn",
			res:      SendResult{Accepted: true, ProviderExecutionID: "exec-1"},
			delivery: DeliveryStartNow,
			wantErr:  ErrInvalidParams, wantText: "turn_id",
		},
		{
			name:     "start_now answered with a queue position is the §5 downgrade",
			res:      SendResult{Accepted: true, TurnID: "turn-1", QueuePosition: i64(4)},
			delivery: DeliveryStartNow,
			wantErr:  ErrDeliveryModeUnsupported, wantText: "silent downgrade",
		},
		{
			name:     "guide answered with a queue position",
			res:      SendResult{Accepted: true, TurnID: "turn-1", QueuePosition: i64(4)},
			delivery: DeliveryGuide,
			wantErr:  ErrDeliveryModeUnsupported, wantText: "silent downgrade",
		},
		{
			name:     "guide opens a new turn so it needs a turn_id",
			res:      SendResult{Accepted: true, Reason: "interrupted and restarted"},
			delivery: DeliveryGuide,
			wantErr:  ErrInvalidParams, wantText: "turn_id",
		},
		{
			name:     "queue with a position",
			res:      SendResult{Accepted: true, QueuePosition: i64(3)},
			delivery: DeliveryQueue,
		},
		{
			name:     "queue without a position claims nothing",
			res:      SendResult{Accepted: true},
			delivery: DeliveryQueue,
			wantErr:  ErrInvalidParams, wantText: "queue_position",
		},
		{
			name:     "queue position 0 is not a position",
			res:      SendResult{Accepted: true, QueuePosition: i64(0)},
			delivery: DeliveryQueue,
			wantErr:  ErrInvalidParams, wantText: "unknown",
		},
		{
			name:     "queue may carry an early turn_id without meaning started",
			res:      SendResult{Accepted: true, QueuePosition: i64(1), TurnID: "turn-9"},
			delivery: DeliveryQueue,
		},
		{
			name:     "refusal with a reason",
			res:      SendResult{Accepted: false, Reason: "DELIVERY_MODE_UNSUPPORTED"},
			delivery: DeliveryGuide,
		},
		{
			name:     "refusal without a reason",
			res:      SendResult{Accepted: false},
			delivery: DeliveryGuide,
			wantErr:  ErrInvalidParams, wantText: "why",
		},
		{
			name:     "refusal that still holds a slot",
			res:      SendResult{Accepted: false, Reason: "busy", QueuePosition: i64(2)},
			delivery: DeliveryQueue,
			wantErr:  ErrInvalidParams, wantText: "accepted=false",
		},
		{
			name:     "idempotent replay answers the same way",
			res:      SendResult{Accepted: true, TurnID: "turn-1", IdempotencyHit: true},
			delivery: DeliveryStartNow,
		},
		{
			name:     "invented delivery mode",
			res:      SendResult{Accepted: true, TurnID: "turn-1"},
			delivery: "immediate",
			wantErr:  ErrInvalidParams, wantText: "not one of",
		},
		{
			name:     "empty delivery mode",
			res:      SendResult{Accepted: true, TurnID: "turn-1"},
			delivery: "",
			wantErr:  ErrInvalidParams, wantText: "not one of",
		},
	}
	for _, c := range cases {
		err := c.res.Validate(c.delivery)
		if c.wantErr == 0 {
			if err != nil {
				t.Errorf("%s: Validate(%s) = %v, want legal", c.name, c.delivery, err)
			}
			continue
		}
		var e *Error
		if !asError(err, &e) || e.Code != c.wantErr {
			t.Errorf("%s: Validate(%s) = %v, want code %s", c.name, c.delivery, err, c.wantErr)
			continue
		}
		if c.wantText != "" && !strings.Contains(err.Error(), c.wantText) {
			t.Errorf("%s: message %q does not contain %q", c.name, err, c.wantText)
		}
	}
}

// TestSendParamsValidate pins the request side of §5 / CO-7 / §4: the tri-state must be
// one of three, §4 makes the idempotency key mandatory on this high-risk write, and
// CO-7's queue strategy only means something on a queue request.
func TestSendParamsValidate(t *testing.T) {
	cases := []struct {
		name         string
		p            SendParams
		wantErr      ErrorCode
		wantTxt      string
		wantStrategy string
	}{
		{name: "start_now", p: SendParams{RemoteSessionID: "s1", Text: "hi", Delivery: DeliveryStartNow, IdempotencyKey: "k1"}, wantStrategy: QueueInterruptAndRun},
		{name: "queue with the new strategy", p: SendParams{
			RemoteSessionID: "s1", Delivery: DeliveryQueue, QueueStrategy: QueueAfterTurn, IdempotencyKey: "k1"},
			wantStrategy: QueueAfterTurn},
		{name: "queue without a strategy reads as interrupt_and_run", p: SendParams{
			RemoteSessionID: "s1", Delivery: DeliveryQueue, IdempotencyKey: "k1"}, wantStrategy: QueueInterruptAndRun},
		{name: "no session", p: SendParams{Text: "hi", Delivery: DeliveryStartNow, IdempotencyKey: "k1"},
			wantErr: ErrInvalidParams, wantTxt: "remote_session_id"},
		{name: "invented delivery state", p: SendParams{RemoteSessionID: "s1", Delivery: "later", IdempotencyKey: "k1"},
			wantErr: ErrDeliveryModeUnsupported, wantTxt: "not one of"},
		{name: "missing delivery state", p: SendParams{RemoteSessionID: "s1", IdempotencyKey: "k1"},
			wantErr: ErrDeliveryModeUnsupported, wantTxt: "not one of"},
		{name: "no idempotency key", p: SendParams{RemoteSessionID: "s1", Delivery: DeliveryStartNow},
			wantErr: ErrInvalidParams, wantTxt: "twice"},
		{name: "invented strategy", p: SendParams{RemoteSessionID: "s1", Delivery: DeliveryQueue,
			QueueStrategy: "drop_older", IdempotencyKey: "k1"}, wantErr: ErrInvalidParams, wantTxt: "queue_strategy"},
		{name: "strategy on a start_now request", p: SendParams{RemoteSessionID: "s1", Delivery: DeliveryStartNow,
			QueueStrategy: QueueAfterTurn, IdempotencyKey: "k1"}, wantErr: ErrInvalidParams, wantTxt: "only says what a queue"},
	}
	for _, c := range cases {
		err := c.p.Validate()
		if c.wantErr == 0 {
			if err != nil {
				t.Errorf("%s: Validate = %v, want legal", c.name, err)
			}
		} else {
			var e *Error
			if !asError(err, &e) || e.Code != c.wantErr {
				t.Errorf("%s: Validate = %v, want code %s", c.name, err, c.wantErr)
			} else if c.wantTxt != "" && !strings.Contains(err.Error(), c.wantTxt) {
				t.Errorf("%s: message %q lacks %q", c.name, err, c.wantTxt)
			}
		}
		if c.wantStrategy != "" && c.p.Strategy() != c.wantStrategy {
			t.Errorf("%s: Strategy() = %q, want %q (CO-7)", c.name, c.p.Strategy(), c.wantStrategy)
		}
	}
}

// TestCreateSessionIdempotency pins CO-5: the call is idempotent through
// client_request_id, and its reply has exactly two shapes - created or hit.
func TestCreateSessionIdempotency(t *testing.T) {
	cases := []struct {
		name    string
		p       CreateSessionParams
		wantErr ErrorCode
	}{
		{name: "plain create", p: CreateSessionParams{ClientRequestID: "018f9c3a-0000-7000-8000-000000000000"}},
		{name: "with base sha", p: CreateSessionParams{
			ClientRequestID: "018f9c3a", BaseSHA: "0123456789abcdef0123456789abcdef01234567"}},
		{name: "no client_request_id", p: CreateSessionParams{Title: "t"}, wantErr: ErrInvalidParams},
		{name: "truncated base sha", p: CreateSessionParams{ClientRequestID: "x", BaseSHA: "abc123"},
			wantErr: ErrInvalidParams},
	}
	for _, c := range cases {
		err := c.p.Validate()
		if c.wantErr == 0 && err != nil {
			t.Errorf("%s: Validate = %v, want legal", c.name, err)
		}
		if c.wantErr != 0 {
			var e *Error
			if !asError(err, &e) || e.Code != c.wantErr {
				t.Errorf("%s: Validate = %v, want code %s", c.name, err, c.wantErr)
			}
		}
	}
	// The reply: created true is a new session, created false is the idempotent hit
	// that must name the same remote session; an empty id is neither.
	res := []struct {
		name    string
		r       CreateSessionResult
		wantErr ErrorCode
	}{
		{name: "created", r: CreateSessionResult{RemoteSessionID: "s-1", Created: true}},
		{name: "idempotent hit", r: CreateSessionResult{RemoteSessionID: "s-1", Created: false}},
		{name: "no remote session id", r: CreateSessionResult{Created: true}, wantErr: ErrInvalidParams},
		{name: "empty hit", r: CreateSessionResult{}, wantErr: ErrInvalidParams},
	}
	for _, c := range res {
		err := c.r.Validate()
		if c.wantErr == 0 && err != nil {
			t.Errorf("%s: Validate = %v, want legal", c.name, err)
		}
		if c.wantErr != 0 {
			var e *Error
			if !asError(err, &e) || e.Code != c.wantErr {
				t.Errorf("%s: Validate = %v, want code %s", c.name, err, c.wantErr)
			}
		}
	}
}
