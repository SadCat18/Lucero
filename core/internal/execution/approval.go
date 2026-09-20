package execution

import "context"

// ApprovalStates 是 backend 唯一能问的审批问题（内部契约 §3 规则 1）：
// "我能不能继续" = BlockingState(executionID)。实现由 cmd/core 注入 internal/approval，
// 本包不得 import 它，更不得自己碰 approvals 表。
type ApprovalStates interface {
	BlockingState(ctx context.Context, executionID string) (string, error)
}
