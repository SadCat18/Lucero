package main

// sendpipe 是 `POST /executions/{id}/send` 的连接半边，住在 cmd/core（装配归我）。
// 09-25 之前它的测试面是空的：api/http 那批 send 用例走的是 `stubTurn`（`send_test.go:413`），
// 证的是"Service 会调那个接口"，证不了"我这一半真能把一次 start_now 送到 Adapter、
// 且送不到时给的是 §15.2 里那三个码而不是 500"。本文件补的就是这一格。
//
// 分两类：四格"到不了子进程"的分岔不需要开关（不 build、不起进程），
// 端到端那一格按同一套 `LUCERO_TEST_FAKE_ADAPTER=1`（与 probe/discover 那两条一致）。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
)

// seedFakeInstance 插一行 key=fake 的实例（specs 里给不给这个 key，决定走 501 还是走连接）。
func seedFakeInstance(t *testing.T, d *db.DB) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ai-fake', 'fake', 'fake', 1, 1)`); err != nil {
		t.Fatal(err)
	}
}

// seedSession 造三种"落点"形状：有远端 id、远端 id 是空串、远端 id 是 NULL。
// 后两种在库里是两件事（`”` 与 `NULL`），但对 send 是同一件事：没有落点。
func seedSession(t *testing.T, d *db.DB, id string, remote any) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, created_at, updated_at)
		VALUES (?, 'ai-fake', ?, 1, 1)`, id, remote); err != nil {
		t.Fatal(err)
	}
}

func TestSendPipeRefusesBeforeSpawningAnything(t *testing.T) {
	// specs 里只有 "other" ⇒ key=fake 的实例是"本机没配起法"那一格。
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "other", displayName: "other", command: "does-not-matter"}})
	seedFakeInstance(t, d)
	seedSession(t, d, "s-remote", "remote-1")
	seedSession(t, d, "s-empty", "")
	seedSession(t, d, "s-null", nil)

	before := countRowsIn(t, d, `SELECT count(*) FROM event_journal`)
	cases := []struct {
		name, sessionID string
		want            error
	}{
		{"没有这条 session", "s-nope", session.ErrSessionNotFound},
		{"远端 id 是空串", "s-empty", execution.ErrSessionNotLinked},
		{"远端 id 是 NULL（与空串同一处置）", "s-null", execution.ErrSessionNotLinked},
		{"实例在但本机没配起法", "s-remote", httpapi.ErrAdapterNotLaunchable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, err := pipe.CallSendTurn(context.Background(), execution.TurnCall{
				ExecutionID: "ex-1", SessionID: c.sessionID, Text: "hi",
				Delivery: "start_now", IdempotencyKey: "k-1",
			})
			if !errors.Is(err, c.want) {
				t.Errorf("该是 %v，实际 %v", c.want, err)
			}
			if rec.Accepted {
				t.Errorf("被挡下的 send 不许回 accepted=true: %+v", rec)
			}
		})
	}
	if n := countRowsIn(t, d, `SELECT count(*) FROM event_journal`); n != before {
		t.Errorf("这四格都该在起连接之前返回，不该写任何东西：事件从 %d 变 %d", before, n)
	}
}

// TestSendPipeNormalizesLaunchFailureIntoUnresponsive 钉"归一"那一腿：
// 起连接/握手/发帧这一路上**不是** `*adapter.Error` 的失败，必须变成 ADAPTER_UNRESPONSIVE（⇒503），
// 既不许漏成 500（transport 的 default 支），也不许和"没配起法"(501) 混成一句。
func TestSendPipeNormalizesLaunchFailureIntoUnresponsive(t *testing.T) {
	bogus := filepath.Join(t.TempDir(), "definitely-not-an-adapter.exe")
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "fake", command: bogus}})
	seedFakeInstance(t, d)
	seedSession(t, d, "s-remote", "remote-1")

	rec, err := pipe.CallSendTurn(context.Background(), execution.TurnCall{
		ExecutionID: "ex-2", SessionID: "s-remote", Text: "hi",
		Delivery: "start_now", IdempotencyKey: "k-2",
	})
	var ae *adapter.Error
	if !errors.As(err, &ae) {
		t.Fatalf("非 adapter 错误必须归一成 *adapter.Error（否则 transport 落 500）: %T %v", err, err)
	}
	if ae.Code != adapter.ErrAdapterUnresponsive {
		t.Errorf("该归一成 ADAPTER_UNRESPONSIVE（503），实际 %v", ae.Code)
	}
	if errors.Is(err, httpapi.ErrAdapterNotLaunchable) {
		t.Errorf("「配了但病了」不许报成「没配起法」（503 与 501 是两件事）: %v", err)
	}
	if rec.Accepted {
		t.Errorf("没送出去的 send 不许 accepted=true: %+v", rec)
	}
}

// TestSendTurnOverRealFakeAdapter 是这一批最硬的一条：真起我们自己的 fake-adapter，
// 走 probe → discover（留下带远端 id 的 session 行）→ 两次真 CallSendTurn。
// 它证的是装配本身（`main.go:212` 那句 `EnableTurn(sendPipe)`）不是接了个跑不通的东西。
func TestSendTurnOverRealFakeAdapter(t *testing.T) {
	if os.Getenv("LUCERO_TEST_FAKE_ADAPTER") != "1" {
		t.Skip("未开 LUCERO_TEST_FAKE_ADAPTER=1：不 build、不起真子进程（交付前必须开着跑一遍）")
	}
	exe := buildFakeAdapterHere(t)
	probe, discover, pipe, d := newBareDiscover(t,
		[]adapterSpec{{key: "fake", displayName: "Fake Adapter", command: exe}})
	ctx := context.Background()

	view, err := probe.Probe(ctx, "fake")
	if err != nil {
		t.Fatalf("probe 该先成功（send 要用 discover 留下的 session 行）: %v", err)
	}
	items, _, err := discover.DiscoverSessions(ctx, view.ID)
	if err != nil || len(items) == 0 {
		t.Fatalf("discover 该留下可用的 session 行: %v (%d 条)", err, len(items))
	}
	sess := items[0].SessionID
	eventsBefore := countRowsIn(t, d, `SELECT COALESCE(max(seq), 0) FROM event_journal`)

	for _, key := range []string{"k-e2e-1", "k-e2e-2"} {
		rec, err := pipe.CallSendTurn(ctx, execution.TurnCall{
			ExecutionID: "ex-e2e", SessionID: sess, Text: "hello",
			Delivery: "start_now", IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("%s: 真 fake-adapter 的 start_now 该被接: %v", key, err)
		}
		if !rec.Accepted {
			t.Errorf("%s: fake-adapter 回了 accepted=false（reason=%q）", key, rec.Reason)
		}
	}
	// 连接面自己不替 Service 记账：send 期间新增的事件行**不许**落在 execution / task 这两个聚合上
	// （`execution_created`、`task_preparing` 那一类只能由 `execution.Service`/`task.Service` 在事务里发）。
	// 会新增行是**预期**的 —— 现起一条连接就是现起一个子进程，生命周期那一腿要记 `process_started`；
	// 所以这里按聚合点名，而不是笼统要求"0 行"（后者会把合法的生命周期记账误判成越界）。
	for _, r := range journalSince(t, d, eventsBefore) {
		if r.aggType == "execution" || r.aggType == "task" {
			t.Errorf("连接面不许替 Service 记账：%s/%s（event=%s）", r.aggType, r.aggID, r.evType)
		}
	}
	t.Logf("两次 send 期间新增的事件行（预期是进程生命周期那一腿，不是 sendpipe 写的）：%v", journalSince(t, d, eventsBefore))
	if n := countRowsIn(t, d, `SELECT count(*) FROM audit_entries`); n != 0 {
		t.Errorf("sendpipe 不许碰审计，实际 %d 行", n)
	}
}

type journalRow struct {
	evType  string
	aggType string
	aggID   string
}

// journalSince 取 seq 大于 upper 的那些事件行（event_journal.seq 是 AUTOINCREMENT，所以"之后新增的"可数）。
func journalSince(t *testing.T, d *db.DB, upper int64) []journalRow {
	t.Helper()
	rows, err := d.Read.Query(`SELECT event_type, aggregate_type, aggregate_id FROM event_journal WHERE seq > ? ORDER BY seq`, upper)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []journalRow
	for rows.Next() {
		var r journalRow
		if err := rows.Scan(&r.evType, &r.aggType, &r.aggID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
