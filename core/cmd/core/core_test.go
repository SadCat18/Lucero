package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
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
	// 第三十二批把 GET /api/v1/projects 接通了，所以"仍未实现"的例子换成 `:read`（它还等 REQ-DB-5）。
	if err := getJSON(t, "http://"+core.endpoint+"/api/v1/notifications/n-none:read", ep.Token, nil); !isStatus(err, "501") {
		t.Errorf("未实现的端点应 501，实际 %v", err)
	}
	// 而 /projects 这条现在必须在真进程的装配里真的能用 —— 顺带把新接的读通道钉到真二进制上：
	// 空库 ⇒ items 是空数组（不是 null）、next_cursor 是字符串（REQ-F-19 只承认空串这一种"没有"）。
	var projects map[string]any
	if err := getJSON(t, "http://"+core.endpoint+"/api/v1/projects", ep.Token, &projects); err != nil {
		t.Fatalf("GET /api/v1/projects 已经接了，应 200，实际 %v", err)
	}
	if items, ok := projects["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("空库的 projects 列表应为空数组，实际 %v", projects)
	}
	if _, ok := projects["next_cursor"].(string); !ok {
		t.Errorf(`next_cursor 该是字符串键，实际 %#v`, projects["next_cursor"])
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

// appendAuditThroughStore 与上面那条的区别只有一处：走 **交付用的 `core.store.Write`**，
// 所以 `auditedStore` 那半（提交成功后按链头差交账）会被真触发。一笔事务里连写 n 行，
// 是因为阈值现在数的是 seq 差（audit 的 A18-3 改判），一行一笔会让断言变成"翻 n 次页"。
func appendAuditThroughStore(c *Core, count int) error {
	return c.store.Write(context.Background(), func(tx *db.Tx) error {
		ch := &audit.Chain{
			Head:   audit.HeadQuery(tx),
			Insert: tx,
			Now:    func() int64 { return time.Now().UnixMilli() },
		}
		for i := 0; i < count; i++ {
			auditRowNo++
			if _, err := ch.Append(context.Background(), audit.Entry{
				AuditID: fmt.Sprintf("aud-%d", auditRowNo), PrincipalID: "system",
				Action: "task.pause", ResourceType: "task", ResourceID: "t-anchor",
				Outcome: "SUCCESS",
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// auditHeadRow 读库里的链头，用来把断言钉在"真那一行"上而不是我传进去的数上。
func auditHeadRow(t *testing.T, c *Core) (int64, string) {
	t.Helper()
	var seq int64
	var hash string
	if err := c.db.Read.QueryRow(`SELECT seq, entry_hash FROM audit_entries ORDER BY seq DESC LIMIT 1`).
		Scan(&seq, &hash); err != nil {
		t.Fatal(err)
	}
	return seq, hash
}

// appendAuditRows 用 audit 的公开写入口（Chain + 事务内的 HeadQuery）造 n 行真审计记录。
// 不自己拼 INSERT：链哈希是 audit 的地盘，手搓一份就是第二处真相。
func appendAuditRows(t *testing.T, c *Core, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		auditRowNo++
		rowID := auditRowNo
		if err := c.db.Write(context.Background(), func(tx *db.Tx) error {
			ch := &audit.Chain{
				Head:   audit.HeadQuery(tx),
				Insert: tx,
				Now:    func() int64 { return time.Now().UnixMilli() },
			}
			_, err := ch.Append(context.Background(), audit.Entry{
				// audit_id 上有 UNIQUE：编号必须跨多次调用单调，不能用"这批的第几个"。
				AuditID: fmt.Sprintf("aud-%d", rowID), PrincipalID: "system",
				Action: "task.pause", ResourceType: "task", ResourceID: "t-anchor",
				Outcome: "SUCCESS",
			})
			return err
		}); err != nil {
			t.Fatalf("造审计第 %d 行: %v", rowID, err)
		}
	}
}

var auditRowNo int

func anchorLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestShutdownFlushesAuditAnchor 是 ADR-018 / T-64③ 的四条：
// 空链不凭空造锚点；补的锚点内容必须等于库里那一行的 (seq, entry_hash)；
// 认证到同一行再收尾是 no-op（不重复、不报错）；追加之后补第二行且**不覆盖第一行**（append-only）。
// 最后一条证的是 close() 里真的挂了收尾 —— 关库之后就读不到链头了。
func TestShutdownFlushesAuditAnchor(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	// 中途 t.Fatalf 也不能把 core.lock 攥在手里不放 —— 那会让 t.TempDir() 的清理报一句
	// "file being used by another process"，把真问题埋在最下面。
	closed := false
	t.Cleanup(func() {
		if !closed {
			core.close()
		}
	})
	path := filepath.Join(core.cfg.DataDir, "audit.head")

	if err := core.flushAuditAnchor(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("一行审计都没有时不该造锚点文件（没检查过 ≠ 已认证到 0），实际 stat err=%v", err)
	}

	appendAuditRows(t, core, 2)
	var wantSeq int64
	var wantHash string
	if err := core.db.Read.QueryRow(`SELECT seq, entry_hash FROM audit_entries ORDER BY seq DESC LIMIT 1`).
		Scan(&wantSeq, &wantHash); err != nil {
		t.Fatal(err)
	}
	if err := core.flushAuditAnchor(); err != nil {
		t.Fatal(err)
	}
	lines := anchorLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("第一次收尾应有一行锚点，实际 %d 行: %q", len(lines), lines)
	}
	fields := strings.Split(lines[0], "\t")
	if len(fields) != 3 || fields[0] != strconv.FormatInt(wantSeq, 10) || fields[1] != wantHash {
		t.Errorf("锚点不是库里那一行的 (seq, entry_hash): %q vs seq=%d hash=%s", fields, wantSeq, wantHash)
	}

	if err := core.flushAuditAnchor(); err != nil {
		t.Fatalf("重复收尾必须安静地什么都不做: %v", err)
	}
	if got := len(anchorLines(t, path)); got != 1 {
		t.Errorf("重复收尾写了第 %d 行", got)
	}

	appendAuditRows(t, core, 1)
	if err := core.flushAuditAnchor(); err != nil {
		t.Fatal(err)
	}
	lines = anchorLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("追加之后应有两行锚点，实际 %d", len(lines))
	}
	if lines[0] != fields[0]+"\t"+fields[1]+"\t"+fields[2] {
		t.Errorf("append-only 被破坏，第一行被改写了: %q", lines[0])
	}

	// close() 里挂的那一次：留一行没认证的，只调 close()，不手动 flush。
	appendAuditRows(t, core, 1)
	closed = true
	core.close()
	if got := len(anchorLines(t, path)); got != 3 {
		t.Errorf("close() 没有补锚点，行数 %d（期望 3）", got)
	}
}

// TestBootReportsChainStatusHonestly 钉 ADR-018 与 T-64③ 的两面：
// 该乐观时才乐观（逐链真重算 + 撞到锚点 = VERIFIED_LOCAL，且必须连"锚点之后不在覆盖内"一起说），
// 该闭嘴时闭嘴（没行 / 没锚点 / 链落后于锚点）—— 这三种里任何一种冒出 VERIFIED_LOCAL 都是虚报。
// 第四档是删掉一行模拟篡改：锚点领先于库内，正是 Check 自己定义的 ErrChainBehindAnchor。
func TestBootReportsChainStatusHonestly(t *testing.T) {
	cases := []struct {
		name     string
		rows     int
		anchored bool
		truncate bool // 锚点写好之后再删掉最后一行：库里比锚点少
		want     string
	}{
		{"一行审计都没有 → UNKNOWN", 0, false, false, "UNKNOWN"},
		{"有行但没锚点 → UNVERIFIED", 2, false, false, "UNVERIFIED"},
		{"逐链重算并撞到锚点 → VERIFIED_LOCAL", 1, true, false, string(audit.StatusVerifiedLocal)},
		{"锚点领先于库里（删过行）→ UNVERIFIED", 3, true, true, "UNVERIFIED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, err := boot(context.Background(), testConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			defer core.close()
			if tc.rows > 0 {
				appendAuditRows(t, core, tc.rows)
			}
			if tc.anchored {
				if err := core.flushAuditAnchor(); err != nil {
					t.Fatal(err)
				}
			}
			if tc.truncate {
				if _, err := core.db.Read.Exec(`DELETE FROM audit_entries WHERE seq = (SELECT max(seq) FROM audit_entries)`); err != nil {
					t.Fatal(err)
				}
			}
			var buf bytes.Buffer
			out := log.Writer()
			t.Cleanup(func() { log.SetOutput(out) })
			log.SetOutput(&buf)
			core.reportAuditChain(context.Background())
			log.SetOutput(out)

			got := buf.String()
			if !strings.Contains(got, tc.want) {
				t.Errorf("状态里应含 %s，实际 %q", tc.want, got)
			}
			// 没挣到 VERIFIED_LOCAL 的档里，连那个词都不许出现：一句"不得报 X"和一条 X
			// 在 grep / UI 抓日志时长得一样，所以否认句里也不写。
			if tc.want != string(audit.StatusVerifiedLocal) && strings.Contains(got, string(audit.StatusVerifiedLocal)) {
				t.Errorf("虚报了校验通过（缺锚点/没校验过/链落后都不许）: %q", got)
			}
			if tc.want == string(audit.StatusVerifiedLocal) && !strings.Contains(got, "不在锚点覆盖内") {
				t.Errorf("VERIFIED_LOCAL 必须连着它的上限一起说: %q", got)
			}
			low := strings.ToLower(got)
			for _, claim := range []string{"tamper", "signed", "防篡改", "已签名", "已校验"} {
				if strings.Contains(low, strings.ToLower(claim)) {
					t.Errorf("出现了 ADR-018 禁止的措辞 %q: %q", claim, got)
				}
			}
		})
	}
}

// TestAuditEntryColumnsMatchSchema 是我在 cmd/core 读 audit_entries 的唯一护栏：
// 列清单来自 000001 迁移（共享的 schema 真相），但表一旦加列/改名，我的 Scan 就会静默少读一列 ——
// 少读一列的后果是 VerifyChain 重算不出同一个 entry_hash（fail closed，不会误报通过），
// 但那等于链校验从此永远说"不"，谁都不会读日志去追根因。所以让它在测试里就响。
func TestAuditEntryColumnsMatchSchema(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()

	rows, err := core.db.Read.Query(`SELECT name FROM pragma_table_info('audit_entries') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != strings.Join(auditEntryColumns, ",") {
		t.Fatalf("audit_entries 的列与我读的清单不一致（改表时要同步 cmd/core/auditchain.go）：\n  库里 = %v\n  读的 = %v", got, auditEntryColumns)
	}
}

// makeTaskRow 用真 Service 建一条 Task（CREATED、版本 1、一行 task_created），走的是交付用的写入口。
func makeTaskRow(t *testing.T, c *Core, id string) {
	t.Helper()
	ctx := context.Background()
	if err := c.store.Write(ctx, func(tx *db.Tx) error {
		_, err := c.tasks.Create(ctx, tx, task.NewTask{
			ID: id, Title: "§5.4 走一遍", Priority: "NORMAL", EventID: "evc-" + id, PrincipalID: "system",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// driveRejection 走一遍真生产路径：task.Service.ApplyExecutionEvent → 投影被拒 →
// reporter 在同一个事务里写 Notification + Audit（§5.4 的两条记录）。
func driveRejection(t *testing.T, c *Core, taskID, nonce string) error {
	t.Helper()
	ctx := context.Background()
	return c.store.Write(ctx, func(tx *db.Tx) error {
		_, err := c.tasks.ApplyExecutionEvent(ctx, tx, taskID, task.EdgeApprovalResolved, false, false, task.Command{
			EventID: "ev-" + nonce, PrincipalID: "system", CorrelationID: "cor-" + nonce,
			Notification: "ntf-" + nonce, RejEvent: "ncev-" + nonce, RejAudit: "aud-" + nonce,
		})
		return err
	})
}

// TestAnchorAdvancesOnlyAfterCommit 是 T-79 判据的两半：
// ① 计数器只在**事务提交成功之后**前进 —— 回滚的那笔一行都不该算（提交前推进会让锚点领先库内状态，
//
//	崩溃后一次普通回滚会被读成"篡改"，误报比不报更糟）；
//
// ② 连续 append 跨过 `CheckpointEveryAppends` 时锚点必须多出新一行，内容就是刚提交那一行的 (seq, entry_hash)。
// 阈值不做成旋钮（ADR-018 明令），而 audit 已把"跨阈值"从**调用次数**改成 **seq 差**（A18-3 的幂等改判），
// 所以 ② 只能用真行把链推到边界前一格再跨过去 —— 那 1000 行是这笔用例的代价，不是可以糊过去的常数。
func TestAnchorAdvancesOnlyAfterCommit(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	ctx := context.Background()

	makeTaskRow(t, core, "t-rej")
	if err := driveRejection(t, core, "t-rej", "a"); err != nil {
		t.Fatalf("被拒投影本身该提交成功（§5.4 只是记账）: %v", err)
	}
	if n := core.checkpoints.Counted(); n != 1 {
		t.Fatalf("提交成功后计数器应是 1，实际 %d", n)
	}
	if _, err := os.Stat(core.anchor.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("才 1 行就写锚点？阈值是 %d", audit.CheckpointEveryAppends)
	}
	// 被拒的 Task 不该动：一次拒绝改的是 audit/notification，不是 Task 版本。
	var status string
	var version int64
	if err := core.db.Read.QueryRow(`SELECT status, version FROM tasks WHERE id='t-rej'`).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "CREATED" || version != 1 {
		t.Errorf("§5.4 的拒绝对 Task 该是零新增版本零事件，实际 %s v%d", status, version)
	}

	// 回滚那一笔：reporter 已经在事务里写了 Notification + Audit，整笔被外层错误带回滚 → 不该计数。
	before := core.checkpoints.Counted()
	err = core.store.Write(ctx, func(tx *db.Tx) error {
		if _, e := core.tasks.ApplyExecutionEvent(ctx, tx, "t-rej", task.EdgeApprovalResolved, false, false, task.Command{
			EventID: "ev-rb", PrincipalID: "system",
			Notification: "ntf-rb", RejEvent: "ncev-rb", RejAudit: "aud-rb",
		}); e != nil {
			return e
		}
		return errors.New("外层故意回滚")
	})
	if err == nil {
		t.Fatal("这笔写本该失败")
	}
	if n := core.checkpoints.Counted(); n != before {
		t.Errorf("回滚的那笔也被计数了：%d → %d", before, n)
	}
	// 回滚后那些 id 必须仍是"没用过"的：拿同一批 id 重来一次要能成功（幂等重试的前提）。
	if err := driveRejectionRollbackIds(t, core); err != nil {
		t.Errorf("被回滚的 id 没腾出来：%v", err)
	}

	// 跨阈值这一格现在数的是 **seq 差**（audit 按 A18-3 改判，`n` 退化成建议值），
	// 所以用真行把它推过去，而不是拿调用次数糊：先推到边界前一格，再补最后一行。
	owed := audit.CheckpointEveryAppends - core.checkpoints.Counted()
	if err := appendAuditThroughStore(core, owed-1); err != nil {
		t.Fatal(err)
	}
	if n := core.checkpoints.Counted(); n != audit.CheckpointEveryAppends-1 {
		t.Fatalf("推到边界前一格之后 Counted() = %d，应为 %d", n, audit.CheckpointEveryAppends-1)
	}
	if _, err := os.Stat(core.anchor.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("差 1 就跨过阈值，这时候还不该有锚点")
	}
	if err := appendAuditThroughStore(core, 1); err != nil {
		t.Fatal(err)
	}
	lines := anchorLines(t, core.anchor.Path)
	if len(lines) != 1 {
		t.Fatalf("跨过阈值后应有一行锚点，实际 %d 行: %q", len(lines), lines)
	}
	wantSeq, wantHash := auditHeadRow(t, core)
	if !strings.HasPrefix(lines[0], strconv.FormatInt(wantSeq, 10)+"\t"+wantHash+"\t") {
		t.Errorf("锚点不是刚提交那一行的 (seq, entry_hash)：\n  锚点 = %q\n  库里 = %d %s", lines[0], wantSeq, wantHash)
	}
	if n := core.checkpoints.Counted(); n != 0 {
		t.Errorf("那一行锚点没把这一段覆盖掉，还欠 %d", n)
	}
}

// driveRejectionRollbackIds 用刚才那笔被回滚事务里的同一批 id 再记一次：
// 成功就说明回滚真的把行拿掉了（重试复用同一个 id 是 §8 幂等的要求）。
func driveRejectionRollbackIds(t *testing.T, c *Core) error {
	t.Helper()
	ctx := context.Background()
	return c.store.Write(ctx, func(tx *db.Tx) error {
		_, err := c.tasks.ApplyExecutionEvent(ctx, tx, "t-rej", task.EdgeApprovalResolved, false, false, task.Command{
			EventID: "ev-rb2", PrincipalID: "system",
			Notification: "ntf-rb", RejEvent: "ncev-rb", RejAudit: "aud-rb",
		})
		return err
	})
}

// TestSealCoversTheBytesThatLand 钉内部契约 §2.2.1 那条不变量在**我这侧的装配**上成立：
// §5.4 的审计行由我造 `audit.Chain`，所以"参与 entry_hash 的字节必须与落库的字节逐字节相同"只有我能保证。
// 今天 `rejections.go` 那条链**没有注** `Scrub`（T-109 被主控压在 db 的 T-107 之后，14:25），
// 所以这条用例在链重算不出来时自我让路成 SKIP —— 那不是"测试失败"，那是**缺陷形状的实测**
// （14:51:21：VerifyChain 报 `seq 1 entry_hash is 9823935a… recomputed 4d4aff22…`）。
// T-109 那一行注上之后 SKIP 自动消失，它就变成一道真闸门。**别把它改成永远绿的断言**，
// 也别因为它是 SKIP 就当 §2.2.1 在我这侧已收口。
func TestSealCoversTheBytesThatLand(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	ctx := context.Background()

	const secret = "sk-abcdefghijklmnop0123456789" // 与 audit.Rules() 的形状规则同形
	makeTaskRow(t, core, "t-scrub")
	if err := core.store.Write(ctx, func(tx *db.Tx) error {
		return core.reporter.Report(ctx, tx, task.RejectionReport{
			Rejection: task.Rejection{TaskState: task.Created, ExecutionEvent: "execution_completed",
				Reason: "用户粘贴的内容里有个密钥 " + secret},
			TaskID: "t-scrub", NotificationID: "ntf-scrub", EventID: "ev-scrub",
			AuditID: "aud-scrub", PrincipalID: "system", CorrelationID: "c-scrub", CausationID: "c-scrub",
		})
	}); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := core.db.Read.QueryRow(`SELECT details_json FROM audit_entries WHERE audit_id = 'aud-scrub'`).
		Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, secret) {
		t.Errorf("密钥原样落进了审计行：审计面板一旦公开就是泄露（强制点在 db，规则在 audit，但行是我写的）")
	}
	// 自破坏探针：夹具要是命中不了脱敏规则，这条用例就只是空转，必须当场喊出来。
	if !strings.Contains(stored, audit.Rules().Marker) {
		t.Fatalf("落库的 details 里没有脱敏标记，这条用例没有验到任何东西：marker=%q stored=%s", audit.Rules().Marker, stored)
	}
	entries, err := loadAuditEntries(ctx, core.db.Read)
	if err != nil {
		t.Fatal(err)
	}
	if verr := audit.VerifyChain(entries, audit.Head{}); verr != nil {
		// 主控要的"拿到消息之前别改实现"——失败分支就得把三个数一次打全：两个哈希 + 读到的那一行。
		for i, e := range entries {
			if i > 2 {
				break
			}
			t.Logf("第 %d 行：seq=%d audit_id=%s stored=%s recomputed-need-prev=%s details=%s",
				i, e.Seq, e.AuditID, head(e.EntryHash, 12), head(e.PreviousHash, 12), e.DetailsJSON)
		}
		t.Errorf("§2.2.1 的不变量破了：entry_hash 与落库字节不是同一串 —— %v", verr)
	}

	// 归因这一半也要钉住：密封前那次拿到的命中由**调用者**写成一行 redaction 审计行，
	// 否则"字节对了但归因静默缩水"（§2.2.1 第 1 条点名的正是这种"看起来正常"的失效）。
	var redaction *audit.Entry
	for i := range entries {
		if entries[i].Action == "redaction.execution_failed" {
			redaction = &entries[i]
		}
	}
	if redaction == nil {
		t.Fatalf("命中明明发生了（上面已验过 marker），却没写归因行，审计里就查不到是谁被脱敏：%q", actionsOf(entries))
	}
	if strings.Contains(redaction.DetailsJSON, secret) {
		t.Error("归因行里出现了原值：它只许记列名/规则名/计数（§2.2）")
	}
	// ① 两类命中都要在（我这条 §5.4 的 reason 走 pattern，通知体的 deny-key 走 key），
	// ② 同一列的命中计数必须是 1 而不是 2（§2.2.1 第 3 件：db 对 audit_entries 不再二记）。
	// 归因行是 JSON 列，就按 JSON 读 —— 拆字符串会把"我解析错了"误报成"归因丢了"。
	var record struct {
		Schema     int `json:"schema"`
		Redactions []struct {
			Column string `json:"column"`
			Kind   string `json:"kind"`
			Match  string `json:"match"`
			Count  int    `json:"count"`
		} `json:"redactions"`
	}
	if err := json.Unmarshal([]byte(redaction.DetailsJSON), &record); err != nil {
		t.Fatalf("归因行的 details_json 不是合法 JSON: %v\n%s", err, redaction.DetailsJSON)
	}
	if record.Schema != 1 {
		t.Errorf("归因行没带信封里的 \"schema\":1（§2）：%s", redaction.DetailsJSON)
	}
	byKind := map[string]int{}
	for _, h := range record.Redactions {
		byKind[h.Kind]++
		if h.Count != 1 {
			t.Errorf("列 %s 的命中计数是 %d，不是 1 —— 二记就是 §2.2.1 第 3 件要关的洞", h.Column, h.Count)
		}
		if h.Match == "" {
			t.Errorf("列 %s 的命中没写规则名，等于没归因", h.Column)
		}
	}
	// REQ-A11 的那一半：密封前那次在**审计行自己身上**命中的必须在这里，
	// 因为值一旦被换成 marker，事后谁都再也找不回来（db 二扫只认键）。
	own := false
	for _, h := range record.Redactions {
		if h.Column == "audit_entries.details_json" {
			own = true
		}
	}
	if !own {
		t.Errorf("归因行里没有 audit_entries.details_json 这一列 —— 密封前那次的命中被丢了：%s", redaction.DetailsJSON)
	}
	if byKind["pattern"] == 0 {
		t.Errorf("归因行里没有 pattern 命中（值驱动的命中一旦换成 marker 就再也找不回来）：%s", redaction.DetailsJSON)
	}
	// key 命中这里**不该**出现：deny-key 是按列名判的（`details_json`/`body` 不在 deny 列里），
	// 所以这条路径的归因只会有 pattern 一类。真要钉 key 命中得换一个 deny 列，那是 audit 的用例。
	if redaction.PreviousHash == "" {
		t.Error("归因行没接在 §5.4 那行后面（丢了前序哈希，就不是一条链了）")
	}
	t.Logf("链共 %d 行，归因行 kinds=%v 共 %d 条命中", len(entries), byKind, len(record.Redactions))
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func actionsOf(entries []audit.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Action)
	}
	return out
}

// TestRedactionRowIsWrittenOncePerRejection 钉主控①要的幂等断言：同一条 rejection 重放两次，
// 主行与归因行各自仍只有 1 行、第二笔整体回滚。
// **别拿它当"audit_id 必须派生"的证据**：我把派生 id 换成非派生形状做过变异，这条照样绿 ——
// 第二笔在更早的 notification id 上就撞了 UNIQUE，挡重放的是 §8 的"整批 id 复用"，不是这里的键。
// 它真正有用的反向对照是最后那半段：Reason 里没有可脱敏内容时**不许**写归因行（空行是链上噪音）。
func TestRedactionRowIsWrittenOncePerRejection(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	ctx := context.Background()
	makeTaskRow(t, core, "t-idem")

	same := func() task.RejectionReport {
		return task.RejectionReport{
			Rejection: task.Rejection{TaskState: task.Created, ExecutionEvent: "execution_completed",
				Reason: "矩阵外边沿；用户原文里有个 sk-abcdefghijklmnop0123456789"},
			TaskID:         "t-idem",
			NotificationID: "ntf-idem",
			EventID:        "ev-idem",
			AuditID:        "aud-idem",
			PrincipalID:    "system", CorrelationID: "c-idem", CausationID: "c-idem",
		}
	}
	if err := core.store.Write(ctx, func(tx *db.Tx) error { return core.reporter.Report(ctx, tx, same()) }); err != nil {
		t.Fatalf("第一笔 §5.4 该提交成功: %v", err)
	}
	second := core.store.Write(ctx, func(tx *db.Tx) error { return core.reporter.Report(ctx, tx, same()) })
	if second == nil {
		t.Error("重放同一批 id 居然提交成功了 —— 派生 id 没起到「靠 UNIQUE 挡重放」的作用，归因行数迟早会虚高")
	}
	if n := countAuditAction(t, core, "redaction.execution_failed"); n != 1 {
		t.Errorf("归因行应有 1 行，实际 %d 行（重放的那笔必须整体回滚）", n)
	}
	if n := countAuditAction(t, core, "execution_failed"); n != 1 {
		t.Errorf("§5.4 主行应有 1 行，实际 %d 行", n)
	}
	if n := countRowsBy(t, core, `SELECT count(*) FROM notifications WHERE id = 'ntf-idem'`); n != 1 {
		t.Errorf("通知应有 1 行，实际 %d 行", n)
	}
	// 反向对照：Reason 里没有可脱敏内容时，RecordRedaction 必须**不写行**（空归因行是噪音，§2.2）。
	if err := core.store.Write(context.Background(), func(tx *db.Tx) error {
		rec := same()
		rec.TaskID, rec.NotificationID, rec.EventID, rec.AuditID = "t-idem", "ntf-benign", "ev-benign", "aud-benign"
		rec.Reason = "纯矩阵文案"
		return core.reporter.Report(context.Background(), tx, rec)
	}); err != nil {
		t.Fatal(err)
	}
	if n := countAuditAction(t, core, "redaction.execution_failed"); n != 1 {
		t.Errorf("没有命中的那笔不该写归因行，实际共 %d 行", n)
	}
}

// TestTheTwoRedactionHitShapesStayInStep 是主控②要的那条交叉一致性用例（test 规矩 4 那把尺子）：
// `db.RedactionHit` 与 `audit.RedactionHit` 是 §0 依赖方向逼出来的两份同形结构，
// 我这里靠搬运同步 —— 所以**任一侧单独漂（改名、加字段、换 Kind 词表）都必须报红**，不能靠注释求同步。
func TestTheTwoRedactionHitShapesStayInStep(t *testing.T) {
	// 形状级：字段名与字段种类逐个对，多一个少一个都算漂。
	dbType, auditType := reflect.TypeOf(db.RedactionHit{}), reflect.TypeOf(audit.RedactionHit{})
	if dbType.NumField() != auditType.NumField() {
		t.Fatalf("两份命中结构的字段数已经不同：db %d 个、audit %d 个", dbType.NumField(), auditType.NumField())
	}
	for i := 0; i < dbType.NumField(); i++ {
		a, b := dbType.Field(i), auditType.Field(i)
		if a.Name != b.Name || a.Type.Kind() != b.Type.Kind() {
			t.Errorf("第 %d 个字段漂了：db.%s(%s) vs audit.%s(%s)", i, a.Name, a.Type.Kind(), b.Name, b.Type.Kind())
		}
	}

	// 值级：同一批输入喂给两侧，搬运必须一字不差。
	cols := []string{"details_json", "body", "pid"}
	args := []any{`{"note":"sk-abcdefghijklmnop0123456789"}`, "token=ghp_AAAAAAAAAAAA", int64(7)}
	values, hits, err := db.Scrub("audit_entries", cols, args)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatalf("这批输入一个命中都没有 ⇒ 下面所有断言都是空转（规则或夹具漂了）")
	}
	mapped := toAuditHits(hits)
	if len(mapped) != len(hits) {
		t.Fatalf("搬运丢了命中：%d → %d", len(hits), len(mapped))
	}
	for i, h := range hits {
		m := mapped[i]
		if h.Column != m.Column || h.Match != m.Match || h.Count != m.Count || string(h.Kind) != string(m.Kind) {
			t.Errorf("第 %d 条命中搬样不保真：db=%+v audit=%+v", i, h, m)
		}
	}
	for i, c := range cols {
		if c == "pid" && values[i] != int64(7) {
			t.Errorf("非文本列被改了：pid=%v", values[i])
		}
	}
}

func countAuditAction(t *testing.T, c *Core, action string) int {
	t.Helper()
	var n int
	if err := c.db.Read.QueryRow(`SELECT count(*) FROM audit_entries WHERE action = ?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countRowsBy(t *testing.T, c *Core, q string) int {
	t.Helper()
	var n int
	if err := c.db.Read.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestWiredAnchorCounterIsAttached 钉 T-64① 里我能证的那一半：计数器建出来了、锚点路径在数据目录下、
// **并且我这条腿的交账真的喂进了它** —— 走 `auditedStore` 的那笔让 `Counted()` 追上真链头，
// 绕过它的那笔不让。阈值落盘那一半在 TestAnchorAdvancesOnlyAfterCommit，两半各证各的。
// "counter 挂进了 approval store" 仍然证不了（`SQLiteStore.cp` 未导出，唯一外部效果要写满 1000 条审批）
// —— 已提 REQ-A-4 请 approval 导一个只读确认口。
// 最后那对 Advance/Counted 断言把 A18-3 争的点变成了行为级的：audit 改成按 seq 差计账之后，
// 同一个链头报两次不改账 —— "两条腿双计"在这个口径下结构上不成立，不必再靠"只有一个调用点"来保证。
func TestWiredAnchorCounterIsAttached(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	if core.checkpoints == nil || core.approvals == nil {
		t.Fatal("① 没接：counter 或 approval store 是 nil")
	}
	if core.anchor.Path != filepath.Join(core.cfg.DataDir, "audit.head") {
		t.Error("counter 指向的锚点不在数据目录下")
	}
	// "counter 挂进了 store"证不了：SQLiteStore.cp 是未导出字段，唯一可观察效果是
	// 跨过一次 CheckpointEveryAppends 才落一行锚点 —— 那要写 1000 条审批、或经 approval.Service
	// 造真审批，都是别人的竖线。已提 REQ-A-4 请 approval 导一个只读确认口。
	// 能直接证的是我建的那一段：counter → 锚点文件。
	appendAuditRows(t, core, 1)
	if n := core.checkpoints.Counted(); n != 0 {
		t.Errorf("绕过 auditedStore 的一笔被算进我的交账了：%d —— 交账该挂在\"我这笔提交之后\"，不是\"库里多了行\"", n)
	}
	// 走我的腿一笔：audit 改成按 seq 差计数之后，交账的效果就是 Counted() 追上真链头。
	if err := appendAuditThroughStore(core, 1); err != nil {
		t.Fatal(err)
	}
	seq, hash := auditHeadRow(t, core)
	if n := core.checkpoints.Counted(); n != int(seq) {
		t.Errorf("交账后 Counted() = %d，应等于链头 seq %d", n, seq)
	}
	// A18-3 争的"两条腿双计"在这个口径下不成立：同一个 head 再报一次不改账。
	// 这条断言是行为级的，比"数有几个 Advance( 调用点"那种 grep 判据结实，所以留在这里。
	if err := core.checkpoints.Advance(1, audit.Head{Seq: seq, Hash: hash}); err != nil {
		t.Fatal(err)
	}
	if n := core.checkpoints.Counted(); n != int(seq) {
		t.Errorf("重复报同一段被当成了新增：%d → %d", seq, n)
	}
	if _, err := os.Stat(core.anchor.Path); !errors.Is(err, os.ErrNotExist) {
		t.Error("没跨阈值就写了锚点文件")
	}
}

// TestAnchorPathFollowsTheDataDir 钉住 T-37 的那一条约定：cmd/core 只做一件事 —— 把 --data-dir
// 交给锚点。换数据目录就必须换锚点位置，否则删掉数据目录还会留下孤儿检查点。
func TestAnchorPathFollowsTheDataDir(t *testing.T) {
	cfg := testConfig(t)
	cfg.DataDir = filepath.Join(cfg.DataDir, "深一层", "data")
	core, err := boot(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	if core.anchor == nil || core.anchor.Path != filepath.Join(cfg.DataDir, "audit.head") {
		t.Fatalf("锚点没跟着 --data-dir 走: %+v", core.anchor)
	}
}
