// 真 Core 联调（B-F1 解除后的取证面）。默认跳过 —— 只有给出 core.endpoint 文件路径时才跑：
//   LUCERO_CORE_ENDPOINT_FILE=<dir>/core.endpoint npx vitest run
// WS 那半边另加一把锁：LUCERO_WS_READY=1。锁没开就是 skipped，不是 passed ——
// REQ-F-17 未修前 journal 为空时服务端不出任何控制帧，跑它只会红。
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { createCoreClient, CoreError } from '@lucero/api-client/client';
import { openEventStream, type WsFrame } from '@lucero/api-client/events';

const FILE = process.env.LUCERO_CORE_ENDPOINT_FILE ?? '';
const live = !!FILE;
const wsReady = !!process.env.LUCERO_WS_READY;

// core.endpoint 实测是 JSON：{"host_port","token","version"}
const conf = live ? JSON.parse(readFileSync(FILE, 'utf8')) as
  { host_port: string; token: string; version?: string } : { host_port: '', token: '' };
const base = `http://${conf.host_port}`;
const wsBase = `ws://${conf.host_port}`;
const token = conf.token ?? '';

const core = () => createCoreClient({ baseUrl: base, token });
/** 票与口令是凭据：断言与日志里只出现掩码 */
const mask = (t: string) => `***(len ${t.length})`;

async function firstFrame(url: string, ms = 2500): Promise<WsFrame | null> {
  return new Promise((resolve) => {
    const ws = new WebSocket(url);
    let done = false;
    const finish = (v: WsFrame | null) => { if (!done) { done = true; try { ws.close(); } catch { /* noop */ } resolve(v); } };
    const t = setTimeout(() => finish(null), ms);
    ws.onopen = () => { ws.send('{"type":"hello"}'); };   // F-16：客户端先报游标
    ws.onmessage = (e) => { clearTimeout(t); finish(JSON.parse(String(e.data)) as WsFrame); };
    ws.onerror = () => { clearTimeout(t); finish(null); };
    ws.onclose = () => { clearTimeout(t); finish(null); };
  });
}

describe.skipIf(!live)('真 Core 联调 · REST 面', () => {
  it('health 200；state 在 T-30 词表内且无 DEGRADED；migration_version 是整数', async () => {
    const h = await core().health();
    expect(['BOOTING', 'READY', 'STOPPING', 'STOPPED', 'UNKNOWN']).toContain(h.state);
    expect(h.state).not.toBe('DEGRADED');
    expect(typeof h.ready).toBe('boolean');
    expect(h.migration_version === null || typeof h.migration_version === 'number').toBe(true);
    console.log(`  health: state=${h.state} ready=${h.ready} mig=${h.migration_version} ` +
      `version=${h.version} endpoint=${conf.host_port} token=${mask(token)}`);
  });

  it('无口令 → 401，且是契约③ §2 的统一错误体', async () => {
    const err = await createCoreClient({ baseUrl: base }).listProjects().then(() => null, (e) => e as CoreError);
    expect(err).toBeInstanceOf(CoreError);
    expect(err!.status).toBe(401);
    expect(err!.code).toBe('unauthorized');
  });

  it('未实现端点 → 501 not_implemented（不假装成功）', async () => {
    const err = await core().listProjects().then(() => null, (e) => e as CoreError);
    expect(err!.status).toBe(501);
    expect(err!.code).toBe('not_implemented');
    expect(err!.retryable).toBe(true);
  });

  it('§1.3：回环 dev origin 放行；非回环 origin 403 origin_not_allowed', async () => {
    expect((await fetch(`${base}/api/v1/health`, { headers: { origin: 'http://localhost:5173' } })).status).toBe(200);
    const bad = await fetch(`${base}/api/v1/health`, { headers: { origin: 'http://example.com:5173' } });
    expect(bad.status).toBe(403);
    expect((await bad.json()).error.code).toBe('origin_not_allowed');
  });

  it('§2.1 反向用例：同一请求两次 → 两张不同的有效票（幂等豁免成立）', async () => {
    const a = await core().wsTicket();
    const b = await core().wsTicket();
    expect(a.ticket).toBeTruthy();
    expect(a.ticket).not.toBe(b.ticket);
    expect(a.expires_in).toBe(30);
    console.log(`  两张票不同：${mask(a.ticket)} vs ${mask(b.ticket)}，expires_in=${a.expires_in}`);
  });
});

describe.skipIf(!live || !wsReady)('真 Core 联调 · WS 面（等 REQ-F-17）', () => {
  it('换票 → 客户端 hello → 首帧 hello_ack 且带 server_seq', async () => {
    const { ticket } = await core().wsTicket();
    const f = await firstFrame(`${wsBase}/ws/v1/events?ticket=${ticket}`);
    expect(f).not.toBeNull();
    expect(f!.type).toBe('hello_ack');
    expect(typeof (f as { server_seq?: number }).server_seq).toBe('number');
  });

  it('票单次使用：第二次拿同一张票必须被拒（close 1008，不升级）', async () => {
    const { ticket } = await core().wsTicket();
    const good = await firstFrame(`${wsBase}/ws/v1/events?ticket=${ticket}`);
    expect(good?.type).toBe('hello_ack');
    const reused = await firstFrame(`${wsBase}/ws/v1/events?ticket=${ticket}`);
    expect(reused).toBeNull();          // 握手被拒 → 拿不到任何帧
  });

  it('events.ts 实走一遍：换票建连 → 收到控制帧即 live 并抬游标', async () => {
    let state = '';
    let cursor: number | null = null;
    const s = openEventStream({
      url: async (fromSeq) => {
        const { ticket } = await core().wsTicket();
        return `${wsBase}/ws/v1/events?ticket=${ticket}` + (fromSeq === null ? '' : `&from_seq=${fromSeq}`);
      },
      fromSeq: null,
      onEvent: () => {},
      onResync: () => {},
      onState: (x) => { state = x; },
      onSeq: (n) => { cursor = n; },
    });
    await new Promise((r) => setTimeout(r, 2000));
    expect(state).toBe('live');
    expect(cursor).not.toBeNull();
    s.close();
    console.log(`  换票建连：state=${state} 游标抬到 ${cursor}`);
  });
});
