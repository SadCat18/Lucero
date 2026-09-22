package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/SadCat18/lucero/core/internal/process"
)

// 这里是 §7.2 / REQ-T12 要的那半座桥：adapter 只要一根双工管道
// （`adapter.Launcher.Start(ctx, adapterKey) (io.ReadWriteCloser, error)`），
// 拉起、期限、重启与 backoff 全部留在 cmd/core 这一侧 —— 它自己的注释写明了这点，
// 而 §7.2 不许 probe 发明第二份健康/重试模型。
//
// 两件事按 T-133② 的要求先兜住：
//   - **ctx 必须带期限**：不带就直接拒。T-132 那两条死锁是"没人给期限 ⇒ 挂住 handler 线程"，
//     期限的正主是 adapter（它会补默认值），但那一半没落地之前，这道闸在我这边先合上。
//   - **`process_instances` 的改行今天仍没有通道**（§1.1 第 13 条 09-23 裁定：`process_instances` 不进
//     `narrowTables`，因为那条入口"不取版本、不写事件"；真正的洞是 **REQ-DB-2**，主控写明"不算 backend 欠账"），
//     所以这里 `Rec` 仍是 nil。第三十四~三十七批已有 `process.Recorder` 的实现（`dropaudit.go`：`RecordDrop` 写审计行；
//     生命周期那三格按 T-168 一律"出声但不写" —— 拿 `Append` 建行会丢掉已登记的 `process_started`，
//     正是 §1.1 第 13 条禁止的那个形状），但**不挂在这座桥上** —— 今天没有任何调用方会走到丢弃分支
//     （probe 未接 ⇒ 没有跑着的连接），挂上去就是给一个空转的东西记账。
//     probe 落地那天要做的两件事：给 `launcher` 补一把写通道（`auditedStore`）与一个 `now`，
//     然后在 `Supervisor{…}` 里加 `Rec: newDropRecorder(store, adapterKey, now, logf)`。
//     `Rec` 为 nil 也不是漏装，是它文档里明说允许的形态
//     （"a Supervisor with no recorder keeps supervising and simply reports through Sink"）。

// launcher 实现 adapter.Launcher。command 是"这个 key 该怎么起"的装配位：
// 真的装配给 fake-adapter 的路径，测试给测试自己的 helper 进程。
type launcher struct {
	command  func(adapterKey string) (*exec.Cmd, error)
	deadline time.Duration
	logf     func(format string, args ...any)
}

func newLauncher(command func(string) (*exec.Cmd, error), deadline time.Duration, logf func(string, ...any)) *launcher {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &launcher{command: command, deadline: deadline, logf: logf}
}

var errNoDeadline = errors.New("cmd/core: 拉起的 ctx 必须带期限")

// Start 起一次子进程并交回一根双工管道。一次调用 = 一次尝试，不在这里重试
// （§7.2 把 v0.1 钉在一次尝试上，重试会变成"自带 backoff 故事的自动恢复循环"）。
func (l *launcher) Start(ctx context.Context, adapterKey string) (io.ReadWriteCloser, error) {
	if l.command == nil {
		return nil, errors.New("cmd/core: 没装配子进程的起法（command 为空）")
	}
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		if l.deadline <= 0 {
			return nil, fmt.Errorf("%w: adapter_key=%q", errNoDeadline, adapterKey)
		}
		ctx, cancel = context.WithTimeout(ctx, l.deadline)
	}
	sup := &process.Supervisor{
		Kind:    process.KindAdapter,
		Launch:  func() (*exec.Cmd, error) { return l.command(adapterKey) },
		Restart: process.RestartNever,
		Sink: func(ev process.Event) {
			l.logf("adapter %s 进程事件: kind=%s pid=%d status=%s exit=%d", adapterKey, ev.Kind, ev.PID, ev.Status, ev.ExitCode)
		},
	}
	child, err := sup.Start(ctx)
	if err != nil {
		if cancel != nil {
			cancel() // 起都没起来，就更没有东西要继承这份期限
		}
		return nil, err
	}
	return &duplexPipe{sup: sup, child: child, ctx: ctx, cancel: cancel}, nil
}

// duplexPipe 把 Child 的 stdin/stdout 合成一根管道：交出去就只问一个问题——谁来关（§7.2 选这个形状的原因）。
// Stderr 不在这里合流：它是 Core 另存诊断流的地盘，混进协议管道就会把帧读坏。
type duplexPipe struct {
	sup    *process.Supervisor
	child  *process.Child
	ctx    context.Context
	cancel context.CancelFunc // 装配方补的那个默认期限，生命周期归这根管道
	closed bool
}

func (p *duplexPipe) Read(b []byte) (int, error) {
	// 期限到没到，读之前先看一眼：不这样做的话，一个从不出声的子进程会让读永久阻塞。
	// 真正的 watchdog（到期就 kill）要再起一个 goroutine，而 §9.2 规则 5 禁"裸 goroutine"，
	// 所以这里的保证是"下一次读会失败"，不是"到点立刻醒"。差别记在注释里，别当更强。
	if err := p.ctx.Err(); err != nil {
		return 0, fmt.Errorf("adapter pipe: %w", err)
	}
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	return p.child.Stdout.Read(b)
}

func (p *duplexPipe) Write(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, fmt.Errorf("adapter pipe: %w", err)
	}
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	return p.child.Stdin.Write(b)
}

// Close 交回 stdin（让"读到 EOF 就退"的子进程自然结束），再让 Supervisor 收口。
func (p *duplexPipe) Close() error {
	if p.closed {
		return nil
	}
	p.closed = true
	err := p.sup.Stop(p.child, 2*time.Second)
	p.child.CloseStreams()
	if p.cancel != nil {
		p.cancel() // 期限随管道一起还回去，不留在 context 树里
	}
	if err != nil && childAlreadyGone(err, p.child.PID) {
		return nil
	}
	return err
}

// childAlreadyGone 把"孩子本来就不在了"从关闭失败里分出去。这不是假想分支：
// 一个在握手里就崩掉的 adapter 走到这里时，cmd.Wait 早已释放句柄，实测
// Windows 上 Kill 拿到的是 EINVAL（不是 ErrProcessDone），而 Unix 上是 ErrProcessDone。
// 两种都说明 Close 的目标已经达成；真还活着却杀不掉才把错误交回调用方。
// 判"在不在"沿用 cmd/core/lock.go 的同一条 Windows 语义（FindProcess 对已退进程报错）。
func childAlreadyGone(err error, pid int) bool {
	if errors.Is(err, os.ErrProcessDone) {
		return true
	}
	proc, perr := os.FindProcess(pid)
	if perr != nil {
		return true
	}
	_ = proc.Release()
	return false
}
