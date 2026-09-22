package notification_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/notification"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// seedNote 用**本包自己的** Service.Notify 建一条 UNREAD（不手写 INSERT 造状态）：
// 这样「读之前那一行长什么样」与生产路径同源，夹具不会自成一套第二真相。
func seedNote(t *testing.T, d *db.DB, id string) notification.Notification {
	t.Helper()
	var created notification.Notification
	svc := notification.NewService()
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		var err error
		created, err = svc.Notify(context.Background(), tx, notification.Draft{
			ID: id, EventID: "ev-" + id, Type: notification.TypeProjectionRejected,
			Priority: notification.PriorityHigh, Title: "被拒投影", TaskID: "task-1",
			PrincipalID: "system",
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func markRead(t *testing.T, d *db.DB, id, eventID string) (notification.Notification, bool, error) {
	t.Helper()
	var (
		out     notification.Notification
		changed bool
	)
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		var err error
		out, changed, err = notification.NewService().MarkRead(context.Background(), tx, id,
			notification.ReadCommand{EventID: eventID, PrincipalID: "local-user", CorrelationID: "c-" + eventID})
		return err
	})
	return out, changed, err
}

func noteState(t *testing.T, d *db.DB, id string) (state string, readAt int64, version int64) {
	t.Helper()
	if err := d.Read.QueryRow(`SELECT state, COALESCE(read_at, 0), version FROM notifications WHERE id = ?`, id).
		Scan(&state, &readAt, &version); err != nil {
		t.Fatal(err)
	}
	return
}

func TestMarkReadTransitionsOnceAndEmitsExactlyOneEvent(t *testing.T) {
	d := openDB(t)
	seedTask(t, d)
	created := seedNote(t, d, "n-read-1")
	if created.State != notification.StateUnread {
		t.Fatalf("前提：新建该是 UNREAD，实际 %s", created.State)
	}

	out, changed, err := markRead(t, d, "n-read-1", "cmd-1")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("UNREAD→READ 该报「真改了」")
	}
	// 返回的是**读回库里那一行**（C18 裁定 ②）：不是我在 Go 里拼出来的那个 struct。
	if out.State != notification.StateRead || out.Version != 2 || out.ReadAt == 0 {
		t.Errorf("返回值该是读回的那行（READ v2 + read_at 有值），实际 %+v", out)
	}
	if out.HandledAt != 0 {
		t.Errorf(":read 不该碰 handled_at，实际 %d", out.HandledAt)
	}
	if st, _, v := noteState(t, d, "n-read-1"); st != "READ" || v != 2 {
		t.Errorf("库里该是 READ v2（一次 Touch 推进一个版本，ADR-017），实际 %s v%d", st, v)
	}
	// 恰一行事件，且 §6 那一格的四个键都在、aggregate_type 是 notification。
	var typ, agg, payload string
	if err := d.Read.QueryRow(`SELECT event_type, aggregate_type, payload_json FROM event_journal WHERE event_type = 'notification_read'`).
		Scan(&typ, &agg, &payload); err != nil {
		t.Fatalf("notification_read 该恰好一行: %v", err)
	}
	if agg != "notification" {
		t.Errorf("aggregate_type 该是 notification（平台的聚合守卫也盯着这一对），实际 %q", agg)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"notification_type", "priority", "task_id", "approval_id"} {
		if _, ok := m[k]; !ok {
			t.Errorf("§6 要求的 payload 键 %s 不在（同族三行共用那一列）: %s", k, payload)
		}
	}
	if m["schema"] != float64(1) {
		t.Errorf("payload 缺 §2 信封的 schema: %s", payload)
	}
	// created 那条事件仍在（读不该把过去抹掉）。
	if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'notification_created'`); n != 1 {
		t.Errorf("notification_created 该还有 1 行，实际 %d", n)
	}
}

func TestMarkReadTwiceIsANoOpThatNeitherBumpsVersionNorEmits(t *testing.T) {
	d := openDB(t)
	seedTask(t, d)
	seedNote(t, d, "n-read-2")

	_, first, err := markRead(t, d, "n-read-2", "cmd-a")
	if err != nil || !first {
		t.Fatalf("首读该成功且报「真改了」: %v changed=%v", err, first)
	}
	_, _, v1 := noteState(t, d, "n-read-2")
	stamp1 := readAtOf(t, d, "n-read-2")

	out, second, err := markRead(t, d, "n-read-2", "cmd-b")
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Error("重复 :read 该报「没改」（C36 裁定 ①：200 no-op，不是 409）")
	}
	if out.State != notification.StateRead || out.Version != v1 {
		t.Errorf("no-op 的返回值该还是库里那一行（READ v%d），实际 %+v", v1, out)
	}
	if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'notification_read'`); n != 1 {
		t.Errorf("第二次该什么都不发，事件却变成 %d 行", n)
	}
	if got := readAtOf(t, d, "n-read-2"); got != stamp1 {
		t.Errorf("no-op 连 read_at 都不许顺手刷新（那会把第一次读的时间冲掉）: %d → %d", stamp1, got)
	}
}

func readAtOf(t *testing.T, d *db.DB, id string) int64 {
	t.Helper()
	var v int64
	if err := d.Read.QueryRow(`SELECT COALESCE(read_at, 0) FROM notifications WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestMarkReadRefusesHandledStates 钉 C36 裁定 ② 那一格：已处置的三态不许被重新标成「读过」。
// 状态是夹具直接改的（这三条边今天没有生产路径），所以断言的重点在**拒了之后什么都没动**。
func TestMarkReadRefusesHandledStates(t *testing.T) {
	for _, st := range []string{"HANDLED", "IGNORED", "EXPIRED"} {
		t.Run(st, func(t *testing.T) {
			d := openDB(t)
			seedTask(t, d)
			seedNote(t, d, "n-x")
			if _, err := d.Read.Exec(`UPDATE notifications SET state = ? WHERE id = ?`, st, "n-x"); err != nil {
				t.Fatal(err)
			}
			_, changed, err := markRead(t, d, "n-x", "cmd-refuse")
			if !errors.Is(err, notification.ErrIllegalTransition) {
				t.Fatalf("%s→READ 该报 ErrIllegalTransition，实际 %v", st, err)
			}
			if changed {
				t.Error("被拒的调用不许报「真改了」")
			}
			if got, _, v := noteState(t, d, "n-x"); got != st || v != 1 {
				t.Errorf("被拒之后库里那行不该动（%s v1），实际 %s v%d", st, got, v)
			}
			if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'notification_read'`); n != 0 {
				t.Errorf("被拒的迁移不发事件，实际 %d 行", n)
			}
		})
	}
}

func TestMarkReadValidatesBeforeTouchingTheRow(t *testing.T) {
	d := openDB(t)
	seedTask(t, d)
	seedNote(t, d, "n-valid")

	if _, _, err := markRead(t, d, "n-valid", "   "); !errors.Is(err, notification.ErrInvalidDraft) {
		t.Errorf("空 event_id 该在入口就拒（重试要复用同一个 id），实际 %v", err)
	}
	if got, _, v := noteState(t, d, "n-valid"); got != "UNREAD" || v != 1 {
		t.Errorf("被拒的调用不留痕，实际 %s v%d", got, v)
	}
	if _, _, err := markRead(t, d, "n-none", "cmd-x"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("未知 id 该透出 sql.ErrNoRows（transport 映成 404），实际 %v", err)
	}
}

// TestPendingCanBecomeRead 是允许集的另一面：PENDING 也算「还没读过」。
// 单独一条是因为它是白名单里的**第二格** —— 只测 UNREAD 的话，把白名单写成一行也会照样绿。
func TestPendingCanBecomeRead(t *testing.T) {
	d := openDB(t)
	seedTask(t, d)
	seedNote(t, d, "n-pending")
	if _, err := d.Read.Exec(`UPDATE notifications SET state = 'PENDING' WHERE id = ?`, "n-pending"); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := markRead(t, d, "n-pending", "cmd-p"); err != nil || !changed {
		t.Fatalf("PENDING→READ 该放行，实际 changed=%v err=%v", changed, err)
	}
	if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'notification_read'`); n != 1 {
		t.Errorf("恰一行 notification_read，实际 %d", n)
	}
}

// TestReadEventTypeNameIsNotInvented 防一种很安静的错：常量值写歪（比如 notifications_read）。
// 注册表那一侧是字符串集合，拼错就是 appendEvent 当场拒 —— 但那发生在**运行期**，
// 所以这里在编译期外面再钉一次：常量必须与 §6 展开后的名字逐字相同。
func TestReadEventTypeNameIsNotInvented(t *testing.T) {
	if notification.EventRead != "notification_read" {
		t.Errorf("事件名漂了: %q", notification.EventRead)
	}
	if !strings.HasPrefix(notification.EventRead, "notification_") {
		t.Error("§1 命名：<domain>_<past-tense-action>")
	}
}
