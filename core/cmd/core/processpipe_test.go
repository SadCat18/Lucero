package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// 编译期就钉住"这座桥确实长成 adapter 要的样子"：签名漂了这里就红，不用等谁来调。
var _ adapter.Launcher = (*launcher)(nil)

const pipeHelperEnv = "LUCERO_TEST_PIPE_HELPER"

// TestHelperPipeChild 不是测试，是被上面那些用例 fork 出来的子进程入口（Go 的标准做法）：
// echo 原样回吐 stdin；silent 一声不出、也不退 —— 那才是"没有期限就会挂住读"的真实形状；
// exit 一上来就自己退 —— 那是"孩子没了，Close 不该报关闭失败"的形状。
func TestHelperPipeChild(t *testing.T) {
	switch os.Getenv(pipeHelperEnv) {
	case "":
		t.Skip("只在子进程里跑")
	case "silent":
		// 挂在 stdin 上等死，而不是 select{}：后者会被 runtime 判成
		// "all goroutines are asleep - deadlock" 直接 fatal（实测退出码 2），
		// 孩子秒死 ⇒ cmd.Wait 释放句柄 ⇒ Close 的 Kill 只剩 EINVAL，
		// 而 requireProcessGone 白过 —— 这条孤儿检查就变成空转了。
		_, _ = io.Copy(io.Discard, os.Stdin)
		// stdin 被交回去也不走：Close 是先 Stop 再 CloseStreams，少了 Stop 的话
		// 光靠"EOF 让它自己退"就能骗过孤儿检查 ⇒ 这一格必须赖着不走。
		for {
			time.Sleep(time.Second)
		}
	case "exit":
		os.Exit(0)
	default:
		_, _ = io.Copy(os.Stdout, os.Stdin)
		os.Exit(0)
	}
}

func pipeCommand(mode string) func(string) (*exec.Cmd, error) {
	return func(string) (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperPipeChild$")
		cmd.Env = append(os.Environ(), pipeHelperEnv+"="+mode)
		return cmd, nil
	}
}

// TestLauncherCarriesARecorder 是 T-181② 的行为级证据：probe 一落地，这座桥上第一次真的会有跑着的连接，
// 所以 `Rec` 从今天起不是 nil。判据**不看字段是不是 nil**（那又是一条"只验声明文本"的假证），
// 看的是 `Supervisor` 一 `emit(EventStarted)`，生命周期那一格就以"出声、不写库"的形状落到日志里，
// 而且点得出名：哪个 adapter、哪个 pid。
//
// `store` 给的是零值 `auditedStore{}`（里面 `d` 是 nil）—— 这不是省事，是把它变成一根探针：
// 今天谁把这三格改成真写，第一下就是 nil 指针 panic，用例当场炸，不会静默放行。
func TestLauncherCarriesARecorder(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	l := newLauncher(pipeCommand("echo"), 5*time.Second, logf, auditedStore{}, nil)
	pipe, err := l.Start(context.Background(), "fake")
	if err != nil {
		t.Fatalf("Start 该成功： %v", err)
	}
	if _, err := pipe.Write([]byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(pipe, buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if err := pipe.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, `action=started adapter_key="fake"`) {
		t.Fatalf("挂了 recorder 之后该喊出 started 那一格（含身份），实际日志：\n%s", joined)
	}
	if strings.Contains(joined, `adapter_key="fake" pid=0`) {
		t.Errorf("喊话里的 pid 是 0，等于没说出是哪个进程：\n%s", joined)
	}
	// 三格里今天只有 `RecordStart` 会在这条路上被叫到：`Stop` 不发 EXITED（那是 Run() 的事），
	// 而 `RecordDrop` 要有人读帧才会响 —— 这条如实记在账上，不当成"三格都验过"。
	if n := strings.Count(joined, "action=started"); n != 1 {
		t.Errorf("生命周期出声该恰好一条（started），实际 %d 条：\n%s", n, joined)
	}
	// 第四十九批之后这个装配是"只要桥、不要库"（`auditedStore{}`，`d` 为空）⇒ 写口装不出来，
	// 三格必须退回**明说的**只出声：既不是静默 no-op，也不是把 nil 递下去 panic。
	if !strings.Contains(joined, "只出声") {
		t.Errorf("没递库通道时该喊出「写口没装配、只出声」，实际:\n%s", joined)
	}
}

// TestLauncherRefusesADeadlinelessContext 钉 T-133② 那句"从 handler 进去的 ctx 必须带期限，别等它"：
// 没期限且装配方也没给默认值 ⇒ 不起进程、直接明确报错。宁可不干，也不留一条会挂住 handler 线程的路。
func TestLauncherRefusesADeadlinelessContext(t *testing.T) {
	l := newLauncher(pipeCommand("echo"), 0, nil, auditedStore{}, nil)
	_, err := l.Start(context.Background(), "codex")
	if !errors.Is(err, errNoDeadline) {
		t.Fatalf("无期限的 ctx 该被拒，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Errorf("错误里要能看出是哪个 key 的装配出了问题: %v", err)
	}
}

// TestLauncherPipeRoundTrips 真子进程走一遍：写进去的字节要从同一根管读回来，
// Close 之后再读必须失败（不是永久阻塞），也不能留下没人收的孤儿子进程。
// 两种装配形状都跑：期限由调用方带，和调用方不带、由装配方补默认值 ——
// 后者曾经一装上就坏（默认期限那条腿用 defer cancel()，Start 一返回这根管道就已经"过期"了）。
func TestLauncherPipeRoundTrips(t *testing.T) {
	const bound = 20 * time.Second
	for _, tc := range []struct {
		name     string
		deadline time.Duration // launcher 的默认期限；0 = 只认调用方给的
	}{
		{"caller-deadline", 0},
		{"assembly-default", bound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.deadline == 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, bound)
				defer cancel()
			}
			var pid int
			l := newLauncher(pipeCommand("echo"), tc.deadline, capturePID(&pid), auditedStore{}, nil)
			pipe, err := l.Start(ctx, "codex")
			if err != nil {
				t.Fatal(err)
			}
			defer pipe.Close()

			want := "ping\n"
			if _, err := pipe.Write([]byte(want)); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, len(want))
			if _, err := io.ReadFull(pipe, buf); err != nil {
				t.Fatalf("管道没把字节回吐出来: %v", err)
			}
			if string(buf) != want {
				t.Errorf("回吐成 %q，期望 %q", buf, want)
			}
			if err := pipe.Close(); err != nil {
				t.Errorf("Close 报错: %v", err)
			}
			if _, err := pipe.Read(buf); err == nil {
				t.Error("Close 之后还能读到东西 ⇒ 关闭没真收口")
			}
			if err := pipe.Close(); err != nil { // 幂等：第二次 Close 不该报错
				t.Errorf("重复 Close 应无害，实际 %v", err)
			}
			requireProcessGone(t, pid)
		})
	}
}

// TestLauncherCloseToleratesAChildThatAlreadyDied 钉 childAlreadyGone 那格：
// 孩子自己先退了（adapter 握手期间崩掉就是这一形），Close 不该把"已经不在了"报成关闭失败。
func TestLauncherCloseToleratesAChildThatAlreadyDied(t *testing.T) {
	var pid int
	l := newLauncher(pipeCommand("exit"), 5*time.Second, capturePID(&pid), auditedStore{}, nil)
	pipe, err := l.Start(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	time.Sleep(300 * time.Millisecond) // 等 cmd.Wait 回收完：Kill 对回收过的孩子才会给 EINVAL

	if err := pipe.Close(); err != nil {
		t.Errorf("孩子已经自己退了，Close 该如实成功，实际 %v", err)
	}
	requireProcessGone(t, pid)
}

// TestLauncherDeadlineBoundsReads 用一个"一声不出也不退"的子进程验：装配方给了默认期限之后，
// 期限到了读必须**返回错误**，而不是把调用方钉死。注释里也写清这条保证的边界（下一次读才醒）。
func TestLauncherDeadlineBoundsReads(t *testing.T) {
	var pid int
	l := newLauncher(pipeCommand("silent"), 120*time.Millisecond, capturePID(&pid), auditedStore{}, nil)
	pipe, err := l.Start(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	time.Sleep(300 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		_, err := pipe.Read(buf)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("期限已过但读还成功返回 ⇒ 那条到期保证是假的")
		}
		// 认到 DeadlineExceeded 这一级就够：只判"错误非空"的话，一次提前的 cancel
		// （Start 里 defer cancel() 那个真 bug）也会让它非空，测的就不是期限了。
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("错误该是期限造成的，实际 %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("读把 goroutine 钉死了：期限没起作用（这正是 T-132 那一类的形状）")
	}
	// 这个子进程不会因 stdin 关闭而自行退出 ⇒ 只有 Close 真的走到 Supervisor.Stop 才会被收走。
	// echo 那个孩子测不出这一条（它自己会 EOF 退出），所以孤儿断言必须挂在 silent 上。
	if err := pipe.Close(); err != nil {
		t.Errorf("Close 报错: %v", err)
	}
	requireProcessGone(t, pid)
}

// stubProbeStore 只为把 NewRegistry 调到"能检查 Launcher"那一步：本批不接 probe，
// 所以这里不验它写了什么，只验装配缺件时是构造期就炸、还是等运行时才发现。
type stubProbeStore struct{}

func (stubProbeStore) RecordProbe(context.Context, adapter.ProbeResult) (string, error) {
	return "ai-1", nil
}
func (stubProbeStore) MarkUnverified(context.Context, string, string, string) (string, error) {
	return "ai-1", nil
}

// TestRegistryRejectsAnEntryWithoutALauncher 是这座桥与 Registry 的接缝：装配漏了 Launcher，
// 必须在构造期就炸，而不是等有人真去 probe 才发现 launcher 为空。
func TestRegistryRejectsAnEntryWithoutALauncher(t *testing.T) {
	_, err := adapter.NewRegistry(adapter.Version{Major: 1, Minor: 0},
		[]adapter.Entry{{AdapterKey: "codex", DisplayName: "Codex"}}, stubProbeStore{}, func() int64 { return 1 })
	if err == nil {
		t.Fatal("条目没带 Launcher，NewRegistry 该在构造期就拒")
	}
	if !strings.Contains(err.Error(), "launcher") {
		t.Errorf("错误该指出缺的是 launcher: %v", err)
	}
}

// capturePID 从 launcher 的事件日志里抓第一个 pid：Sink 天生带 pid，
// 不用为了测试再开一个取 pid 的口子（那会多一处只有测试用的 API）。
func capturePID(dst *int) func(string, ...any) {
	return func(_ string, args ...any) {
		for _, a := range args {
			if v, ok := a.(int); ok && v > 0 && *dst == 0 {
				*dst = v
			}
		}
	}
}

// requireProcessGone 验"Close 真把子进程收走了"。Windows 上 FindProcess 对已退进程会报错，
// 这就是判据；不靠"我以为 Stop 里那句 Kill 会执行"。
func requireProcessGone(t *testing.T, pid int) {
	t.Helper()
	if pid == 0 {
		t.Fatal("没从事件里抓到 pid ⇒ 这条孤儿检查会空转")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		p, err := os.FindProcess(pid)
		if err != nil {
			return // 判不到这个进程 = 它不在了
		}
		_ = p.Release()
		if time.Now().After(deadline) {
			t.Fatalf("Close 之后 pid %d 还活着 ⇒ 留了孤儿子进程", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestOwnerIDRefusesZeroStore 是 §7.3 那个"第二处可达点"的直接反例：`ownerID` 自己拿到零值 `*db.DB`
// 时必须返回错误（不是 panic、也不是静默返回空串）—— 装配闸之外再钉一次，因为这道闸管的是"任何人调我都有确定结局"。
func TestOwnerIDRefusesZeroStore(t *testing.T) {
	l := newLauncher(pipeCommand("echo"), 5*time.Second, nil, auditedStore{}, nil)
	id, err := l.ownerID("fake")
	if !errors.Is(err, errNoStore) {
		t.Fatalf("零值 store 的 ownerID 该返回 errNoStore，实际 err=%v id=%q", err, id)
	}
	if id != "" {
		t.Errorf("返回错误时不该再给一个像样的 id，实际 %q", id)
	}
}
