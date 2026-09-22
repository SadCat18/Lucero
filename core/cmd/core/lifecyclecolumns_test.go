package main

// REQ-T12 那五条行级判据（`exit_code` / `restart_policy` / `owner_type` / `metadata_json` / `status`）
// 今天在整个仓库里的**覆盖是 0**：巡检 03:21 量的（`cmd/core` 测试真读 `process_instances` 只有 1 处、
// 断的还是 `count=0`），我在 03:4x 复量一致（非测试码里 `process_instances` 只出现在注释里，零写调用）。
//
// 所以这个文件不能假装"已经把列断言写完了"—— 那张一行都落不了库：
// `process_instances` 既不在 `aggTable` 里、也没有 `AggProcess`（`internal/process/supervisor.go:164~176`
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
	"strings"
	"sync"
	"testing"
	"time"
)

// lifecycleRow 是 §8/§14 要求 `process_instances` 为一台 ADAPTER 进程记下的那半行 ——
// 半行是因为另外那半（pid / started_at / exited_at / exit_code）来自事件，而这半是**装配期事实**
// （`supervisor.go:178~190` 那句"the injector is built next to the Supervisor … so it holds the policy and the owner"）。
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

// TestLifecycleWriterAbsentButShoutPresent 把今天的事实钉成一条：真起一个子进程（装配过的 launcher），
// recorder 那一格**真出声**，而 `process_instances` **一行都没有** —— 两半要一起断，
// 因为只断 `count=0` 的话，把 `Rec` 摘掉也照样绿（第 ② 批那条挂桥就白做了）。
func TestLifecycleWriterAbsentButShoutPresent(t *testing.T) {
	core, err := boot(t.Context(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()

	var mu sync.Mutex
	var lines []string
	l := newLauncher(func(string) (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperPipeChild$")
		cmd.Env = append(os.Environ(), pipeHelperEnv+"=exit")
		return cmd, nil
	}, 5*time.Second, func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}, core.store, func() int64 { return time.Now().UnixMilli() })

	pipe, err := l.Start(context.Background(), "fake")
	if err != nil {
		t.Fatalf("起子进程: %v", err)
	}
	if err := pipe.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	mu.Lock()
	joined := strings.Join(lines, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "action=started") {
		t.Fatalf("生命周期那一格该真出过声（否则下面那句零行只是「没人尝试过」）:\n%s", joined)
	}
	if n := countProcessInstances(t, core); n != 0 {
		t.Errorf("REQ-DB-2 落地前这里该是 0 行，实际 %d 行（有人绕道写了吗？把这条改成正向断言并关掉 SKIP）", n)
	}
	// 五列的读面今天就得存在：按列名 SELECT 一次，列被改名/删掉会在这里响，而不是在合验那天沉默。
	var dead int
	if err := core.db.Read.QueryRow(
		`SELECT count(*) FROM process_instances WHERE exit_code IS NOT NULL OR restart_policy <> 'NEVER'
		   OR owner_type IS NOT NULL OR metadata_json <> '{}' OR status <> 'UNKNOWN'`).Scan(&dead); err != nil {
		t.Fatalf("五列的读面本身读不动（列名漂了？）: %v", err)
	}
}

func countProcessInstances(t *testing.T, core *Core) int64 {
	t.Helper()
	var n int64
	if err := core.db.Read.QueryRow(`SELECT count(*) FROM process_instances`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestLifecycleRowLandsTheFiveColumns 是**升格格**：REQ-DB-2 那条通道（外加 C34 的 owner_type/owner_id 词表）
// 一落地，`LUCERO_REQ_DB2=1` 就该跑起来并断真行的列值。今天它显式 SKIP，并且把"等谁"写进理由里 ——
// 判据表引用它时不许把 SKIP 当绿。
func TestLifecycleRowLandsTheFiveColumns(t *testing.T) {
	if os.Getenv("LUCERO_REQ_DB2") != "1" {
		t.Skip("等 REQ-DB-2（无版本、但要发事件的表的写通道）+ C34（owner_type/owner_id 词表）：" +
			"设 LUCERO_REQ_DB2=1 才跑；今天生命周期那三格只出声不写库（T-168）")
	}
	// 走到这里说明通道已经存在 —— 那时把这段换成真的一次 supervisor 生命周期 + assertLifecycleRow(真行, wantAdapterRow())。
	t.Fatal("LUCERO_REQ_DB2=1 但这段还没接上真写路径：等 REQ-DB-2 落地的人把它补完（不许改成 SKIP 蒙过去）")
}
