package main

// TestNoTurnFrameIsNotTheSameAsWedge 是 T-381③ⅱ 要的那一刀：**「回执到了没帧」与「对端楔住」
// 是两种状态**，混了会把崩溃恢复那一格测成假象（主控原话）。两腿走**同一套 handler 装配**，
// 差别只许来自被验事实本身：
//
//	真 producer（帧真到了，表内码） ⇒ 202 + 已密封(COMPLETED) + execution 落 SUCCEEDED +
//	                                task 同笔推到 WAITING_ACCEPTANCE + 不喊表外的码
//	对端楔住（连回执都没有）       ⇒ 503 + 不密封(IN_PROGRESS) + execution 留 RUNNING +
//	                                不落任何终态事件 + 不喊 + 耗时贴着 10 秒
//
// **第一腿的判据本批改过（第 296 条 ①(a)，上一版是第 293 条写的）**：原文断「真 producer 只回回执、
// 不发帧 ⇒ execution 留 RUNNING」，而 `cmd/fake-adapter/main.go` 09-27 17:09:44（CO-54 丙落）之后
// `start_now` 那一路**真会发 `turn/finished`（SUCCEEDED）** ⇒ 那条判据的前提过期，红在
// 「留 RUNNING」「一条终态事件都不许有」两行（实测 SUCCEEDED、2 行）。**我改判据不改 adapter**：
// adapter 的文件不是我的，我也不为绿去挪它。
//
// 「真帧没到」这个状态本用例**不再拿真 producer 造**（它现在造不出来了），那一格的覆盖在
// `sendreap_test.go` 的三分表（拿替身帧喂：没帧不喊 / 表外码喊 / 表内码不喊）与本用例第二腿
// （没回执也没帧）。要新增覆盖是单独一格，不夹在这里。

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNoTurnFrameIsNotTheSameAsWedge(t *testing.T) {
	if os.Getenv("LUCERO_TEST_FAKE_ADAPTER") != "1" {
		t.Skip("未开 LUCERO_TEST_FAKE_ADAPTER=1：不 build、不起真子进程（交付前必须开着跑一遍）")
	}

	t.Run("真producer发了收尾帧就落终态", func(t *testing.T) {
		exe := buildFakeAdapterHere(t)
		ctx := context.Background()
		probe, discover, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "fake", command: exe}})
		view, err := probe.Probe(ctx, "fake")
		if err != nil {
			t.Fatalf("probe 真 fake-adapter 失败：%v", err)
		}
		items, _, err := discover.DiscoverSessions(ctx, view.ID)
		if err != nil || len(items) == 0 {
			t.Fatalf("discover 该留下真会话：%v（%d 条）", err, len(items))
		}
		sess := items[0].SessionID
		var instanceID string
		if err := d.Read.QueryRow(`SELECT adapter_instance_id FROM sessions WHERE id=?`, sess).Scan(&instanceID); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, status, created_at, updated_at)
			VALUES ('task-rf', '真 producer 这一格', 'RUNNING', 1, 1)`); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, attempt_no,
			status, state_confidence, trigger_type, created_at, updated_at)
			VALUES ('ex-rf', 'task-rf', ?, ?, 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`, instanceID, sess); err != nil {
			t.Fatal(err)
		}

		tap := &logTap{}
		pipe.logf = tap.f // sendpipe 那句「未裁的码」只走 logf，不收集就断不到
		srv := stallServe(t, d, pipe)
		defer srv.Close()

		got := stallPost(t, srv.URL+"/api/v1/executions/ex-rf/send",
			`{"text":"真 producer 的 start_now","delivery":"start_now"}`, "k-rf", 25*time.Second)
		if got.err != nil {
			t.Fatalf("这一发根本发不出去：%v", got.err)
		}
		if got.code != http.StatusAccepted {
			t.Errorf("回执照给 ⇒ 该 202（503 是「楔住」那一腿的答案，两态不许互串），实际 %d %s", got.code, got.body)
		}
		if st, stored := stallIdem(t, d, "k-rf"); st != "COMPLETED" || stored == "" {
			t.Errorf("投递成功就该密封成响应，实际 state=%s body=%q", st, stored)
		}
		// 帧到了 + 表内码 ⇒ 第二笔必须落终态。上一版在这里断「留 RUNNING」，
		// 那句「真 producer 不发帧」的前提已随 CO-54 丙（09-27 17:09:44）过期 —— 见本用例顶注。
		if st := stallStatus(t, d, `SELECT status FROM executions WHERE id='ex-rf'`); st != "SUCCEEDED" {
			t.Errorf("真帧带表内码 ⇒ 该落终态 SUCCEEDED，实际 %s", st)
		}
		// 表内码不喊：喊是「码没裁过」那一格的形状（三分表在 sendreap_test.go），这里只钉它不外溢。
		if n := tap.count("未裁"); n != 0 {
			t.Errorf("表内码不该喊未裁，实际 %d 句：%v", n, tap.lines)
		}
		// 终态三格成套：execution 终态 + task 投影 + 同笔推进到 WAITING_ACCEPTANCE（契约④-c）。
		// 上一版在这里断「一行都不许有」，同一条过期前提 ⇒ 一起改；少哪一格都是「落了一半」。
		for _, want := range []struct {
			typ string
			n   int
		}{
			{"execution_succeeded", 1},
			{"task_agent_completed", 1},
			{"task_waiting_acceptance", 1},
		} {
			if got := stallCount(t, d, `SELECT count(*) FROM event_journal WHERE event_type='`+want.typ+`'`); got != want.n {
				t.Errorf("%s 该恰 %d 行，实际 %d 行", want.typ, want.n, got)
			}
		}
		if st := stallStatus(t, d, `SELECT status FROM tasks WHERE id='task-rf'`); st != "WAITING_ACCEPTANCE" {
			t.Errorf("同笔推进该把 task 推到 WAITING_ACCEPTANCE，实际 %s", st)
		}
	})

	t.Run("对端楔住连回执都没有", func(t *testing.T) {
		srv, d, pid, _, tap := stallFixtureTap(t, "silent")
		got := stallPost(t, srv.URL+"/api/v1/executions/ex-st/send",
			`{"text":"楔住","delivery":"start_now"}`, "k-wg", 20*time.Second)
		if got.err != nil {
			t.Fatalf("这一发根本发不出去：%v", got.err)
		}
		if got.code != http.StatusServiceUnavailable || !strings.Contains(got.body, "adapter_unresponsive") {
			t.Errorf("楔住该 503 adapter_unresponsive，实际 %d %s", got.code, got.body)
		}
		if st, _ := stallIdem(t, d, "k-wg"); st != "IN_PROGRESS" {
			// 与上一腿的差别就在这一格（COMPLETED vs IN_PROGRESS）—— 不是"有没有帧"，
			// 把两格读成一格就是把两态读成一态。
			t.Errorf("楔住不许密封（要留给同键续跑），实际 state=%s", st)
		}
		if n := tap.count("未裁"); n != 0 {
			t.Errorf("楔住这一路同样没收到帧 ⇒ 不该喊，实际 %d 句：%v", n, tap.lines)
		}
		if got.el < 9*time.Second || got.el > 18*time.Second {
			t.Errorf("这一腿该被 sendAttemptDeadline(10s) 界住，实际 %v（太短=没走到连接面；太长=没界住）", got.el)
		}
		requireProcessGone(t, *pid)
	})
}
