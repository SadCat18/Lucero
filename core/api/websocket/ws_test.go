package wsapi_test

import (
	"bufio"
	"context"
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
		t.Fatalf("收到意外帧: %q（在等 %q）", line, want)
	}
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
	if f := cl.mustFrame(t, "resync_required"); f["type"] != "resync_required" {
		t.Fatal("first frame must be resync_required")
	}
	f := cl.mustFrame(t, "event")
	data, _ := f["data"].(map[string]any)
	if data["seq"] != float64(10) {
		t.Errorf("resync 之后应退到现存最老的一行，实际 %v", data)
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
