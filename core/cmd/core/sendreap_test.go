package main

// 甲（T-370①）落点 (b) 的判据：`reapTurnFinished` 只按帧内容认收尾，且**绝不阻塞**。
// 这两条各自都有反例（判决记账本）：把 `default:` 摘掉 ⇒ "空通道那一格"挂死；
// 把 `IsFinished()` 那道筛去掉 ⇒ delta 会被当成收尾码收上来。

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/process"
)

func finishedFrame(code string) *adapter.StreamFrame {
	return &adapter.StreamFrame{Method: adapter.MethodTurnFinished, ResultCode: code}
}

func TestReapTurnFinishedPicksOnlyTheTerminalFrame(t *testing.T) {
	in := make(chan process.Inbound, 8)
	// 顺序有意混：delta、别人那条执行的收尾、进程死了的一声、我们的收尾、之后还有一条 delta。
	in <- process.Inbound{ExecutionID: "ex-A", Frame: &adapter.StreamFrame{Method: adapter.MethodStreamDelta, Text: "半句"}}
	in <- process.Inbound{ExecutionID: "ex-B", Frame: finishedFrame("SUCCEEDED")}
	in <- process.Inbound{ExecutionID: "ex-A", Err: errors.New("no reply")}
	in <- process.Inbound{ExecutionID: "ex-A", Frame: finishedFrame("FAILED")}
	in <- process.Inbound{ExecutionID: "ex-A", Frame: &adapter.StreamFrame{Method: adapter.MethodStreamItem}}
	close(in)

	got := reapTurnFinished(in, "ex-A")
	want := execution.TurnFinish{Seen: true, ResultCode: "FAILED"}
	if got != want {
		t.Errorf("该只收 ex-A 的那一帧收尾（FAILED），实际 %+v（收到别人的、或把 delta 当收尾都是这里红）", got)
	}
}

// TestReapTurnFinishedDoesNotBlockOnEmptyChannel 是边界二那一腿：缓冲里没有帧时必须**立刻**回零值。
// 写成"等一会儿看看"就是它自己违反了那条边界，所以这里给一个永不写入、也永不关闭的通道：
// 阻塞的实现会把这一格顶到测试超时，非阻塞的实现在几毫秒内回。
func TestReapTurnFinishedDoesNotBlockOnEmptyChannel(t *testing.T) {
	in := make(chan process.Inbound) // 无缓冲、没人写、不关闭
	done := make(chan execution.TurnFinish, 1)
	go func() { done <- reapTurnFinished(in, "ex-C") }()

	select {
	case got := <-done:
		if got.Seen || got.ResultCode != "" {
			t.Errorf("空缓冲该回零值，实际 %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reapTurnFinished 在无帧的通道上阻塞了 ⇒ 甲的语义（非阻塞扫一遍）被违反")
	}
}

// TestReapTurnFinishedAttributesByFrameContent 钉归因那一腿：帧上的执行号必须**精确等于**这次执行，
// 空号（归不到人）不许被认领 —— 那等于替别人猜一个身份（§6 规则 5 / CO-8 同一口径）。
func TestReapTurnFinishedAttributesByFrameContent(t *testing.T) {
	in := make(chan process.Inbound, 2)
	in <- process.Inbound{Frame: finishedFrame("SUCCEEDED")} // 空执行号：归不到人
	in <- process.Inbound{ExecutionID: "ex-D", Frame: finishedFrame("CANCELLED")}
	close(in)

	if got := reapTurnFinished(in, "ex-D"); got != (execution.TurnFinish{Seen: true, ResultCode: "CANCELLED"}) {
		t.Errorf("该收 ex-D 那条（CANCELLED），实际 %+v", got)
	}

	// 反面对照：只有空号那一帧时，不能替某个执行认下它。
	only := make(chan process.Inbound, 1)
	only <- process.Inbound{Frame: finishedFrame("SUCCEEDED")}
	close(only)
	if got := reapTurnFinished(only, "ex-E"); got.Seen {
		t.Errorf("执行号空着的收尾帧不许算到 ex-E 头上，实际 %+v", got)
	}
}

// TestNoteTurnFinishSeparatesTheThreeStates 是 T-381③ⅱ 要的那三分：
// **没收到帧**（今天真 producer 的 start_now 就是它）与**收到表外的码**与**收到表内的码**，
// 只有一格该喊。混了任何两格，恢复/崩溃那一格就会被测成假象（"没帧"被当成"楔住"就是主控点的那一种）。
func TestNoteTurnFinishSeparatesTheThreeStates(t *testing.T) {
	var tap []string
	logf := func(format string, args ...any) { tap = append(tap, fmt.Sprintf(format, args...)) }

	cases := []struct {
		name string
		fin  execution.TurnFinish
		want int
	}{
		{"根本没收到帧", execution.TurnFinish{}, 0},
		{"收到表外的码（\"OK\"）", execution.TurnFinish{Seen: true, ResultCode: "OK"}, 1},
		{"收到表内的码（SUCCEEDED）", execution.TurnFinish{Seen: true, ResultCode: "SUCCEEDED"}, 0},
		{"收到表内的码（CANCELLED）", execution.TurnFinish{Seen: true, ResultCode: "CANCELLED"}, 0},
		{"INTERRUPTED 不是 turn 级码（§6 第 6 条推论②）", execution.TurnFinish{Seen: true, ResultCode: "INTERRUPTED"}, 1},
	}
	for _, tc := range cases {
		tap = nil
		noteTurnFinish(logf, "ex-1", tc.fin)
		n := 0
		for _, l := range tap {
			if strings.Contains(l, "未裁") {
				n++
			}
		}
		if n != tc.want {
			t.Errorf("%s：该喊 %d 句，实际 %d（%v）", tc.name, tc.want, n, tap)
		}
		if tc.want == 1 && !strings.Contains(tap[0], tc.fin.ResultCode) {
			t.Errorf("喊出来必须带上那个码本身（否则事后查不到是哪一形）：%v", tap)
		}
	}

	// logf 为 nil 不许炸（装配上它是可选的，见 sendPipe.logf 的用法）
	tap = nil
	noteTurnFinish(nil, "ex-1", execution.TurnFinish{Seen: true, ResultCode: "OK"})
}
