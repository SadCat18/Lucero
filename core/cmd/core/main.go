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
	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

// Version 由构建注入；没有就用这个，别让 health 报空。
var Version = "0.1.0-dev"

type config struct {
	DataDir    string
	Migrations string
	TakeOver   bool
	TicketTTL  time.Duration
}

// Core 是跑起来的那一份：持有库、锁、口令、监听与运行态。
type Core struct {
	cfg      config
	lock     *lock
	db       *db.DB
	token    string
	endpoint string // host:port
	ln       net.Listener
	tickets  *httpapi.Tickets

	state atomic.Pointer[string]
}

func main() {
	var cfg config
	flag.StringVar(&cfg.DataDir, "data-dir", defaultDataDir(), "数据目录：放 lucero.db / core.lock / core.endpoint")
	flag.StringVar(&cfg.Migrations, "migrations", "migrations", "迁移 .sql 目录（v0.1 不做迁移内嵌，T-33 已撤回 REQ-T-2）")
	flag.BoolVar(&cfg.TakeOver, "take-over-lock", false, "显式接管上一个进程留下的锁文件")
	var ttl time.Duration
	flag.DurationVar(&ttl, "ws-ticket-ttl", 30*time.Second, "一次性 WS 短票的有效期（契约③ §1.2 默认 30s）")
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

// boot 建目录 → 拿锁 → 开库 → 迁移 → 生成口令 → 写 core.endpoint → 监听回环端口。
// 顺序里"先迁移再写事件"是硬要求：InitRedaction 必须在 SQLite 之后、事件可用之前（§2.3）。
func boot(ctx context.Context, cfg config) (*Core, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("建数据目录: %w", err)
	}
	l, err := acquireLock(cfg.DataDir, cfg.TakeOver)
	if err != nil {
		return nil, err
	}
	core := &Core{cfg: cfg, lock: l}
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

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		core.close()
		return nil, fmt.Errorf("生成口令: %w", err)
	}
	core.token = base64.RawURLEncoding.EncodeToString(buf)

	// 端口由 Core 选定并写进 core.endpoint（契约③ §1.1）：只绑 127.0.0.1。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		core.close()
		return nil, fmt.Errorf("监听回环地址: %w", err)
	}
	core.ln = ln
	core.endpoint = ln.Addr().String()
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
	return core, nil
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

// handler 把两个 transport 面拼起来：/ws/v1/events 走一次性短票（REQ-F-2，不接受 Authorization 头），
// 其余 /api/v1 走 Bearer。两条共用同一个回环来源判定，CORS 规则不出现第二套。
func (c *Core) handler() http.Handler {
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
		Tasks:   task.NewService(nil), TaskList: task.NewRepository(c.db.Read), Store: c.db,
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

func (c *Core) close() {
	if c.lock != nil {
		c.lock.release()
	}
	if c.db != nil {
		c.db.Close()
	}
}
