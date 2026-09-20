package task_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

// TestRepositoryReadsWhatTouchWrote 证明只读侧与写侧看到的是同一份真相：
// db.Create 建首行 → v1；一次 Touch 改状态 → v2；Repository 读回来必须一字不差。
func TestRepositoryReadsWhatTouchWrote(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	const id = "task-1"
	now := time.Now().UnixMilli()

	if err := writeTask(t, d, ctx, id, now); err != nil {
		t.Fatal(err)
	}
	repo := task.NewRepository(d.Read)

	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.Queued || got.Version != 2 {
		t.Errorf("读回 %s v%d，应为 QUEUED v2", got.Status, got.Version)
	}
	if got.Title != "跑通垂直链" || got.Priority != "HIGH" || got.Mode != "EXECUTE" {
		t.Errorf("标量列读错: %+v", got)
	}
	// 没写过的可空列必须是空/0，不能被造出值；也不能被折算成 FAILED。
	if got.ProjectID != "" || got.ParentTaskID != "" || got.Description != "" || got.RequestedBy != "" {
		t.Errorf("NULL 列被造出了值: %+v", got)
	}
	if got.AcceptedAt != 0 || got.Archived {
		t.Errorf("accepted_at/archived_at 读错: %+v", got)
	}

	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("缺行应透传 sql.ErrNoRows，实际 %v", err)
	}

	if q, err := repo.List(ctx, task.Filter{Status: task.Queued, Limit: 10}); err != nil || len(q) != 1 || q[0].ID != id {
		t.Errorf("ListByStatus(QUEUED) = %v %v", q, err)
	}
	if r, err := repo.List(ctx, task.Filter{Status: task.Running, Limit: 10}); err != nil || len(r) != 0 {
		t.Errorf("没有 RUNNING 的任务，ListByStatus 却返回 %v (%v)", r, err)
	}
	counts, err := repo.StatusCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts[task.Queued] != 1 || len(counts) != 1 {
		t.Errorf("StatusCounts = %v，应为 {QUEUED:1}", counts)
	}
}

// TestRepositoryKeepsUnknownReadable 守 §57/§12 的读侧半边：
// 状态是 UNKNOWN 时读回来还是 UNKNOWN，不能被折算成 FAILED，accepted_at 也不能被当成 0 就丢信息。
// 这里用裸 SQL 播种，因为按 §5.5 正常运行根本没有"进入 UNKNOWN"的路径。
func TestRepositoryKeepsUnknownReadable(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, status, created_at, updated_at, accepted_at)
		VALUES ('u-1', '状态不明', 'UNKNOWN', 1, 1, 555)`); err != nil {
		t.Fatal(err)
	}
	got, err := task.NewRepository(d.Read).Get(ctx, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.Unknown {
		t.Errorf("UNKNOWN 被读成了 %s", got.Status)
	}
	if got.AcceptedAt != 555 {
		t.Errorf("accepted_at 被折算成 %d", got.AcceptedAt)
	}
	if task.Terminal(got.Status) {
		t.Error("UNKNOWN 不是终态，恢复引擎要能带证据离开它")
	}
}

// TestPayloadLandsInJournalWithSchema 把 §2 的 "schema":1 保证钉在真库上：
// 业务包故意不注入这个键（db 已确认平台层缺键就补），所以要有人证明补到了库里。
func TestPayloadLandsInJournalWithSchema(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	const id = "task-9"
	now := time.Now().UnixMilli()
	m, err := task.Payload(task.CreatedPayload{Title: "跑通垂直链", Priority: "HIGH",
		Mode: "EXECUTE", RiskLevel: "LOW", ProjectID: "p-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Write(ctx, func(tx *db.Tx) error {
		if _, err := tx.Append("principals",
			[]string{"id", "principal_type", "display_name", "created_at", "updated_at"},
			[]any{"system", "SYSTEM", "Core", now, now}); err != nil {
			return err
		}
		return tx.Create(db.AggTask, id,
			[]string{"title", "created_at", "updated_at"},
			[]any{"跑通垂直链", now, now},
			db.EventDraft{EventID: "ev-payload-1", Type: task.EventCreated,
				Aggregate: db.AggTask, AggregateID: id, PrincipalID: "system", Payload: m})
	}); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE aggregate_id = ?`, id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema":1`, `"title":"跑通垂直链"`, `"priority":"HIGH"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload_json 缺 %s，实际 %s", want, payload)
		}
	}
}

func writeTask(t *testing.T, d *db.DB, ctx context.Context, id string, now int64) error {
	t.Helper()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		if _, err := tx.Append("principals",
			[]string{"id", "principal_type", "display_name", "created_at", "updated_at"},
			[]any{"system", "SYSTEM", "Core", now, now}); err != nil {
			return err
		}
		return tx.Create(db.AggTask, id,
			[]string{"title", "priority", "mode", "created_at", "updated_at"},
			[]any{"跑通垂直链", "HIGH", "EXECUTE", now, now},
			db.EventDraft{EventID: "ev-1", Type: task.EventCreated,
				Aggregate: db.AggTask, AggregateID: id, PrincipalID: "system"})
	}); err != nil {
		return err
	}
	return d.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.Touch(db.AggTask, id, "status = ?, updated_at = ?",
			[]any{string(task.Queued), now},
			db.EventDraft{EventID: "ev-2", Type: task.EventQueued,
				Aggregate: db.AggTask, AggregateID: id, PrincipalID: "system"})
		return err
	})
}
