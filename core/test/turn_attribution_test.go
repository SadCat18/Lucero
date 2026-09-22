package test

// 判据文本住在**契约② §4.2 判据表 T84**（T84-6 = 交叉校验两腿、T84-7 = 丢弃那一半的 Audit），
// 这里只留指针 + 实现理由（REQ-T10：别再造第二份文本）。
//
// 为什么不需要向 adapter 索要任何注入面（T84-6 这条本来被我列成 blocked，主控 13:3x 指出路径存在，实测对）：
// `Supervisor` 的字段全导出、`Launch` 就是一个 `func() (*exec.Cmd, error)`，
// 而 FakeAdapter 的 `evidence()` 照 `session/send` 参数里的 `execution_id` **原样回显**。
// 于是"不一致"只要让 `CallTurn` 绑 exec-B、参数里写 exec-EVIL 就自然产生 —— 用的是协议本来的形状。
// 全程不碰 `process` 的未导出成员，也不需要导出 `Connection`（REQ-T9 那条教训还在这儿）。
//
// 全文件不用父级 `t.Fatal` 卡前置条件（T-124 那族错）：需要子进程的两条各自在**子用例内** Skip，
// 所以 `-v` 里每条都有自己的 `--- SKIP` 行，一条都不会静默消失。

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/process"
)

// startSupervisedFake 起真 FakeAdapter 并握手。旗标关闭时整条 Skip —— 那是"要不要花一次 build"的开关，
// 不是"依赖没落地"：交付前必须开着跑（判据表命令里就写着它）。
func startSupervisedFake(t *testing.T) (*process.Supervisor, *process.Connection, context.Context) {
	return startSupervisedFakeOpt(t, nil)
}

// startSupervisedFakeOpt 多一个 Rec：T84-7 要看的正是"丢弃这个决定有没有到达接缝"。
// Rec 传 nil 就是原来那条路（§14 那条注释写着 nil-safe，今天的树上确实没人实现 Recorder）。
func startSupervisedFakeOpt(t *testing.T, rec process.Recorder) (*process.Supervisor, *process.Connection, context.Context) {
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
		Rec:    rec,
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

// dropRecorder 是 T84-7 的观察面：一个只记账的 process.Recorder 实现。
//
// 为什么我自己实现、而不是整条等 backend（账本 T-141 把 T84-7 拆成三段归属）：
// 接缝（`Recorder` 的第四个方法 `RecordDrop` + `connection.recordDrop`）adapter 22:5x 已经交出来了，
// 所以"丢弃这个决定到没到接缝、带什么字段"今天就能判；
// 而"到了接缝之后落成一行审计、且 `event_journal` 零新增"才是 backend 在写的第二段。
// **把两段混成一条 blocked，等于让已经能判的那半也一起躺着。**
type dropRecorder struct {
	mu    sync.Mutex
	drops []process.DroppedEvent
	life  int // 生命周期三方法的调用次数：只打印不判（判它就是拿实现的注释当契约）
}

func (r *dropRecorder) RecordStart(process.Event) { r.bump() }
func (r *dropRecorder) RecordExit(process.Event)  { r.bump() }
func (r *dropRecorder) RecordCrash(process.Event) { r.bump() }

func (r *dropRecorder) RecordDrop(ev process.DroppedEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drops = append(r.drops, ev)
}

func (r *dropRecorder) bump() {
	r.mu.Lock()
	r.life++
	r.mu.Unlock()
}

func (r *dropRecorder) snapshot() ([]process.DroppedEvent, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]process.DroppedEvent(nil), r.drops...), r.life
}

// 编译期钉子：接缝的形状（四个方法、void 返回、不递行句柄）一旦动，本文件先编不过，
// 而不是等到跑起来才发现我在观察一个已经不存在的接口。
var _ process.Recorder = (*dropRecorder)(nil)

// TestDroppedFrameReachesAuditSeam 执行契约② 判据表 T84-7 的两段：
// 第一段（现存）= 一次丢弃必须到接缝，带齐"帧里的 id / 票号绑的 id / 票号"三个字段；
// 第二段（SKIP blocked）= 到了接缝那一发必须落成一行审计、且 `event_journal` 零新增。
func TestDroppedFrameReachesAuditSeam(t *testing.T) {
	t.Run("T84-7a 丢弃到达接缝，且收下的帧不误报", func(t *testing.T) {
		rec := &dropRecorder{}
		_, conn, ctx := startSupervisedFakeOpt(t, rec)
		// 反例先做：一致的那一发不许留下任何"丢弃"记录（只判正例会漏掉"逢帧就记一行"这种假审计）。
		if _, err := conn.CallTurn(ctx, "exec-t847-ok", "session/send",
			sendParams("exec-t847-ok", "ik-t847-ok", "start_now")); err != nil {
			t.Fatalf("session/send（一致那一发）: %v", err)
		}
		if !waitUntil(3*time.Second, func() bool {
			return conn.Turn("exec-t847-ok").State() == adapter.TurnRunning
		}) {
			t.Fatalf("前提没成立：一致那一发都没推到 RUNNING，正例反例都无从谈起")
		}
		if drops, _ := rec.snapshot(); len(drops) != 0 {
			t.Errorf("收下的一发留下了 %d 条丢弃记录（%v）—— 接缝上多记一行，审计面就多一行假事实", len(drops), drops)
		}
		// 正例：票号绑 B、帧里写 EVIL ⇒ 两帧都该被丢，且每一发都要留下名字对不上的那两个 id。
		const bound, lying = "exec-t847-bound", "exec-t847-evil"
		reply, err := conn.CallTurn(ctx, bound, "session/send", sendParams(lying, "ik-t847-b", "start_now"))
		if err != nil {
			t.Fatalf("session/send（矛盾那一发）: %v", err)
		}
		ref := reply.ID
		var drops []process.DroppedEvent
		waitUntil(2*time.Second, func() bool {
			drops, _ = rec.snapshot()
			return len(drops) > 0
		})
		drops, life := rec.snapshot()
		if len(drops) == 0 {
			t.Fatalf("矛盾帧被丢掉了（tracker 停在 %q/%q），但接缝一条记录都没收到 —— §4.2 的处置是「丢弃 + Audit」，不是静默丢弃",
				bound, conn.Turn(bound).State())
		}
		var methods []string
		for _, d := range drops {
			methods = append(methods, d.Method)
			if d.FrameExecutionID != lying {
				t.Errorf("丢弃记录里帧内 id 是 %q，应当原样是 %q —— 审计要记「帧自己写了什么」，不是记纠正后的值", d.FrameExecutionID, lying)
			}
			if d.BoundExecutionID != bound {
				t.Errorf("丢弃记录里票号绑定的 id 是 %q，期望 %q —— 两个真相源必须同时在场，否则事后无法复盘是谁在说谎",
					d.BoundExecutionID, bound)
			}
			if d.TurnRef != ref {
				t.Errorf("丢弃记录里的 turn_ref 是 %q，本轮票号是 %q —— 没有票号这行审计找不到发起方", d.TurnRef, ref)
			}
			if d.Reason == "" {
				t.Errorf("丢弃记录没带理由（method=%s）—— 只记「丢了」等于什么都没记", d.Method)
			}
			if d.Kind != process.KindAdapter {
				t.Errorf("丢弃记录的 Kind=%v，期望 %v", d.Kind, process.KindAdapter)
			}
		}
		t.Logf("T84-7a：%d 条丢弃记录（method=%v），帧内 id=%q / 票号绑定 id=%q 两个都在；生命周期三方法被调 %d 次（只留痕不判）",
			len(drops), methods, lying, bound, life)
	})

	t.Run("T84-7b 一行审计且 event_journal 零新增", func(t *testing.T) {
		// 判据形状已经写在这里，只差被测的那一段落地 —— 前提不成立时显式 SKIP，不静默消失（T-124）。
		//
		// 要断的三件事（契约② T84-7 那一格 + §12"UNKNOWN 不许显示成确定值"在本层的落点）：
		// ① 接缝收到 N 条丢弃 ⇒ `audit_entries` 恰好新增 N 行（多一行是伪造事实，少一行是静默丢弃）；
		// ② `event_journal` 新增 0 行 —— 丢弃是审计事实，不是域事件，发出去就等于把矛盾下游化；
		// ③ 审计行里帧内 id 与票号绑定 id 逐字保留（脱敏之后仍是两个都在，REQ-A10/A11 那条顺序）。
		//
		// 阻塞点（点名，不含糊）：`Recorder` 的实现方在 `cmd/core/processpipe.go`（账本 T-141 第二段，
		// backend 23:0x 在途）。它没落地之前，②③ 两条在真链路上没有任何被测物；
		// 而我**不能**拿 `internal/process` 自己的 stubRecorder 去判这三条 —— 那是在判替身，
		// 判到的绿与产品行为无关（T-29：判决必须落在被测层自己的返回值或直接产物上）。
		t.Skipf("blocked（REQ-T12 第二段，归 backend）：等 cmd/core 里出现真正的 process.Recorder 实现，" +
			"本用例改成起真 Core + 造一次矛盾帧 + 数 audit_entries/event_journal 两张表的行数差")
	})
}
