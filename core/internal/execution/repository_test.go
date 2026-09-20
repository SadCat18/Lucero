package execution_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	return d
}

// seed 用裸 SQL 播数据：只读侧要验的是"读得像不像"，写侧的合法路径（db.Create/Touch + 状态机）
// 另有 internal/task 的 rejection_db_test.go 在管，这里不重复造一套写入口。
func seed(t *testing.T, d *db.DB) {
	t.Helper()
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Read.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, a := range [][2]string{{"ad-codex", "codex"}, {"ad-qoder", "qoder"}} {
		must(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, a[0], a[1], a[1])
	}
	must(`INSERT INTO tasks (id, title, created_at, updated_at) VALUES ('task-1', '跑通垂直链', 1, 1)`)
	must(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, created_at, updated_at)
		VALUES ('sess-q', 'ad-qoder', 'remote-q', 1, 1)`)
	// 同一个 Task 三次尝试，跨两家软件三个 Session：TaskID 不变（ADR-013）。
	must(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, predecessor_execution_id,
		attempt_no, status, state_confidence, trigger_type, provider_execution_id, created_at, updated_at, heartbeat_at)
		VALUES ('ex-1', 'task-1', 'ad-codex', NULL, NULL, 1, 'SUCCEEDED', 'HIGH', 'INITIAL', 'p1', 1, 1, 1)`)
	must(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, predecessor_execution_id,
		attempt_no, status, state_confidence, trigger_type, created_at, updated_at, heartbeat_at)
		VALUES ('ex-2', 'task-1', 'ad-qoder', 'sess-q', 'ex-1', 2, 'STARTING', 'UNKNOWN', 'MANUAL_SWITCH', 2, 2, 2)`)
	must(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no, status, trigger_type,
		created_at, updated_at) VALUES ('ex-3', 'task-1', 'ad-codex', 3, 'WAITING_INPUT', 'HANDOFF', 3, 3)`)
	// UNKNOWN 既不该被恢复扫描收走，也不该被折算成 FAILED。
	must(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no, status, state_confidence,
		trigger_type, created_at, updated_at) VALUES ('ex-4', 'task-1', 'ad-codex', 4, 'UNKNOWN', 'LOW', 'RECOVERY', 4, 4)`)
}

func TestGetKeepsNullsAndUnknown(t *testing.T) {
	d := openDB(t)
	seed(t, d)

	got, err := execution.NewRepository(d.Read).Get(context.Background(), "ex-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != execution.Starting || got.Version != 1 {
		t.Errorf("读回 %s v%d，应为 STARTING v1", got.Status, got.Version)
	}
	if got.StateConfidence != execution.ConfidenceUnknown {
		t.Errorf("state_confidence 被折算成了 %s", got.StateConfidence)
	}
	if got.ProviderExecutionID != "" || got.SessionID != "sess-q" {
		t.Errorf("可空列读错: %+v", got)
	}
	if got.PredecessorExecutionID != "ex-1" || got.AttemptNo != 2 || got.TriggerType != execution.TriggerManualSwitch {
		t.Errorf("前驱链/触发类型读错: %+v", got)
	}
	if _, err := execution.NewRepository(d.Read).Get(context.Background(), "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("缺行应透传 sql.ErrNoRows，实际 %v", err)
	}
}

func TestListByTaskShowsEveryAttempt(t *testing.T) {
	d := openDB(t)
	seed(t, d)

	all, err := execution.NewRepository(d.Read).ListByTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("该 Task 应有 4 次尝试，实际 %d", len(all))
	}
	adapters := map[string]bool{}
	for i, e := range all {
		if e.TaskID != "task-1" {
			t.Fatalf("第 %d 条换了 TaskID: %q", i, e.TaskID)
		}
		if e.AttemptNo != int64(i+1) {
			t.Errorf("attempt_no 应按时间递增，第 %d 条是 %d", i, e.AttemptNo)
		}
		adapters[e.AdapterInstanceID] = true
	}
	if len(adapters) != 2 {
		t.Errorf("换过两家软件才对，实际 %v", adapters)
	}
}

// 「Crash Recovery Query」只收五个状态；SUCCEEDED 与 UNKNOWN 都不在内——
// 前者已定局，后者没有证据不能猜（§57）。
func TestRecoveryScanTakesOnlyTheFiveStates(t *testing.T) {
	d := openDB(t)
	seed(t, d)

	got, err := execution.NewRepository(d.Read).ListCrashRecoveryCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range got {
		ids[e.ID] = true
		switch e.Status {
		case execution.Starting, execution.Running, execution.WaitingApproval,
			execution.WaitingInput, execution.Transferring:
		default:
			t.Errorf("恢复扫描把 %s（%s）收进来了", e.Status, e.ID)
		}
	}
	// 顺序不声明：heartbeat_at 为 NULL 的行在 SQLite 升序里排最前，那不是契约。
	if len(ids) != 2 || !ids["ex-2"] || !ids["ex-3"] {
		t.Errorf("应只收 ex-2/ex-3，实际 %v", ids)
	}
}
