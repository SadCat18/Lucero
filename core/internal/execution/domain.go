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
// 四份 *_json 快照列先不读：写侧用到时再加，别提前造。
type Execution struct {
	ID                     string
	TaskID                 string
	AdapterInstanceID      string
	SessionID              string
	PredecessorExecutionID string
	AttemptNo              int64
	Status                 State
	StateConfidence        Confidence
	TriggerType            TriggerType
	ProviderExecutionID    string
	Version                int64 // ADR-017：If-Match 的比较基准（契约③ §2）
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
