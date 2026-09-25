package task

// Task 是用户的目标，不属于任何一家软件（ADR-013）：故意没有 adapter / session 字段，
// Provider 与 Session 只是它名下 Execution 的属性（关联见 internal/session 的 ForTask）。
// 空串字段表示库里的 NULL；时间戳 0 = 该列没有值。
// 三个 *_json 列（budget / policy_snapshot / metadata）先不读：写侧用到时再加，别提前造。
type Task struct {
	ID           string
	ProjectID    string
	ParentTaskID string
	SessionID    string // 来源会话（REQ-LK-1：`task_session_links` 里 role=ORIGIN 那一行，可以没有）
	Title        string
	Description  string
	TaskType     string
	Mode         string
	Priority     string
	Status       State
	RiskLevel    string
	RequestedBy  string
	CreatedAt    int64
	UpdatedAt    int64
	AcceptedAt   int64
	Archived     bool
	Version      int64 // ADR-017：乐观并发的比较基准（If-Match 见契约③ §2）
}
