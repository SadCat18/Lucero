package quota

// EventObserved = §6 目录中触发者为 quota 的那一行（Audit 列为 –）。
// 本包不实现注入 "schema":1 的 Payload 助手，理由见 internal/session/events.go 顶部。
const EventObserved = "quota_observed"

type ObservedPayload struct {
	QuotaAccountID  string          `json:"quota_account_id"`
	QuotaKey        string          `json:"quota_key"`
	ObservationKind ObservationKind `json:"observation_kind"`
	Confidence      Confidence      `json:"confidence"`
}
