package session

// `POST /api/v1/executions/{id}/send` 的 session 半边：落一行 messages + 恰一行 message_recorded。
// 触发者是 session（事件契约 §6：Message 属于 Session），所以写入口在本包而不是 execution。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// RecordMessageCommand 是一次本地消息落库所需的外部输入。
// ID / EventID 由调用方预生成（§3 要 UUIDv7；重试复用同一个才谈得上幂等）。
type RecordMessageCommand struct {
	ID      string
	EventID string
	Text    string
	// Attachments 是 yaml SendMessageRequest.attachments（§3 未列上传端点 → REQ-F-13）；
	// 存 content_json，不新开列（迁移归 db）。
	Attachments   []string
	PrincipalID   string
	CorrelationID string
	CausationID   string
}

// RecordMessage 落一条 USER/MESSAGE 本地消息 + 恰一行 message_recorded（同一事务）。
// sequence_no = 该 Session 现有最大值 + 1（首条从 1 起）；remote_message_id 留 NULL ——
// 这条消息还没推到 Provider，空串会撞 idx_messages_remote_unique 的语义（非 NULL 才参与唯一）。
// 成功后 Touch sessions.last_message_at（message_recorded 是 Session 聚合的事件，不是 messages 表的）。
func (s *Service) RecordMessage(ctx context.Context, tx Writer, sessionID string, cmd RecordMessageCommand) (Message, error) {
	if strings.TrimSpace(cmd.ID) == "" || strings.TrimSpace(cmd.EventID) == "" {
		return Message{}, fmt.Errorf("%w: 缺 message_id 或 event_id（重试要复用同一个）", ErrInvalidCommand)
	}
	if _, err := NewRepository(tx).Get(ctx, sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Message{}, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
		}
		return Message{}, err
	}

	var maxSeq sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT max(sequence_no) FROM messages WHERE session_id = ?`, sessionID).Scan(&maxSeq); err != nil {
		return Message{}, fmt.Errorf("session: sequence of %s: %w", sessionID, err)
	}
	seq := int64(1)
	if maxSeq.Valid && maxSeq.Int64 >= 1 {
		seq = maxSeq.Int64 + 1
	}

	now := s.now()
	contentJSON := "{}"
	if len(cmd.Attachments) > 0 {
		b, err := json.Marshal(map[string]any{"attachments": cmd.Attachments})
		if err != nil {
			return Message{}, fmt.Errorf("session: attachments 编不成 JSON: %w", err)
		}
		contentJSON = string(b)
	}
	if _, err := tx.Append("messages",
		[]string{"id", "session_id", "role", "kind", "sequence_no", "text_content", "content_json", "created_at"},
		[]any{cmd.ID, sessionID, string(RoleUser), string(KindMessage), seq, cmd.Text, contentJSON, now}); err != nil {
		return Message{}, fmt.Errorf("session: append message %s: %w", cmd.ID, err)
	}

	payload, err := payloadOf(MessageRecordedPayload{
		Role: RoleUser, Kind: KindMessage, SequenceNo: seq, RemoteMessageID: "",
	})
	if err != nil {
		return Message{}, fmt.Errorf("session: message_recorded payload: %w", err)
	}
	if _, err := tx.Touch(db.AggSession, sessionID,
		"last_message_at = ?, updated_at = ?", []any{now, now},
		db.EventDraft{
			EventID: cmd.EventID, Type: EventMessageRecorded, Aggregate: db.AggSession, AggregateID: sessionID,
			CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID, PrincipalID: cmd.PrincipalID,
			Payload: payload,
		}); err != nil {
		return Message{}, fmt.Errorf("session: touch %s: %w", sessionID, err)
	}

	return Message{
		ID: cmd.ID, SessionID: sessionID, Role: RoleUser, Kind: KindMessage,
		SequenceNo: seq, TextContent: cmd.Text, CreatedAt: now,
	}, nil
}
