package execution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/task"
)

// 甲（T-370①）落点 (a) 的判据：`turn/finished` 的 result_code 只有被裁过的那三个能落终态，
// 其余一律"一行都不写并且喊"。这一格要防的只有一件事：**把收到帧折成成功**。

// finishFixture 把两条聚合开到"能收终态"的那一格：execution RUNNING v3、task RUNNING v2。
// 为什么用裸 SQL 播这半程：本包测的是"收尾落成什么"，不是"怎么跑起来"（那是 send 腿第 285 条的判据），
// 而 task 的 RUNNING 属于**另一个聚合的既有事实**。
func finishFixture(t *testing.T, d *db.DB, execID string) {
	t.Helper()
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Read.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	must(`UPDATE tasks SET status='RUNNING' WHERE id='task-1'`)
	must(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, created_at, updated_at)
		VALUES ('sess-f', 'ad-codex', 'remote-f', 1, 1)`)
	must(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, 'task-1', 'ad-codex', 'sess-f', 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`, execID)
}

func finishSvc() *execution.Service {
	s := execution.NewService()
	s.EnableSend(task.NewService(nil), nil) // 投影器要，落消息口这一格不用（不写 messages）
	return s
}

func TestFinishFromTurnRefusesUnmappedCodes(t *testing.T) {
	d := openDB(t)
	seedWriteFixture(t, d)
	finishFixture(t, d, "ex-f1")
	svc := finishSvc()

	for _, code := range []string{"OK", "", "succeeded", "INTERRUPTED", "TIMEOUT", "SUCCEEDED "} {
		before := rowsOf(t, d, `SELECT count(*) FROM event_journal`)
		var gotErr error
		if err := d.Write(context.Background(), func(tx *db.Tx) error {
			_, _, err := svc.FinishFromTurn(context.Background(), tx, "ex-f1", code, execution.Command{
				EventID: "ev-x", TaskEventID: "ev-t", PrincipalID: "local-user",
			})
			gotErr = err
			return errors.New("rollback") // 无论域怎么写，这一趟都不许留在库里
		}); err == nil {
			t.Fatalf("这一笔本该回滚（域没喊就是成功）：code=%q", code)
		}
		if !errors.Is(gotErr, execution.ErrUnmappedResultCode) {
			t.Errorf("code=%q 该喊 ErrUnmappedResultCode，实际 %v", code, gotErr)
		}
		if n := rowsOf(t, d, `SELECT count(*) FROM event_journal`); n != before {
			t.Errorf("code=%q 未裁过却写了 %d 行事件（前 %d 后 %d）", code, n-before, before, n)
		}
		if st := execStatus(t, d, "ex-f1"); st != "RUNNING" {
			t.Errorf("code=%q 未裁过却把 execution 推进了，实际 %s", code, st)
		}
	}
}

// TestMappedPredicateAgreesWithTheAction 钉"表只有一份"：谓词说可映射 ⇔ 动作不喊未裁。
// 少了这一腿，将来有人在装配层加第二个字符串表就看不住（边界一）。
func TestMappedPredicateAgreesWithTheAction(t *testing.T) {
	d := openDB(t)
	seedWriteFixture(t, d)
	finishFixture(t, d, "ex-f2")
	svc := finishSvc()

	for _, code := range []string{"SUCCEEDED", "FAILED", "CANCELLED", "OK", "", "interrupted"} {
		mapped := execution.MappedTurnResultCode(code)
		var gotErr error
		_ = d.Write(context.Background(), func(tx *db.Tx) error {
			_, _, err := svc.FinishFromTurn(context.Background(), tx, "ex-f2", code, execution.Command{
				EventID: "ev-p-" + code, TaskEventID: "ev-pt-" + code,
				TaskWaitingAcceptanceEventID: "ev-pw-" + code, PrincipalID: "local-user",
			})
			gotErr = err
			return errors.New("rollback")
		})
		if mapped == errors.Is(gotErr, execution.ErrUnmappedResultCode) {
			t.Errorf("code=%q 上谓词与动作不一致：Mapped=%v 而动作 err=%v", code, mapped, gotErr)
		}
	}
}

func TestFinishFromTurnLandsTerminalAndProjectsTask(t *testing.T) {
	cases := []struct {
		code, wantExec, wantTask, wantExecEvent string
		wantTaskEvent                           string
	}{
		// SUCCEEDED 那一格的 task 落点是 WAITING_ACCEPTANCE 而不是 AGENT_COMPLETED：
		// 契约④-c 之后后者只是同一笔事务里的中间态（半格不许留在库里），见下面 task_waiting_acceptance 的判据。
		{"SUCCEEDED", "SUCCEEDED", "WAITING_ACCEPTANCE", "execution_succeeded", "task_agent_completed"},
		{"FAILED", "FAILED", "FAILED", "execution_failed", ""},
		{"CANCELLED", "CANCELLED", "CANCELLED", "execution_cancelled", ""},
	}
	for _, tc := range cases {
		d := openDB(t)
		seedWriteFixture(t, d)
		id := "ex-" + tc.code
		finishFixture(t, d, id)
		svc := finishSvc()

		var already bool
		if err := d.Write(context.Background(), func(tx *db.Tx) error {
			_, af, err := svc.FinishFromTurn(context.Background(), tx, id, tc.code, execution.Command{
				EventID: "ev-" + tc.code, TaskEventID: "evt-" + tc.code,
				TaskWaitingAcceptanceEventID: "evt-wait-" + tc.code, PrincipalID: "local-user",
			})
			already = af
			return err
		}); err != nil {
			t.Fatalf("%s: 落终态失败：%v", tc.code, err)
		}
		if already {
			t.Errorf("%s: 第一次就报了 alreadyFinished", tc.code)
		}
		if st := execStatus(t, d, id); st != tc.wantExec {
			t.Errorf("execution 该落成 %s，实际 %s", tc.wantExec, st)
		}
		if n := rowsOf(t, d, `SELECT count(*) FROM event_journal WHERE event_type=? AND aggregate_id=?`, tc.wantExecEvent, id); n != 1 {
			t.Errorf("§6 事件 %s 该恰一行，实际 %d", tc.wantExecEvent, n)
		}
		// payload 里的 result_code 必须是帧里那个原话（不翻译）。
		var rc string
		if err := d.Read.QueryRow(`SELECT json_extract(payload_json,'$.result_code') FROM event_journal
			WHERE event_type=? AND aggregate_id=?`, tc.wantExecEvent, id).Scan(&rc); err != nil {
			t.Fatal(err)
		}
		if rc != tc.code {
			t.Errorf("payload 的 result_code 该是帧里的原话 %q，实际 %q", tc.code, rc)
		}
		tst := taskStatus(t, d, "task-1")
		if tst != tc.wantTask {
			t.Errorf("%s: task 该投影成 %s，实际 %s", tc.code, tc.wantTask, tst)
		}
		if tc.wantTaskEvent == "" {
			if n := rowsOf(t, d, `SELECT count(*) FROM event_journal WHERE event_type='task_agent_completed'`); n != 0 {
				t.Errorf("%s 不该驱动 AGENT_COMPLETED，实际发了 %d 行", tc.code, n)
			}
		} else if n := rowsOf(t, d, `SELECT count(*) FROM event_journal WHERE event_type=?`, tc.wantTaskEvent); n != 1 {
			t.Errorf("task 侧事件 %s 该恰一行，实际 %d", tc.wantTaskEvent, n)
		}
		// 契约④-c：链尾那条只在"投影落定 AGENT_COMPLETED"这一格同笔发生，且 payload 带 execution_id。
		wantWait := 0
		if tc.wantTask == "WAITING_ACCEPTANCE" {
			wantWait = 1
		}
		if n := rowsOf(t, d, `SELECT count(*) FROM event_journal WHERE event_type='task_waiting_acceptance'`); n != wantWait {
			t.Errorf("%s: task_waiting_acceptance 该 %d 行（只有 SUCCEEDED 那一格同笔推进），实际 %d",
				tc.code, wantWait, n)
		}
		if wantWait == 1 {
			var got string
			if err := d.Read.QueryRow(`SELECT json_extract(payload_json,'$.execution_id')
				FROM event_journal WHERE event_type='task_waiting_acceptance'`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != id {
				t.Errorf("task_waiting_acceptance.payload.execution_id 该是 %q，实际 %q", id, got)
			}
		}
	}
}

// TestFinishFromTurnReplayDoesNotDoubleWrite 是幂等那一腿：同一帧再来一次 ⇒ alreadyFinished=true、
// 事件一行都不许多。少了这条，续跑路径就会在"已终态"上再补一条 execution_succeeded（版本冲突或假重复）。
func TestFinishFromTurnReplayDoesNotDoubleWrite(t *testing.T) {
	d := openDB(t)
	seedWriteFixture(t, d)
	finishFixture(t, d, "ex-replay")
	svc := finishSvc()

	run := func(ev string) bool {
		already := false
		if err := d.Write(context.Background(), func(tx *db.Tx) error {
			_, af, err := svc.FinishFromTurn(context.Background(), tx, "ex-replay", "SUCCEEDED", execution.Command{
				EventID: ev, TaskEventID: ev + "-t", TaskWaitingAcceptanceEventID: ev + "-w",
				PrincipalID: "local-user",
			})
			already = af
			return err
		}); err != nil {
			t.Fatalf("第二趟该按幂等处理，实际 %v", err)
		}
		return already
	}
	if first := run("ev-1"); first {
		t.Fatal("第一趟不该是 alreadyFinished")
	}
	before := rowsOf(t, d, `SELECT count(*) FROM event_journal`)
	if second := run("ev-2"); !second {
		t.Error("同一终态再来一次该报 alreadyFinished=true")
	}
	if n := rowsOf(t, d, `SELECT count(*) FROM event_journal`); n != before {
		t.Errorf("重放多发或改了 %d 行事件", n-before)
	}
	if n := rowsOf(t, d, `SELECT count(*) FROM notifications WHERE notification_type='projection_rejected'`); n != 0 {
		t.Errorf("幂等这一路不该产生拒绝记录，实际 %d 行", n)
	}
}

func execStatus(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(`SELECT status FROM executions WHERE id=?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func taskStatus(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(`SELECT status FROM tasks WHERE id=?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}
