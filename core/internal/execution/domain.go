package execution

// Confidence 是「状态置信度」四级词表；UNKNOWN 是合法值，不得折算成确定状态（§57）。
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

// Execution 是某个 Provider/Model 对 Task 的一次真实执行（ADR-014）。
// ID 由调用方在写事务时分配（UUIDv7），本包不生成。
//
// 三份 *_json 快照列（model / permission / environment）**读侧已接**：yaml `Execution` 的
// `model_snapshot` / `permission_snapshot` / `environment_snapshot` 是同一份视图的三格，
// transport 不许另开 SELECT 补（§7.2）⇒ 只能在这里读。**写侧今天仍没有生产者**
// （`Create` 与 `advance` 都不碰这三列，列默认 `'{}'`）⇒ 出口恒是库里的真值，不是编的。
// 第四份 `model_config_json` 是编排侧的另一格，域与视图都不取它，别顺手加。
//
// **第八十批按 yaml `Execution`（core-v1.yaml:1151~1174）补的那批列**，与 task（domain.go:5）
// 同一套 NULL 约定：可空列 **空串 / 0 = 库里 NULL**，出口由 transport 折回 null。
// 写者各不相同，读之前逐个核过（缺口与归属写在字段后面的注里，交账第 297/298 条另有清单）：
//   - `model_id`：**有写者** —— `Service.Create` 走 in.ModelID 落列（service.go:143），
//     但生产装配 `api/http/tasks.go:499` 今天不传 ⇒ 实际落库 NULL，读到什么出什么；
//   - `created_at`/`updated_at`：`Create`/`advance` 都写（毫秒时钟）；
//   - `result_code`/`error_code` 与 `started_at`/`finished_at`：**第八十一批（甲档）补的写者** ——
//     全在 `advance` 的同一笔 Touch 里（`columnWrites`，service.go:216）：started_at 落在
//     `execution_started`、终态三列落在 `Terminal(to)` 那一笔；没信号的值不进 SET（NULL 是真值）；
//   - `error_message`：**仍然没有写者** —— §6 payload 没有这个键、`turn/finished` 帧没有 error 字段，
//     transport 的文案不许塞进领域列 ⇒ 生产上恒 NULL（缺口归契约，第 298 条）；
//   - `heartbeat_at`：**乙档已裁不动**（v0.1 无心跳信号源，写了就是发明没发生的事）。
type Execution struct {
	ID                     string
	TaskID                 string
	AdapterInstanceID      string
	SessionID              string
	ModelID                string // 列 model_id（可空外键）；空串 = NULL
	PredecessorExecutionID string
	AttemptNo              int64
	Status                 State
	StateConfidence        Confidence
	TriggerType            TriggerType
	ProviderExecutionID    string
	Version                int64  // ADR-017：If-Match 的比较基准（契约③ §2）
	ModelSnapshot          string // 对应列 model_snapshot_json（NOT NULL DEFAULT '{}'，JsonMap）
	PermissionSnapshot     string // 对应列 permission_snapshot_json
	EnvironmentSnapshot    string // 对应列 environment_snapshot_json
	ResultCode             string // 列 result_code（可空）；写者 columnWrites：终态那一笔（第八十一批）
	ErrorCode              string // 列 error_code（可空）；写者同上，只有 §6 payload 那个信号在场才写
	ErrorMessage           string // 列 error_message（可空）；今天没有信号源 ⇒ 不写，出口 null 是真值
	StartedAt              int64  // 列 started_at（可空，毫秒）；0 = NULL；写者：execution_started 首刻
	HeartbeatAt            int64  // 列 heartbeat_at（可空，毫秒）；0 = NULL；乙档已裁：无信号源，不写
	FinishedAt             int64  // 列 finished_at（可空，毫秒）；0 = NULL；写者：终态那一笔
	CreatedAt              int64  // 列 created_at（NOT NULL，毫秒）
	UpdatedAt              int64  // 列 updated_at（NOT NULL，毫秒）
}

// NewAttempt 落地 ADR-013/ADR-015：换 Provider、换 Session、失败重来都只产生新的
// Execution —— TaskID 原样传递，绝不新建 Task，Provider 失败也不丢 Task。
func (e Execution) NewAttempt(trigger TriggerType, adapterInstanceID, sessionID string) Execution {
	return Execution{
		TaskID:                 e.TaskID,
		AdapterInstanceID:      adapterInstanceID,
		SessionID:              sessionID,
		PredecessorExecutionID: e.ID,
		AttemptNo:              e.AttemptNo + 1,
		Status:                 Created,
		TriggerType:            trigger,
	}
}
