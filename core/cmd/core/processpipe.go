package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
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
//   - **生命周期那三格从第四十九批起真写库**（REQ-DB-2 那条通道到位：`platform/db` 三入口 + `internal/process`
//     的 `LifecycleStore`）。这里只负责**装配**：把观察不到的事实（process_type / owner / executable / restart_policy）
//     与一笔 `*db.Tx` 递过去，成不成都喊（契约① §1.1 第 17 条"落点与角色"——语义在观察方，cmd/core 不写那张表的 SQL，
//     所以 `process_instances` 这个字面量在这里只出现在注释里，W13 那条判据不变）。
//     历史一句：09-23 那版这里是"四格里只有 `RecordDrop` 真写、生命周期三格只出声"（T-168 收回 T-162①，
//     因为拿 `Append` 建行会把已登记的 `process_started` 静默丢掉）；那条"出声但不写"的形态今天仍然保留，
//     但只在**没递库通道**的装配上（`lifecycleFor` 那道 nil 闸，`TestLauncherCarriesARecorder` 钉的就是它）。
//     一个 adapter 一个 recorder：`DroppedEvent` 里没有任何身份字段，身份只能由构造点带（T-155 删掉 `ProcessRole` 之后更是如此）。
//     `Rec` 为 nil 仍是它文档里明说允许的形态（"a Supervisor with no recorder keeps supervising and simply
//     reports through Sink"），但今天真装配不是那个形态。

// launcher 实现 adapter.Launcher。command 是"这个 key 该怎么起"的装配位：
// 真的装配给 fake-adapter 的路径，测试给测试自己的 helper 进程。
// store/now 是给 `RecordDrop` 那半格用的（T-181：probe 落地 ⇒ 桥上第一次真的有跑着的连接）。
type launcher struct {
	command  func(adapterKey string) (*exec.Cmd, error)
	deadline time.Duration
	logf     func(format string, args ...any)
	store    auditedStore
	now      func() int64
}

func newLauncher(command func(string) (*exec.Cmd, error), deadline time.Duration, logf func(string, ...any),
	store auditedStore, now func() int64) *launcher {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	return &launcher{command: command, deadline: deadline, logf: logf, store: store, now: now}
}

var errNoDeadline = errors.New("cmd/core: 拉起的 ctx 必须带期限")

// supervisorFor 造"这个 key 的那台子进程该怎么被看着"这件事的唯一一份配置。
// 它存在的原因是 discover 那条腿要的是 `Supervisor.Connect`（起子进程 + 开读帧的泵），
// 而 `Start` 只要 `Supervisor.Start`（起子进程 + 交回管道）—— 两个入口共用一份配置，
// 免得 `Restart`/`Rec`/`Sink` 三处各写一遍、迟早分叉（`Rec` 那半格尤其：漏一份就等于那条路上没人记账）。
//
// `restart`/`kind` 在这里只写一次并同时喂给两处（Supervisor 与生命周期写口）：
// 那一格记的 `restart_policy` 是"这台 Supervisor 的政策"，不是某一次尝试的结果 ⇒ 两边读的是同一个值。
func (l *launcher) supervisorFor(adapterKey string) *process.Supervisor {
	const (
		kind    = process.KindAdapter
		restart = process.RestartNever // §7.2 把 v0.1 钉在一次尝试上
	)
	return &process.Supervisor{
		Kind:    kind,
		Launch:  func() (*exec.Cmd, error) { return l.command(adapterKey) },
		Restart: restart,
		// 一个 adapter 一个 recorder：`DroppedEvent` 里没有任何身份字段，adapter_key 只能由构造点带。
		Rec: newDropRecorder(l.store, adapterKey, l.now, l.logf, l.lifecycleFor(adapterKey, kind, restart)),
		Sink: func(ev process.Event) {
			l.logf("adapter %s 进程事件: kind=%s pid=%d status=%s exit=%d", adapterKey, ev.Kind, ev.PID, ev.Status, ev.ExitCode)
		},
	}
}

// ownerTypeAdapterInstance 这个词表值的**唯一定义已归 `internal/process`**（CO-49：`process.OwnerTypeAdapterInstance`，
// 契约① §1.1 第 18 条 —— `owner_id` 是 `adapter_instances.id` 那个 UUIDv7，不是 adapter_key）。
// 装配层不再存第二份：改名或加词过去会**静默不一致**，现在编译器替我对账。
const ownerTypeAdapterInstance = process.OwnerTypeAdapterInstance

// lifecycleFor 装配这个 key 的那一份生命周期写口（§1.1 第 17 条"落点与角色"：
// "落一行 + 恰一条事件"的语义住在 `internal/process`，cmd/core 只把观察不到的事实递进去 + 递 `*db.Tx`）。
//
// 造不出来（词表/政策不合法、没 id 源）就返回 nil 并喊一声：那会让三格退回"只出声"，
// 那是**明说的降级**，不是静默 no-op —— 而监督本身不该因为记账件坏了就起不了子进程。
func (l *launcher) lifecycleFor(adapterKey string, kind process.Kind, restart process.RestartPolicy) *process.LifecycleStore {
	// 没有库通道就没有写口：这一格退回"只出声"，而不是装出一个一调就 nil panic 的 store。
	// （测试里就有 `auditedStore{}` 这种"只要桥、不要库"的装配，今天它照样只出声、照样绿。）
	if l.store.d == nil {
		l.logf("adapter %s 没递库通道进来：生命周期写口不装配，三格只出声", adapterKey)
		return nil
	}
	exe := ""
	// 起法问不出来 ⇒ executable 留空，不再另喊一句：同一个 key 的 `Start` 会把这条失败原样喊出去，
	// 这里再喊一遍就是同一件事两处声音（迟早分叉）。
	if cmd, err := l.command(adapterKey); err == nil && cmd != nil {
		exe = cmd.Path
	}
	owner, err := l.ownerID(adapterKey)
	if err != nil {
		l.logf("adapter %s 的 owner_id 反查失败，这一行按\"无归属\"落: %v", adapterKey, err)
	}
	// 查不到实例行就把 **整个 owner 一起留空**，不是只留空 owner_id：
	// `NewLifecycleStore` 拒的正是"半个归属"（`owner_type` 有、`owner_id` 空 ⇒ 比没有更坏，第 18 条同一句），
	// 而"这一行还没登记进 `adapter_instances`"是**真实现状** —— probe 是先起子进程、后写实例行的
	// （`internal/adapter` 的 `Registry.Probe` 先 `Launcher.Start`、后才写实例行），所以第一次尝试必然早于它自己的实例行。
	// 后果要写明白：第一次 probe 起的那台进程，行里没有归属；同一 key 之后的尝试才有。
	ownerType := ""
	if owner != "" {
		ownerType = ownerTypeAdapterInstance
	}
	store, err := process.NewLifecycleStore(process.ProcessRow{
		Kind: kind, OwnerType: ownerType, OwnerID: owner,
		Executable: exe, RestartPolicy: restart,
	}, func() (string, error) { return adapter.NewRequestID().String(), nil }, l.now, l.logf)
	if err != nil {
		l.logf("adapter %s 的生命周期写口没装配成，这一格退回只出声: %v", adapterKey, err)
		return nil
	}
	return store
}

// ownerID 反查 "这个 key 对应哪个 adapter_instances 行"（T-208③：复用 `discoverassembly.go` 开头那条反查的形状，
// 方向相反而形状相同 —— 一条 SELECT、查不到就原样抛 `sql.ErrNoRows`）。
// 还没 probe 过 ⇒ 库里本来就没有这一行 ⇒ 留空是**事实**（列可空），不是"拿空串冒充知道"。
// 这一条与 discover 那条是这个表名在 cmd/core 里的两处读，写仍一处都不许（W13-2）。
func (l *launcher) ownerID(adapterKey string) (string, error) {
	// §7.3 点名的第二个可达点（`l.store.d.Read`）：这里也返回错误，不留 nil 解引用的路径。
	// 与上面 `lifecycleFor` 那道闸不重复：闸管的是"没库就别装写口"，这句管的是"任何人调我都要拿到确定结局"。
	if l.store.d == nil {
		return "", errNoStore
	}
	var id string
	err := l.store.d.Read.QueryRow(`SELECT id FROM adapter_instances WHERE adapter_key = ?`, adapterKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

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
	sup := l.supervisorFor(adapterKey)
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
	// "孩子本来就不在了"不算关闭失败 —— 这不是假想分支：一个在握手里就崩掉的 adapter 走到这里时
	// 回收那一句已经释放了句柄，实测 Windows 上 Kill 拿到的是 EINVAL（`kill: invalid argument`）而不是
	// 那个"进程已完"的哨兵错（Unix 才是后者），两种都说明 Close 的目标已达成；真还活着却杀不掉才把错误交回去。
	// 判据本身不自留副本：T-368② 换成 `internal/process` 那份导出的唯一来源（两份函数体我今天自己
	// 按字节比过，IDENTICAL）。**别把这句读成"FindProcess 是通用判活"** —— `lock.go` 里"为什么不再是 pid 判活"
	// 那段讲的正是反面（进程对象在任一句柄关闭前一直存在）；这一格能用是因为**我们是父方、且回收已发生**。
	if err != nil && process.ChildAlreadyGone(err, p.child.PID) {
		return nil
	}
	return err
}
