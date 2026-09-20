// Package wsapi 是 /ws/v1/events 的 transport：RFC 6455 服务端里本项目用得上的那一小块。
// 和 api/http 一样，这里零业务判断、零状态迁移（契约③ §7），事件一律来自注入的 Journal 读视图。
//
// 有意不支持分片：面板只会发一条 hello，发不出整帧的客户端不是本项目的用户；
// 收到 fin=0 就用 1003 关掉，不假装能处理。
// ponytail: 手写帧是"这台机器加不了依赖（go.mod 归主控）"的代价；
//
//	要换 gorilla/websocket 需要 REQ-WS-1（在 go.mod 上加一行 + 允许下载）。
package wsapi

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const rfc6455GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// AcceptKey 是 RFC 6455 §1.3 的握手应答。用例里钉着该章的示例向量，
// 免得我自己的实现和我自己的测试一起错到同一个方向。
func AcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + rfc6455GUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// Event 是 event_journal 的一行（契约① §2 的信封）。payload 已按列名解成对象，
// 不再让每个客户端都去二次 JSON.parse 一个字符串字段。
type Event struct {
	Seq              int64          `json:"seq"`
	EventID          string         `json:"event_id"`
	EventType        string         `json:"event_type"`
	AggregateType    string         `json:"aggregate_type"`
	AggregateID      string         `json:"aggregate_id"`
	AggregateVersion int64          `json:"aggregate_version"`
	CorrelationID    string         `json:"correlation_id,omitempty"`
	CausationID      string         `json:"causation_id,omitempty"`
	PrincipalID      string         `json:"principal_id,omitempty"`
	Payload          map[string]any `json:"payload_json"`
	CreatedAt        int64          `json:"created_at"`
}

// Journal 是本包需要的全部读能力。实现方负责只返回**已提交**的行
// （ADR-008：推送永远在事务提交之后）。
type Journal interface {
	// After 返回 seq > after 的行，按 seq 升序，最多 limit 条。
	After(ctx context.Context, after int64, limit int) ([]Event, error)
	// Oldest 返回当前还在表里的最小 seq；空表返回 0。用来判断"journal 已被裁剪"。
	Oldest(ctx context.Context) (int64, error)
}

// Tickets 是一次性短票的校验口，*api/http.Tickets 天然满足。
// 这里刻意不复用 httpapi.Tickets 类型：那样 transport 的两个子包会互相咬住。
type Tickets interface {
	Redeem(ticket string) bool
}

type Config struct {
	Tickets      Tickets
	Journal      Journal
	Page         int           // 补发时每页多少条，默认 500
	Heartbeat    time.Duration // ping 帧间隔，默认 5s
	Poll         time.Duration // 没有 EventBus 之前多久查一次新行，默认 500ms
	HelloTimeout time.Duration // 等多久客户端的 hello，默认 5s
	// AllowOrigin 为 nil 时不做来源判定；cmd/core 传进来的是 httpapi.LoopbackOrigin，
	// 与 REST 面共用同一条规则（契约③ §1.3）。
	AllowOrigin func(origin string) bool
	Now         func() time.Time
}

func (c *Config) fill() {
	if c.Page <= 0 {
		c.Page = 500
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = 5 * time.Second
	}
	if c.Poll <= 0 {
		c.Poll = 500 * time.Millisecond
	}
	if c.HelloTimeout <= 0 {
		c.HelloTimeout = 5 * time.Second
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}

// Handler 挂到 /ws/v1/events。WebSocket 不接受 Authorization 头（REQ-F-2），
// 所以口令走 ?ticket=：校验即作废、单次使用、30 秒过期，不写库不写日志。
func Handler(cfg Config) http.Handler {
	cfg.fill()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && cfg.AllowOrigin != nil && !cfg.AllowOrigin(origin) {
			http.Error(w, `{"error":{"code":"origin_not_allowed","message":"不接受该来源的跨源请求","retryable":false}}`,
				http.StatusForbidden)
			return
		}
		// 先确认这真的是个握手请求，再验票：不然一个坏握手会把一次性票白白烧掉。
		if !isUpgrade(r) {
			http.Error(w, `{"error":{"code":"upgrade_required","message":"必须走 WebSocket 握手","retryable":false}}`,
				http.StatusBadRequest)
			return
		}
		if !cfg.Tickets.Redeem(r.URL.Query().Get("ticket")) {
			// 票坏了就当没这回事：不升级、不回显原因细节给未验票的一端。
			http.Error(w, `{"error":{"code":"unauthorized","message":"票据无效或已过期","retryable":false}}`,
				http.StatusUnauthorized)
			return
		}
		conn, br, err := upgrade(w, r)
		if err != nil {
			// upgrade 里已经写好状态码了。
			return
		}
		serve(r, conn, br, cfg, initialFrom(r))
	})
}

var errNotWebSocket = errors.New("wsapi: 不是合法的 WebSocket 升级请求")

// initialFrom 取 ?from_seq=（契约① §8 的游标形态）；客户端 hello 里的 from_seq 优先。
func initialFrom(r *http.Request) int64 {
	n, err := strconv.ParseInt(r.URL.Query().Get("from_seq"), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// isUpgrade：握手三件套齐了才去验票，避免坏握手烧掉一次性凭证。
func isUpgrade(r *http.Request) bool {
	return tokenListHas(r.Header.Get("Connection"), "upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		r.Header.Get("Sec-WebSocket-Key") != ""
}

func upgrade(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.Reader, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, nil, errNotWebSocket
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, `{"error":{"code":"internal","message":"当前 listener 不支持 hijack","retryable":false}}`,
			http.StatusInternalServerError)
		return nil, nil, errors.New("wsapi: no hijacker")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + AcceptKey(key) + "\r\n\r\n"
	if _, err := io.WriteString(conn, resp); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, rw.Reader, nil
}

func tokenListHas(list, want string) bool {
	for _, part := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(part), want) {
			return true
		}
	}
	return false
}
