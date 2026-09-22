// 游标 / 去重 / resync 回落 / 链投影 —— 用假 socket 喂帧，不碰真实网络，也不冒充联调。
import { describe, expect, it } from 'vitest';
import { CHAIN, chain, connectEvents, data, link, runChain, stepDone, stepState } from '../stores/panel';
import type { EventEntry } from '@lucero/api-client/events';

type EventType = EventEntry['event_type'];

const ev = (seq: number, event_type: string): EventEntry => ({
  seq, event_id: `e${seq}`, event_type: event_type as EventType, aggregate_type: 'task',
  aggregate_id: 'agg-1', aggregate_version: 1, created_at: 1,
});

interface FakeSocket {
  url: string; closed: boolean; sent: string[];
  close(): void;
  send(data: string): void;
  onopen: (() => void) | null;
  onmessage: ((e: { data: string }) => void) | null;
  onerror: (() => void) | null;
  onclose: (() => void) | null;
}
const made: FakeSocket[] = [];
const socketFactory = (url: string) => {
  const s: FakeSocket = {
    url, closed: false, sent: [],
    close() { this.closed = true; },
    send(d: string) { this.sent.push(d); },
    onopen: null, onmessage: null, onerror: null, onclose: null,
  };
  made.push(s);
  return s as unknown as WebSocket;
};
const open = (i = 0) => made[i]?.onopen?.();
const tick = () => new Promise((r) => setTimeout(r, 0));

describe('F-16：客户端必须先发 hello', () => {
  it('无游标 → 只发 {type:"hello"}，服务端才把起点定在 journal 头', async () => {
    made.length = 0; link.seq = null;
    const s = connectEvents({ url, socketFactory });
    await tick(); open();
    expect(JSON.parse(made[0].sent[0])).toEqual({ type: 'hello' });
    s.close();
  });
  it('有游标 → 带 from_seq；面板自己伪造 hello_ack 不算已就绪', async () => {
    made.length = 0; link.seq = 77;
    const s = connectEvents({ url, socketFactory });
    await tick(); open();
    expect(JSON.parse(made[0].sent[0])).toEqual({ type: 'hello', from_seq: 77 });
    link.state = 'connecting';
    s.feed(JSON.stringify({ type: 'hello', from_seq: 77 }));
    expect(link.state).toBe('live');           // 服务端回显 hello = 就绪
    s.feed(JSON.stringify({ type: 'hello_ack', server_seq: 90 }));
    expect(link.seq).toBe(90);                 // 服务端 hello_ack 抬游标
    s.feed(JSON.stringify({ type: 'hello_ack', server_seq: 12 }));
    expect(link.seq).toBe(90);                 // 更低的 server_seq 不回退（REQ-F-17 的客户端防御）
    s.close();
  });
});
const urls: (number | null)[] = [];
const url = (fromSeq: number | null) => { urls.push(fromSeq); return 'ws://fake/ws/v1/events'; };

describe('事件契约 §7 链投影', () => {
  it('11 步覆盖了链上的全部 22 类事件，一步不缺', () => {
    const mapped = new Set(CHAIN.flatMap((s) => s.events));
    const ORACLE = ['project_created', 'session_discovered', 'session_managed', 'task_created',
      'task_queued', 'task_preparing', 'execution_created', 'execution_started', 'task_running',
      'message_recorded', 'execution_output_flushed', 'execution_approval_requested',
      'task_waiting_approval', 'approval_requested', 'notification_created', 'approval_resolved',
      'grant_issued', 'execution_approval_resolved', 'execution_succeeded', 'task_agent_completed',
      'task_waiting_acceptance', 'task_accepted'];
    expect(ORACLE.filter((e) => !mapped.has(e))).toEqual([]);
    expect(CHAIN).toHaveLength(11);
  });
  it('步骤完成只由事件决定：没收到事件就是未完成', () => {
    data.seen.clear(); data.events.length = 0;
    expect(stepDone(CHAIN[0])).toBe(false);
    expect(stepState(CHAIN[1], 1)).toBe('pending');
    data.seen.add('project_created');
    expect(stepDone(CHAIN[0])).toBe(true);
    expect(stepState(CHAIN[1], 1)).toBe('current');
  });
});

describe('WS 游标与 resync 回落', () => {
  it('游标只前进、重叠事件按 seq 丢弃、ping 不伪造事件、resync 走 REST 并标注缺口', () => {
    link.seq = null; link.resync = false; data.seen.clear(); data.events.length = 0;
    const s = connectEvents({ url, socketFactory });
    s.feed(JSON.stringify({ type: 'hello', from_seq: 0 }));
    expect(link.state).toBe('live');

    s.feed(JSON.stringify({ type: 'event', data: ev(7, 'project_created') }));
    expect(link.seq).toBe(7);
    expect(data.events).toHaveLength(1);

    // 重连后服务端从更早的 from_seq 补发会造成重叠：必须幂等丢弃
    s.feed(JSON.stringify({ type: 'event', data: ev(7, 'project_created') }));
    expect(data.events).toHaveLength(1);

    // 故意回退的 seq 不得把游标拉回去
    s.feed(JSON.stringify({ type: 'event', data: ev(6, 'task_created') }));
    expect(link.seq).toBe(7);
    expect(data.seen.has('task_created')).toBe(false);

    s.feed(JSON.stringify({ type: 'ping', server_seq: 9 }));
    expect(data.events).toHaveLength(1);
    expect(link.state).toBe('live');

    s.feed(JSON.stringify({ type: 'resync_required' }));
    expect(link.resync).toBe(true);
    expect(link.reason).toContain('裁剪');
    expect(data.events).toHaveLength(0);
    expect(link.seq).toBe(7);
    s.close();
  });

  it('断线重连用 last seq 作为 from_seq，且每次重连都重新换票', async () => {
    urls.length = 0; made.length = 0; link.seq = null;
    const s = connectEvents({ url, socketFactory });
    await tick();
    s.feed(JSON.stringify({ type: 'event', data: ev(42, 'task_accepted') }));
    made[0].onclose?.();
    await new Promise((r) => setTimeout(r, 300));
    expect(urls).toEqual([null, 42]);
    s.close();
  });

  it('F-9：hello_ack 把游标抬到 journal 头；hello 只是回显，不动游标', async () => {
    made.length = 0; link.seq = null; data.seen.clear(); data.events.length = 0;
    const s = connectEvents({ url, socketFactory });
    await tick();
    s.feed(JSON.stringify({ type: 'hello', from_seq: 0 }));
    expect(link.seq).toBeNull();          // 带 from_seq 的那条路径：hello 是回显，不能当游标用
    s.feed(JSON.stringify({ type: 'hello_ack', server_seq: 1200 }));
    expect(link.seq).toBe(1200);          // 不带 from_seq：必须抬升，否则下次重连还落在旧位置
    expect(link.state).toBe('live');
    s.feed(JSON.stringify({ type: 'hello_ack', server_seq: 900 }));
    expect(link.seq).toBe(1200);          // 只前进不回退
    expect(data.events).toHaveLength(0);  // 控制帧不进时间线、不算事件
    s.close();
  });

  /**
   * F20-4 判据要求的读法：**字面上的第一帧**。为躲心跳而 `continue` 的容错读法会把
   * "首帧是 X"悄悄降级成"第 N 帧里出现过 X"，所以这里一次只喂一帧、且喂的就是被断言的那一帧。
   * 断的是**客户端自己的游标**（`s.seq`，即下次重连要声明的位置）而不是面板显示值 ——
   * 第一版我只断 `link.seq`，变异"把回显当游标用"当场不咬：`link.seq` 只由 `onSeq` 写，看不见内部游标被改。
   * 用例名即契约③ §5 判据表 F20-4 那一格写的 `helloAckAdvancesCursor` —— **改名必须回写那一格**。
   */
  async function afterFirstFrame(startSeq: number | null, frame: Record<string, unknown>) {
    made.length = 0; urls.length = 0; link.seq = startSeq; link.state = 'connecting';
    data.events.length = 0; data.seen.clear();
    const s = connectEvents({ url, socketFactory });
    await tick();
    s.feed(JSON.stringify(frame));
    const got = { cursor: s.seq, shown: link.seq, state: link.state, events: data.events.length };
    s.close();
    return got;
  }

  it('helloAckAdvancesCursor：首帧 hello_ack ⇒ server_seq 写进游标；首帧 hello 回显 ⇒ 不得覆盖（契约③ §5 F20-4）', async () => {
    // 形状②：hello 省略游标 → 首帧 hello_ack{server_seq}。不写进游标 = 重连后重复渲染同一段事件。
    expect(await afterFirstFrame(null, { type: 'hello_ack', server_seq: 1200 }))
      .toEqual({ cursor: 1200, shown: 1200, state: 'live', events: 0 });
    // 抬升只在"比已知位置更新"时发生；server_seq 更低时不得把游标拽回去（REQ-F-17 的客户端防御）
    expect((await afterFirstFrame(1200, { type: 'hello_ack', server_seq: 900 })).cursor).toBe(1200);

    // 形状①：hello 带游标 → 首帧是**回显**，只当就绪信号。三个方向都不许覆盖，其中"高于游标"最危险：
    // 照抄就等于承认 43..900 已看过，那段事件永不再补发 —— 面板会静缺一截。
    for (const echoed of [42, 9, 900]) {
      const r = await afterFirstFrame(42, { type: 'hello', from_seq: echoed });
      expect(r.cursor, `回显 from_seq=${echoed} 不得改写客户端游标`).toBe(42);
      expect(r.shown, `回显也不该顺手改写面板显示值`).toBe(42);
      expect(r.state, `回显仍是就绪信号，不该把连接判成未就绪`).toBe('live');
    }

    // 可观察的落点：游标写没写对，看**下一次重连声明了什么**。这是"写进自己的游标"唯一外部可证的形态。
    made.length = 0; urls.length = 0; link.seq = null;
    const s = connectEvents({ url, socketFactory });
    await tick();
    s.feed(JSON.stringify({ type: 'hello_ack', server_seq: 777 }));
    made[0].onclose?.();
    await new Promise((r) => setTimeout(r, 300));
    open(1);                                        // 假 socket 不自动 open（见 F-16 那组）
    expect(JSON.parse(made[1].sent[0])).toEqual({ type: 'hello', from_seq: 777 });
    s.close();
  });

  it('§2.1：换票失败（Core 没起来）不谎报已连接，按退避重来', async () => {
    made.length = 0; link.seq = null;
    const s = connectEvents({
      url: async () => { throw new Error('ws-ticket 401'); },
      socketFactory,
    });
    await tick();
    expect(made).toHaveLength(0);
    expect(link.state).toBe('reconnecting');
    expect(link.detail).toContain('换票失败');
    s.close();
  });
});

describe('Core 未落地时的诚实口径', () => {
  it('runChain 第一跳连不上就标 blocked，不写任何 ok', async () => {
    data.seen.clear();
    await runChain();
    expect(chain.calls.length).toBeGreaterThan(0);
    expect(chain.calls[0].outcome).toBe('blocked');
    expect(chain.calls.some((c) => c.outcome === 'ok')).toBe(false);
    expect(chain.running).toBe(false);
  });
});
