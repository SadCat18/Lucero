package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// TestRealCoreStreamsRealJournalRows 是整条链的最小组合：
// 真 Core 进程内启动 → REST 签一张票 → 真 SQLite 里建一行 Task（含事件）→
// 走真 WebSocket 收帧 → 再补一行看它是否被实时推下来。
// 中间没有任何一环是替身，所以 journal 的 SQL、平台层补的 "schema":1、
// 一次性短票、帧格式这四件事只要有一处错，这条就会红。
func TestRealCoreStreamsRealJournalRows(t *testing.T) {
	core := bootAndServe(t)
	base := "http://" + core.endpoint

	ticket := issueTicket(t, base, core.token)

	const title1 = "骨架联调-1"
	if err := writeTaskEvent(t, core, "t-ws-1", "task_created", title1); err != nil {
		t.Fatal(err)
	}

	ws := dialWS(t, core.endpoint, "/ws/v1/events?ticket="+ticket)
	defer ws.Close()
	if err := writeWS(ws, `{"type":"hello","from_seq":0}`); err != nil {
		t.Fatal(err)
	}

	first := readWSFrame(t, ws)
	if first["type"] != "event" {
		t.Fatalf("第一帧应是 event，实际 %v", first)
	}
	data, _ := first["data"].(map[string]any)
	if data["event_type"] != "task_created" || data["aggregate_type"] != "task" || data["aggregate_id"] != "t-ws-1" {
		t.Errorf("帧里的信封不对: %v", data)
	}
	payload, ok := data["payload_json"].(map[string]any)
	if !ok || payload["schema"] != float64(1) {
		t.Errorf(`payload_json 应已解成对象且含平台补的 "schema":1，实际 %T %v`, data["payload_json"], data["payload_json"])
	}
	if payload["title"] != title1 {
		t.Errorf("payload 内容不对: %v", payload)
	}

	// 连接之后再落一行，验"提交之后才推"不是靠谁通知的（现在还没有 EventBus）。
	if err := writeTaskEvent(t, core, "t-ws-2", "task_created", "骨架联调-2"); err != nil {
		t.Fatal(err)
	}
	second := readWSFrameWithType(t, ws, "event")
	d2, _ := second["data"].(map[string]any)
	if d2["aggregate_id"] != "t-ws-2" {
		t.Errorf("实时增量没推下来: %v", d2)
	}
	if seq1, _ := data["seq"].(float64); seq1 >= d2["seq"].(float64) {
		t.Errorf("seq 必须严格递增：%v -> %v", seq1, d2["seq"])
	}

}

// --- 极简 WS 客户端（Go 标准库没有客户端）---

func dialWS(t *testing.T, hostPort, path string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", hostPort)
	if err != nil {
		t.Fatal(err)
	}
	req := "GET " + path + " HTTP/1.1\r\nHost: " + hostPort + "\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("握手应 101，实际 %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("Accept 不符 RFC 向量: %q", got)
	}
	// 把已缓冲的字节挪回连接： bufio 可能已经把 101 + 首帧一起吃进来了。
	return &bufferedConn{Conn: c, br: br}
}

func writeWS(c net.Conn, s string) error {
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	payload := []byte(s)
	frame := append([]byte{0x81, 0x80 | byte(len(payload))}, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := c.Write(frame)
	return err
}

func readWSFrame(t *testing.T, c net.Conn) map[string]any {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var br *bufio.Reader
	if bc, ok := c.(*bufferedConn); ok {
		br = bc.br
	} else {
		br = bufio.NewReader(c)
	}
	var h [2]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		t.Fatalf("读帧头失败: %v", err)
	}
	n := int(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		io.ReadFull(br, b[:])
		n = int(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		io.ReadFull(br, b[:])
		n = int(binary.BigEndian.Uint64(b[:]))
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(br, payload); err != nil {
		t.Fatalf("读帧体失败: %v", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(payload, &frame); err != nil {
		t.Fatalf("帧不是 JSON: %q", payload)
	}
	return frame
}

func readWSFrameWithType(t *testing.T, c net.Conn, want string) map[string]any {
	t.Helper()
	for {
		f := readWSFrame(t, c)
		if f["type"] == want {
			return f
		}
		if f["type"] == "ping" {
			continue // 心跳插在哪都不算违约
		}
		t.Fatalf("收到意外帧: %v", f)
	}
}

// bufferedConn 把握手时被读进缓冲区的字节还给后续读取。
type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.br.Read(p) }

// --- 真库写入 ---

var principalOnce sync.Once

func seedPrincipalOnce(t *testing.T, core *Core) {
	t.Helper()
	principalOnce.Do(func() {
		if _, err := core.db.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES ('system', 'SYSTEM', 'Core', 1, 1)`); err != nil {
			t.Fatal(err)
		}
	})
}

func writeTaskEvent(t *testing.T, core *Core, id, eventType, title string) error {
	// event_journal.principal_id 有外键，主体行先建一次即可（靠 Read 池，不在写事务里做测试准备）。
	seedPrincipalOnce(t, core)
	return core.db.Write(context.Background(), func(tx *db.Tx) error {
		m, err := json.Marshal(map[string]any{"title": title, "priority": "HIGH", "mode": "EXECUTE",
			"risk_level": "LOW"})
		if err != nil {
			return err
		}
		payload := map[string]any{}
		if err := json.Unmarshal(m, &payload); err != nil {
			return err
		}
		return tx.Create(db.AggTask, id,
			[]string{"title", "created_at", "updated_at"}, []any{title, int64(1), int64(1)},
			db.EventDraft{EventID: "ev-" + id, Type: eventType, Aggregate: db.AggTask,
				AggregateID: id, PrincipalID: "system", Payload: payload})
	})
}

func issueTicket(t *testing.T, base, token string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/ws-ticket", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("签票应 200，实际 %d", resp.StatusCode)
	}
	var body struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Ticket == "" {
		t.Fatal("签票返回空票")
	}
	return body.Ticket
}

// 用完的票再拿来连等价于"坏票"：Redeem 已把它删掉，所以那条路径与
// api/websocket 里 TestBadTicketNeverUpgrades 是同一条，不在此重复。
