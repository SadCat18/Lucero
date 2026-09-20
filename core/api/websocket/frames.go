package wsapi

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// 帧的读写只用本项目用得上的那一格：单帧文本、控制帧、客户端必须掩码。
const (
	maxIncoming = 64 << 10 // 客户端只该发一条 hello；超过 64KiB 直接拒
	opText      = 0x1
	opClose     = 0x8
	opPing      = 0x9
	opPong      = 0xA
)

// errProtocol 把"客户端不守协议"和"连接断了"分开：前者要回 close(1002)，后者安静收工。
var errProtocol = errors.New("wsapi: 协议错误")

type framer struct {
	c  net.Conn
	w  *bufio.Writer
	mu sync.Mutex // 出站帧只有一个生产者（主循环）会写，pong 例外，所以必须锁
}

func (f *framer) write(opcode byte, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(payload) > maxIncoming {
		return fmt.Errorf("wsapi: 出站帧 %d 字节，超过单帧上限", len(payload))
	}
	n := len(payload)
	hdr := make([]byte, 0, 10)
	hdr = append(hdr, 0x80|opcode) // FIN + opcode：出站一律单帧
	switch {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n < 1<<16:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		hdr = append(hdr, 127)
		hdr = append(hdr, b[:]...)
	}
	if _, err := f.w.Write(hdr); err != nil {
		return err
	}
	if _, err := f.w.Write(payload); err != nil {
		return err
	}
	return f.w.Flush()
}

func (f *framer) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return f.write(opText, b)
}

// read 收一帧。客户端帧必须掩码（RFC 6455 §5.1），没掩码或分片都按协议错误关掉。
func (f *framer) read(br *bufio.Reader) (opcode byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(br, h[:]); err != nil {
		return 0, nil, err
	}
	if h[0]&0x80 == 0 {
		return 0, nil, fmt.Errorf("%w: 不支持分片帧", errProtocol)
	}
	opcode = h[0] & 0x0f
	if h[1]&0x80 == 0 {
		return 0, nil, fmt.Errorf("%w: 客户端帧必须掩码", errProtocol)
	}
	n := int(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(br, b[:]); err != nil {
			return 0, nil, err
		}
		n = int(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(br, b[:]); err != nil {
			return 0, nil, err
		}
		v := binary.BigEndian.Uint64(b[:])
		if v > maxIncoming {
			return 0, nil, fmt.Errorf("%w: 入站帧超过 64KiB", errProtocol)
		}
		n = int(v)
	}
	if n > maxIncoming {
		return 0, nil, fmt.Errorf("%w: 入站帧超过 64KiB", errProtocol)
	}
	var key [4]byte
	if _, err = io.ReadFull(br, key[:]); err != nil {
		return 0, nil, err
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(br, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= key[i%4]
	}
	if opcode == opClose {
		return opClose, payload, io.EOF
	}
	return opcode, payload, nil
}

func (f *framer) close(code uint16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], code)
	_ = f.write(opClose, b[:])
	_ = f.c.Close()
}

// hello 是连接后的第一帧（契约③ §5）：{"type":"hello","from_seq":N}
type hello struct {
	Type    string `json:"type"`
	FromSeq int64  `json:"from_seq"`
}

func serve(r *http.Request, conn net.Conn, br *bufio.Reader, cfg Config, from int64) {
	f := &framer{c: conn, w: bufio.NewWriter(conn)}
	defer conn.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 等 hello。超时不踢人：面板可能先连后说话，但更常见的是握手残包，
	// 所以这里用 HelloTimeout 定死等待，超时按 from_seq=0（或 ?from_seq=）继续。
	_ = conn.SetReadDeadline(cfg.Now().Add(cfg.HelloTimeout))
	if op, payload, err := f.read(br); err == nil {
		if op == opText {
			var h hello
			if json.Unmarshal(payload, &h) == nil && h.Type == "hello" && h.FromSeq > from {
				from = h.FromSeq
			}
		}
	} else if errors.Is(err, errProtocol) {
		f.close(1002)
		return
	} else if !errors.Is(err, os.ErrDeadlineExceeded) && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		// 读 hello 出别的错： malformed 得离谱，直接关。
		f.close(1002)
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	// journal 被裁剪过：游标已经不在表里了，先出 resync_required，
	// 再退到现存最老的一行继续推（客户端按契约改走 REST 全量拉，不是我们把门关了）。
	if oldest, err := cfg.Journal.Oldest(ctx); err == nil && oldest > 0 && from < oldest-1 {
		if err := f.writeJSON(map[string]any{"type": "resync_required"}); err != nil {
			f.close(1011)
			return
		}
		from = oldest - 1
	}

	stop := make(chan struct{})
	go func() { // 只负责读：客户端 close / 断线时把主循环叫醒
		defer close(stop)
		for {
			op, payload, err := f.read(br)
			if errors.Is(err, errProtocol) {
				f.close(1002)
				return
			}
			if err != nil {
				return
			}
			switch op {
			case opPing:
				if err := f.write(opPong, payload); err != nil {
					return
				}
			case opClose:
				return
			default: // 连接建立后的文本帧本项目没有用途，读完丢掉，不解释成指令
			}
			if errors.Is(err, errProtocol) {
				f.close(1002)
				return
			}
		}
	}()

	beat := time.NewTicker(cfg.Heartbeat)
	defer beat.Stop()
	poll := time.NewTicker(cfg.Poll)
	defer poll.Stop()

	last := from
	send := func(events []Event) error {
		for _, e := range events {
			if err := f.writeJSON(map[string]any{"type": "event", "data": e}); err != nil {
				return err
			}
			last = e.Seq
		}
		return nil
	}

	for {
		events, err := cfg.Journal.After(ctx, last, cfg.Page)
		if err != nil {
			f.close(1011)
			return
		}
		if err := send(events); err != nil {
			return
		}
		if len(events) < cfg.Page {
			break // 补发完了，进实时
		}
	}

	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-beat.C:
			// 心跳故意不带 event_id：客户端据此区分"活着"与"有新事件"（契约① §2）。
			if err := f.writeJSON(map[string]any{"type": "ping", "server_seq": last}); err != nil {
				return
			}
		case <-poll.C:
			events, err := cfg.Journal.After(ctx, last, cfg.Page)
			if err != nil {
				f.close(1011)
				return
			}
			if err := send(events); err != nil {
				return
			}
		}
	}
}
