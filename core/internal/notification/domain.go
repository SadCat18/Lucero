// Package notification 是 Notification 聚合的唯一拥有者：别的角色（audit / adapter）
// 要发通知只能调本包 Service，不得自己写 notifications 表（契约③ §7 C12 补行）。
package notification

// Priority 是 notifications.priority 的四个值。
type Priority string

const (
	PriorityLow      Priority = "LOW"
	PriorityMedium   Priority = "MEDIUM"
	PriorityHigh     Priority = "HIGH"
	PriorityCritical Priority = "CRITICAL"
)

func (p Priority) Valid() bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical:
		return true
	}
	return false
}

// State 是 notifications.state 的六个值。
type State string

const (
	StateUnread  State = "UNREAD"
	StateRead    State = "READ"
	StatePending State = "PENDING"
	StateHandled State = "HANDLED"
	StateIgnored State = "IGNORED"
	StateExpired State = "EXPIRED"
)

func (s State) Valid() bool {
	switch s {
	case StateUnread, StateRead, StatePending, StateHandled, StateIgnored, StateExpired:
		return true
	}
	return false
}

// Type 是 notification_type：库里这列没有 CHECK（可自由扩展），所以词表只登记
// 契约已经点名的那几个，别的地方不要凭空加。
type Type string

const (
	// TypeProjectionRejected 是事件契约 §5.4 第 2 条点名的唯一一种 Core 自发通知。
	TypeProjectionRejected Type = "projection_rejected"
)

// Notification 是一条站内通知。空串字段 = 库里的 NULL；时间戳 0 = 没有发生过。
type Notification struct {
	ID         string
	Type       Type
	Priority   Priority
	State      State
	Title      string
	Body       string
	ProjectID  string
	TaskID     string
	ApprovalID string
	CreatedAt  int64
	ReadAt     int64
	HandledAt  int64
	ExpiresAt  int64
	Version    int64
}

// Draft 是建单输入。ID 必须由调用方预生成（UUIDv7）：重试要复用同一个，不然会产生第二条通知
// （领域模型「Idempotency」、契约① §3 重放保护）。
type Draft struct {
	ID         string
	Type       Type
	Priority   Priority
	Title      string
	Body       string
	ProjectID  string
	TaskID     string
	ApprovalID string
	ExpiresAt  int64

	PrincipalID   string
	CorrelationID string
	CausationID   string
	EventID       string
}
