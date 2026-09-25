package session

// 事件类型 = §6 目录中触发者为 session 的四行（含 message_recorded：Message 属于 Session）。
// 四行的 Audit 列都是 –，所以本包没有 RequiresAudit 表。
//
// 本包故意不实现 task/execution 里那个注入 "schema":1 的 Payload 助手：第四份重复不值
// （见 progress 的 C8，已请主控把 schema 注入收敛到 db.Touch/Create 内部）。
const (
	EventDiscovered      = "session_discovered"
	EventManaged         = "session_managed"
	EventSynced          = "session_synced"
	EventMessageRecorded = "message_recorded"
)

type DiscoveredPayload struct {
	RemoteSessionID   string `json:"remote_session_id"`
	AdapterInstanceID string `json:"adapter_instance_id"`
}

// ManagedPayload 是事件契约 §6 的 `session_managed` 那一格登记的键。**`ProjectID` 是指针不是字符串**：
// C41 裁定（09-23 04:1x）要求"两键都必须在、`project_id` 允许显式 `null`"（同 REQ-F-22 的"键必须在、值可空"），
// 而折成 string 之后"没有归属"会序列化成 `""` —— 那是"绑到一个空串命名的项目"，外键列上根本不存在这种值。
type ManagedPayload struct {
	Managed   bool    `json:"managed"`
	ProjectID *string `json:"project_id"`
}

type SyncedPayload struct {
	SyncState      SyncState  `json:"sync_state"`
	SyncCursor     string     `json:"sync_cursor"`
	RemoteRevision string     `json:"remote_revision"`
	Confidence     Confidence `json:"confidence"`
}

type MessageRecordedPayload struct {
	Role            Role   `json:"role"`
	Kind            Kind   `json:"kind"`
	SequenceNo      int64  `json:"sequence_no"`
	RemoteMessageID string `json:"remote_message_id"`
}
