package task_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
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
	// 脱敏是 fail-closed 的：没 InitRedaction 之前任何写入都被拒（内部契约 §2.2）。
	// 两处与契约不一致，都记进了 progress：
	//   C13 实际签名是 db.InitRedaction(denyKeys, patterns, marker)，不是 §2.2 的 InitRedaction(audit.RedactionRules)；
	//   C14 这里故意不 import internal/audit 拿规则集——§2.3 规定这条接线属于 cmd/core，
	//       业务包（含我的测试）一旦 import 它，就会被别的角色在途的编译错误带红。
	//       权威规则集仍是 audit.Rules()，本包不测脱敏。
	rules := []string{"token"}
	if err := db.InitRedaction(rules, nil, "[REDACTED]"); err != nil {
		t.Fatal(err)
	}
	return d
}

func countEvents(t *testing.T, d *db.DB, aggregateID string) int64 {
	t.Helper()
	var n int64
	// 按 §5.4 更正后的口径限定 aggregate_type=task：Notification 是独立聚合，
	// 它那一行 notification_created 本来就该出现。
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE aggregate_type = 'task' AND aggregate_id = ?`,
		aggregateID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func getTask(t *testing.T, d *db.DB, id string) (task.State, int64) {
	t.Helper()
	var status string
	var version int64
	if err := d.Read.QueryRow(`SELECT status, version FROM tasks WHERE id = ?`, id).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	return task.State(status), version
}

// TestRejectedProjectionWritesNoTaskEvent 是 §5.4 第 1 条的真表断言：
// 投影被拒时 tasks 行既不改状态也不取版本，event_journal 里属于该 Task 的事件零新增；
// 对照组走一次合法投影，证明"会写"的那条路真的在写（不然前一条是空断言）。
func TestRejectedProjectionWritesNoTaskEvent(t *testing.T) {
	cases := []struct {
		name         string
		from         task.State
		edge         string
		handoffReady bool
		wantEvents   int64
		wantStatus   task.State
		wantVersion  int64
	}{
		{"§5.4 无合法目标", task.Preparing, task.EdgeFailed, true, 0, task.Preparing, 1},
		{"对照：合法投影", task.Preparing, task.EdgeStarted, false, 1, task.Running, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			d := openDB(t)
			const id = "task-1"
			now := time.Now().UnixMilli()

			err := d.Write(ctx, func(tx *db.Tx) error {
				// event_journal.principal_id 有外键指向 principals，先建主体。
				if _, err := tx.Append("principals",
					[]string{"id", "principal_type", "display_name", "created_at", "updated_at"},
					[]any{"system", "SYSTEM", "Core", now, now}); err != nil {
					return err
				}
				return tx.Create(db.AggTask, id,
					[]string{"title", "status", "created_at", "updated_at"},
					[]any{"跑通垂直链", string(c.from), now, now},
					db.EventDraft{EventID: "ev-create-" + id, Type: task.EventCreated,
						Aggregate: db.AggTask, AggregateID: id, PrincipalID: "system"})
			})
			if err != nil {
				t.Fatal(err)
			}
			before := countEvents(t, d, id)
			if status, version := getTask(t, d, id); status != c.from || version != 1 {
				t.Fatalf("造数据阶段就该是 %s v1，实际 %s v%d", c.from, status, version)
			}

			// 这一段就是写侧 Service 将来要做的事：先裁决，被拒则一个字节都不写聚合。
			var rejected task.Rejection
			err = d.Write(ctx, func(tx *db.Tx) error {
				to, ok, rej := task.ProjectTaskState(c.from, c.edge, c.handoffReady, true)
				if !ok {
					rejected = rej
					return nil
				}
				_, err := tx.Touch(db.AggTask, id, "status = ?, updated_at = ?",
					[]any{string(to), now},
					db.EventDraft{EventID: "ev-move-" + id, Type: task.EventRunning,
						Aggregate: db.AggTask, AggregateID: id, PrincipalID: "system"})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}

			if got := countEvents(t, d, id) - before; got != c.wantEvents {
				t.Errorf("该 Task 的 event_journal 新增 %d 条，§5.4 要求 %d 条", got, c.wantEvents)
			}
			status, version := getTask(t, d, id)
			if status != c.wantStatus || version != c.wantVersion {
				t.Errorf("落库结果 = %s v%d，期望 %s v%d", status, version, c.wantStatus, c.wantVersion)
			}
			if c.wantEvents == 0 {
				if rejected.Reason == "" || rejected.TaskState != c.from || rejected.ExecutionEvent != c.edge {
					t.Errorf("被拒的投影没交出 §5.4 要求的记录内容: %+v", rejected)
				}
			} else if rejected.Reason != "" {
				t.Errorf("放行的投影却给出了拒绝记录: %+v", rejected)
			}
		})
	}
}
