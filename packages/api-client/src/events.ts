import type { components } from './gen/schema';

type S = components['schemas'];
export type EventEntry = S['EventJournalEntry'];
/** 帧集合一律取生成物，不在这里重写一遍（硬约束 2） */
export type WsFrame = S['WsFrame'];

/** 面板顶部的链路状态。`stalled` 与 `live` 必须可区分：§5 说 UI 用 ping 区分"活着"与"卡住" */
export type LinkState = 'connecting' | 'live' | 'stalled' | 'reconnecting' | 'closed';

export interface EventStreamOptions {
  /**
   * 由调用方拼完整 ws(s) URL。§2.1 裁定后这里必须能异步换一次性短票
   * （票单次使用 → 每次重连都要重新申请），所以返回 Promise 也合法。
   */
  url: (fromSeq: number | null) => string | Promise<string>;
  /** 起始游标；null = 不带 from_seq（其缺省语义契约③ 未定义 → REQ-F-9） */
  fromSeq?: number | null;
  onEvent: (event: EventEntry) => void;
  /** journal 已裁剪：调用方必须回去走 REST 全量拉，不得用部分事件流假装状态完整 */
  onResync: (info: { serverSeq: number | null }) => void;
  onState: (state: LinkState, detail?: string) => void;
  onSeq?: (seq: number) => void;
  stallMs?: number;
  backoffMs?: number[];
  socketFactory?: (url: string) => WebSocket;
}

export function openEventStream(o: EventStreamOptions) {
  const stallMs = o.stallMs ?? 15_000;
  const backoff = o.backoffMs ?? [250, 1000, 4000];
  const socket = o.socketFactory ?? ((u: string) => new WebSocket(u));
  let lastSeq: number | null = o.fromSeq ?? null;
  let ws: WebSocket | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let attempt = 0;
  let closed = false;

  const arm = () => {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => o.onState('stalled', `${stallMs}ms 内既无事件也无 ping`), stallMs);
  };
  const disarm = () => { if (timer) clearTimeout(timer); timer = null; };

  function handle(raw: string) {
    let f: WsFrame;
    try {
      f = JSON.parse(raw) as WsFrame;
    } catch {
      o.onState('stalled', '无法解析的帧');
      return;
    }
    arm();
    switch (f.type) {
      case 'hello':
        attempt = 0;
        o.onState('live');
        break;
      case 'hello_ack':
        // F-9：不带 from_seq 时只收此后新事件。游标必须抬到 journal 头，
        // 否则一次没有任何事件的重连会永远落在旧位置上。
        attempt = 0;
        if (lastSeq === null || f.server_seq > lastSeq) {
          lastSeq = f.server_seq;
          o.onSeq?.(f.server_seq);
        }
        o.onState('live');
        break;
      case 'ping':
        attempt = 0;
        o.onState('live');
        break;
      case 'event':
        // 重连补发可能重叠：游标只前进，重复事件按 seq 幂等丢弃
        if (lastSeq !== null && f.data.seq <= lastSeq) break;
        lastSeq = f.data.seq;
        o.onEvent(f.data);
        o.onSeq?.(f.data.seq);
        break;
      case 'resync_required':
        o.onResync({ serverSeq: lastSeq });
        lastSeq = null;
        reconnect();
        break;
    }
  }

  async function connect() {
    if (closed) return;
    o.onState(attempt ? 'reconnecting' : 'connecting');
    let target: string;
    try {
      target = await o.url(lastSeq);
    } catch (err) {
      // 换短票失败（Core 没起来 / 票被拒）也按退避重来，不谎报成已连接
      const wait = backoff[Math.min(attempt, backoff.length - 1)];
      attempt++;
      o.onState('reconnecting', `握手前换票失败：${String(err)}（${wait}ms 后重试）`);
      timer = setTimeout(connect, wait);
      return;
    }
    if (closed) return;
    arm();
    ws = socket(target);
    ws.onopen = () => {
      arm();
      // F-16：服务端等客户端先报游标，不说就一直不出控制帧。
      // 不带 from_seq 时只发 {type:"hello"}，让服务端把起点定在 journal 头。
      ws?.send(lastSeq === null ? '{"type":"hello"}' : JSON.stringify({ type: 'hello', from_seq: lastSeq }));
    };
    ws.onmessage = (e: MessageEvent) => handle(typeof e.data === 'string' ? e.data : String(e.data));
    ws.onerror = () => o.onState('reconnecting', 'socket error');
    ws.onclose = () => {
      disarm();
      if (closed) return o.onState('closed');
      const wait = backoff[Math.min(attempt, backoff.length - 1)];
      attempt++;
      o.onState('reconnecting', `${wait}ms 后重连`);
      timer = setTimeout(connect, wait);
    };
  }

  function reconnect() {
    ws?.close();
    connect();
  }

  connect();
  return {
    get seq() { return lastSeq; },
    /** 测试与热重载用的注入口 */
    feed(raw: string) { handle(raw); },
    close() {
      closed = true;
      disarm();
      ws?.close();
    },
  };
}

export type EventStream = ReturnType<typeof openEventStream>;
