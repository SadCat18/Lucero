package test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/approval"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// FIRST SPRINT §7 的 Crash 清单。能真跑的先真跑：
// 「Core 进程在事务中途死亡不得损坏 SQLite、已提交事件不得丢」这一条只依赖 db 平台层，
// 用子进程自杀的方式今天就能验。
//
// 当时剩下的几条写着「要等 cmd/core（backend）与 adapters/fake（adapter）起进程」，一律 t.Skip。
// **今天 cmd/core 能 build、能 boot、能被硬杀重启**（见 `buildCore` 与 `coreRun`），所以 W2-7 把这一族
// 逐条重估过：能跑的那半改成真断言，真跑不了的那半**拆成子用例 Skip** 并逐条点名缺的是哪一格
// （file:line + 全仓计数），不整条永远 Skip，也绝不用假对象凑绿（交接包 §9：不拿桩绿冒充真跑）。

const (
	envCrashDB     = "LUCERO_CRASH_DB"
	envCrashHelper = "LUCERO_CRASH_HELPER"
	committedRows  = 5
)

// migrationsDir 定位 core/migrations（go.mod 同级）。
func migrationsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "migrations"), nil
		}
		up := filepath.Dir(dir)
		if up == dir {
			return "", errors.New("找不到模块根，无法定位 core/migrations")
		}
		dir = up
	}
}

// migrate 建库并应用 core/migrations 下已发布的全部脚本。
func migrate(t *testing.T, path string) *db.DB {
	t.Helper()
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = d.Close() })
	dir, err := migrationsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

func createProject(ctx context.Context, d *db.DB, n int) error {
	id := fmt.Sprintf("019e0000-0000-7000-8000-%012d", n)
	return d.Write(ctx, func(tx *db.Tx) error {
		return tx.Create(db.AggProject, id,
			[]string{"name", "canonical_path", "created_at", "updated_at"},
			[]any{fmt.Sprintf("p%d", n), fmt.Sprintf("/p/%d", n), 1757000000000, 1757000000000},
			db.EventDraft{
				EventID:   fmt.Sprintf("019e1111-0000-7000-8000-%012d", n),
				Type:      "project_created",
				Aggregate: db.AggProject, AggregateID: id,
				Payload: map[string]any{"schema": 1, "name": fmt.Sprintf("p%d", n)},
			})
	})
}

// TestCrashProcessDeathKeepsDurableData 事务中途进程死亡：库必须还在，
// 已提交的 5 条一个不少，第 6 条（未提交）一个不多。
func TestCrashProcessDeathKeepsDurableData(t *testing.T) {
	if os.Getenv(envCrashHelper) == "1" {
		crashHelper()
		return
	}
	if err := db.InitRedaction([]string{"token"}, []string{}, "[REDACTED]"); err != nil {
		t.Fatalf("InitRedaction: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "core.db")

	cmd := exec.Command(os.Args[0], "-test.run=TestCrashProcessDeathKeepsDurableData", "-test.v")
	cmd.Env = append(os.Environ(), envCrashHelper+"=1", envCrashDB+"="+path)
	out, err := cmd.CombinedOutput()
	body := string(out)
	if err == nil {
		t.Fatalf("子进程本该在事务中途退出，却正常返回:\n%s", body)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("启动子进程失败: %v\n%s", err, body)
	}
	if !strings.Contains(body, fmt.Sprintf("COMMITTED=%d", committedRows)) {
		t.Fatalf("子进程没有报告它提交了 %d 条，说明它死于非预期位置:\n%s", committedRows, body)
	}

	// 重新打开同一批文件（含热 -wal）：这就是"重启 Core"在平台层的等价物。
	d := migrate(t, path)
	ctx := context.Background()
	if mode, err := d.JournalMode(); err != nil || mode != "wal" {
		t.Errorf("journal_mode=%q err=%v，期望 wal", mode, err)
	}
	var check string
	if err := d.Read.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if check != "ok" {
		t.Fatalf("进程崩溃后 integrity_check = %q（§12 红线：Core Crash 损坏 SQLite）", check)
	}
	var projects, events int
	if err := d.Read.QueryRowContext(ctx, "SELECT count(*) FROM projects").Scan(&projects); err != nil {
		t.Fatal(err)
	}
	if err := d.Read.QueryRowContext(ctx, "SELECT count(*) FROM event_journal").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if projects != committedRows || events != committedRows {
		t.Errorf("崩溃后 projects=%d event_journal=%d，期望各 %d（未提交的那条不得留下）",
			projects, events, committedRows)
	}
	// 独立于上面计数的判据：那条"插完没提交"的记录必须两侧都不存在。
	// 只比总数会漏掉"未提交的留下了、已提交的丢了一条"这种自相抵消的情况。
	var doomed int
	if err := d.Read.QueryRowContext(ctx,
		`SELECT count(*) FROM projects WHERE canonical_path = '/p/doomed'`).Scan(&doomed); err != nil {
		t.Fatal(err)
	}
	if doomed != 0 {
		t.Errorf("未提交的项目行残留在库里（%d 行）", doomed)
	}
	var doomedEvent int
	if err := d.Read.QueryRowContext(ctx,
		`SELECT count(*) FROM event_journal WHERE aggregate_id = '019e0000-0000-7000-8000-999999999999'`).
		Scan(&doomedEvent); err != nil {
		t.Fatal(err)
	}
	if doomedEvent != 0 {
		t.Errorf("未提交的事务给 event_journal 留下了 %d 行（§12 红线：事件与状态必须同生同死）", doomedEvent)
	}
	// 版本列必须与事件数一致：每个聚合恰好 version=1 一条事件。
	var versions int
	if err := d.Read.QueryRowContext(ctx,
		"SELECT count(*) FROM projects WHERE version <> 1 OR version IS NULL").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 0 {
		t.Errorf("有 %d 行的 version 不是 1（未提交事务被部分写入？）", versions)
	}
}

// crashHelper 在子进程里跑：提交 5 条，然后在第 6 条的事务中间硬退出。
func crashHelper() {
	path := os.Getenv(envCrashDB)
	if path == "" {
		fmt.Println("FAIL: no db path")
		os.Exit(3)
	}
	if err := db.InitRedaction([]string{"token"}, []string{}, "[REDACTED]"); err != nil {
		fmt.Println("FAIL: InitRedaction:", err)
		os.Exit(3)
	}
	d, err := db.Open(path)
	if err != nil {
		fmt.Println("FAIL: open:", err)
		os.Exit(3)
	}
	defer d.Close()
	dir, err := migrationsDir()
	if err != nil {
		fmt.Println("FAIL: migrations dir:", err)
		os.Exit(3)
	}
	if err := d.Migrate(dir); err != nil {
		fmt.Println("FAIL: migrate:", err)
		os.Exit(3)
	}
	ctx := context.Background()
	for i := range committedRows {
		if err := createProject(ctx, d, i); err != nil {
			fmt.Println("FAIL: commit:", err)
			os.Exit(3)
		}
	}
	fmt.Printf("COMMITTED=%d\n", committedRows)
	// 到这里才开第二个事务，并在插完聚合、还没 COMMIT 时死掉。
	_ = d.Write(ctx, func(tx *db.Tx) error {
		_ = tx.Create(db.AggProject, "019e0000-0000-7000-8000-999999999999",
			[]string{"name", "canonical_path", "created_at", "updated_at"},
			[]any{"doomed", "/p/doomed", 1757000000000, 1757000000000},
			db.EventDraft{EventID: "019e1111-0000-7000-8000-999999999999", Type: "project_created",
				Aggregate: db.AggProject, AggregateID: "019e0000-0000-7000-8000-999999999999",
				PrincipalID: "system", Payload: map[string]any{"schema": 1}})
		os.Exit(9) // 不给任何收尾机会：等价于 SIGKILL（Windows 上没有 SIGKILL）
		return nil
	})
}

// TestCrashRestartIdentifiesInterrupted §7「重启 Core 后正确识别 interrupted / recoverable
// Execution」。骨架：拉起 Core → 造一个 RUNNING Execution → kill → 重启 → 断言
// Task 走 §5.1 的 RUNNING→INTERRUPTED（Recovery 边）、Execution 进 INTERRUPTED 或 UNKNOWN，
// 且 event_journal 里出现 task_state_corrected 时才允许状态被"改判"。
// ---- 真跑：拉起 core/cmd/core、硬杀、重启 ------------------------------------

// coreProc 是把 Core 当真子进程驱动的最小夹具。
type coreProc struct {
	t       *testing.T
	dataDir string
	cmd     *exec.Cmd
}

func buildCore(t *testing.T) string {
	t.Helper()
	goBin := os.Getenv("LUCERO_GO")
	if goBin == "" {
		goBin = "go"
	}
	exe := filepath.Join(t.TempDir(), "core.exe")
	b := exec.Command(goBin, "build", "-o", exe, "./cmd/core")
	b.Dir = moduleRoot()
	b.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := b.CombinedOutput(); err != nil {
		t.Skipf("blocked: build cmd/core failed (backend in flight), 不拿桩绿冒充真跑: %v\n%s", err, out)
	}
	return exe
}

func (c *coreProc) start(exe string) {
	c.dataDir = filepath.Join(c.t.TempDir(), "data")
	c.cmd = exec.Command(exe, "-data-dir", c.dataDir,
		"-migrations", filepath.Join(moduleRoot(), "migrations"))
	c.cmd.Stderr = io.Discard
	c.cmd.Stdout = io.Discard
	if err := c.cmd.Start(); err != nil {
		c.t.Fatalf("启动 Core: %v", err)
	}
	c.t.Cleanup(func() { _ = c.cmd.Process.Kill() })
}

type endpoint struct {
	HostPort string `json:"host_port"`
	Token    string `json:"token"`
	Version  string `json:"version"`
}

func (c *coreProc) waitEndpoint() endpoint {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(c.dataDir, "core.endpoint")); err == nil {
			var e endpoint
			if json.Unmarshal(b, &e) == nil && e.HostPort != "" {
				return e
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	c.t.Fatal("20s 内没有写出 core.endpoint（§1.1 要求启动后立即可被发现）")
	return endpoint{}
}

// probeHealth 是 health 的**非致命**版本，只给窗口内的诊断用。
// 为什么必须另有一个：`health()` 请求失败就 `t.Fatal`，而"上一代端口暂时不通"正是重启窗口里
// 的正常状态 —— 我 05:01:43 第一版就拿它做试探，结果诊断探针自己把用例判死了（1.45s FAIL，
// 红句是 `connectex: No connection…`，与端口假设毫无关系）。诊断不能有权力杀人。
func probeHealth(e endpoint) (string, error) {
	req, err := http.NewRequest("GET", "http://"+e.HostPort+"/api/v1/health", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	cl := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if s, ok := out["state"].(string); ok {
		return s, nil
	}
	return "无 state 字段", nil
}

func health(t *testing.T, e endpoint) map[string]any {
	t.Helper()
	req, err := http.NewRequest("GET", "http://"+e.HostPort+"/api/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	// 必须显式关代理：本机有代理在听，而 Go 的默认 Transport 会读 HTTP_PROXY/ALL_PROXY，
	// 把 127.0.0.1 的请求转给代理 → 拿到空响应会被误判成"Core 没起来"（T-31③a）。
	cl := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("health 请求失败: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("health 响应解不开: %v", err)
	}
	return out
}

// TestCrashKillCoreAndRestart 真跑 §7 的"运行中 kill Core → 重启"两件事：
// 硬杀（TerminateProcess ≈ SIGKILL，不给任何收尾机会）之后，
// ① 库必须没坏（integrity_check=ok，且热 -wal 能被下一次打开消化掉）；
// ② Core 必须能在**没有人工干预**（不加 -take-over-lock）的情况下重启并就绪；
// ③ 已迁移的 schema 与迁移记账不能因为崩溃而丢或重放。
func TestCrashKillCoreAndRestart(t *testing.T) {
	exe := buildCore(t)
	if err := db.InitRedaction([]string{"token"}, []string{}, "[REDACTED]"); err != nil {
		t.Fatal(err)
	}
	c := &coreProc{t: t}
	c.start(exe)
	e := c.waitEndpoint()
	got := health(t, e)
	if got["state"] != "READY" || got["ready"] != true {
		t.Fatalf("第一次启动就没就绪: %v", got)
	}
	// 不写死数字：health 报的 migration_version 必须等于库里的 MAX(version)。
	// backend 实测把断言写成 ==2 就被 000003 打假（T-31②），所以这里等 Core 死后直接问库。
	hv, okH := got["migration_version"].(float64)
	if !okH {
		t.Errorf("health.migration_version 不是数字: %v", got["migration_version"])
	}
	hvI := int64(hv)
	_ = c.cmd.Process.Kill()
	if err := c.cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("等待被杀的进程: %v", err)
		}
	}
	dbPath := filepath.Join(c.dataDir, "lucero.db")
	if _, err := os.Stat(dbPath + "-wal"); err == nil {
		t.Log("崩溃现场保留了热 -wal 文件，正好验证恢复路径")
	}
	d := migrate(t, dbPath) // 重开同一批文件 = "重启 Core"在平台层的等价物
	var check string
	if err := d.Read.QueryRowContext(context.Background(), "PRAGMA integrity_check").Scan(&check); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if check != "ok" {
		t.Fatalf("kill Core 之后库损坏了（§12 红线：Core Crash 损坏 SQLite）: %q", check)
	}
	var tables, migrations int
	var dbMax int64
	ctx := context.Background()
	if err := d.Read.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&dbMax); err != nil {
		t.Fatal(err)
	}
	if dbMax != hvI {
		t.Errorf("health.migration_version=%d 而库里 MAX(version)=%d —— UI 显示的状态与 Core 实际状态不一致（§12 红线）",
			hvI, dbMax)
	}
	if err := d.Read.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := d.Read.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if tables < 33 || migrations < 1 {
		t.Errorf("崩溃后 tables=%d migrations=%d，期望 >=33 / >=1", tables, migrations)
	}
}

// classifySecondBootStderr 把"第二代到底为什么没起来"这一句判决单独拆出来，好让它自己能被测。
// 它只在红的时候被调用，而红的时候正是没人逐字读 stderr 的时刻 —— 一个会撒谎的分类器比没有分类器更贵：
// 09-24 07:3x REQ-L-1 的修法 (a) 落盘、`lock.go` 里那套 `os.FindProcess` pid 启发式被整体删除之后，
// 我这段旧文案还在把读者支去查一段**已经不存在的代码**。三条约定：
// ① 只按第二代自己打出来的那句分派，对不上就返回空（现场照印，机制不猜）；
// ② **不许出现具体实现符号**（`os.FindProcess` / `judgeHolder` 这类会随修法腐烂）；
// ③ 键词与 `cmd/core/lock.go` 两条错误文案的对齐是当天读码核对的（见账本），不写死在这里当断言——
//
//	他们改措辞时我的分类器只会退回"不猜"，不会撒谎，也不会红得莫名其妙。
func classifySecondBootStderr(s string) string {
	switch {
	case strings.Contains(s, "已有 Core 在跑"):
		return "\n>>> 形状判定：第二代判定锁里记的那个端口上**真有一个还在答 /health 的 Core**，" +
			"于是它拒绝起、也从没写 endpoint。⇒ 上一代没真的退，或那个端口上确实另起了一个 Core。" +
			"两种都不是判据这边的问题，归 `cmd/core`。"
	case strings.Contains(s, "端口被别的进程占着"):
		return "\n>>> 形状判定：第二代**绑不上**锁里记的那个端口、且对该端口的健康探针没拿到应答。" +
			"崩溃重启撞上这一支有两种现实：占端口的真不是 Core；或它是「还没退干净的上一代」" +
			"（socket 要等进程终止才回收）。哪一种都不由我从这里猜 —— 归 `cmd/core`，现场在上面。"
	}
	return ""
}

// TestSecondBootShapeClassifierOnlySaysWhatItHeard 钉住上面那条分类器自己：
// 两个判决各自落地、都对不上时**必须沉默**，以及两句话都不许带上一次修法的实现符号。
// 这三条都是合成输入 —— 真红一次不需要，因为这里被测的就是"拿到一段 stderr 会说什么"，
// 而 stderr 是我能自己造的（§1.1 第 16 条：只有真数据能红的不算闭上）。
func TestSecondBootShapeClassifierOnlySaysWhatItHeard(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantSub string // 空 = 必须沉默
		mustNot string
	}{
		{"活 Core 占着端口", "2026/09/24 07:36:31 core: cmd/core: 已有 Core 在跑同一个数据目录", "真有一个还在答 /health 的 Core", "os.FindProcess"},
		{"端口被别人占着", "core: cmd/core: 数据目录里记录的端口被别的进程占着，不是 Core（端口 50747）", "绑不上", "os.FindProcess"},
		{"沉默的失败", "core: 迁移失败：no such table: sessions", "", ""},
		{"空 stderr 也不许编", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySecondBootStderr(tc.in)
			if tc.wantSub == "" {
				if got != "" {
					t.Errorf("这段输入里没有锁的形状，分类器却开口了：%q", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("判决没落地：想要含 %q，实际 %q", tc.wantSub, got)
			}
			if tc.mustNot != "" && strings.Contains(got, tc.mustNot) {
				t.Errorf("判决里出现了会腐烂的实现符号 %q（修法一改它就开始撒谎）：%q", tc.mustNot, got)
			}
		})
	}
}

// TestCrashRestartSameDataDir 用**同一个数据目录**重启：单实例锁的残留必须被自动识别并回收，
// 否则真实用户重启电脑后 Core 起不来（这是 §7"重启 Core"的实际含义）。
func TestCrashRestartSameDataDir(t *testing.T) {
	exe := buildCore(t)
	dir := t.TempDir()
	// 两个孩子的 stderr 必须**接住而不是丢掉**：05:04 之前这里是 `io.Discard`，
	// 于是"新 core 到底为什么没写 endpoint"在我手里没有任何证据可看，只能靠猜
	// （巡检那条端口假设之所以一度没法证伪，一半责任在我的 Discard 上）。
	var firstLog, secondLog bytes.Buffer
	first := exec.Command(exe, "-data-dir", dir, "-migrations", filepath.Join(moduleRoot(), "migrations"))
	first.Stderr = &firstLog
	first.Stdout = io.Discard
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	e1 := waitForEndpoint(t, dir)
	if got := health(t, e1); got["state"] != "READY" {
		t.Fatalf("首次未就绪: %v", got)
	}
	_ = first.Process.Kill()
	_ = first.Wait()

	// 上一代的 core.endpoint 还在原地，所以要认出"这是新一代写的"。
	// 换代见证用 **口令**：`cmd/core` 每次 boot 用 crypto/rand 现编 32 字节（main.go:177~182），
	// 不入库、不落配置 ⇒ 新文件里的 token 与旧的不同，就一定是另一代进程写的。
	// 端口不当见证的理由：Core 绑 `127.0.0.1:0`（main.go:185），端口由 OS 发还，理论上可以重发同一个。
	// **但这条假设已被我自己的采样否掉**：05:03:27~05:04:22 十二次同配置采样里
	// "新一代沿用旧端口"出现 0 次（那 12 次里 11 绿 1 红），所以这次的 26s 红不是端口造成的；
	// 端口断言仍然是错的判法（它不该参与判决），只是它不是本次红的原因。
	start := time.Now()
	second := exec.Command(exe, "-data-dir", dir, "-migrations", filepath.Join(moduleRoot(), "migrations"))
	second.Stderr = &secondLog
	second.Stdout = io.Discard
	if err := second.Start(); err != nil {
		t.Fatalf("第二次启动进程失败: %v", err)
	}
	t.Cleanup(func() { _ = second.Process.Kill() })
	deadline := start.Add(25 * time.Second)
	var e2 endpoint
	var (
		rounds          int
		lastSeen        endpoint
		fileRewritten   bool
		sawNewToken     bool
		samePortAsOld   bool
		lastErr         string
		samePortReadyAt int // 第一次旧端口还能被 health 答 READY 的轮次（诊断用）
	)
	for time.Now().Before(deadline) {
		rounds++
		b, err := os.ReadFile(filepath.Join(dir, "core.endpoint"))
		if err != nil {
			lastErr = "读不到 core.endpoint: " + err.Error()
		} else if json.Unmarshal(b, &lastSeen) != nil {
			lastErr = "core.endpoint 不是合法 JSON"
		} else {
			if fi, e := os.Stat(filepath.Join(dir, "core.endpoint")); e == nil && !fi.ModTime().Before(start) {
				fileRewritten = true
			}
			if lastSeen.Token != "" && lastSeen.Token != e1.Token {
				sawNewToken = true
				// "端口相同"只有在**确认是新一代**之后才有意义：
				// 窗口第一轮读到的本来就是旧文件，那时端口必然相同 —— 我 05:02:42 第一版就是这么误报的
				// （8/8 都打"同端口"日志，等于什么都没测到）。
				if lastSeen.HostPort == e1.HostPort {
					samePortAsOld = true
					if s, err := probeHealth(lastSeen); err == nil && s == "READY" {
						samePortReadyAt = rounds
					}
				}
			}
			if fileRewritten && sawNewToken && lastSeen.HostPort != "" {
				e2 = lastSeen
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	// 失败时把"两种红"分开：文件没被改写 ⇒ 锁没回收（产品缺）；改写了但 health 不 READY ⇒ 新一代没起来（产品缺）；
	// 改写、口令变、health READY ⇒ 换代成功，红只可能出在判据自己身上。
	if e2.HostPort == "" {
		hv := "未探"
		if lastSeen.HostPort != "" {
			s, err := probeHealth(lastSeen)
			if err != nil {
				hv = "探测失败: " + err.Error()
			} else {
				hv = s
			}
		}
		shape := classifySecondBootStderr(secondLog.String())
		t.Fatalf("硬杀后同目录重启 25s 内没认出新一代 core.endpoint —— 现场：轮数=%d 文件被改写=%v "+
			"口令换过=%v 新一代沿用旧端口=%v 且曾答 READY 的轮次=%d 最后一次读到的=%+v health=%v 最近一次读文件=%q"+
			"\n第一代 stderr 尾部：%s\n第二代 stderr 尾部：%s（旧值：端口=%s 口令前 8 字=%s）%s",
			rounds, fileRewritten, sawNewToken, samePortAsOld, samePortReadyAt, lastSeen, hv, lastErr,
			tailOf(firstLog.Bytes(), 400), tailOf(secondLog.Bytes(), 600),
			e1.HostPort, e1.Token[:8], shape)
	}
	if samePortAsOld {
		t.Logf("换代时 OS 把上一代的端口 %s 又发给了新一代（旧断言在此会拒收健康的新 core） —— 换代靠口令认出（token 前 8 字 %s→%s），端口不参与判决",
			e1.HostPort, e1.Token[:8], e2.Token[:8])
	}
	got := health(t, e2)
	if got["state"] != "READY" {
		t.Errorf("崩溃后重启没就绪: %v", got)
	}
}

// TestCoreReadyEventIsJournalized 契约① §6 登记了 core_ready / core_shutdown（后者还要 Audit）。
// REQ-T7 落地后的真断言：一次成功 boot（health=READY、迁移已记账）之后
// event_journal 里必须有且恰有一条 core_ready，aggregate_type=core、principal=system，
// payload 带 version 与 migration_version（崩溃恢复要能数出"起过几次、各带什么版本"）。
func TestCoreReadyEventIsJournalized(t *testing.T) {
	exe := buildCore(t)
	if err := db.InitRedaction([]string{"token"}, []string{}, "[REDACTED]"); err != nil {
		t.Fatal(err)
	}
	c := &coreProc{t: t}
	c.start(exe)
	e := c.waitEndpoint()
	got := health(t, e)
	if got["state"] != "READY" || got["ready"] != true {
		t.Fatalf("启动没就绪，无法验 core_ready: %v", got)
	}
	_ = c.cmd.Process.Kill()
	if err := c.cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("等待被杀的进程: %v", err)
		}
	}
	// 重开同一批文件读 journal（与 TestCrashKillCoreAndRestart 同一形状）。
	dbPath := filepath.Join(c.dataDir, "lucero.db")
	d := migrate(t, dbPath)
	ctx := context.Background()
	var n int
	if err := d.Read.QueryRowContext(ctx,
		`SELECT count(*) FROM event_journal WHERE event_type = ?`, "core_ready").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("一次成功 boot 应恰落 1 条 core_ready，实际 %d 条", n)
	}
	var aggType, aggID, principal, payload string
	if err := d.Read.QueryRowContext(ctx,
		`SELECT aggregate_type, aggregate_id, principal_id, payload_json
		 FROM event_journal WHERE event_type = ?`, "core_ready").
		Scan(&aggType, &aggID, &principal, &payload); err != nil {
		t.Fatal(err)
	}
	if aggType != "core" || aggID != "core" {
		t.Errorf("core_ready 应挂 aggregate_type=core / id=core，实际 %s/%s", aggType, aggID)
	}
	if principal != "system" {
		t.Errorf("core_ready 的 principal 应为 system，实际 %q", principal)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("payload 不是合法 JSON: %v (%s)", err, payload)
	}
	for _, k := range []string{"version", "migration_version"} {
		if _, ok := p[k]; !ok {
			t.Errorf("core_ready payload 缺 %s（REQ-T7 点名的两键）: %s", k, payload)
		}
	}
	// migration_version 必须与 health 报的一致（不写死数字）。
	if hv, ok := got["migration_version"].(float64); ok {
		if mv, ok2 := p["migration_version"].(float64); !ok2 || mv != hv {
			t.Errorf("payload.migration_version=%v 与 health 的 %v 不一致", p["migration_version"], hv)
		}
	}
}

// tailOf 取子进程日志的尾部：失败时最有用的通常是最后几行（panic、拒绝起锁的报错），
// 而整份 Core 日志可能长到把判决刷出屏幕。
func tailOf(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		if s == "" {
			return "(空)"
		}
		return s
	}
	return "…" + s[len(s)-n:]
}

func waitForEndpoint(t *testing.T, dir string) endpoint {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join(dir, "core.endpoint"))
		var e endpoint
		if err == nil && json.Unmarshal(b, &e) == nil && e.HostPort != "" {
			return e
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("25s 内没有 core.endpoint")
	return endpoint{}
}

// ══════════════════════════════════════════════════════════════════════════
// §7 崩溃族共用夹具：真 core.exe 子进程 + 真 SQLite + 真 HTTP/WS。
// 一个测试进程两副面孔（见 TestMain）：平时跑用例，被 cmd/core 当 adapter 拉起时卡住等人杀。
// ══════════════════════════════════════════════════════════════════════════

const (
	// envAdapterChild 一支两用，值是「孩子要把自己的 pid 写进去的那个文件」。
	// 我们把 `-adapter hang=<本测试二进制>` 与这个变量一起塞进 **Core 子进程**的环境，
	// Core 只当它是普通环境变量原样传给孩子（`exec.Cmd.Env` 为 nil 时继承），自己不读、也不需要懂；
	// 我们自己的进程里永远没有它，所以不会误入孩子那条路。
	envAdapterChild = "LUCERO_TEST_ADAPTER_CHILD"

	// 与 api/http 那边的 testSecret / secretShape 同形：真规则集（audit.Rules）里
	// `sk-[A-Za-z0-9_-]{16,}` 是命中脱敏的那条 pattern，用来验「首次与重放都得是打码版」。
	crashSecret = "sk-abcdefghijklmnop0123456789"
	crashShape  = `sk-[A-Za-z0-9_-]{16,}`
	crashMarker = "[REDACTED]"
)

// TestMain 只为一件事存在：让本测试二进制能充当「会挂住的 Adapter 子进程」。
// 没有它，被 cmd/core 无参数拉起来的孩子会跑整包用例 —— 那不是崩溃夹具，那是灾难。
// （同一个 helper 形状在 cmd/fake-adapter/main_test.go:24 有先例。）
func TestMain(m *testing.M) {
	if pidFile := os.Getenv(envAdapterChild); pidFile != "" {
		adapterChildHang(pidFile)
		return
	}
	os.Exit(m.Run())
}

// adapterChildHang 是孩子那半：先把 pid 落盘（外面要精确杀**这一台**，不猜、不扫进程表），
// 然后卡在读 stdin 上 —— 不回握手、也不自己退。stdin 断了（Core 死了或 probe 收口）才退出，
// 所以「它还活着」完全由 Core 那根管道决定，与我们杀不杀它无关。
func adapterChildHang(pidFile string) {
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
	_, _ = io.ReadAll(os.Stdin)
	os.Exit(0)
}

// redactForCrash 把本进程的脱敏规则调成与 Core 生产码同形（cmd/core/main.go:160 用的是 audit.Rules）。
// 不带 pattern 的话 `sk-…` 不会被打码，第 6 条的断言就成了空断言。
func redactForCrash(t *testing.T) {
	t.Helper()
	if err := db.InitRedaction([]string{"token"}, []string{crashShape}, crashMarker); err != nil {
		t.Fatalf("InitRedaction: %v", err)
	}
}

// coreRun 把 core.exe 当真子进程驱动：可带启动参数与环境变量、可硬杀、可同目录重启。
type coreRun struct {
	t      *testing.T
	exe    string
	dir    string
	args   []string
	env    []string
	cmd    *exec.Cmd
	stderr bytes.Buffer
	e      endpoint
}

// boot 起一代 Core 并等到「换代 + 就绪」两件事都成真。
// 换代用**口令**认（与 TestCrashRestartSameDataDir 同一判据）：上一代的 core.endpoint 还在原地，
// 端口却可能被 OS 原样发还，所以 token 不变就一定是旧文件。
func (r *coreRun) boot() {
	prev := r.e
	argv := []string{"-data-dir", r.dir, "-migrations", filepath.Join(moduleRoot(), "migrations")}
	argv = append(argv, r.args...)
	cmd := exec.Command(r.exe, argv...)
	cmd.Stdout = io.Discard
	cmd.Stderr = &r.stderr
	if len(r.env) > 0 {
		cmd.Env = append(os.Environ(), r.env...)
	}
	if err := cmd.Start(); err != nil {
		r.t.Fatalf("启动 Core: %v", err)
	}
	r.cmd = cmd
	proc := cmd
	r.t.Cleanup(func() { _ = proc.Process.Kill() })

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join(r.dir, "core.endpoint"))
		var e endpoint
		if err == nil && json.Unmarshal(b, &e) == nil && e.HostPort != "" &&
			(prev.Token == "" || e.Token != prev.Token) {
			r.e = e
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if r.e == prev || r.e.HostPort == "" || (prev.Token != "" && r.e.Token == prev.Token) {
		r.t.Fatalf("30s 内没等到新一代 core.endpoint（上一代口令前 8 字=%s）\nCore stderr 尾部: %s",
			shortToken(prev.Token), tailOf(r.stderr.Bytes(), 600))
	}
	// 端口写出来 ≠ 就绪：必须真探到 READY 才算这一代起来了。
	readyDeadline := time.Now().Add(30 * time.Second)
	last := "(一次都没探到)"
	for time.Now().Before(readyDeadline) {
		s, err := probeHealth(r.e)
		if err != nil {
			last = err.Error()
		} else {
			last = s
			if s == "READY" {
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	r.t.Fatalf("Core 30s 内没就绪（最后见到 state=%q）\nCore stderr 尾部: %s",
		last, tailOf(r.stderr.Bytes(), 600))
}

// killHard 硬杀（TerminateProcess ≈ SIGKILL，不给任何收尾机会），并把进程收掉。
func (r *coreRun) killHard() {
	if r.cmd == nil || r.cmd.Process == nil {
		return
	}
	_ = r.cmd.Process.Kill()
	if err := r.cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			r.t.Fatalf("等待被硬杀的 Core: %v", err)
		}
	}
}

// awaitPID 等孩子把 pid 写进文件（自己写 = 精确，不猜、不扫进程表）。
func (r *coreRun) awaitPID(path string, timeout time.Duration) int {
	r.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n > 0 {
				return n
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.t.Fatalf("%s 内没拿到子进程 pid（Core 没把它拉起来？）\nCore stderr 尾部: %s",
		timeout, tailOf(r.stderr.Bytes(), 600))
	return 0
}

type apiResult struct {
	status int
	body   []byte
	err    error
}

// apiCall 一次真 HTTP 往返。代理必须显式关掉（本机有代理在听，见 health() 的注释）。
func apiCall(e endpoint, method, path, principal, key, body string) apiResult {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://"+e.HostPort+path, rd)
	if err != nil {
		return apiResult{err: err}
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	req.Header.Set("Content-Type", "application/json")
	if principal != "" {
		req.Header.Set("X-Lucero-Principal", principal)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	cl := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := cl.Do(req)
	if err != nil {
		return apiResult{err: err}
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	return apiResult{status: resp.StatusCode, body: out, err: err}
}

func mustAPI(t *testing.T, e endpoint, method, path, principal, key, body string, want int) []byte {
	t.Helper()
	r := apiCall(e, method, path, principal, key, body)
	if r.err != nil {
		t.Fatalf("%s %s: %v", method, path, r.err)
	}
	if r.status != want {
		t.Fatalf("%s %s: 期望 %d，实际 %d: %s", method, path, want, r.status, tailOf(r.body, 400))
	}
	return r.body
}

// crashSeed 两条执行链：一条挂在「会被杀的那台 Adapter（hang）」上，一条挂另一台。
// §7 要的是「只动该 Adapter 的 in-flight，其它一律不动」，所以两边都得有才验得了。
type crashSeed struct {
	hangSession, hangTask, hangExecution    string
	otherSession, otherTask, otherExecution string
}

// seedCrashFixture 在 Core 起来**之前**把行准备好（进程外写库，纯夹具 SQL，不冒充任何生产写路径）。
// 造 RUNNING 的 Execution 今天没有 HTTP 入口（`:queue` 建出来的是 CREATED，要 adapter 真投递才 RUNNING），
// 所以这一层只能 SQL —— 正因如此，「这条 Execution 该被改判」那半留给了 REQ-RC-1 的子用例。
func seedCrashFixture(t *testing.T, dir string) crashSeed {
	t.Helper()
	d := openMigrated(t, dir)
	ins := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Read.Exec(q, args...); err != nil {
			t.Fatalf("夹具插入失败 %s: %v", q, err)
		}
	}
	// principals 先于一切：event_journal.principal_id 与 idempotency_keys.principal_id 都有外键，
	// 而 cmd/core 的 ensurePrincipal（main.go:269）是「查到就跳过」的幂等形状，不会和这里打架。
	ins(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
		VALUES ('local-user', 'USER', '本地用户', 1, 1)`)
	ins(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
		VALUES ('system', 'SYSTEM', 'Core', 1, 1)`)

	var s crashSeed
	s.hangSession, s.hangTask, s.hangExecution = seedLane(t, d, "hang", "1")
	s.otherSession, s.otherTask, s.otherExecution = seedLane(t, d, "other", "2")
	return s
}

func seedLane(t *testing.T, d *db.DB, adapterKey, suffix string) (sessionID, taskID, executionID string) {
	t.Helper()
	p := func(slot string) string { return "019e2222-0000-7000-8000-0000000000" + suffix + slot }
	adapterID := p("1")
	sessionID, taskID, executionID = p("2"), p("3"), p("4")
	ins := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Read.Exec(q, args...); err != nil {
			t.Fatalf("夹具插入失败 %s: %v", q, err)
		}
	}
	ins(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES (?, ?, ?, 1, 1)`, adapterID, adapterKey, adapterKey)
	ins(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, managed, sync_state,
			state_confidence, created_at, updated_at)
		VALUES (?, ?, ?, 1, 'SYNCED', 'HIGH', 1, 1)`, sessionID, adapterID, "remote-"+suffix)
	ins(`INSERT INTO tasks (id, title, created_at, updated_at) VALUES (?, ?, 1, 1)`,
		taskID, "崩溃夹具-"+adapterKey)
	ins(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, attempt_no, status,
			state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`,
		executionID, taskID, adapterID, sessionID)
	return sessionID, taskID, executionID
}

// openMigrated 开同一份 lucero.db 并应用迁移（夹具那一步在 Core 起来之前跑，
// Core 自己随后也会跑一次 —— 迁移幂等，这正是「重启」的实际形状）。
func openMigrated(t *testing.T, dir string) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(dir, "lucero.db"))
	if err != nil {
		t.Fatalf("打开库 %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = d.Close() })
	mig, err := migrationsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(mig); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

// withDB 短暂开一条自己的连接去问库（Core 同时开着写连接也没事：WAL + busy_timeout=5000）。
func withDB(t *testing.T, dir string, fn func(d *db.DB)) {
	t.Helper()
	d, err := db.Open(filepath.Join(dir, "lucero.db"))
	if err != nil {
		t.Fatalf("打开库: %v", err)
	}
	defer func() { _ = d.Close() }()
	fn(d)
}

func countSQL(t *testing.T, dir, q string, args ...any) int {
	t.Helper()
	var n int
	withDB(t, dir, func(d *db.DB) {
		if err := d.Read.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	})
	return n
}

// execState 问库：这条 Execution 现在什么状态、最后一次被动是什么时候。
// ok=false 就是行不见了 —— §7 里那句「崩溃不许把行弄丢」的最直接形态。
func execState(t *testing.T, dir, id string) (status string, updatedAt int64, ok bool) {
	t.Helper()
	withDB(t, dir, func(d *db.DB) {
		err := d.Read.QueryRow(`SELECT status, updated_at FROM executions WHERE id = ?`, id).
			Scan(&status, &updatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return
		}
		if err != nil {
			t.Fatalf("读 Execution %s: %v", id, err)
		}
		ok = true
	})
	return status, updatedAt, ok
}

func journalSeqs(t *testing.T, dir string) []int64 {
	t.Helper()
	var out []int64
	withDB(t, dir, func(d *db.DB) {
		rows, err := d.Read.Query(`SELECT seq FROM event_journal ORDER BY seq`)
		if err != nil {
			t.Fatalf("读 event_journal: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var s int64
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("读 seq: %v", err)
			}
			out = append(out, s)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("读 event_journal: %v", err)
		}
	})
	return out
}

// missingSeqs 报出「崩溃前在、重启后没了」的那些 seq：已提交事件不得丢的直接判据。
func missingSeqs(t *testing.T, dir string, before []int64) []int64 {
	t.Helper()
	now := map[int64]bool{}
	for _, s := range journalSeqs(t, dir) {
		now[s] = true
	}
	var gone []int64
	for _, s := range before {
		if !now[s] {
			gone = append(gone, s)
		}
	}
	return gone
}

func shortToken(tok string) string {
	if tok == "" {
		return "(无)"
	}
	if len(tok) > 8 {
		return tok[:8] + "…"
	}
	return tok
}

// killPID 从外面杀一台**不是我们孩子**的进程：这里必须精确到 pid，所以 pid 由孩子自己落盘。
func killPID(t *testing.T, pid int) {
	t.Helper()
	p, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("按 pid=%d 找进程: %v", pid, err)
	}
	if err := p.Kill(); err != nil {
		t.Fatalf("杀 pid=%d: %v", pid, err)
	}
}

// ── 极简 WS 客户端（Go 标准库没有客户端）─────────────────────────────────────

func issueWSTicket(t *testing.T, e endpoint) string {
	t.Helper()
	b := mustAPI(t, e, "POST", "/api/v1/ws-ticket", "", "", "", http.StatusOK)
	var out struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Ticket == "" {
		t.Fatalf("签票响应不对: %s（err=%v）", tailOf(b, 300), err)
	}
	return out.Ticket
}

type wsConn struct {
	net.Conn
	br *bufio.Reader
}

func (w *wsConn) Read(p []byte) (int, error) { return w.br.Read(p) }

func dialWS(t *testing.T, hostPort, path string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", hostPort)
	if err != nil {
		t.Fatalf("拨 WS: %v", err)
	}
	req := "GET " + path + " HTTP/1.1\r\nHost: " + hostPort + "\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("WS 握手应 101，实际 %d", resp.StatusCode)
	}
	return &wsConn{Conn: c, br: br}
}

func writeWSHello(t *testing.T, c net.Conn, payload string) {
	t.Helper()
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		t.Fatal(err)
	}
	b := []byte(payload)
	frame := append([]byte{0x81, 0x80 | byte(len(b))}, mask[:]...)
	for i, v := range b {
		frame = append(frame, v^mask[i%4])
	}
	if _, err := c.Write(frame); err != nil {
		t.Fatal(err)
	}
}

func readWSFrame(t *testing.T, c net.Conn) map[string]any {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var br *bufio.Reader
	if wc, ok := c.(*wsConn); ok {
		br = wc.br
	} else {
		br = bufio.NewReader(c)
	}
	var h [2]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		t.Fatalf("读帧头: %v", err)
	}
	n := int(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(br, b[:]); err != nil {
			t.Fatalf("读扩展长度: %v", err)
		}
		n = int(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(br, b[:]); err != nil {
			t.Fatalf("读扩展长度: %v", err)
		}
		n = int(binary.BigEndian.Uint64(b[:]))
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(br, payload); err != nil {
		t.Fatalf("读帧体: %v", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(payload, &frame); err != nil {
		t.Fatalf("帧不是 JSON: %q: %v", payload, err)
	}
	return frame
}

// nextWSEvent 收到下一条业务事件为止。ping / hello 回显插在哪都不算违约；
// 别的帧型**当场判死** —— 补发窗口里冒出意料之外的帧正是这条用例要抓的形状。
func nextWSEvent(t *testing.T, c net.Conn) map[string]any {
	t.Helper()
	for {
		f := readWSFrame(t, c)
		switch f["type"] {
		case "event":
			return f
		case "ping", "hello", "hello_ack":
			continue
		default:
			t.Fatalf("补发窗口里收到意外帧: %v", f)
		}
	}
}

func eventSeq(t *testing.T, frame map[string]any) int64 {
	t.Helper()
	d, _ := frame["data"].(map[string]any)
	seq, ok := d["seq"].(float64)
	if !ok {
		t.Fatalf("事件帧没有 seq: %v", frame)
	}
	return int64(seq)
}

// ══════════════════════════════════════════════════════════════════════════
// §7 的六条
// ══════════════════════════════════════════════════════════════════════════

// TestCrashRestartIdentifiesInterrupted §7「重启 Core 后正确识别 interrupted / recoverable Execution」。
// 真跑：拉起 Core → 一条 RUNNING Execution → 硬杀 → 同目录重启 → 断言行没丢、没被误判成失败，
// 且**状态要动就必须有改判事件**（§7 原话：event_journal 里出现改判事件时才允许被改判）。
// 子用例「把它认出来并改判」在 T-391 第二段（09-29 主控裁「分」）之后**转真**：
// 夹具补一行进程事实 ⇒ 恢复扫描把 RUNNING 改判成 INTERRUPTED（那条边今天唯一的生产者就是它）。
func TestCrashRestartIdentifiesInterrupted(t *testing.T) {
	exe := buildCore(t)
	redactForCrash(t)
	dir := t.TempDir()
	s := seedCrashFixture(t, dir)

	r := &coreRun{t: t, exe: exe, dir: dir}
	r.boot()
	if h := health(t, r.e); h["state"] != "READY" || h["ready"] != true {
		t.Fatalf("首次启动没就绪: %v", h)
	}

	before, beforeAt, ok := execState(t, dir, s.hangExecution)
	if !ok || before != "RUNNING" {
		t.Fatalf("夹具没就位（status=%q ok=%v）", before, ok)
	}
	evBefore := countSQL(t, dir, `SELECT count(*) FROM event_journal WHERE aggregate_id = ?`, s.hangExecution)
	seqsBefore := journalSeqs(t, dir)

	r.killHard()
	r.boot()
	if h := health(t, r.e); h["state"] != "READY" || h["ready"] != true {
		t.Fatalf("同目录重启没就绪: %v", h)
	}
	if gone := missingSeqs(t, dir, seqsBefore); len(gone) > 0 {
		t.Errorf("崩溃前已提交的事件丢了 %d 条: %v（崩溃前共 %d 条）", len(gone), gone, len(seqsBefore))
	}

	after, afterAt, ok := execState(t, dir, s.hangExecution)
	if !ok {
		t.Fatalf("重启后 Execution %s 的行不见了（§7：崩溃不许把行弄丢）", s.hangExecution)
	}
	if after == "FAILED" || after == "CANCELLED" || after == "SUCCEEDED" {
		t.Errorf("崩溃被当成了结局：Execution 被改判成 %s（§7/CO-8：进程死掉不是失败的证据）", after)
	}
	if after != before || afterAt != beforeAt {
		evAfter := countSQL(t, dir, `SELECT count(*) FROM event_journal WHERE aggregate_id = ?`, s.hangExecution)
		if evAfter == evBefore {
			t.Errorf("重启把 Execution 从 %s（updated_at=%d）改成了 %s（updated_at=%d），"+
				"event_journal 里针对该聚合的事件一条都没多 —— 没有证据的改判（§7 红线）",
				before, beforeAt, after, afterAt)
		} else {
			t.Logf("Execution 被改判成 %s（%d→%d 条事件），改判带着证据", after, evBefore, evAfter)
		}
	}

	t.Run("把 interrupted 认出来并改判（带证据）", func(t *testing.T) {
		// 夹具缺的那行进程事实在夹具层补上：process_instances 是夹具 SQL 可写的，且这一行只是
		// 「上一代 Core 起过、随它一起死掉的那台」的登记 —— 不冒充任何生产写路径（seedCrashFixture 那句照读）。
		// 没有这一行时归因闸门落 CO-44② 的「不改判、只喊」，状态不动。
		r.killHard()
		withDB(t, dir, func(d *db.DB) {
			var adapterID string
			if err := d.Read.QueryRow(`SELECT adapter_instance_id FROM executions WHERE id = ?`,
				s.hangExecution).Scan(&adapterID); err != nil {
				t.Fatalf("读这条 Execution 的 adapter 实例: %v", err)
			}
			if _, err := d.Read.Exec(`INSERT INTO process_instances
				(id, process_type, owner_type, owner_id, pid, status, executable, metadata_json, created_at, updated_at)
				VALUES ('pi-crash-hang', 'ADAPTER', 'adapter_instance', ?, 4242, 'EXITED', 'hang',
					'{"attempt":1}', 1, 1)`, adapterID); err != nil {
				t.Fatalf("夹具进程行插入失败: %v", err)
			}
		})
		r.boot()
		if h := health(t, r.e); h["state"] != "READY" || h["ready"] != true {
			t.Fatalf("补进程行后重启没就绪: %v", h)
		}

		// ① 正身是 INTERRUPTED（09-29 主控裁「分」：Core 重启那一支 = RUNNING→INTERRUPTED，
		//    FIRST SPRINT §7 interrupted/recoverable；Adapter 崩溃那一支的 →UNKNOWN 本轮不接线）。
		after, _, ok := execState(t, dir, s.hangExecution)
		if !ok || after != "INTERRUPTED" {
			t.Fatalf("恢复扫描没把它认成 INTERRUPTED: ok=%v status=%s（失败时看 core 日志里「恢复扫描」那几句）",
				ok, after)
		}
		// ② 改判必须带证据（§7 原话：event_journal 里出现改判事件时才允许被改判）。
		if n := countSQL(t, dir, `SELECT count(*) FROM event_journal
			WHERE aggregate_id = ? AND event_type = 'execution_interrupted'`, s.hangExecution); n != 1 {
			t.Errorf("改判必须恰带一条 execution_interrupted，实际 %d 条", n)
		}
		// ③ 同事务那条 audit（§6 Audit 列 ✓，events.go:52）。
		if n := countSQL(t, dir, `SELECT count(*) FROM audit_entries
			WHERE action = 'execution_interrupted' AND execution_id = ?`, s.hangExecution); n != 1 {
			t.Errorf("§6 Audit 列 ✓ 的那条同事务 audit 行没写出来: %d 行", n)
		}
		// ④ 另一台没补进程行 ⇒ 一个字都不许动（归因闸门按台分格）。
		if st, _, ok := execState(t, dir, s.otherExecution); !ok || st != "RUNNING" {
			t.Errorf("另一台的 Execution 被这次扫描动了（它查不到自己的进程行 ⇒ 不改判只喊）: ok=%v status=%s", ok, st)
		}
	})
}

// TestCrashAdapterKillDoesNotTakeCoreDown §7「kill Adapter」+ 协议 §8：Core 必须活着，
// 且只有该 Adapter 上的 in-flight Execution 转 UNKNOWN（不是 FAILED）。
// 今天真跑得动的那半：真 Core 配一台「会挂住的 Adapter 子进程」（就是本测试二进制），
// 从外面按 pid 精确杀掉它，然后验 Core 还活着、且别的 Adapter 的 Execution 一个字都没动。
// 「转 UNKNOWN」与「process_crashed」两格各有各的缺口，拆成子用例 Skip 点名。
func TestCrashAdapterKillDoesNotTakeCoreDown(t *testing.T) {
	exe := buildCore(t)
	redactForCrash(t)
	dir := t.TempDir()
	s := seedCrashFixture(t, dir)
	pidFile := filepath.Join(dir, "adapter.pid")

	r := &coreRun{t: t, exe: exe, dir: dir,
		args: []string{"-adapter", "hang=" + os.Args[0]},
		env:  []string{envAdapterChild + "=" + pidFile}}
	r.boot()
	firstHP := r.e.HostPort
	if h := health(t, r.e); h["state"] != "READY" {
		t.Fatalf("启动没就绪: %v", h)
	}

	// probe 这一发会挂住：孩子不回握手，所以我们先拿到 pid，再从外面杀它。
	resCh := make(chan apiResult, 1)
	go func() {
		resCh <- apiCall(r.e, "POST", "/api/v1/adapters/hang/probe", "local-user", "k-crash-probe", "")
	}()
	pid := r.awaitPID(pidFile, 15*time.Second)
	killPID(t, pid)

	var res apiResult
	select {
	case res = <-resCh:
	case <-time.After(20 * time.Second):
		t.Fatal("子进程被杀之后 20s 内 probe 还没返回 —— Core 挂在了这一发上")
	}
	t.Logf("子进程被外力杀掉后 probe 的应答: %d %s", res.status, tailOf(res.body, 300))
	if res.err != nil {
		t.Fatalf("probe 请求本身失败: %v", res.err)
	}
	if res.status == http.StatusOK {
		t.Errorf("子进程被杀之后 probe 还报 200: %s", tailOf(res.body, 300))
	}

	// ① Core 必须还活着，而且是**同一代**进程（端口没换 = 没重启、没倒）。
	h := health(t, r.e)
	if h["state"] != "READY" || h["ready"] != true {
		t.Errorf("Adapter 被杀之后 Core 不再就绪: %v", h)
	}
	if r.e.HostPort != firstHP {
		t.Errorf("Core 端口从 %s 换成了 %s —— 这不是同一个还活着的 Core", firstHP, r.e.HostPort)
	}
	// ② 另一台 Adapter 上的 Execution 一个字都不许动（§8：只有该 Adapter 的 in-flight 才该动）。
	if st, at, ok := execState(t, dir, s.otherExecution); !ok || st != "RUNNING" {
		t.Errorf("另一台 Adapter 的 Execution 被这次崩溃动了: ok=%v status=%s updated_at=%d", ok, st, at)
	}
	// ③ 被杀的确实是 Core 自己拉起来的那台（process_started 落了行、pid 对得上）——
	//    少了这一句，上面两条可能只是「Core 压根没把 adapter 起起来」。
	if n := countSQL(t, dir, `SELECT count(*) FROM process_instances WHERE pid = ?`, pid); n != 1 {
		t.Errorf("pid=%d 在 process_instances 里有 %d 行，期望恰 1 行（Core 没把它登记成自己的子进程？）", pid, n)
	}

	t.Run("process_crashed 事件 + process_instances 进 FAILED", func(t *testing.T) {
		t.Skip("blocked: 真进程里杀掉 Adapter 不会落 process_crashed —— 唯一 emit EventCrashed 的地方是 " +
			"internal/process/supervisor.go:405 的 Supervisor.Run，而 **Run 在生产码里零调用点**" +
			"（全仓只有 supervisor_test.go 调它）；cmd/core 只走 Start（cmd/core/processpipe.go:162）" +
			"与 Stop（processpipe.go:211），duplexPipe.Close 也不 emit。" +
			"⇒ process_instances 不会进 FAILED、exit_code 不会落、process_crashed 不会写。" +
			"另注：原 SKIP 文案里「Notification 有 process_crashed」与协议 §8 不符 —— " +
			"Notification 是连续 5 次崩溃预算耗尽那一支，单次崩溃的可观察物本来就是 process_crashed 事件。" +
			"归属 backend/process（把 Run 或等价的退出观察接进 cmd/core）。")
	})
	t.Run("该 Adapter 的 in-flight Execution 转 UNKNOWN 且带证据", func(t *testing.T) {
		t.Skip("blocked: 这一支按 09-29 主控裁「分」**本轮不接线** —— Adapter 崩溃（Core 活着、对端进程死）⇒ " +
			"→UNKNOWN，与 Core 重启那条 INTERRUPTED 边是两条边、两个场景，扫描器按场景分叉（契约① :706~723）。" +
			"它的触发点是 Supervisor 观察到子进程死亡 ⇒ 依赖 internal/process/supervisor.go:381 Supervisor.Run " +
			"（W2-7 点名的生产码零调用点，归 backend/process；今天 cmd/core 只走 processpipe.go:162 Start / " +
			":211 Stop）⇒ 排在本轮之后。事件名已裁 execution_unknown（REQ-RC-2 裁「甲」，契约① :717~719），" +
			"但**登记授权随落地批**、现在不登 §6 —— C8-12 是双向对拍，先登行必红 ⇒ 顺序照 REQ-DB-4：同一批里" +
			"加常量 + 主控批 §6 行。Task 侧不是欠账：execution_unknown 不进 §5.3（投影无合法目标 ⇒ 不产生 Task " +
			"事件），§5.1 里 task RUNNING 的恢复列只有 INTERRUPTED、本就没有 RUNNING→UNKNOWN 这条边 ——" +
			"这是矩阵规定的，不立项。要补的夹具同另一个子用例：该台的 process_instances 行。" +
			"归属 backend/process（退出观察接进 cmd/core）+ 主控（§6 登记授权随落地批）。")
	})
}

// TestCrashNoDuplicateMessageAfterRestart §7「不重复发消息」+ 事件契约 §8：
// 同一个 Idempotency-Key 在崩溃前后只能产生一次副作用。
// 端点按现量改口：原 SKIP 文案点的 POST /sessions/{id}/messages 已随端点裁除不做（契约③ §3），
// 今天能验同一条红线的是 POST /api/v1/executions/{id}/send（Idempotency-Key 必带，executions.go:275）。
// delivery=queue 不走连接面 ⇒ 真进程、真崩溃、真重放都能真跑。
func TestCrashNoDuplicateMessageAfterRestart(t *testing.T) {
	exe := buildCore(t)
	redactForCrash(t)
	dir := t.TempDir()
	s := seedCrashFixture(t, dir)

	r := &coreRun{t: t, exe: exe, dir: dir}
	r.boot()
	sendBody := `{"text":"崩溃前后只能发一次","delivery":"queue"}`
	first := mustAPI(t, r.e, "POST", "/api/v1/executions/"+s.hangExecution+"/send",
		"local-user", "k-crash-send", sendBody, http.StatusAccepted)

	msgs := countSQL(t, dir, `SELECT count(*) FROM messages`)
	nonBoot := countSQL(t, dir, `SELECT count(*) FROM event_journal WHERE event_type <> 'core_ready'`)
	if msgs == 0 {
		t.Fatalf("首次 send 一条 messages 都没落，夹具没就位")
	}

	r.killHard()
	r.boot()

	replay := mustAPI(t, r.e, "POST", "/api/v1/executions/"+s.hangExecution+"/send",
		"local-user", "k-crash-send", sendBody, http.StatusAccepted)
	if !bytes.Equal(first, replay) {
		t.Errorf("重启后同键重放与首次不是逐字节同一份：\n  首次 = %s\n  重放 = %s", first, replay)
	}
	if got := countSQL(t, dir, `SELECT count(*) FROM messages`); got != msgs {
		t.Errorf("messages 行数从 %d 变成 %d（§40：高风险动作一次都不许重复执行）", msgs, got)
	}
	if got := countSQL(t, dir, `SELECT count(*) FROM event_journal WHERE event_type <> 'core_ready'`); got != nonBoot {
		t.Errorf("非 core_ready 事件数从 %d 变成 %d —— 重放多写了事件", nonBoot, got)
	}
	var (
		state    string
		response string
	)
	withDB(t, dir, func(d *db.DB) {
		if err := d.Read.QueryRow(`SELECT state, response_json FROM idempotency_keys WHERE key = ?`,
			"k-crash-send").Scan(&state, &response); err != nil {
			t.Fatalf("读 idempotency_keys: %v", err)
		}
	})
	if state != "COMPLETED" {
		t.Errorf("重放之后 idempotency_keys.state=%q，期望 COMPLETED（密封没跨过崩溃）", state)
	}
	if response != string(first) {
		t.Errorf("库里存的 response_json 与首次响应不是同一份：\n  库 = %s\n  首次 = %s", response, first)
	}
	// 重启后再打一发同键：仍必须是同一份（两发重放互相也得逐字节一致）。
	again := mustAPI(t, r.e, "POST", "/api/v1/executions/"+s.hangExecution+"/send",
		"local-user", "k-crash-send", sendBody, http.StatusAccepted)
	if !bytes.Equal(first, again) {
		t.Errorf("第二次重放与首次不同：\n  首次 = %s\n  再来 = %s", first, again)
	}
}

// TestCrashApprovalNotLost §7「Approval 不丢」：WAITING_APPROVAL 在崩溃前后必须仍待决，
// 且不得被自动批准（红线：Approval 应用于错误 Execution）。
// 今天真跑得动的那半：用 internal/approval 的**真 Service**（不是夹具 SQL）在崩溃前落一条 PENDING，
// 硬杀 + 同目录重启之后，行必须还在、仍 PENDING、绑的还是同一条 Execution。
// 「SweepExpired 只按 expires_at 判过期」与读写面那两半各自缺一格，拆成子用例 Skip。
func TestCrashApprovalNotLost(t *testing.T) {
	exe := buildCore(t)
	redactForCrash(t)
	dir := t.TempDir()
	s := seedCrashFixture(t, dir)

	var approvalID string
	withDB(t, dir, func(d *db.DB) {
		now := func() int64 { return time.Now().UnixMilli() }
		svc := approval.NewService(approval.NewSQLiteStore(d, now), now)
		a, err := svc.Request(context.Background(), approval.Draft{
			PrincipalID: "local-user", TaskID: s.hangTask, ExecutionID: s.hangExecution,
			Type: "TOOL_CALL", Action: "shell.exec", Resource: "/bin/ls",
			RiskLevel: "HIGH", ExpiresAt: now() + 60_000,
		})
		if err != nil {
			t.Fatalf("用真 Service 落一条 PENDING approval: %v", err)
		}
		approvalID = a.ID
	})
	if approvalID == "" {
		t.Fatal("approval 没落库")
	}

	r := &coreRun{t: t, exe: exe, dir: dir}
	r.boot()
	if h := health(t, r.e); h["state"] != "READY" {
		t.Fatalf("启动没就绪: %v", h)
	}

	r.killHard()
	r.boot()
	if h := health(t, r.e); h["state"] != "READY" {
		t.Fatalf("同目录重启没就绪: %v", h)
	}

	var (
		state       string
		executionID string
		expiresAt   int64
	)
	withDB(t, dir, func(d *db.DB) {
		now := func() int64 { return time.Now().UnixMilli() }
		a, err := approval.NewSQLiteStore(d, now).Get(context.Background(), approvalID)
		if errors.Is(err, approval.ErrNotFound) {
			t.Fatalf("重启后 approvals 行 %s 不见了（§7：Approval 不丢）", approvalID)
		}
		if err != nil {
			t.Fatalf("读 approvals: %v", err)
		}
		state, executionID, expiresAt = a.State, a.ExecutionID, a.ExpiresAt
	})
	if state != "PENDING" {
		t.Errorf("重启后 approval 状态是 %q，期望仍 PENDING（红线：崩溃不得替人做决定）", state)
	}
	if executionID != s.hangExecution {
		t.Errorf("approval 绑的 execution 从 %s 换成了 %s（红线：Approval 不得用于错误 Execution）",
			s.hangExecution, executionID)
	}
	if expiresAt == 0 {
		t.Errorf("expires_at 被清空了 —— 后面那条 SweepExpired 用例就没有判据可依")
	}

	t.Run("SweepExpired 只按 expires_at 判过期", func(t *testing.T) {
		t.Skip("blocked: cmd/core 里没有任何 SweepExpired 调用点 —— 全仓非测试码只有 " +
			"internal/approval/approval.go:406 的实现本体与 :67/:76/:permission.go:81 的注释，" +
			"cmd/core/main.go:174 只构造了 approval.NewSQLiteStore（锚点计数器用），" +
			"**没有 approval.NewService** ⇒ 过期收敛这条生产路径今天不存在，跑了也是空转。" +
			"归属 backend（approval/audit 侧 T-372 之后的 Core 接线）。")
	})
	t.Run("approvals 读写面（GET list / GET {id} / POST {id}:resolve）", func(t *testing.T) {
		t.Skip("blocked: core/api/http/approvals.go:30、:48、:56 三条全 501 not_implemented，" +
			"文案明写「Core 还没装配这一层」并点名归属 notWiredOwner = approval/audit 那一族（T-372）。" +
			"⇒ 崩溃前后「读得到、待决着」这条今天在 HTTP 面上验不了，只能验到库层（本用例上面那三条）。" +
			"归属 T-372 + backend 接线。")
	})
}

// TestCrashEventJournalComplete §7「Event Journal 不丢」的完整版：崩溃前已提交事件
// 与重启后补发窗口（/ws/v1/events?from_seq）必须连续无缺口。
// 真 Core + 真 socket + 真票（POST /api/v1/ws-ticket）：从 seq=0 起一条不缺、一步不跳。
func TestCrashEventJournalComplete(t *testing.T) {
	exe := buildCore(t)
	redactForCrash(t)
	dir := t.TempDir()
	_ = seedCrashFixture(t, dir)

	r := &coreRun{t: t, exe: exe, dir: dir}
	r.boot()
	// 崩溃前先落几条真业务事件：POST /api/v1/tasks 是真写，事件进 journal。
	for i := 1; i <= 3; i++ {
		mustAPI(t, r.e, "POST", "/api/v1/tasks", "local-user",
			fmt.Sprintf("k-ws-crash-%d", i), fmt.Sprintf(`{"title":"崩溃前 %d"}`, i), http.StatusCreated)
	}
	want := journalSeqs(t, dir)
	if len(want) < 4 {
		t.Fatalf("崩溃前 journal 只有 %d 条（core_ready + 3 条 task_created），夹具没就位: %v", len(want), want)
	}

	r.killHard()
	r.boot()
	after := journalSeqs(t, dir)
	// 崩溃前已提交的事件一条都不许少（库里这一半）。
	if gone := missingSeqs(t, dir, want); len(gone) > 0 {
		t.Fatalf("重启后崩溃前的事件丢了: %v（崩溃前 %v，重启后 %v）", gone, want, after)
	}

	// 补发窗口这一半：从 from_seq=0 起必须把崩溃前那批按 seq 原样、连续地推下来。
	ws := dialWS(t, r.e.HostPort, "/ws/v1/events?ticket="+issueWSTicket(t, r.e))
	defer ws.Close()
	writeWSHello(t, ws, `{"type":"hello","from_seq":0}`)
	if f := readWSFrame(t, ws); f["type"] != "hello" || f["from_seq"] != float64(0) {
		t.Fatalf(`带游标的第一帧应是 hello 回显 {"type":"hello","from_seq":0}，实际 %v`, f)
	}

	var got []int64
	for len(got) < len(want) {
		got = append(got, eventSeq(t, nextWSEvent(t, ws)))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("补发窗口在第 %d 条断了：库里 seq=%d，WS 收到 %d\n  库里 = %v\n  WS   = %v",
				i+1, want[i], got[i], want, got)
		}
	}
	// 一路推到「当前」为止：重启那条 core_ready 是新代写的，也在同一段连续窗口里。
	if len(after) > len(want) {
		if last := eventSeq(t, nextWSEvent(t, ws)); last != after[len(after)-1] {
			t.Errorf("补发窗口没走到当前：期望收到 seq=%d，实际 %d", after[len(after)-1], last)
		}
	}
	t.Logf("崩溃前 %d 条 + 重启后新增，补发窗口连续收到 seq=%v", len(want), got)
}

// TestIdempotentReplayBodyIsByteIdentical 内部契约 §1.1 第 13 条"响应体口径"（C18 裁定：
// 首次也回读库）。这条判的是 §12 红线里"Secret 泄露到响应"的另一个入口：
// 若 handler 直接返回自己 Marshal 的那一份，一旦被脱敏命中，**首次响应就是未打码的**，
// 库里存的却是打码版；首次与重放也就此分叉。判据两条：
//
//	① 同一 Idempotency-Key 的 201 body 与重放 body 逐字节相等；
//	② 塞一个命中脱敏规则的值（如 sk-…）时，两份响应都等于库里 response_json 的打码版。
//
// 真跑形状：真 Core、真库，**崩溃跨过去**再重放 —— 这一格是 api/http 那边 in-process 用例验不到的
// （它们没有第二次进程）。变异按 T-66④ 做：把回读改成 handler 自己 Marshal ⇒ 必须红。
func TestIdempotentReplayBodyIsByteIdentical(t *testing.T) {
	exe := buildCore(t)
	redactForCrash(t)
	dir := t.TempDir()

	r := &coreRun{t: t, exe: exe, dir: dir}
	r.boot()
	body := `{"title":"标题里贴了密钥 ` + crashSecret + `","priority":"NORMAL"}`
	first := mustAPI(t, r.e, "POST", "/api/v1/tasks", "local-user", "k-crash-replay", body, http.StatusCreated)
	if bytes.Contains(first, []byte(crashSecret)) {
		t.Errorf("首次 201 body 把未脱敏的密钥形状值发出去了: %s", first)
	}
	if !bytes.Contains(first, []byte(crashMarker)) {
		t.Errorf("首次 body 里没有打码标记，这条就成了空断言: %s", first)
	}
	tasksBefore := countSQL(t, dir, `SELECT count(*) FROM tasks`)
	nonBootBefore := countSQL(t, dir, `SELECT count(*) FROM event_journal WHERE event_type <> 'core_ready'`)

	r.killHard()
	r.boot()

	replay := mustAPI(t, r.e, "POST", "/api/v1/tasks", "local-user", "k-crash-replay", body, http.StatusCreated)
	if !bytes.Equal(first, replay) {
		t.Errorf("① 崩溃前后同键重放与首次不是逐字节相等：\n  首次 = %s\n  重放 = %s", first, replay)
	}
	var stored, state string
	withDB(t, dir, func(d *db.DB) {
		if err := d.Read.QueryRow(`SELECT response_json, state FROM idempotency_keys WHERE key = ?`,
			"k-crash-replay").Scan(&stored, &state); err != nil {
			t.Fatalf("读 idempotency_keys: %v", err)
		}
	})
	if state != "COMPLETED" {
		t.Errorf("密封没跨过崩溃：state=%q", state)
	}
	if stored != string(first) || stored != string(replay) {
		t.Errorf("② wire 与存储不是同一份（C18 那一格）：\n  库里 = %s\n  首次 = %s\n  重放 = %s",
			stored, first, replay)
	}
	if bytes.Contains([]byte(stored), []byte(crashSecret)) {
		t.Errorf("库里存了明文: %s", stored)
	}
	// 重放一条副作用都不许多。
	if n := countSQL(t, dir, `SELECT count(*) FROM tasks`); n != tasksBefore {
		t.Errorf("重放把 tasks 从 %d 条写成了 %d 条", tasksBefore, n)
	}
	if n := countSQL(t, dir, `SELECT count(*) FROM event_journal WHERE event_type <> 'core_ready'`); n != nonBootBefore {
		t.Errorf("重放把非 core_ready 事件从 %d 条写成了 %d 条", nonBootBefore, n)
	}
	var rowTitle string
	withDB(t, dir, func(d *db.DB) {
		if err := d.Read.QueryRow(`SELECT title FROM tasks LIMIT 1`).Scan(&rowTitle); err != nil {
			t.Fatalf("读 tasks.title: %v", err)
		}
	})
	if bytes.Contains([]byte(rowTitle), []byte(crashSecret)) {
		t.Errorf("明文密钥进了 tasks.title —— 打码不能只发生在响应层: %s", rowTitle)
	}
}

// 一次性正向对照：改一行注释，让收戳与起戳不同
