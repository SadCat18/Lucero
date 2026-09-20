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
	list, err := notification.NewRepository(d.Read).ListByState(ctx, notification.StateUnread, 10)
	if err != nil || len(list) != 1 || list[0].ID != "n-1" {
		t.Errorf("ListByState(UNREAD) = %v %v", list, err)
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
