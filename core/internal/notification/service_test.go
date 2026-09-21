package notification_test

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/notification"
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
	// 脱敏 fail-closed，没初始化过任何写入都会被拒。规则集的所有者是 audit，
	// 本包不 import 它（接线属 cmd/core，见 §2.3 / progress C14），这里只放够跑通的最小集合。
	if err := db.InitRedaction([]string{"token"}, nil, "[REDACTED]"); err != nil {
		t.Fatal(err)
	}
	// event_journal.principal_id 是指向 principals 的外键：先把两种主体建出来。
	seedPrincipals(t, d)
	return d
}

func seedPrincipals(t *testing.T, d *db.DB) {
	t.Helper()
	for _, p := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
		if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, p[0], p[1], p[0]); err != nil {
			t.Fatal(err)
		}
	}
}

func seedTask(t *testing.T, d *db.DB) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, created_at, updated_at)
		VALUES ('task-1', '跑通垂直链', 1, 1)`); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, d *db.DB, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := d.Read.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNotifyWritesOneRowAndOneEvent(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	seedTask(t, d)
	svc := notification.NewService()

	err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Notify(ctx, tx, notification.Draft{
			ID: "n-1", EventID: "ev-n-1", Type: notification.TypeProjectionRejected,
			Title: "投影无合法目标", Body: "§5.1 没有 PREPARING→TRANSFERRING 这条边",
			TaskID: "task-1", PrincipalID: "system", CorrelationID: "c-1", CausationID: "c-1",
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := notification.NewRepository(d.Read).Get(ctx, "n-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != notification.StateUnread || got.Priority != notification.PriorityMedium {
		t.Errorf("默认值不对: state=%q priority=%q", got.State, got.Priority)
	}
	if got.Version != 1 || got.TaskID != "task-1" || got.ApprovalID != "" {
		t.Errorf("读回不对: %+v", got)
	}
	if got.CreatedAt == 0 {
		t.Error("created_at 没写")
	}
	if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE aggregate_type = 'notification' AND aggregate_id = 'n-1'`); n != 1 {
		t.Errorf("notification_created 应恰有一行，实际 %d", n)
	}
	if typ := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'notification_created'`); typ != 1 {
		t.Errorf("事件类型应为 §6 登记的 notification_created，实际 %d", typ)
	}
	list, _, err := notification.NewRepository(d.Read).ListByState(ctx,
		notification.ListFilter{State: notification.StateUnread, Limit: 10})
	if err != nil || len(list) != 1 || list[0].ID != "n-1" {
		t.Errorf("ListByState(UNREAD) = %v %v", list, err)
	}
}

// seedInbox 直接摆出八行通知（含一行 READ 的，用来证明 state 谓词还在）：
// 三档 priority、两个相同 created_at（逼出 id 这一条 tie-break 腿）。
func seedInbox(t *testing.T, d *db.DB) {
	t.Helper()
	f := [][4]string{
		{"a1", "CRITICAL", "100", "UNREAD"},
		{"a2", "CRITICAL", "300", "UNREAD"},
		{"b1", "HIGH", "200", "UNREAD"},
		{"c1", "MEDIUM", "500", "UNREAD"},
		{"c2", "MEDIUM", "500", "UNREAD"},
		{"d1", "LOW", "900", "UNREAD"},
		{"d2", "LOW", "100", "UNREAD"},
		{"e1", "CRITICAL", "999", "READ"},
	}
	for _, row := range f {
		if _, err := d.Read.Exec(`INSERT INTO notifications
			(id, notification_type, priority, state, title, created_at, version)
			VALUES (?, 'PROJECTION_REJECTED', ?, ?, 't', ?, 1)`,
			row[0], row[1], row[3], row[2]); err != nil {
			t.Fatal(err)
		}
	}
}

// TestListByStatePagesWithKeysetCursor 钉契约③ §2 的"翻页只用 keyset、不许 offset"：
// 翻三页拼起来的序列必须等于手写的全量序（不重、不漏、不串 state），
// 并且每页带回来的位置能直接当下一页的入参 —— 这就是 handler 将来要编进 next_cursor 的东西。
// 期望序是**手抄的第二份**（CRITICAL>HIGH>MEDIUM>LOW，同档 created_at 倒序，同刻 id 正序），
// 与 repository 里那句 SQL CASE 各自独立，任何一边漂了这条就红。
func TestListByStatePagesWithKeysetCursor(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	seedInbox(t, d)
	repo := notification.NewRepository(d.Read)

	want := []string{"a2", "a1", "b1", "c1", "c2", "d1", "d2"}
	var (
		got   []string
		after *notification.ListPosition
		pages int
	)
	for pages < 5 {
		rows, last, err := repo.ListByState(ctx, notification.ListFilter{
			State: notification.StateUnread, After: after, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatalf("第 %d 次调用回了空页 —— 短页就该收，说明翻页规则不对", pages+1)
		}
		pages++
		for _, n := range rows {
			got = append(got, n.ID)
		}
		if len(rows) < 3 {
			break // 短页 = 到底（handler 在这种位置回 next_cursor ""）
		}
		pos := last
		after = &pos
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("三页拼起来不等于全量序:\n 实际 %v\n 期望 %v", got, want)
	}
	if pages != 3 {
		t.Errorf("应是三页（3+3+1），实际 %d 页", pages)
	}
	// 位置里的 Rank 由 SQL 算出：第一页末行是 b1（HIGH → 档 1）。
	// 断言它是因为 handler 只会把这一串三元组编码进 next_cursor，Rank 错了下一页就跳档。
	_, firstPageLast, err := repo.ListByState(ctx, notification.ListFilter{
		State: notification.StateUnread, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if firstPageLast != (notification.ListPosition{Rank: 1, CreatedAt: 200, ID: "b1"}) {
		t.Errorf("第一页末行位置不对: %+v", firstPageLast)
	}
	// 越界游标（比最后一行还靠后）必须是空页，而不是回绕到第一页。
	rows, _, err := repo.ListByState(ctx, notification.ListFilter{
		State: notification.StateUnread,
		After: &notification.ListPosition{Rank: 3, CreatedAt: 100, ID: "d2"}, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("末行之后的游标应回空页，实际 %d 行", len(rows))
	}
}

// 三个外键列必须写 NULL 而不是空串：” 是非 NULL，会直接撞 approval_id / task_id 的外键。
func TestNotifyLeavesAbsentForeignKeysNull(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)

	err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := notification.NewService().Notify(ctx, tx, notification.Draft{
			ID: "n-2", EventID: "ev-n-2", Type: "approval_pending", Title: "没有关联对象的通知",
		})
		return err
	})
	if err != nil {
		t.Fatalf("没带 task_id/approval_id 的建单不该撞外键: %v", err)
	}
	if n := countRows(t, d, `SELECT count(*) FROM notifications WHERE id='n-2' AND task_id IS NULL AND approval_id IS NULL AND project_id IS NULL AND body IS NULL`); n != 1 {
		t.Errorf("可空列没保持 NULL（被写成了空串），计数 %d", n)
	}
}

func TestNotifyRejectsBadDrafts(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	svc := notification.NewService()

	cases := map[string]notification.Draft{
		"缺 id":          {EventID: "e", Type: "t", Title: "x"},
		"缺 event_id":    {ID: "i", Type: "t", Title: "x"},
		"缺 type":        {ID: "i", EventID: "e", Title: "x"},
		"缺 title":       {ID: "i", EventID: "e", Type: "t"},
		"priority 不在词表": {ID: "i", EventID: "e", Type: "t", Title: "x", Priority: "URGENT"},
	}
	for name, draft := range cases {
		err := d.Write(ctx, func(tx *db.Tx) error {
			_, err := svc.Notify(ctx, tx, draft)
			return err
		})
		if !errors.Is(err, notification.ErrInvalidDraft) {
			t.Errorf("%s：应返回 ErrInvalidDraft，实际 %v", name, err)
		}
	}
	if n := countRows(t, d, `SELECT count(*) FROM notifications`); n != 0 {
		t.Errorf("被拒的 Draft 不该留下任何行，实际 %d", n)
	}
	if n := countRows(t, d, `SELECT count(*) FROM event_journal`); n != 0 {
		t.Errorf("被拒的 Draft 不该写事件，实际 %d 行", n)
	}
}

// §6 目录里触发者为 notification 的两行：键对账 + Audit 列都是 –。
func TestEventCatalogMatchesContract6(t *testing.T) {
	want := []string{"approval_id", "notification_type", "priority", "task_id"}
	for name, v := range map[string]any{
		notification.EventCreated: notification.CreatedPayload{NotificationType: "projection_rejected", Priority: "HIGH"},
		notification.EventHandled: notification.HandledPayload{NotificationType: "projection_rejected", Priority: "HIGH"},
	} {
		if !strings.HasPrefix(name, "notification_") {
			t.Errorf("%s 不符合 §1 命名", name)
		}
		m, err := notification.Payload(v)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for k := range m {
			got = append(got, k)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s payload 键 = %v，§6 要求 %v", name, got, want)
		}
	}
	if len(notification.TypeProjectionRejected) == 0 {
		t.Error("§5.4 点名的 notification_type 不能丢")
	}
}
