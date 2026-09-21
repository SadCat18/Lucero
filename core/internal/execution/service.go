package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// ErrInvalidExecution 是入参校验失败：在碰库之前拦住，别把 SQLite 的约束文本透给调用方。
var ErrInvalidExecution = errors.New("execution: invalid input")

// ErrVersionConflict 对应契约③ §2 的 409 version_conflict。
var ErrVersionConflict = errors.New("execution: version conflict")

// ErrNoEventForTransition 不是"这步不让走"，是**平台层今天表达不了这步**：
// 见 §5.2 与 §6 的错位 —— `CREATED→STARTING` 在 §6 没有对应事件，而 `Tx.Touch` 强制要一个已注册事件。
// 细节与两条可选修法记在 progress 的 REQ-DB-4；在这里宁可明确失败，也不自己造一个事件类型（§9.1 P0）。
var ErrNoEventForTransition = errors.New("execution: §6 has no event for this transition")

// Command 是一次状态推进所需的外部输入；字段与 §6 各 payload 的键一一对应。
type Command struct {
	EventID       string
	PrincipalID   string
	CorrelationID string
	CausationID   string

	ProviderExecutionID string
	SnapshotSHA256      string
	Reason              string
	ResultCode          string
	ErrorCode           string

	WantVersion int64 // <=0 = 不设 If-Match（Core 内部驱动）
}

// Result 是一次推进的结果。
type Result struct {
	From, To State
	Advanced bool
	Version  int64
}

// Service 是 Execution 的写路径。它不自己开事务，也不自己生成 id（§3 要 UUIDv7，重试要复用同一个）。
type Service struct{}

func NewService() *Service { return &Service{} }

// NewExecution 是建首行的输入。ModelID / SessionID 可空（进库是 NULL，不是空串）。
type NewExecution struct {
	ID                     string
	TaskID                 string
	AdapterInstanceID      string
	SessionID              string
	ModelID                string
	PredecessorExecutionID string
	AttemptNo              int64
	TriggerType            TriggerType

	EventID       string
	PrincipalID   string
	CorrelationID string
	CausationID   string
}

// Create 落第一行：CREATED、版本 1、恰一行 execution_created，同一个事务。
// attempt_no 与 predecessor_execution_id 是一对信号：ADR-013/015 下换 Provider 只产生新 Execution，
// TaskID 原样传递 —— 所以 attempt_no>1 必须带前驱，反之不许带。这条不是防御性啰嗦，
// 是"重试有没有丢 Task"唯一能在库里被读出来的痕迹。
func (s *Service) Create(ctx context.Context, tx *db.Tx, in NewExecution) (Execution, error) {
	switch {
	case in.ID == "":
		return Execution{}, fmt.Errorf("%w: 缺 id（调用方生成 UUIDv7）", ErrInvalidExecution)
	case in.EventID == "":
		return Execution{}, fmt.Errorf("%w: 缺 event_id", ErrInvalidExecution)
	case in.TaskID == "":
		return Execution{}, fmt.Errorf("%w: Execution 必须挂在一个 Task 上（ADR-014）", ErrInvalidExecution)
	case in.AdapterInstanceID == "":
		return Execution{}, fmt.Errorf("%w: 缺 adapter_instance_id", ErrInvalidExecution)
	}
	if in.AttemptNo == 0 {
		in.AttemptNo = 1
	}
	if in.AttemptNo < 1 {
		return Execution{}, fmt.Errorf("%w: attempt_no %d 必须 >= 1", ErrInvalidExecution, in.AttemptNo)
	}
	if in.AttemptNo > 1 && in.PredecessorExecutionID == "" {
		return Execution{}, fmt.Errorf("%w: attempt_no %d 却没有 predecessor_execution_id，换 Provider 的痕迹就断了", ErrInvalidExecution, in.AttemptNo)
	}
	if in.AttemptNo == 1 && in.PredecessorExecutionID != "" {
		return Execution{}, fmt.Errorf("%w: 首次执行不该带前驱", ErrInvalidExecution)
	}
	if in.TriggerType == "" {
		in.TriggerType = TriggerInitial
	}
	if !in.TriggerType.Valid() {
		return Execution{}, fmt.Errorf("%w: trigger_type %q 不在 §23 词表里", ErrInvalidExecution, in.TriggerType)
	}

	now := time.Now().UnixMilli()
	cols := []string{"task_id", "adapter_instance_id", "attempt_no", "status",
		"trigger_type", "state_confidence", "created_at", "updated_at"}
	vals := []any{in.TaskID, in.AdapterInstanceID, in.AttemptNo, string(Created),
		string(in.TriggerType), string(ConfidenceUnknown), now, now}
	// 三个可空列（全是外键）只在真有值时进列表：空串不是 NULL，会直接撞外键。
	for _, opt := range []struct {
		col string
		val string
	}{
		{"session_id", in.SessionID},
		{"model_id", in.ModelID},
		{"predecessor_execution_id", in.PredecessorExecutionID},
	} {
		if opt.val != "" {
			cols = append(cols, opt.col)
			vals = append(vals, opt.val)
		}
	}

	payload, err := Payload(CreatedPayload{TaskID: in.TaskID, AdapterInstanceID: in.AdapterInstanceID,
		ModelID: in.ModelID, AttemptNo: in.AttemptNo, TriggerType: in.TriggerType})
	if err != nil {
		return Execution{}, fmt.Errorf("execution: execution_created payload: %w", err)
	}
	if err := tx.Create(db.AggExecution, in.ID, cols, vals, db.EventDraft{
		EventID: in.EventID, Type: EventCreated, Aggregate: db.AggExecution, AggregateID: in.ID,
		CorrelationID: in.CorrelationID, CausationID: in.CausationID,
		PrincipalID: in.PrincipalID, Payload: payload,
	}); err != nil {
		return Execution{}, fmt.Errorf("execution: create %s: %w", in.ID, err)
	}
	return Execution{ID: in.ID, TaskID: in.TaskID, AdapterInstanceID: in.AdapterInstanceID,
		SessionID: in.SessionID, PredecessorExecutionID: in.PredecessorExecutionID,
		AttemptNo: in.AttemptNo, Status: Created, StateConfidence: ConfidenceUnknown,
		TriggerType: in.TriggerType, Version: 1,
	}, nil
}

// Started 是 STARTING→RUNNING：这一次执行真的在 Provider 上起来了，
// 所以 payload 必须带得走 provider_execution_id 与快照指纹（§6）。
func (s *Service) Started(ctx context.Context, tx *db.Tx, id string, cmd Command) (Result, error) {
	return s.advance(ctx, tx, id, Running, cmd)
}

// advance 是所有推进共用的四步：库里读当前行 → If-Match → §5.2 裁决 → 事件类型由 EventOf 决定 → Touch。
// 任何一步不过就一行都不写。
func (s *Service) advance(ctx context.Context, tx *db.Tx, id string, to State, cmd Command) (Result, error) {
	cur, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	res := Result{From: cur.Status, To: cur.Status, Version: cur.Version}
	if cmd.WantVersion > 0 && cmd.WantVersion != cur.Version {
		return res, fmt.Errorf("%w: 请求带 v%d，库里已是 v%d", ErrVersionConflict, cmd.WantVersion, cur.Version)
	}
	if err := Check(cur.Status, to, Normal, Evidence{}); err != nil {
		return res, fmt.Errorf("execution: %s→%s 不在 §5.2 矩阵里: %w", cur.Status, to, err)
	}
	evType := EventOf(cur.Status, to)
	if evType == "" {
		return res, fmt.Errorf("execution: %s→%s 在 §6 没有对应事件（REQ-DB-4），我不自己造一个: %w",
			cur.Status, to, ErrNoEventForTransition)
	}
	payload, err := eventPayload(cur.Status, to, cmd)
	if err != nil {
		return res, err
	}
	v, err := tx.Touch(db.AggExecution, id, "status = ?, updated_at = ?",
		[]any{string(to), time.Now().UnixMilli()},
		db.EventDraft{EventID: cmd.EventID, Type: evType, Aggregate: db.AggExecution, AggregateID: id,
			CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
			PrincipalID: cmd.PrincipalID, Payload: payload})
	if err != nil {
		return res, fmt.Errorf("execution: touch %s: %w", id, err)
	}
	res.To, res.Advanced, res.Version = to, true, v
	return res, nil
}

// eventPayload 按 §6 那一行的键装配 payload；键名与目录逐字一致，缺字段宁可为空串也不自己造名字。
func eventPayload(from, to State, cmd Command) (map[string]any, error) {
	var v any
	switch EventOf(from, to) {
	case EventStarted:
		v = StartedPayload{ProviderExecutionID: cmd.ProviderExecutionID, ModelSnapshotSHA256: cmd.SnapshotSHA256}
	case EventResumed:
		v = ResumedPayload{From: from}
	case EventPaused:
		v = PausedPayload{Reason: cmd.Reason}
	case EventSucceeded, EventFailed, EventCancelled, EventInterrupted:
		v = ResultPayload{ResultCode: cmd.ResultCode, ErrorCode: cmd.ErrorCode, Usage: map[string]any{}}
	default:
		return nil, fmt.Errorf("execution: %s→%s 没有 §6 事件类型: %w", from, to, ErrNoEventForTransition)
	}
	m, err := Payload(v)
	if err != nil {
		return nil, fmt.Errorf("execution: payload: %w", err)
	}
	return m, nil
}
