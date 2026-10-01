package main

// 批次表要有主人（T-429，契约① 第 25 条④-b「攒批归属」10-01 主控裁「要」）。
//
// 主控把危害定性改对了一处，我也照它改的定性来判：不是"提前落盘不整齐"，是
// **新代/别台已经把那一轮收尾落定了 ⇒ 老泵退场踢出去的批次撞上契约② §6 规则 8（终态之后只喊不写）
// ⇒ 那段内容永久不在库里**。所以三条边界各有一条读数：
//   (i)  两台适配器并发攒批 + 其中一台泵退场 ⇒ 另一台的批次**一行不动**、随后仍照常落；
//   (ii) 只清自己的之后，**没有任何一代的内容永久滞留**（每一代退场都能清掉自己那格；跳过别人必须出声）；
//   (iii) 零外溢：两个阈值、四键载荷、攒批语义一字没动 —— 这里新增的只有"换了一条连接算一次边界"。
//
// 为什么用合成帧而不是真子进程：这一格判的是**退场那一次扫描的归属**，那是纯本层逻辑；
// 真链路那条（defer 真的挂在泵上）由 `TestPumpFlushesWhatIsPendingWhenThePumpExits` 钉、
// 它的牙是上一批的变异 O-g。两格各证各的，不互相冒充。

import (
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// seedOwnedRow 播一条"有人在攒它"的执行：`FlushOutput` 只认两件事 —— 这一行在不在、定没定局，
// 它不认适配器。所以这一格要的是**两张执行行 + 两个 hub 身份（key + 代次）**，不需要两台真进程。
// 为什么不复用 `seedClaimRow`：它每次都会插一行 `adapter_instances('ad-fake','fake')` ——
// 一条执行好用、两条就撞 `UNIQUE(adapter_key)`（我第一版就是这么红的，2067 那条）。
func seedOwnedRow(t *testing.T, d *db.DB, id, instID, adapterKey string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, health, created_at, updated_at)
		VALUES (?, ?, ?, 'HEALTHY', 1, 1)`, instID, adapterKey, adapterKey); err != nil {
		t.Fatalf("播 adapter_instances(%s): %v", instID, err)
	}
	if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, status, created_at, updated_at)
		VALUES (?, '攒批归属这一格', 'RUNNING', 1, 1)`, "task-"+id); err != nil {
		t.Fatalf("播 tasks(%s): %v", id, err)
	}
	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, ?, ?, 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`, id, "task-"+id, instID); err != nil {
		t.Fatalf("播 executions(%s): %v", id, err)
	}
}

func TestPumpExitOnlyTakesItsOwnBatch(t *testing.T) {
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{
		{key: "fake", displayName: "Fake Adapter", command: "不需要孩子"},
		{key: "qoder", displayName: "Qoder Adapter", command: "不需要孩子"},
	})
	hub := pipe.hub
	restore := swapLogf(hub)
	defer restore()
	var tap logTap
	hub.logf = tap.f
	seedOwnedRow(t, d, "ex-own-a", "ad-fake", "fake")
	seedOwnedRow(t, d, "ex-own-b", "ad-qoder", "qoder")

	// 两台各攒一块：都还没落库（前提，不是结论）。
	hub.noteOutput("fake", 1, "ex-own-a", "ASSISTANT", "甲台的一块")
	hub.noteOutput("qoder", 1, "ex-own-b", "ASSISTANT", "乙台的一块")
	if n := countPumpChunks(t, d, "ex-own-a") + countPumpChunks(t, d, "ex-own-b"); n != 0 {
		t.Fatalf("前提不成立：还没到边界就该一行不落，实际已落 %d 行", n)
	}

	// (i) 甲台的泵退场 ⇒ 只有甲台那一格该被带走。
	hub.flushAllPending("fake", 1)
	if n := countPumpChunks(t, d, "ex-own-a"); n != 1 {
		t.Errorf("本条连接攒的那格该被自己的退场带走，实际落了 %d 行（0 行=没落，>1 行=不该动它）", n)
	}
	if n := countPumpChunks(t, d, "ex-own-b"); n != 0 {
		t.Errorf("别的适配器攒的批次被一台不相干的泵退场带走了（%d 行）⇒ 正是 T-429 要堵的那一格：%d", n, n)
	}
	if n := pumpEventsOf(t, d, "execution_output_flushed"); n != 1 {
		t.Errorf("只该落一条批次事件（别台那条还没到边界），实际 %d 条：%v", n, tap.lines)
	}
	// 跳过别人的那一格必须出声 —— 不然"该由还活着的那条负责"与"没人管了"在日志里长得一样。
	if line := tap.lineWith("不属于本条连接"); line == "" {
		t.Errorf("退场时跳过了别人的批次却一句话没有，实际日志：%v", tap.lines)
	}

	// (ii) 乙台自己退场 ⇒ 它那格照常落：只清自己的**不等于**让内容永久滞留。
	hub.flushAllPending("qoder", 1)
	if n := countPumpChunks(t, d, "ex-own-b"); n != 1 {
		t.Fatalf("乙台自己退场后它那一批该落库，实际 %d 行 ⇒ 归属改成了新的丢失：%v", n, tap.lines)
	}
	if ch, got := pumpChunkAt(t, d, "ex-own-b", 1); ch != "ASSISTANT" || got != "乙台的一块" {
		t.Errorf("落的必须是那块原样内容，实际 %s/%q", ch, got)
	}
	if len(hub.pending) != 0 {
		t.Errorf("两代各自退场之后表里不该剩东西（有主无主都不许滞留），实际 %d 格", len(hub.pending))
	}
}

// TestPumpTreatsANewConnectionAsABatchBoundary 判的是同一台适配器**换代**那一维：
// 老代攒的半批不许被新代的帧悄悄续上（续上之后就没人知道这一批跨了两条连接），
// 换代因此算一次边界；而换代之前的那一格归老代，老代退场时自己带走。
func TestPumpTreatsANewConnectionAsABatchBoundary(t *testing.T) {
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: "不需要孩子"}})
	hub := pipe.hub
	restore := swapLogf(hub)
	defer restore()
	var tap logTap
	hub.logf = tap.f
	seedOwnedRow(t, d, "ex-own-g", "ad-fake", "fake")

	hub.noteOutput("fake", 1, "ex-own-g", "ASSISTANT", "第一代的半句")
	// 第二代同一执行再来一块：先把第一代那一批作为边界落掉，再开一格属于第二代。
	hub.noteOutput("fake", 2, "ex-own-g", "ASSISTANT", "第二代的半句")
	if n := countPumpChunks(t, d, "ex-own-g"); n != 1 {
		t.Fatalf("换代该是一次边界（先落老代那一块），实际落了 %d 行：%v", n, tap.lines)
	}
	if _, got := pumpChunkAt(t, d, "ex-own-g", 1); got != "第一代的半句" {
		t.Errorf("先落的必须是老代那块，实际 %q", got)
	}
	// 老代此时已经"迟到了"：它退场不许把新代那块一起带走（那正是撞 §6 规则 8 的形状）。
	hub.flushAllPending("fake", 1)
	if n := countPumpChunks(t, d, "ex-own-g"); n != 1 {
		t.Errorf("老代退场把新代攒的批次也带走了（落了 %d 行）⇒ 归属还是没立住：%v", n, tap.lines)
	}
	hub.flushAllPending("fake", 2)
	if n := countPumpChunks(t, d, "ex-own-g"); n != 2 {
		t.Fatalf("新代自己退场后第二块该落库，实际 %d 行 ⇒ 内容滞留了：%v", n, tap.lines)
	}
	if _, got := pumpChunkAt(t, d, "ex-own-g", 2); got != "第二代的半句" {
		t.Errorf("第二行该是新代那块，实际 %q", got)
	}
	if len(hub.pending) != 0 {
		t.Errorf("两代都退场后表该空，实际 %d 格", len(hub.pending))
	}
}
