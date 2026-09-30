package main

// turnpump 是 REQ-TURN-1 裁「Ⅰ」的落点（契约① 第 25 条 ④-b，那条立项写的是
// "`turn/finished` 与 send 回执的先后时序在 v0.1 未定义"；09-28 21:4x 三条出路里裁了「Ⅰ」：
// **每 adapter 实例一条常驻连接 + 常驻入站泵，C42 随裁放开**）。
//
// 它解决的那一格很具体：生产者义务已经落地（`cmd/fake-adapter` 的 `settle` 把 `turn/finished`
// 写在**自己的回执之后**，T-392 ①），而上一形状里连接随 send 返回就 `Close` ⇒
// "帧到了没人收"从边角变成常态。契约否掉的另两条出路我照录在这里，免得下一个人再走一遍：
//   - Ⅱ（send 改异步回执）答非所问 —— 收尾帧只活在活着的管道上，连接关了什么回执都收不到；
//   - Ⅲ（只支持同步收尾的 Provider）把真接入挡在门外。
//
// 三件事决定它长这样：
//   - **归因不在这里做**：帧上的 `Inbound.ExecutionID` 已经是 `internal/process` 按 §6 规则 5
//     的 turn_ref→execution 绑定解出来的 Core 执行号（绑不上的那种帧根本到不了这条通道，
//     它在 `connection.go` 的 `attributeLocked` 那里就被丢弃 + 审计）。泵只认那个号，不猜身份。
//   - **写发生在泵这一侧、且只在一笔短事务里**：`store.Write` 与 handler 用的是同一张面
//     （REQ-S-1：写面只有 `platform/db` 那一条串行连接，网络往返不许关进事务 —— 泵的这一笔里没有网络）。
//   - **只收不写 + 三态分辨这一块是整搬进来的**（③ 给的字）：原 `sendreap.go` 的
//     `finishOf`/未裁码那一声搬到这里，`sendpipe` 里回执后那条收割腿删除。契约② §6 第 6 条的
//     推论①（收到帧 ≠ 成功）与推论②（`INTERRUPTED` 不是 turn 级码）在这里照旧生效。
//
// 边界四条（T-391⑤ 重申，加上 T-408 那一条；与契约同文）：
//   - **不 sleep、不轮询等帧**：泵阻塞在 `Inbound()` 的 channel 接收上。阻塞接收不是"等够"，
//     它没有任何时间预算，帧不来就只是parking，`stop`/hub ctx 一关立刻退出。
//   - **不替 Provider 编终态**：泵只在**真有 `turn/finished` 帧**时写终态。连接死了、帧没来，
//     终态一行都不写 —— 那一条腿走的是"不可判定"（`UNKNOWN`，`Occasion=Recovery`），不是折成 FAILED。
//   - **同步路径不造 `UNKNOWN`**：`UNKNOWN` 在这文件里只有一个来源 —— `watchDeath` 观察到对端进程
//     终止之后的 `markCrash`（REQ-RC-2 那一支，T-404 ①② 接的线）。收尾那一腿（`FinishFromTurn`）
//     只认帧里裁过的三个码，永远产不出 `UNKNOWN`。
//   - **这一层不推状态边**：`claimed` 那一笔只写把手那一列（`execution_claimed`，SET 里没有 `status`），
//     契约② §6 规则 1「没有 dispatched 就不许显示 running」不因它而松动。
//
// 关于 `Supervisor.Run`（W2-7 点名"生产码零调用点"）：这一格**没有**改成调用它，原因不是我忘了，
// 是今天这份导出面接不上 —— `Run` 自己 `s.Start()` 起孩子、把 `*Child` 留在循环里（`supervisor.go:387`），
// 而常驻连接要的是那台孩子的管道，只能由"从被监督的孩子建连接"那一步给。也就是说 `Run` 与连接面各起各的孩子，
// 同一条 Supervisor 上两个都用就会变成**两台子进程**。所以这里的形状是（T-404 ① 之后）：
// 一台 key 一份 `Supervisor`（`supervisorFor` 只调一次），孩子由 `Start` 起、连接由 `NewConnection` 接，
// 死亡由 `watchDeath` 那份 `Child.Wait` 观察到 —— 与 `Run` 用的是**同一个原语**，区别只在等完去改判而不是去重启。
// "自动重启"那一格仍归 `Run`（`ladderMayStart` 在有连接挂着时拒），重启阶梯与熔断的数值都不在这文件里。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
)

// pumpExitBound 是"请一条泵收工"之后最多等它退出的时间。**沿用同一个 2 秒**而不新造一档：
// 契约① §1.1 第 23 条那句裁定是"不新增第三种期限、不另开一个 Kill"，`Connection.Close` 收
// 同一条孩子用的就是这个数（`connection.go:334~337`）。等它不是为了正确性（泵的写落在哪一刻都不改
// 状态机的结论），是为了**关停之后不再有后台写**这件事可断言。超时就喊出来，不硬等。
const pumpExitBound = 2 * time.Second

// turnPumpPrincipal 是泵这一腿的事件主体。泵不是任何人的请求：它收到的是对端自己决定发出的帧，
// 所以与恢复扫描同一处置（`main.go:204` 那句 `PrincipalID: "system"`），不拿"上一次 send 的那个主体"冒充
// —— 那会把一条与用户无关的收尾算到某个用户头上。
const turnPumpPrincipal = "system"

// pumpCallDeadline 是泵**每一笔外呼**自带的期限（T-414 ①，09-30 21:2x 主控裁，量级 10 秒）。
// 守的是契约① 第 23 条⑧款那句「连接面上每一次调用必须自带期限」（原文在
// `specs/contracts-internal-v0.1.md:593`）—— 平台层既不检测也不自救，唯一被允许解开的是 `Close`。
//
// 为什么这一格非有不可：`h.ctx` 是 Core 的生命周期，它**不会到期**。写面只有
// `platform/db` 那一条串行连接（`db.go:48` 的 `write.SetMaxOpenConns(1)`），泵一旦排在别人身后，
// 它就不再有下一次 `select` ⇒ 入站帧堆在缓冲里没人收、收尾与 claimed 一起失踪，而日志一个字都不说。
// 那不是"慢"，是**泵被一次写吃掉**（第 306 条那处负载下的单次复现就是这个形状的一个断面）。
//
// 为什么是 10 秒：不是我从空里挑的数，是与同仓那两个同族常量同一个值、同一处出处 ——
// `sendpipe.go:49` 的 `sendAttemptDeadline`、`probeassembly.go:33` 的 `probeAttemptDeadline`
// 都是 `10 * time.Second`。三件事（一次 send、一次 probe、一笔泵上的外呼）等的是同一句"别把一次
// 调用变成无限等待"，所以它们该一起到期，不该出现第四档。`pumpExitBound` 那 2 秒是另一件事
// （"请一条泵收工之后最多等它退出"，契约① §1.1 第 23 条裁的就是它），这里不动它、也不新造第三档。
//
// 到期之后做什么：**明确失败并喊出来，一行都不写**（`store.Write` 带着期限 ctx ⇒ 事务起不来或起来就回滚），
// 然后**回到 select 继续收帧** —— 后半句才是 ② 那条判据断的东西。
const pumpCallDeadline = 10 * time.Second

// callCtx 给泵的一笔外呼套上那个期限。它是**每一次调用一个**、不是一次腿一个：
// `handleDeath` 里那圈 `markCrash` 若共用一份 ctx，第一台孩子的失败会把后面几条一起到期掉。
// ctx 挂的是 `h.ctx`：Core 真要关了他就散，不靠这个 10 秒当关停手段（那是另一件事）。
func (h *turnHub) callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(h.ctx, pumpCallDeadline)
}

// turnHub 持有"每个 adapter key 一条常驻连接 + 一条常驻入站泵"这件事。
// 它住在 cmd/core：连接面与装配面都在这层（`internal/process` 是 adapter 的包，一个字都不改）。
type turnHub struct {
	launch *launcher
	store  auditedStore
	svc    *execution.Service
	// health 是 §7.2 那一列的**唯一写口的调用位**（T-402 ① 把它开在 `internal/adapter`：
	// `(*adapter.SQLiteStore).DowngradeHealth`）。泵只调用、不写那条 SQL，也不自己拼第二份健康模型（§7.2）。
	// nil = 没配健康写口（测试里可以不带），那时死亡只喊、不动 health。
	health *adapter.SQLiteStore
	// facts 是崩溃那一支要的过程事实（按 adapter_instance_id 取）。SQL 住在本层，域不碰那张表（§0）。
	facts execution.ProcessFacts
	logf  func(format string, args ...any)
	// ctx 是 Core 的生命周期，不是任何一次请求的：泵与连接都挂它。
	// 请求腿自己的期限在 `sendpipe.go`（`sendAttemptDeadline`），两件事不能混。
	ctx context.Context

	mu    sync.Mutex
	links map[string]*residentLink

	// flights 是"这台连接上还没收到收尾的执行"，键是 execution_id。它的存在理由只有一个：
	// 崩溃那一支要一份**可归属的在飞名单**（协议 §8：崩溃只影响该 Adapter 上的 in-flight Execution）。
	// 名单不能从库里猜（"哪些 RUNNING"会把别的 Core 世代、别人的连接也算进来），只能是**我自己发过帧的那些**。
	// provider id 记在这里：契约② §6 规则 3 要它挂在 `turn/claimed` 那一刻，泵是全仓唯一看见那一帧的地方。
	flightsMu sync.Mutex
	flights   map[string]flight
}

// flight 一条在飞执行的观察：挂在哪个 key 上 + claimed 带回来的 Provider 把手（没有则空串，不猜）。
type flight struct {
	adapterKey string
	providerID string
}

// residentLink 一个 key 的常驻面：一份 Supervisor（`Rec`/生命周期写口只构造一次）+ 当下这条连接 + 它的泵。
// 换代只换 `conn`/`stop`/`done`/`gen`，`sup` 不换：一台子进程一份监督配置是这里的地基，
// 重建它会把 `restart_policy` 与那条 `process_instances` 归属重新发明一遍。
type residentLink struct {
	// mu 串行化"这条 key 的连接怎么换"：并发两发 send 不能起两台子进程。
	mu   sync.Mutex
	sup  *process.Supervisor
	conn *process.Connection
	// child 是这条连接骑的那个进程（T-398 ① 的 `NewConnection` 要求"本 Supervisor 自己起的"，
	// 死亡观察也要它：`Child.Wait` 是 `Run` 用的同一个原语）。
	child *process.Child
	// watchCancel 只停"死亡观察"这一件事。它必须存在并且只在**主动关停**时按：
	// `Child.Wait` 在整个等待期间持有 `waitMu`，而 `Connection.Close` 第一句之后要问
	// `Child.Reaped()`（同一把锁）—— 一个不被取消的长 Wait 与一次 Close 就是死锁，
	// 本批实测的栈：goroutine A 在 `Child.Wait` 里持锁等孩子死，goroutine B 在 `Close`→`Reaped` 上等那把锁，
	// 而孩子只有走完 B 的 Kill 才会死。cancel 是打破环的那只手，不是超时兜底。
	//
	// 它同时也是"我们主动收的"那件事的**唯一**记号：T-408 ③ 给了二选一（补一条 `werr == nil`
	// 而旗起作用的用例 / 承认冗余把旗删掉），我交不出那条用例 ⇒ 摘掉那面 `closing atomic.Bool`。
	// 理由按字说：`closeAll` 的顺序是"按 cancel → 退役"，旗为真的每一刻 cancel 都已按过，
	// 于是 `Wait` 带 err、`werr != nil` 那一格先挡住，旗永远走不到被读的那一步；
	// 更要紧的是它**能**挡错东西 —— 一次紧邻关停的真死亡（孩子刚死、cancel 紧随其后）会被读成
	// "不是崩溃"，那是一条该改判却没改的。无人能观察的状态不留，会吞真观察的状态更不留。
	// 判据有牙：把 `watchDeath` 里的 `werr != nil` 摘掉，`TestShutdownIsNotACrash` 红
	// （这些用例的 hub ctx 是 `context.Background()`，关停时 `h.ctx.Err()` 仍是 nil ⇒
	//   关停那一格今天**只**由 werr 守着；观察者现在自己喊"不作崩溃处理"，所以那枚变异不会
	//   再靠赛跑蒙过去 —— 第一版就是那样被蒙的，见台账第 306 条 N-g）。
	watchCancel context.CancelFunc
	// stop 由退役方关（请泵收工），done 由泵自己关（退役方等它）。两个 channel 分开放是必须的：
	// 只有一个的话，"我已经请它退"和"它真的退了"就分不清，关停顺序就只能靠运气。
	stop chan struct{}
	done chan struct{}
	gen  uint // 第几代连接，只进日志：退役与重连的先后从日志里要读得出来
}

func newTurnHub(ctx context.Context, launch *launcher, store auditedStore, svc *execution.Service,
	health *adapter.SQLiteStore, facts execution.ProcessFacts,
	logf func(format string, args ...any)) *turnHub {
	if logf == nil {
		logf = func(format string, args ...any) {}
	}
	return &turnHub{launch: launch, store: store, svc: svc, health: health, facts: facts,
		logf: logf, ctx: ctx, links: map[string]*residentLink{}, flights: map[string]flight{}}
}

// watch 登记一次在飞：send 腿在把 `session/send` 写出去**之前**调它。
// 早于出帧登记是因为反过来会漏：帧一写出去对端就可能已经开了轮次，而它对这发 send 的回执还没回来。
func (h *turnHub) watch(adapterKey, executionID string) {
	h.flightsMu.Lock()
	defer h.flightsMu.Unlock()
	if h.flights == nil {
		h.flights = map[string]flight{}
	}
	h.flights[executionID] = flight{adapterKey: adapterKey}
}

// noteClaim 是契约② §6 规则 3 在**观察面**的落点：`turn/claimed` 到的那一刻把 Provider 的把手记进在飞名单，
// 崩溃那一声（`markCrash`）才说得出"这一条在 Provider 上是哪一个"。它管的只是内存：
// 库那一侧由 `recordClaim` 走，两条腿各喊各的，谁也不替谁担保（落库失败时内存那份仍在，反之亦然）。
func (h *turnHub) noteClaim(executionID, providerID string) {
	h.flightsMu.Lock()
	defer h.flightsMu.Unlock()
	f, ok := h.flights[executionID]
	if !ok || providerID == "" || f.providerID == providerID {
		return
	}
	f.providerID = providerID
	h.flights[executionID] = f
	h.logf("turn 泵: execution=%s 的 claimed 带回 provider_execution_id=%s（在飞名单已记，落库那一腿见 recordClaim）",
		executionID, providerID)
}

// dropFlight 在"这一轮确认没开起来"时注销（对端明确拒绝 / 回执形状不合 / accepted=false）。
// 挂住的 send **不注销**：那一轮在对端可能还在跑，注销就等于把崩溃时该改判的执行悄悄从名单里抹掉。
func (h *turnHub) dropFlight(executionID string) {
	h.flightsMu.Lock()
	defer h.flightsMu.Unlock()
	delete(h.flights, executionID)
}

// takeFlights 一次性取走这台 key 上的在飞名单（取走即清空）：崩溃处理与重连之间不许有第二次认领。
func (h *turnHub) takeFlights(adapterKey string) map[string]string {
	h.flightsMu.Lock()
	defer h.flightsMu.Unlock()
	out := map[string]string{}
	for id, f := range h.flights {
		if f.adapterKey == adapterKey {
			out[id] = f.providerID
			delete(h.flights, id)
		}
	}
	return out
}

// connection 交出这个 key 的常驻连接，没有就起一条并开泵。
// ctx 是**调用方那一条请求腿的期限**：它只管"这一发愿意等多久"（起孩子 + 握手），
// 连接本身不挂它 —— 那正是「Ⅰ」与旧形状的区别：旧形状里这个 ctx 一到期，连接就随 `defer Close` 没了。
func (h *turnHub) connection(ctx context.Context, key string) (*process.Connection, error) {
	link := h.link(key)
	link.mu.Lock()
	defer link.mu.Unlock()
	if link.conn != nil {
		return link.conn, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("turn 泵 %s: 请求腿已经没有期限可用: %w", key, err)
	}
	// T-404 ① 的接线：起孩子与建连接分成两步，第二步走 `NewConnection`。
	// 为什么不用 `Connect`（它就是 Start + NewConnection）：泵要的是**那个孩子本身** ——
	// 死亡观察靠 `Child.Wait`（`Run` 用的同一个原语），而 `NewConnection` 的归属检查
	// （`attach`：不是本 Supervisor 起的孩子 ⇒ `ErrForeignChild`；一个孩子两根泵 ⇒ `ErrChildBusy`）
	// 只有在"我先起了它、再把它交给自己"这条路上才是真的被走到的代码，而不是文档里的承诺。
	child, err := link.sup.Start(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := link.sup.NewConnection(child, coreProtocolVersion())
	if err != nil {
		// 孩子已经起来、连接没建成 ⇒ 当场收，不留没人认领的进程（`Connection.Close` 还没上场）。
		h.stopChild(key, link.sup, child, "连接没接上")
		return nil, err
	}
	if _, err := conn.Initialize(ctx); err != nil {
		// 握手没成 ⇒ 孩子已经起来了，必须当场还回去。留它就只能等下一次 `Close`，
		// 而这一条连接根本没人持有 ⇒ 那是一台没人认领的子进程（本批实测踩过，见台账）。
		h.closeConn(key, conn, "握手没成")
		return nil, err
	}
	link.conn = conn
	link.child = child
	link.stop = make(chan struct{})
	link.done = make(chan struct{})
	link.gen++
	gen := link.gen
	// 一条连接一个泵 goroutine + 一条死亡观察，都挂在这条连接的生命周期上（退役方关 `stop`、
	// Core 关停关 `h.ctx`）—— §9.2 规则 5 禁的是没有主人、没有退出条件的裸 goroutine，不是这两条：
	// 两者的退出条件都是显式的，且 `Start` 自己就已经为这个孩子挂了一枚 per-child 的 Wait goroutine。
	go h.pump(key, conn, gen, link.stop, link.done)
	wctx, cancelWatch := context.WithCancel(h.ctx)
	link.watchCancel = cancelWatch
	go h.watchDeath(key, child, gen, wctx)
	h.logf("turn 泵 %s #%d: 常驻连接已起（pid=%d，%s 之后帧由泵收，不随回执关闭）", key, gen, child.PID, "turn/finished")
	return conn, nil
}

func (h *turnHub) link(key string) *residentLink {
	h.mu.Lock()
	defer h.mu.Unlock()
	link, ok := h.links[key]
	if !ok {
		link = &residentLink{sup: h.launch.supervisorFor(key)}
		h.links[key] = link
	}
	return link
}

// pump 是那条常驻入站泵：收 → 判 → 该写就写一笔短事务 → 再收。
// 它**不**在任何一次 send 之内启动或停止：连接活多久它活多久。
func (h *turnHub) pump(key string, conn *process.Connection, gen uint, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	in := conn.Inbound()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-stop:
			return
		case msg, ok := <-in:
			if !ok {
				// 今天 `internal/process` 不关这条通道（死亡走的是 `dead`，不是 close），
				// 这一格是"哪天它关了我也不能变成空转"：忙循环会把一颗核吃掉，而阻塞在这里才是设计。
				return
			}
			h.handle(key, gen, msg)
		}
	}
}

// watchDeath 是崩溃那一支的**触发点**（契约① 第 25 条 ④-a :713 那句"触发点 = Supervisor 观察到子进程死亡"）。
// 它等的就是 `Child.Wait` —— `Supervisor.Run` 用的同一个原语，区别只在这里等完是去改判而不是去重启：
// §7.2 把 v0.1 钉在"一次尝试"上（`RestartNever`），重启阶梯那一格仍归 `Run`（今天拒在有连接挂着，
// 见 `ladderMayStart`，`Close` 之后才允许）。
func (h *turnHub) watchDeath(key string, child *process.Child, gen uint, wctx context.Context) {
	_, werr := child.Wait(wctx)
	if werr != nil || h.ctx.Err() != nil {
		// 两种"不是对端死了"：观察被主动取消（`closeAll` 按下的那声 cancel，err 就是它 —— 这一格
		// 同时是关停与崩溃之间唯一的分界，见 residentLink 那段），或 Core 已经在退出路上。
		// 宁可什么都不写 —— 把一次正常关停写成一片 UNKNOWN 改判是更坏的错。
		//
		// 这一声**不是啰嗦，是把这条腿变成可观察的**：早先这里直接 return，判据只能"等连接退役之后
		// 数崩溃那一声有没有出现"—— 那是拿观察者的赛跑运气当证据（本批 N-g 那枚变异第一次跑就是被它
		// 蒙过去的，见台账第 306 条）。有了这一声，等待谓词就等于被断言的谓词：等到它就是观察者做完了决定。
		why := "主动关停：closeAll 先按了 cancel，Wait 带回的就是它"
		if h.ctx.Err() != nil {
			why = "Core 已经在退出路上"
		}
		h.logf("turn 泵 %s #%d: 孩子终止但不作崩溃处理（%s）⇒ 不改判、不降健康", key, gen, why)
		return
	}
	h.logf("turn 泵 %s #%d: 观察到对端进程终止 pid=%d ⇒ 走崩溃那一支（REQ-RC-2）", key, gen, child.PID)
	h.retire(key, "对端进程已终止")
	h.handleDeath(key, gen, child.PID)
}

// handleDeath 处理"这台上的在飞执行"与 §7.2 的健康列，两件各自独立、都必须出声：
//   - 在飞的每一条 ⇒ 一笔短事务调 `MarkCrashUnknown`（协议 §8：崩溃只影响该 Adapter 上的 in-flight，
//     一律转 `UNKNOWN`，不折成 FAILED）。`execution_unknown` 已在 §6 与平台目录里登记
//     （09-30 三处同改那一批），所以这一笔今天真落得下去；**落不下去时也必须喊**，
//     不许退化成"只改状态不发事件"（判据是配对的：喊成功就要库里真有，喊失败就要库里真没动）。
//   - 健康列 ⇒ `DowngradeHealth(key, UNAVAILABLE)`。写口在 `internal/adapter`（T-402 ①），泵只调用。
//     没有实例行时它返回 `db.ErrAggregateNotFound` 并明说"登记是 probe 的事" ⇒ 泵照它喊，不去补建行。
func (h *turnHub) handleDeath(key string, gen uint, pid int) {
	flights := h.takeFlights(key)
	marked := 0
	if len(flights) == 0 {
		h.logf("turn 泵 %s #%d: 对端终止 pid=%d，这台没有在飞的执行 ⇒ 不改判任何一条", key, gen, pid)
	} else if h.svc == nil || h.facts == nil {
		// 半装配的泵：不静默跳过，这一格要喊出来（崩溃观察已经发生，改判却没做）。
		h.logf("turn 泵 %s #%d: 有 %d 条在飞但泵没接改判面（svc/facts 未装配）⇒ 一行不写",
			key, gen, len(flights))
	} else {
		instanceID, err := h.launch.ownerID(key)
		if err != nil {
			h.logf("turn 泵 %s #%d: 反查 adapter_instances 失败，改判先停手: %v", key, gen, err)
		} else {
			factsCtx, cancelFacts := h.callCtx()
			facts, err := h.facts(factsCtx, instanceID)
			cancelFacts()
			if err != nil {
				h.logf("turn 泵 %s #%d: 取进程事实失败，改判先停手: %v", key, gen, err)
			} else if len(facts) == 0 {
				h.logf("turn 泵 %s #%d: 这台没有进程行可归属 ⇒ %d 条在飞一律不改判（CO-44②）",
					key, gen, len(flights))
			} else {
				for id, providerID := range flights {
					if h.markCrash(key, gen, id, providerID, facts) {
						marked++
					}
				}
			}
		}
	}
	// §7.2 那一列：走 adapter 的唯一写口。三种结局各喊一声，然后**最后**再落那声收尾。
	health := healthWord(h.health)
	if h.health != nil {
		healthCtx, cancelHealth := h.callCtx()
		changed, err := h.health.DowngradeHealth(healthCtx, key, adapter.HealthUnavailable)
		cancelHealth()
		switch {
		case err != nil:
			h.logf("turn 泵 %s #%d: 降健康没写成: %v", key, gen, err)
			health = "写失败"
		case changed:
			h.logf("turn 泵 %s #%d: 健康已降 %s（对端进程终止）", key, gen, adapter.HealthUnavailable)
			health = "已降"
		default:
			h.logf("turn 泵 %s #%d: 健康已是 %s ⇒ 不重复写（没有变化的变化不是一条事件）", key, gen, adapter.HealthUnavailable)
			health = "已在该档"
		}
	}
	// **收尾一声必须是本函数最后一条**：判据等它 = 等崩溃处理整个跑完。
	// （上一版把它放在健康那笔之前，用例等到的是一条"还没做完"的行，读健康列就读成旧值 ——
	//  同一个族的老规矩：等待谓词必须就是被断言的那个谓词。）
	h.logf("turn 泵 %s #%d: 崩溃处理收尾：在飞 %d 条、改判 %d 条、健康 %s", key, gen, len(flights), marked, health)
}

// markCrash 一条在飞执行的改判：一笔短事务，与 send/扫描那两笔同形（写面只有一条连接，REQ-S-1）。
func (h *turnHub) markCrash(key string, gen uint, executionID, providerID string, facts []execution.ProcessFact) bool {
	callCtx, cancel := h.callCtx()
	defer cancel()
	err := h.store.Write(callCtx, func(tx *db.Tx) error {
		_, e := h.svc.MarkCrashUnknown(callCtx, tx, executionID, facts)
		return e
	})
	if err != nil {
		h.logf("turn 泵 %s #%d: execution=%s 的崩溃改判没落成（provider 把手=%q）⇒ 这一格不动: %v",
			key, gen, executionID, providerID, err)
		return false
	}
	h.logf("turn 泵 %s #%d: execution=%s 判为 UNKNOWN（对端终止，provider 把手=%q）",
		key, gen, executionID, providerID)
	return true
}

// healthWord 给"没接写口"那一格用：参数取**具体类型**而不是 any —— `*adapter.SQLiteStore` 装进接口之后
// 即使是 nil 指针，`store == nil` 也是 false（平台层那条 typed-nil 老坑，`main.go` 的 adaptersField 就是为它存在的）。
func healthWord(store *adapter.SQLiteStore) string {
	if store == nil {
		return "写口未装配"
	}
	return "未调用"
}

// retire 丢掉这条 key 的当前连接（泵收工 + 孩子交回），并把"这里没有连接了"留下 ——
// 下一次 send 于是重连。这就是 ④ 那句"死亡即重连"今天能落的形状：重连发生在**下一次有人要它**，
// 不是一个自己转的重启循环（那个归 `Supervisor.Run`，见文件头）。
func (h *turnHub) retire(key, why string) {
	link := h.link(key)
	link.mu.Lock()
	defer link.mu.Unlock()
	if link.conn == nil {
		return // 已经退役过（并发里两发都看到同一个死连接时，只该有一声喊）
	}
	conn, stop, done, gen := link.conn, link.stop, link.done, link.gen
	link.conn, link.stop, link.done = nil, nil, nil
	h.retireLocked(key, gen, conn, stop, done, why)
}

func (h *turnHub) retireLocked(key string, gen uint, conn *process.Connection, stop chan struct{},
	done chan struct{}, why string) {
	close(stop)
	select {
	case <-done:
	case <-time.After(pumpExitBound):
		// 泵还压在一笔写上：不硬等（等下去会把关停变成第二个"挂住的 send"），但这一声必须喊出来。
		h.logf("turn 泵 %s #%d: %s 之后 %s 内没退出（那一笔写还在飞），照样关连接", key, gen, why, pumpExitBound)
	}
	h.closeConn(key, conn, why)
	h.logf("turn 泵 %s #%d: 连接退役（%s）⇒ 下一次 send 重连", key, gen, why)
}

// stopChild 收的是"连接还没建成"那种半成品：没有 `Connection` 就没有 `Close` 可走，
// 只能按 `Connection.Close` 的同一条顺序自己来一遍 —— 先还管道再杀进程是第 19 条判过的反例，
// 所以顺序是 Kill（`Supervisor.Stop`）在前、`CloseStreams` 在后。
func (h *turnHub) stopChild(key string, sup *process.Supervisor, child *process.Child, why string) {
	// Stop 是 Supervisor 唯一的 Kill 通道（泵不自己拿进程句柄）；错误里"目标早不在"那一格由
	// 导出的 ChildAlreadyGone 过滤，与 duplexPipe.Close 同一个判据（第 19 条那格）。
	err := sup.Stop(child, pumpExitBound)
	child.CloseStreams()
	if err != nil && !process.ChildAlreadyGone(err, child.PID) {
		h.logf("turn 泵 %s: 连接没建成、孩子也没收干净（%s）: %v", key, why, err)
		return
	}
	h.logf("turn 泵 %s: 连接没建成，孩子已当场收回（%s）", key, why)
}

func (h *turnHub) closeConn(key string, conn *process.Connection, why string) {
	if err := conn.Close(); err != nil {
		// 关不掉是"这台孩子我没收干净"的实情，不许静默：孤儿进程会把 §14 那一行留在 RUNNING。
		h.logf("turn 泵 %s: 关闭连接失败（%s）: %v", key, why, err)
	}
}

// closeAll 在 Core 关停时把每条常驻连接都退役。`boot` 失败那几条路径也走它（此时通常一条都没起）。
func (h *turnHub) closeAll() {
	h.mu.Lock()
	keys := make([]string, 0, len(h.links))
	for k := range h.links {
		keys = append(keys, k)
	}
	h.mu.Unlock()
	for _, k := range keys {
		l := h.link(k)
		// 顺序要紧：先放手观察（释放 `waitMu`），再走 `retire`→`Connection.Close`。
		// 反过来就是本批踩过的那次死锁 —— Close 在 `Reaped()` 上等一把只有孩子死了才会放的锁。
		// 这一声 cancel 同时就是"是我们主动收的"那条记录（residentLink 那段）。
		if l.watchCancel != nil {
			l.watchCancel()
		}
		h.retire(k, "Core 关停")
	}
}

// observedFinish 是"这一帧是一次收尾"那件事：执行号 + 帧里的原话。
// 刻意不叫 `TurnFinish`，也不带 `Seen`：泵是逐帧处理的，"没收到帧"在这里不是一个时刻 ——
// 那正是「Ⅰ」换掉的形状（旧形状里"回执时没帧"要当场决定不写终态，现在"还没到"就只是还没到）。
type observedFinish struct {
	executionID string
	resultCode  string
}

// finishOf 是整搬进来的那半块"**只收**不写"：只按帧内容认收尾。
//   - `msg.Err != nil` 是"进程死了 / 证据破了"那一声（`connection.go:657` 那一支），不是收尾；
//   - 不是 `turn/finished` 的帧跳过（`turn/claimed`、`turn/dispatched`、delta 都不关这一轮）；
//   - 执行号为空不算命中：拿一条归不到执行的收尾去推进某个执行，就是 §6 规则 5 与 CO-8 禁的"替别人猜一个身份"。
//     （到这里的帧都已由 `attributeLocked` 解过号，这一格守的是"哪天上面放宽了我也不能跟着猜"。）
func finishOf(msg process.Inbound) (observedFinish, bool) {
	if msg.Err != nil || msg.Frame == nil || !msg.Frame.IsFinished() || msg.ExecutionID == "" {
		return observedFinish{}, false
	}
	return observedFinish{executionID: msg.ExecutionID, resultCode: msg.Frame.ResultCode}, true
}

// recordClaim 是 `executions.provider_execution_id` 那一列在全仓唯一的**发射位**（REQ-RC-3 裁 (i)，
// 契约① :741；(ii)「db 开一道无事件的列写口子」被明拒）。三条硬时机里，语义那两条在域那一侧
// （`claim.go`：SET 里没有 `status`、异值不许覆盖），这里守的是**时机与出声**：
//   - 只有认出 `turn/claimed` 那一帧才走到这里，`turn/dispatched` 一次都不该打进来；
//   - 一笔短事务，泵的这一笔里没有网络往返、没有起孩子（REQ-S-1：写面只有那一条串行连接）；
//   - 四种结局各喊一声、都不许静默：没带把手 / 泵没接落库面 / 落成了 / 库里已是同一把手。
//     撞平台事件目录那道墙（§6 与 `internal/eventjournal` 的登记比这一批晚一步落）走的是"没落成"那一支，
//     墙的意义与 `recovery.go` 那一段相同：**明确失败 + 一行都不写**，不退化成"只改状态不发事件"。
func (h *turnHub) recordClaim(key string, gen uint, executionID, providerID string) {
	if providerID == "" {
		h.logf("turn 泵 %s #%d: execution=%s 的 claimed 帧没带 provider_execution_id ⇒ 这一列不写（空串不是把手，也不许替 Provider 编一个）",
			key, gen, executionID)
		return
	}
	if h.svc == nil {
		// 半装配的泵（只接了观察面）：内存那份照记，但这一声必须喊 —— "这一列没入库"不是一件没人知道的事。
		h.logf("turn 泵 %s #%d: execution=%s 的 claimed 带回把手却没接落库面（svc 未装配）⇒ %q 不入库",
			key, gen, executionID, providerID)
		return
	}
	cmd := h.claimCommand(providerID)
	callCtx, cancel := h.callCtx()
	defer cancel()
	var changed bool
	err := h.store.Write(callCtx, func(tx *db.Tx) error {
		c, e := h.svc.RecordClaim(callCtx, tx, executionID, cmd)
		changed = c
		return e
	})
	switch {
	case err != nil:
		h.logf("turn 泵 %s #%d: execution=%s 的 claimed 落库：没落成（provider_execution_id=%q）⇒ 库里这一格不动: %v",
			key, gen, executionID, providerID, err)
	case changed:
		h.logf("turn 泵 %s #%d: execution=%s 的 claimed 落库：已写入 provider_execution_id（execution_claimed，这一笔不推任何状态边）",
			key, gen, executionID)
	default:
		h.logf("turn 泵 %s #%d: execution=%s 的 claimed 落库：库里已是同一把手 ⇒ 不重复发事件",
			key, gen, executionID)
	}
}

// handle 是那块的另一半："分辨"完才决定写不写。三种状态互不重叠（契约② §6 第 6 条）：
//   - 表里没有这个码（`"OK"`、`"INTERRUPTED"` 都是样本）⇒ **一行都不写**，只喊（推论①：收到帧 ≠ 成功）；
//   - 表里有 ⇒ 一笔短事务落终态 + §5.3 投影 + 链尾那半格（`FinishFromTurn` 里那些一个字没改）；
//   - 库里已经是这个终态（同帧重放、或两条腿都收到了同一份收尾）⇒ `FinishFromTurn` 自己认出并已 finished，
//     这里只把那一声响记下来，不补第二个版本的事件。
//
// 三种都不 sleep、都不轮询、都不替 Provider 编状态。
func (h *turnHub) handle(key string, gen uint, msg process.Inbound) {
	// `turn/claimed` 是 Provider 把手进来的那一帧（契约② §6 规则 3：时机就在这里，早不早都不算）。
	// 两条腿各走各的：内存那份给崩溃时用，库里那份是 `executions.provider_execution_id` 的唯一写者。
	// **`turn/dispatched` 一律不进这一段**（REQ-RC-3 裁 (i) 的三条硬时机之一），判据在 `turnpump_claim_test.go`。
	if msg.Err == nil && msg.Frame != nil && msg.Frame.Method == adapter.MethodTurnClaimed && msg.ExecutionID != "" {
		h.noteClaim(msg.ExecutionID, msg.Frame.ProviderExecutionID)
		h.recordClaim(key, gen, msg.ExecutionID, msg.Frame.ProviderExecutionID)
	}
	fin, ok := finishOf(msg)
	if !ok {
		return
	}
	// 收尾到了 ⇒ 这一条不再"在飞"（不管落不落得成终态，崩溃那一支都不该再改判它第二次）。
	defer h.dropFlight(fin.executionID)
	// 先问域"这个码裁过没有"，再决定要不要占那一条串行写连接：表外的码注定写不成，
	// 不该为它开一笔事务再回滚（取值表只有一份，所以这里调的是导出的谓词而不是自己比字符串）。
	if !execution.MappedTurnResultCode(fin.resultCode) {
		h.logf("turn 泵 %s #%d: execution=%s 收到未裁的 result_code=%q ⇒ 不写终态（契约② §6 第 6 条：表里只有 SUCCEEDED/FAILED/CANCELLED）",
			key, gen, fin.executionID, fin.resultCode)
		return
	}
	var already bool
	callCtx, cancel := h.callCtx()
	defer cancel()
	err := h.store.Write(callCtx, func(tx *db.Tx) error {
		_, a, err := h.svc.FinishFromTurn(callCtx, tx, fin.executionID, fin.resultCode, h.command())
		already = a
		return err
	})
	switch {
	case err != nil:
		// 投影被拒 / 行不在 / 平台层报错：库这一格保持原样，喊出来。恢复那一腿是 REQ-RC-1/RC-2 的，
		// 不在这里替它下结论，更不许把一次落库失败折成"这一轮失败了"（那是替 Provider 编终态）。
		h.logf("turn 泵 %s #%d: execution=%s 收尾没落成（result_code=%s）⇒ 库里这一格不动: %v",
			key, gen, fin.executionID, fin.resultCode, err)
	case already:
		h.logf("turn 泵 %s #%d: execution=%s 已是 %s ⇒ 不重复发事件（同帧重放）",
			key, gen, fin.executionID, fin.resultCode)
	default:
		h.logf("turn 泵 %s #%d: execution=%s 迟到收尾已落终态 %s", key, gen, fin.executionID, fin.resultCode)
	}
}

// command 铸泵这一笔要的三个事件身份。三个都得单独铸：`event_journal.event_id` 是 UNIQUE 列，
// 而这一笔在两个聚合上是**三条**事件（execution 终态 + task 投影 + 链尾那条 task_waiting_acceptance），
// 域不自己生成身份（§3 的 UUIDv7 由调用方铸）。
func (h *turnHub) command() execution.Command {
	correlation := adapter.NewRequestID().String()
	return execution.Command{
		EventID:                      adapter.NewRequestID().String(),
		TaskEventID:                  adapter.NewRequestID().String(),
		TaskWaitingAcceptanceEventID: adapter.NewRequestID().String(),
		PrincipalID:                  turnPumpPrincipal,
		CorrelationID:                correlation,
		CausationID:                  correlation,
	}
}

// claimCommand 给 `recordClaim` 那一笔：它只发**一条**事件（execution 上的 `execution_claimed`），
// 所以只要一个身份，不像收尾那一笔要三个（三条事件）。主体仍是 `system`：把手是对端自己报的，
// 不是某个用户请求出来的（与 `turnPumpPrincipal` 同一条理由）。
func (h *turnHub) claimCommand(providerID string) execution.Command {
	correlation := adapter.NewRequestID().String()
	return execution.Command{
		EventID:             adapter.NewRequestID().String(),
		PrincipalID:         turnPumpPrincipal,
		CorrelationID:       correlation,
		CausationID:         correlation,
		ProviderExecutionID: providerID,
	}
}

// connectionLost 问的是"这一次失败是不是对端没了"：是 ⇒ 这条连接不再可用，退役它。
// `LostError` 是 `internal/process` 导出的那一个哨兵（`ErrConnectionLost`），泵不自己判进程在不在 ——
// 判"孩子没在"的那一份唯一来源在 `process.ChildAlreadyGone`，而它管的是"收口时目标早不在"那一格，不是这里。
func connectionLost(err error) bool {
	return errors.Is(err, process.ErrConnectionLost)
}
