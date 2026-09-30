package main

// 判据 (a)：契约① 第 25 条 ④-b 那句"回执已返回、连接未关、延迟 `turn/finished` 到达 ⇒ execution
// 转终态 + 事件"（REQ-TURN-1 裁「Ⅰ」，T-391 派给本批的唯一一条）。
//
// **这一条是注入驱动的**（T-387② 要的写法，名字与注释都说明这一点）：它用 fake-adapter 的
// `debug/hold`（§10 第五个注入端点，T-386① 授权、adapter 已落地）把"收尾帧"扣在**回执之后**，
// 再用 `debug/hold on:false` 放出来。它**不证**"真 Provider 的帧一定会在这个窗口内到"，
// 它证的是 Core 这一侧的形状：连接不随回执关闭、泵还在收、迟到那份收尾真的落进库。
// 契约② §6 第 6 条推论③（`start_now` 必须发收尾帧、它的常态位置是回执之后）里"必须发"那一半
// 是 producer 的义务，由 `adapter` 交；这一条只接得住它已经交的那一半。
//
// 为什么"没开 LUCERO_TEST_FAKE_ADAPTER=1 就 SKIP"不等于这一条没落：这条判据要的是**真进程 +
// 真 stdio 管道 + 真泵**，替身帧造不出"连接还开着"这件事（那正是被验的对象）。
// 判决表（写什么/喊什么/什么都不做）在 `turnpump_test.go`，两条用例各管一层，不许互相冒充。
//
// 判据 (b) 不在这里写用例 —— 它是**对本文件这条判据做一次变异**（把连接改回 per-send、回执即
// `Close` ⇒ (a) 必须红），做法与读数记在台账；"不做出这条就是没牙"是主控 T-391② 的原话。

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
)

func TestPumpLandsTheLateTurnFrameOverRealAdapter(t *testing.T) {
	if os.Getenv("LUCERO_TEST_FAKE_ADAPTER") != "1" {
		t.Skip("未开 LUCERO_TEST_FAKE_ADAPTER=1：不 build、不起真子进程（这条判据必须开着跑）")
	}
	exe := buildFakeAdapterHere(t)
	probe, discover, pipe, d := newBareDiscover(t,
		[]adapterSpec{{key: "fake", displayName: "Fake Adapter", command: exe}})
	tap := &logTap{}
	pipe.hub.logf = tap.f // 泵那几声"落库了/没落成"只有从这里能看见

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
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
		VALUES ('task-late', '迟到收尾这一格', 'RUNNING', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES ('ex-late', 'task-late', ?, ?, 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`, instanceID, sess); err != nil {
		t.Fatal(err)
	}

	// 先让泵把连接起来（这就是"常驻"的含义：连接不是某一发 send 的私产），再把收尾帧扣住。
	conn, err := pipe.hub.connection(ctx, "fake")
	if err != nil {
		t.Fatalf("起常驻连接失败：%v", err)
	}
	if _, err := conn.Call(ctx, "debug/hold", map[string]any{"on": true}); err != nil {
		t.Fatalf("武装 debug/hold 失败（这一条判据的地基）：%v", err)
	}

	srv := stallServe(t, d, pipe)
	defer srv.Close()
	got := stallPost(t, srv.URL+"/api/v1/executions/ex-late/send",
		`{"text":"迟到收尾这一格","delivery":"start_now"}`, "k-late-1", 20*time.Second)
	if got.err != nil {
		t.Fatalf("这一发根本发不出去：%v", got.err)
	}
	if got.code != http.StatusAccepted {
		t.Fatalf("回执那一拍该是 202（判据 (c) 的零外溢就在这一格：202 语义一字不动），实际 %d %s", got.code, got.body)
	}

	// ── (a) 的前半：**回执已经返回，而终态还没落**。这一读是"迟到"两个字的证据本身 ——
	// 没有它，后面那句"落成了"完全可能是回执之前就到的帧写的（那就什么都没证）。
	if st := pumpStatus(t, d, `SELECT status FROM executions WHERE id='ex-late'`); st != "RUNNING" {
		t.Errorf("帧还扣在 hold 上 ⇒ 回执之后此刻仍该是 RUNNING，实际 %s（写早了就是有人在替 Provider 编终态）", st)
	}
	if n := eventsOf(t, d, "execution_succeeded"); n != 0 {
		t.Errorf("没有收尾帧就不该有终态事件，实际 %d 条", n)
	}
	// 连接没被这发 send 关掉，也没被多起一条：还是同一个对象、还是第 1 代。
	again, err := pipe.hub.connection(ctx, "fake")
	if err != nil {
		t.Errorf("取常驻连接失败 ⇒ 连接可能被 send 收尾关掉了：%v", err)
	}
	if again != conn {
		t.Errorf("一次 send 之后连接换了对象（%p ⇒ %p）⇒ 还是 per-send 的形状，不是常驻", conn, again)
	}
	if n := tap.count("常驻连接已起"); n != 1 {
		t.Errorf("两次取连接只该起一条（%d 句）⇒ 多起的就是 per-send；实际日志：%v", n, tap.lines)
	}

	// ── 放出被扣的收尾帧：这一刻起它才是"回执之后才到"的那一帧。
	if _, err := conn.Call(ctx, "debug/hold", map[string]any{"on": false, "code": "SUCCEEDED"}); err != nil {
		t.Fatalf("放出 debug/hold 失败：%v", err)
	}
	if !tap.wait(t, "迟到收尾已落终态", 15*time.Second) {
		t.Fatalf("迟到帧没有被泵落成终态 ⇒ 判据 (a) 未落地（日志：%v）"+
			"——把连接改回 per-send（回执即 Close）时这一格就是这么红的（判据 (b) 的反例）", tap.lines)
	}

	// ── (a) 的后半：落的是**成套**的终态，不是半个。
	if st := pumpStatus(t, d, `SELECT status FROM executions WHERE id='ex-late'`); st != "SUCCEEDED" {
		t.Errorf("泵该把这一条落成 SUCCEEDED，实际 %s", st)
	}
	if st := pumpStatus(t, d, `SELECT status FROM tasks WHERE id='task-late'`); st != "WAITING_ACCEPTANCE" {
		t.Errorf("§5.3 投影 + 契约④-c 那半格该同笔推到 WAITING_ACCEPTANCE，实际 %s", st)
	}
	for _, typ := range []string{"execution_succeeded", "task_agent_completed", "task_waiting_acceptance"} {
		if n := eventsOf(t, d, typ); n != 1 {
			t.Errorf("%s 该恰 1 行，实际 %d 行（泵只写一笔，多一行就是写了两次）", typ, n)
		}
	}
	if n := pumpStatus(t, d, `SELECT count(*) FROM executions WHERE id='ex-late' AND finished_at IS NOT NULL`); n != "1" {
		t.Errorf("终态那一笔该把 finished_at 一起写上，实际 %s", n)
	}
	// 密封仍在 handler 那一笔里，泵不碰它（两笔各管各的，这是「Ⅰ」之后的分工）。
	if st, _ := stallIdem(t, d, "k-late-1"); st != "COMPLETED" {
		t.Errorf("投递成功该密封成 COMPLETED（判据 (c)：202 与密封语义一字不动），实际 state=%s", st)
	}
	// ⑤ 那条边界在这一格是可查的：同步路径上不许长出 UNKNOWN（它属于恢复那两条腿）。
	if n := eventsOf(t, d, "execution_unknown"); n != 0 {
		t.Errorf("泵不许写 UNKNOWN（T-391⑤），实际 %d 条", n)
	}
	if n := tap.count("未裁"); n != 0 {
		t.Errorf("真 producer 发的是表内码，不该喊未裁，实际 %d 句：%v", n, tap.lines)
	}
}

// TestPumpKeepsOneConnectionForTwoSends 是「Ⅰ」的另一半：**连接复用**（C42 随裁放开）。
// 同一 key 连发两次 send 只该起一台子进程 —— 旧形状"每次多起一个子进程"是当时记在注释里的已知代价，
// 现在那条代价被这条裁定取消了，所以要有一格钉住它真的被取消了。
// 这一条同样走真进程：一台子进程起没起，只有真进程数得出来（替身里"起"这件事不存在）。
func TestPumpKeepsOneConnectionForTwoSends(t *testing.T) {
	if os.Getenv("LUCERO_TEST_FAKE_ADAPTER") != "1" {
		t.Skip("未开 LUCERO_TEST_FAKE_ADAPTER=1：不 build、不起真子进程")
	}
	exe := buildFakeAdapterHere(t)
	probe, discover, pipe, _ := newBareDiscover(t,
		[]adapterSpec{{key: "fake", displayName: "Fake Adapter", command: exe}})
	tap := &logTap{}
	pipe.hub.logf = tap.f

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := probe.Probe(ctx, "fake")
	if err != nil {
		t.Fatalf("probe 失败：%v", err)
	}
	items, _, err := discover.DiscoverSessions(ctx, view.ID)
	if err != nil || len(items) == 0 {
		t.Fatalf("discover 该留下真会话：%v", err)
	}
	sess := items[0].SessionID

	for i, key := range []string{"k-reuse-1", "k-reuse-2"} {
		if _, err := pipe.CallSendTurn(ctx, execution.TurnCall{
			ExecutionID: "ex-reuse", SessionID: sess, Text: "复用这一格",
			Delivery: "start_now", IdempotencyKey: key,
		}); err != nil {
			t.Fatalf("%s: 第 %d 发 send 失败：%v", key, i+1, err)
		}
	}
	if n := tap.count("常驻连接已起"); n != 1 {
		t.Errorf("两发 send 该共用一条常驻连接（起一次），实际起了 %d 次：%v", n, tap.lines)
	}
	// 第二发如果起了新连接，第一发那台孩子就成了没人认领的孤儿 —— 那正是旧形状 + 常驻泵的坏法。
	// 这里不数进程：`closeAll` 挂在 `newBareDiscover` 的 cleanup 上，跑完由整包的残留检查兜（见台账）。
}

// TestPumpReconnectsAfterTheAdapterDies 钉 ④ 那句"死亡即重连"。形状是**注入驱动**的：
// 用 fake-adapter 的 `debug/crash`（§10 的注入端点，它先回执再退，退出码 2）把这条常驻连接的孩子打死，
// 然后看 Core 这一侧：在飞的那一发拿 503 家族、那条连接被退役、下一发自己重连成第二代。
//
// 为什么这一格必须有：连接不再随回执关闭之后，"孩子死了"从"那一发 send 的私事"变成
// "一条会一直存在的连接的状态" —— 不重连的话这个 key 从此每发都 503，而库里没有任何东西会说这件事。
// 契约里那条"死亡即重连并降健康"的**降健康**（`adapter_instances.health`）本批没做，也不该由我写：
// 全仓今天没有任何非测试码写过 health（判据见台账），写它的是 `internal/adapter` 那份 store。
func TestPumpReconnectsAfterTheAdapterDies(t *testing.T) {
	if os.Getenv("LUCERO_TEST_FAKE_ADAPTER") != "1" {
		t.Skip("未开 LUCERO_TEST_FAKE_ADAPTER=1：不 build、不起真子进程")
	}
	exe := buildFakeAdapterHere(t)
	probe, discover, pipe, _ := newBareDiscover(t,
		[]adapterSpec{{key: "fake", displayName: "Fake Adapter", command: exe}})
	tap := &logTap{}
	pipe.hub.logf = tap.f

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := probe.Probe(ctx, "fake")
	if err != nil {
		t.Fatalf("probe 失败：%v", err)
	}
	items, _, err := discover.DiscoverSessions(ctx, view.ID)
	if err != nil || len(items) == 0 {
		t.Fatalf("discover 该留下真会话：%v", err)
	}
	sess := items[0].SessionID

	first, err := pipe.hub.connection(ctx, "fake")
	if err != nil {
		t.Fatalf("起第一条常驻连接失败：%v", err)
	}
	if _, err := first.Call(ctx, adapter.MethodDebugCrash, nil); err != nil {
		t.Fatalf("debug/crash 该先回执（它回完才退）：%v", err)
	}

	// 在飞的那一发：孩子已经没了 ⇒ 连接面给的是 LostError ⇒ 归一 503 家族，并且把这条连接退役。
	if _, err := pipe.CallSendTurn(ctx, execution.TurnCall{
		ExecutionID: "ex-dead", SessionID: sess, Text: "打死之后第一发",
		Delivery: "start_now", IdempotencyKey: "k-dead-1",
	}); err == nil {
		t.Fatal("对端已经崩掉，这一发不该成功")
	}
	if n := tap.count("连接退役"); n != 1 {
		t.Errorf("死掉的连接该被退役恰一次（多退役一次就是关了别人新起的），实际 %d 句：%v", n, tap.lines)
	}

	// 下一发：重连成第二代，投递照常接。
	rec, err := pipe.CallSendTurn(ctx, execution.TurnCall{
		ExecutionID: "ex-dead", SessionID: sess, Text: "重连之后那一发",
		Delivery: "start_now", IdempotencyKey: "k-dead-2",
	})
	if err != nil {
		t.Fatalf("重连之后那一发该被接住：%v", err)
	}
	if !rec.Accepted {
		t.Errorf("重连后该 accepted=true，实际 %+v", rec)
	}
	if n := tap.count("常驻连接已起"); n != 2 {
		t.Errorf("第一代 + 重连的第二代 = 恰两条连接，实际 %d 句：%v", n, tap.lines)
	}
	// 两代必须是两台不同的孩子：同一个 pid 意味着"重连"只是把旧对象又递了一遍。
	second, err := pipe.hub.connection(ctx, "fake")
	if err != nil {
		t.Fatalf("取第二代连接失败：%v", err)
	}
	if first.Child().PID == second.Child().PID {
		t.Errorf("重连拿到了同一个 pid（%d）⇒ 那不是重连，是把旧对象又递了一遍", first.Child().PID)
	}
}
