package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// §6.3 与契约② 那条"delta 级输出不进 event_journal"合起来把这一格切成两半：
// 内容走 `execution_output_chunks` 的行（面板补拉的唯一途径，yaml `getExecutionOutput`），
// 而**批次这个事实**走 §6 登记好的那一行 `execution_output_flushed`
// （`specs/events/event-contract-v0.1.md:215`，载荷 `batch_first_seq` `batch_last_seq` `char_count` `channel`，Audit `–`）。
// 这条事件名**不是新登记的**（T-424 边界二：不许为这一格在 §6 单开名字），它一直在那张表里，
// 缺的从来是写者 —— 写者是这条常驻入站泵（REQ-TURN-1 裁「Ⅰ」之后才有"帧到了有人收"这件事）。
//
// 写形状照 `internal/session/record.go` 的 `RecordMessage`（同族先例，不是我自己发明的第三条路）：
// 先 `Append` 明细行，再 `Touch` 父聚合并**在同一笔里**发那一条事件。
// 为什么不能只 `Append`：那就是"行有了、批次这个事实没有"，而 `Append` 不发事件是平台层的规矩（REQ-DB-6），
// 把两件事分在两次调用上正是 T-168 那类事故的形状。

// ErrOutputChannelUnwritten 是"这一帧的 channel 在 `execution_output_chunks` 的词表里没有位置"。
// 典型：`stream/delta` 带着 `THOUGHT` 来 —— 适配器那一侧有四个值（ASSISTANT/THOUGHT/TOOL/STATUS），
// 迁移里那一列的 CHECK 只有 ASSISTANT/STDOUT/STDERR/TOOL/STATUS（`000001_core_schema.sql`）。
// 这一格**不折算、不硬塞**：THOUGHT 不是"想说成 ASSISTANT 的正文"，也不该由我去改 CHECK（迁移冻结、000001 归 db）。
// 报出来，裁由契约与 db。
var ErrOutputChannelUnwritten = errors.New("execution: 这一帧的 channel 在 execution_output_chunks 的词表里没有位置")

// outputChunkChannels 是那列 CHECK 认的五个值 —— 抄自迁移（`000001_core_schema.sql:350~352`），
// 不是另立一份词表。抄来的东西必漂移，所以对账不靠读那段文本、靠**让库自己裁决**：
// `output_test.go` 里那五个值逐个真写一遍（库不收 ⇒ 红），并断 CHECK 之外的 `THOUGHT` 在本层就被拒。
var outputChunkChannels = map[string]bool{
	"ASSISTANT": true, "STDOUT": true, "STDERR": true, "TOOL": true, "STATUS": true,
}

// OutputBatch 是一次批次：同一 Execution、同一 channel 的一串 delta（按到达顺序）。
// 批次的键里必须有 channel，因为 §6 那一行的载荷只有一个 `channel` 键 —— 混两条 channel 进一批，
// 那个键就只能说谎。切换 channel 因此是一次 flush 的边界，不是"攒着以后合并"。
type OutputBatch struct {
	Channel string
	Texts   []string
}

// FlushResult 是一次批次落盘的可读结果（泵把它喊进日志，也交给判据）。
type FlushResult struct {
	Channel   string
	FirstSeq  int64
	LastSeq   int64
	CharCount int64
	Rows      int
}

// FlushOutput 把一批 delta 落成 chunk 行 + 恰一条 `execution_output_flushed`。
//
// 三条不许：
//   - **不许改状态**：SET 里只有 `updated_at`（与 `RecordClaim` 同一条形状 —— 输出批次不是状态变化的证据）；
//     而且不许为"让面板看见在跑"顺手补一个 status（契约② §6 规则 1：开始证明只认 dispatched）。
//   - **不许发第二条事件**：一批一行一事件，重放/续跑要复用同一批内容的前提是"帧已经去重过了"
//     （`internal/process` 按 §6 规则 5 的 (turn_ref, sequence_no) 丢重复），所以这里不再另设幂等键；
//     这一列的 seq 是 Core 自己按 `max(sequence_no)+1` 排的（同 messages 的先例），
//     UNIQUE(execution_id, sequence_no) 因此是"两次 flush 不会写到同一号"的兜底而不是幂等门。
//   - **不许在没这条 Execution 的时候写行，也不许往已定局的行后追加**：先 `Get` 一次（外键也会挡，
//     但外键文案不是我能喊给面板的话），终态那一格与 `RecordClaim` 同形。
//
// 空批次明确失败：一次"什么都没收到"不该留下一行 flush —— 那会把"这批有内容"这句谎写进事件。
func (s *Service) FlushOutput(ctx context.Context, tx *db.Tx, id string, batch OutputBatch,
	cmd Command) (FlushResult, error) {
	if strings.TrimSpace(id) == "" {
		return FlushResult{}, fmt.Errorf("%w: 缺 execution id", ErrInvalidExecution)
	}
	if strings.TrimSpace(cmd.EventID) == "" {
		return FlushResult{}, fmt.Errorf("%w: 缺 event_id（§3 的 UUIDv7 由调用方铸，重放要复用同一个）", ErrInvalidExecution)
	}
	if len(batch.Texts) == 0 {
		return FlushResult{}, fmt.Errorf("%w: 空批次不是一条 flush（没有内容就不该说「落了一批」）", ErrInvalidExecution)
	}
	channel := strings.ToUpper(strings.TrimSpace(batch.Channel))
	if !outputChunkChannels[channel] {
		return FlushResult{}, fmt.Errorf("%w: %q", ErrOutputChannelUnwritten, batch.Channel)
	}
	cur, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return FlushResult{}, err
	}
	// 已定局的行不接受迟到的输出：与 `RecordClaim` 同一条理由（`claim.go:57`）—— 在终态之后追加内容会把
	// "这批达在结束之前"这句假话写进事件序列。泵的批次落在终态那一笔**之前**（`turnpump.go` 的帧边界顺序），
	// 所以正常链路走不到这一支；走到就是对端在收尾之后还推内容 ⇒ 喊出来，不写。
	if Terminal(cur.Status) {
		return FlushResult{}, fmt.Errorf("execution: %s 已定局（%s），迟到的输出不落库: %w", id, cur.Status, ErrIllegalTransition)
	}

	// 与 RecordMessage 同一件做法：批次号在**这一列的自有范围内**取 max+1，跨 channel 共用一个序列
	// —— UNIQUE(execution_id, sequence_no) 是按执行排的，不是按 channel 排的（迁移就这么写的）。
	var maxSeq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(max(sequence_no), 0) FROM execution_output_chunks WHERE execution_id = ?`, id).
		Scan(&maxSeq); err != nil {
		return FlushResult{}, fmt.Errorf("execution: 取 %s 的输出水位: %w", id, err)
	}

	now := time.Now().UnixMilli()
	res := FlushResult{FirstSeq: maxSeq + 1, Channel: channel}
	chars := int64(0)
	for _, text := range batch.Texts {
		// 空 text 不落行：§6 那行要说的是"这批写了几个字符"，写一行空内容会把补拉游标往前推而没给内容。
		if text == "" {
			continue
		}
		res.Rows++
		res.LastSeq = maxSeq + int64(res.Rows)
		chars += int64(len([]rune(text)))
		if _, err := tx.Append("execution_output_chunks",
			[]string{"execution_id", "sequence_no", "channel", "content", "created_at"},
			[]any{id, res.LastSeq, channel, text, now}); err != nil {
			return FlushResult{}, fmt.Errorf("execution: append output chunk %s#%d: %w", id, res.LastSeq, err)
		}
	}
	if res.Rows == 0 {
		return FlushResult{}, fmt.Errorf("%w: 一批里全是空内容（不写行、也不发事件）", ErrInvalidExecution)
	}
	res.CharCount = chars

	payload, err := Payload(OutputFlushedPayload{
		BatchFirstSeq: res.FirstSeq, BatchLastSeq: res.LastSeq,
		CharCount: chars, Channel: channel,
	})
	if err != nil {
		return FlushResult{}, fmt.Errorf("execution: execution_output_flushed payload: %w", err)
	}
	// 一批的内容多行、事件只一条，而事件必须挂在聚合的**下一个版本**上（平台层_stamp_版本）：
	// 这就是为什么这里仍是 Touch 而不是 AppendEventOnly —— 后者只给事件不给"这一批真的动了这一聚合"。
	if _, err := tx.Touch(db.AggExecution, id, "updated_at = ?", []any{now}, db.EventDraft{
		EventID: cmd.EventID, Type: EventOutputFlushed, Aggregate: db.AggExecution, AggregateID: id,
		CorrelationID: cmd.CorrelationID, CausationID: cmd.CausationID,
		PrincipalID: cmd.PrincipalID, Payload: payload,
	}); err != nil {
		return FlushResult{}, fmt.Errorf("execution: touch %s（execution_output_flushed，只写 updated_at）: %w", id, err)
	}
	return res, nil
}

// OutputChunk 是 `execution_output_chunks` 的一行（yaml `OutputChunk`：sequence_no/channel/content/created_at
// 四个必填 + id/execution_id 两格）。读面与写面同在一个包内（T-424 边界三：**面板只经这里**，不直查那张表）。
type OutputChunk struct {
	ID          int64  `json:"id"`
	ExecutionID string `json:"execution_id"`
	SequenceNo  int64  `json:"sequence_no"`
	Channel     string `json:"channel"`
	Content     string `json:"content"`
	CreatedAt   int64  `json:"created_at"`
}

// ListOutput 是 `GET /executions/{id}/output` 的读法：`after_seq` 之后的批次行，按 sequence_no 升序，
// 最多 limit 条。为什么游标是**序号**而不是时间戳：`sequence_no` 在一行执行内由本包自己按 max+1 排（见
// FlushOutput），单调、无并列、且与 UNIQUE(execution_id, sequence_no) 同一个键；created_at 三条同毫秒就并列，
// 那种游标会要么漏行要么重行（messages / notifications 那两族同一条理由）。
//
// 返回的 `HasMore` 是"这一页之后还有没有"：多读一条来判断，不把整表数一遍。
func (r *Repository) ListOutput(ctx context.Context, executionID string, afterSeq int64, limit int) ([]OutputChunk, bool, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, execution_id, sequence_no, channel, content, created_at
		 FROM execution_output_chunks
		 WHERE execution_id = ? AND sequence_no > ?
		 ORDER BY sequence_no ASC LIMIT ?`, executionID, afterSeq, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("execution: list output of %s: %w", executionID, err)
	}
	defer rows.Close()
	var out []OutputChunk
	for rows.Next() {
		var c OutputChunk
		if err := rows.Scan(&c.ID, &c.ExecutionID, &c.SequenceNo, &c.Channel, &c.Content, &c.CreatedAt); err != nil {
			return nil, false, fmt.Errorf("execution: scan output of %s: %w", executionID, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("execution: read output of %s: %w", executionID, err)
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, rows.Err()
}
