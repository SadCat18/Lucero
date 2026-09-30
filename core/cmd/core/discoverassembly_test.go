package main

// discover 的装配半边测三件事：分页拉到底、拉不完时**不许交部分清单**、
// 以及三种"连不上/没配/不认识"的区分（404 实例不存在 / 501 本机没配起法 / 503 那个 Adapter 病了）。
// 端到端那条（真 fake-adapter + 真握手）按同一套 `LUCERO_TEST_FAKE_ADAPTER=1` 开关，与 probe 那条一致。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
	"github.com/SadCat18/lucero/core/internal/task"
)

// stubCaller 只需要 `Call` 这一格能力：它按页返回 §4 的形状，第二页起该带上次的游标。
type stubCaller struct {
	pages   []sessionListPage
	calls   int
	lastReq []map[string]any
}

func (s *stubCaller) Call(_ context.Context, method string, params any) (*adapter.Frame, error) {
	if method != "session/list" {
		return nil, fmt.Errorf("stub: 不该调 %q", method)
	}
	p, _ := params.(map[string]any)
	s.lastReq = append(s.lastReq, p)
	if s.calls >= len(s.pages) {
		return nil, errors.New("stub: 页用完了，说明调用方没有停在 next_cursor 为空处")
	}
	raw, err := json.Marshal(s.pages[s.calls])
	if err != nil {
		return nil, err
	}
	s.calls++
	return &adapter.Frame{Result: raw}, nil
}

func page(remote, cursor string) sessionListPage {
	return sessionListPage{
		Sessions:   []sessionListRow{{RemoteSessionID: remote, Title: "t", UpdatedAt: 1}},
		NextCursor: cursor,
	}
}

func TestListAllSessionsFollowsTheCursor(t *testing.T) {
	caller := &stubCaller{pages: []sessionListPage{page("r-1", "c1"), page("r-2", "")}}
	got, err := listAllSessions(context.Background(), caller, "fake")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].RemoteSessionID != "r-1" || got[1].RemoteSessionID != "r-2" {
		t.Errorf("两页该拼成两条，实际 %+v", got)
	}
	if caller.calls != 2 {
		t.Errorf("next_cursor 为空就该停，实际发了 %d 页", caller.calls)
	}
	// 第二页必须把游标带上，否则就是从头再拉一遍（并把同一批行看成"新行"）。
	if len(caller.lastReq) != 2 || caller.lastReq[1]["cursor"] != "c1" {
		t.Errorf("第二页该带 cursor=c1，实际 %+v", caller.lastReq)
	}
}

func TestListAllSessionsRefusesToTruncate(t *testing.T) {
	// 每一页都还有 next_cursor ⇒ 触顶必须**报错**，不许交一份"看起来完整"的前 N 页：
	// 面板拿它会以为远端只有这些会话（§12 那一族的"部分结果冒充全部"）。
	caller := &stubCaller{}
	for i := 0; i <= discoverMaxPages; i++ {
		caller.pages = append(caller.pages, page("r", fmt.Sprintf("c%d", i)))
	}
	_, err := listAllSessions(context.Background(), caller, "fake")
	if err == nil {
		t.Fatal("触顶该报错，结果安静地返回了")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", discoverMaxPages)) {
		t.Errorf("报错里要能看见封顶是多少页，实际 %v", err)
	}
	if caller.calls != discoverMaxPages {
		t.Errorf("该在恰好 %d 页处停下，实际发了 %d 页", discoverMaxPages, caller.calls)
	}
}

func newBareDiscover(t *testing.T, specs []adapterSpec) (*probeService, *discoverService, *sendPipe, *db.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := db.InitRedaction([]string{"token"}, nil, "[REDACTED]"); err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
		if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, p[0], p[1], p[0]); err != nil {
			t.Fatal(err)
		}
	}
	now := func() int64 { return 4_000 }
	svc := session.NewService(session.NewRepository(d.Read), now)
	// 泵要的那个 Service：`FinishFromTurn` 需要 task 投影器，所以这里与真装配同形（EnableSend 两件都给）。
	execSvc := execution.NewService()
	execSvc.EnableSend(task.NewService(nil), svc)
	probe, discover, pipe, _, err := newAdapterServices(context.Background(), d, auditedStore{d: d}, specs,
		probeAttemptDeadline, now, func(string, ...any) {}, svc, execSvc)
	if err != nil {
		t.Fatal(err)
	}
	// 常驻连接是真子进程：用例结束必须退役它们，否则这一包跑完留下一堆没人收的孩子（第 19 条那一格）。
	t.Cleanup(func() {
		if pipe.hub != nil {
			pipe.hub.closeAll()
		}
	})
	if probe == nil || discover == nil || pipe == nil {
		t.Fatal("给了 specs 就该三个都装配出来")
	}
	return probe, discover, pipe, d
}

func TestDiscoverDistinguishesUnknownInstanceFromUnconfiguredKey(t *testing.T) {
	_, discover, _, d := newBareDiscover(t, []adapterSpec{{key: "other", displayName: "other", command: "does-not-matter"}})

	// ① 实例根本不存在 ⇒ sql.ErrNoRows（transport ⇒ 404）
	if _, _, err := discover.DiscoverSessions(context.Background(), "no-such-instance"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("未知实例该透出 sql.ErrNoRows（→404），实际 %v", err)
	}
	// ② 实例在、但它的 key 本机没配起法 ⇒ ErrAdapterNotLaunchable（transport ⇒ 501，不是 503）
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ai-fake', 'fake', 'fake', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := discover.DiscoverSessions(context.Background(), "ai-fake"); !errors.Is(err, httpapi.ErrAdapterNotLaunchable) {
		t.Errorf("没配起法该报 ErrAdapterNotLaunchable（→501），实际 %v", err)
	}
	// 两种都没写任何东西
	if n := countRowsIn(t, d, `SELECT count(*) FROM sessions`); n != 0 {
		t.Errorf("两发失败的都不该建 session 行，实际 %d", n)
	}
}

func countRowsIn(t *testing.T, d *db.DB, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := d.Read.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestDiscoverEndToEndOverFakeAdapter 是这一批最硬的一条：probe → discover 同一台机器上连着跑，
// 链上第一次留下 session 行（面板链第 2/3/4 格等的就是这一步）。
func TestDiscoverEndToEndOverFakeAdapter(t *testing.T) {
	if os.Getenv("LUCERO_TEST_FAKE_ADAPTER") != "1" {
		t.Skip("未开 LUCERO_TEST_FAKE_ADAPTER=1：不 build、不起真子进程（交付前必须开着跑一遍）")
	}
	exe := buildFakeAdapterHere(t)
	// 三个服务只构造一次、共用同一份 specs 与 launcher（造两遍就是给漂移留位置）。
	probe, discover, _, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: exe}})
	ctx := context.Background()

	view, err := probe.Probe(ctx, "fake")
	if err != nil {
		t.Fatalf("probe 该先成功（discover 要用它的实例 id）: %v", err)
	}
	items, created, err := discover.DiscoverSessions(ctx, view.ID)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if created != len(items) || created == 0 {
		t.Fatalf("首探清单该全是新行，实际 created=%d items=%v", created, items)
	}
	for _, it := range items {
		if it.SessionID == "" || it.RemoteSessionID == "" {
			t.Errorf("items 每条都要有本地 id 与远端 id，实际 %+v", it)
		}
	}
	if n := countRowsIn(t, d, `SELECT count(*) FROM sessions WHERE adapter_instance_id = ?`, view.ID); int64(n) != int64(created) {
		t.Errorf("库里行数该等于 created，实际 %d vs %d", n, created)
	}
	if n := countRowsIn(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'session_discovered'`); int64(n) != int64(created) {
		t.Errorf("一行会话配一条 session_discovered，实际 %d vs created=%d", n, created)
	}
	// 第二次：幂等 ⇒ 零新建、返回同一批 id（面板重进这一页不该长出重复会话）
	items2, created2, err := discover.DiscoverSessions(ctx, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created2 != 0 {
		t.Errorf("重复 discover 该建 0 行，实际 %d", created2)
	}
	for i := range items2 {
		if items2[i].SessionID != items[i].SessionID {
			t.Errorf("第 %d 条换了本地 id（%q→%q），去重没生效", i, items[i].SessionID, items2[i].SessionID)
		}
	}
	if n := countRowsIn(t, d, `SELECT count(*) FROM event_journal WHERE event_type = 'session_discovered'`); int64(n) != int64(created) {
		t.Errorf("第二次不该再发事件，实际共 %d 行", n)
	}
	// discover 不绑项目（C41 那一格的原意）
	if n := countRowsIn(t, d, `SELECT count(*) FROM sessions WHERE project_id IS NOT NULL`); n != 0 {
		t.Errorf("discover 建出来的行 project_id 该全是 NULL，实际 %d 行有值", n)
	}
}
