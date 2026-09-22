package test

// §4.2 两帧取证信号 `turn/claimed` / `turn/dispatched`（CO-29 回写，09-21 11:5x 落文）。
// 本文件是那四条语义的**独立转录**：判据来自契约② §4.2 与 §6 规则 5 的文本，
// 表里的期望值一条都不从 `stream.go` 的实现里抄（实现改了而契约没改 ⇒ 这里应当红）。
//
// 判据见 **契约② §4.2 判据表 T84**（T84-1~T84-7；09-22 由本文件的注释镜像进本文）。
// 状态列、以及 T84-7 的三段归属（adapter 给 `Recorder` 加"丢弃"事件、backend 在 `cmd/core` 注实现、
// test 落判据）都以契约那张为准，这里不抄第二份；本文件与 `turn_attribution_test.go` 只是执行者。
//
// 一句只属于"怎么写判据"的教训留在这儿：**契约改一次，判据就要跟着翻一次**。
// 12:2x 那版按 §4.2 原文"两处未定"只断了一边；主控 12:4x 裁定后 T84-4/5 翻成硬断言，
// T84-6 也从"缺注入面"翻成实测可跑（我 12:5x 列成 blocked 是错的，13:3x 指路才对）。
// 不变那半句：口径未定时不判绿（等于把"暂按代码"钉成契约），也不判红（等于替别人做决定）。

import (
	"encoding/json"
	"testing"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// t84NotDefinite 是"这一格只断不许是确定状态"的哨兵：契约没说被拒之后 tracker 里具体留什么，
// 我把实现今天挑的那个值写进期望，就是拿实现冒充契约。
const t84NotDefinite = "#not-definite"

// --- T84-1 ------------------------------------------------------------------

// TestClaimedCarriesNoTurnID §4.2 语义 1：claimed 故意不带 turn_id，"接下了"不等于"开始了"。
// 反向半边：dispatched 之后必须拿得到 turn_id —— 否则"空"只是因为根本没记。
func TestClaimedCarriesNoTurnID(t *testing.T) {
	var tr adapter.TurnTracker
	if err := tr.Claim(true, "exec-1", "prov-1"); err != nil {
		t.Fatalf("合法 turn/claimed 被拒: %v", err)
	}
	if got := tr.TurnID(); got != "" {
		t.Errorf("Claim 之后 TurnID()=%q，期望空 —— §4.2 语义 1 说 claimed 故意不带 turn_id", got)
	}
	if got := tr.State(); got != adapter.TurnClaimed {
		t.Errorf("Claim(accepted=true) 之后 State()=%q，期望 CLAIMED", got)
	}
	afterClaim := tr.TurnID()
	if err := tr.Dispatch("turn-1", "exec-1", "prov-1"); err != nil {
		t.Fatalf("合法 turn/dispatched 被拒: %v", err)
	}
	if got := tr.TurnID(); got != "turn-1" {
		t.Errorf("Dispatch 之后 TurnID()=%q，期望 turn-1 —— 开始证明只有 dispatched 给得出", got)
	}
	t.Logf("T84-1 两侧：Claim 之后 TurnID=%q → Dispatch 之后 TurnID=%q、State=%s",
		afterClaim, tr.TurnID(), tr.State())
}

// --- T84-2 ------------------------------------------------------------------

// TestRunningRequiresDispatch §4.2 语义 2：没有 turn/dispatched，Core 不许把状态显示成 RUNNING。
// 表里只有一行是正例（Claim→Dispatch 同一 execution_id）——它保证"永远拿不到 RUNNING"
// 这种塌成一团的实现骗不过去；其余每一行都是"差一点证据"的形状，都必须拿不到 RUNNING。
func TestRunningRequiresDispatch(t *testing.T) {
	cases := []struct {
		name      string
		step      func(*adapter.TurnTracker) error
		wantState string
		mustErr   bool // 该形状契约是否要求当面拒绝（不许静默收下）
	}{
		{
			name:      "只 claimed：接下了但没开始",
			step:      func(t *adapter.TurnTracker) error { return t.Claim(true, "e1", "p1") },
			wantState: adapter.TurnClaimed,
		},
		{
			name:      "claimed 且 accepted=false：这是一句回答，不是没回答，但更不是运行中",
			step:      func(t *adapter.TurnTracker) error { return t.Claim(false, "e1", "p1") },
			wantState: "",
		},
		{
			name: "claimed 缺 provider 名字（证据不全的一帧不许留下确定状态）",
			step: func(t *adapter.TurnTracker) error { return t.Claim(true, "e1", "") },
			// 契约只说"该拒"（§4.2 表：claimed 的 provider_execution_id 为空 ⇒ INVALID_PARAMS）与
			// "不许显示确定状态"（§12）。被拒之后 tracker 具体落成 "" 还是 UNKNOWN 是实现细节，
			// 所以这里只断"两个确定状态都不是"，不替实现挑一个。
			wantState: t84NotDefinite,
			mustErr:   true,
		},
		{
			name: "dispatched 先到（没有 claimed）",
			step: func(t *adapter.TurnTracker) error { return t.Dispatch("turn-1", "e1", "p1") },
			// CO-8：缺另一半证据就是"无证据"，而 §8 说无证据落 UNKNOWN、不落 FAILED。
			wantState: adapter.TurnUnknown,
			mustErr:   true,
		},
		{
			name: "claimed 之后 dispatched 指向另一个 execution",
			step: func(t *adapter.TurnTracker) error {
				if err := t.Claim(true, "e1", "p1"); err != nil {
					return err
				}
				return t.Dispatch("turn-1", "e2", "p1")
			},
			wantState: adapter.TurnUnknown,
			mustErr:   true,
		},
		{
			name: "dispatched 缺 turn_id（必填项缺一即无效）",
			step: func(t *adapter.TurnTracker) error {
				if err := t.Claim(true, "e1", "p1"); err != nil {
					return err
				}
				return t.Dispatch("", "e1", "p1")
			},
			wantState: adapter.TurnClaimed,
			mustErr:   true,
		},
		{
			name: "重复 dispatched：第二轮不许由 CLOSED/CLAIMED 之外的状态伪造出来",
			step: func(t *adapter.TurnTracker) error {
				if err := t.Claim(true, "e1", "p1"); err != nil {
					return err
				}
				if err := t.Dispatch("turn-1", "e1", "p1"); err != nil {
					return err
				}
				return t.Dispatch("turn-2", "e1", "p1")
			},
			wantState: adapter.TurnUnknown,
			mustErr:   true,
		},
		{
			name: "正例：claimed 然后 dispatched 同一个 execution",
			step: func(t *adapter.TurnTracker) error {
				if err := t.Claim(true, "e1", "p1"); err != nil {
					return err
				}
				return t.Dispatch("turn-1", "e1", "p1")
			},
			wantState: adapter.TurnRunning,
		},
	}
	positives := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var tr adapter.TurnTracker
			err := tc.step(&tr)
			got := tr.State()
			if tc.wantState == t84NotDefinite {
				if got == adapter.TurnClaimed || got == adapter.TurnRunning || got == adapter.TurnClosed {
					t.Errorf("证据不全的一帧留下了确定状态 %q —— §12 不许把没证据的东西显示成确定值", got)
				}
			} else if got != tc.wantState {
				t.Errorf("State()=%q，期望 %q", got, tc.wantState)
			}
			// 这一条才是 §4.2 语义 2 / §12 的本体：**任何**一步证据不完整的形状都不许留下 RUNNING。
			if tc.wantState != adapter.TurnRunning && got == adapter.TurnRunning {
				t.Errorf("§4.2 语义 2 违反：没有完整 dispatched 证据却把状态显示成 RUNNING（err=%v）", err)
			}
			// 错误面两侧都判：该拒的不许静默收下（调用方忽略返回值时也要有信号），
			// 不该拒的不许乱拒（把合法的 claimed 当错误，Core 就会把"接下了"当成事故）。
			if tc.mustErr && err == nil {
				t.Errorf("契约要求当面拒绝的形状返回了 nil（§4.2 表：这类帧要么 INVALID_PARAMS 要么 INTERNAL）")
			}
			if !tc.mustErr && err != nil {
				t.Errorf("契约没要求拒绝的形状被拒: %v", err)
			}
			if err != nil && codeOf(err) != adapter.ErrInvalidParams && codeOf(err) != adapter.ErrInternal {
				t.Errorf("错误码 %s，期望 INVALID_PARAMS（必填缺失）或 INTERNAL（证据配对不成立）", codeOf(err))
			}
			if tc.wantState == adapter.TurnRunning {
				positives++
			}
			t.Logf("State=%q err=%v（期望 %q）", got, err, tc.wantState)
		})
	}
	// 两侧都断的最后一环：正例必须**恰好一条**。多于一条说明表写松了，零条说明 RUNNING 根本不可达。
	if positives != 1 {
		t.Errorf("能合法到达 RUNNING 的形状有 %d 条，期望恰好 1 条", positives)
	}
}

// --- T84-3 ------------------------------------------------------------------

// TestClaimedKeepsProviderHandle §4.2 语义 3：provider_execution_id 必须挂在 claimed 上。
// 崩溃窗口是"claimed 了但还没 dispatched"，Recovery 要先拿这个 Handle 去问 Provider
// 才允许判 UNKNOWN；少了它就只能猜。所以**连 accepted=false 的拒绝路径也要留下两个 id**。
func TestClaimedKeepsProviderHandle(t *testing.T) {
	// 半边一：空的 provider_execution_id 必须被拒，而且不许留下"看起来有名字"的假证据。
	var a adapter.TurnTracker
	err := a.Claim(true, "e1", "")
	if err == nil {
		t.Error("turn/claimed 带空 provider_execution_id 却返回 nil —— §4.2 表：该字段为空即 INVALID_PARAMS")
	} else if codeOf(err) != adapter.ErrInvalidParams {
		t.Errorf("空 provider_execution_id 的错误码 = %s，期望 INVALID_PARAMS", codeOf(err))
	}
	if got := a.ProviderExecutionID(); got != "" {
		t.Errorf("被拒的一帧还是把 Handle 记成了 %q —— 空值被回读成\"有名字\"就是替 Recovery 造证据", got)
	}

	// 半边二：正常 claimed 之后两个 id 都要在（Recovery 用它问 Provider）。
	var b adapter.TurnTracker
	if err := b.Claim(true, "e1", "prov-42"); err != nil {
		t.Fatalf("合法 claimed 被拒: %v", err)
	}
	if b.ExecutionID() != "e1" || b.ProviderExecutionID() != "prov-42" {
		t.Errorf("claimed 之后 id 留存 = (%q,%q)，期望 (e1,prov-42)", b.ExecutionID(), b.ProviderExecutionID())
	}

	// 半边三：被拒的那一轮（accepted=false）同样要留下两个 id —— 它是"一句回答"，
	// 而 Recovery 分辨"跑过但没名字"与"根本没跑"靠的就是这两个值在不在。
	var c adapter.TurnTracker
	if err := c.Claim(false, "e1", "prov-77"); err != nil {
		t.Fatalf("accepted=false 的 claimed 应当是合法回答而不是错误: %v", err)
	}
	if c.ExecutionID() != "e1" || c.ProviderExecutionID() != "prov-77" {
		t.Errorf("accepted=false 把 id 丢了（(%q,%q)）⇒ 崩在这个窗口时 Recovery 只能猜，§4.2 语义 3 失效",
			c.ExecutionID(), c.ProviderExecutionID())
	}
	if c.State() == adapter.TurnRunning || c.State() == adapter.TurnClaimed {
		t.Errorf("被拒的一轮显示成 %q —— accepted=false 不许看起来像在跑或已接下", c.State())
	}
	t.Logf("T84-3 三侧：空 Handle 被拒且不留值(%v)；正常 claimed 留 (e1,prov-42)；accepted=false 仍留 (e1,prov-77) 且 State=%q",
		err, c.State())
}

// --- T84-4 ------------------------------------------------------------------

// TestTurnRefMandatoryOnEveryNotification §4.2 裁定 ①（主控 12:4x 定，12:54 巡检确认代码腿已落）：
// **所有** Adapter→Core 通知都带 `turn_ref`，取证两帧不再例外 —— 归属规则收成一条无例外的写法，
// 多带的那个 `execution_id` 是"可交叉验证的冗余"，不是第二真相源（交叉校验那一腿见 T84-6）。
// 两侧都断：缺 turn_ref ⇒ 必须被拒；带上 ⇒ 必须能成帧（否则上一条只是"NewStream 永远报错"）。
func TestTurnRefMandatoryOnEveryNotification(t *testing.T) {
	all := []string{
		adapter.MethodStreamDelta, adapter.MethodStreamItem, adapter.MethodStreamState,
		adapter.MethodTurnFinished, adapter.MethodTurnClaimed, adapter.MethodTurnDispatch,
	}
	for _, m := range all {
		if !adapter.CarriesTurnRef(m) {
			t.Errorf("%s 不在携带 turn_ref 的集合里 —— 裁定 ① 取消了取证两帧的例外", m)
		}
		_, err := adapter.NewStream("r1", &adapter.StreamFrame{Method: m, SequenceNo: 1,
			ExecutionID: "e1", Item: json.RawMessage(`{}`)})
		if err == nil {
			t.Errorf("%s 空 turn_ref 竟然能成帧 ⇒ 多 execution 并发时这一帧不知道该归给谁", m)
		} else if codeOf(err) != adapter.ErrInvalidParams {
			t.Errorf("%s 空 turn_ref 的错误码 = %s，期望 INVALID_PARAMS", m, codeOf(err))
		}
		if _, err := adapter.NewStream("r1", &adapter.StreamFrame{Method: m, SequenceNo: 1,
			TurnRef: "7", ExecutionID: "e1", Item: json.RawMessage(`{}`)}); err != nil {
			t.Errorf("%s 带 turn_ref 仍被拒: %v", m, err)
		}
	}
	// 一个都不许漏：覆盖数本身要钉住，否则将来 §4 加第七种通知时这条会悄悄少测一个。
	if got := len(all); got != 6 {
		t.Errorf("本用例覆盖的通知方法有 %d 个，期望 6（§4 方法表加行时这里要跟着加）", got)
	}
	t.Logf("T84-4 两侧：%d 种通知逐个判过：缺 turn_ref ⇒ INVALID_PARAMS、带上 ⇒ 成帧", len(all))
}

// --- T84-5：§4.2 裁定 ② ------------------------------------------------------

// TestDispatchedHandleOptionalButNeverContradicts §4.2 裁定 ②：`dispatched` 的
// `provider_execution_id` **允许空**（前提：同一轮的 `claimed` 已给过非空值），
// 但**给了非空就必须等于 claimed 那个值**。校验点落在 **Core 侧 tracker**，不是 Adapter 的
// 帧生成（主控 12:55 点名，与 T-29"变异打在待验那一层"同源）—— 所以三条形状全打在
// `TurnTracker.Dispatch` 的返回值与它自己维护的 `State()` / `ProviderExecutionID()` 上。
//
// 为什么不是"必填更严所以更安全"（原文理由留在这儿，免得下一个人反手改回去）：CO-13 承认有的
// Provider 到 dispatched 时给不出名字，此时强制非空 ⇒ 真 Adapter 发不出 `dispatched` ⇒
// **那一轮永远不显示 running**。这个失败模式比"缺一个字段"严重得多。
func TestDispatchedHandleOptionalButNeverContradicts(t *testing.T) {
	cases := []struct {
		name       string
		claim      string // claimed 给出的 provider_execution_id
		dispatch   string // dispatched 给出的 provider_execution_id
		wantState  string
		wantHandle string // tracker 事后还握着哪个名字（Recovery 就靠它问 Provider）
		wantErrNil bool
	}{
		{
			name:  "空名字：合法，且必须沿用 claimed 给过的那个",
			claim: "prov-1", dispatch: "",
			wantState: adapter.TurnRunning, wantHandle: "prov-1", wantErrNil: true,
		},
		{
			name:  "同名：合法",
			claim: "prov-1", dispatch: "prov-1",
			wantState: adapter.TurnRunning, wantHandle: "prov-1", wantErrNil: true,
		},
		{
			name:  "异名：同一轮出现两个名字，宁可 UNKNOWN 也不许挑一个信",
			claim: "prov-1", dispatch: "prov-2",
			wantState: adapter.TurnUnknown, wantErrNil: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var tr adapter.TurnTracker
			if err := tr.Claim(true, "e1", tc.claim); err != nil {
				t.Fatalf("前置 claimed(%q): %v", tc.claim, err)
			}
			err := tr.Dispatch("turn-1", "e1", tc.dispatch)
			if got := tr.State(); got != tc.wantState {
				t.Errorf("Dispatch(%q) 之后 State()=%q，期望 %q（err=%v）", tc.dispatch, got, tc.wantState, err)
			}
			if tc.wantErrNil && err != nil {
				t.Errorf("裁定 ② 允许的形状被拒: %v", err)
			}
			if !tc.wantErrNil {
				if err == nil {
					t.Error("冲突的名字被静默收下 ⇒ 同一轮有两个真相源，Recovery reconnect 时不知道拿哪个")
				} else if codeOf(err) != adapter.ErrInternal {
					t.Errorf("错误码 %s，期望 INTERNAL（证据配对不成立，不是参数格式问题）", codeOf(err))
				}
			}
			if tc.wantHandle != "" && tr.ProviderExecutionID() != tc.wantHandle {
				t.Errorf("tracker 手里的名字 = %q，期望 %q —— 空值那一腿若把它清空，等于把已经问得到的 run 变成查不到",
					tr.ProviderExecutionID(), tc.wantHandle)
			}
			t.Logf("claimed=%q、dispatched=%q ⇒ State=%q Handle=%q err=%v",
				tc.claim, tc.dispatch, tr.State(), tr.ProviderExecutionID(), err)
		})
	}
	// "允许空"的资格从哪来：claimed 没给过名字时根本进不到 CLAIMED，所以 dispatched 的空名字
	// 永远不会被当成"有名字"。这一整链在这里串一次，防的是两个半边各自演进后中间塌掉。
	var tr adapter.TurnTracker
	if err := tr.Claim(true, "e1", ""); err == nil {
		t.Error("claimed 空名字竟然合法 —— 那会让「dispatched 可空」变成「这一轮从来没被命名过」")
	}
	if err := tr.Dispatch("t1", "e1", ""); err == nil || tr.State() == adapter.TurnRunning {
		t.Errorf("没有 claimed 打底，dispatched 空名字仍拿到了 RUNNING（State=%q, err=%v）", tr.State(), err)
	}
	t.Logf("T84-5 前提腿：claimed(空) 被拒 ⇒ 停在 State=%q；无打底的 dispatched(空) 拿不到 RUNNING", tr.State())
}

// --- T84-6 / T84-7：裁定 ① 的交叉校验与 Audit 落点 ----------------------------

// 交叉校验那一腿（一致放行 / 不一致丢弃）已经落成硬断言，但**不在这个文件里**：
// 它要的是"票号绑定 ↔ 帧内 execution_id"这对关系，只有在真消费路径上才存在 ——
// 见 `turn_attribution_test.go` 的 `TestTurnRefCrossCheck`（`process.Supervisor` + 真 FakeAdapter）。
// 路径不需要向 adapter 索要任何注入面：`CallTurn` 绑一个 id、`session/send` 参数里写另一个 id，
// FakeAdapter 照参数回显，矛盾帧就自然产生（我 12:5x 把这条列成 blocked 是错的，主控 13:3x 指路后实测可走）。
//
// 仍然缺的只有 **T84-7 = Audit 那一半**：裁定 ① 写"丢弃 + Audit"，但 `internal/process` 的
// `Recorder` 只有 RecordStart/RecordExit/RecordCrash 三个生命周期方法，包内 grep `audit` 零命中 ⇒
// 这条今天没有可断的对象，已作为 REQ-T12 请主控指定由谁记（process 自己记，还是 Inbound 消费者记）。
// 那一条判据落地后要断的是：一次丢弃产生一行 audit 记录，且**不写 event_journal**（同 §5.4 的分工）。
