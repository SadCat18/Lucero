package main

// 输出批次那一腿的泵侧判据（T-424）。域那一格管"一批写什么"（`internal/execution/output_test.go`），
// 泵管的是只有它管得到的那三件事：
//   - **攒批**：连续几帧 delta 不许一块一行、一笔事件（那是把 §6.3 那半格又拆碎）；
//   - **边界**：任何别的帧都是一次边界，`turn/finished` 也算 ⇒ 批次必须落在终态那一笔**之前**
//     （有序证据：事件表里的 seq 对拍，不是"某一瞬间看到过行"的单点快照 —— 同一族第 82 批的口径）；
//   - **泵退出时不丢已达内容**：`pump` 退出那一句 defer 是唯一替"没说完的半句"担保的地方。
//
// 还有两种结局各钉一格：词表外的 channel（THOUGHT 是适配器有、这张表没有的那一个）必须**喊出来而不落库**，
// 也不许把同一轮的收尾连坐掉；真子进程那一腿要看见真帧真的变成了库里的行（值不是测试自己塞进去的）。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
)

func countPumpChunks(t *testing.T, d *db.DB, id string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM execution_output_chunks WHERE execution_id = ?`, id).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// pumpChunkAt 读某一号那一行的 channel 与内容（批次对不对，看的是逐行的形状而不是一个计数）。
func pumpChunkAt(t *testing.T, d *db.DB, id string, seq int64) (string, string) {
	t.Helper()
	var channel, content string
	err := d.Read.QueryRow(`SELECT channel, content FROM execution_output_chunks
		WHERE execution_id = ? AND sequence_no = ?`, id, seq).Scan(&channel, &content)
	if err != nil {
		t.Fatalf("读 %s #%d: %v", id, seq, err)
	}
	return channel, content
}

// journalSeq 取某条事件的 `seq`（有序证据的尺子）；没有就报 0，调用方据此红。
func journalSeq(t *testing.T, d *db.DB, id, evType string) int64 {
	t.Helper()
	var seq int64
	err := d.Read.QueryRow(`SELECT COALESCE(max(seq), 0) FROM event_journal
		WHERE aggregate_id = ? AND event_type = ?`, id, evType).Scan(&seq)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

// deltaFrame 造一帧真形状的 delta（合成帧用例的输入；判的是"哪一帧动笔"，与 claim 那两条同手法）。
func deltaFrame(id, channel, text string) process.Inbound {
	return process.Inbound{ExecutionID: id, Frame: &adapter.StreamFrame{
		Method: adapter.MethodStreamDelta, Channel: channel, Text: text}}
}

func TestPumpBatchesDeltasUntilTheFrameBoundary(t *testing.T) {
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: "不需要孩子"}})
	hub := pipe.hub
	restore := swapLogf(hub)
	defer restore()
	var tap logTap
	hub.logf = tap.f
	seedClaimRow(t, d, "ex-o")
	beforeAudit, beforeHash := pumpAuditHead(t, d)

	for _, txt := range []string{"你", "好", "吗"} {
		hub.handle("fake", 1, deltaFrame("ex-o", "ASSISTANT", txt))
	}
	// 攒批的正身：三帧到了，库里还一行都不该有、一声"已落"都不该喊。
	if n := countPumpChunks(t, d, "ex-o"); n != 0 {
		t.Fatalf("三帧 delta 该攒在一处（一块一行就把 §6.3 那半格拆碎了），实际已落 %d 行", n)
	}
	if n := tap.count("输出批次已落"); n != 0 {
		t.Fatalf("没到边界不该出声，实际 %d 句：%v", n, tap.lines)
	}

	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-o",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnFinished, ResultCode: "SUCCEEDED"}})

	if n := countPumpChunks(t, d, "ex-o"); n != 3 {
		t.Fatalf("边界到了该落三行，实际 %d 行：%v", n, tap.lines)
	}
	for i, want := range []string{"你", "好", "吗"} {
		if ch, content := pumpChunkAt(t, d, "ex-o", int64(i+1)); ch != "ASSISTANT" || content != want {
			t.Errorf("第 %d 行该是 ASSISTANT/%q，实际 %s/%q（delta 不许被拼接或改写）", i+1, want, ch, content)
		}
	}
	if n := pumpEventsOf(t, d, "execution_output_flushed"); n != 1 {
		t.Errorf("一批恰一条 execution_output_flushed，实际 %d 行", n)
	}
	// 有序证据（不是单点快照）：批次事件必须**排在**终态事件之前。
	flushSeq := journalSeq(t, d, "ex-o", "execution_output_flushed")
	finSeq := journalSeq(t, d, "ex-o", "execution_succeeded")
	if !(flushSeq > 0 && finSeq > 0 && flushSeq < finSeq) {
		t.Errorf("批次必须落在终态那一笔之前（面板补拉不该看见「终态有了、末尾几块还没有」）：flush seq=%d finished seq=%d",
			flushSeq, finSeq)
	}
	if st := pumpExecStatus(t, d, "ex-o"); st != "SUCCEEDED" {
		t.Errorf("收尾那一笔照常该落终态，实际 %s", st)
	}
	if n := tap.count("输出批次已落"); n != 1 {
		t.Errorf("一次边界一声，实际 %d 句：%v", n, tap.lines)
	}
	// §6 :215 那行的 Audit 列是 `–` ⇒ 链尾一格都不许多（这一条腿里只有批次 + 一条正常收尾，
	// 两者都不带审计，所以这里能断"没动"而不是"动了几条"）。
	afterAudit, afterHash := pumpAuditHead(t, d)
	if afterAudit != beforeAudit || afterHash != beforeHash {
		t.Errorf("输出批次不许动审计链：链尾从 seq=%d/hash=%s 走到 seq=%d/hash=%s", beforeAudit, beforeHash, afterAudit, afterHash)
	}
}

func TestPumpFlushesWhenTheChannelChanges(t *testing.T) {
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: "不需要孩子"}})
	hub := pipe.hub
	restore := swapLogf(hub)
	defer restore()
	var tap logTap
	hub.logf = tap.f
	seedClaimRow(t, d, "ex-ch")

	hub.handle("fake", 1, deltaFrame("ex-ch", "ASSISTANT", "a"))
	hub.handle("fake", 1, deltaFrame("ex-ch", "ASSISTANT", "b"))
	// 换 channel 就是一次边界：上一批必须自己落下去，事件里那唯一的 `channel` 键才不会说谎。
	hub.handle("fake", 1, deltaFrame("ex-ch", "TOOL", "c"))

	if n := countPumpChunks(t, d, "ex-ch"); n != 2 {
		t.Fatalf("换 channel 时该把上一批（两行）落下，实际 %d 行：%v", n, tap.lines)
	}
	if ch, content := pumpChunkAt(t, d, "ex-ch", 1); ch != "ASSISTANT" || content != "a" {
		t.Errorf("第一批首行该是 ASSISTANT/a，实际 %s/%q", ch, content)
	}
	if n := pumpEventsOf(t, d, "execution_output_flushed"); n != 1 {
		t.Errorf("第一批该恰一条事件，实际 %d 条", n)
	}
	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-ch",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnFinished, ResultCode: "SUCCEEDED"}})

	// 第二批（只有 TOOL 那一块）在收尾边界落，序号接着 3 往下排。
	if n := countPumpChunks(t, d, "ex-ch"); n != 3 {
		t.Fatalf("收尾时该把第二批也落掉，实际 %d 行：%v", n, tap.lines)
	}
	if ch, content := pumpChunkAt(t, d, "ex-ch", 3); ch != "TOOL" || content != "c" {
		t.Errorf("第三行该是 TOOL/c（号段跨 channel 续排），实际 %s/%q", ch, content)
	}
	if n := pumpEventsOf(t, d, "execution_output_flushed"); n != 2 {
		t.Errorf("两批两条事件，各说各的号段，实际 %d 条", n)
	}
	if n := tap.count("输出批次已落"); n != 2 {
		t.Errorf("两次边界两声，实际 %d 句：%v", n, tap.lines)
	}
}

// TestPumpRefusesThoughtChannelWithoutWriting 钉"不折算、不硬塞、也不连坐"：
// 适配器有 THOUGHT 这个通道而那张表没有这个值 ⇒ 喊出来、一行不写，而**同一轮的收尾照常落**。
// 为什么必须照常落：把一次拒写折成"这一轮失败了"就是替 Provider 编终态（§6 第 6 条那句反过来的形状）。
func TestPumpRefusesThoughtChannelWithoutWriting(t *testing.T) {
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: "不需要孩子"}})
	hub := pipe.hub
	restore := swapLogf(hub)
	defer restore()
	var tap logTap
	hub.logf = tap.f
	seedClaimRow(t, d, "ex-th")

	hub.handle("fake", 1, deltaFrame("ex-th", "THOUGHT", "想"))
	hub.handle("fake", 1, deltaFrame("ex-th", "THOUGHT", "再想"))
	if n := tap.count("输出批次"); n != 0 {
		t.Fatalf("一批没落也不许悄悄，但更不许在半路就出声，实际：%v", tap.lines)
	}
	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-th",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnFinished, ResultCode: "SUCCEEDED"}})

	if n := countPumpChunks(t, d, "ex-th"); n != 0 {
		t.Errorf("THOUGHT 在那列的词表里没有位置 ⇒ 一行都不该落（折算成 ASSISTANT 就是把思考与正文混成一列），实际 %d 行", n)
	}
	if n := pumpEventsOf(t, d, "execution_output_flushed"); n != 0 {
		t.Errorf("没落成却留了批次事件：%d 条", n)
	}
	line := tap.lineWith("输出批次没落成")
	if line == "" {
		t.Fatalf("这一批必须喊出来（不假装已落），实际日志：%v", tap.lines)
	}
	if !strings.Contains(line, "THOUGHT") {
		t.Errorf("那一声要点名是哪个值没处放，实际 %q", line)
	}
	if st := pumpExecStatus(t, d, "ex-th"); st != "SUCCEEDED" {
		t.Errorf("一次拒写不许连坐同一轮的收尾（那成了替 Provider 编状态），实际 %s", st)
	}
	if n := pumpEventsOf(t, d, "execution_succeeded"); n != 1 {
		t.Errorf("终态事件该恰一条，实际 %d 条", n)
	}
}

// TestPumpFlushesWhatIsPendingWhenThePumpExits 钉 `pump` 那一句 defer：
// 一帧真 delta 之后跟着的 `stream/item` 会把批次立刻带走，所以"退场时还有存货"这一形
// 在真链路上要人为造 —— 这里直接往泵的观察面注一块已达内容，再让真孩子当场死掉。
// 判的是那一句 defer 本身：摘掉它，这一格当场红（本批变异 O-e）。
func TestPumpFlushesWhatIsPendingWhenThePumpExits(t *testing.T) {
	pipe, tap, d, sess := deathFixture(t)
	insertInFlightRow(t, d, "ex-pend", "task-pend", sess)
	armHold(t, pipe)
	// 内存里留一块"没说完的半句"：不发后续帧，也就没有任何边界会带走它。
	pipe.hub.noteOutput("fake", 1, "ex-pend", "ASSISTANT", "没说完的半句")
	if n := countPumpChunks(t, d, "ex-pend"); n != 0 {
		t.Fatalf("这一格的前提是没落库，实际已有 %d 行", n)
	}

	crashIt(t, pipe)
	if !tap.wait(t, "输出批次已落", 20*time.Second) {
		t.Fatalf("泵退场时没把已达内容落库 ⇒ defer 那一腿断了：%v", tap.lines)
	}
	if n := countPumpChunks(t, d, "ex-pend"); n != 1 {
		t.Fatalf("那块半句该落成一行，实际 %d 行", n)
	}
	if ch, content := pumpChunkAt(t, d, "ex-pend", 1); ch != "ASSISTANT" || content != "没说完的半句" {
		t.Errorf("落的内容要与内存里那块逐字对上，实际 %s/%q", ch, content)
	}
	if n := pumpEventsOf(t, d, "execution_output_flushed"); n != 1 {
		t.Errorf("退场这一批也恰一条事件，实际 %d 条", n)
	}
	// 审计链那一格不在这里判：这一条腿走的是崩溃，`execution_unknown` 在 §6 里 Audit 列是 ✓，
	// 链尾**本来就该动**。"输出批次自己不动链"由上面那条合成帧用例判（那里只有批次 + 正常收尾）。
}

// auditHead 读审计链的链尾（走 `audit` 自己那道读嘴，不直查那张表）：seq 由 `Chain.Append`
// 按"链尾 +1"排并自带 fork 绊线 ⇒ 链尾没动 = 一条都没新写。
func pumpAuditHead(t *testing.T, d *db.DB) (int64, string) {
	t.Helper()
	seq, hash, err := audit.HeadQuery(d.Read)(context.Background())
	if err != nil {
		t.Fatalf("读审计链链尾: %v", err)
	}
	return seq, hash
}

// TestPumpWritesRealDeltaFramesIntoChunks 是真子进程那一腿：fake 一轮里发的 delta
// （`cmd/fake-adapter/main.go:564` 那句 "echo: " + text）真的变成库里的行。
// 合成帧给不了这两件：字段是真从帧上读下来的，而且真链路的帧序（state → delta → item）确实带走了批次。
func TestPumpWritesRealDeltaFramesIntoChunks(t *testing.T) {
	pipe, tap, d, sess := deathFixture(t)
	armHoldAndSend(t, pipe, d, "ex-real", "task-real", sess)
	if !tap.wait(t, "输出批次已落", 20*time.Second) {
		t.Fatalf("真 delta 帧没被认成输出内容：%v", tap.lines)
	}
	if n := countPumpChunks(t, d, "ex-real"); n < 1 {
		t.Fatalf("真链路至少该落一块，实际 %d 行：%v", n, tap.lines)
	}
	ch, content := pumpChunkAt(t, d, "ex-real", 1)
	if ch != "ASSISTANT" || !strings.HasPrefix(content, "echo: ") {
		t.Errorf("首行该是对端真发出来的那条 ASSISTANT delta，实际 %s/%q", ch, content)
	}
	if n := pumpEventsOf(t, d, "execution_output_flushed"); n < 1 {
		t.Errorf("落了行就该有批次事件，实际 %d 条", n)
	}
	// 出声与库里配对（老规矩）：事件说的那个号段，必须就是库里那一截 —— 面板收到事件之后
	// 拿 `after_seq` 补拉，靠的就是这两份对上。
	assertEventSaysWhatRowsAre(t, d, "ex-real")
}

// assertEventSaysWhatRowsAre 把 `execution_output_flushed` 载荷里的号段与库里真实行对拍：
// 号段的起点必须真是那一截的最小序号、终点是最大序号，且中间不断号。
// 为什么对拍而不是解析日志那一句：喊话的措辞会改，而"事件说的 == 库里有的"是这一格的合同本身。
func assertEventSaysWhatRowsAre(t *testing.T, d *db.DB, id string) {
	t.Helper()
	var payload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE aggregate_id = ?
		AND event_type = 'execution_output_flushed' ORDER BY seq DESC LIMIT 1`, id).Scan(&payload); err != nil {
		t.Fatalf("读最近那条批次事件: %v", err)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("批次事件的载荷不是 JSON（原文 %q）: %v", payload, err)
	}
	// 按 JSON 数字读回（平台层会补 `schema`，所以这里只点名 §6 那四键里的两个号）。
	seqOf := func(key string) int64 {
		v, ok := p[key].(float64)
		if !ok {
			t.Fatalf("载荷里缺 %s（§6 :215 那四键），原文 %q", key, payload)
		}
		return int64(v)
	}
	first, last := seqOf("batch_first_seq"), seqOf("batch_last_seq")
	var minSeq, maxSeq, n int64
	if err := d.Read.QueryRow(`SELECT COALESCE(min(sequence_no),0), COALESCE(max(sequence_no),0), count(*)
		FROM execution_output_chunks WHERE execution_id = ? AND sequence_no BETWEEN ? AND ?`, id, first, last).
		Scan(&minSeq, &maxSeq, &n); err != nil {
		t.Fatal(err)
	}
	if minSeq != first || maxSeq != last || n != last-first+1 {
		t.Errorf("事件说的号段 #%d..#%d 与库里那一截对不上（实际 #%d..#%d、%d 行）⇒ 补拉会漏行或重行",
			first, last, minSeq, maxSeq, n)
	}
}

// swapLogf 把 hub 的喊话临时接到测试自己的 taps 上，返回"换回原样"那一句。
// 为什么要换回去：`go test -run TestPumpBatches` 这种选择器会让本用例成为**全包唯一**的 logf，
// 那时 defer 若不在，后面任何用例的日志都会静默消失（= 一条只看全跑的判据）。
func swapLogf(hub *turnHub) func() {
	prev := hub.logf
	return func() { hub.logf = prev }
}
