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
// 边界三条（T-391⑤ 重申，与契约同文）：
//   - **不 sleep、不轮询等帧**：泵阻塞在 `Inbound()` 的 channel 接收上。阻塞接收不是"等够"，
//     它没有任何时间预算，帧不来就只是parking，`stop`/hub ctx 一关立刻退出。
//   - **不替 Provider 编终态**：泵只在**真有 `turn/finished` 帧**时写库。连接死了、帧没来，
//     这里一行都不写 —— 那一条腿归 `REQ-RC-2`（→`UNKNOWN`，`Occasion=Recovery`），主控明写不同批做。
//   - **同步路径不造 `UNKNOWN`**：本文件里没有任何 UNKNOWN 的写入。
//
// 关于 `Supervisor.Run`（W2-7 点名"生产码零调用点"）：这一格**没有**改成调用它，原因不是我忘了，
// 是今天这份导出面接不上 —— `Run` 自己 `s.Start()` 起孩子、把 `*Child` 留在循环里（`supervisor.go:387`），
// 而常驻连接要的是那台孩子的管道，只能由 `Supervisor.Connect` 给（`connection.go:100~117` 里
// `Connect` 也调 `Start`）。也就是说 `Run` 与 `Connect` 各起各的孩子，同一条 Supervisor 上两个都用
// 就会变成**两台子进程**。所以这里的形状是：一台 key 一份 `Supervisor`（`supervisorFor` 只调一次），
// 连接由它 `Connect`，死亡由在飞的那一次调用拿回来的 `ErrConnectionLost` 报出来 ⇒ 退役 + 下一次 send 重连。
// "自动重启 + 健康降级"那一格仍归 `Supervisor.Run`，也就是仍归 REQ-RC-2 那一批（契约① 第 25 条 ④-a
// 那句"这条的接线依赖 `Supervisor.Run` 那一格 ⇒ 排在本轮之后"），要它得先有"从被监督的孩子建连接"的导出面。

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

// turnHub 持有"每个 adapter key 一条常驻连接 + 一条常驻入站泵"这件事。
// 它住在 cmd/core：连接面与装配面都在这层（`internal/process` 是 adapter 的包，一个字都不改）。
type turnHub struct {
	launch *launcher
	store  auditedStore
	svc    *execution.Service
	logf   func(format string, args ...any)
	// ctx 是 Core 的生命周期，不是任何一次请求的：泵与连接都挂它。
	// 请求腿自己的期限在 `sendpipe.go`（`sendAttemptDeadline`），两件事不能混。
	ctx context.Context

	mu    sync.Mutex
	links map[string]*residentLink
}

// residentLink 一个 key 的常驻面：一份 Supervisor（`Rec`/生命周期写口只构造一次）+ 当下这条连接 + 它的泵。
// 换代只换 `conn`/`stop`/`done`/`gen`，`sup` 不换：一台子进程一份监督配置是这里的地基，
// 重建它会把 `restart_policy` 与那条 `process_instances` 归属重新发明一遍。
type residentLink struct {
	// mu 串行化"这条 key 的连接怎么换"：并发两发 send 不能起两台子进程。
	mu   sync.Mutex
	sup  *process.Supervisor
	conn *process.Connection
	// stop 由退役方关（请泵收工），done 由泵自己关（退役方等它）。两个 channel 分开放是必须的：
	// 只有一个的话，"我已经请它退"和"它真的退了"就分不清，关停顺序就只能靠运气。
	stop chan struct{}
	done chan struct{}
	gen  uint // 第几代连接，只进日志：退役与重连的先后从日志里要读得出来
}

func newTurnHub(ctx context.Context, launch *launcher, store auditedStore, svc *execution.Service,
	logf func(format string, args ...any)) *turnHub {
	if logf == nil {
		logf = func(format string, args ...any) {}
	}
	return &turnHub{launch: launch, store: store, svc: svc, logf: logf, ctx: ctx,
		links: map[string]*residentLink{}}
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
	conn, err := link.sup.Connect(ctx, coreProtocolVersion())
	if err != nil {
		return nil, err
	}
	if _, err := conn.Initialize(ctx); err != nil {
		// 握手没成 ⇒ 孩子已经起来了，必须当场还回去。留它就只能等下一次 `Close`，
		// 而这一条连接根本没人持有 ⇒ 那是一台没人认领的子进程（本批实测踩过，见台账）。
		h.closeConn(key, conn, "握手没成")
		return nil, err
	}
	link.conn = conn
	link.stop = make(chan struct{})
	link.done = make(chan struct{})
	link.gen++
	gen := link.gen
	// 一条连接一个泵 goroutine，挂在连接的生命周期上（退役方关 `stop`、Core 关停关 `h.ctx`）——
	// §9.2 规则 5 禁的是没有主人、没有退出条件的裸 goroutine，不是这条。
	go h.pump(key, conn, gen, link.stop, link.done)
	h.logf("turn 泵 %s #%d: 常驻连接已起（%s 之后帧由泵收，不随回执关闭）", key, gen, "turn/finished")
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

// handle 是那块的另一半："分辨"完才决定写不写。三种状态互不重叠（契约② §6 第 6 条）：
//   - 表里没有这个码（`"OK"`、`"INTERRUPTED"` 都是样本）⇒ **一行都不写**，只喊（推论①：收到帧 ≠ 成功）；
//   - 表里有 ⇒ 一笔短事务落终态 + §5.3 投影 + 链尾那半格（`FinishFromTurn` 里那些一个字没改）；
//   - 库里已经是这个终态（同帧重放、或两条腿都收到了同一份收尾）⇒ `FinishFromTurn` 自己认出并已 finished，
//     这里只把那一声响记下来，不补第二个版本的事件。
//
// 三种都不 sleep、都不轮询、都不替 Provider 编状态。
func (h *turnHub) handle(key string, gen uint, msg process.Inbound) {
	fin, ok := finishOf(msg)
	if !ok {
		return
	}
	// 先问域"这个码裁过没有"，再决定要不要占那一条串行写连接：表外的码注定写不成，
	// 不该为它开一笔事务再回滚（取值表只有一份，所以这里调的是导出的谓词而不是自己比字符串）。
	if !execution.MappedTurnResultCode(fin.resultCode) {
		h.logf("turn 泵 %s #%d: execution=%s 收到未裁的 result_code=%q ⇒ 不写终态（契约② §6 第 6 条：表里只有 SUCCEEDED/FAILED/CANCELLED）",
			key, gen, fin.executionID, fin.resultCode)
		return
	}
	var already bool
	err := h.store.Write(h.ctx, func(tx *db.Tx) error {
		_, a, err := h.svc.FinishFromTurn(h.ctx, tx, fin.executionID, fin.resultCode, h.command())
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

// connectionLost 问的是"这一次失败是不是对端没了"：是 ⇒ 这条连接不再可用，退役它。
// `LostError` 是 `internal/process` 导出的那一个哨兵（`ErrConnectionLost`），泵不自己判进程在不在 ——
// 判"孩子没在"的那一份唯一来源在 `process.ChildAlreadyGone`，而它管的是"收口时目标早不在"那一格，不是这里。
func connectionLost(err error) bool {
	return errors.Is(err, process.ErrConnectionLost)
}
