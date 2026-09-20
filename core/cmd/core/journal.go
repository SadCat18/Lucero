package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	wsapi "github.com/SadCat18/lucero/core/api/websocket"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// journalReader 是 event_journal 的读视图。
// REQ-WS-2：这块读能力在契约③ §7 里还没有归属，先住在 cmd/core（transport 的接线点），
// db 交付自己的 journal 读入口后换掉这一段就行 —— 注意它只读**已提交**的行，
// 所以 ADR-008 的"提交之后才推"天然成立，不需要谁去通知它。
type journalReader struct{ d *db.DB }

const journalCols = `seq, event_id, event_type, aggregate_type, aggregate_id, aggregate_version,
	correlation_id, causation_id, principal_id, payload_json, created_at`

func (j journalReader) After(ctx context.Context, after int64, limit int) ([]wsapi.Event, error) {
	rows, err := j.d.Read.QueryContext(ctx,
		`SELECT `+journalCols+` FROM event_journal WHERE seq > ? ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, fmt.Errorf("journal: after %d: %w", after, err)
	}
	defer rows.Close()
	var out []wsapi.Event
	for rows.Next() {
		var (
			e                     wsapi.Event
			corr, caus, principal sql.NullString
			raw                   string
		)
		if err := rows.Scan(&e.Seq, &e.EventID, &e.EventType, &e.AggregateType, &e.AggregateID,
			&e.AggregateVersion, &corr, &caus, &principal, &raw, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("journal: after %d scan: %w", after, err)
		}
		e.CorrelationID, e.CausationID, e.PrincipalID = corr.String, caus.String, principal.String
		if err := json.Unmarshal([]byte(raw), &e.Payload); err != nil {
			// payload_json 有 CHECK(json_valid) 兜底，走到这里说明库已经坏了；
			// 不能把一条读不懂的事件推给前端假装正常。
			return nil, fmt.Errorf("journal: seq %d 的 payload 不是合法 JSON: %w", e.Seq, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Oldest 返回表里还在的最小 seq，空表 0。用来识别"游标已经比 journal 还老"。
func (j journalReader) Oldest(ctx context.Context) (int64, error) {
	var v sql.NullInt64
	if err := j.d.Read.QueryRowContext(ctx, `SELECT min(seq) FROM event_journal`).Scan(&v); err != nil {
		return 0, fmt.Errorf("journal: oldest: %w", err)
	}
	return v.Int64, nil
}

// Head 返回此刻的最大 seq，空表 0 —— 契约③ §5 的"不带游标只收此后新事件"的起点，
// 也是 hello_ack.server_seq 该带的值（OpenAPI WsHelloAck 的字段说明）。
func (j journalReader) Head(ctx context.Context) (int64, error) {
	var v sql.NullInt64
	if err := j.d.Read.QueryRowContext(ctx, `SELECT max(seq) FROM event_journal`).Scan(&v); err != nil {
		return 0, fmt.Errorf("journal: head: %w", err)
	}
	return v.Int64, nil
}
