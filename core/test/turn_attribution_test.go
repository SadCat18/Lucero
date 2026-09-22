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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		// 要断的三件事（契约② T84-7 那一格 + 契约③ §7.2 + §12"UNKNOWN 不许显示成确定值"在本层的落点）：
		// ① 接缝收到 N 条丢弃 ⇒ `audit_entries` 恰好新增 N 行（多一行是伪造事实，少一行是静默丢弃）；
		// ② `event_journal` 新增 0 行 —— 丢弃是审计事实，不是域事件，发出去就等于把矛盾下游化；
		// ③ 审计行里帧内 id 与票号绑定 id 逐字保留、且**能指出是哪个 Adapter**（脱敏之后仍是两个都在）。
		//
		// 观察面（T-163 裁定②：不给导出接缝，`Recorder` 留在 `package main`）：
		// **黑盒子进程 + 数两张表** —— 起真 Core、造一次矛盾帧、比 `audit_entries`/`event_journal` 的行数差。
		// 白盒 import 不到是对的，但那条路不需要任何人挪包。
		//
		// 摘 SKIP 的三段前提里只有①能被机器判，那就只判①；②③写成名字，不假装我看见了。
		root := repoRoot(t)
		if root == "" {
			t.Skip("仓库根没找到 ⇒ 前提无法判定。宁可 SKIP 也不装作判过（判据看不见东西时最坏的就是给个干净理由）")
		}
		mounted, evidence := dropRecorderMounted(root)
		if !mounted {
			t.Skipf("blocked（触发条件按 T-175 定死两条，两条同时成立才摘）：①%s；"+
				"②真 Adapter 连接还不存在（probe / `:sync` 未落地），所以『票号绑定 id 与帧内 id 不一致』这一发今天没有真实的产生方式。"+
				"T-175 原话：**拿一条走不到的代码路径判绿，比 SKIP 更坏**。"+
				"（这条门为什么读源码而不是数 grep：01:35 实测 `newDropRecorder(` 排除测试后原始命中 2 处，"+
				"其中 `dropaudit.go:38` 是函数定义本身、`processpipe.go:30` 是一句注释 ⇒ 真调用点 0。"+
				"『非 0 命中也要读原文』在这里被写成了代码。）"+
				"三段前提里曾列的『注入面』T-175 已裁**先不加**：为一条判据新开 `debug/misattribute` 是扩产品结构；"+
				"重开条件是①②都成立之后仍然造不出矛盾帧，那时再开 REQ 讨论（可能是走真 Adapter 的某条真实错误路径）。",
				evidence)
		}
		t.Skipf("前提①已满足（%s），但仍不测：还差②真 Adapter 连接（T-175 的第二条触发条件）。"+
			"**这条 SKIP 的理由变了，就是它该被改写的信号** —— 不要把它换成一个会红的 TODO，更不要凭空绿。", evidence)
	})
}

// dropRecorderMounted 只回答一个问题：`cmd/core` 的**生产码**里有没有把丢弃记账挂上桥。
// 判的是 `newDropRecorder(` 这个调用点，不判 dropaudit.go 存不存在 ——
// 文件在盘上不等于挂上了（09-23 00:54 实测就是"实现在、生产码调用点 0 处"）。
// 路径从仓库根拼，不写 `../cmd/core`：go test 的 cwd 是包目录，深度一改就静默扫到空集，
// 而"扫到空集"在这段代码里会伪装成一个很干净的前提成立。
func dropRecorderMounted(root string) (bool, string) {
	dir := filepath.Join(root, "core", "cmd", "core")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return false, fmt.Sprintf("扫 %s 失败：%v", dir, err)
	}
	if len(files) == 0 {
		return false, fmt.Sprintf("%s 一个 .go 都没扫到（目录挪了？这是判据看不见东西，不是前提成立）", dir)
	}
	var hits []string
	prod := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // 别人自己的单测里当然会调用它，那不算装配
		}
		prod++
		raw, err := os.ReadFile(f)
		if err != nil {
			return false, fmt.Sprintf("读 %s 失败：%v", f, err)
		}
		for _, line := range strings.Split(stripGoComments(string(raw)), "\n") {
			if !strings.Contains(line, "newDropRecorder(") {
				continue
			}
			trimmed := strings.TrimSpace(line)
			// 定义行不算调用点。这条守卫不是我新加的聪明：C8-11 那条腿上我专门为
			// `func writeError(...)` 写过同一条，今天在自己的门上漏了一遍 —— 同族错误第二次，记进账本。
			// 漏掉的后果很具体：门会说"前提①已满足"，而盘上什么都没接。
			if strings.HasPrefix(trimmed, "func ") {
				continue
			}
			hits = append(hits, trimmed)
		}
	}
	if len(hits) == 0 {
		return false, fmt.Sprintf("%s 下 %d 个生产 .go（共扫 %d 个 .go）里没有 `newDropRecorder(` 的调用点", dir, prod, len(files))
	}
	return true, fmt.Sprintf("找到调用点 %q（共 %d 处）", hits[0], len(hits))
}
