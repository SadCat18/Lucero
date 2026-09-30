package main

// 崩溃注入端到端（T-370① 明排在甲之后那一格，任务 #14）：**真子进程 + 真连接面 + 真 handler + 真库**，两半分开断。
//   半①（我的面）：Adapter 挂住期间，别人的写必须照常秒答 —— REQ-S-1 的病根本体就是
//     "handler 的写事务罩着连接面"，旧形状下一发挂住的 send 停掉全 Core 的写。
//   半②（我这一侧的边界）：Adapter 自己崩掉时，`Close` 不许把"目标早不在"报成关闭失败，
//     也不许留下没人收的孩子（`requireProcessGone` 真查进程，不靠"我以为那句 Kill 会执行"）。
//
// 为什么孩子用测试自己（`pipeCommand`）而不是 fake-adapter：Core 侧**没有**任何能武装 `debug/hang`/`debug/crash`
// 的路径（16:0x 复测：非测试码里 `MethodDebug*` 只有 `internal/adapter/protocol.go:35` 那个常量，
// `cmd/core` 与 `api/http` 零调用点），而 `-adapter key=<路径>` 不收参数（`probeassembly.go:43~44`
// 那句"v0.1 不开参数位"）⇒ 真进程里我没法让 fake-adapter 挂住。这一格因此把"挂住/崩掉"落在**我能装的地方**
// （launcher 的 command 是注入位）；fake-adapter 那两半归 `adapter`/`test`，缺口原样报主控（账本第 292 条），
// 不在这里用假绿把它填掉。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/project"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

const stallToken = "stall-token"

// stallFixture 装出"handler 层 + 真库 + 真连接面"整条链，连接面的孩子换成指定模式。
// 返回里带抓到的孩子 pid（`capturePID` 从生命周期日志里取，不是猜的）。
// stallFixtureTap 与 stallFixture 同一套装配，只是多返回 pipe 与它的日志收集器（"喊"只有从这能看见）。
func stallFixtureTap(t *testing.T, mode string) (*httptest.Server, *db.DB, *int, *sendPipe, *logTap) {
	t.Helper()
	pid := new(int)
	tap := &logTap{}
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "fake", command: filepath.Join(t.TempDir(), "用不上-由 pipeCommand 代")}})
	if _, ok := pipe.byKey["fake"]; !ok {
		t.Fatal("装配里没留下 key=fake 的 spec（这一格会退化成 501，测不到连接面）")
	}
	// 「Ⅰ」之后连接归泵持有 ⇒ 换孩子的那一格里要换的是**泵**（泵带着 launcher），不是 pipe.launch。
	// svc 给 nil：这两台替身（silent / exit）根本活不到发收尾帧那一步，泵那一腿永远不会被调用；
	// 真会发帧的装配（fake-adapter）在 `turnpump_late_test.go` 里带的是真 Service。
	pipe.hub = newTurnHub(context.Background(),
		newLauncher(pipeCommand(mode), sendAttemptDeadline, tapPID(tap, pid), auditedStore{d: d}, nil),
		auditedStore{d: d}, nil, nil, nil, tap.f)
	t.Cleanup(pipe.hub.closeAll)

	must := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Read.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// principals 不在这里播：`newBareDiscover` 已经插过 system/local-user（再插就是 UNIQUE 撞车，
	// 我第一次跑就是这么红的）。这一格要的是它没播的那三张：实例、会话、执行。
	must(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ai-fake', 'fake', 'fake', 1, 1)`)
	must(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, created_at, updated_at)
		VALUES ('sess-st', 'ai-fake', 'remote-st', 1, 1)`)
	must(`INSERT INTO tasks (id, title, status, created_at, updated_at) VALUES ('task-st', '挂住这一格', 'RUNNING', 1, 1)`)
	must(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES ('ex-st', 'task-st', 'ai-fake', 'sess-st', 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`)

	pipe.logf = tap.f // 装配位：sendpipe 那句"未裁的码"只走 logf，收集它才谈得上断言
	srv := stallServe(t, d, pipe)
	t.Cleanup(srv.Close)
	return srv, d, pid, pipe, tap
}

// stallFixture 是两个既有判据用的薄封装（它们不关心日志）。
func stallFixture(t *testing.T, mode string) (*httptest.Server, *db.DB, *int) {
	t.Helper()
	srv, d, pid, _, _ := stallFixtureTap(t, mode)
	return srv, d, pid
}

// logTap 把 sendPipe 与泵的 logf 换成一个可断言的收集器（"喊"只有从这里能看见）。
type logTap struct {
	mu    sync.Mutex
	lines []string
	// ping 是"有新行了"那颗信号：`wait` 靠它醒，不靠睡着轮询。
	// 缓冲 1 + 非阻塞发：两次检查之间到达的行不会被丢（那颗 ping 会留在缓冲里，下一次 select 立刻消费）。
	ping chan struct{}
}

func (l *logTap) note(line string) {
	l.mu.Lock()
	l.lines = append(l.lines, line)
	if l.ping == nil {
		l.ping = make(chan struct{}, 1)
	}
	ping := l.ping
	l.mu.Unlock()
	select {
	case ping <- struct{}{}:
	default:
	}
}

func (l *logTap) f(format string, args ...any) { l.note(fmt.Sprintf(format, args...)) }

// wait 等一条含 sub 的喊出现：**事件驱动 + 有界**。泵是"先落库、后喊那一声"（同一 goroutine 里两句
// 挨着），所以等到这一声就等于等到那一笔写 ⇒ 等待谓词与被断言的谓词是同一件事。
// 到期就返回 false 把判决交回调用方，不硬等、不 sleep 轮询。
func (l *logTap) wait(t *testing.T, sub string, within time.Duration) bool {
	t.Helper()
	deadline := time.After(within)
	for {
		l.mu.Lock()
		var hit bool
		for _, s := range l.lines {
			if strings.Contains(s, sub) {
				hit = true
				break
			}
		}
		ping := l.ping
		l.mu.Unlock()
		if hit {
			return true
		}
		if ping == nil { // 一行都还没喊过：补一颗可等的 ping（与 note 同一颗）
			l.mu.Lock()
			if l.ping == nil {
				l.ping = make(chan struct{}, 1)
			}
			ping = l.ping
			l.mu.Unlock()
		}
		select {
		case <-ping:
		case <-deadline:
			return false
		}
	}
}

// lineWith 返回最后一条含 sub 的行（判据要看某一声明白里的数字，不只看有没有喊过）。
func (l *logTap) lineWith(sub string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	last := ""
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			last = s
		}
	}
	return last
}

func (l *logTap) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

// tapPID 把两条"只有日志能看见"的腿合到同一个收集器上：`capturePID` 要的 pid 与泵那几声喊。
// 装配位只有一个 logf ⇒ 合成一个，不另开第二条通道（那会变成"两处日志各说一半"）。
func tapPID(tap *logTap, pid *int) func(string, ...any) {
	inner := capturePID(pid)
	return func(format string, args ...any) {
		tap.f(format, args...)
		inner(format, args...)
	}
}

// stallServe 是"同一套 handler 装配"的共用段：两半的判据都得走**同一条** transport 路径，
// 不然"没收到帧"与"对端楔住"的差别会被装配差异替掉（T-381③ⅱ 要的就是这两者可区分）。
func stallServe(t *testing.T, d *db.DB, pipe *sendPipe) *httptest.Server {
	t.Helper()
	execSvc := execution.NewService()
	execSvc.EnableSend(task.NewService(nil), session.NewService(session.NewRepository(d.Read)))
	execSvc.EnableTurn(pipe)

	hs := httpapi.NewHandler(httpapi.Config{
		Token: stallToken, Version: "test-1",
		State:            func() string { return httpapi.StateReady },
		MigrationVersion: func() int64 { return 3 },
		Tickets:          httpapi.NewTickets(30*time.Second, nil),
		Store:            d,
		Projects:         project.NewService(nil),
		ExecutionSend:    execSvc,
	})
	return httptest.NewServer(hs)
}

type stallAnswer struct {
	code   int
	body   string
	el     time.Duration
	err    error
	closed bool // 客户端先到期限（连接还挂着）
}

func stallPost(t *testing.T, url, body, idem string, timeout time.Duration) stallAnswer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+stallToken)
	req.Header.Set("X-Lucero-Principal", "local-user")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	cli := &http.Client{Timeout: timeout + 10*time.Second}
	start := time.Now()
	resp, err := cli.Do(req)
	if err != nil {
		closed := errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline exceeded") ||
			strings.Contains(err.Error(), "timeout")
		return stallAnswer{err: err, el: time.Since(start), closed: closed}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return stallAnswer{code: resp.StatusCode, body: string(b), el: time.Since(start)}
}

// TestHangAdapterDoesNotStallOtherWrites 是半①。客户端只等 2.5 秒就放弃 ——
// 那个"放弃"本身就是证据：连接面此刻还挂着（`silent` 孩子不回 initialize，`sendAttemptDeadline` 是 10 秒）。
// 而在这 2.5 秒里，`POST /projects` 必须 201 且秒答；两件事同一时刻成立，才叫"没被拖住"。
func TestHangAdapterDoesNotStallOtherWrites(t *testing.T) {
	srv, d, pid := stallFixture(t, "silent")

	sendDone := make(chan stallAnswer, 1)
	go func() {
		sendDone <- stallPost(t, srv.URL+"/api/v1/executions/ex-st/send",
			`{"text":"挂住这一格","delivery":"start_now"}`, "k-stall-1", 2500*time.Millisecond)
	}()

	time.Sleep(300 * time.Millisecond) // 只制造重叠，**不参与判决**（判据在下面两条耗时上）
	proj := stallPost(t, srv.URL+"/api/v1/projects",
		`{"canonical_path":"D:/stall/proj","name":"挂住期间的写"}`, "k-stall-proj", 3*time.Second)
	if proj.err != nil {
		t.Fatalf("挂住期间另一发写根本发不出去：%v", proj.err)
	}
	if proj.code != http.StatusCreated {
		t.Errorf("POST /projects 该 201，实际 %d %s", proj.code, proj.body)
	}
	if proj.el >= time.Second {
		t.Errorf("REQ-S-1 的判据被违反：挂住的 send 让别人排了 %v 的队", proj.el)
	}

	var send stallAnswer
	select {
	case send = <-sendDone:
	case <-time.After(20 * time.Second):
		t.Fatal("send 连客户端期限都不认 ⇒ 这条腿没界（真正的原病）")
	}
	if !send.closed {
		t.Errorf("2.5 秒内 send 不该有答案（孩子是 silent 的），实际 %d %s —— 那说明这一格没测到「挂住」那一形",
			send.code, send.body)
	}
	if send.el < 2*time.Second {
		t.Errorf("send 只跑了 %v ⇒ 上面那次「秒答」是在跟一条早就结束的腿抢跑，重叠没成立", send.el)
	}
	if st := stallStatus(t, d, `SELECT status FROM executions WHERE id='ex-st'`); st != "RUNNING" {
		t.Errorf("挂住期间 execution 该留在 RUNNING（同步路径不造 UNKNOWN），实际 %s", st)
	}
	// 那一列此刻是**身份**不是响应（幂等列的"两义 + 读取条件"：IN_PROGRESS ⇒ 凭证、COMPLETED ⇒ 响应）。
	// 我第一次跑这里断的是 `stored != ""` 就红 —— 那是我把设计断反了，不是代码错。
	st, stored := stallIdem(t, d, "k-stall-1")
	if st != "IN_PROGRESS" {
		t.Errorf("挂住的一发不该密封（要留给同键续跑），实际 state=%s body=%q", st, stored)
	}
	var cred struct {
		NeedsTurn *bool   `json:"needs_turn"`
		MessageID *string `json:"message_id"`
		SessionID *string `json:"session_id"`
		Text      *string `json:"text"`
	}
	if err := json.Unmarshal([]byte(stored), &cred); err != nil {
		t.Fatalf("IN_PROGRESS 那一格该是可续跑的凭证 JSON，解不出来：%v（原文 %q）", err, stored)
	}
	if cred.NeedsTurn == nil || !*cred.NeedsTurn || cred.MessageID == nil || *cred.MessageID == "" ||
		cred.SessionID == nil || *cred.SessionID == "" || cred.Text == nil {
		t.Errorf("凭证缺了「再打一次」所需的最小集，实际 %s", stored)
	}
	if n := stallCount(t, d, `SELECT count(*) FROM projects WHERE name='挂住期间的写'`); n != 1 {
		t.Errorf("那一发写该真落了库（不然「秒答」是答了个空请求），实际 %d 行", n)
	}
	requireProcessGone(t, *pid)
}

// TestCrashedAdapterIsNotReportedAsCloseFailure 是半②：孩子一上来就自己退（连握手都没成）。
// Core 这一侧要 ① 给得出诚实答案（503 家族、retryable，而不是 500、也不是等满期限），
// ② 不留没人收的孩子。③ 顺带钉"崩掉不是 FAILED 的证据"：execution 仍留 RUNNING。
func TestCrashedAdapterIsNotReportedAsCloseFailure(t *testing.T) {
	srv, d, pid := stallFixture(t, "exit")

	got := stallPost(t, srv.URL+"/api/v1/executions/ex-st/send",
		`{"text":"孩子先死了","delivery":"start_now"}`, "k-crash-1", 8*time.Second)
	if got.err != nil {
		t.Fatalf("发不出去：%v", got.err)
	}
	if got.el >= 5*time.Second {
		t.Errorf("孩子早没了却等到 %v ⇒ 「目标早不在」那一层没接上（第 19 条那一格）", got.el)
	}
	if got.code != http.StatusServiceUnavailable {
		t.Errorf("该 503（连接面病了这一族），实际 %d %s", got.code, got.body)
	}
	var e struct {
		Error struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(got.body), &e); err != nil {
		t.Fatal(err)
	}
	if e.Error.Code != "adapter_unresponsive" || !e.Error.Retryable {
		t.Errorf("答案该是 retryable 的 adapter_unresponsive，实际 %+v", e.Error)
	}
	if st := stallStatus(t, d, `SELECT status FROM executions WHERE id='ex-st'`); st != "RUNNING" {
		t.Errorf("进程死了不许被读成 FAILED：execution 该留 RUNNING，实际 %s", st)
	}
	if *pid == 0 {
		t.Fatal("没抓到孩子 pid ⇒ 那条孤儿检查会空转")
	}
	requireProcessGone(t, *pid)
}

func stallStatus(t *testing.T, d *db.DB, q string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(q).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func stallIdem(t *testing.T, d *db.DB, key string) (string, string) {
	t.Helper()
	var st string
	var stored sql.NullString
	if err := d.Read.QueryRow(`SELECT state, response_json FROM idempotency_keys WHERE key=?`, key).Scan(&st, &stored); err != nil {
		t.Fatal(err)
	}
	return st, stored.String
}

func stallCount(t *testing.T, d *db.DB, q string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
