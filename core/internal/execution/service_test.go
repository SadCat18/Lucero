package execution_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// seedWriteFixture 播三样外键前提：principals（event_journal.principal_id 要）、
// 一家 adapter_instance、一个 task。用裸 SQL 是因为它们是**别的聚合的既有事实**，
// 不是本包要测的写路径。
func seedWriteFixture(t *testing.T, d *db.DB) {
	t.Helper()
	// 平台层不初始化脱敏就拒绝写（fail closed）。规则的最小副本自带：
	// 权威是 `audit.Rules()`，但业务包的测试不 import 规则包（C14：audit 在途改动会把本包带红）。
	if err := db.InitRedaction([]string{"token"}, []string{`sk-[A-Za-z0-9_-]{16,}`}, "[REDACTED]"); err != nil {
		t.Fatalf("装脱敏规则: %v", err)
	}
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Read.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, p := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
		must(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, p[0], p[1], p[0])
	}
	must(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ad-codex', 'codex', 'codex', 1, 1)`)
	must(`INSERT INTO tasks (id, title, created_at, updated_at) VALUES ('task-1', '跑通垂直链', 1, 1)`)
}

func rowsOf(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestCreateWritesFirstRowAndEvent 是写侧第一步的真表形状：CREATED v1 + 恰一行 execution_created，
// 三个可空外键没传就必须是 NULL（不是空串），payload 带 §6 点名的那几个键。
func TestCreateWritesFirstRowAndEvent(t *testing.T) {
	d := openDB(t)
	seedWriteFixture(t, d)
	ctx := context.Background()
	svc := execution.NewService()

	var got execution.Execution
	if err := d.Write(ctx, func(tx *db.Tx) error {
		var err error
		got, err = svc.Create(ctx, tx, execution.NewExecution{
			ID: "ex-1", TaskID: "task-1", AdapterInstanceID: "ad-codex",
			EventID: "ev-c1", PrincipalID: "system",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got.Status != execution.Created || got.Version != 1 || got.AttemptNo != 1 ||
		got.TriggerType != execution.TriggerInitial || got.StateConfidence != execution.ConfidenceUnknown {
		t.Errorf("Create 返回不对: %+v", got)
	}
	var status string
	var version int64
	var sessionNull, modelNull bool
	if err := d.Read.QueryRow(`SELECT status, version, session_id IS NULL, model_id IS NULL
		FROM executions WHERE id='ex-1'`).Scan(&status, &version, &sessionNull, &modelNull); err != nil {
		t.Fatal(err)
	}
	if status != "CREATED" || version != 1 || !sessionNull || !modelNull {
		t.Errorf("库里形状不对: %s v%d session_null=%v model_null=%v", status, version, sessionNull, modelNull)
	}
	var payload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal
		WHERE event_type='execution_created' AND aggregate_id='ex-1'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"task_id":"task-1"`, `"adapter_instance_id":"ad-codex"`,
		`"attempt_no":1`, `"trigger_type":"INITIAL"`, `"schema":1`} {
		if !strings.Contains(payload, want) {
			t.Errorf("execution_created payload 缺 %s，实际 %s", want, payload)
		}
	}
	if n := rowsOf(t, d, `SELECT count(*) FROM event_journal WHERE aggregate_id='ex-1'`); n != 1 {
		t.Errorf("首行该只有一行事件，实际 %d", n)
	}
}

// TestCreateRefusesBadInputWithoutWriting 钉"校验在碰库之前"：四种非法输入一种痕迹都不许留。
// 其中 attempt_no 与前驱的互斥那条是 ADR-013/015 的痕迹：换 Provider 必须留下"第几次、从哪次来"。
func TestCreateRefusesBadInputWithoutWriting(t *testing.T) {
	d := openDB(t)
	seedWriteFixture(t, d)
	ctx := context.Background()
	svc := execution.NewService()

	cases := []struct {
		name string
		in   execution.NewExecution
	}{
		{"不挂 Task", execution.NewExecution{ID: "b1", AdapterInstanceID: "ad-codex", EventID: "e1"}},
		{"没有 adapter", execution.NewExecution{ID: "b2", TaskID: "task-1", EventID: "e2"}},
		{"trigger 不在 §23 词表", execution.NewExecution{ID: "b3", TaskID: "task-1",
			AdapterInstanceID: "ad-codex", TriggerType: "SOMETIMES", EventID: "e4"}},
		{"attempt_no=2 却没有前驱", execution.NewExecution{ID: "b4", TaskID: "task-1",
			AdapterInstanceID: "ad-codex", AttemptNo: 2, EventID: "e5"}},
		{"首次执行却带了前驱", execution.NewExecution{ID: "b5", TaskID: "task-1",
			AdapterInstanceID: "ad-codex", PredecessorExecutionID: "ex-0", EventID: "e6"}},
		{"缺 event_id", execution.NewExecution{ID: "b6", TaskID: "task-1", AdapterInstanceID: "ad-codex"}},
	}
	for _, tc := range cases {
		err := d.Write(ctx, func(tx *db.Tx) error {
			_, e := svc.Create(ctx, tx, tc.in)
			return e
		})
		if !errors.Is(err, execution.ErrInvalidExecution) {
			t.Errorf("%s：应报 ErrInvalidExecution，实际 %v", tc.name, err)
		}
	}
	if n := rowsOf(t, d, `SELECT count(*) FROM executions`); n != 0 {
		t.Errorf("被拒的建执行竟然落了 %d 行", n)
	}
	if n := rowsOf(t, d, `SELECT count(*) FROM event_journal`); n != 0 {
		t.Errorf("被拒的建执行写出了 %d 行事件", n)
	}
}

// TestStartedNeedsStarting 钉两件事：STARTING→RUNNING 走 execution_started（带 provider 侧两个键）；
// 而 CREATED 直接跳 RUNNING 被 §5.2 拒 —— 想先进 STARTING 就得碰 REQ-DB-4（§6 没有 CREATED→STARTING 的事件，
// 平台层 Touch 又强制要一个已注册事件），所以这里用裸 SQL 摆一条 STARTING 的行，等于把那一步"当作已发生"。
func TestStartedAdvancesFromStartingAndRefusesSkip(t *testing.T) {
	d := openDB(t)
	seedWriteFixture(t, d)
	ctx := context.Background()
	svc := execution.NewService()

	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES ('ex-s', 'task-1', 'ad-codex', 1, 'STARTING', 'UNKNOWN', 'INITIAL', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	// 另建一条停在 CREATED 的，验"不许跳过 STARTING"。
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, e := svc.Create(ctx, tx, execution.NewExecution{ID: "ex-c", TaskID: "task-1",
			AdapterInstanceID: "ad-codex", EventID: "ev-c2", PrincipalID: "system"})
		return e
	}); err != nil {
		t.Fatal(err)
	}

	var res execution.Result
	if err := d.Write(ctx, func(tx *db.Tx) error {
		var err error
		res, err = svc.Started(ctx, tx, "ex-s", execution.Command{
			EventID: "ev-st", PrincipalID: "system", WantVersion: 1,
			ProviderExecutionID: "prov-77", SnapshotSHA256: "abc123",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if res.From != execution.Starting || res.To != execution.Running || res.Version != 2 || !res.Advanced {
		t.Errorf("Started 结果不对: %+v", res)
	}
	var payload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal
		WHERE event_type='execution_started' AND aggregate_id='ex-s'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"provider_execution_id":"prov-77"`, `"model_snapshot_sha256":"abc123"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("execution_started payload 缺 %s: %s", want, payload)
		}
	}

	// CREATED 直接→RUNNING：矩阵先拒，且一行不写。
	err := d.Write(ctx, func(tx *db.Tx) error {
		_, e := svc.Started(ctx, tx, "ex-c", execution.Command{EventID: "ev-skip", PrincipalID: "system"})
		return e
	})
	if !errors.Is(err, execution.ErrIllegalTransition) {
		t.Errorf("CREATED→RUNNING 应被 §5.2 拒，实际 %v", err)
	}
	if n := rowsOf(t, d, `SELECT count(*) FROM event_journal WHERE aggregate_id='ex-c'`); n != 1 {
		t.Errorf("被拒的推进多写了事件（应只剩 execution_created 那一行），实际 %d", n)
	}
}
