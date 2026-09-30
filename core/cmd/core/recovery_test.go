package main

import (
	"bytes"
	"context"
	"log"
	"reflect"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
)

// TestBootWiresTheRecoveryScanEntry 钉 T-391/B 的接线本身：§2.3 里 Event Journal 之后那一格
// 必须真跑（恢复扫描是 Occasion=Recovery 的生产入口），并且把 CO-45③ 的活进程计数喊出来。
// 反例落点：摘掉 main.go 里 `core.runRecovery(ctx)` 那一行 ⇒ 下面两句都找不到，这条用例红。
func TestBootWiresTheRecoveryScanEntry(t *testing.T) {
	var buf bytes.Buffer
	out := log.Writer()
	t.Cleanup(func() { log.SetOutput(out) })
	log.SetOutput(&buf)

	core, err := boot(context.Background(), testConfig(t))
	log.SetOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()

	got := buf.String()
	for _, want := range []string{
		"恢复扫描（REQ-RC-1）",
		"活进程计数（CO-45③",
		// 新库没有在飞的 Execution ⇒ 这一句必须出现；0 行也必须按 CO-44② 出声。
		"本次没有候选",
		"0 行按 CO-44② 出声",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("boot 日志里没有 %q —— Recovery 那一格没接上（或没按 CO-44② 出声）:\n%s", want, got)
		}
	}
}

// TestActiveProcessCountsUsesTheRegisteredCO45Triple 钉 CO-45③ 登记的那个口径：
// status IN ('STARTING','RUNNING','STOPPING') 按 process_type 分组，其余状态一律不算活。
// 反例落点：把 STOPPING 拿掉（或把 EXITED 算进来）⇒ 这条用例红 —— 口径的权威在契约① 第 20 条，
// 不在代码注释里。
func TestActiveProcessCountsUsesTheRegisteredCO45Triple(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()

	seedProcess := func(id, kind, status string) {
		t.Helper()
		if _, err := core.db.Read.Exec(`INSERT INTO process_instances
			(id, process_type, owner_type, owner_id, pid, status, metadata_json, created_at, updated_at)
			VALUES (?, ?, 'adapter_instance', 'ad-1', 4242, ?, '{}', 1, 1)`, id, kind, status); err != nil {
			t.Fatal(err)
		}
	}
	seedProcess("pi-running", "ADAPTER", "RUNNING")
	seedProcess("pi-stopping", "ADAPTER", "STOPPING")
	seedProcess("pi-starting", "WORKER", "STARTING")
	seedProcess("pi-exited", "ADAPTER", "EXITED")
	seedProcess("pi-failed", "WORKER", "FAILED")

	counts, err := activeProcessCounts(context.Background(), core.db.Read)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"ADAPTER": 2, "WORKER": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("CO-45③ 口径数出来 %v，应为 %v（三个活状态之外的行不许混进来）", counts, want)
	}
}

// TestProcessFactsComeFromTheRealProcessRow 钉证据来源的三半：
// ① 只回该台的行（含已退出的 —— 上一代跑过什么正是崩溃恢复要问的）；
// ② attempt 必须与 internal/process 真写进 metadata_json 的那个键同源 ⇒ 这一行走真写口落，
//
//	不手写 JSON（手写就等于让判据照着我自己的猜测写）；
//
// ③ 陌生台 = 0 行 —— 那正是 CO-44② 闸门的输入（不改判，只喊）。
func TestProcessFactsComeFromTheRealProcessRow(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	ctx := context.Background()

	lc, err := process.NewLifecycleStore(process.ProcessRow{
		Kind: process.KindAdapter, OwnerType: process.OwnerTypeAdapterInstance, OwnerID: "ad-1",
		Executable: "fake-adapter", RestartPolicy: process.RestartNever,
	}, func() (string, error) { return "pi-real", nil }, func() int64 { return 1000 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := core.db.Write(ctx, func(tx *db.Tx) error {
		_, err := lc.RecordProcessStart(tx, process.Event{
			Kind: process.EventStarted, PID: 4242, Status: process.StatusStarting,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// 另一台的一行造成本来就退出过的形态（含已退出的 + 按台分格）。
	if _, err := core.db.Read.Exec(`INSERT INTO process_instances
		(id, process_type, owner_type, owner_id, pid, status, metadata_json, created_at, updated_at)
		VALUES ('pi-other', 'ADAPTER', 'adapter_instance', 'ad-2', 77, 'EXITED', '{"attempt":1}', 2, 3)`,
	); err != nil {
		t.Fatal(err)
	}

	facts := processFactsOf(core.db.Read)
	got, err := facts(ctx, "ad-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("ad-1 应有 1 行（真写口落的那行），实际 %+v", got)
	}
	f := got[0]
	if f.ID != "pi-real" || f.PID != 4242 || f.Status != string(process.StatusStarting) {
		t.Errorf("读回的事实不对: %+v", f)
	}
	if f.Attempt != 1 {
		t.Errorf("attempt 读成 %d，应为 1 —— attemptOf 的键必须与 internal/process 写进 metadata_json 的那个逐字同源", f.Attempt)
	}

	other, err := facts(ctx, "ad-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].ID != "pi-other" || other[0].Status != "EXITED" || other[0].PID != 77 {
		t.Errorf("ad-2 的行读错: %+v", other)
	}

	stranger, err := facts(ctx, "ad-never-seen")
	if err != nil {
		t.Fatal(err)
	}
	if len(stranger) != 0 {
		t.Errorf("陌生台必须是 0 行（CO-44② 闸门的输入，不许拿别人的行顶上）: %+v", stranger)
	}
}

// TestRecoveryScanInterruptsAttributedExecutionAndWritesAudit 是 T-391 第二段（09-29 主控裁「分」）
// 的真库判据：起一个真 Core（boot 把四件依赖接齐），播一条在飞 Execution + 该台的一行进程事实，
// 跑一次启动扫描，然后在库里对三样东西 ——
//
//	① executions.status = INTERRUPTED（不是 FAILED：进程死不是失败的证据，§7/CO-8）；
//	② event_journal 恰一条 execution_interrupted（改判必须带证据，§7 原话）；
//	③ audit_entries 恰一条同事务行（§6 Audit 列 ✓），audit_id 从事件 id 派生、details_json 带得出理由；
//
// 另一台没有进程行的在飞 Execution 一格都不许动（归因闸门按台分格，CO-44②）。
// 反例落点：把 main.go 接的 recoveryAudit 摘掉（或 markInterrupted 里 RequiresAudit 那一段删掉）
// ⇒ ③ 是 0 行，这条用例红。
func TestRecoveryScanInterruptsAttributedExecutionAndWritesAudit(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	ctx := context.Background()

	seedExecution := func(id, taskID, adapterID, sessionID string) {
		t.Helper()
		for _, q := range []struct {
			sql string
			arg []any
		}{
			{`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
				VALUES (?, ?, ?, 1, 1)`, []any{adapterID, adapterID, adapterID}},
			{`INSERT INTO tasks (id, title, created_at, updated_at) VALUES (?, ?, 1, 1)`,
				[]any{taskID, "恢复扫描-" + adapterID}},
			{`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, managed, sync_state,
					state_confidence, created_at, updated_at)
				VALUES (?, ?, ?, 1, 'SYNCED', 'HIGH', 1, 1)`, []any{sessionID, adapterID, "remote-" + adapterID}},
			{`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, attempt_no, status,
					state_confidence, trigger_type, created_at, updated_at)
				VALUES (?, ?, ?, ?, 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`,
				[]any{id, taskID, adapterID, sessionID}},
		} {
			if _, err := core.db.Read.Exec(q.sql, q.arg...); err != nil {
				t.Fatalf("夹具插入失败 %s: %v", q.sql, err)
			}
		}
	}
	// 有进程行的那台（归因得上）与没有进程行的那台（归因不上）一起播。
	seedExecution("ex-attr", "task-attr", "ad-attr", "sess-attr")
	seedExecution("ex-none", "task-none", "ad-none", "sess-none")
	if _, err := core.db.Read.Exec(`INSERT INTO process_instances
		(id, process_type, owner_type, owner_id, pid, status, executable, metadata_json, created_at, updated_at)
		VALUES ('pi-attr', 'ADAPTER', 'adapter_instance', 'ad-attr', 4242, 'EXITED', 'fake-adapter',
			'{"attempt":2}', 1, 1)`); err != nil {
		t.Fatalf("夹具进程行插入失败: %v", err)
	}

	core.runRecovery(ctx)

	one := func(q string, args ...any) string {
		t.Helper()
		var s string
		if err := core.db.Read.QueryRow(q, args...).Scan(&s); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return s
	}
	if st := one(`SELECT status FROM executions WHERE id = 'ex-attr'`); st != "INTERRUPTED" {
		t.Errorf("归因得上的那条应是 INTERRUPTED，实际 %s（09-29 裁：Core 重启那一支 = RUNNING→INTERRUPTED）", st)
	}
	if st := one(`SELECT status FROM executions WHERE id = 'ex-none'`); st != "RUNNING" {
		t.Errorf("没有进程行的那台一格都不许动（CO-44② 不改判只喊），实际 %s", st)
	}
	if n := journalCountFor(t, core, "ex-attr"); n != 1 {
		t.Fatalf("改判必须恰带一条事件，实际 %d", n)
	}
	eventID := one(`SELECT event_id FROM event_journal
		WHERE aggregate_id = 'ex-attr' AND event_type = 'execution_interrupted' ORDER BY seq DESC LIMIT 1`)

	var (
		auditID, principal, details string
	)
	if err := core.db.Read.QueryRow(`SELECT audit_id, principal_id, details_json FROM audit_entries
		WHERE action = 'execution_interrupted' AND execution_id = 'ex-attr'`).
		Scan(&auditID, &principal, &details); err != nil {
		t.Fatalf("§6 Audit 列 ✓ 的那一行没写出去: %v", err)
	}
	if auditID != "audit-"+eventID {
		t.Errorf("audit_id %q 应从事件 id 派生（audit-%s），这样归因行才指得回事件", auditID, eventID)
	}
	if principal != "system" {
		t.Errorf("启动扫描是 Core 自己发起的观察，principal 应是 system，实际 %s", principal)
	}
	if !strings.Contains(details, "pi-attr") || !strings.Contains(details, "pid=4242") {
		t.Errorf("details_json 里的 Reason 必须说得出依据（哪一行进程行）: %s", details)
	}
	if n := auditCountFor(t, core, "ex-none"); n != 0 {
		t.Errorf("没改判的那条不许记 audit 行: %d", n)
	}
}

// journalCountFor 数某聚合在 event_journal 里有几条（cmd/core 侧的读口，与 domain 侧那条同义）。
func journalCountFor(t *testing.T, core *Core, aggregateID string) int {
	t.Helper()
	var n int
	if err := core.db.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE aggregate_id = ?`,
		aggregateID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// auditCountFor 数某 execution 在 audit_entries 里有几条 execution_interrupted 行。
func auditCountFor(t *testing.T, core *Core, executionID string) int {
	t.Helper()
	var n int
	if err := core.db.Read.QueryRow(`SELECT count(*) FROM audit_entries
		WHERE action = 'execution_interrupted' AND execution_id = ?`, executionID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
