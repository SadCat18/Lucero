package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// ErrVersionConflict 对应契约③ §2 的 409 version_conflict："我看到的不是你现在的"。
var ErrVersionConflict = errors.New("task: version conflict")

// ErrProjectionRejected 是"边沿不合法、且没注入 RejectionReporter"的哨兵：
// §5.4 不许静默吞，没注入就必然失败 —— 但失败形状要让调用方能识别成"状态机拒绝"
// （409 illegal_transition）而不是 500 装配错误。Report 写失败仍走普通 error（那才是 500）。
var ErrProjectionRejected = errors.New("task: projection rejected")

// RejectionReport 是 §5.4 要求的那两条记录（Notification + Audit）的全部输入。
// 三个 id 都由调用方预生成：重试必须复用同一个，否则会发第二条通知（§8 幂等）；
// AuditID 单独存在是因为 audit_entries.audit_id 有自己的 UNIQUE 命名空间，不能拿事件 id 顶。
type RejectionReport struct {
	Rejection
	TaskID         string
	NotificationID string
	EventID        string
	AuditID        string
	PrincipalID    string
	CorrelationID  string
	CausationID    string
}

// RejectionReporter 由 cmd/core 绑到 internal/notification 的 Service 与 audit 的哈希链上。
// §5.4 第 3 条不许静默吞，所以没注入就是必然失败，而不是"跳过记录"。
type RejectionReporter interface {
	Report(ctx context.Context, tx *db.Tx, r RejectionReport) error
}

// Command 是一条 Execution 边沿落库所需的外部输入；字段与 §6 各 payload 的键一一对应。
type Command struct {
	EventID       string
	PrincipalID   string
	CorrelationID string
	CausationID   string

	ExecutionID  string
	ApprovalID   string
	HandoffID    string
	ResultCode   string
	ErrorCode    string
	LeaseState   string
	Reason       string // 人给的理由（:pause 用）。CancelledPayload 走 ErrorCode，那是执行侧带来的错误码，两者不同源。
	Notification string // 被拒时那条 Notification 的预生成 id
	RejEvent     string // 被拒时 notification_created 的事件 id
	RejAudit     string // 被拒时那条 audit_entries 的 audit_id（§5.4 的第二条记录）

	WantVersion int64 // <=0 = 不设 If-Match（Core 内部驱动：Scheduler / Recovery / Adapter 回调）
}

// Result 是一次投影落库的结果。
type Result struct {
	From, To  State
	Advanced  bool
	Version   int64 // 推进后的新版本；被拒时是库里那个原版本
	Rejection Rejection
}

// Service 是 Task 的写路径。它不自己开事务：一个命令一个事务由调用方（api/调度）持有，
// 提交之后才 Publish（内部契约 §1 规则 2，Tx 也不提供任何发送事件的能力）。
type Service struct{ rejections RejectionReporter }

func NewService(rejections RejectionReporter) *Service { return &Service{rejections: rejections} }

// ApplyExecutionEvent 是 §5.3 投影的落地入口，顺序是 §1.1 第 11 条那一条：
// 同一事务内 SELECT 当前 status/version → 比对 If-Match → §5.3 投影 → 合法才 Touch，
// 被拒则交 RejectionReporter 写 Notification + Audit，Task 的版本与事件一行都不许多。
//
// 状态机的 from 一律取自库里，不接受调用方口头声明：否则就是 §12
// 「UI 显示的状态与 Core 实际状态不一致」的成因。
func (s *Service) ApplyExecutionEvent(ctx context.Context, tx *db.Tx, taskID, edge string,
	handoffReady, approved bool, cmd Command) (Result, error) {

	cur, err := NewRepository(tx).Get(ctx, taskID)
	if err != nil {
		return Result{}, err
	}
	res := Result{From: cur.Status, To: cur.Status, Version: cur.Version}
	if err := guardIfMatch(cur, cmd); err != nil {
		return res, err
	}

	to, ok, rej := ProjectTaskState(res.From, edge, handoffReady, approved)
	res.Rejection = rej
	if !ok {
		if s.rejections == nil {
			return res, fmt.Errorf("%w（%s）: 没注入 RejectionReporter，§5.4 第 3 条不许静默吞", ErrProjectionRejected, rej.Reason)
		}
		if err := s.rejections.Report(ctx, tx, RejectionReport{
			Rejection: rej, TaskID: taskID, NotificationID: cmd.Notification,
			EventID: cmd.RejEvent, AuditID: cmd.RejAudit,
			PrincipalID: cmd.PrincipalID, CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
		}); err != nil {
			return res, fmt.Errorf("task: §5.4 的两条记录没写出去: %w", err)
		}
		return res, nil
	}

	evType, payload, err := edgeEvent(edge, to, cmd)
	if err != nil {
		return res, err
	}
	newVersion, err := tx.Touch(db.AggTask, taskID, "status = ?, updated_at = ?",
		[]any{string(to), time.Now().UnixMilli()},
		db.EventDraft{EventID: cmd.EventID, Type: evType, Aggregate: db.AggTask, AggregateID: taskID,
			CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
			PrincipalID: cmd.PrincipalID, Payload: payload})
	if err != nil {
		return res, fmt.Errorf("task: touch %s: %w", taskID, err)
	}
	res.To, res.Advanced, res.Version = to, true, newVersion
	return res, nil
}

// edgeEvent 把 §5.3 的边沿翻译成 §6 那一行的 event_type + payload。
// 键名与 §6 一致，缺字段宁可为空串也不自己造名字（对账用例在 events_test.go）。
func edgeEvent(edge string, to State, cmd Command) (string, map[string]any, error) {
	var typ string
	var v any
	switch to {
	case WaitingApproval:
		typ, v = EventWaitingApproval, WaitingApprovalPayload{ApprovalID: cmd.ApprovalID}
	case AgentCompleted:
		typ, v = EventAgentCompleted, AgentCompletedPayload{ExecutionID: cmd.ExecutionID, ResultCode: cmd.ResultCode}
	case Interrupted:
		typ, v = EventInterrupted, InterruptedPayload{ExecutionID: cmd.ExecutionID, LeaseState: cmd.LeaseState}
	case Cancelled:
		typ, v = EventCancelled, CancelledPayload{Reason: cmd.ErrorCode}
	case Failed:
		typ, v = EventFailed, FailedPayload{ErrorCode: cmd.ErrorCode}
	case Transferring:
		typ, v = EventTransferring, TransferringPayload{HandoffID: cmd.HandoffID}
	case Running:
		typ, v = EventRunning, RunningPayload{ExecutionID: cmd.ExecutionID}
	default:
		return "", nil, fmt.Errorf("task: 投影出的 %s 没有对应的 §6 事件类型", to)
	}
	m, err := Payload(v)
	if err != nil {
		return "", nil, fmt.Errorf("task: %s payload: %w", typ, err)
	}
	return typ, m, nil
}

// guardIfMatch 是契约③ §2 那条乐观并发：只有请求真带了 If-Match 才比对。
func guardIfMatch(cur Task, cmd Command) error {
	if cmd.WantVersion > 0 && cmd.WantVersion != cur.Version {
		return fmt.Errorf("%w: 请求带 v%d，库里已是 v%d", ErrVersionConflict, cmd.WantVersion, cur.Version)
	}
	return nil
}

// advance 是所有状态动作共用的四步：库里读当前行（状态与版本都从这儿来，不接受调用方口头给）
// → If-Match → 起始态白名单（allowedFrom=nil 表示交给矩阵自己判）→ §5.1 状态机裁决 → Touch（一个版本、一行事件）。
// 任何一步不过就一行都不写。
func (s *Service) advance(ctx context.Context, tx *db.Tx, id string, cmd Command, to State,
	allowedFrom map[State]bool, eventOf func(Task) (string, any)) (Result, error) {

	cur, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	res := Result{From: cur.Status, To: cur.Status, Version: cur.Version}
	if err := guardIfMatch(cur, cmd); err != nil {
		return res, err
	}
	if allowedFrom != nil && !allowedFrom[cur.Status] {
		return res, fmt.Errorf("task: 起始态 %s 不在本动作允许的集合里（目标 %s）: %w", cur.Status, to, ErrIllegalTransition)
	}
	if err := Check(cur.Status, to, Normal, Evidence{}); err != nil {
		return res, fmt.Errorf("task: %s→%s 不在 §5.1 矩阵里: %w", cur.Status, to, err)
	}
	evType, payload := eventOf(cur)
	m, err := Payload(payload)
	if err != nil {
		return res, fmt.Errorf("task: %s payload: %w", evType, err)
	}
	v, err := tx.Touch(db.AggTask, id, "status = ?, updated_at = ?",
		[]any{string(to), time.Now().UnixMilli()},
		db.EventDraft{EventID: cmd.EventID, Type: evType, Aggregate: db.AggTask, AggregateID: id,
			CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
			PrincipalID: cmd.PrincipalID, Payload: m})
	if err != nil {
		return res, fmt.Errorf("task: touch %s: %w", id, err)
	}
	res.To, res.Advanced, res.Version = to, true, v
	return res, nil
}

// NewTask 是建任务的输入。ID 与 EventID 都由调用方生成（§3 要 UUIDv7；重试复用同一个才谈得上幂等）。
type NewTask struct {
	ID            string
	Title         string
	Description   string
	TaskType      string
	Mode          string
	Priority      string
	RiskLevel     string
	ProjectID     string
	RequestedBy   string
	EventID       string
	PrincipalID   string
	CorrelationID string
	CausationID   string
}

// ErrInvalidTask 是入参校验失败：这些词表都是 000001 里 CHECK 过的列，
// 在这里拦住才能给出可控的错误体，而不是把 SQLite 的约束文本透给客户端。
var ErrInvalidTask = errors.New("task: invalid task")

var taskPriorities = map[string]bool{"LOW": true, "NORMAL": true, "HIGH": true, "CRITICAL": true}
var taskRiskLevels = map[string]bool{"LOW": true, "MEDIUM": true, "HIGH": true, "CRITICAL": true, "UNKNOWN": true}

// Create 落第一行：状态 CREATED、版本 1、恰一行 task_created，同一个事务。
func (s *Service) Create(ctx context.Context, tx *db.Tx, in NewTask) (Task, error) {
	if in.ID == "" {
		return Task{}, fmt.Errorf("%w: 缺 id（调用方生成 UUIDv7，重试要复用同一个）", ErrInvalidTask)
	}
	if in.EventID == "" {
		return Task{}, fmt.Errorf("%w: 缺 event_id", ErrInvalidTask)
	}
	if strings.TrimSpace(in.Title) == "" {
		return Task{}, fmt.Errorf("%w: title 不能为空", ErrInvalidTask)
	}
	setDefault(&in.TaskType, "GENERAL")
	setDefault(&in.Mode, "EXECUTE")
	setDefault(&in.Priority, "NORMAL")
	setDefault(&in.RiskLevel, "LOW")
	if !taskPriorities[in.Priority] {
		return Task{}, fmt.Errorf("%w: priority %q 不在 §18 词表里", ErrInvalidTask, in.Priority)
	}
	if !taskRiskLevels[in.RiskLevel] {
		return Task{}, fmt.Errorf("%w: risk_level %q 不在词表里", ErrInvalidTask, in.RiskLevel)
	}

	now := time.Now().UnixMilli()
	cols := []string{"title", "task_type", "mode", "priority", "status", "risk_level", "created_at", "updated_at"}
	vals := []any{in.Title, in.TaskType, in.Mode, in.Priority, string(Created), in.RiskLevel, now, now}
	// 三个可空列（含两个外键）只在真有值时进列表：空串是非 NULL，会直接撞 project_id / requested_by 的外键。
	for _, opt := range []struct {
		col string
		val string
	}{
		{"description", in.Description},
		{"project_id", in.ProjectID},
		{"requested_by", in.RequestedBy},
	} {
		if opt.val != "" {
			cols = append(cols, opt.col)
			vals = append(vals, opt.val)
		}
	}

	m, err := Payload(CreatedPayload{Title: in.Title, Priority: in.Priority, Mode: in.Mode,
		RiskLevel: in.RiskLevel, ProjectID: in.ProjectID})
	if err != nil {
		return Task{}, fmt.Errorf("task: task_created payload: %w", err)
	}
	if err := tx.Create(db.AggTask, in.ID, cols, vals, db.EventDraft{
		EventID: in.EventID, Type: EventCreated, Aggregate: db.AggTask, AggregateID: in.ID,
		CorrelationID: in.CorrelationID, CausationID: in.CausationID,
		PrincipalID: in.PrincipalID, Payload: m,
	}); err != nil {
		return Task{}, fmt.Errorf("task: create %s: %w", in.ID, err)
	}
	return Task{ID: in.ID, Title: in.Title, TaskType: in.TaskType, Mode: in.Mode,
		Priority: in.Priority, Status: Created, RiskLevel: in.RiskLevel,
		ProjectID: in.ProjectID, RequestedBy: in.RequestedBy, Description: in.Description,
		CreatedAt: now, UpdatedAt: now, Version: 1}, nil
}

// Queue 是唯一合法的"排队"入口：CREATED→QUEUED（§5.1）。
func (s *Service) Queue(ctx context.Context, tx *db.Tx, id string, cmd Command) (Result, error) {
	return s.advance(ctx, tx, id, cmd, Queued, nil, func(t Task) (string, any) {
		return EventQueued, QueuedPayload{Priority: t.Priority}
	})
}

// Prepare 是 QUEUED→PREPARING（§5.1），Scheduler 拉起 Execution 之后回头调它。
// execution_id 是这条事件的语义核心（§6），不是可选装饰：ADR-013/015 下 Task 与 Execution 是两个聚合，
// 一条说"开始准备"却点不出准备者的事件等于伪造，所以在碰库之前就要失败。
func (s *Service) Prepare(ctx context.Context, tx *db.Tx, id string, cmd Command) (Result, error) {
	if cmd.ExecutionID == "" {
		return Result{}, fmt.Errorf("%w: task_preparing 缺 execution_id", ErrInvalidTask)
	}
	return s.advance(ctx, tx, id, cmd, Preparing, nil, func(t Task) (string, any) {
		return EventPreparing, PreparingPayload{ExecutionID: cmd.ExecutionID}
	})
}

// Pause 把任务挂到 PAUSED。起始态不在这设白名单：§5.1 里指向 PAUSED 的边本来就散在七个状态上
// （QUEUED/PREPARING/RUNNING/WAITING_APPROVAL/WAITING_INPUT/BLOCKED/WAITING_ACCEPTANCE），矩阵说了算。
// reason 允许为空：§3 没给 :pause 定义请求体，"查不到条款的要求"不许我替契约发明（见 progress C21）。
func (s *Service) Pause(ctx context.Context, tx *db.Tx, id string, cmd Command) (Result, error) {
	return s.advance(ctx, tx, id, cmd, Paused, nil, func(t Task) (string, any) {
		return EventPaused, PausedPayload{Reason: cmd.Reason}
	})
}

// resumedFrom 是 §6 给 task_resumed.from 定死的取值集合：只有真的被挂起来过的状态才谈得上"恢复"。
// 不加这道白名单会出这种事：INTERRUPTED 也有到 QUEUED/RUNNING 的合法边（§5.1），
// 于是一次崩溃恢复被写成 task_resumed{from:"INTERRUPTED"} —— 目录里没这个值，且没人暂停过。
var resumedFrom = map[State]bool{Paused: true, Blocked: true}

// Resume 只承认"从 PAUSED 或 BLOCKED 回到 QUEUED 或 RUNNING"。
// 目标态**必须**由调用方点名：tasks 表没有"暂停之前是什么"这一列，而 §5.1 里
// PAUSED→QUEUED 与 PAUSED→RUNNING 两条边都合法，替客户端猜就等于凭空决定要不要有一个 Execution。
func (s *Service) Resume(ctx context.Context, tx *db.Tx, id string, to State, cmd Command) (Result, error) {
	if to != Queued && to != Running {
		return Result{}, fmt.Errorf("%w: resume 的目标只能是 QUEUED 或 RUNNING，给了 %q", ErrInvalidTask, to)
	}
	return s.advance(ctx, tx, id, cmd, to, resumedFrom, func(t Task) (string, any) {
		return EventResumed, ResumedPayload{From: t.Status}
	})
}

// Cancel 把任务落到终态 CANCELLED。起始态不设白名单：§5.1 里几乎每个非终态都有这条边，
// 能不能走由矩阵判 —— 尤其别从 ACCEPTED/CANCELLED 这两个终态回退（矩阵里没有出边，会被拒）。
// reason 与 FailedPayload/CancelledPayload 里那个来自执行侧的 ErrorCode 不同源：
// 这里是**人**给的理由（:cancel 的请求体），执行侧驱动的取消走 ApplyExecutionEvent 那条路。
func (s *Service) Cancel(ctx context.Context, tx *db.Tx, id string, cmd Command) (Result, error) {
	return s.advance(ctx, tx, id, cmd, Cancelled, nil, func(t Task) (string, any) {
		return EventCancelled, CancelledPayload{Reason: cmd.Reason}
	})
}

// Accept 是 WAITING_ACCEPTANCE→ACCEPTED：验收是终态闸门，accepted_by 必须是**做这次写操作的那个主体**，
// 不接受请求体自报（契约③ §1.4：写操作只认 local-user，system/adapter 由 Core 内部产生）。
func (s *Service) Accept(ctx context.Context, tx *db.Tx, id string, cmd Command) (Result, error) {
	if cmd.PrincipalID == "" {
		return Result{}, fmt.Errorf("%w: task_accepted 需要 accepted_by（写主体，不是请求体字段）", ErrInvalidTask)
	}
	return s.advance(ctx, tx, id, cmd, Accepted, onlyWaitingAcceptance, func(t Task) (string, any) {
		return EventAccepted, AcceptedPayload{AcceptedBy: cmd.PrincipalID}
	})
}

// onlyWaitingAcceptance 钉住验收动作的起始态。矩阵本身也只给这一条边
// （WAITING_ACCEPTANCE 才有到 ACCEPTED 的出边），所以这是一道**语义**守卫而不是补矩阵的漏：
// 万一以后矩阵放宽，这里不会让"从 RUNNING 直接验收"悄悄变成合法。
var onlyWaitingAcceptance = map[State]bool{WaitingAcceptance: true}

func setDefault(p *string, v string) {
	if *p == "" {
		*p = v
	}
}
