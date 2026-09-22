package notification

import "encoding/json"

// 事件类型 = §6 目录中触发者为 notification 的三行（Audit 列都是 –）。
// 第三行 `notification_read` 是 09-22 REQ-DB-5 补登记的（那条裁定明写：目录里只有
// `_created`/`_handled` 时，`POST /notifications/{id}:read` 今天**没有合法事件可发**，只能挂 501）。
const (
	EventCreated = "notification_created"
	EventHandled = "notification_handled"
	EventRead    = "notification_read"
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

// ReadPayload 与 `_handled` 同形 —— §6 那一格是三行**共用**同一列 payload 键
// （`notification_type` `priority` `task_id` `approval_id`），所以这里不是"顺手复制一份"，
// 而是照目录的形状各写一份：三行各自键集合漂了才会红。
type ReadPayload struct {
	NotificationType Type     `json:"notification_type"`
	Priority         Priority `json:"priority"`
	TaskID           string   `json:"task_id"`
	ApprovalID       string   `json:"approval_id"`
}
