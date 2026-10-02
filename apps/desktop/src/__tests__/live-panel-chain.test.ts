/**
 * T-420：面板 store 自己那条链，必须跑在**真 Core + 真 WS** 上。
 *
 * 与 chain.test.ts 的区别就是这轮的要求本身：那里 `ws://fake/…`（:60）+ `as unknown as WebSocket`（:31）是替身，
 * 这里**不传 socketFactory、不 cast**，走 store 默认的 `ticketedUrl()` ⇒ 真换票（§2.1）+ 真 WebSocket + 真事件流。
 *
 * 通路：Core（含 fake adapter）← Vite dev 反代（`/api` 注入 Authorization、`/ws` 走 ws 代理）→ 面板代码。
 * **面板不持有口令**（契约③ §1 第 2.2 项 / REQ-F-18）：口令只在反代进程环境里，store 一行都没改。
 * 测试侧只做浏览器白送的那一步：把相对 URL 补成绝对（下面三行），不截获、不改写任何响应。
 * 独立见证人用**直连** Core（绕开反代）读同一批事实 ⇒ 面板的数字与响应不同源就红。
 *
 * 环境变量：LUCERO_PANEL_ORIGIN（反代源）、LUCERO_CORE_ENDPOINT_FILE（直连用）；缺任一 ⇒ 整文件 skip 并打印原因。
 */
import { describe, expect, beforeAll, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { createCoreClient } from '@lucero/api-client/client';
import { CHAIN, chain, connectEvents, data, detail, link, openDetail, refresh, runChain, stepDone } from '../stores/panel';
import type { DetailKind } from '../stores/panel';

const origin = process.env.LUCERO_PANEL_ORIGIN ?? '';
const endpointFile = process.env.LUCERO_CORE_ENDPOINT_FILE ?? '';
const live = origin !== '' && endpointFile !== '';
if (!live) console.log('  [skip 原因] 缺 LUCERO_PANEL_ORIGIN 或 LUCERO_CORE_ENDPOINT_FILE ⇒ 这一格没跑，不算绿');

type Row = Record<string, unknown> & { id: string };
const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

describe.skipIf(!live)('面板 store 真后台：runChain() 连真 Core + 真 WS，数字与响应同源对拍', () => {
  /** 独立见证：直连 Core，不经过面板用的那个反代 */
  let direct: ReturnType<typeof createCoreClient>;
  /** F-18 实证用的账本：面板每一发 fetch 有没有自己带 Authorization（只记布尔，凭据值一律不落这里） */
  const sent: Array<{ path: string; hasAuth: boolean }> = [];

  // 端点解析、建客户端、补 location/fetch 全在 beforeAll：skip 的套件 vitest 仍会执行 describe 体，
  // 放在这里离线闸门就会拿 ENOENT 报红（10-01 12:5x 那轮 vitest/f204 两条腿就是这么红的）。
  beforeAll(() => {
    const conf = JSON.parse(readFileSync(endpointFile, 'utf8')) as { host_port: string; token: string };
    direct = createCoreClient({
      baseUrl: `http://${conf.host_port}`,
      extraHeaders: { authorization: `Bearer ${conf.token}`, 'x-lucero-principal': 'local-user' },
    });

    // 浏览器白送的那一步：相对 → 绝对，原样转发给真 fetch（不看、不改响应）。
    // **只登记相对 URL 那一支**＝面板自己发的；独立见证人走的是绝对 URL（它按契约带 Bearer），
    // 混进来记就会把见证人的口令算成面板的（23:4x 第一版就这么假红过一次）。
    const realFetch = globalThis.fetch;
    (globalThis as { location?: unknown }).location = { origin, href: `${origin}/` };
    globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.startsWith('/')) {
        sent.push({ path: url, hasAuth: new Headers(init?.headers ?? {}).get('authorization') !== null });
        return realFetch(origin + url, init);
      }
      return realFetch(input as string | URL, init);
    }) as typeof fetch;
  });

  /** 链上真实产生的 execution id：只从真 WS 帧里取（不自己造 id） */
  function executionId(): string {
    const f = data.events.find((e) => e.event_type === 'execution_created');
    return f ? f.aggregate_id : '';
  }

  async function waitLive(ms: number) {
    const until = Date.now() + ms;
    while (Date.now() < until) {
      if (link.state === 'live') return true;
      await wait(150);
    }
    return false;
  }

  it('真换票 + 真 WebSocket：state=live、游标在抬，且 health 与直连同值', async () => {
    connectEvents();                                  // ← 一个参数都不传：走 store 默认的 ticketedUrl()
    const got = await waitLive(8_000);
    expect(got, `8s 内没等到 live（state=${link.state} detail=${link.detail}）`).toBe(true);
    expect(typeof link.seq, '游标必须是数字').toBe('number');
    const h = await direct.health();
    // 面板的 health 来自它自己的读路径（App 挂载时那次 refresh）：测试里得先让它读一次，
    // 不然比的是 "null vs READY" —— 那是我没驱动到，不是面板掺假（12:52 那次就是这么假红的）。
    await refresh();
    expect(data.health?.state, '面板 state 与直连不等').toBe(h.state);
    expect(data.health?.version, '面板 version 与直连不等').toBe(h.version);
    expect(data.health?.migration_version, '面板 mig 与直连不等').toBe(h.migration_version);
    // 这里**不断"有帧"**：一个刚起来的 Core，journal 可以真的还没事件（09-30 手工那跑点链前也是"已见事件 0"），
    // 把"没帧"当红就是拿时序当缺陷。帧的断言放在链跑完之后（test 3）。
    console.log(`  真 WS：state=${link.state} seq=${link.seq ?? 'null'} health=${h.state} mig=${h.migration_version}（帧数留到链后再断）`);
  }, 30_000);

  it('runChain() 自己走一遍：链真推进到 send，第一个不 ok 的必须点名哪一跳、给什么原因', async () => {
    await runChain();
    // runChain 的 finally 里 refresh() 是**不被 await 的**（面板自己不需要等，但判据要）：
    // 不等它，后面 test 3 就会拿"面板还没重读"的 0 行去比响应的 1 行 —— 那是时序，不是缺陷（12:47 那次就是这么红的）。
    await refresh();
    expect(chain.calls.length, '链一条都没发').toBeGreaterThan(0);
    expect(chain.running, '跑完还挂着 running').toBe(false);
    console.log('  chain.calls 全文：');
    for (const c of chain.calls) console.log(`    ${c.step.padEnd(10)} ${c.method.padEnd(58)} ${c.outcome} ${c.detail}`);
    const bad = chain.calls.find((c) => c.outcome !== 'ok');
    // 不钉别人的进度（不硬数条数）：只断「不 ok 的那条必须自己说清是哪一跳、什么结果」
    if (bad) {
      expect(['blocked', 'error'], `${bad.method} 的 outcome=${bad.outcome} 不在合法集合里`).toContain(bad.outcome);
      expect(bad.detail.length, `${bad.method} 报了 ${bad.outcome} 却不给原因`).toBeGreaterThan(0);
    }
    // 停在 approval 这一格时，"没有待审行"这句话必须**两把尺都同意**：面板说没有、直连也得说没有。
    // 只有一把尺说没有 = 面板漏读（那这一格必须红），两把尺都说没有 = 缺的是服务端产生审批的那条腿（不归我修，也不许我编归因）。
    if (bad?.step === 'approval') {
      try {
        const witness = await direct.listApprovals({ state: 'PENDING', limit: 50 });
        console.log(`  链停在 approval：面板「${bad.detail}」｜独立见证直连同一发 = ${witness.items.length} 行待审`
          + (witness.items.length === 0 ? ' ⇒ 两边一致，缺的确实是服务端那条产生审批的腿' : ' ⇒ 面板漏读！'));
        expect(witness.items.length, '面板停在"没有待审行"，但直连读到了行 ⇒ 面板漏读').toBe(0);
      } catch (err) {
        const code = (err as { code?: string }).code ?? '';
        expect(code, '面板停在 approval，而直连读审批列表给的不是 not_implemented 那种错法').toBe('not_implemented');
        console.log('  链停在 approval：独立见证直连 = 501 not_implemented ⇒ 这一格服务端还没注册（与"注册了但 0 行"要分开读）');
      }
    }
    const okSteps = chain.calls.filter((c) => c.outcome === 'ok').map((c) => c.step);
    for (const s of ['adapter', 'project', 'discover', 'managed', 'task', 'execution', 'send']) {
      expect(okSteps, `链没走到面板的 ${s} 步（ok 的只有 ${okSteps.join(',')}）`).toContain(s);
    }
    for (const t of ['project_created', 'session_discovered', 'session_managed', 'task_created', 'task_queued', 'execution_created']) {
      expect(data.seen.has(t), `真 WS 里没看到事件 ${t}`).toBe(true);
    }
  }, 120_000);

  it('面板每个数字都来自响应：条数与逐行字段同直连一致（null 不许折成 ""/false；那一格 501 时面板必须空着）', async () => {
    // 独立见证人按格读，**501 是合法结果**（quota / approvals 这类未注册的面）：那时面板必须显示"无行"，
    // 而不是凭空造几行 —— 这比"跳过错过的面"强，正是 §12 那条（UNKNOWN/未接通不许折算成有值）。
    type Face = { rows: Row[] | null; unwired: boolean };
    const read = async (label: string, fn: () => Promise<{ items?: unknown[] }>): Promise<Face> => {
      try {
        const page = await fn();
        return { rows: (page.items ?? []) as Row[], unwired: false };
      } catch (err) {
        const code = (err as { code?: string }).code ?? '';
        expect(code, `${label} 直连失败，只许 not_implemented（未注册），别种错法默默放过`).toBe('not_implemented');
        return { rows: null, unwired: true };
      }
    };
    // 链跑过之后再断"有帧"才是安全的（test 1 里不断它，是因为那时 journal 可以真的还空着）
    expect(data.events.length, '链跑完、WS 也 live 了，时间线却还是空的 ⇒ 事件根本没进 store').toBeGreaterThan(0);
    expect(link.seq ?? 0, '链跑完游标没推进').toBeGreaterThan(0);
    const srvTasks = await read('listTasks', () => direct.listTasks({ limit: 50 }));
    const srvSessions = await read('listSessions', () => direct.listSessions({ limit: 50 }));
    const srvApprovals = await read('listApprovals', () => direct.listApprovals({ state: 'PENDING', limit: 50 }));
    let quotaSrv: Record<string, unknown> | null = null;
    try {
      quotaSrv = (await direct.quota({})) as unknown as Record<string, unknown>;
    } catch (err) {
      expect((err as { code?: string }).code, 'quota 未注册只能是 not_implemented').toBe('not_implemented');
    }

    const cmp = (face: Face, panelRows: Row[], label: string) => {
      if (face.unwired) {
        expect(panelRows.length, `${label} 那格服务端是 501，面板却显示了 ${panelRows.length} 行 = 掺假`).toBe(0);
        return;
      }
      const server = face.rows ?? [];
      expect(panelRows.length, `${label} 条数与响应不等（面板 ${panelRows.length} vs 响应 ${server.length}）`).toBe(server.length);
      for (const s of server) {
        const p = panelRows.find((x) => x.id === s.id);
        if (!p) continue;                             // 面板有分页上限：缺行不算不等，有就必须同值
        for (const k of ['status', 'version', 'managed', 'state', 'sync_cursor', 'remote_revision']) {
          if (!(k in s) || s[k] === undefined) continue;
          expect((p as Record<string, unknown>)[k],
            `${label} ${s.id} 的 ${k}：面板 ${JSON.stringify((p as Record<string, unknown>)[k])} vs 响应 ${JSON.stringify(s[k])}`).toStrictEqual(s[k]);
        }
      }
    };
    cmp(srvTasks, data.tasks as Row[], 'task');
    cmp(srvSessions, data.sessions as Row[], 'session');
    cmp(srvApprovals, data.approvals as Row[], 'approval');
    const obs = (x: unknown) => (x as { observation?: Record<string, unknown> })?.observation ?? {};
    if (quotaSrv) {
      for (const k of ['used_percent', 'remaining_percent', 'used_value', 'remaining_value']) {
        expect(obs(data.quota)[k], `额度 ${k}：面板与响应不等`).toStrictEqual(obs(quotaSrv)[k]);
      }
    } else {
      expect(data.quota, 'quota 那格服务端 501，面板却拿到了对象 = 掺假').toBeNull();
    }
    console.log(`  对拍通过：tasks=${srvTasks.rows?.length ?? '501'} sessions=${srvSessions.rows?.length ?? '501'}`
      + ` approvals=${srvApprovals.rows?.length ?? '501'} quota=${quotaSrv ? '有' : '501'} 面板 seen=${data.seen.size} 帧=${data.events.length}`);
  }, 60_000);

  it('第 9 格的输出面从外面看：面板 `data.output` 的每一块都得来自 `GET /output` 的响应（store 里那条腿是 fire-and-forget，会被 .catch 吞掉）', async () => {
    // 为什么单开这一条：panel.ts:229 只等 WS 帧，帧到了就直接往下走；真正拉内容的是 panel.ts:96 的
    // `pullOutput(...).catch(() => {})` —— **失败不打声、不进 chain.calls**。所以"链过了第 9 格"在面板里是不可观察的，
    // 必须拿直连响应来比（同源对拍的同一把尺，只是这一格此前根本没有刻度）。
    const flushed = data.events.find((e) => e.event_type === 'execution_output_flushed');
    expect(flushed, '链已经走过等事件那一关，时间线里却没有 execution_output_flushed 帧').toBeDefined();
    const exId = String((flushed as unknown as { aggregate_id?: string }).aggregate_id ?? '');
    expect(exId.length, 'execution_output_flushed 帧里没有 aggregate_id').toBeGreaterThan(0);

    let srv: Awaited<ReturnType<typeof direct.executionOutput>> | null = null;
    try {
      srv = await direct.executionOutput(exId, {});
    } catch (err) {
      expect((err as { code?: string }).code, 'output 面未注册只能是 not_implemented').toBe('not_implemented');
    }
    if (!srv) {
      expect(data.output.length, `output 那格服务端 501，面板却攒了 ${data.output.length} 块 = 掺假`).toBe(0);
      console.log('  第 9 格（面板侧）：服务端 501 ⇒ 面板空着，通过');
      return;
    }
    const rows = srv.items ?? [];
    if (rows.length === 0) {
      expect(data.output.length, '服务端 0 块、面板却有块 = 掺假').toBe(0);
      console.log(`  第 9 格（面板侧）同源通过：/output → 200 且 items=0（合法）、next_cursor="${srv.next_cursor}"、面板 0 块`);
      return;
    }
    // pullOutput 是 WS 回调里发出的、没人 await：给它落地时间，但不许拿"还没到"当通过
    const until = Date.now() + 5_000;
    while (data.output.length < rows.length && Date.now() < until) await wait(200);
    expect(data.output.length,
      `服务端 /output 有 ${rows.length} 块，面板只有 ${data.output.length} 块 ⇒ 那条被 .catch 吞掉的腿没落地`).toBeGreaterThanOrEqual(rows.length);
    for (const s of rows) {
      const p = data.output.find((x) => x.sequence_no === s.sequence_no
        && (s.execution_id === undefined || x.execution_id === s.execution_id));
      expect(p, `面板缺 /output 响应里的 sequence_no=${s.sequence_no}`).toBeDefined();
      if (p) {
        expect(p.channel, `sequence_no=${s.sequence_no} 的 channel 不同源`).toBe(s.channel);
        expect(p.content, `sequence_no=${s.sequence_no} 的 content 不同源`).toBe(s.content);
      }
    }
    console.log(`  第 9 格（面板侧）同源通过：/output → 200、响应 ${rows.length} 块、面板 ${data.output.length} 块、`
      + `next_cursor="${srv.next_cursor}"、逐块 channel/content 等值`);
  }, 60_000);

  it('详情格（T-433 ⑥）：面板那一行详情 = 一次真 by-id GET，与直连响应整对象相等；读不到就留 null + 原因', async () => {
    const cases: Array<[DetailKind, string, () => Promise<unknown>]> = [
      ['task', data.tasks[0]?.id ?? '', () => direct.getTask(data.tasks[0]!.id)],
      ['session', data.sessions[0]?.id ?? '', () => direct.getSession(data.sessions[0]!.id)],
      ['execution', executionId(), () => direct.getExecution(executionId())],
    ];
    for (const [kind, id, readDirect] of cases) {
      if (!id) { console.log(`  详情格 ${kind}：没有可用的行 ⇒ 这一格没跑，不算绿`); continue; }
      const srv = await readDirect();
      const row = await openDetail(kind, id);
      expect(row, `面板 ${kind} 详情没读到东西（error=${detail.error}）`).not.toBeNull();
      expect(row, `${kind} 详情与直连 by-id 响应不是同一个对象内容`).toStrictEqual(srv);
      expect(detail.endpoint, `${kind} 详情没记下它读的是哪个端点`).toContain(`/api/v1/${kind}s/`);
      console.log(`  详情格 ${kind} 同源通过：${detail.endpoint} · ${Object.keys(row ?? {}).length} 个键整对象相等`);
    }
    // 读不到那一支：不存在的 id ⇒ row 必须留 null、原因必须非空（§12：不许拿空对象或 0 冒充有值）
    const gone = await openDetail('approval', '01h00000-0000-7000-8000-000000000000');
    expect(gone, '不存在的审批 id 竟然读出了行 = 掺假').toBeNull();
    expect(detail.error.length, '读失败却没留下任何原因').toBeGreaterThan(0);
    console.log(`  详情格读不到那一支：error=「${detail.error}」、row=null`);
  }, 90_000);

  it('F-18 实证（T-433 ⑤）：面板每一发 REST 都不自带 Authorization，口令只在反代进程里；换票那一发也确实发过', () => {
    expect(sent.length, '一条请求都没记录到 ⇒ 这一格没跑，不算绿').toBeGreaterThan(0);
    const withAuth = sent.filter((s) => s.hasAuth).map((s) => s.path);
    expect(withAuth, `面板自己发出了 Authorization：${withAuth.join(' ,')}`).toHaveLength(0);
    const ticket = sent.filter((s) => s.path.includes('ws-ticket')).length;
    expect(ticket, '没走过 §2.1 换票 ⇒ WS 那条不能算真链路').toBeGreaterThan(0);
    console.log(`  F-18：面板共发 ${sent.length} 发（含 ws-ticket ${ticket} 发），自带 Authorization 的 = 0；`
      + `链路状态=${link.state}（口令值不在本文件、不在产物、只在反代进程环境）`);
  });

  it('store 自身不掺假：seen 与时间线是同一批真帧（不多不少），点亮格只看事件', () => {
    const fromFrames: Set<string> = new Set(data.events.map((e) => e.event_type));
    for (const t of data.seen) expect(fromFrames.has(t), `seen 里有时间线之外的类型：${t}`).toBe(true);
    for (const t of fromFrames) expect(data.seen.has(t), `时间线里有、seen 却没记：${t}`).toBe(true);
    const lit = CHAIN.filter((s) => stepDone(s)).map((s) => s.key);
    console.log(`  点亮格（只看事件）= ${lit.join(',') || '（无）'}`);
    console.log(`  已见类型（升序）= ${[...data.seen].sort().join(',')}`);
    expect(data.seen.size, '一个事件都没看到 = 这条 WS 其实是假的').toBeGreaterThan(0);
  });
});
