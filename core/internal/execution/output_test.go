package execution_test

// §6.3 那一刀两半的写者判据（T-424）：内容走 `execution_output_chunks` 的行，
// **批次这个事实**走恰一条 `execution_output_flushed`（`specs/events/event-contract-v0.1.md:215`，
// 载荷四键 `batch_first_seq` `batch_last_seq` `char_count` `channel`，Audit 列 `–`）。
//
// 这一格里最容易写错的三件事，各有一条用例对着：
//   1. **借这一笔把状态推一格** —— 行从 STARTING 起步，终态列取 COALESCE(-1/'')，所以 SET 里混进
//      `status`/`finished_at` 立刻红（与 `claim_test.go` 同一条打法）；
//   2. **一批发多条事件或多批发一条** —— 号段与行数逐批对，两批之间的水位必须续上；
//   3. **词表抄两份必漂移** —— 那列 CHECK 认的五个值**逐个真写一遍**（库不收 ⇒ 红），
//      CHECK 之外的 `THOUGHT`（适配器有、这张表没有）必须在进库之前就被本层拒掉、且一行不写。
//      对账靠让数据库自己裁决，不靠把那段 CHECK 文本抄第二遍。
//
// 审计那一格走绊线而不是数表：理由与 `claim_test.go` 的 `svcWithAuditSpy` 同一条（T-421 ② 换掉了
// 业务包测试里直查审计表的那句，C14 又禁止本包 import 规则包）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// outputOnce 开一笔写事务跑一次 FlushOutput：与 `claimOnce` 同形，域要的是调用方给事务。
func outputOnce(ctx context.Context, d *db.DB, svc *execution.Service, id string,
	batch execution.OutputBatch, cmd execution.Command) (execution.FlushResult, error) {
	var res execution.FlushResult
	err := d.Write(ctx, func(tx *db.Tx) error {
		r, e := svc.FlushOutput(ctx, tx, id, batch, cmd)
		res = r
		return e
	})
	return res, err
}

func outputCmd(eventID string) execution.Command {
	return execution.Command{EventID: eventID, PrincipalID: "system",
		CorrelationID: "corr-out", CausationID: "corr-out"}
}

// chunkRows 把这一条执行的 chunk 行原样读回来（判据要看的是"库里到底落了几行、落的什么"）。
func chunkRows(t *testing.T, d *db.DB, id string) []execution.OutputChunk {
	t.Helper()
	rows, err := d.Read.Query(`SELECT id, execution_id, sequence_no, channel, content, created_at
		FROM execution_output_chunks WHERE execution_id = ? ORDER BY sequence_no`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []execution.OutputChunk
	for rows.Next() {
		var c execution.OutputChunk
		if err := rows.Scan(&c.ID, &c.ExecutionID, &c.SequenceNo, &c.Channel, &c.Content, &c.CreatedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func outputVersion(t *testing.T, d *db.DB, id string) int64 {
	t.Helper()
	var v int64
	if err := d.Read.QueryRow(`SELECT version FROM executions WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestFlushOutputWritesChunkRowsAndOneBatchEvent 是那一笔写的正身（六对）：
// 行数 / 号段 / 内容原样 / 恰一条事件 / 载荷四键 / **状态一格没动**。
// 文本特意用中文：`char_count` 按**字素**数（3 字），按字节数会得到 9 —— 这一格不这么打就看不出来。
func TestFlushOutputWritesChunkRowsAndOneBatchEvent(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	spy := &auditSpy{}
	svc := svcWithAuditSpy(spy)
	ctx := context.Background()
	// ex-2 是 STARTING（seed 播的）：非终态、且不是 RUNNING —— 从它出发，"顺手推状态"那种写法必红。
	before := outputVersion(t, d, "ex-2")

	res, err := outputOnce(ctx, d, svc, "ex-2",
		execution.OutputBatch{Channel: "ASSISTANT", Texts: []string{"你", "好", "吗"}}, outputCmd("ev-out-1"))
	if err != nil {
		t.Fatalf("一批三块没落成：%v", err)
	}
	got := chunkRows(t, d, "ex-2")
	if len(got) != 3 {
		t.Fatalf("三块 delta 该落三行，实际 %d 行 %+v", len(got), got)
	}
	for i, want := range []string{"你", "好", "吗"} {
		if got[i].Content != want {
			t.Errorf("第 %d 行内容该原样是 %q，实际 %q（delta 不许被解读、拼接或裁剪）", i+1, want, got[i].Content)
		}
		if got[i].SequenceNo != int64(i+1) {
			t.Errorf("第 %d 行的 sequence_no 该是 %d，实际 %d", i+1, i+1, got[i].SequenceNo)
		}
		if got[i].Channel != "ASSISTANT" {
			t.Errorf("channel 该原样落，实际 %q", got[i].Channel)
		}
	}
	if res.Rows != 3 || res.FirstSeq != 1 || res.LastSeq != 3 || res.CharCount != 3 {
		t.Errorf("返回值要与库里那三行对上，实际 %+v", res)
	}
	if st, fin, rc := execRowWide(t, d, "ex-2"); st != "STARTING" || fin != -1 || rc != "" {
		t.Errorf("输出批次不是状态变化的证据：应是 STARTING / finished_at NULL / result_code NULL，实际 %s / %d / %q", st, fin, rc)
	}
	if v := outputVersion(t, d, "ex-2"); v != before+1 {
		t.Errorf("动了一列就要升一版（ADR-017）：v%d → v%d", before, v)
	}
	// 一批 = 恰一条事件，且载荷只有 §6 那四键（+ 平台层补的 schema）。
	evID, evType, payload := journalRow(t, d, "ex-2")
	if evType != execution.EventOutputFlushed {
		t.Errorf("事件名该是 §6 :215 那一行 execution_output_flushed，实际 %q", evType)
	}
	for _, want := range []string{`"batch_first_seq":1`, `"batch_last_seq":3`, `"char_count":3`, `"channel":"ASSISTANT"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload 缺 %s，原文 %q", want, payload)
		}
	}
	for _, ban := range []string{`"status"`, `"text"`, `"content"`, `"items"`} {
		if strings.Contains(payload, ban) {
			t.Errorf("payload 里不许有 %s（内容走行、事件只报批次；把内容抄进事件就是 §6.3 明令禁止的那件事）：%q", ban, payload)
		}
	}
	if n := journalCount(t, d, "ex-2"); n != 1 {
		t.Errorf("一批该恰一条事件，实际 %d 条（最近那条 id=%s）", n, evID)
	}
	if n := journalCount(t, d, "task-1"); n != 0 {
		t.Errorf("输出不进 §5.3 的边集 ⇒ task 一侧零事件，实际 %d 条", n)
	}
	if spy.calls != 0 {
		t.Errorf("§6 那一行的 Audit 列是 `–` ⇒ 这一笔不该叫审计，实际叫了 %d 次", spy.calls)
	}
}

// TestFlushOutputAcceptsEveryChannelTheTableAllows 是"词表抄两份必漂移"的那条对账：
// 那列 CHECK 认的五个值逐个真写一遍。**为什么这比读迁移文本强**：文本改了而这里没改，读文本的判据
// 会跟着一起错；让库自己裁决，库里不收 ⇒ 这一格红，库里收而这里拒 ⇒ 那一格也红（两面都咬）。
func TestFlushOutputAcceptsEveryChannelTheTableAllows(t *testing.T) {
	for _, ch := range []string{"ASSISTANT", "STDOUT", "STDERR", "TOOL", "STATUS"} {
		t.Run(ch, func(t *testing.T) {
			d := openDB(t)
			seed(t, d)
			seedSystemPrincipal(t, d)
			initRedactionForWrite(t)
			svc := execution.NewService()
			ctx := context.Background()
			// 每格一座自己的库：五个值互不干扰，红了直接指出是哪一个 channel。
			if _, err := outputOnce(ctx, d, svc, "ex-2",
				execution.OutputBatch{Channel: ch, Texts: []string{"x"}}, outputCmd("ev-ch-"+ch)); err != nil {
				t.Fatalf("channel=%s 是该表认的值（迁移 :350~352），本层拒了：%v", ch, err)
			}
			got := chunkRows(t, d, "ex-2")
			if len(got) != 1 || got[0].Channel != ch {
				t.Fatalf("该落一行 channel=%s，实际 %+v", ch, got)
			}
			if n := eventsOfType(t, d, "ex-2", execution.EventOutputFlushed); n != 1 {
				t.Errorf("恰一条 execution_output_flushed，实际 %d 条", n)
			}
		})
	}
}

// TestFlushOutputRefusesThoughtWithoutWriting 钉的是"适配器有四个通道、这张表只有五个值"那一格：
// `THOUGHT` 在两边都无家可归 ⇒ **不折算成 ASSISTANT、不硬塞、不改 CHECK**，明确失败且一行不写。
// 为什么不能顺手折算：思考过程与正文混进同一列之后，面板再也分不开那两段，而这条信息只有 channel 带着。
func TestFlushOutputRefusesThoughtWithoutWriting(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	svc := execution.NewService()
	ctx := context.Background()

	_, err := outputOnce(ctx, d, svc, "ex-2",
		execution.OutputBatch{Channel: "THOUGHT", Texts: []string{"我在想"}}, outputCmd("ev-thought"))
	if err == nil {
		t.Fatal("THOUGHT 在那列的 CHECK 里没有位置，这一笔该明确失败")
	}
	if !errors.Is(err, execution.ErrOutputChannelUnwritten) {
		t.Errorf("该以 ErrOutputChannelUnwritten 失败（调用方要能分清「词表没有这一格」与「库坏了」），实际 %v", err)
	}
	if !strings.Contains(err.Error(), "THOUGHT") {
		t.Errorf("报错里要点名是哪个值没处放，实际 %v", err)
	}
	if n := len(chunkRows(t, d, "ex-2")); n != 0 {
		t.Errorf("被拒的一格零行，实际 %d 行", n)
	}
	if n := journalCount(t, d, "ex-2"); n != 0 {
		t.Errorf("被拒的一格零事件，实际 %d 条", n)
	}
}

// TestFlushOutputSecondBatchContinuesTheSequence 钉两批之间的水位：第二批从 4 起、号段自报正确、
// 事件两条各说各的号段。为什么这一格有牙：`max(sequence_no)+1` 写成 `count(*)+1` 在今天等价，
// 但一旦有一批被拒/被删过就分叉 —— 而 UNIQUE(execution_id, sequence_no) 会把分叉变成整笔回滚。
func TestFlushOutputSecondBatchContinuesTheSequence(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	svc := execution.NewService()
	ctx := context.Background()

	first, err := outputOnce(ctx, d, svc, "ex-2",
		execution.OutputBatch{Channel: "ASSISTANT", Texts: []string{"a", "b"}}, outputCmd("ev-b1"))
	if err != nil {
		t.Fatalf("第一批没落成：%v", err)
	}
	if _, err := outputOnce(ctx, d, svc, "ex-2",
		execution.OutputBatch{Channel: "TOOL", Texts: []string{"c"}}, outputCmd("ev-b2")); err != nil {
		t.Fatalf("第二批没落成：%v", err)
	}
	if first.FirstSeq != 1 || first.LastSeq != 2 {
		t.Errorf("第一批号段该是 1..2，实际 %+v", first)
	}
	got := chunkRows(t, d, "ex-2")
	if len(got) != 3 {
		t.Fatalf("两批共三行，实际 %d 行", len(got))
	}
	for i, want := range []int64{1, 2, 3} {
		if got[i].SequenceNo != want {
			t.Errorf("第 %d 行序号该续到 %d，实际 %d", i+1, want, got[i].SequenceNo)
		}
	}
	if got[2].Channel != "TOOL" {
		t.Errorf("第二批的 channel 该是 TOOL，实际 %q", got[2].Channel)
	}
	// 两条事件、各自报自己的号段（面板收到哪一条就补拉那一段）。
	if n := eventsOfType(t, d, "ex-2", execution.EventOutputFlushed); n != 2 {
		t.Fatalf("两批该两条事件，实际 %d 条", n)
	}
	secondPayload := ""
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE aggregate_id = ?
		AND event_type = 'execution_output_flushed' ORDER BY seq DESC LIMIT 1`, "ex-2").Scan(&secondPayload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"batch_first_seq":3`, `"batch_last_seq":3`, `"char_count":1`, `"channel":"TOOL"`} {
		if !strings.Contains(secondPayload, want) {
			t.Errorf("第二条事件的载荷缺 %s，原文 %q", want, secondPayload)
		}
	}
	if v := outputVersion(t, d, "ex-2"); v != 3 {
		t.Errorf("两批 = 两版（v1 起步），实际 v%d", v)
	}
}

// TestFlushOutputRefusesWithoutWritingAnything 把八道"不该写"的闸一起钉。
// 每格都要求**零行 + 零事件 + 版本不动**。"空批次留一行 flush"与"全空 text 也发事件"
// 都是会把谎写进历史的形状。
func TestFlushOutputRefusesWithoutWritingAnything(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		batch     execution.OutputBatch
		eventID   string
		wantErr   error
		wantTxt   string
		rowExists bool // 库里真有这一行 ⇒ 才能断"版本没动"（空 id 与 ex-ghost 两格没行可断）
	}{
		{name: "空 execution id", id: "", batch: execution.OutputBatch{Channel: "ASSISTANT", Texts: []string{"x"}},
			eventID: "ev-x", wantErr: execution.ErrInvalidExecution, wantTxt: "缺 execution id"},
		{name: "缺事件身份（域不生成 id）", id: "ex-2", batch: execution.OutputBatch{Channel: "ASSISTANT", Texts: []string{"x"}},
			wantErr: execution.ErrInvalidExecution, wantTxt: "event_id", rowExists: true},
		{name: "空批次不是一条 flush", id: "ex-2", batch: execution.OutputBatch{Channel: "ASSISTANT"},
			eventID: "ev-x", wantErr: execution.ErrInvalidExecution, wantTxt: "空批次", rowExists: true},
		{name: "一批里全是空内容", id: "ex-2", batch: execution.OutputBatch{Channel: "ASSISTANT", Texts: []string{"", ""}},
			eventID: "ev-x", wantErr: execution.ErrInvalidExecution, wantTxt: "全是空内容", rowExists: true},
		{name: "词表外的 channel", id: "ex-2", batch: execution.OutputBatch{Channel: "THOUGHT", Texts: []string{"x"}},
			eventID: "ev-x", wantErr: execution.ErrOutputChannelUnwritten, wantTxt: "词表", rowExists: true},
		{name: "channel 缺失", id: "ex-2", batch: execution.OutputBatch{Texts: []string{"x"}},
			eventID: "ev-x", wantErr: execution.ErrOutputChannelUnwritten, wantTxt: "", rowExists: true},
		{name: "已定局的行不接受迟到的输出", id: "ex-1", batch: execution.OutputBatch{Channel: "ASSISTANT", Texts: []string{"x"}},
			eventID: "ev-x", wantErr: execution.ErrIllegalTransition, wantTxt: "已定局", rowExists: true},
		{name: "行不存在", id: "ex-ghost", batch: execution.OutputBatch{Channel: "ASSISTANT", Texts: []string{"x"}},
			eventID: "ev-x", wantErr: nil, wantTxt: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := openDB(t)
			seed(t, d)
			seedSystemPrincipal(t, d)
			initRedactionForWrite(t)
			spy := &auditSpy{}
			svc := svcWithAuditSpy(spy)
			ctx := context.Background()
			var before int64
			if tc.rowExists {
				before = outputVersion(t, d, tc.id)
			}
			_, err := outputOnce(ctx, d, svc, tc.id, tc.batch, outputCmd(tc.eventID))
			if err == nil {
				t.Fatalf("这一格该明确失败")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("该以 %v 失败，实际 %v", tc.wantErr, err)
			}
			if tc.wantTxt != "" && !strings.Contains(err.Error(), tc.wantTxt) {
				t.Errorf("报错里要看得见原因（含 %q），实际 %v", tc.wantTxt, err)
			}
			if tc.id != "" {
				if n := len(chunkRows(t, d, tc.id)); n != 0 {
					t.Errorf("被拒的一格不许留 chunk 行，实际 %d 行", n)
				}
				if n := journalCount(t, d, tc.id); n != 0 {
					t.Errorf("被拒的一格零事件，实际 %d 条", n)
				}
			}
			if tc.rowExists {
				if v := outputVersion(t, d, tc.id); v != before {
					t.Errorf("没写就不许升版本：v%d → v%d", before, v)
				}
			}
			if spy.calls != 0 {
				t.Errorf("被拒的一格不许叫审计：%d 次", spy.calls)
			}
		})
	}
}

// TestListOutputPagesBySequence 是补拉游标的本体：`after_seq` 之后、按序号升序、一页取不满就不报还有，
// 而**沿游标走完必须一行不重、一行不漏**。
// 为什么不用 created_at 当游标：同毫秒的三块会并列，那种游标要么漏要么重 —— 这一条就是它的反例位。
func TestListOutputPagesBySequence(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	svc := execution.NewService()
	ctx := context.Background()
	for i, b := range []execution.OutputBatch{
		{Channel: "ASSISTANT", Texts: []string{"1", "2"}},
		{Channel: "TOOL", Texts: []string{"3", "4"}},
		{Channel: "STATUS", Texts: []string{"5"}},
	} {
		if _, err := outputOnce(ctx, d, svc, "ex-2", b, outputCmd("ev-page-"+string(rune('a'+i)))); err != nil {
			t.Fatalf("第 %d 批没落成：%v", i+1, err)
		}
	}
	repo := execution.NewRepository(d.Read)

	seen := []int64{}
	cursor := int64(0)
	for page := 0; ; page++ {
		if page > 10 {
			t.Fatalf("游标没往前走，第 %d 页还停在 after_seq=%d（死循环）", page, cursor)
		}
		items, hasMore, err := repo.ListOutput(ctx, "ex-2", cursor, 2)
		if err != nil {
			t.Fatalf("ListOutput(after_seq=%d): %v", cursor, err)
		}
		for _, it := range items {
			seen = append(seen, it.SequenceNo)
		}
		if len(items) == 0 {
			break
		}
		if hasMore != (len(items) == 2) {
			t.Errorf("after_seq=%d 这一页 %d 条却报了 hasMore=%v（多读一条来判断，不许整表数）", cursor, len(items), hasMore)
		}
		cursor = items[len(items)-1].SequenceNo
		if !hasMore {
			break
		}
	}
	if len(seen) != 5 {
		t.Fatalf("走完该看见 5 行，实际 %d 行：%v", len(seen), seen)
	}
	for i, want := range []int64{1, 2, 3, 4, 5} {
		if seen[i] != want {
			t.Fatalf("顺序该是 1..5（升序、不重不漏），实际 %v", seen)
		}
	}
	// 走到底之后：还问一次，必须是空页 + hasMore=false（200 空 items 是诚实答案，不是冒充）。
	items, hasMore, err := repo.ListOutput(ctx, "ex-2", 5, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || hasMore {
		t.Errorf("水位之后没有内容 ⇒ 空页且 hasMore=false，实际 %d 条 hasMore=%v", len(items), hasMore)
	}
}

// TestOutputFlushedIsNotAStateEdgeAndCarriesNoAudit 与 `claim_test.go` 同形的那条反向钉子：
// 它今天**确实**是一条状态迁移事件名（`execution_started` 那一族之外），所以更要钉住它不在 `EventOf` 里 ——
// 一旦有人把它补进状态机，`advance` 就可能在某条边上凭空发一条"批次"，把 §6.3 那半格又搅浑。
func TestOutputFlushedIsNotAStateEdgeAndCarriesNoAudit(t *testing.T) {
	all := []execution.State{execution.Created, execution.Starting, execution.Running,
		execution.WaitingApproval, execution.WaitingInput, execution.Paused, execution.Transferring,
		execution.Succeeded, execution.Failed, execution.Cancelled, execution.Interrupted, execution.Unknown}
	for _, from := range all {
		for _, to := range all {
			if got := execution.EventOf(from, to); got == execution.EventOutputFlushed {
				t.Errorf("EventOf(%s,%s) 产出了 execution_output_flushed ⇒ 它被登记成状态边了（它不是）", from, to)
			}
		}
	}
	if execution.RequiresAudit(execution.EventOutputFlushed) {
		t.Error("§6 那一行的 Audit 列是 `–` ⇒ output_flushed 不许进 auditRequired")
	}
	m, err := execution.Payload(execution.OutputFlushedPayload{BatchFirstSeq: 1, BatchLastSeq: 1, CharCount: 1, Channel: "ASSISTANT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 4 {
		t.Errorf("载荷恰 §6 :215 那四键，实际 %v", m)
	}
}
