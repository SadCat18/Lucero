package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// migrations 用仓库里 db 角色那三份真 .sql，不复制 DDL、不改文件。
func testConfig(t *testing.T) config {
	t.Helper()
	return config{
		DataDir:    t.TempDir(),
		Migrations: filepath.Join("..", "..", "migrations"),
		TicketTTL:  30 * time.Second,
	}
}

func bootAndServe(t *testing.T) *Core {
	t.Helper()
	core, err := boot(t.Context(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.close)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() { _ = core.serve(ctx) }()
	return core
}

// TestHealthReflectsARealBoot 走真启动路径：建目录 → 拿锁 → 开库 → 迁移 → 生成口令
// → 写 core.endpoint → 监听回环端口 → READY。前端与 test 角色的联调都靠这条先跑起来。
func TestHealthReflectsARealBoot(t *testing.T) {
	core := bootAndServe(t)
	cfg := core.cfg
	url := "http://" + core.endpoint + "/api/v1/health"

	var h map[string]any
	if err := getJSON(t, url, "", &h); err != nil {
		t.Fatal(err)
	}
	if h["state"] != "READY" || h["ready"] != true {
		t.Errorf("启动完成后应为 READY 且 ready=true: %v", h)
	}
	// 迁移版本取自真库，不写死数字：多一个 .sql 也不该把用例改红。
	var want int64
	if err := core.db.Read.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&want); err != nil {
		t.Fatal(err)
	}
	if got := h["migration_version"]; got != float64(want) {
		t.Errorf("migration_version = %v，库里是 %d", got, want)
	}
	if v, _ := h["version"].(string); v == "" {
		t.Error("health 没报 Core 版本")
	}

	b, err := os.ReadFile(filepath.Join(cfg.DataDir, "core.endpoint"))
	if err != nil {
		t.Fatal(err)
	}
	var ep endpointFile
	if err := json.Unmarshal(b, &ep); err != nil {
		t.Fatalf("core.endpoint 不是能解析的形状: %v (%s)", err, b)
	}
	if !strings.HasPrefix(ep.HostPort, "127.0.0.1:") || ep.Token == "" {
		t.Errorf("endpoint 文件内容不对: %+v", ep)
	}
	// 权限位不在这断：Windows 上 Go 传的 mode 会被默认 ACL 映射掉（0600/0700 建出来都是 rwxrwxrwx），
	// 断言它只会得到一个假失败。真正的边界是 Bearer 口令 + 只绑 127.0.0.1（契约③ §1.1/§1.2），
	// 上面两条都测了。core.endpoint 里的口令仍按 0600 请求写入，给非 Windows 部署留正确默认。
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "core.endpoint")); err != nil {
		t.Fatal(err)
	}

	// 除 /health 外一律要口令；口令对了才轮到业务面（tasks 已实现 → 200），不许假成功。
	if err := getJSON(t, "http://"+core.endpoint+"/api/v1/tasks", "", nil); !isStatus(err, "401") {
		t.Errorf("没带口令读 /api/v1/tasks 应 401，实际 %v", err)
	}
	var listed map[string]any
	if err := getJSON(t, "http://"+core.endpoint+"/api/v1/tasks", ep.Token, &listed); err != nil {
		t.Fatalf("带对口令读 tasks 应 200，实际 %v", err)
	}
	if items, ok := listed["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("空库的 tasks 列表应为空数组，实际 %v", listed)
	}
	// 还没接的面仍然 501，不许假成功。
	if err := getJSON(t, "http://"+core.endpoint+"/api/v1/projects", ep.Token, nil); !isStatus(err, "501") {
		t.Errorf("未实现的端点应 501，实际 %v", err)
	}
}

// boot 的接线顺序里"SQLite → 装脱敏 → 迁移 → 才允许写事件"（§2.3）必须是可写的状态：
// 平台层 fail-closed（db 自己在 touch_test.go 里钉），所以 boot 之后能写成就是这条顺序生效的证据。
func TestBootLeavesTheStoreWritable(t *testing.T) {
	core := bootAndServe(t)
	// principals 已由 boot 建好（§1.4 单用户 + 两处外键），这里只验"boot 之后能写成、一行事件"。
	err := core.db.Write(t.Context(), func(tx *db.Tx) error {
		return tx.Create(db.AggTask, "t-boot",
			[]string{"title", "created_at", "updated_at"}, []any{"x", int64(1), int64(1)},
			db.EventDraft{EventID: "e-boot", Type: "task_created", Aggregate: db.AggTask,
				AggregateID: "t-boot", PrincipalID: "system"})
	})
	if err != nil {
		t.Fatalf("boot 之后写不进去，说明 §2.3 的接线顺序没生效: %v", err)
	}
	var events int64
	if err := core.db.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE aggregate_id = 't-boot'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("建行应恰带一行事件，实际 %d", events)
	}
}

// 单实例：同一个数据目录不能起第二个 Core（§2.3 第二步）。
func TestSecondBootRefused(t *testing.T) {
	cfg := testConfig(t)
	core, err := boot(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()

	if _, err := boot(t.Context(), cfg); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("第二个 Core 应被单实例锁拒掉，实际 %v", err)
	}
}

// 崩溃测试会 kill Core，锁文件必然是死的：必须能自动接管，否则"重启不丢任务"根本没法测。
func TestStaleLockIsTakenOver(t *testing.T) {
	cfg := testConfig(t)
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "core.lock"), []byte("2147483646\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, err := boot(t.Context(), cfg)
	if err != nil {
		t.Fatalf("死锁文件没能接管（crash 测试会卡在这儿）: %v", err)
	}
	core.close()

	if _, err := os.Stat(filepath.Join(cfg.DataDir, "core.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("正常退出应清掉锁文件，实际 %v", err)
	}
}

func getJSON(t *testing.T, url, token string, out any) error {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return errors.New(resp.Status)
			}
			if out == nil {
				return nil
			}
			return json.NewDecoder(resp.Body).Decode(out)
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond) // serve() 在另一个 goroutine 里起来，等一下
	}
}

func isStatus(err error, code string) bool {
	return err != nil && strings.Contains(err.Error(), code)
}
