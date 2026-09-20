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
	// maxIncoming 只用于**出站**：单帧超过这个大小我们不发。
	maxIncoming = 64 << 10
	// maxInbound 是入站上限：契约③ §2「WebSocket 帧」第 ① 条 —— 入站必须单帧、≤125 字节有效载荷
	// （我们收的只有 {type:"resubscribe",from_seq} 一类小 JSON）。控制帧按 RFC 本来也 ≤125。
	maxInbound = 125
	opText     = 0x1
	opBinary   = 0x2
	opClose    = 0x8
	opPing     = 0x9
	opPong     = 0xA
)

// allowedInboundOpcode 实现同一条第 ③ 项：入站只允许 text/binary/close/ping/pong，其余按协议错误关。
func allowedInboundOpcode(op byte) bool {
	switch op {
	case opText, opBinary, opClose, opPing, opPong:
		return true
	}
	return false
}

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

// read 收一帧，并在分配负载之前把契约③ §2「WebSocket 帧」的入站约定逐条判完：
// 单帧（不分片）、RSV 为 0、opcode 在白名单、客户端必须掩码、有效载荷 ≤ maxInbound。
// 任一条不过都返回 errProtocol，由调用方回 close(1002)。
func (f *framer) read(br *bufio.Reader) (opcode byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(br, h[:]); err != nil {
		return 0, nil, err
	}
	if h[0]&0x80 == 0 {
		return 0, nil, fmt.Errorf("%w: 不支持分片帧", errProtocol)
	}
	// RSV1-3 只在协商了扩展时才可置位；我们从不回 Sec-WebSocket-Extensions（契约③ §2 帧约定②），
	// 所以置了就是对端违约。不挡的话会把一段压缩字节当 JSON 解，报错信息指向错的地方。
	if h[0]&0x70 != 0 {
		return 0, nil, fmt.Errorf("%w: 未协商任何扩展，RSV 必须为 0（实际 %#x）", errProtocol, h[0]&0x70)
	}
	opcode = h[0] & 0x0f
	if !allowedInboundOpcode(opcode) {
		return 0, nil, fmt.Errorf("%w: 入站 opcode %#x 不在允许集合里", errProtocol, opcode)
	}
	if h[1]&0x80 == 0 {
		return 0, nil, fmt.Errorf("%w: 客户端帧必须掩码", errProtocol)
	}
	n := int(h[1] & 0x7f)
	if n > maxInbound {
		// 长度声明本身就先判，避免后面按它分配。
		return 0, nil, fmt.Errorf("%w: 入站帧声明 %d 字节，超过 %d 上限", errProtocol, n, maxInbound)
	}
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(br, b[:]); err != nil {
			return 0, nil, err
		}
		n = int(binary.BigEndian.Uint16(b[:]))
		if n > maxInbound {
			return 0, nil, fmt.Errorf("%w: 入站帧 %d 字节，超过 %d 上限", errProtocol, n, maxInbound)
		}
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(br, b[:]); err != nil {
			return 0, nil, err
		}
		// 64 位长度分支：在 make([]byte, n) 之前判，巨型声明撑不出内存分配。
		v := binary.BigEndian.Uint64(b[:])
		if v > maxInbound {
			return 0, nil, fmt.Errorf("%w: 入站帧声明 %d 字节，超过 %d 上限", errProtocol, v, maxInbound)
		}
		n = int(v)
	}
	if n > maxInbound {
		return 0, nil, fmt.Errorf("%w: 入站帧 %d 字节，超过 %d 上限", errProtocol, n, maxInbound)
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

// hello 是连接后的第一帧（契约③ §5 / F-16）：{"type":"hello","from_seq":N}。
// from_seq 用指针：客户端**省略**它和**给 0** 是两回事（省略=只要此后新事件；0=从头补发），
// OpenAPI 的 WsClientHello 也允许显式 null，两种都归"省略"。
type hello struct {
	Type    string `json:"type"`
	FromSeq *int64 `json:"from_seq"`
}

func serve(r *http.Request, conn net.Conn, br *bufio.Reader, cfg Config, from int64, declared bool) {
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
			// F-16：hello 的 from_seq **覆盖** ?from_seq=，不是取较大者——"客户端 hello 优先"。
			// 带 from_seq = 从那里补发；省略或 null = 只要此后新事件（起点由 Head 定，绝不从 0 重放）。
			if json.Unmarshal(payload, &h) == nil && h.Type == "hello" {
				if h.FromSeq != nil && *h.FromSeq >= 0 {
					from, declared = *h.FromSeq, true
				} else {
					declared = false
				}
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

	// 首帧（REQ-F-17）：journal 空不空都必须先说一句话，绝不让客户端停在"无首帧"。
	// 带游标 → 回 hello 回显（客户端只当就绪信号，不据此改游标）；
	// 不带 → 把起点抬到 journal 头并回 hello_ack{server_seq}，客户端必须把它记成新游标。
	// 后一条同时堵掉"缺省即从 0 全量重放"（OpenAPI cursor-default：journal 大时等于自己 DDOS 自己）。
	if declared {
		if err := f.writeJSON(map[string]any{"type": "hello", "from_seq": from}); err != nil {
			f.close(1011)
			return
		}
	} else {
		head, err := cfg.Journal.Head(ctx)
		if err != nil {
			f.close(1011)
			return
		}
		from = head
		if err := f.writeJSON(map[string]any{"type": "hello_ack", "server_seq": head}); err != nil {
			f.close(1011)
			return
		}
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
