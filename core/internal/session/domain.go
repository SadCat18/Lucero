// Package session 拥有 Session / Message 的领域类型与只读查询（内部契约 §4：
// "某 task 的 sessions" 由本包提供，task 包不暴露 link 表）。
// 写侧一行 SQL 都没有：落库等 internal/platform/db 编译通过后的 db.Create / db.Touch。
package session

// SyncState 是领域模型「Session Sync State」的八个值。UNKNOWN 是合法值：
// Adapter 无法确认真实状态时必须用 UNKNOWN，不能猜。
type SyncState string

const (
	SyncUnknown     SyncState = "UNKNOWN"
	SyncSynced      SyncState = "SYNCED"
	SyncSyncing     SyncState = "SYNCING"
	SyncLocalAhead  SyncState = "LOCAL_AHEAD"
	SyncRemoteAhead SyncState = "REMOTE_AHEAD"
	SyncConflict    SyncState = "CONFLICT"
	SyncDegraded    SyncState = "DEGRADED"
	SyncUnavailable SyncState = "UNAVAILABLE"
)

func (s SyncState) Valid() bool {
	switch s {
	case SyncUnknown, SyncSynced, SyncSyncing, SyncLocalAhead,
		SyncRemoteAhead, SyncConflict, SyncDegraded, SyncUnavailable:
		return true
	}
	return false
}

// Confidence 是「状态置信度」四级词表。
type Confidence string

const (
	ConfidenceHigh    Confidence = "HIGH"
	ConfidenceMedium  Confidence = "MEDIUM"
	ConfidenceLow     Confidence = "LOW"
	ConfidenceUnknown Confidence = "UNKNOWN"
)

func (c Confidence) Valid() bool {
	switch c {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow, ConfidenceUnknown:
		return true
	}
	return false
}

// LinkRole 是 task_session_links.role 的六个值（领域模型「Task ↔ Session」）。
type LinkRole string

const (
	LinkOrigin    LinkRole = "ORIGIN"
	LinkExecution LinkRole = "EXECUTION"
	LinkHandoff   LinkRole = "HANDOFF"
	LinkReview    LinkRole = "REVIEW"
	LinkCollab    LinkRole = "COLLAB"
	LinkReference LinkRole = "REFERENCE"
)

func (r LinkRole) Valid() bool {
	switch r {
	case LinkOrigin, LinkExecution, LinkHandoff, LinkReview, LinkCollab, LinkReference:
		return true
	}
	return false
}

type Role string

const (
	RoleUser      Role = "USER"
	RoleAssistant Role = "ASSISTANT"
	RoleSystem    Role = "SYSTEM"
	RoleTool      Role = "TOOL"
)

type Kind string

const (
	KindMessage    Kind = "MESSAGE"
	KindToolCall   Kind = "TOOL_CALL"
	KindToolResult Kind = "TOOL_RESULT"
	KindStatus     Kind = "STATUS"
	KindSummary    Kind = "SUMMARY"
)

// Session 对应一家真实软件里的一个已有会话。空串字段表示库里的 NULL。
// 时间戳 0 = 该列是 NULL（从未发生），与状态无关，不参与"UNKNOWN 不得折算"那条红线。
type Session struct {
	ID                string
	AdapterInstanceID string
	ProjectID         string
	RemoteSessionID   string
	Title             string
	Managed           bool
	SyncState         SyncState
	// SyncCursor 用指针是 REQ-F-21 第 ④ 条要求的，不是风格：`sessions.sync_cursor` 可 NULL 且无默认，
	// 而 NULL 的含义是"从未同步过 → 下次从头拉"，与"一个空串"在同步域里是**两件相反的事**。
	// 折成 string 就分不开了（NULL 会变成 ""），出口要么被迫发空串（§5 禁止的缺省即全量重放），
	// 要么靠猜。nil = 库里为 NULL；非 nil 一定指向一个非空字符串。
	SyncCursor      *string
	RemoteRevision  string
	StateConfidence Confidence
	LastMessageAt   int64
	LastSyncedAt    int64
	CreatedAt       int64
	UpdatedAt       int64
	Archived        bool
	Version         int64 // ADR-017：乐观并发与"我看到的不是你现在的"
}

type Message struct {
	ID              string
	SessionID       string
	RemoteMessageID string
	Role            Role
	Kind            Kind
	SequenceNo      int64
	TextContent     string
	CreatedAt       int64
	SyncedAt        int64
}

// TaskSession 是一条 task_session_links 加上它指向的 Session。
type TaskSession struct {
	Session
	LinkID  string
	Role    LinkRole
	Ordinal int64
}
