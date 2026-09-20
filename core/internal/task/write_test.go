package task_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/eventjournal"
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

// requireCatalogued 检查这几个事件类型有没有进 db 的注册表（internal/eventjournal 的 catalog）。
// C2 裁定新增的五个（task_paused / task_blocked / task_resumed / execution_paused / execution_resumed）
// 至今不在里面，写它们会被平台层按 §9.1 判 P0 直接拒 → 已提 REQ-DB-3。
// 这里用**条件跳过**而不是把断言删掉或注释掉：db 一加进注册表，这些正向用例自己就开始跑，
// 不需要谁记得回来取消 skip；跳过时会打出等谁，不会被读成"通过"。
func requireCatalogued(t *testing.T, types ...string) {
	t.Helper()
	for _, s := range types {
		if !eventjournal.Registered(s) {
			t.Skipf("%q 还没进 internal/eventjournal 的注册表（REQ-DB-3 等 db），写它会被 §9.1 判 P0", s)
		}
	}
}

// newTaskIn 把一条 Task 直接摆在指定状态（openWriteDB 已经种好两个 principals，所以不像
// service_test.go 的 newTaskAt 那样再插一遍 —— 那会撞 principals 的主键）。
// 用途：暂停/恢复只关心"从哪个状态动"，不关心它是怎么走到那儿的。
func newTaskIn(t *testing.T, d *db.DB, ctx context.Context, id string, status task.State) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		return tx.Create(db.AggTask, id,
			[]string{"title", "status", "created_at", "updated_at"},
			[]any{"暂停与恢复", string(status), now, now},
			db.EventDraft{EventID: "ev-new-" + id, Type: task.EventCreated,
				Aggregate: db.AggTask, AggregateID: id, PrincipalID: "system"})
	}); err != nil {
		t.Fatal(err)
	}
}

func eventsOf(t *testing.T, d *db.DB, id string) int {
	t.Helper()
	return countRows(t, d, `SELECT count(*) FROM event_journal WHERE aggregate_id=?`, id)
}

func payloadOf(t *testing.T, d *db.DB, eventType, id string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(
		`SELECT payload_json FROM event_journal WHERE event_type=? AND aggregate_id=?`, eventType, id).Scan(&s); err != nil {
		t.Fatalf("没有 %s 这一行事件（%s）: %v", eventType, id, err)
	}
	return s
}

// TestPrepareWritesPreparingWithExecution 钉三件事：
// ① CREATED→PREPARING 不在 §5.1 矩阵里，被拒且一行不写；
// ② QUEUED→PREPARING 落 task_preparing，且 payload 真点名了 execution_id（§6）；
// ③ 缺 execution_id 的 Prepare 在碰库之前就失败 —— 不是"写完再回滚"。
func TestPrepareWritesPreparingWithExecution(t *testing.T) {
	d, ctx := openWriteDB(t)
	svc := task.NewService(nil)
	mustCreate(t, d, ctx, "t-p", "准备开工", "NORMAL")

	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Prepare(ctx, tx, "t-p", task.Command{EventID: "ev-illegal", PrincipalID: "local-user", ExecutionID: "ex-1"})
		return err
	}); !errors.Is(err, task.ErrIllegalTransition) {
		t.Errorf("CREATED→PREPARING 应被 §5.1 拒掉，实际 %v", err)
	}
	if n := eventsOf(t, d, "t-p"); n != 1 {
		t.Fatalf("被拒的 Prepare 留下了 %d 行事件", n)
	}

	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Queue(ctx, tx, "t-p", task.Command{EventID: "ev-q", PrincipalID: "local-user"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Prepare(ctx, tx, "t-p", task.Command{EventID: "ev-noex", PrincipalID: "local-user"})
		return err
	}); !errors.Is(err, task.ErrInvalidTask) {
		t.Errorf("缺 execution_id 的 Prepare 应报 ErrInvalidTask，实际 %v", err)
	}
	if n := eventsOf(t, d, "t-p"); n != 2 {
		t.Fatalf("缺 execution_id 的 Prepare 竟然写了 %d 行事件（应为 2：created+queued）", n)
	}

	var res task.Result
	if err := d.Write(ctx, func(tx *db.Tx) error {
		var err error
		res, err = svc.Prepare(ctx, tx, "t-p", task.Command{EventID: "ev-prep", PrincipalID: "local-user", ExecutionID: "ex-1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if res.To != task.Preparing || res.Version != 3 || !res.Advanced {
		t.Errorf("Prepare 结果不对: %+v", res)
	}
	if p := payloadOf(t, d, task.EventPreparing, "t-p"); !strings.Contains(p, `"execution_id":"ex-1"`) {
		t.Errorf("task_preparing 的 payload 没点名 execution_id: %s", p)
	}
}

// TestPauseThenResumeWritesBothEvents 是 :pause/:resume 的真表往返：
// RUNNING→PAUSED 带 reason，PAUSED→RUNNING 带 from=PAUSED；重复暂停被矩阵拒；
// resume 到 CANCELLED 这类目标在动作层就拒（那是 :cancel 的活），且一行不写。
func TestPauseThenResumeWritesBothEvents(t *testing.T) {
	d, ctx := openWriteDB(t)
	svc := task.NewService(nil)
	newTaskIn(t, d, ctx, "t-run", task.Running)

	// 这一条不依赖任何事件类型注册，所以放在最前面先跑掉：目标态不在 QUEUED/RUNNING 里的
	// resume 属于误用（:cancel 有它自己的动作），必须在读库之前就被动作层拒。
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Resume(ctx, tx, "t-run", task.Cancelled, task.Command{EventID: "ev-bad-to", PrincipalID: "local-user"})
		return err
	}); !errors.Is(err, task.ErrInvalidTask) {
		t.Errorf("resume 到 CANCELLED 应报 ErrInvalidTask，实际 %v", err)
	}

	requireCatalogued(t, task.EventPaused, task.EventResumed)
	var p task.Result
	if err := d.Write(ctx, func(tx *db.Tx) error {
		var err error
		p, err = svc.Pause(ctx, tx, "t-run", task.Command{
			EventID: "ev-pause", PrincipalID: "local-user", Reason: "用户在饭点按了暂停", WantVersion: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if p.From != task.Running || p.To != task.Paused || p.Version != 2 {
		t.Errorf("Pause 结果不对: %+v", p)
	}
	if got := payloadOf(t, d, task.EventPaused, "t-run"); !strings.Contains(got, `"reason":"用户在饭点按了暂停"`) {
		t.Errorf("task_paused 没带上 reason: %s", got)
	}

	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Pause(ctx, tx, "t-run", task.Command{EventID: "ev-pause-2", PrincipalID: "local-user"})
		return err
	}); !errors.Is(err, task.ErrIllegalTransition) {
		t.Errorf("PAUSED→PAUSED 不在矩阵里，应被拒，实际 %v", err)
	}
	if n := eventsOf(t, d, "t-run"); n != 2 {
		t.Fatalf("两次被拒的动作写到了 %d 行（应停在 paused 那一行）", n)
	}

	var r task.Result
	if err := d.Write(ctx, func(tx *db.Tx) error {
		var err error
		r, err = svc.Resume(ctx, tx, "t-run", task.Running, task.Command{
			EventID: "ev-resume", PrincipalID: "local-user", WantVersion: 2})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if r.From != task.Paused || r.To != task.Running || r.Version != 3 {
		t.Errorf("Resume 结果不对: %+v", r)
	}
	if got := payloadOf(t, d, task.EventResumed, "t-run"); !strings.Contains(got, `"from":"PAUSED"`) {
		t.Errorf("task_resumed.from 应是 PAUSED: %s", got)
	}
}

// TestResumeRefusesStatesThatWereNeverSuspended 是 resumedFrom 白名单的唯一证据。
// 前提要先证住：§5.1 确实给 INTERRUPTED 开了到 RUNNING 的边 —— 所以光靠矩阵拦不住，
// 不设白名单的话一次崩溃恢复会被写成 task_resumed{from:"INTERRUPTED"}，而 §6 的 from 只有 PAUSED/BLOCKED。
func TestResumeRefusesStatesThatWereNeverSuspended(t *testing.T) {
	if err := task.Check(task.Interrupted, task.Running, task.Normal, task.Evidence{}); err != nil {
		t.Fatalf("前提变了：§5.1 本该允许 INTERRUPTED→RUNNING，实际 %v", err)
	}
	d, ctx := openWriteDB(t)
	svc := task.NewService(nil)
	newTaskIn(t, d, ctx, "t-int", task.Interrupted)

	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Resume(ctx, tx, "t-int", task.Running, task.Command{EventID: "ev-r", PrincipalID: "local-user"})
		return err
	}); !errors.Is(err, task.ErrIllegalTransition) {
		t.Errorf("INTERRUPTED 不是 resume 的合法起始态，实际 %v", err)
	}
	if n := eventsOf(t, d, "t-int"); n != 1 {
		t.Fatalf("被拒的 resume 留下了 %d 行事件", n)
	}

	// BLOCKED 才是 §6 认的另一种 from，且它到 QUEUED 的边也在矩阵里。
	// 这一小段要写 task_resumed（还没进注册表，REQ-DB-3），所以放在上面那两条守卫之后：
	// 上面那两条不依赖注册表，无论 db 有没有补都会真跑。
	requireCatalogued(t, task.EventResumed)
	newTaskIn(t, d, ctx, "t-blk", task.Blocked)
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Resume(ctx, tx, "t-blk", task.Queued, task.Command{EventID: "ev-rb", PrincipalID: "local-user"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := payloadOf(t, d, task.EventResumed, "t-blk"); !strings.Contains(got, `"from":"BLOCKED"`) {
		t.Errorf("task_resumed.from 应是 BLOCKED: %s", got)
	}
}
