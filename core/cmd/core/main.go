// Command core 是 Lucero Core 的启动入口（契约③ §7 归 backend）。
//
// 启动顺序照内部契约 §2.3：Config → 单实例锁 → Vault → SQLite → Migration → Event Journal
// → Recovery → Adapter Supervisor → Scheduler → HTTP/WS → Health → Ready。
// 本轮只把已经存在的环节接上：Config / 锁 / SQLite / Migration / Event Journal（写库即写事件）
// / HTTP / Health / Ready；Vault、Recovery、Adapter Supervisor、Scheduler 四个组件还没人交付，
// 所以这里不摆 nil 占位（见 progress 的 Remaining），前端要的两项是 /health 与 /ws-ticket。
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	wsapi "github.com/SadCat18/lucero/core/api/websocket"
	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/approval"
	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/eventjournal"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/project"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// Version 由构建注入；没有就用这个，别让 health 报空。
var Version = "0.1.0-dev"

type config struct {
	DataDir    string
	Migrations string
	TakeOver   bool
	TicketTTL  time.Duration
	// Adapters 是 `-adapter key=可执行路径` 收进来的重复参数；空 = 不暴露 probe 这条口。
	Adapters adapterFlag
}

// Core 是跑起来的那一份：持有库、锁、口令、监听与运行态。
type Core struct {
	cfg      config
	lock     *lock
	db       *db.DB
	token    string
	endpoint string // host:port
	anchor   *audit.Anchor
	// checkpoints 是 ADR-018 的"每 1000 次成功 append 补一行锚点"计数器；anchor 是它写的地方。
	checkpoints *audit.CheckpointCounter
	// approvals 是 approval 的 SQLite store。T-64① 要求锚点计数器挂到它身上 ——
	// 它自己的 write() 已经是"事务内建 tx 绑定 Chain、提交成功后 ch.Checkpoint()"的形状，
	// 所以这里只是把计数器接上，不另造第二套交账时机。
	approvals *approval.SQLiteStore
	// reporter/tasks/store 是 §5.4 与"提交后才交账"这条时序的落点（见 rejections.go）。
	reporter *rejectionReporter
	tasks    *task.Service
	store    auditedStore
	// instances 是分派腿的只读门（T-263）：adapter_key → adapter_instances.id。
	// 与 probe 的写通道是两个 SQLiteStore 实例（同库不同视图），probe 那个只在 -adapter 配了时才建。
	instances *adapter.SQLiteStore
	// executions 是分派腿的写侧：:queue 在同一笔事务里调 Service.Create 建 Execution。
	executions *execution.Service
	// sessionSvc 是同一个 Service 的两次用途（`:sync` 只读 / `:manage` 写），构造一次好过两次：
	// 两处各自的时钟一旦分叉，`updated_at` 与 `state_confidence` 就会来自两个时刻。
	sessionSvc *session.Service
	// probe 是 §7.2 那条 probe 腿的装配产物（Registry + Launcher）。nil = 一个 `-adapter` 都没配，
	// 此时 transport 那格维持 501 —— 注入时必须走 adaptersField()，不能直接把 nil 塞进接口。
	probe *probeService
	// discover 是 sessions:discover 那条腿，与 probe 同一个 launcher、同一句"没配就是 nil"；
	// 注入走 discoverField()，同一条 typed-nil 陷阱不解释第二遍。
	discover *discoverService
	ln       net.Listener
	tickets  *httpapi.Tickets

	state atomic.Pointer[string]
}

func main() {
	var cfg config
	flag.StringVar(&cfg.DataDir, "data-dir", defaultDataDir(), "数据目录：放 lucero.db / core.lock / core.endpoint")
	flag.StringVar(&cfg.Migrations, "migrations", "migrations", "迁移 .sql 目录（v0.1 不做迁移内嵌，T-33 已撤回 REQ-T-2）")
	flag.BoolVar(&cfg.TakeOver, "take-over-lock", false, "已多余：单实例锁按 REQ-L-1 改判端口，不再需要人工接管死 pid")
	var ttl time.Duration
	flag.DurationVar(&ttl, "ws-ticket-ttl", 30*time.Second, "一次性 WS 短票的有效期（契约③ §1.2 默认 30s）")
	flag.Var(&cfg.Adapters, "adapter", "登记一个可探测的 Adapter：`key=可执行路径`，可重复给；一个都不给 ⇒ probe 维持 501")
	flag.Parse()
	cfg.TicketTTL = ttl

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	core, err := boot(ctx, cfg)
	if err != nil {
		log.Fatalf("core: %v", err)
	}
	defer core.close()

	if err := core.serve(ctx); err != nil {
		log.Fatalf("core: 退出异常: %v", err)
	}
}

func defaultDataDir() string {
	if d := os.Getenv("LUCERO_DATA_DIR"); d != "" {
		return d
	}
	return filepath.Join(".", "lucero-data")
}

// boot 建目录 → 绑回环端口 → 拿锁（锁的内容就是那个端口，REQ-L-1）→ 开库 → 迁移 → 生成口令 → 写 core.endpoint。
// 顺序里"先绑端口再拿锁"是判活的前提：锁文件必须一发布出来就带着能判活的端口；
// "先迁移再写事件"是硬要求：InitRedaction 必须在 SQLite 之后、事件可用之前（§2.3）。
func boot(ctx context.Context, cfg config) (*Core, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("建数据目录: %w", err)
	}
	if cfg.TakeOver {
		// 不静默吞掉一个"看起来在做点什么"的 flag：它现在什么都不做，就得当场说一句。
		log.Printf("core: -take-over-lock 已随 REQ-L-1 变成多余（判活看端口、不看 pid），本次忽略这个 flag")
	}
	// 端口由 Core 选定并写进 core.endpoint（契约③ §1.1）：只绑 127.0.0.1。
	// 提到拿锁之前，是因为"这个端口绑得上"就是"上一个持有者必已死"的那条网络事实（见 lock.go）。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("监听回环地址: %w", err)
	}
	l, err := acquireLock(cfg.DataDir, ln.Addr().(*net.TCPAddr).Port)
	if err != nil {
		ln.Close() // 端口判活刚用这只耳朵借过一次，拿不到锁就还回去，别留一个绑死了又没人听的端口
		return nil, err
	}
	core := &Core{cfg: cfg, lock: l, ln: ln, endpoint: ln.Addr().String()}
	booting := httpapi.StateBooting
	core.state.Store(&booting)

	d, err := db.Open(filepath.Join(cfg.DataDir, "lucero.db"))
	if err != nil {
		core.close()
		return nil, fmt.Errorf("开库: %w", err)
	}
	core.db = d
	// 规则的所有者是 audit，强制点在 db（内部契约 §2）。import 只出现在这里 ——
	// 接线属于 cmd/core（§2.3），业务包不 import audit 是为了不被别人的在途改动带红（C14）。
	rules := audit.Rules()
	if err := db.InitRedaction(rules.DenyKeys, rules.Patterns, rules.Marker); err != nil {
		core.close()
		return nil, fmt.Errorf("装脱敏规则: %w", err)
	}
	if err := d.Migrate(cfg.Migrations); err != nil {
		core.close()
		return nil, fmt.Errorf("迁移: %w", err)
	}
	// ADR-018 的链头锚点（T-64①）：路径与库同在 --data-dir 下，格式由 audit 包独占，
	// cmd/core 只负责"什么时候提交"。这里必须在迁移之后 —— 锚点读的是 audit_entries 的链头。
	core.anchor = &audit.Anchor{Path: filepath.Join(cfg.DataDir, "audit.head")}
	// T-64①：锚点计数器 + 挂到 approval store 上。clock 与 audit/approval 两边同一份（Unix 毫秒）。
	nowMillis := func() int64 { return time.Now().UnixMilli() }
	core.checkpoints = audit.NewCheckpointCounter(core.anchor, nowMillis)
	core.approvals = approval.NewSQLiteStore(d, nowMillis)
	core.approvals.UseCheckpoints(core.checkpoints)
	// §5.4 的两条记录在这里接线（拖了五轮的那颗雷）：不注入 reporter 的话，
	// 任何一次被拒投影都会直接失败整个请求（task.Service 对 nil reporter 是硬报错，不许静默吞）。
	core.reporter = &rejectionReporter{notes: notification.NewService(), now: nowMillis}
	core.tasks = task.NewService(core.reporter)
	core.store = auditedStore{d: d, checkpoints: core.checkpoints}
	// 分派腿两个依赖（T-263 的只读门 + execution 写侧）。instances 无条件建 ——
	// 即使一个 -adapter 都没配也要能回答"这个 key 不存在"（404）而不是 501；
	// probe 那条腿的写通道仍由 newAdapterServices 按 specs 是否为空决定建不建。
	core.instances, err = adapter.NewSQLiteStore(d, nowMillis)
	if err != nil {
		core.close()
		return nil, fmt.Errorf("装配 adapter 读通道: %w", err)
	}
	core.executions = execution.NewService()

	// session 的那个 Service 只构造一次（读侧 `:sync`、写侧 `:manage`，以及 discover 建行也共用），
	// 时钟用上面那份 nowMillis。它必须建在下面两个装配之前：discover 的"建行"就在这个 Service 里。
	core.sessionSvc = session.NewService(session.NewRepository(d.Read), nowMillis)
	// send 腿两格：task 投影（§5.3）与 session 落消息（message_recorded）。execution→task、
	// execution→session 依赖无环；反向注入在这里完成，NewService() 签名不变。
	core.executions.EnableSend(core.tasks, core.sessionSvc)

	// T-181① / T-198：probe、discover、send 三条腿一起装配（同一份 `launcher`、同一套"这个 key 怎么起"）。
	// 一个 `-adapter` 都没给 ⇒ 三个都是 nil ⇒ transport 那三格维持 501
	// （"这台机器没配 Adapter"与"你给错了 key"是两种处置，不许拿 400 伪装成前者，见 probeassembly.go）。
	probe, discover, sendPipe, err := newAdapterServices(d, core.store, cfg.Adapters.specs, probeAttemptDeadline, nowMillis,
		func(format string, args ...any) { log.Printf(format, args...) }, core.sessionSvc)
	if err != nil {
		core.close()
		return nil, err
	}
	core.probe = probe
	core.discover = discover
	// 连接半边：send 的 start_now/guide 走这里；nil 时那两态 501 ErrTurnNotWired，queue 照常。
	// 可以晚于 EnableSend —— 两格独立，NewService 签名不变。
	if sendPipe != nil {
		core.executions.EnableTurn(sendPipe)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		core.close()
		return nil, fmt.Errorf("生成口令: %w", err)
	}
	core.token = base64.RawURLEncoding.EncodeToString(buf)

	// 端口在拿锁之前就已绑好（见上面那段），这里只负责把它登记成 core.endpoint。
	if err := core.writeEndpointFile(); err != nil {
		core.close()
		return nil, err
	}
	core.tickets = httpapi.NewTickets(cfg.TicketTTL, nil)
	// principals 有两处外键指向它（event_journal.principal_id 与 tasks.requested_by），
	// 而契约③ §1.4 是单用户、写操作只认 local-user → 启动时把这唯一主体建出来。
	if err := core.ensurePrincipal("local-user", "USER", "本机用户"); err != nil {
		core.close()
		return nil, err
	}
	if err := core.ensurePrincipal("system", "SYSTEM", "Core"); err != nil {
		core.close()
		return nil, err
	}
	core.reportAuditChain(ctx)
	// REQ-T7：core_ready 进 journal（契约① §6）。必须在迁移之后 —— 事件表那时才在；
	// 必须在返回之前 —— serve() 一进就把 state 置 READY，health 报 READY 时这条事件得已经在库里。
	if err := core.appendCoreReady(ctx); err != nil {
		core.close()
		return nil, fmt.Errorf("写 core_ready 事件: %w", err)
	}
	return core, nil
}

// appendCoreReady 落 §6 的 core_ready：aggregate_type=core（versionless，走 AppendEventOnly）、
// principal=system、payload 带 version 与 migration_version（REQ-T7 点名的两键）。
// 每次成功 boot 一条 —— 崩溃恢复要能数出"起过几次、各带什么版本"。
func (c *Core) appendCoreReady(ctx context.Context) error {
	var mv int64
	if err := c.db.Read.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&mv); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("读迁移版本: %w", err)
	}
	return c.db.Write(ctx, func(tx *db.Tx) error {
		return tx.AppendEventOnly(db.AggCore, "core", db.EventDraft{
			EventID:     adapter.NewRequestID().String(),
			Type:        eventjournal.CoreReady,
			Aggregate:   db.AggCore,
			AggregateID: "core",
			PrincipalID: "system",
			Payload:     map[string]any{"version": Version, "migration_version": mv},
		})
	})
}

// ensurePrincipal 幂等地建一条 principals（非聚合表，按 §1.1 第 12 条用 Append，不占版本号）。
func (c *Core) ensurePrincipal(id, kind, display string) error {
	var n int
	if err := c.db.Read.QueryRow(`SELECT count(*) FROM principals WHERE id = ?`, id).Scan(&n); err != nil {
		return fmt.Errorf("查主体 %s: %w", id, err)
	}
	if n > 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	return c.db.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.Append("principals",
			[]string{"id", "principal_type", "display_name", "created_at", "updated_at"},
			[]any{id, kind, display, now, now})
		return err
	})
}

// adaptersField 把"没装配"翻译成接口的 nil。Go 里 `var p *probeService; i = p` 得到的是
// **非 nil 的接口**（类型在、值不在），而 transport 那格的闸门判的是 `c.Adapters == nil` ——
// 直接塞就会把一个"该回 501"的形态变成"进去就 panic"。
func (c *Core) adaptersField() httpapi.AdapterProber {
	if c.probe == nil {
		return nil
	}
	return c.probe
}

// discoverField 与 adaptersField 是同一条规矩的两个实例：nil 检查放在这里，不给 transport 留 typed nil。
func (c *Core) discoverField() httpapi.SessionDiscoverer {
	if c.discover == nil {
		return nil
	}
	return c.discover
}

// handler 把两个 transport 面拼起来：/ws/v1/events 走一次性短票（REQ-F-2，不接受 Authorization 头），
// 其余 /api/v1 走 Bearer。两条共用同一个回环来源判定，CORS 规则不出现第二套。
func (c *Core) handler() http.Handler {
	// `:read` 与 `:handle` 共用同一个 `*notification.Service`：它只带一个 `now` 闭包，造两份也不会坏，
	// 但会让人以为这两路口径可以各自漂移（CO-49 那条"同一份值只留一处定义"的规矩按同一个理由走）。
	notifSvc := notification.NewService()
	rest := httpapi.NewHandler(httpapi.Config{
		Token:   c.token,
		Version: Version,
		State:   func() string { return *c.state.Load() },
		MigrationVersion: func() int64 {
			var v int64
			if err := c.db.Read.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&v); err != nil {
				return 0
			}
			return v
		},
		Tickets: c.tickets,
		Tasks:   c.tasks, TaskList: task.NewRepository(c.db.Read), Store: c.store,
		// T-229 的读面那一格：`GET /api/v1/executions?task_id=` 是纯读，不注入 Store。
		// 它同时是"编排腿真建出了 execution"目前唯一能从进程外看到的窗口（写侧 `Service.Create` 今天零调用者）。
		Executions: execution.NewRepository(c.db.Read),
		// inbox 那一腿是纯读：不注入 Store，所以它不需要事务通道。
		Notifications: notification.NewRepository(c.db.Read),
		// `:read` 是这条竖线第一次真改 notifications 的一行状态（C31 已 GO）：写要事务通道，所以配 Store。
		NotifRead: notifSvc,
		// `:handle` 的边集是 09-25 契约① §1.1 第 21 条裁的（`UNREAD`/`READ`/`PENDING → HANDLED`、
		// `HANDLED` 重放是 200 no-op、`IGNORED`/`EXPIRED` 409 且一行都不写）；同一把 Service、同一个 Store。
		NotifHandle: notifSvc,
		// 建项目这条口只要一个 Service（canonicalize 与冲突判定都在 internal/project 里）；
		// 时钟与 §5.4/审计那两处共用同一个 nowMillis，别在业务包里各取一次时间。
		Projects: project.NewService(nil),
		// GET /projects 只要一条读通道：翻页规则住在 internal/project，这里不复制。
		ProjectRead: c.db.Read,
		// :sync 今天只有只读链这一格能力（C25）：Service 里一行都不写，所以只给它一把读的 Repository。
		Sessions: c.sessionSvc,
		// 面板的 listSessions 今天打的是 catch-all 的 501，这条把它接成真读路径。
		SessionRead: session.NewRepository(c.db.Read),
		// messages 读面（第六十二批格2）：生产者 RecordMessage 已在，api-contract :113~119
		// 的接线条件满足。与 SessionRead 同一个 Repository 值、两个字段（各管一条路径的 nil 闸）。
		SessionMessages: session.NewRepository(c.db.Read),
		// `:manage` 是这条竖线上第二次真改聚合（第一次是 notifications 的 `:read`）：
		// 同一个 Service 复给读侧与写侧，时钟也复用那份共享的 nowMillis（别在业务包里各取一次时间）。
		SessionManage: c.sessionSvc,
		// probe 这条口（§7.2 的 backend 半边）。走 c.adaptersField() 而不是直接塞：
		// 把 typed nil 装进接口会得到一个"非 nil 但一调就 panic"的值，501 那格就白留了。
		Adapters: c.adaptersField(),
		// discover 与 probe 同一条腿的两个入口，"没配 Adapter"时同样是 nil ⇒ 501（不是 404、不是 405）。
		SessionDiscover: c.discoverField(),
		// 分派腿（T-263 + C43 后半格）：Instances 无条件注入（没配 adapter 也要能回 404 而不是 501），
		// ExecutionCreate 同理 —— 两个都非 nil，:queue 才走完整分派到 PREPARING v3。
		Instances:       c.instances,
		ExecutionCreate: c.executions,
		// send 腿（yaml :544）：ExecutionSend + Store 都在才挂 POST {id}/send；
		// 缺任一个落 catch-all 501（C28②）。
		ExecutionSend: c.executions,
	})
	mux := http.NewServeMux()
	mux.Handle("/ws/v1/events", wsapi.Handler(wsapi.Config{
		Tickets: c.tickets, Journal: journalReader{c.db}, AllowOrigin: httpapi.LoopbackOrigin,
	}))
	mux.Handle("/", rest)
	return mux
}

func (c *Core) serve(ctx context.Context) error {
	srv := &http.Server{Handler: c.handler(), ReadHeaderTimeout: 5 * time.Second}
	ready := httpapi.StateReady
	c.state.Store(&ready)

	errs := make(chan error, 1)
	go func() { errs <- srv.Serve(c.ln) }()

	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stopping := httpapi.StateStopping
		c.state.Store(&stopping)
		// 先不健康再关听，让在飞的请求有机会收尾；推送只发生在事务提交之后（ADR-008），
		// 这里没有未提交的写会被留在中途。
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}

// flushAuditAnchor 在关停时把链头补成一条锚点（T-64③）。
// 用 Anchor.Commit 而不是 CheckpointCounter.Advance：Advance 只在跨过 CheckpointEveryAppends
// 整数倍时才落，表达不了"欠一行就补一行"；Commit 是 audit 导出的写入口，
// 行的格式因此只有一处真相（在 cmd/core 手搓 seq\thash\tms 就是第二处）。
// 三种情况不动文件：一行审计都没有（没什么可认证）、锚点已经认证到这一行、以及库读不出来。
func (c *Core) flushAuditAnchor() error {
	if c.anchor == nil || c.db == nil {
		return nil
	}
	seq, hash, err := audit.HeadQuery(c.db.Read)(context.Background())
	if err != nil {
		return fmt.Errorf("读审计链头: %w", err)
	}
	if seq == 0 {
		return nil
	}
	head, err := c.anchor.Expected(seq)
	switch {
	case err == nil && head.Seq == seq:
		return nil // 已经认证到这一行，重复 Commit 会被 seq 递增检查拒掉
	case err != nil && !errors.Is(err, audit.ErrNoAnchor):
		return fmt.Errorf("读锚点: %w", err)
	}
	return c.anchor.Commit(seq, hash, time.Now().UnixMilli())
}

func (c *Core) writeEndpointFile() error {
	b, err := json.Marshal(endpointFile{HostPort: c.endpoint, Token: c.token, Version: Version})
	if err != nil {
		return err
	}
	path := filepath.Join(c.cfg.DataDir, "core.endpoint")
	// 0o600 在这里只是"请求"：Windows 上它被默认 ACL 映射成 rwxrwxrwx（core_test.go 因此不断言权限位），
	// 只有非 Windows 部署才拿到正确默认。所以这个文件的口令**不受文件权限保护**、同用户任意进程可读——
	// 它是"同用户边界内的门帘，不是隔离机制"（T-53⑤）。正因如此才更不能开一个能远程要到口令的端点。
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("写 core.endpoint: %w", err)
	}
	return nil
}

type endpointFile struct {
	HostPort string `json:"host_port"`
	Token    string `json:"token"`
	Version  string `json:"version"`
}

// reportAuditChain 在启动阶段把 ADR-018 的三态喊出来（T-64③）。
// 三态来自 `audit.Check(整条链, 锚点)` 的真重算，不是"看起来没事就当通过"：
// 缺锚点、读不出来、链落后于锚点都落回 UNVERIFIED/UNKNOWN。
// 唯一允许的乐观措辞是 VERIFIED_LOCAL 那一档，而且必须连着它的上限一起说 ——
// 锚点之后的行不在锚点覆盖范围内（Check 自己的注释也是这么写的）。
// 措辞红线：任何一档都不许出现"防篡改/已签名/signed/tamper"；这里只打日志，不塞进 /health
// （那个端点的键集被用例钉着，链状态字段要连着口径一起加，第 55 条）。
func (c *Core) reportAuditChain(ctx context.Context) {
	status, err := c.auditChainStatus(ctx)
	if err != nil {
		log.Printf("audit 链: UNVERIFIED（校验没能跑成: %v）—— 这不等于校验通过", err)
		return
	}
	if status == audit.StatusVerifiedLocal {
		log.Printf("audit 链: %s —— 逐链重算并撞到锚点；锚点之后的行不在锚点覆盖内", status)
		return
	}
	log.Printf("audit 链: %s", status)
}

func (c *Core) close() {
	// 关库之前补最后一次锚点（T-64③）：db 一关就读不到链头了。
	// 失败要喊出来但不改退出码 —— 干净关停因为锚点写不下去而变成非零退出，
	// 会让"Core 停下来了"这件事实被误报。
	if err := c.flushAuditAnchor(); err != nil {
		log.Printf("core: 审计锚点没收尾: %v", err)
	}
	if c.lock != nil {
		c.lock.release()
	}
	// 端口现在是锁的判据（REQ-L-1），boot 半途失败却不还回端口，下一位 judgeHolder 就会被我自己的耳朵骗成
	// "端口被占" —— 重复 Close 无害（serve/Shutdown 之后再关只返回一个 error）。
	if c.ln != nil {
		c.ln.Close()
	}
	if c.db != nil {
		c.db.Close()
	}
}
