package session

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// seedSync 摆三条形状不同的 Session（都要真外键行，所以先建 adapter_instances）：
// s-data 有消息也有游标（只读链能走）、s-never 只有消息没有游标（从未同步过）、
// s-empty 两样都没有（C25 那一支"完全无 Provider"），s-high 的库里置信度是 HIGH（用来验降级喊法）。
func seedSync(t *testing.T, raw *sql.DB) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := raw.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ad-1', 'codex', 'Codex', 1, 1)`)
	for _, s := range []struct{ id, state, cursor, confidence string }{
		{"s-data", "SYNCED", "cur-77", "HIGH"},
		{"s-never", "UNKNOWN", "", "UNKNOWN"},
		{"s-empty", "UNKNOWN", "", "UNKNOWN"},
		{"s-high", "SYNCED", "cur-9", "HIGH"},
	} {
		var cursor any // 空串在这里就是"库里写 NULL"，不是长度为零的游标
		if s.cursor != "" {
			cursor = s.cursor
		}
		exec(`INSERT INTO sessions (id, adapter_instance_id, sync_state, sync_cursor, state_confidence,
			created_at, updated_at) VALUES (?, ?, ?, ?, ?, 1, 1)`, s.id, "ad-1", s.state, cursor, s.confidence)
	}
	exec(`INSERT INTO messages (id, session_id, role, kind, sequence_no, text_content, created_at)
		VALUES ('m-1', 's-data', 'USER', 'MESSAGE', 1, '第一句', 1)`)
	exec(`INSERT INTO messages (id, session_id, role, kind, sequence_no, text_content, created_at)
		VALUES ('m-2', 's-never', 'USER', 'MESSAGE', 1, '从未同步过的那条', 1)`)
	exec(`INSERT INTO messages (id, session_id, role, kind, sequence_no, text_content, created_at)
		VALUES ('m-4', 's-high', 'USER', 'MESSAGE', 1, '置信度是昨天的高', 1)`)
}

func TestSyncFromLocalDecidesTheThreeWays(t *testing.T) {
	ctx := context.Background()
	raw := openSchema(t)
	seedSync(t, raw)
	svc := NewService(NewRepository(raw))

	// ① 只读链能走：降级表态 + 原样回显库里的位置。
	got, err := svc.SyncFromLocal(ctx, "s-data")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != SyncDegraded || got.Cursor == nil || *got.Cursor != "cur-77" {
		t.Errorf("只读链那一支不对: %+v", got)
	}
	// ② 库里置信度是 HIGH 也不许端出去：这次回答只读了本地，§49 说推断不能标 HIGH。
	if got.Confidence != ConfidenceLow {
		t.Errorf("降级还报 %q，置信度该是 LOW", got.Confidence)
	}
	// ③ 有消息但从未同步过：位置该是 nil（出口就是 null = 下次从头拉），不是空串。
	never, err := svc.SyncFromLocal(ctx, "s-never")
	if err != nil {
		t.Fatal(err)
	}
	if never.Cursor != nil {
		t.Errorf("从未同步过的游标该是 nil，实际 %q", *never.Cursor)
	}
	// ④ 两样都没有 = 没有能给你的东西，也没有 Provider 可问。
	if _, err := svc.SyncFromLocal(ctx, "s-empty"); !errors.Is(err, ErrNoLocalData) {
		t.Errorf("空会话该报 ErrNoLocalData，实际 %v", err)
	}
	// ⑤ 对象不存在与"没有内容"是两件事（404 与 503 的分别就落在这条线上）。
	if _, err := svc.SyncFromLocal(ctx, "s-nope"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("不存在的 Session 该报 ErrSessionNotFound，实际 %v", err)
	}
}

// TestAdapterDowngradedCannotRideASessionTouch 是"503 这一支今天发不出 `adapter_downgraded`"的现场证据：
// 事件契约 §6 给这个事件的 aggregate_type 是 `adapter`，而 platform/db 的守卫要求事件上写的聚合
// **等于被 Touch 的那个聚合** —— 从我这条腿（Session）发出去，要么被守卫拒，要么把事件挂到 session 名下错标归属。
// 真要如实发，得去 Touch `adapter_instances`：那是别人家的聚合行，还附带一次版本推进，不在我的写入范围里。
// 所以 C25 那句"503 + 同时发事件"今天只能落一半，剩下半格已按 REQ-B-1 报主控。
func TestAdapterDowngradedCannotRideASessionTouch(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := db.InitRedaction([]string{"token"}, nil, ""); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
			VALUES ('ad-1', 'codex', 'Codex', 1, 1)`,
		`INSERT INTO sessions (id, adapter_instance_id, sync_state, created_at, updated_at)
			VALUES ('s-1', 'ad-1', 'UNKNOWN', 1, 1)`,
	} {
		if _, err := d.Read.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	err = d.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.Touch(db.AggSession, "s-1", "sync_state = ?", []any{"DEGRADED"}, db.EventDraft{
			EventID: "ev-1", Type: "adapter_downgraded", Aggregate: db.AggAdapter, AggregateID: "ad-1",
		})
		return err
	})
	if err == nil {
		t.Fatal("守卫竟然放行了：那 `adapter_downgraded` 就挂在 session 聚合名下了，这是错标归属")
	}
	if !strings.Contains(err.Error(), `aggregate_type "adapter" but touching "session"`) {
		t.Errorf("红的理由该是聚合归属不符，实际 %v", err)
	}
	// 同一笔事务真的没留下任何东西：守卫炸掉不能把 sync_state 改成 DEGRADED 再回滚失败。
	var state string
	if err := d.Read.QueryRow(`SELECT sync_state FROM sessions WHERE id = 's-1'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "UNKNOWN" {
		t.Errorf("那笔被拒的写留下了痕迹：sync_state=%q", state)
	}
}
