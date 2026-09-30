package main

// sendpipe 是 `POST /executions/{id}/send` 的连接半边，住在 cmd/core（装配归我）。
// 09-25 之前它的测试面是空的：api/http 那批 send 用例走的是 `stubTurn`（`send_test.go:413`），
// 证的是"Service 会调那个接口"，证不了"我这一半真能把一次 start_now 送到 Adapter、
// 且送不到时给的是 §15.2 里那三个码而不是 500"。本文件补的就是这一格。
//
// 分两类：四格"到不了子进程"的分岔不需要开关（不 build、不起进程），
// 端到端那一格按同一套 `LUCERO_TEST_FAKE_ADAPTER=1`（与 probe/discover 那两条一致）。

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestSendRequestLegHonorsCallersDeadline 是 T-345③ 那句"换常量时把注释与用例一起改"里**行为**那一腿：
// 期限那一步（`sendpipe.go:76~81`）只能"外层没期限才补一个"，不许把这一腿从调用方手里摘出去。
//
// 形状是双向的，两个界都断：外层给 1.5 秒 + 一个一声不出的连接（`pipeCommand("silent")`，
// 与 processpipe 那批用例共用同一个替身，不需要 `LUCERO_TEST_FAKE_ADAPTER`）⇒
//   - 下界 `>= 1.2s`：证明确实等到了"要回执"那一步、并由**外层的期限**掐掉；
//     少了这一腿，`Connect` 秒失败也会让"耗时 < 上限"白过（判据空转）。
//   - 上界 `< 8s`：若写成 `WithTimeout(context.Background(), sendAttemptDeadline)`（丢掉父 ctx），
//     这里就是 ~10 秒 ⇒ 红。8 秒是留给机器负载的余量，不是量出来的数。
//
// 为什么不用"外层 30 分钟"那种写法：`WithTimeout(parent, 10s)` 取 min 之后仍贴着父期限，
// 那种形状与正确的不可分 —— 只有父期限比 10 秒短才分得出。
func TestSendRequestLegHonorsCallersDeadline(t *testing.T) {
	_, _, base, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "fake", command: "用不上：连接由 pipeCommand 起"}})
	spec, ok := base.byKey["fake"]
	if !ok {
		t.Fatal("装配里没留下 key=fake 的 spec（这条用例会退化成 501，测不到期限）")
	}
	seedFakeInstance(t, d)
	seedSession(t, d, "s-hang", "remote-1")

	// 自己的 launcher：连接面换成"起得来、但一声不出"的那个替身。
	l := newLauncher(pipeCommand("silent"), probeAttemptDeadline, nil, auditedStore{d: d}, nil)
	pipe := &sendPipe{read: base.read, hub: silentHub(t, l, d), byKey: map[string]adapterSpec{"fake": spec}}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	started := time.Now()
	rec, err := pipe.CallSendTurn(ctx, execution.TurnCall{
		ExecutionID: "ex-hang", SessionID: "s-hang", Text: "外层期限",
		Delivery: "start_now", IdempotencyKey: "k-hang",
	})
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("一声不出的连接该失败，实际回执 %+v —— 通了就不该再看耗时（那是另一条腿）", rec)
	}
	var ae *adapter.Error
	if !errors.As(err, &ae) || ae.Code != adapter.ErrAdapterUnresponsive {
		t.Errorf("挂住的连接必须归一成 ADAPTER_UNRESPONSIVE（503），实际 %T %v", err, err)
	}
	if elapsed < 1200*time.Millisecond {
		t.Errorf("只等了 %v，比外层 1.5 秒还早 ⇒ 这条腿没走到「等回执」那一步，耗时上限那一条是在空转", elapsed)
	}
	if elapsed >= 8*time.Second {
		t.Errorf("外层只给了 1.5 秒，这一腿却等了 %v ⇒ 期限那一步把父 ctx 换成了自己的（或没守着 ctx）", elapsed)
	}
}

// TestSendRequestLegBoundsWhenCallerHasNoDeadline 补上一条便宜的判据给不了的那一格：
// 外层**没给期限**时，`sendpipe.go` 那句"才补一个"必须真补上，否则一个挂住的 Adapter 就把 handler 线程永久占着
// —— 这正是 T-345③ 那个 10 秒唯一能行为可见的时刻。代价是这条用例每次要等满那一档（实测见台账，耗时带负载条件）。
//
// 双向界：`>= 8s` 证明"确实补了期限、并由它掐掉"（摘掉整块 `if !ok` ⇒ 这一腿挂到包超时）；
// `<= 25s` 证明"补的是那个量级、不是无限"。两条都**分不出借没借**（两个常量同值），那是上一条源形状判据的活。
func TestSendRequestLegBoundsWhenCallerHasNoDeadline(t *testing.T) {
	_, _, base, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "fake", command: "用不上：连接由 pipeCommand 起"}})
	spec, ok := base.byKey["fake"]
	if !ok {
		t.Fatal("装配里没留下 key=fake 的 spec（这条用例会退化成 501，测不到期限）")
	}
	seedFakeInstance(t, d)
	seedSession(t, d, "s-bound", "remote-1")

	l := newLauncher(pipeCommand("silent"), probeAttemptDeadline, nil, auditedStore{d: d}, nil)
	pipe := &sendPipe{read: base.read, hub: silentHub(t, l, d), byKey: map[string]adapterSpec{"fake": spec}}

	started := time.Now()
	rec, err := pipe.CallSendTurn(context.Background(), execution.TurnCall{
		ExecutionID: "ex-bound", SessionID: "s-bound", Text: "外层无期限",
		Delivery: "start_now", IdempotencyKey: "k-bound",
	})
	elapsed := time.Since(started)
	if err == nil {
		t.Fatalf("一声不出的连接该失败，实际回执 %+v", rec)
	}
	var ae *adapter.Error
	if !errors.As(err, &ae) || ae.Code != adapter.ErrAdapterUnresponsive {
		t.Errorf("挂住的连接必须归一成 ADAPTER_UNRESPONSIVE（503），实际 %T %v", err, err)
	}
	if elapsed < 8*time.Second {
		t.Errorf("外层没给期限，这一腿却在 %v 就退了 ⇒ 「补一个期限」那一步没落地", elapsed)
	}
	if elapsed > 25*time.Second {
		t.Errorf("等了 %v 才退 ⇒ 补的那个量级不是 10 秒那一档", elapsed)
	}
}

// TestSendAttemptDeadlineIsItsOwnConstant 钉 T-345③ 那两句字面：send 有自己的期限常量、
// **不再借** `probeAttemptDeadline`，而且换数时注释与用例一起改。
//
// 三腿都判形状：① const 定义位在 `sendpipe.go` 上、且那个数就是 10 秒；② 定义之外真有人用它
// （只留个没人用的定义=没接线）；③ 代码里不再出现 `probeAttemptDeadline`。
// 用 `go/parser` 而不是 grep，原因很具体：`sendpipe.go` 的注释正文自己写着"不再借 `probeAttemptDeadline`"，
// 字面 grep 会把这句自述当成引用（假红）。
//
// 为什么"那个数"要从**源里读**而不是写 `sendAttemptDeadline != 10*time.Second`：
// 引用符号本身会让"把常量内联回调用点 / 挪走定义"这类变异变成 BUILD-BROKEN（无效变异、不计判决 ——
// 本批 M5 第一发就是这么废的）。读定义位右侧那段字面，既钉住 10 秒，又让"只改数"那一手真能红。
//
// 这条判据**证不到**的一件事，写给下一个读它的人，别拿它当"量级被行为验过"：
// 两个常量今天同值（都是 10 秒）⇒ 没有行为用例分得出"借了没借"，这条只能判名字与数。
// 要让"借没借"行为可分，得先给两个不同的量级 —— 量级归主控裁。
func TestSendAttemptDeadlineIsItsOwnConstant(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sendpipe.go", nil, 0) // mode 0 ⇒ 注释不进 AST
	if err != nil {
		t.Fatalf("解析 sendpipe.go 失败（这条判据的地基）：%v", err)
	}

	defIdents := map[*ast.Ident]bool{}
	var deadlineValue string
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				defIdents[name] = true
				if name.Name == "sendAttemptDeadline" && len(vs.Values) == 1 {
					var buf bytes.Buffer
					if err := printer.Fprint(&buf, fset, vs.Values[0]); err != nil {
						t.Fatalf("取 `sendAttemptDeadline` 的定义值失败：%v", err)
					}
					deadlineValue = buf.String()
				}
			}
		}
	}
	if deadlineValue != "10 * time.Second" {
		t.Errorf("T-345③ 给的占位量级是 10 秒、且定义位要在 `sendpipe.go` 的 const 上（换数时注释与本条用例一起改）；"+
			"从定义位实际读到 %q（空串=这一格里没有那个 const）", deadlineValue)
	}

	var declared, usedOutsideDef, mentionsProbe bool
	ast.Inspect(file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		switch id.Name {
		case "sendAttemptDeadline":
			if defIdents[id] {
				declared = true
			} else {
				usedOutsideDef = true
			}
		case "probeAttemptDeadline":
			mentionsProbe = true
		}
		return true
	})
	if !declared {
		t.Errorf("`sendpipe.go` 的 const 定义位上没有 `sendAttemptDeadline`（数被内联回调用点=没拆常量）")
	}
	if !usedOutsideDef {
		t.Errorf("`sendpipe.go` 里除定义之外没人用 `sendAttemptDeadline`（那这条腿又回到没期限/借来的期限）")
	}
	if mentionsProbe {
		t.Errorf("`sendpipe.go` 的代码里仍出现 `probeAttemptDeadline` ⇒ ③ 那句「不再借」没落地（注释里的自述不算，parser 已剥掉注释）")
	}
}

// silentHub 给"要一条起得来但一声不出的连接"那两条期限用例装泵。
// svc 给 nil：这台替身从不发收尾帧，泵那一腿（要写库的那条）永远走不到；
// 走到就说明形状变了，那时它会以 nil 解引用红得很难看 —— 那是想要的效果，不是要兜的分支。
func silentHub(t *testing.T, l *launcher, d *db.DB) *turnHub {
	t.Helper()
	hub := newTurnHub(context.Background(), l, auditedStore{d: d}, nil, nil)
	// 常驻 = 用例结束必须退役，否则这两条用例会各留一台活着的孩子（本包跑完查残留，见台账）。
	t.Cleanup(hub.closeAll)
	return hub
}
