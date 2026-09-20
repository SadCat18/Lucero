package notification

import "encoding/json"

// 事件类型 = §6 目录中触发者为 notification 的两行（Audit 列都是 –）。
const (
	EventCreated = "notification_created"
	EventHandled = "notification_handled"
)

// Payload 只做 struct→map；"schema":1 由平台层 appendEvent 缺键时补（内部契约 §1.1 第 8~9 条）。
func Payload(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

type CreatedPayload struct {
	NotificationType Type     `json:"notification_type"`
	Priority         Priority `json:"priority"`
	TaskID           string   `json:"task_id"`
	ApprovalID       string   `json:"approval_id"`
}

type HandledPayload struct {
	NotificationType Type     `json:"notification_type"`
	Priority         Priority `json:"priority"`
	TaskID           string   `json:"task_id"`
	ApprovalID       string   `json:"approval_id"`
}
