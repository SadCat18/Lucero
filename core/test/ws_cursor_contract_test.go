package test

// REQ-F-20 游标权威判据（契约③ §5，主控 11:3x 裁定、11:41 落文档）。
// 本体口径一句话：**游标的唯一权威是 hello 帧；?from_seq= 只在"根本没有 hello 到达"时才生效。**
//
// 判据见 **契约③ §5 判据表 F20**（13:1x 由本文件的注释镜像进本文，四张表住一处；
// 判据本体以契约为准，这里不再抄一份，避免两处真相）。
//
// 一处请主控补进表里：F20-1 那一行只写了"不补发"，但用例实际还断了两条**对照腿**，
// 少了它们这判据就是半边的 —— ① 随后新提交的事件仍要实时推得下来（"不补发"不许靠关推送装出来）；
// ② 显式 `from_seq=0` 必须补发全部（证明上一条的"空"是真没有，不是解析器失明）。
// F20-3 同理，它的第二个方向（query 大、hello 小）专门用来抓"取较大者"那种看着更保守的错实现，
// 表里"两个方向都判"那句可以，但建议把这半句写进去，否则容易被读成一个方向。
//
// 实现层的两个选择留在本文件（它们不是判据，是"怎么把判据跑起来"）：
// 为什么在本包自建 Journal 替身而不是连真 Core：`cmd/core` 现在启起来 `event_journal` 是 0 行
// （REQ-T7：`core_ready` 没进 journal），写端点 `POST /api/v1/tasks` 也还没有（U13-6）——
// 真链路上我造不出"游标之下有历史、游标之上还有新行"这一组数据。wsapi 把 `Journal` 定成注入接口
// 正是为此。等 REQ-T7/U13-6 落地，同一批断言会再加一条真子进程的被测面（见 progress 的 Remaining）。
//
// 客户端帧是本文件自己按 RFC 6455 写的（掩码、单帧、≤125 字节），故意不复用包内 framer：
// 拿被测方的编码器自证，两边会一起错到同一个方向。握手那一钉用 RFC §1.3 的示例向量。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	wsapi "github.com/SadCat18/lucero/core/api/websocket"
)

// --- 被测接缝：真 HTTP 服务 + 真 WS 握手 + 注入的 Journal ---------------------

const (
	// RFC 6455 §1.3 的示例向量：钉住"我的客户端"自己没写错握手。
	wsRFCExampleKey    = "dGhlIHNhbXBsZSBub25jZQ=="
	wsRFCExampleAccept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	// hello 等待与轮询都由**本用例**设定，所以下面所有等待预算都从它推导，不抄实现的默认值。
	wsHelloTimeout = 300 * time.Millisecond
	wsPoll         = 20 * time.Millisecond
)

// wsJournal 是 event_journal 的只读替身：只装"已提交"的行，seq 连续从 1 开始。
type wsJournal struct {
	mu   sync.Mutex
	rows []wsapi.Event
}

func newWSJournal(n int) *wsJournal {
	j := &wsJournal{}
	for i := 1; i <= n; i++ {
		j.rows = append(j.rows, wsapi.Event{
			Seq: int64(i), EventID: fmt.Sprintf("019e0000-0000-7000-8000-%012d", i),
			EventType: "task_updated", AggregateType: "task", AggregateID: "t-1",
			AggregateVersion: int64(i), CreatedAt: int64(1757000000000 + i),
		})
	}
	return j
}

func (j *wsJournal) After(_ context.Context, after int64, limit int) ([]wsapi.Event, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []wsapi.Event
	for _, e := range j.rows {
		if e.Seq > after {
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (j *wsJournal) Oldest(_ context.Context) (int64, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.rows) == 0 {
		return 0, nil
	}
	return j.rows[0].Seq, nil
}

func (j *wsJournal) Head(_ context.Context) (int64, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.rows) == 0 {
		return 0, nil
	}
	return j.rows[len(j.rows)-1].Seq, nil
}

// append 模拟"事务提交之后才推"（ADR-008）：客户端连上之后再补一行。
func (j *wsJournal) append(e wsapi.Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.rows = append(j.rows, e)
}

type wsTicket struct{}

// 接缝对不上就编译不过：判据不能因为 wsapi 改了接口而静默变成"什么都没测"。
var (
	_ wsapi.Journal = (*wsJournal)(nil)
	_ wsapi.Tickets = wsTicket{}
)

func (wsTicket) Redeem(t string) bool { return t == "ok" }

// startWS 起真 httptest 服务挂 wsapi.Handler。心跳设成一小时：
// 判"没有帧"的时候，一条 ping 混进来就会把断言变成"没等到"而不是"确实没有"。
func startWS(t *testing.T, j wsapi.Journal) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(wsapi.Handler(wsapi.Config{
		Tickets: wsTicket{}, Journal: j,
		HelloTimeout: wsHelloTimeout, Poll: wsPoll, Heartbeat: time.Hour,
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- 我自己的极简 WS 客户端（掩码出站帧、只读服务端明文帧）--------------------

type wsClient struct {
	conn net.Conn
	br   *bufio.Reader
}

type wsFrame struct {
	Type string // 帧里 "type" 的值；解不出 JSON 时为 "#nonjson"
	Body map[string]any
	Raw  string
}

func wsDial(t *testing.T, srvURL, path string) *wsClient {
	t.Helper()
	u, err := url.Parse(srvURL)
	if err != nil {
		t.Fatalf("httptest 地址解不开: %v", err)
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatalf("连不上: %v", err)
	}
	c := &wsClient{conn: conn, br: bufio.NewReader(conn)}
	t.Cleanup(func() { _ = conn.Close() })
	req := "GET " + path + " HTTP/1.1\r\nHost: " + u.Host +
		"\r\nUpgrade: websocket\r\nConnection: Upgrade" +
		"\r\nSec-WebSocket-Key: " + wsRFCExampleKey + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("发握手失败: %v", err)
	}
	resp, err := http.ReadResponse(c.br, &http.Request{Method: "GET", URL: u})
	if err != nil {
		t.Fatalf("握手没有 HTTP 响应: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("握手状态 = %d，期望 101（body: %v）", resp.StatusCode, resp.Header.Get("Sec-WebSocket-Accept"))
	}
	// 这一条钉的是我的客户端，不是被测方：向量来自 RFC 本身。
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != wsRFCExampleAccept {
		t.Fatalf("Sec-WebSocket-Accept = %q，RFC 6455 §1.3 期望 %q", got, wsRFCExampleAccept)
	}
	return c
}

// sendText 发一帧客户端文本帧：FIN=1、opcode=text、必须掩码、载荷 ≤125（契约③ §2 帧约定）。
func (c *wsClient) sendText(s string) error {
	if len(s) > 125 {
		return fmt.Errorf("客户端帧 %d 字节，超过契约③ §2 的 125 上限", len(s))
	}
	var out bytes.Buffer
	out.WriteByte(0x80 | 0x1)
	out.WriteByte(0x80 | byte(len(s)))
	var key [4]byte
	copy(key[:], []byte{0x12, 0x34, 0x56, 0x78}) // 固定掩码 key 足够：可复现比随机更值钱
	out.Write(key[:])
	for i := 0; i < len(s); i++ {
		out.WriteByte(s[i] ^ key[i%4])
	}
	_, err := c.conn.Write(out.Bytes())
	return err
}

// read 读一帧。budget 是"最多愿意等多久"，超时返回错误（调用方决定那是失败还是"确实没帧"）。
func (c *wsClient) read(budget time.Duration) (wsFrame, error) {
	if err := c.conn.SetReadDeadline(time.Now().Add(budget)); err != nil {
		return wsFrame{}, err
	}
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		return wsFrame{}, err
	}
	op := h[0] & 0x0f
	if h[1]&0x80 != 0 {
		return wsFrame{}, errors.New("服务端帧带掩码，违反 RFC 6455 §5.1")
	}
	n := int(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(c.br, b[:]); err != nil {
			return wsFrame{}, err
		}
		n = int(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(c.br, b[:]); err != nil {
			return wsFrame{}, err
		}
		n = int(binary.BigEndian.Uint64(b[:]))
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return wsFrame{}, err
	}
	if op == 0x8 {
		return wsFrame{}, io.EOF
	}
	if op != 0x1 {
		return wsFrame{}, fmt.Errorf("收到 opcode %#x 的帧，期望文本帧", op)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return wsFrame{Type: "#nonjson", Raw: string(payload)}, nil
	}
	typ, _ := body["type"].(string)
	if typ == "" {
		typ = "#no-type"
	}
	return wsFrame{Type: typ, Body: body, Raw: string(payload)}, nil
}

// seqs 把 event 帧的 seq 抽出来，顺序即到达顺序——"补发了哪些"要看到达序列，不看集合。
func seqs(frames []wsFrame) []int64 {
	var out []int64
	for _, fr := range frames {
		if fr.Type != "event" {
			continue
		}
		data, _ := fr.Body["data"].(map[string]any)
		if v, ok := data["seq"].(float64); ok {
			out = append(out, int64(v))
		}
	}
	return out
}

// types 是帧类型的到达序列，用来判"首帧是什么"。
func types(frames []wsFrame) []string {
	out := make([]string, 0, len(frames))
	for _, fr := range frames {
		out = append(out, fr.Type)
	}
	return out
}

// collect 在 budget 内把帧收干净（收不到就返回已收到的；判"没有帧"时靠的是"故意等到超时"）。
func (c *wsClient) collect(budget time.Duration) ([]wsFrame, error) {
	var out []wsFrame
	for {
		fr, err := c.read(budget)
		if err != nil {
			return out, err
		}
		out = append(out, fr)
		budget = 100 * time.Millisecond // 后续帧之间不需要再等一整个 hello 窗口
	}
}

// --- F20-1 ------------------------------------------------------------------

// TestHelloCursorOmittedOverridesQuery F20-1：只给 ?from_seq=0 而 hello 省略游标 ⇒
// 首帧必须是 hello_ack{server_seq}、query 之下的历史**一条都不补发**；
// 反过来"随后新提交的事件仍然要实时推得到"，否则"不补发"就是靠关推送装出来的。
func TestHelloCursorOmittedOverridesQuery(t *testing.T) {
	j := newWSJournal(3) // head = 3
	srv := startWS(t, j)
	c := wsDial(t, srv.URL, "/ws/v1/events?ticket=ok&from_seq=0")
	// 省略 from_seq（连字段都不给），按 §5 是明确声明"我只要此后新事件"。
	if err := c.sendText(`{"type":"hello"}`); err != nil {
		t.Fatalf("发 hello: %v", err)
	}
	first, err := c.read(wsHelloTimeout * 2)
	if err != nil {
		t.Fatalf("首帧都没等到: %v", err)
	}
	if first.Type != "hello_ack" {
		t.Errorf("首帧类型 = %q（%s），期望 hello_ack —— query 上的游标没被省略的 hello 作废", first.Type, first.Raw)
	}
	if got, ok := first.Body["server_seq"].(float64); !ok || int64(got) != 3 {
		t.Errorf("hello_ack.server_seq = %v，期望等于 journal 头 3（替身自己装的行数，不是实现的常量）", first.Body["server_seq"])
	}
	rest, _ := c.collect(2 * wsPoll)
	if s := seqs(rest); len(s) != 0 {
		t.Errorf("hello 省略游标却补发了 seq=%v（§5：省略=只要此后新事件，绝不从 0 重放）", s)
	}
	// 反面：不补发不等于不推。追加一行，必须作为 event 帧到达。
	j.append(wsapi.Event{Seq: 4, EventID: "019e0000-0000-7000-8000-000000000004",
		EventType: "task_updated", AggregateType: "task", AggregateID: "t-1", AggregateVersion: 4})
	live, err := c.read(wsHelloTimeout)
	if err != nil {
		t.Fatalf("新事件没推下来（%v），说明「不补发」是靠不推送装出来的", err)
	}
	if live.Type != "event" {
		t.Errorf("新事件那一帧类型 = %q（%s），期望 event", live.Type, live.Raw)
	}
	if data, _ := live.Body["data"].(map[string]any); fmt.Sprint(data["seq"]) != "4" {
		t.Errorf("推下来的帧不是新那一行: %s", live.Raw)
	}
	t.Logf("F20-1 两侧：%s ⇒ 首帧 %s；随后追加 seq=4 ⇒ 收到 %s", `{"type":"hello"}`, first.Raw, live.Raw)

	// 反向对照（F20-1 的另一半）：**显式** from_seq=0 必须从头补发。
	// 缺这一条，上面的「补发了 seq=… ⇒ 判红」可能是空的：帧解析或 seqs() 坏了也会同样判绿。
	// 这里用同一套解析器跑出 [1 2 3]，才证明上一条的「空」是真没有，不是我看不见。
	j2 := newWSJournal(3)
	srv2 := startWS(t, j2)
	c2 := wsDial(t, srv2.URL, "/ws/v1/events?ticket=ok")
	if err := c2.sendText(`{"type":"hello","from_seq":0}`); err != nil {
		t.Fatalf("发 hello(from_seq=0): %v", err)
	}
	zero, _ := c2.collect(wsHelloTimeout * 2)
	gotZero := seqs(zero)
	if !equalSeqs(gotZero, []int64{1, 2, 3}) {
		t.Errorf("显式 from_seq=0 的补发 = %v，期望 [1 2 3]（§5：省略 ≠ 0）", gotZero)
	}
	t.Logf("F20-1 对照：显式 0 ⇒ 同一套解析器读到补发 %v ⇒ 上面那条「没补发」不是解析器失明", gotZero)
}

// --- F20-2 ------------------------------------------------------------------

// TestNoHelloFallsBackToQueryCursor F20-2：只给 ?from_seq= 且超时不发 hello ⇒
// 回落到 query，首帧回显 hello{from_seq=query}（**不是** hello_ack），补发恰为 seq>query 的行。
func TestNoHelloFallsBackToQueryCursor(t *testing.T) {
	j := newWSJournal(4) // head = 4
	srv := startWS(t, j)
	c := wsDial(t, srv.URL, "/ws/v1/events?ticket=ok&from_seq=2")
	// 故意一句话都不说，等过 HelloTimeout。
	frames, err := c.collect(wsHelloTimeout * 4)
	if len(frames) == 0 {
		t.Fatalf("超时后一条帧都没有: %v", err)
	}
	if frames[0].Type != "hello" {
		t.Errorf("首帧 = %q（%s），期望回显 hello —— 没有 hello 到达时 query 才生效，形状也必须是回显不是 ack", frames[0].Type, frames[0].Raw)
	}
	if got, ok := frames[0].Body["from_seq"].(float64); !ok || int64(got) != 2 {
		t.Errorf("回显的 from_seq = %v，期望 2（就是 URL 上那个值）", frames[0].Body["from_seq"])
	}
	if got, want := seqs(frames), []int64{3, 4}; !equalSeqs(got, want) {
		t.Errorf("补发 seq = %v，期望恰为 %v（seq>query、升序、不多不少不少一条）", got, want)
	}
	t.Logf("F20-2 两侧：不发 hello ⇒ 首帧 %s，补发到达序列 %v", frames[0].Raw, seqs(frames))
}

// mustAfter 直接问替身 Journal"从某个游标起会补发哪些"，用来算错读会产出的序列。
func mustAfter(t *testing.T, j *wsJournal, from int64) []int64 {
	t.Helper()
	rows, err := j.After(context.Background(), from, 1000)
	if err != nil {
		t.Fatalf("问替身 After(%d): %v", from, err)
	}
	out := make([]int64, 0, len(rows))
	for _, e := range rows {
		out = append(out, e.Seq)
	}
	return out
}

func bigger(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func equalSeqs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- F20-3 ------------------------------------------------------------------

// TestHelloCursorOverridesQuery F20-3：hello 带的游标与 query 不一致时以 hello 为准。
// 两个方向都判：query 小 / hello 大 与 query 大 / hello 小。
// 后者专门用来抓"取较大者"那种看着更保守、实则把客户端游标静默改小的实现。
func TestHelloCursorOverridesQuery(t *testing.T) {
	cases := []struct {
		name         string
		query        int64
		hello        int64
		wantBackfill []int64
	}{
		{name: "query 小于 hello", query: 0, hello: 5, wantBackfill: []int64{6}},
		{name: "query 大于 hello", query: 9, hello: 2, wantBackfill: []int64{3, 4, 5, 6}},
	}
	j := newWSJournal(6) // head = 6
	srv := startWS(t, j)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := wsDial(t, srv.URL, fmt.Sprintf("/ws/v1/events?ticket=ok&from_seq=%d", tc.query))
			if err := c.sendText(fmt.Sprintf(`{"type":"hello","from_seq":%d}`, tc.hello)); err != nil {
				t.Fatalf("发 hello: %v", err)
			}
			frames, _ := c.collect(wsHelloTimeout * 2)
			if len(frames) == 0 {
				t.Fatal("一条帧都没收到")
			}
			if frames[0].Type != "hello" {
				t.Errorf("带游标的 hello 应得到回显，实际首帧 %q（%s）", frames[0].Type, frames[0].Raw)
			}
			if got, ok := frames[0].Body["from_seq"].(float64); !ok || int64(got) != tc.hello {
				t.Errorf("回显 from_seq = %v，期望 %d（hello 的值，不是 query 的 %d）",
					frames[0].Body["from_seq"], tc.hello, tc.query)
			}
			if got := seqs(frames); !equalSeqs(got, tc.wantBackfill) {
				t.Errorf("补发 seq = %v，期望 %v（以 hello=%d 为准；query=%d 应当作废）",
					got, tc.wantBackfill, tc.hello, tc.query)
			} else {
				t.Logf("F20-3 %s：query=%d、hello=%d ⇒ 首帧 %s、补发 %v", tc.name, tc.query, tc.hello, frames[0].Raw, got)
			}
			if s := types(frames); len(s) > 0 && s[0] == "hello_ack" {
				t.Errorf("带游标却走了 ack 分支（§5 只在省略/超时时才抬到 head）：%v", s)
			}
			// 独立度检查：期望值必须**不等于**"错读"会产出的序列，否则这条断言只是把实现念了一遍。
			// §5 点名的两种错读：query 优先（旧文档那行）、取较大者（看着更保守）。
			if q := mustAfter(t, j, tc.query); equalSeqs(q, tc.wantBackfill) {
				t.Errorf("用例无区分度：期望 %v 与「query 优先」的产出相同 ⇒ 判不出 hello 有没有覆盖 query", tc.wantBackfill)
			}
			if tc.query > tc.hello { // 只有这个方向上"取较大者"与"以 hello 为准"才不同
				if m := mustAfter(t, j, bigger(tc.query, tc.hello)); equalSeqs(m, tc.wantBackfill) {
					t.Errorf("用例无区分度：期望 %v 与「取较大者」的产出相同 ⇒ 判不出是不是 max()", tc.wantBackfill)
				}
			}
			t.Logf("错读对照：query 优先会发 %v、取较大者会发 %v，而 §5 要求 %v",
				mustAfter(t, j, tc.query), mustAfter(t, j, bigger(tc.query, tc.hello)), tc.wantBackfill)
		})
	}
}
