package main

// 派发之前那三道题的判据（T-452）。三道题都在 `gateBeforeDispatch` 里，
// 但这一格里**只有两件事值得单独钉**，各自对应主控点名的一枚反例：
//   1. **挂着待审批就不许派发**（`ApprovalStates.BlockingState` 这一格的牙）——
//      库里写着 WAITING_APPROVAL 那种情形状态机自己就挡了，这里挡的是"审批行落了、边没推上"的半成窗口；
//      反例 B-2a 摘掉这一步 ⇒ "待审批的执行仍能起来"当场红。
//   2. **临时授权必须有读者**（`permission.Engine.CheckTemporaryGrant` 这一格的牙）——
//      一张覆盖着同一动作的活授权，是这一腿上唯一能把"挂着待审批"改判成放行的东西；
//      反例 B-2b 把它换成常量 true ⇒ 没有授权的那一格也放行，红。
// 用真 Engine、真 `approval.Service`（连授权都由它按 §3 规则 6 发出来），不用桩：
// 桩会给出盘上不存在的答案，而这一腿的全部价值就在于"8 小时那条记录今天真有人读"。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/approval"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/permission"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// seedPendingOnly 造那个**半成窗口**：落一张 PENDING，但库里那一行还停在 RUNNING。
// 走 audit 的 `Service.Request`（不是裸 INSERT —— `approvals` 表对本包是禁区），
// 边推不上这件事由"我们根本不调 raiseGate"来造成，而不是靠改库。
func seedPendingOnly(t *testing.T, asks *approval.Service, ex execution.Execution, action string) approval.Approval {
	t.Helper()
	a, err := asks.Request(context.Background(), approval.Draft{
		PrincipalID: approvalGatePrincipal, TaskID: ex.TaskID, ExecutionID: ex.ID,
		AdapterInstanceID: ex.AdapterInstanceID, Type: approvalGateType,
		RiskLevel: "HIGH", Action: action, Resource: "src/a.txt",
		ExpiresAt: gateNow + approvalWindowMillis,
	})
	if err != nil {
		t.Fatalf("落那张 PENDING: %v", err)
	}
	return a
}

func readRow(t *testing.T, d *db.DB, id string) execution.Execution {
	t.Helper()
	ex, err := execution.NewRepository(d.Read).Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

// TestDispatchIsRefusedWhileApprovalPending 是 BlockingState 那一格的正面：
// 挂着待审批（库里状态还是 RUNNING，正是半成窗口）时，`gateBeforeDispatch` 必须拒绝，
// 而且拒绝要落在**已登记的出口**上（包着 `execution.ErrIllegalTransition` ⇒ transport 现成的 409），
// 并且这一趟一行都不许多写：不推边、不再落第二张单、不改状态。
func TestDispatchIsRefusedWhileApprovalPending(t *testing.T) {
	hub, d, tap, restore := newGateHub(t, "ex-gate-block")
	defer restore()
	asks := wireGate(t, hub, d)
	ex := readRow(t, d, "ex-gate-block")
	a := seedPendingOnly(t, asks, ex, "filesystem.write")

	err := hub.gateBeforeDispatch(context.Background(), "ex-gate-block")
	if err == nil {
		t.Fatalf("挂着待审批（approval=%s）还把这一轮派出去 ⇒ 这就是把门关着当没门", a.ID)
	}
	if !errors.Is(err, execution.ErrIllegalTransition) {
		t.Errorf("拒绝该落在已登记的 409 出口（ErrIllegalTransition）上，实际是 %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "filesystem.write") {
		t.Errorf("拒绝里要点名被挡的那个动作，否则面板无从解释：%v", err)
	}
	if st := gateStatus(t, d, "ex-gate-block"); st != "RUNNING" {
		t.Errorf("被拒的这一趟把库里改成了 %q ⇒ 一道门不该顺手改状态", st)
	}
	if rows := pendingApprovals(t, asks); len(rows) != 1 {
		t.Errorf("挂着的那张单子被复制成了 %d 张：拒绝不是再落一张", len(rows))
	}
	_ = tap
}

// TestTemporaryGrantReopensTheGate 钉"8 小时临时允许"今天真的有人读：
// 第一张单子被批准并带了临时授权（走 audit 的 `Resolve`，授权由它自己发，我不写那张表），
// 第二张同动作的单子还挂着 ⇒ 派发不该再等一次人工，而是由那一格的授权放行，并**喊出是谁放行的**。
func TestTemporaryGrantReopensTheGate(t *testing.T) {
	hub, d, tap, restore := newGateHub(t, "ex-gate-grant")
	defer restore()
	asks := wireGate(t, hub, d)
	ex := readRow(t, d, "ex-gate-grant")

	first := seedPendingOnly(t, asks, ex, "command.exec")
	restore2 := swapGateClock(hub, gateNow+60_000) // 决议时刻：授权窗口从这一刻起算
	if err := asks.Resolve(context.Background(), first.ID, approval.Resolution{
		State: approval.APPROVED, By: approvalGatePrincipal, Note: "本轮允许并记住",
		Temporary: &approval.GrantRequest{
			ScopeType: permission.ScopeTask, ScopeID: ex.TaskID, Duration: int64(8 * time.Hour / time.Millisecond)},
	}); err != nil {
		restore2()
		t.Fatalf("批准并带临时授权: %v", err)
	}
	restore2()

	// 第二张同动作的单子还挂着（半成窗口重演一次），这一次必须有授权来挡下"再问一次人"。
	seedPendingOnly(t, asks, ex, "command.exec")

	if err := hub.gateBeforeDispatch(context.Background(), "ex-gate-grant"); err != nil {
		t.Fatalf("覆盖同一动作的临时授权本该放行这一轮，实际被拒: %v", err)
	}
	line := tap.lineWith("临时授权覆盖了它")
	if line == "" {
		t.Errorf("放行要喊出是谁放行的（否则这条授权与「没人读它」从外部看不出差别）")
	}
	if st := gateStatus(t, d, "ex-gate-grant"); st != "RUNNING" {
		t.Errorf("这一腿只该决定派不派发，不该改状态，实际 %q", st)
	}
}

// swapGateClock 临时把门里那只钟挪到一个给定时刻（授权窗口要从"决议那一刻"起算），
// 返回换回原样的那一句 —— 用例之间不许共享一只被拨过的钟。
func swapGateClock(hub *turnHub, at int64) func() {
	old := hub.gate.now
	hub.gate.now = func() int64 { return at }
	return func() { hub.gate.now = old }
}

// TestOrdinaryDispatchIsNotGated 是"该放行必须放行"那一腿：没挂着门、没有授权、
// 而 §3.1 把这一发判成低/中风险 ⇒ 三道题都答完、一行都不许多写、直接派发。
//
// 这一腿不是我"顺手加的对照组"，它是**唯一能照出我把两套词汇合错一个串的地方**：
// 我把派发动作写成协议方法名 `session/send`（线格式）而不是 §3.1 的 `session.send`（权限词表）时，
// `RiskLevel` 答"未登记"⇒ 失败关闭成"要人批"⇒ 装配好的 Core 会把每一发正常派发都落一张待审批。
// 只断"被拒时要拦住"的那几条判据全都还是绿的 —— 缺的就是这一条反向的腿。
func TestOrdinaryDispatchIsNotGated(t *testing.T) {
	hub, d, tap, restore := newGateHub(t, "ex-gate-open")
	defer restore()
	asks := wireGate(t, hub, d)

	if err := hub.gateBeforeDispatch(context.Background(), "ex-gate-open"); err != nil {
		t.Fatalf("一道干净的派发（没挂着审批、没授权、低/中风险）不该被拦：%v", err)
	}
	if rows := pendingApprovals(t, asks); len(rows) != 0 {
		t.Errorf("不该落任何待审批，实际 %d 张（落了就是「每发都要人批」那个形状）", len(rows))
	}
	if st := gateStatus(t, d, "ex-gate-open"); st != "RUNNING" {
		t.Errorf("这一腿只答派不派发，库里却被推成 %q", st)
	}
	if n := countJournalType(t, d, "ex-gate-open", execution.EventApprovalRequested); n != 0 {
		t.Errorf("不该有 %s 事件，实际 %d 条", execution.EventApprovalRequested, n)
	}
	if line := tap.lineWith("临时授权"); line != "" {
		t.Errorf("没有命中授权却喊了授权那一格，会把「没读策略表」伪装成「读了」：%s", line)
	}
}
