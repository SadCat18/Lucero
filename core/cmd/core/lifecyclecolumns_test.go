package main

// REQ-T12 那五条行级判据（`exit_code` / `restart_policy` / `owner_type` / `metadata_json` / `status`）
// 今天在整个仓库里的**覆盖是 0**：巡检 03:21 量的（`cmd/core` 测试真读 `process_instances` 只有 1 处、
// 断的还是 `count=0`），我在 03:4x 复量一致（非测试码里 `process_instances` 只出现在注释里，零写调用）。
//
// 所以这个文件不能假装"已经把列断言写完了"—— 那张一行都落不了库：
// `process_instances` 既不在 `aggTable` 里、也没有 `AggProcess`（`internal/process` 的 `Supervisor` 那段注释
// 那段就是量出来的），`Touch`/`Create` 表达不了它；而 `Append` 不写事件，拿它建 `process_started` 那一行
// 就是 T-168 收回 T-162① 时判掉的"行写了、事件静默丢掉"。真正的通道挂在 **REQ-DB-2** 上（外加 **C34** 的词表）。
//
// 那我能做什么、且只有我能做（`adapter` import 不到 `package main`，合验替我写不了）：
//   ① 把"这一行应该长什么样"写成**一张表 + 一个比较函数**，并用**合成反例**证明它六个字段每个都会响
//      （不是装饰：合验升格成断真行的时候，用的就是同一个 `assertLifecycleRow`）；
//   ② 把今天的事实钉住：真起一个子进程、recorder 真出声、库里**零行**（这条比原来那句 `count=0` 多的正是"出声"那一半
//      —— 只有 `count=0` 的话，`Rec` 摘掉也照样绿，那就是"绿得没内容"）；
//   ③ 留一个**显式开关**的升格格（`LUCERO_REQ_DB2=1`）：通道一落地，那条就真跑列值断言；今天它**显式 SKIP 并写明等谁**，
//      不许静默消失（同 T84-7b 那条"挂阻塞条件而不是不写"的口径）。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/process"
)

// lifecycleRow 是 §8/§14 要求 `process_instances` 为一台 ADAPTER 进程记下的那半行 ——
// 半行是因为另外那半（pid / started_at / exited_at / exit_code）来自事件，而这半是**装配期事实**
// （`internal/process` 的 `Supervisor` 那句"the injector is built next to the Supervisor … so it holds the policy and the owner"）。
// 字段名逐字对着 000001 的列名，别在这里发明第六列。
type lifecycleRow struct {
	processType   string
	ownerType     string
	ownerID       string
	pid           int64
	status        string
	restartPolicy string
	exitCode      int64
	hasExitCode   bool
	metadata      map[string]any
	executable    string
	hasExecutable bool
	createdAt     int64
	updatedAt     int64
}

// assertLifecycleRow 只比**这五个被点名的列**加上 pid/process_type —— 返回不匹配清单（空 = 全对）。
// 刻意不返回 bool：调用方要能看见是哪一列漂了，"绿"要能解释自己为什么绿。
func assertLifecycleRow(got, want lifecycleRow) []string {
	bad := []string{}
	cmp := func(col string, g, w any) {
		if g != w {
			bad = append(bad, fmt.Sprintf("%s: 库里 %v，期望 %v", col, g, w))
		}
	}
	cmp("process_type", got.processType, want.processType)
	cmp("owner_type", got.ownerType, want.ownerType)
	cmp("status", got.status, want.status)
	cmp("restart_policy", got.restartPolicy, want.restartPolicy)
	cmp("pid", got.pid, want.pid)
	// exit_code 是可空的：NULL 与 0 是两件事（0 = 干净退出，NULL = 还没退），这里分开比。
	if got.hasExitCode != want.hasExitCode {
		bad = append(bad, fmt.Sprintf("exit_code:  presence %v，期望 %v", got.hasExitCode, want.hasExitCode))
	} else if got.hasExitCode && want.hasExitCode {
		cmp("exit_code", got.exitCode, want.exitCode)
	}
	// metadata_json 比**键集合与取值**（§8 的 Crashes 计数就住在这里，没有独立列）。
	gm, _ := json.Marshal(got.metadata)
	wm, _ := json.Marshal(want.metadata)
	if string(gm) != string(wm) {
		bad = append(bad, fmt.Sprintf("metadata_json: %s，期望 %s", gm, wm))
	}
	return bad
}

// readLifecycleRow 把那一行按列读出来。列名写死在这里 = 这五个名字与表的对应关系被机器盯住
// （改列名或删列，这条先编译期/运行期响，而不是让合验安静地读不到东西）。
func readLifecycleRow(t *testing.T, dbh *sql.DB, id string) (lifecycleRow, bool) {
	t.Helper()
	var (
		r                                              lifecycleRow
		ownerType, executable, metadata                sql.NullString
		pid, exitCode, startedAt, createdAt, updatedAt sql.NullInt64
	)
	err := dbh.QueryRow(
		`SELECT process_type, owner_type, owner_id, pid, status, restart_policy,
		        exit_code, executable, metadata_json, created_at, updated_at, heartbeat_at
		 FROM process_instances WHERE id = ?`, id).
		Scan(&r.processType, &ownerType, &r.ownerID, &pid, &r.status, &r.restartPolicy,
			&exitCode, &executable, &metadata, &createdAt, &updatedAt, &sql.NullInt64{})
	if errors.Is(err, sql.ErrNoRows) {
		return lifecycleRow{}, false
	}
	if err != nil {
		t.Fatalf("读 process_instances 那一行: %v", err)
	}
	r.ownerType = ownerType.String
	r.executable = executable.String
	r.hasExecutable = executable.Valid
	r.pid = pid.Int64
	r.hasExitCode = exitCode.Valid
	r.exitCode = exitCode.Int64
	r.createdAt = createdAt.Int64
	r.updatedAt = updatedAt.Int64
	_ = startedAt
	r.metadata = map[string]any{}
	if metadata.Valid {
		if err := json.Unmarshal([]byte(metadata.String), &r.metadata); err != nil {
			t.Fatalf("metadata_json 不是合法 JSON（列上有 json_valid 的 CHECK，这里红说明绕过了它）: %v", err)
		}
	}
	return r, true
}

func insertLifecycleRow(t *testing.T, dbh *sql.DB, id string, r lifecycleRow) {
	t.Helper()
	meta, err := json.Marshal(r.metadata)
	if err != nil {
		t.Fatal(err)
	}
	var exit any
	if r.hasExitCode {
		exit = r.exitCode
	}
	var exec any
	if r.hasExecutable {
		exec = r.executable
	}
	res, err := dbh.Exec(
		`INSERT INTO process_instances (id, process_type, owner_type, owner_id, pid, executable, status,
		    restart_policy, started_at, exited_at, exit_code, metadata_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, r.processType, nullableStr(r.ownerType), nullableStr(r.ownerID), r.pid, exec, r.status,
		r.restartPolicy, r.createdAt, r.updatedAt, exit, string(meta), r.createdAt, r.updatedAt)
	if err != nil {
		t.Fatalf("夹具插一行 process_instances 都插不进（列名/CHECK/STRICT 类型对不上）: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("夹具该恰好插一行，实际 %d", n)
	}
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func wantAdapterRow() lifecycleRow {
	return lifecycleRow{
		processType: "ADAPTER", ownerType: "adapter", ownerID: "fake",
		pid: 4242, status: "RUNNING", restartPolicy: "NEVER",
		metadata: map[string]any{"schema": float64(1), "adapter_key": "fake", "crashes": float64(0)},
	}
}

// TestLifecycleColumnAssertionsHaveTeeth 是这个文件的**正向对照**：
// 五个被点名的列各造一个"库里是错的"那一行，`assertLifecycleRow` 必须逐条报出不匹配。
// 为什么能在今天做：这一段插的是**夹具行**（`_test.go` 里的跨包写按 §1.3 合法），
// 它不假装生产路径能落库 —— 它只证明"等通道落地时，这五条断言不是摆设"。
func TestLifecycleColumnAssertionsHaveTeeth(t *testing.T) {
	core, err := boot(t.Context(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	dbh := core.db.Read

	base := wantAdapterRow()
	base.createdAt, base.updatedAt = 100, 100
	insertLifecycleRow(t, dbh, "pi-good", base)
	got, ok := readLifecycleRow(t, dbh, "pi-good")
	if !ok {
		t.Fatal("夹具那一行没读回来")
	}
	if bad := assertLifecycleRow(got, base); len(bad) != 0 {
		t.Fatalf("一模一样的一行该全对，实际报出: %v", bad)
	}

	// 逐列造反例：每一列都要被点名，少一列就是那条判据是空的。
	cases := []struct {
		col string
		mut func(*lifecycleRow)
	}{
		{"status", func(r *lifecycleRow) { r.status = "UNKNOWN" }},
		{"restart_policy", func(r *lifecycleRow) { r.restartPolicy = "ALWAYS" }},
		{"owner_type", func(r *lifecycleRow) { r.ownerType = "" }},
		{"exit_code", func(r *lifecycleRow) { r.hasExitCode = true; r.exitCode = 1 }},
		{"metadata_json", func(r *lifecycleRow) { r.metadata = map[string]any{"schema": float64(2)} }},
		// process_type 与 pid 不在那五条里，但它们同样是 §8 明写的列，一起钉。
		{"process_type", func(r *lifecycleRow) { r.processType = "WORKER" }},
		{"pid", func(r *lifecycleRow) { r.pid = 1 }},
	}
	for i, tc := range cases {
		id := fmt.Sprintf("pi-bad-%d", i)
		want := base
		mutated := base
		tc.mut(&mutated)
		// 期望写的是"对的那份"（want），库里插的是"错的那份"（mutated）⇒ 必须报出不匹配，且点到那一列。
		insertLifecycleRow(t, dbh, id, mutated)
		gotBad, ok := readLifecycleRow(t, dbh, id)
		if !ok {
			t.Fatalf("%s: 反例行没插进去", tc.col)
		}
		bad := assertLifecycleRow(gotBad, want)
		if len(bad) == 0 {
			t.Errorf("判据没牙：%s 被改坏了却没人报（这条就是巡检说的『绿得没内容』）", tc.col)
			continue
		}
		if !strings.Contains(strings.Join(bad, "\n"), tc.col) {
			t.Errorf("%s 的反例报出了别的列: %v", tc.col, bad)
		}
	}
}

// startOneAttempt 走**装配过的那条桥**起一次真子进程再关掉它（同一个 launcher、同一个写口），
// 返回那一路喊出来的日志行。为什么不能用夹具代替这一格：只有走真装配才证得到"Rec 挂在桥上 + 桥落得进行"。
//
// 注意 `launcher.Start` 不开读帧的泵（那是 `Connect` 的活），所以这里只会看到 STARTED 那一格 ——
// 断言里"恰好一行、恰好一条事件"就是这件事的正面证据（多出来的一行/事件说明有人在别处补了边）。
func startOneAttempt(t *testing.T, core *Core, key string) []string {
	t.Helper()
	var (
		mu    sync.Mutex
		lines []string
	)
	l := newLauncher(func(string) (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperPipeChild$")
		cmd.Env = append(os.Environ(), pipeHelperEnv+"=exit")
		return cmd, nil
	}, 5*time.Second, func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}, core.store, func() int64 { return time.Now().UnixMilli() })

	pipe, err := l.Start(context.Background(), key)
	if err != nil {
		t.Fatalf("起子进程: %v", err)
	}
	if err := pipe.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), lines...)
}

// onlyProcessRow 读回那一行（顺带断"就一行"）：一次尝试一行是 CO-45(a) 的裁定，两行就是有人重复建了。
func onlyProcessRow(t *testing.T, core *Core) (lifecycleRow, string) {
	t.Helper()
	var id string
	if err := core.db.Read.QueryRow(`SELECT id FROM process_instances`).Scan(&id); err != nil {
		t.Fatalf("`process_instances` 该恰好一行（现在 %d 行就读不到唯一那行）: %v",
			countProcessInstances(t, core), err)
	}
	r, ok := readLifecycleRow(t, core.db.Read, id)
	if !ok {
		t.Fatal("id 读得到但那一行读不回来")
	}
	return r, id
}

// pidFromShout 从 Sink 那行里取 pid —— 要的是**与库无关的一份观察**，
// 拿库里的 pid 去期望库里的 pid 就是自证。
func pidFromShout(t *testing.T, lines []string) int64 {
	t.Helper()
	for _, l := range lines {
		i := strings.Index(l, "kind=STARTED pid=")
		if i < 0 {
			continue
		}
		rest := l[i+len("kind=STARTED pid="):]
		j := strings.IndexByte(rest, ' ')
		if j < 0 {
			j = len(rest)
		}
		n, err := strconv.ParseInt(rest[:j], 10, 64)
		if err != nil || n <= 0 {
			t.Fatalf("Sink 那行的 pid 读不出/不合法: %q", l)
		}
		return n
	}
	t.Fatalf("日志里没有 STARTED 那一格，说明桥没出声:\n%s", strings.Join(lines, "\n"))
	return 0
}

// TestLifecycleWriterLandsRowAndShouts 是 `TestLifecycleWriterAbsentButShoutPresent` 的**正向换代**：
// 那条钉的是"REQ-DB-2 落地前一格都不许写"，而它自己的注释写着落地那天要改成正向断言（原文："把这条改成正向断言并关掉 SKIP"）。
// 今天通道到了，所以两半要一起成立才算数：**桥真出声**（把 `Rec` 摘掉就断在第一半）＋ **行真落库**（把写口摘掉就断在第二半）——
// 单断任何一半都能被"另一道 nil 闸给出同样答案"糊过去，这条就是防那个。
// 两支分别钉归属：库里还没有实例行 ⇒ owner 整个留空（probe 先起进程、后写实例行）；登记过 ⇒ owner_type/owner_id 跟着落。
func TestLifecycleWriterLandsRowAndShouts(t *testing.T) {
	t.Run("没登记实例行时 owner 整个留空", func(t *testing.T) {
		core, err := boot(t.Context(), testConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		defer core.close()

		lines := startOneAttempt(t, core, "fake")
		joined := strings.Join(lines, "\n")
		for _, want := range []string{"action=started", "生命周期已落"} {
			if !strings.Contains(joined, want) {
				t.Errorf("这一格该喊出 %q（否则下面那句零行只是「没人尝试过」）:\n%s", want, joined)
			}
		}
		got, _ := onlyProcessRow(t, core)
		if got.ownerType != "" || got.ownerID != "" {
			t.Errorf("没登记过实例行时 owner 该整个留空（半个归属比没有更坏），实际 %q/%q", got.ownerType, got.ownerID)
		}
		if n := countEvents(t, core); n != 1 {
			t.Errorf("一行该配恰一条事件，实际 %d 条", n)
		}
	})

	t.Run("登记过之后 owner 跟着落", func(t *testing.T) {
		core, err := boot(t.Context(), testConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		defer core.close()
		// 夹具行是测试侧写（§1.3 允许，与本包 `discoverassembly_test.go` 同一形状）：它模拟"这个 key 已经 probe 过一次"。
		if _, err := core.db.Read.Exec(
			`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
			 VALUES ('ai-owned', 'fake', 'fake', 1, 1)`); err != nil {
			t.Fatal(err)
		}
		startOneAttempt(t, core, "fake")
		got, _ := onlyProcessRow(t, core)
		if got.ownerType != "adapter_instance" || got.ownerID != "ai-owned" {
			t.Errorf("登记过实例行之后 owner 该跟着落（第 18 条：owner_id 是实例行的 id，不是 key），实际 %q/%q",
				got.ownerType, got.ownerID)
		}
	})

	t.Run("没有起手的那次退出：喊没落库，且不造行不造事件", func(t *testing.T) {
		core, err := boot(t.Context(), testConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		defer core.close()
		var (
			mu    sync.Mutex
			lines []string
		)
		lc, err := process.NewLifecycleStore(process.ProcessRow{Kind: process.KindAdapter, RestartPolicy: process.RestartNever},
			func() (string, error) { return "pi-no-start", nil }, func() int64 { return 7 }, nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := newDropRecorder(core.store, "codex", func() int64 { return 7 },
			func(f string, args ...any) {
				mu.Lock()
				defer mu.Unlock()
				lines = append(lines, fmt.Sprintf(f, args...))
			}, lc)
		// 写口回 `ErrNoLifecycleRow`（库里没有这个实例的行）：这一格**不是**失败被吞，更不是"补一条退出事件"。
		rec.RecordExit(process.Event{Kind: process.EventExited, PID: 99, ExitCode: 0, Status: process.StatusExited})
		mu.Lock()
		joined := strings.Join(lines, "\n")
		mu.Unlock()
		if !strings.Contains(joined, "没落库") {
			t.Fatalf("void 接口报不上去，至少要喊出\"没落库\"，实际:\n%s", joined)
		}
		if strings.Contains(joined, "生命周期已落") {
			t.Errorf("失败的那一次不许喊\"已落\":\n%s", joined)
		}
		if n := countProcessInstances(t, core); n != 0 {
			t.Errorf("查无此行的一格不该造出进程行，实际 %d 行", n)
		}
		if n := countEvents(t, core); n != 0 {
			t.Errorf("也不该造事件，实际 %d 条", n)
		}
	})
}

// TestLifecycleThreeLegsEachMoveTheRow 是契约① 第 17 条末段加严后的**三条腿正面**（T-253③）：
// 建行腿、改行腿（干净退出）、改行腿（崩溃）各自都要在**装配好的形状**下真驱动一次，
// 因为"整包绿"不等于每条腿有牙 —— 反例在 `db-mut51.py` 里（摘掉哪条腿，就必须红在对应那条用例上，且三次输出两两不同）。
//
// 用同一个 recorder 实例连做两次尝试：CO-45(a) 一次尝试一行，所以第二行不该被第一行的边牵动。
func TestLifecycleThreeLegsEachMoveTheRow(t *testing.T) {
	core, err := boot(t.Context(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	if _, err := core.db.Read.Exec(
		`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		 VALUES ('ai-three', 'fake', 'fake', 1, 1)`); err != nil {
		t.Fatal(err)
	}

	var (
		mu    sync.Mutex
		lines []string
	)
	l := newLauncher(pipeCommand("exit"), 5*time.Second, func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}, core.store, func() int64 { return time.Now().UnixMilli() })
	sup := l.supervisorFor("fake")
	rec, ok := sup.Rec.(*dropRecorder)
	if !ok || rec.lifecycle == nil {
		t.Fatalf("这一格要的是装配好的 recorder（握着写口），实际 %T", sup.Rec)
	}

	type attempt struct {
		exitCode int
		crash    bool
	}
	ids := make([]string, 0, 2)
	for i, a := range []attempt{{exitCode: 3}, {exitCode: 9, crash: true}} {
		child, err := sup.Start(t.Context())
		if err != nil {
			t.Fatalf("第 %d 次起子进程: %v", i+1, err)
		}
		ids = append(ids, rec.lifecycle.ID())
		if a.crash {
			rec.RecordCrash(process.Event{Kind: process.EventCrashed, PID: child.PID,
				Status: process.StatusFailed, ExitCode: a.exitCode, Crashes: 1})
		} else {
			rec.RecordExit(process.Event{Kind: process.EventExited, PID: child.PID,
				Status: process.StatusExited, ExitCode: a.exitCode})
		}
		if _, err := child.Wait(t.Context()); err != nil {
			t.Errorf("第 %d 次等子进程退出: %v", i+1, err)
		}
		child.CloseStreams()
	}
	if ids[0] == "" || ids[0] == ids[1] {
		t.Fatalf("两次尝试该是两行（CO-45(a)），实际 %q / %q", ids[0], ids[1])
	}

	first, _ := readLifecycleRow(t, core.db.Read, ids[0])
	if first.status != "EXITED" || !first.hasExitCode || first.exitCode != 3 {
		t.Errorf("干净那条腿该把行改成 EXITED/exit_code=3，实际 %s / 有=%v 值=%d", first.status, first.hasExitCode, first.exitCode)
	}
	if first.metadata["attempt"] != float64(1) {
		t.Errorf("第一行的 metadata.attempt 该是 1，实际 %v", first.metadata["attempt"])
	}
	second, _ := readLifecycleRow(t, core.db.Read, ids[1])
	if second.status != "FAILED" || !second.hasExitCode || second.exitCode != 9 {
		t.Errorf("崩溃那条腿该把第二行改成 FAILED/exit_code=9，实际 %s / 有=%v 值=%d", second.status, second.hasExitCode, second.exitCode)
	}
	if second.metadata["attempt"] != float64(2) || second.metadata["crashes"] != float64(1) {
		t.Errorf("第二行的 metadata 该记 attempt=2 与 crashes=1（§8 的重启预算就住在这里），实际 %v", second.metadata)
	}
	// `previous_instance_id` 是写口自己那一份 attempt 语义（`RecordProcessStart` 把 prev/cur 一起设成这一行），
	// 我这格不去替它定义含义 —— 只断两行各自带 metadata、边没串错行（下面按事件条数校）。
	for want, n := range map[string]int{"process_started": 2, "process_exited": 1, "process_crashed": 1} {
		if got := countEventsOfTypeProc(t, core, want); got != n {
			t.Errorf("%s 该 %d 条，实际 %d 条", want, n, got)
		}
	}
	mu.Lock()
	joined := strings.Join(lines, "\n")
	mu.Unlock()
	for _, id := range ids {
		if !strings.Contains(joined, id) {
			t.Errorf("两条腿的喊话里都没出现行 id %q（那说明写口没真被驱动）:\n%s", id, joined)
		}
	}
}

func countEventsOfTypeProc(t *testing.T, core *Core, evType string) int {
	t.Helper()
	var n int
	if err := core.db.Read.QueryRow(
		`SELECT count(*) FROM event_journal WHERE aggregate_type = 'process' AND event_type = ?`, evType).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countEvents(t *testing.T, core *Core) int {
	t.Helper()
	var n int
	if err := core.db.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE aggregate_type = 'process'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countProcessInstances(t *testing.T, core *Core) int64 {
	t.Helper()
	var n int64
	if err := core.db.Read.QueryRow(`SELECT count(*) FROM process_instances`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestLifecycleRowLandsTheFiveColumns 是 REQ-DB-2 的**升格格**：契约① §1.1 第 17 条把验收命令钉成
// `LUCERO_REQ_DB2=1 go test -run 'TestLifecycleRowLandsTheFiveColumns' ./cmd/core/` 必须 **PASS**（不是 SKIP、不是这用例里那句 `t.Fatal`）。
// 不带 flag 时它仍然显式 SKIP —— 这条不是偷懒：真子进程那一格的成本要付在验收那天，而 SKIP 本身带着"等谁"（判据表不许把 SKIP 当绿）。
//
// 断的是**真走一次 supervisor 生命周期**落下来的那一行：五个被点名的列 + pid/process_type 过 `assertLifecycleRow`，
// 外加"恰好一行、恰好一条 `process_started`"（CO-45(a) 的一次尝试一行 + 第 17 条的行与事件同生同死）。
func TestLifecycleRowLandsTheFiveColumns(t *testing.T) {
	if os.Getenv("LUCERO_REQ_DB2") != "1" {
		t.Skip("带 LUCERO_REQ_DB2=1 才跑（契约① §1.1 第 17 条的验收命令形状）；不带 flag 的默认跑里这格不算样本")
	}
	core, err := boot(t.Context(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	if _, err := core.db.Read.Exec(
		`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		 VALUES ('ai-req-db2', 'fake', 'fake', 1, 1)`); err != nil {
		t.Fatal(err)
	}

	lines := startOneAttempt(t, core, "fake")
	pid := pidFromShout(t, lines)
	got, rowID := onlyProcessRow(t, core)

	want := lifecycleRow{
		processType: "ADAPTER", ownerType: "adapter_instance", ownerID: "ai-req-db2",
		pid: pid, status: string(process.StatusStarting), restartPolicy: "NEVER",
		metadata: map[string]any{"attempt": float64(1), "pid": float64(pid), "previous_instance_id": ""},
	}
	if bad := assertLifecycleRow(got, want); len(bad) != 0 {
		t.Errorf("真走一次生命周期落的那一行不对:\n  %s", strings.Join(bad, "\n  "))
	}
	// executable 不在那五条里，但 §8 的明细点了它：起不来就没人知道起的是什么。
	if !got.hasExecutable || got.executable == "" {
		t.Errorf("executable 该是装配时那份可执行路径，实际有=%v 值=%q", got.hasExecutable, got.executable)
	}
	if got.createdAt == 0 || got.updatedAt == 0 {
		t.Errorf("created_at/updated_at 该是写口自己的时钟（不是事件时刻），实际 %d/%d", got.createdAt, got.updatedAt)
	}
	var evType, evAgg string
	if err := core.db.Read.QueryRow(
		`SELECT aggregate_type, event_type FROM event_journal WHERE aggregate_id = ?`, rowID).Scan(&evAgg, &evType); err != nil {
		t.Fatalf("那一行旁边该有恰一条事件: %v（现在 %d 条）", err, countEvents(t, core))
	}
	if evAgg != "process" || evType != "process_started" {
		t.Errorf("事件该是 process/process_started，实际 %s/%s", evAgg, evType)
	}
}
