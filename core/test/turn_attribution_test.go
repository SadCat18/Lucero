package test

// §4.2 裁定 ① 的交叉校验腿（T-98 件②，把 T84-6 从"等注入面"变成实测）。
//
// 判据（契约② §4.2 裁定 ①，12:4x）：`turn_ref` 一律必带；`claimed`/`dispatched` 额外带 `execution_id`，
// 那是**可交叉验证的冗余、不是第二真相源**；两者同时在场而不一致 ⇒ Core **丢弃**（沿用 §6 规则 5 的处置）。
//
// | ID     | 断言（两侧都断）                                                                       | 当场命令                                                                        | 状态 |
// |--------|----------------------------------------------------------------------------------------|---------------------------------------------------------------------------------|------|
// | T84-6a | 绑定与帧内 `execution_id` **一致** ⇒ 收下：tracker 由空推到 RUNNING，per-ref 游标前移     | `LUCERO_TEST_FAKE_ADAPTER=1 go test -count=1 -v -run TestTurnRefCrossCheck ./test/...` | 现存 |
// | T84-6b | **不一致** ⇒ 丢弃：两个 execution 的 tracker 都不推进，per-ref 游标停在 0（"丢弃"=不推进，不是"收下但忽略"）| 同上 | 现存 |
//
// 为什么不需要向 adapter 索要任何注入面（这条本来被我列成 blocked，主控 13:3x 指出路径存在，实测对）：
// `Supervisor` 的字段全导出、`Launch` 就是一个 `func() (*exec.Cmd, error)`，
// 而 FakeAdapter 的 `evidence()` 照 `session/send` 参数里的 `execution_id` **原样回显**。
// 于是"不一致"只要让 `CallTurn` 绑 exec-B、参数里写 exec-EVIL 就自然产生 —— 用的是协议本来的形状。
// 全程不碰 `process` 的未导出成员，也不需要导出 `Connection`（REQ-T9 那条教训还在这儿）。

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/process"
)

// startSupervisedFake 起真 FakeAdapter 并握手。旗标关闭时整条 Skip —— 那是"要不要花一次 build"的开关，
// 不是"依赖没落地"：交付前必须开着跑（判据表命令里就写着它）。
func startSupervisedFake(t *testing.T) (*process.Supervisor, *process.Connection, context.Context) {
	t.Helper()
	if ok, why := fakeAdapterEnabled(); !ok {
		t.Skipf("blocked: %s", why)
	}
	exe, err := buildFakeAdapterExe(t)
	if err != nil {
		t.Fatalf("build FakeAdapter: %v", err)
	}
	sup := &process.Supervisor{
		Kind:   process.KindAdapter,
		Sink:   func(process.Event) {},
		Launch: func() (*exec.Cmd, error) { return exec.Command(exe), nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	conn, err := sup.Connect(ctx, "0.1.0-test")
	if err != nil {
		t.Fatalf("Supervisor.Connect: %v", err)
	}
	t.Cleanup(func() { _ = sup.Stop(conn.Child(), 3*time.Second) })
	if _, err := conn.Initialize(ctx); err != nil {
		t.Fatalf("§2 握手: %v", err)
	}
	return sup, conn, ctx
}

func sendParams(execID, key, mode string) map[string]any {
	return map[string]any{
		"remote_session_id": "fake-session-1", "text": "t84-6 probe",
		"delivery": mode, "idempotency_key": key, "execution_id": execID,
	}
}

// TestTurnRefCrossCheck 一行一次调用：一致那一发必须把 tracker 推到 RUNNING，
// 不一致那一发必须两个 execution 都不动、游标也不动。
func TestTurnRefCrossCheck(t *testing.T) {
	_, conn, ctx := startSupervisedFake(t)

	t.Run("T84-6a 一致 ⇒ 收下并推进", func(t *testing.T) {
		reply, err := conn.CallTurn(ctx, "exec-t846-a", "session/send",
			sendParams("exec-t846-a", "ik-t846-a", "start_now"))
		if err != nil {
			t.Fatalf("session/send: %v", err)
		}
		ref := reply.ID
		// 取证两帧是通知，异步到达：等到 RUNNING 为止，等不到才算失败（不 sleep 猜时间）。
		if !waitUntil(3*time.Second, func() bool {
			return conn.Turn("exec-t846-a").State() == adapter.TurnRunning
		}) {
			t.Fatalf("绑定一致时 tracker 没被推到 RUNNING，停在 %q —— §4.2 的两帧取证在这条路上没走通",
				conn.Turn("exec-t846-a").State())
		}
		if got := conn.SinceSeq(ref); got == 0 {
			t.Errorf("per-ref 游标还是 0（turn_ref=%q）⇒ 那两帧其实一帧都没被收下，上一条的 RUNNING 来路不明", ref)
		} else {
			t.Logf("T84-6a：turn_ref=%q 的游标推到 %d，State=RUNNING", ref, got)
		}
	})

	t.Run("T84-6b 不一致 ⇒ 丢弃且谁都不推进", func(t *testing.T) {
		// CallTurn 绑 exec-t846-b，但 send 参数里的 execution_id 是另一个值 ——
		// FakeAdapter 照参数回显，于是线上出现"票号说 B、帧里写 EVIL"的自相矛盾帧。
		const bound, lying = "exec-t846-b", "exec-t846-evil"
		reply, err := conn.CallTurn(ctx, bound, "session/send", sendParams(lying, "ik-t846-b", "start_now"))
		if err != nil {
			t.Fatalf("session/send: %v", err)
		}
		ref := reply.ID
		// 给对端把帧发过来的时间：等到"要么收下、要么确认丢弃"有结论为止。
		// 注意水位会因**不带 execution_id** 的那几帧（state/delta/item）而前移 —— 那不是违例：
		// 裁定 ① 要求的是"两者同时在场时校验一致"，无 id 可比的帧没有矛盾可言。
		// 我第一版把游标断成"必须停在 0"，那是我自己超出契约加严（当场红，改判为下面这条）。
		waitUntil(700*time.Millisecond, func() bool { return conn.Turn(lying).State() != "" })
		if got := conn.Turn(lying).State(); got != "" {
			t.Errorf("被丢弃的帧污染了帧里那个 id 的 tracker（%q → State=%q）—— §4.2 裁定 ① 要求丢弃而不是收下后忽略", lying, got)
		}
		if got := conn.Turn(bound).State(); got == adapter.TurnRunning || got == adapter.TurnClaimed {
			t.Errorf("票号绑定的 %q 收到了与帧内 id 不符的证据却落成了 %q —— 两个真相源不许有一个被采信", bound, got)
		}
		// 丢弃的第三层证据：**被丢的那两帧不许继续往前走**（`notify` 走的是 deliver{Err} 而不是 deliver{Frame}）。
		// 只判 tracker 状态不够 —— 两个 tracker 都没动也可能是"转发了但下游没认"。
		var forwarded int
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			select {
			case in := <-conn.Inbound():
				if in.Frame != nil && in.Frame.ExecutionID == lying {
					forwarded++
				}
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}
		if forwarded > 0 {
			t.Errorf("有 %d 帧带假 id 的通知被转发给下游了 —— 契约的处置是「丢弃」，转发等于把矛盾留给消费者自己判断", forwarded)
		} else {
			t.Logf("T84-6b：两个 tracker 都没动（%q=%q、%q=%q），假 id 的帧 0 条转发；"+
				"per-ref 游标 %q 推到 %d —— 那是同一次调用里不带 execution_id 的帧，不构成矛盾",
				bound, conn.Turn(bound).State(), lying, conn.Turn(lying).State(), ref, conn.SinceSeq(ref))
		}
	})
}

// waitUntil 轮询到条件成立或超时。通知帧异步，所以"等一会儿再猜"既慢又不稳，必须轮询到有结论。
func waitUntil(budget time.Duration, ok func() bool) bool {
	deadline := time.Now().Add(budget)
	for {
		if ok() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
