package task_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

// openWriteDB 复用 rejection_db_test.go 的 openDB（真库 + 迁移 + 装好脱敏），
// 只补上两个 principals 行：event_journal.principal_id 与 tasks.requested_by 都有外键。
func openWriteDB(t *testing.T) (*db.DB, context.Context) {
	t.Helper()
	d := openDB(t)
	for _, p := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
		if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, p[0], p[1], p[0]); err != nil {
			t.Fatal(err)
		}
	}
	return d, context.Background()
}

func mustCreate(t *testing.T, d *db.DB, ctx context.Context, id, title, priority string) task.Task {
	t.Helper()
	var out task.Task
	err := d.Write(ctx, func(tx *db.Tx) error {
		var err error
		out, err = task.NewService(nil).Create(ctx, tx, task.NewTask{
			ID: id, EventID: "ev-" + id, Title: title, Priority: priority,
			RequestedBy: "local-user", PrincipalID: "local-user", CorrelationID: "c-" + id,
		})
		return err
	})
	if err != nil {
		t.Fatalf("Create(%s): %v", id, err)
	}
	return out
}

func countRows(t *testing.T, d *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestCreateThenQueueWritesOneEventPerVersion 是垂直链前两步的真表形状：
// Create 落 CREATED v1 + 一行 task_created；Queue 落 QUEUED v2 + 一行 task_queued；
// 再 Queue 一次必须被 §5.1 拒掉且一行都不写（QUEUED→QUEUED 不在矩阵里）。
func TestCreateThenQueueWritesOneEventPerVersion(t *testing.T) {
	d, ctx := openWriteDB(t)
	svc := task.NewService(nil)

	got := mustCreate(t, d, ctx, "t-1", "跑通垂直链", "HIGH")
	if got.Status != task.Created || got.Version != 1 || got.Mode != "EXECUTE" ||
		got.TaskType != "GENERAL" || got.RiskLevel != "LOW" {
		t.Errorf("Create 返回不对: %+v", got)
	}

	var q task.Result
	if err := d.Write(ctx, func(tx *db.Tx) error {
		var err error
		q, err = svc.Queue(ctx, tx, "t-1", task.Command{EventID: "ev-q1", PrincipalID: "local-user"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !q.Advanced || q.To != task.Queued || q.Version != 2 {
		t.Errorf("Queue 结果不对: %+v", q)
	}

	// 重复 Queue：状态机先拒绝，且不能留下第三个版本或第三行事件。
	err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Queue(ctx, tx, "t-1", task.Command{EventID: "ev-q2", PrincipalID: "local-user"})
		return err
	})
	if !errors.Is(err, task.ErrIllegalTransition) {
		t.Errorf("QUEUED→QUEUED 应被 §5.1 拒掉，实际 %v", err)
	}
	var status string
	var version int64
	if err := d.Read.QueryRow(`SELECT status, version FROM tasks WHERE id='t-1'`).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "QUEUED" || version != 2 {
		t.Errorf("被拒的第二次 Queue 改动了库: %s v%d", status, version)
	}
	if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE aggregate_id='t-1'`); n != 2 {
		t.Errorf("一个版本一行事件：应有 2 行，实际 %d", n)
	}

	// 没传的可空外键必须是 NULL；传了的必须有值。
	var projNull, reqSet bool
	if err := d.Read.QueryRow(`SELECT project_id IS NULL, requested_by IS NOT NULL FROM tasks WHERE id='t-1'`).
		Scan(&projNull, &reqSet); err != nil {
		t.Fatal(err)
	}
	if !projNull || !reqSet {
		t.Errorf("project_id 应为 NULL、requested_by 应写值: project_null=%v requested_set=%v", projNull, reqSet)
	}

	// §6 的 task_created 关键字段必须真的进了 payload（"schema":1 由平台层补）。
	var payload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE event_type='task_created' AND aggregate_id='t-1'`).
		Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"title":"跑通垂直链"`, `"priority":"HIGH"`, `"mode":"EXECUTE"`, `"risk_level":"LOW"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("task_created payload 缺 %s，实际 %s", want, payload)
		}
	}

}

// TestIfMatchRejectsStaleWrite 是契约③ §2 那条：带旧版本号的写必须 409 且不 Touch。
func TestIfMatchRejectsStaleWrite(t *testing.T) {
	d, ctx := openWriteDB(t)
	svc := task.NewService(nil)
	mustCreate(t, d, ctx, "t-im", "并发", "NORMAL")

	err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Queue(ctx, tx, "t-im", task.Command{EventID: "ev-x", PrincipalID: "local-user", WantVersion: 99})
		return err
	})
	if !errors.Is(err, task.ErrVersionConflict) {
		t.Fatalf("应 ErrVersionConflict，实际 %v", err)
	}
	var status string
	var version int64
	if err := d.Read.QueryRow(`SELECT status, version FROM tasks WHERE id='t-im'`).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "CREATED" || version != 1 {
		t.Errorf("被 409 拒掉的写不该动库：%s v%d", status, version)
	}
	if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE aggregate_id='t-im'`); n != 1 {
		t.Errorf("被拒的写不该留第二行事件，实际 %d", n)
	}
}

// 入参校验发生在写库之前：坏输入不能留下半条任务。
func TestCreateRejectsBadInputWithoutWriting(t *testing.T) {
	d, ctx := openWriteDB(t)
	svc := task.NewService(nil)

	bad := []task.NewTask{
		{ID: "", EventID: "e", Title: "缺 id"},
		{ID: "b-1", EventID: "", Title: "缺 event_id"},
		{ID: "b-2", EventID: "e", Title: "   "},
		{ID: "b-3", EventID: "e", Title: "x", Priority: "URGENT"},
		{ID: "b-4", EventID: "e", Title: "x", RiskLevel: "EXTREME"},
	}
	for _, in := range bad {
		err := d.Write(ctx, func(tx *db.Tx) error {
			_, err := svc.Create(ctx, tx, in)
			return err
		})
		if !errors.Is(err, task.ErrInvalidTask) {
			t.Errorf("%+v 应 ErrInvalidTask，实际 %v", in, err)
		}
	}
	for _, tbl := range []string{"tasks", "event_journal"} {
		if n := countRows(t, d, `SELECT count(*) FROM `+tbl); n != 0 {
			t.Errorf("%s 被坏输入写了 %d 行", tbl, n)
		}
	}
}

// keyset 游标：5 条任务按两页翻完，必须不重不漏、顺序稳定。
func TestListKeysetCursorNoGapsNoDupes(t *testing.T) {
	d, ctx := openWriteDB(t)
	repo := task.NewRepository(d.Read)
	for i := 1; i <= 5; i++ {
		if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, status, priority, created_at, updated_at)
			VALUES (?, ?, 'CREATED', 'NORMAL', ?, 1)`, fmt.Sprintf("t%d", i), fmt.Sprintf("标题%d", i), i*10); err != nil {
			t.Fatal(err)
		}
	}

	var seen []string
	filter := task.Filter{Status: task.Created, Limit: 2}
	for page := 0; page < 6; page++ {
		batch, err := repo.List(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch) == 0 {
			break
		}
		if len(batch) > 2 {
			t.Fatalf("limit 没生效：第 %d 页 %d 条", page, len(batch))
		}
		for _, tsk := range batch {
			seen = append(seen, tsk.ID)
		}
		last := batch[len(batch)-1]
		filter.AfterCreatedAt, filter.AfterID = last.CreatedAt, last.ID
	}
	if len(seen) != 5 {
		t.Fatalf("翻页应取到 5 条，实际 %v", seen)
	}
	unique := map[string]bool{}
	for i, id := range seen {
		if unique[id] {
			t.Errorf("第 %d 条 %s 重复出现了：%v", i, id, seen)
		}
		unique[id] = true
	}
	if seen[0] != "t1" || seen[4] != "t5" {
		t.Errorf("顺序应按 (created_at, id) 稳定：%v", seen)
	}
	if byProject, err := repo.List(ctx, task.Filter{Status: task.Created, ProjectID: "nope", Limit: 10}); err != nil || len(byProject) != 0 {
		t.Errorf("不存在的项目应返回空：%v %v", byProject, err)
	}
}
