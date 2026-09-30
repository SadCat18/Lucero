package main

// probeassembly 是 §7.2 末尾那句"**把真 Supervisor 递进来的那一步归 backend 的 cmd/core**"的落点，
// 也是 T-181（09-23 02:08）要的①那一格。这里只放**构造与注入**：
//   - `adapter.Registry` 的条目（key → 起法）来自启动参数，Core 不猜有哪几个 Adapter；
//   - 写通道用 `adapter.NewSQLiteStore`（§7.2 裁定 1：写就在域包里，走 platform/db）；
//   - `Launcher` 就是这座桥上那个 `launcher`（`processpipe.go`），所以 probe 拉起来的子进程
//     从这一刻起真的带着 `Rec` —— ①与②必须同批，否则"挂了个没人会走到的记账"这句前提就一直成立。
//
// **一格刻意没做**：`Idempotency-Key` 在这里只判"有没有带"，不做重放。原因是幂等重放要求
// claim / 副作用 / seal 在同一个事务里（`POST /projects` 就是那个形状），而 `Registry.Probe`
// 自己开事务写库 —— 在它外面再持一个写事务就是 §1.1 那条嵌套死锁（`SetMaxOpenConns(1)`）。
// 要做真重放就得拆成三段事务，而"探针失败后那条 IN_PROGRESS 行怎么算"没人裁过 ⇒ 记 **C37** 等一句，
// 今天的行为明写在用例里：**同一个 key 打两次就是真探两次**（第二次可能带回新的 health），
// 而不是拿第一次的响应糊过去。

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
)

// probeAttemptDeadline 是这座桥自己给"一次拉起"上的期限（launcher 在 ctx 没带 deadline 时补它）。
// 域包那边也有一个同量级的默认值，但它是包内常量（`defaultProbeTimeout`）读不到，所以这里具名一份
// 并说明来源：**两个数字各管一头** —— 那个管"一次握手等多久"，这个管"一个子进程最多活多久"。
const probeAttemptDeadline = 10 * time.Second

// adapterSpec 一个已配置的 Adapter。`displayName` 是 `adapter_instances.display_name` 的 NOT NULL 列、
// 也是 yaml `AdapterInstance` 的必填项，而 §2 的 initialize 结果里**没有这一项**（`adapter.Entry` 的注释
// 就是在说这件事）⇒ 它只能来自装配输入。v0.1 的先例是 `principals` 那两处：display_name 就等于 id。
type adapterSpec struct {
	key         string
	displayName string
	command     string
}

// adapterFlag 让 `-adapter key=可执行路径` 可以重复给。一个 key 一个程序，v0.1 不开参数位
// （要参数就说明这条口在替某个具体产品做定制，那不是装配层的事）。
type adapterFlag struct{ specs []adapterSpec }

func (a *adapterFlag) String() string {
	if a == nil || len(a.specs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(a.specs))
	for _, s := range a.specs {
		parts = append(parts, s.key+"="+s.command)
	}
	return strings.Join(parts, ",")
}

func (a *adapterFlag) Set(v string) error {
	key, path, ok := strings.Cut(v, "=")
	key, path = strings.TrimSpace(key), strings.TrimSpace(path)
	if !ok || key == "" || path == "" {
		return fmt.Errorf("-adapter 要的是 key=可执行路径，收到 %q", v)
	}
	// 重复 key 在这里就拒：`adapter.NewRegistry` 也会拒，但那时报的是"registry 有两个条目"，
	// 读的人得先懂 Registry 才知道是自己参数写重了。
	for _, s := range a.specs {
		if s.key == key {
			return fmt.Errorf("adapter key %q 重复登记", key)
		}
	}
	a.specs = append(a.specs, adapterSpec{key: key, displayName: key, command: path})
	return nil
}

// probeService 是 httpapi.AdapterProber 的实现：调一次 Registry.Probe，把域包给的视图交出去。
// 这里**不补任何字段**（§7.2："handler 里不许补字段"，装配侧同理）。
type probeService struct{ reg *adapter.Registry }

func (p *probeService) Probe(ctx context.Context, adapterKey string) (*adapter.AdapterInstanceView, error) {
	res, err := p.reg.Probe(ctx, adapterKey)
	if err != nil {
		return nil, err
	}
	view := res.AdapterInstance()
	return &view, nil
}

// newAdapterServices 装配 probe、discover 与 send 三条腿 + 那条常驻入站泵。
// specs 为空 ⇒ 返回 (nil, nil, nil, nil, nil)：
// **一个 Adapter 都没配，就没有可探/可发现/可送的东西**，transport 那三格因此维持 501 而不是给每个 key
// 回一个"未知 adapter_key"的 400 —— 后者会把"这台机器上没配 Adapter"伪装成"你给错了名字"，是两种完全不同的处置。
// 三条腿共用同一个 `launcher`：`Supervisor` 的配置（Restart / Rec / Sink）只该有一处（见 `supervisorFor`）。
//
// `execSvc` 只给泵用：收到 `turn/finished` 之后那一笔短事务写的是 execution 终态 + §5.3 的 task 投影
// （REQ-TURN-1「Ⅰ」，见 `turnpump.go`）。send 腿自己不写库（REQ-S-1 的不变量照旧）。
func newAdapterServices(ctx context.Context, d *db.DB, store auditedStore, specs []adapterSpec,
	deadline time.Duration, now func() int64, logf func(string, ...any),
	sessionSvc *session.Service, execSvc *execution.Service) (*probeService, *discoverService, *sendPipe, *turnHub, error) {
	if len(specs) == 0 {
		return nil, nil, nil, nil, nil
	}
	pstore, err := adapter.NewSQLiteStore(d, now)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("装配 probe 的写通道: %w", err)
	}
	launch := newLauncher(commandFor(specs), deadline, logf, store, now)
	entries := make([]adapter.Entry, 0, len(specs))
	byKey := make(map[string]adapterSpec, len(specs))
	for _, s := range specs {
		entries = append(entries, adapter.Entry{AdapterKey: s.key, DisplayName: s.displayName, Launcher: launch})
		byKey[s.key] = s
	}
	reg, err := adapter.NewRegistry(
		adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}, entries, pstore, now)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("装配 adapter.Registry: %w", err)
	}
	discover := &discoverService{
		dbh: d, read: d.Read, launch: launch, byKey: byKey, svc: sessionSvc, logf: logf,
	}
	// 泵先装配：send 腿要的是一条**有主**的常驻连接，不是一个自己起自己关的连接。
	// ctx 是 Core 的生命周期（`boot` 那份），不是任何一次请求的 —— 见 `turnpump.go` 的 `turnHub.ctx`。
	// 泵多两件注入：健康写口的调用位（同一个 `pstore` —— 一台库两份 store 实例是既成事实，
	// 泵与 probe 用同一份配置的理由与 `supervisorFor` 同一条）+ 崩溃那一支要的过程事实（SQL 在本层）。
	hub := newTurnHub(ctx, launch, store, execSvc, pstore, processFactsOf(d.Read), logf)
	pipe := &sendPipe{read: d.Read, hub: hub, byKey: byKey, logf: logf}
	return &probeService{reg: reg}, discover, pipe, hub, nil
}

// commandFor 把"这个 key 该怎么起"做成 launcher 要的闭包。没配起法的 key 理论到不了这里
// （Registry 先一步按"未知 adapter_key"拒掉），留着是为了**不拿 panic 当分支**。
func commandFor(specs []adapterSpec) func(string) (*exec.Cmd, error) {
	byKey := make(map[string]adapterSpec, len(specs))
	for _, s := range specs {
		byKey[s.key] = s
	}
	return func(adapterKey string) (*exec.Cmd, error) {
		s, ok := byKey[adapterKey]
		if !ok {
			return nil, fmt.Errorf("cmd/core: 没给 %q 配起法（要 -adapter %s=<可执行路径>）", adapterKey, adapterKey)
		}
		return exec.Command(s.command), nil
	}
}
