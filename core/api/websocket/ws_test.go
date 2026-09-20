package wsapi_test

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	wsapi "github.com/SadCat18/lucero/core/api/websocket"
)

// acceptKey 的正确性用 RFC 6455 §1.3 的示例向量当外部锚点：
// 我自己的实现配我自己写的客户端，两边一起错也是绿的，只有 RFC 给的常量不会。
func TestAcceptKeyIsTheRFCVector(t *testing.T) {
	got := wsapi.AcceptKey("dGhlIHNhbXBsZSBub25jZQ==")
	if got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("Sec-WebSocket-Accept = %q，RFC 6455 §1.3 要求 s3pPLMBiTxaQ9kYGzzhZRbK+xOo=", got)
	}
}

// 帧约定②：服务端不协商任何扩展。握手响应里一旦出现 Sec-WebSocket-Extensions，浏览器就会
// 启用 permessage-deflate，而我们没有解压器——第一条压缩帧会变成一坨被当作 JSON 解析的乱码。
func TestHandshakeNegotiatesNoExtensions(t *testing.T) {
	srv := serve(t, &fakeJournal{}, &stubTickets{valid: map[string]int{"ok": 1}})
	_, resp := dial(t, srv.URL, "/ws/v1/events?ticket=ok", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("握手应 101，实际 %d", resp.StatusCode)
	}
	for _, h := range []string{"Sec-WebSocket-Extensions", "Sec-WebSocket-Protocol"} {
		if v := resp.Header.Get(h); v != "" {
			t.Errorf("握手不该协商 %s，却回了 %q", h, v)
		}
	}
	// dial 用的就是 RFC 6455 §1.3 的示例 key，所以这里能顺带钉住真回了那个 Accept。
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("Sec-WebSocket-Accept = %q", got)
	}
}

// --- 替身 ---

type stubTickets struct {
	mu       sync.Mutex
	valid    map[string]int // ticket → 可 Redeem 次数
	redeemed int
}

func (s *stubTickets) Redeem(t string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redeemed++
	if n := s.valid[t]; n > 0 {
		s.valid[t] = n - 1
		return true
	}
	return false
}

func (s *stubTickets) burns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.redeemed
}

type fakeJournal struct {
	mu     sync.Mutex
	events []wsapi.Event
	oldest int64
}

func (f *fakeJournal) add(e wsapi.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *fakeJournal) After(_ context.Context, after int64, limit int) ([]wsapi.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []wsapi.Event
	for _, e := range f.events {
		if e.Seq > after {
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeJournal) Oldest(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.oldest, nil
}

// Head 直接取传入事件的最高 seq（测试里 events 就是全表）。
func (f *fakeJournal) Head(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var head int64
	for _, e := range f.events {
		if e.Seq > head {
			head = e.Seq
		}
	}
	return head, nil
}

func ev(seq int64, typ string) wsapi.Event {
	return wsapi.Event{Seq: seq, EventID: fmt.Sprintf("e%d", seq), EventType: typ,
		AggregateType: "task", AggregateID: "t-1", AggregateVersion: seq,
		Payload: map[string]any{"schema": 1}, CreatedAt: 1000 + seq}
}

// --- 极简 WebSocket 客户端（Go 标准库没有客户端，只能自己发帧）---

type wsClient struct {
	c  net.Conn
	br *bufio.Reader
	r  *rand.Rand
}

func dial(t *testing.T, srv, path, ticket string, origin string) (*wsClient, *http.Response) {
	t.Helper()
	u := strings.TrimPrefix(srv, "http://")
	c, err := net.Dial("tcp", u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	key := "dGhlIHNhbXBsZSBub25jZQ==" // 复用 RFC 示例，顺手验证应答
	req := "GET " + path + " HTTP/1.1\r\nHost: " + u + "\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	req += "\r\n"
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	return &wsClient{c: c, br: br, r: rand.New(rand.NewSource(7))}, resp
}

func (cl *wsClient) sendText(s string) error {
	var mask [4]byte
	if _, err := cl.r.Read(mask[:]); err != nil {
		return err
	}
	payload := []byte(s)
	out := make([]byte, 0, 6+len(payload))
	out = append(out, 0x81)
	out = append(out, 0x80|byte(len(payload))) // 客户端必须掩码，长度 <126
	out = append(out, mask[:]...)
	for i, b := range payload {
		out = append(out, b^mask[i%4])
	}
	_, err := cl.c.Write(out)
	return err
}

func (cl *wsClient) readText(deadline time.Duration) (string, error) {
	if err := cl.c.SetReadDeadline(time.Now().Add(deadline)); err != nil {
		return "", err
	}
	var h [2]byte
	if _, err := io.ReadFull(cl.br, h[:]); err != nil {
		return "", err
	}
	opcode := h[0] & 0x0f
	n := int(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(cl.br, b[:]); err != nil {
			return "", err
		}
		n = int(binary.BigEndian.Uint16(b[:]))
	case 127:
		return "", errors.New("测试里不会出现 16 位长度")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(cl.br, payload); err != nil {
		return "", err
	}
	if opcode != 0x1 {
		return "", fmt.Errorf(" opcode=%d 不是文本帧", opcode)
	}
	return string(payload), nil
}

func (cl *wsClient) mustFrame(t *testing.T, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		line, err := cl.readText(time.Until(deadline))
		if err != nil {
			t.Fatalf("等 %q 帧失败: %v", want, err)
		}
		var f map[string]any
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Fatalf("帧不是 JSON: %q", line)
		}
		if f["type"] == want {
			return f
		}
		if f["type"] == "ping" && want != "ping" {
			continue // 心跳会插在任何时候，不该被当成协议违约
		}
		if (f["type"] == "hello" || f["type"] == "hello_ack") && want != "hello" && want != "hello_ack" {
			continue // 首帧（REQ-F-17）在补发之前先到，等 event 的用例不该因此判死
		}
		t.Fatalf("收到意外帧: %q（在等 %q）", line, want)
	}
}

// firstFrame 取**字面第一帧**，什么都不跳过。REQ-F-17 的判据就是"第一帧必须存在且语义正确"，
// 用会跳过无关帧的 mustFrame 去测"首帧"等于没测。
func (cl *wsClient) firstFrame(t *testing.T) map[string]any {
	t.Helper()
	line, err := cl.readText(3 * time.Second)
	if err != nil {
		t.Fatalf("等首帧失败（这就是 REQ-F-17 的原始症状）: %v", err)
	}
	var f map[string]any
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		t.Fatalf("首帧不是 JSON: %q", line)
	}
	return f
}

// --- 用例 ---

func serve(t *testing.T, j wsapi.Journal, tk wsapi.Tickets) *httptest.Server {
	t.Helper()
	h := wsapi.Handler(wsapi.Config{
		Tickets: tk, Journal: j, Heartbeat: 60 * time.Millisecond, Poll: 20 * time.Millisecond,
		HelloTimeout: time.Second, Now: time.Now,
		AllowOrigin: func(origin string) bool { return origin == "" || strings.Contains(origin, "127.0.0.1") },
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestBadTicketNeverUpgrades(t *testing.T) {
	tk := &stubTickets{valid: map[string]int{"good": 1}}
	srv := serve(t, &fakeJournal{}, tk)

	cl, resp := dial(t, srv.URL, "/ws/v1/events?ticket=nope", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("坏票应 401，实际 %d", resp.StatusCode)
	}
	if resp.Header.Get("Upgrade") != "" {
		t.Error("坏票却升级了")
	}
	_ = cl
}

// 坏握手不许烧掉一次性凭证：先判定是不是真握手，再 Redeem。
func TestMalformedHandshakeDoesNotBurnTicket(t *testing.T) {
	tk := &stubTickets{valid: map[string]int{"t-1": 1}}
	srv := serve(t, &fakeJournal{}, tk)

	u := strings.TrimPrefix(srv.URL, "http://")
	c, err := net.Dial("tcp", u)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	io.WriteString(c, "GET /ws/v1/events?ticket=t-1 HTTP/1.1\r\nHost: "+u+"\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("没有握手头的请求应 400，实际 %d", resp.StatusCode)
	}
	if got := tk.burns(); got != 0 {
		t.Errorf("被拒的坏握手烧掉了 %d 张一次性票", got)
	}
}

func TestOriginGuardRunsBeforeTheTicket(t *testing.T) {
	tk := &stubTickets{valid: map[string]int{"t-2": 1}}
	srv := serve(t, &fakeJournal{}, tk)

	_, resp := dial(t, srv.URL, "/ws/v1/events?ticket=t-2", "", "https://evil.example")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("外部 Origin 应 403，实际 %d", resp.StatusCode)
	}
	if got := tk.burns(); got != 0 {
		t.Errorf("被 CORS 挡掉的请求烧掉了 %d 张票", got)
	}
}

// 补发 → 实时 → 心跳：一帧一帧按契约① §2 的形状核对。
func TestBackfillThenLiveThenHeartbeat(t *testing.T) {
	j := &fakeJournal{events: []wsapi.Event{ev(1, "task_created"), ev(2, "task_queued")}}
	srv := serve(t, j, &stubTickets{valid: map[string]int{"ok": 1}})

	cl, resp := dial(t, srv.URL, "/ws/v1/events?ticket=ok", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("握手应 101，实际 %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("握手应答里的 Accept 不符 RFC 向量: %q", got)
	}

	if err := cl.sendText(`{"type":"hello","from_seq":0}`); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		seq  float64
		typ  string
		name string
	}{{1, "task_created", "seq1"}, {2, "task_queued", "seq2"}} {
		f := cl.mustFrame(t, "event")
		data, _ := f["data"].(map[string]any)
		if data["seq"] != want.seq || data["event_type"] != want.typ {
			t.Errorf("补发帧不对: %v", data)
		}
		// payload_json 必须是解好对象的形状，不能让前端二次 parse。
		if p, ok := data["payload_json"].(map[string]any); !ok || p["schema"] == nil {
			t.Errorf("payload_json 没解成对象: %T", data["payload_json"])
		}
	}

	j.add(ev(3, "task_running"))
	f := cl.mustFrame(t, "event")
	data, _ := f["data"].(map[string]any)
	if data["seq"] != float64(3) {
		t.Errorf("实时增量没推下来: %v", data)
	}

	ping := cl.mustFrame(t, "ping")
	if ping["server_seq"] != float64(3) {
		t.Errorf("ping 的 server_seq = %v，应为已推送的最大 seq 3", ping["server_seq"])
	}
	if _, bad := ping["event_id"]; bad {
		t.Error("心跳故意不能带 event_id（契约① §2），带了就等于伪造空事件")
	}
}

// 游标比 journal 还老 → 第一帧 resync_required，然后从现存最老的行继续推。
func TestResyncRequiredWhenJournalTrimmed(t *testing.T) {
	j := &fakeJournal{events: []wsapi.Event{ev(10, "task_running")}, oldest: 10}
	srv := serve(t, j, &stubTickets{valid: map[string]int{"ok": 1}})

	cl, resp := dial(t, srv.URL, "/ws/v1/events?ticket=ok", "", "")
	defer resp.Body.Close()
	if err := cl.sendText(`{"type":"hello","from_seq":3}`); err != nil {
		t.Fatal(err)
	}
	if f := cl.firstFrame(t); f["type"] != "resync_required" {
		t.Fatalf("resync_required 必须是字面第一帧（客户端得先知道游标不可用，再谈就绪），实际 %+v", f)
	}
	f := cl.mustFrame(t, "event")
	data, _ := f["data"].(map[string]any)
	if data["seq"] != float64(10) {
		t.Errorf("resync 之后应退到现存最老的一行，实际 %v", data)
	}
}

// REQ-F-17 的解除条件本身：journal 空不空都必须有首帧，且首帧的类型由"客户端有没有声明游标"决定。
// 这里读的是**字面第一帧**（firstFrame 不做任何跳过）——把 hello_ack 藏在一条 ping 后面，
// 面板照样停在 connecting，所以"迟早会发"不算过。
func TestFirstFrameAlwaysArrives(t *testing.T) {
	filled := func() *fakeJournal {
		return &fakeJournal{events: []wsapi.Event{ev(41, "task_created"), ev(42, "task_queued")}}
	}
	cases := []struct {
		name     string
		j        *fakeJournal
		extraQ   string // 追加在 ticket 之后的查询串
		send     string // 客户端的第一帧；空 = 故意不说，走 HelloTimeout
		wantType string
		wantKey  string
		wantVal  float64
	}{
		{"空 journal + 不带游标 → hello_ack{server_seq:0}", &fakeJournal{}, "", `{"type":"hello"}`,
			"hello_ack", "server_seq", 0},
		{"空 journal + 显式 from_seq=0 → hello 回显", &fakeJournal{}, "", `{"type":"hello","from_seq":0}`,
			"hello", "from_seq", 0},
		{"有历史 + 不带游标 → 抬到 journal 头", filled(), "", `{"type":"hello"}`,
			"hello_ack", "server_seq", 42},
		{"有历史 + 带游标 → 只回显，不动游标", filled(), "", `{"type":"hello","from_seq":41}`,
			"hello", "from_seq", 41},
		{"from_seq 显式 null 归入\"没声明\"", filled(), "", `{"type":"hello","from_seq":null}`,
			"hello_ack", "server_seq", 42},
		{"?from_seq= 单独用（hello 都没发）", filled(), "&from_seq=41", "",
			"hello", "from_seq", 41},
		{"客户端一句话不说，超时后仍要有首帧", &fakeJournal{}, "", "",
			"hello_ack", "server_seq", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := serve(t, c.j, &stubTickets{valid: map[string]int{"ok": 1}})
			cl, resp := dial(t, srv.URL, "/ws/v1/events?ticket=ok"+c.extraQ, "", "")
			defer resp.Body.Close()
			if c.send != "" {
				if err := cl.sendText(c.send); err != nil {
					t.Fatal(err)
				}
			}
			f := cl.firstFrame(t)
			if f["type"] != c.wantType {
				t.Fatalf("第一帧应是 %q，实际 %+v", c.wantType, f)
			}
			if got, ok := f[c.wantKey].(float64); !ok || got != c.wantVal {
				t.Errorf("%s = %v，期望 %v", c.wantKey, f[c.wantKey], c.wantVal)
			}
		})
	}
}

// F-16 的"hello 优先"与 OpenAPI cursor-default 禁止的那件事：缺省游标不能退化成从 0 全量重放。
func TestHelloOverridesQueryAndMissingCursorNeverReplays(t *testing.T) {
	j := &fakeJournal{events: []wsapi.Event{ev(1, "task_created"), ev(2, "task_queued"), ev(3, "task_running")}}
	// 两次连接要两张票：短票本来就是单次使用（§1.2），给 1 的话第二条连接会被 401 掉，
	// 于是 readText 读到的是一段 HTTP 错误正文，报出来的 opcode 全是乱的（我第一版就撞在这上面）。
	srv := serve(t, j, &stubTickets{valid: map[string]int{"ok": 2}})

	// query 想要全量（from_seq=0），hello 只要 2 之后 → 服务端必须听 hello 的。
	cl, resp := dial(t, srv.URL, "/ws/v1/events?ticket=ok&from_seq=0", "", "")
	defer resp.Body.Close()
	if err := cl.sendText(`{"type":"hello","from_seq":2}`); err != nil {
		t.Fatal(err)
	}
	if f := cl.firstFrame(t); f["type"] != "hello" || f["from_seq"] != float64(2) {
		t.Fatalf("hello 没有覆盖 ?from_seq=：%+v", f)
	}
	data, _ := cl.mustFrame(t, "event")["data"].(map[string]any)
	if data["seq"] != float64(3) {
		t.Errorf("覆盖后应只推 seq>2，实际 %+v", data)
	}

	// 什么都不声明：起点必须落在 journal 头（3），此后 200ms 内一条 event 都不许出现。
	cl2, resp2 := dial(t, srv.URL, "/ws/v1/events?ticket=ok", "", "")
	defer resp2.Body.Close()
	if err := cl2.sendText(`{"type":"hello"}`); err != nil {
		t.Fatal(err)
	}
	if f := cl2.firstFrame(t); f["type"] != "hello_ack" || f["server_seq"] != float64(3) {
		t.Fatalf("起点没抬到 journal 头：%+v", f)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		line, err := cl2.readText(time.Until(deadline))
		if err != nil {
			break // 没东西可读正是我们要的
		}
		if strings.Contains(line, `"type":"event"`) {
			t.Fatalf("不带游标却重放了历史：%s", line)
		}
	}
}

// 分片帧、未掩码帧都要被按协议错误关掉，不能被当成正常输入。
func TestProtocolViolationsAreClosed(t *testing.T) {
	srv := serve(t, &fakeJournal{}, &stubTickets{valid: map[string]int{"ok": 1}})
	cl, resp := dial(t, srv.URL, "/ws/v1/events?ticket=ok", "", "")
	defer resp.Body.Close()

	if _, err := cl.c.Write([]byte{0x01, 0x05, 'h', 'e', 'l', 'l', 'o'}); err != nil { // fin=0 且未掩码
		t.Fatal(err)
	}
	_ = cl.c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var h [2]byte
	if _, err := io.ReadFull(cl.br, h[:]); err != nil {
		t.Fatalf("协议违约后服务端没有回应帧: %v", err)
	}
	if h[0]&0x0f != 0x8 {
		t.Errorf("期望收到 close 帧，实际 opcode=%#x", h[0]&0x0f)
	}
}

// 契约③ §2「WebSocket 帧」的 ①②③ 三条必须在服务端真拦住：
// 未知 opcode（③）、**长度声明**超过 125 字节的入站帧含 16/64 位扩展长度（①，
// 且 64 位那条要在分配与读负载之前就拒掉，不能挂在那儿等 1GiB）、置了 RSV 的帧（②的收侧，
// 见 TestHandshakeNegotiatesNoExtensions）。
func TestInboundLimitsAndOpcodeWhitelist(t *testing.T) {
	cases := []struct {
		name     string
		opcode   byte
		rsv      byte
		extended int
		declared uint64
	}{
		{"未分配的 opcode 0x3", 0x3, 0, 7, 0},
		{"控制用途的 opcode 0xB", 0xB, 0, 7, 0},
		{"16 位长度声明 200 字节", 0x1, 0, 16, 200},
		{"64 位长度声明 1GiB", 0x1, 0, 64, 1 << 30},
		{"置了 RSV1（等于偷开 permessage-deflate）", 0x1, 0x40, 7, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := serve(t, &fakeJournal{}, &stubTickets{valid: map[string]int{"ok": 1}})
			cl, resp := dial(t, srv.URL, "/ws/v1/events?ticket=ok", "", "")
			defer resp.Body.Close()
			if err := writeRawFrameErr(cl, c.opcode, c.rsv, c.extended, c.declared); err != nil {
				t.Fatal(err)
			}
			_ = cl.c.SetReadDeadline(time.Now().Add(2 * time.Second))
			var h [2]byte
			if _, err := io.ReadFull(cl.br, h[:]); err != nil {
				t.Fatalf("服务端没有反应（可能挂在读负载上）: %v", err)
			}
			if h[0]&0x0f != 0x8 {
				t.Errorf("期望 close 帧，实际 opcode=%#x", h[0]&0x0f)
			}
		})
	}
}

func writeRawFrameErr(cl *wsClient, opcode, rsv byte, extended int, declared uint64) error {
	var mask [4]byte
	if _, err := cryptorand.Read(mask[:]); err != nil {
		return err
	}
	hdr := []byte{0x80 | rsv | opcode}
	switch extended {
	case 7:
		hdr = append(hdr, 0x80|byte(declared))
	case 16:
		hdr = append(hdr, 0x80|126, byte(declared>>8), byte(declared))
	case 64:
		hdr = append(hdr, 0x80|127)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], declared)
		hdr = append(hdr, b[:]...)
	}
	hdr = append(hdr, mask[:]...)
	_, err := cl.c.Write(hdr)
	return err
}
