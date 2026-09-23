package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// FIRST SPRINT §7 的 Crash 清单。能真跑的先真跑：
// 「Core 进程在事务中途死亡不得损坏 SQLite、已提交事件不得丢」这一条只依赖 db 平台层，
// 用子进程自杀的方式今天就能验。剩下几条要等 cmd/core（backend）与 adapters/fake（adapter）
// 起进程，一律 t.Skip 并点名依赖，绝不用假对象凑绿。

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
		// 把最常见的那一种失败自己说出来：第二代一旦「以为还有 Core 在跑」，红话就不该只停在
		// 「没认出新一代 endpoint」这一层 —— 让读者从几百字现场里自己找拒绝原因，等于没分类。
		shape := ""
		if strings.Contains(secondLog.String(), "已有 Core 在跑") {
			shape = "\n>>> 形状判定：第二代进程**拒绝起锁**（它认为残留锁里的 pid 还活着），所以从未写 endpoint。" +
				"这不是「端口换了没换」的问题，也不是判据这边的问题：存活判定在 `cmd/core/lock.go` 的 " +
				"`os.FindProcess(pid)` 上，Windows 下它对「已退但句柄未净 / pid 被复用」会给「活着」。"
		}
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
// 实测：两次成功启动（health=READY、迁移已记账）之后 event_journal 仍是 0 行。
func TestCoreReadyEventIsJournalized(t *testing.T) {
	t.Skip("blocked(REQ-T7 给 backend): §6 要求 core_ready 进 journal（principal=system，payload 带 version/migration_version），" +
		"实测 cmd/core 启动两次并 READY 之后 event_journal 仍为 0 行；补上后本用例改为断言存在 core_ready 且 aggregate_type=core")
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

func TestCrashRestartIdentifiesInterrupted(t *testing.T) {
	t.Skip("blocked: 需要 cmd/core（backend，契约③ §7 归属）与 Recovery Engine；" +
		"Core 的启动顺序与 /api/v1/health 就绪判定是这条用例的前置")
}

// TestCrashAdapterKillDoesNotTakeCoreDown §7「kill Adapter」+ 协议 §8：Core 必须活着，
// 且只有该 Adapter 上的 in-flight Execution 转 UNKNOWN（不是 FAILED）。
func TestCrashAdapterKillDoesNotTakeCoreDown(t *testing.T) {
	t.Skip("blocked: 需要 adapters/fake（adapter 角色）与 Process Supervisor 接线；" +
		"断言清单已定：Core /health 仍 200、该 Execution 进 UNKNOWN 且带证据、" +
		"其它 Adapter 的 Execution 状态不变、Notification 有 process_crashed")
}

// TestCrashNoDuplicateMessageAfterRestart §7「不重复发消息」+ 事件契约 §8：
// 同一个 Idempotency-Key 在崩溃前后只能产生一次副作用。
func TestCrashNoDuplicateMessageAfterRestart(t *testing.T) {
	t.Skip("blocked: 需要 cmd/core 的 POST /sessions/{id}/messages（backend）；" +
		"断言清单已定：重启后重发同一 Idempotency-Key → 返回首次 response_json、" +
		"messages 行数不变、event_journal 无新行、idempotency_keys 命中计数 +1")
}

// TestCrashApprovalNotLost §7「Approval 不丢」：WAITING_APPROVAL 在崩溃前后必须仍待决，
// 且不得被自动批准（红线：Approval 应用于错误 Execution）。
func TestCrashApprovalNotLost(t *testing.T) {
	t.Skip("blocked: 需要 cmd/core + internal/approval 的 Service 接线（backend/audit）；" +
		"断言清单已定：重启后 approvals 行仍在且 state=PENDING、" +
		"execution_id 绑定不变、SweepExpired 只按 expires_at 判过期")
}

// TestCrashEventJournalComplete §7「Event Journal 不丢」的完整版：崩溃前已提交事件
// 与重启后补发窗口（/ws/v1/events?from_seq）必须连续无缺口。
func TestCrashEventJournalComplete(t *testing.T) {
	t.Skip("blocked: 需要 cmd/core 的 WS 端点（backend）；seq 连续性的一半已由 " +
		"TestCrashProcessDeathKeepsDurableData 覆盖")
}

// TestIdempotentReplayBodyIsByteIdentical 内部契约 §1.1 第 13 条"响应体口径"（C18 裁定：
// 首次也回读库）。这条判的是 §12 红线里"Secret 泄露到响应"的另一个入口：
// 若 handler 直接返回自己 Marshal 的那一份，一旦被脱敏命中，**首次响应就是未打码的**，
// 库里存的却是打码版；首次与重放也就此分叉。判据两条：
//
//	① 同一 Idempotency-Key 的 201 body 与重放 body 逐字节相等；
//	② 塞一个命中脱敏规则的值（如 {"token":"…"}）时，两份响应都等于库里 response_json 的打码版。
func TestIdempotentReplayBodyIsByteIdentical(t *testing.T) {
	t.Skip("blocked: 需要 POST /api/v1/tasks 与幂等回读路径可用（backend，§1.1#13 的实现方）；" +
		"接线后按上面两条写断言，另按 T-66④ 变异必须改变'是否走到回读那一步'")
}
