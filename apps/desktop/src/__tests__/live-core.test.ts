// 真 Core 联调（B-F1 解除后的取证面）。默认跳过 —— 只有给出 core.endpoint 文件路径时才跑：
//   LUCERO_CORE_ENDPOINT_FILE=<dir>/core.endpoint npx vitest run
// WS 那半边另加一把锁：LUCERO_WS_READY=1。锁没开就是 skipped，不是 passed ——
// REQ-F-17 未修前 journal 为空时服务端不出任何控制帧，跑它只会红。
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { createCoreClient, CoreError } from '@lucero/api-client/client';
import { openEventStream, type EventEntry, type WsFrame } from '@lucero/api-client/events';
import { CHAIN, data, stepDone } from '../stores/panel';

const FILE = process.env.LUCERO_CORE_ENDPOINT_FILE ?? '';
const live = !!FILE;
const wsReady = !!process.env.LUCERO_WS_READY;

// core.endpoint 实测是 JSON：{"host_port","token","version"}
const conf = live ? JSON.parse(readFileSync(FILE, 'utf8')) as
  { host_port: string; token: string; version?: string } : { host_port: '', token: '' };
const base = `http://${conf.host_port}`;
const wsBase = `ws://${conf.host_port}`;
const token = conf.token ?? '';

/** 口令只在这个测试里出现：node 侧直连 Core，不经反代。测试文件不进构建产物。 */
const core = () => createCoreClient({ baseUrl: base, extraHeaders: { authorization: `Bearer ${token}` } });
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

/** 收多帧；helloBody 决定客户端按 F-16 报哪个游标。firstFrame 只收首帧、且固定发裸 hello */
async function collectFrames(helloBody: Record<string, unknown> | null, ms = 900): Promise<WsFrame[]> {
  const { ticket } = await core().wsTicket();
  return new Promise((resolve) => {
    const got: WsFrame[] = [];
    const ws = new WebSocket(`${wsBase}/ws/v1/events?ticket=${ticket}`);
    const finish = () => { try { ws.close(); } catch { /* 已关 */ } resolve(got); };
    const timer = setTimeout(finish, ms);
    ws.onopen = () => { if (helloBody) ws.send(JSON.stringify(helloBody)); };
    ws.onmessage = (e) => { got.push(JSON.parse(String(e.data)) as WsFrame); };
    ws.onclose = () => { clearTimeout(timer); finish(); };
    ws.onerror = () => { clearTimeout(timer); finish(); };
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

  it('未接线端点只能回 501 not_implemented（不许假成功，也不许钉成"永远 501"）', async () => {
    // 09-23 03:26 之前我用的是 `listProjects`，那天它脱了 501 ⇒ **我把别人的进度钉成了我的红**。
    // 现在换成仍 501 的 /models，并且写成漂移安全形：接上了就只核形状，没接上就必须是 501。
    const res = await fetch(`${base}/api/v1/models`, { headers: { authorization: `Bearer ${token}` } });
    if (res.status === 200) {
      const page = await res.json();
      expect(Array.isArray(page.items), '接上了就得是 §2 的分页形状').toBe(true);
      return;
    }
    expect(res.status, '未接线只许 501，不许 405/404/假 200').toBe(501);
    const err = (await res.json()).error;
    expect(err.code).toBe('not_implemented');
    expect(err.retryable).toBe(true);
  });

  it('§1.3：回环 dev origin 放行；非回环 origin 403 origin_not_allowed', async () => {
    expect((await fetch(`${base}/api/v1/health`, { headers: { origin: 'http://localhost:5173' } })).status).toBe(200);
    const bad = await fetch(`${base}/api/v1/health`, { headers: { origin: 'http://example.com:5173' } });
    expect(bad.status).toBe(403);
    expect((await bad.json()).error.code).toBe('origin_not_allowed');
  });

  it('T-80/REQ-F-19：分页出口的 next_cursor 只有"空串"一种空表示', async () => {
    // 我生成类型已收紧成 string（非 null），这条负责在真响应上验它，别让我写了不查。
    // 只用已实现的列表端点：/projects 与 /sessions 在 09-21 仍 501，拿它们写断言会假红。
    const page = await core().listTasks({ limit: 5 });
    expect(Array.isArray(page.items)).toBe(true);
    expect(typeof page.next_cursor).toBe('string');
    expect(page.next_cursor).toBe('');          // 未取满一页 → 空串，不是 null、也不是省略
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

describe.skipIf(!live || !wsReady)('真 Core 联调 · WS 面', () => {
  it('换票 → 客户端 hello → 首帧 hello_ack 且带 server_seq', async () => {
    const { ticket } = await core().wsTicket();
    const f = await firstFrame(`${wsBase}/ws/v1/events?ticket=${ticket}`);
    expect(f).not.toBeNull();
    expect(f!.type).toBe('hello_ack');
    expect(typeof (f as { server_seq?: number }).server_seq).toBe('number');
  });

  it('F-9：不带游标→hello_ack 抬到 journal 头；hello 带游标→回显 hello 且只补发其后事件', async () => {
    const bare = await collectFrames({ type: 'hello' });
    expect(bare[0]?.type).toBe('hello_ack');
    expect(typeof (bare[0] as { server_seq?: number }).server_seq).toBe('number');

    await core().createTask({ title: 'live f9 a' });
    await core().createTask({ title: 'live f9 b' });
    const withCursor = await collectFrames({ type: 'hello', from_seq: 1 });
    expect(withCursor[0]?.type).toBe('hello');
    expect((withCursor[0] as { from_seq?: number }).from_seq).toBe(1);
    const replayed = withCursor.filter((f) => f.type === 'event')
      .map((f) => (f as { data?: { seq?: number } }).data!.seq!);
    expect(replayed.length).toBeGreaterThan(0);
    expect(replayed.every((seq) => seq > 1)).toBe(true);   // 禁止「缺省即全量重放」

    const past = await collectFrames({ type: 'hello', from_seq: 999999 });
    expect(past.filter((f) => f.type === 'event')).toHaveLength(0);
  }, 20_000);
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

/* ── 只读面：T-189 实测之后改成的"漂移安全"断言 ─────────────────────────────
   03:26 现盘实测（我自己的二进制 core-t189.exe，03:24:45 构建）：
   `GET /sessions`、`GET /projects`、`GET /projects/{id}`、`GET /notifications`、`POST /sessions/{id}:sync`
   都已经脱了 501；**09-25 18:05:30 复量（T-282）**：`GET /executions?task_id=`、`GET /sessions/{id}/messages` 也已脱 501，
   仍 501 的是 `GET /executions/{id}`（"还没装配读通道"）与 `/executions/{id}/output`；`adapters` 面 / `models` / `approvals`
   / `quota` / `search` 在 mux 注册表里根本没有那几条路径（catch-all）。
   ⇒ 断言不能写"它现在必须 501"（那是把别人的进度钉成我的红），也不能写"它现在必须 200"
   （别人回退我就瞎）。统一形状：**200 就必须符合契约形状；不是 200 就只能是 `not_implemented`**。 */
describe.skipIf(!live)('真 Core 联调 · 只读面与 C28② 口径（T-189）', () => {
  // 期限是承重的：对端挂住时 fetch 会**永远**挂着，既吃掉整条用例的预算，又把 Core 的单写连接一并拖住，
  // 于是"一格挂"在计数里长成"六格红"。给每一发一个 2.5s 的闸，让挂变成一条**说得清是哪一格**的失败。
  const raw = (method: string, path: string, idem?: string, body?: unknown, ifMatch?: number) =>
    fetch(`${base}${path}`, {
      method,
      signal: AbortSignal.timeout(2500),
      headers: { authorization: `Bearer ${token}`, 'x-lucero-principal': 'local-user',
        ...(body !== undefined ? { 'content-type': 'application/json' } : {}),
        ...(idem ? { 'idempotency-key': idem } : {}),
        ...(ifMatch !== undefined ? { 'if-match': String(ifMatch) } : {}) },
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    });

  it('分页出口三条：200 时 next_cursor 必须是字符串（空串=唯一合法"没有下一页"），未接线只许 501', async () => {
    for (const path of ['/api/v1/sessions', '/api/v1/projects', '/api/v1/notifications']) {
      const res = await raw('GET', path);
      if (res.status === 200) {
        const page = await res.json();
        expect(Array.isArray(page.items), path).toBe(true);
        // REQ-F-19：这一侧只许 ""，null / 缺键都是非法的第三种"没有"
        expect(typeof page.next_cursor, `${path} 的 next_cursor 类型`).toBe('string');
      } else {
        expect(res.status, `${path} 未接线只能是 501`).toBe(501);
        expect((await res.json()).error.code).toBe('not_implemented');
      }
    }
  });

  it('C28② 线上口径：yaml 声明过的操作，未接线只能回 501，不许回 405 说"这条路径不接受这个方法"', async () => {
    // `GET /api/v1/tasks/{id}` 在契约③ F-8 批准的三个详情读里、yaml 里有定义；
    // 现盘 tasks.go 的分流是"非 POST 一律 405"，那句话是假话 —— 这条断言今天会红，红了就是证据。
    const res = await raw('GET', '/api/v1/tasks/01TESTNOTEXIST');
    const body = await res.json().catch(() => ({} as { error?: { code?: string } }));
    expect(body?.error?.code, `状态码 ${res.status}`).not.toBe('method_not_allowed');
    expect([200, 404, 501]).toContain(res.status);
  });

  it('§4 第 3/4 格（manage / sync）：501 与 404 是两种不同的红，405 一律不许', async () => {
    // 上一版这条的 else 分支写的是 `expect([200,201,404]).not.toBe(405)` —— 拿**数组字面量**去比 405，
    // 无论对方回什么都永远通过，是条空断言。这里改成先收口状态集、再按状态核各自的形状。
    // 口径：未接线 = 501 not_implemented（装配漏件）；路由通了但行不存在 = 404 session_not_found（数据没到）；
    // 真接通 = 200/201 且必须给得出契约形状。405/500/404-with-其它码 都算红。
    for (const [label, path, idem, body] of [
      // 04:5x 教训：discover 原来也在这个循环里，但它按 `adapter_key` 寻址是错的 —— 见下面单独那条两行期望。
      ['manage', '/api/v1/sessions/01TESTNOTEXIST:manage', 'live-manage', { managed: true }],
      ['sync', '/api/v1/sessions/01TESTNOTEXIST:sync', 'live-sync', undefined],
    ] as const) {
      const res = await raw('POST', path, idem, body);
      const err = await res.json().then((j) => (j as { error?: unknown }).error ?? null) as
        { code?: string; message?: string; retryable?: boolean } | null;
      expect([200, 201, 404, 501],
        `${label} 回了 ${res.status}：只许这四种（501=没装配 / 404=缺行 / 200-201=真通），不许 405 与假成功`)
        .toContain(res.status);
      if (res.status === 501) {
        expect(err?.code, label).toBe('not_implemented');
        expect(err?.retryable, `${label} 的 501 应可重试（等装配）`).toBe(true);
      } else if (res.status === 404) {
        expect(['code', 'message', 'retryable'].every((k) => k in (err ?? {})),
          `${label} 的错误体三键不齐：${JSON.stringify(err)}`).toBe(true);
        expect(err?.code, label).toBe('session_not_found');
        expect(typeof err?.retryable, `${label} retryable 必须是布尔`).toBe('boolean');
      }
      console.log(`  §4 ${label} → ${res.status} ${err?.code ?? '<no error>'}`);
    }

    // 400 and 404 are two different reds: invalid body = 400 validation_failed, not 404 [row missing] nor 501 [not wired].
    const noBody = await raw('POST', '/api/v1/sessions/01TESTNOTEXIST:manage', 'live-manage-empty');
    expect(noBody.status, '缺 required 的 managed ⇒ 应当是请求不合法').toBe(400);
    expect((await noBody.json()).error.code).toBe('validation_failed');
  });

  /**
   * F-8 批准的那条单任务详情读（契约③ :123-125）：`:accept` 之前必须靠它取**最新 version** 当 If-Match。
   * 这条单独成一个用例、且排在链式用例**之前**，理由是 09-25 的实测教训：链上一走到第 8 格 `send` 就把单写连接按住，
   * 后面的写与这条读都再也没机会被观察到 ⇒ 把"读面在不在、形状对不对"放在不会被打断的地方量，链里那一发只管"用它当 If-Match"。
   */
  it('F-8 单任务详情读：CREATED 任务 → GET /tasks/{id} 给得出 yaml 九键与可当 If-Match 的 version', async () => {
    const proj = await raw('POST', '/api/v1/projects', 'f8-proj',
      { name: `f8-${Date.now()}`, canonical_path: `D:/Project/Lucero/.scratch/frontend/f8-${Date.now()}` });
    expect(proj.status, await proj.clone().text()).toBe(201);
    const projectId = (await proj.json() as { id: string }).id;
    // ③（REQ-LK-1）：不带 session_id 是合法状态，所以这条用例不依赖会话，读面本身才是要看的
    const task = await raw('POST', '/api/v1/tasks', 'f8-task',
      { title: 'f8 read face', project_id: projectId, mode: 'MANAGED', priority: 'NORMAL' });
    expect(task.status, await task.clone().text()).toBe(201);
    const t = await task.json() as { id: string; version?: number };

    const det = await raw('GET', `/api/v1/tasks/${t.id}`);
    expect([200, 501], `GET /tasks/{id} 回了 ${det.status}：只许"已接(200)"或"未装配(501)"`).toContain(det.status);
    if (det.status === 501) {
      const e = (await det.json()).error;
      expect(e.code, '未接线只能是 not_implemented').toBe('not_implemented');
      console.log(`  F-8 读面仍 501（未装配）：「${e.message}」⇒ 链里那一发就不带 If-Match`);
      return;
    }
    const tk = await det.json() as Record<string, unknown>;
    // yaml `Task.required` 九键（core-v1.yaml:1072）逐个核 —— 少一个键就是给不出契约形状
    for (const k of ['id', 'title', 'status', 'priority', 'risk_level', 'mode', 'version', 'created_at', 'updated_at']) {
      expect(k in tk, `Task 详情缺 yaml required 的键 ${k}：${Object.keys(tk)}`).toBe(true);
    }
    expect(typeof tk.version, 'version 必须是数字（If-Match 的输入）').toBe('number');
    expect(Number.isInteger(tk.created_at), 'created_at 是 Unix 毫秒整数').toBe(true);
    expect(tk.id, '详情要指回刚建的那条').toBe(t.id);
    console.log(`  F-8 读面 ok：GET /tasks/{id} → 200，九键齐，status=${tk.status} version=${tk.version}`);
  });

  /**
   * §4 第 5 格起到第 11 格的逐跳真跑。
   * 历史：09-24 07:2x 只有 5~6 两格通；09-24 10:10 第 7 格从"读面从未注册"变成"200 空清单"；
   * **09-25 18:05:30（T-282，自属构建 18:05:0x）分派腿落地 ⇒ 第 7 格真给得出 execution 行**，
   * 于是卡点从"读面"移到了第 8 格（send 的 task↔session link）与第 9/10/11 格（output / approvals / accept 的矩阵）。
   * 这里刻意不写"必须 501"（那是把别人的进度钉成我的红，我 04:5x 才犯过），改成每支都各自有断言：
   * 2xx 必须给得出契约形状，501 必须是 `not_implemented`，404/409 必须是那一格该出的码，
   * 且**每一支都 console.log 出状态码与 message** —— 免得"提前 return"让"验到了"和"什么都没验"在计数里长得一样。
   */
  it('§4 第 5~11 格逐跳真跑：能断的断契约形状，断不到的记 blocked 带原因（不许钉 501，也不许假绿）', async (ctx) => {
    // 一个来源：probe 的寻址键与 :queue 请求体声明的键必须是同一个字面量，别写两遍各自漂。
    const akey = 'fake';
    const probe = await raw('POST', `/api/v1/adapters/${akey}/probe`, 't56-probe');
    if (probe.status === 501) ctx.skip();          // harness 少 -adapter fake= ⇒ 不是我该红的东西
    expect(probe.status).toBe(200);
    const instId = (await probe.json() as { id: string }).id;
    const disc = await raw('POST', `/api/v1/adapters/${instId}/sessions:discover`, 't56-disc');
    expect(disc.status).toBe(200);
    const sid = ((await disc.json() as { items: { session_id: string }[] }).items ?? [])[0]?.session_id;
    if (!sid) ctx.skip();                           // 空清单是合法结果，但后面没得串，skip 而不是假装过

    const proj = await raw('POST', '/api/v1/projects', 't56-proj',
      { name: `t56-${Date.now()}`, canonical_path: `D:/Project/Lucero/.scratch/frontend/t56-${Date.now()}` });
    expect(proj.status).toBe(201);
    const projectId = (await proj.json() as { id: string }).id;

    const task = await raw('POST', '/api/v1/tasks', 't56-task',
      { title: 't56 slice', project_id: projectId, session_id: sid, mode: 'MANAGED', priority: 'NORMAL' });
    expect(task.status, await task.clone().text()).toBe(201);
    const t = await task.json() as { id: string; status?: string; version?: number };
    expect(typeof t.id).toBe('string');

    const q = await raw('POST', `/api/v1/tasks/${t.id}:queue`, 't56-queue', { adapter_key: akey });
    expect([200, 409], `queue 回了 ${q.status}`).toContain(q.status);
    if (q.status === 409) expect((await q.json()).error.code).toBe('illegal_transition');

    // C43（09-24）：`:queue` 的调度对象是**调用方声明的事实** ⇒ 请求体 `{"adapter_key": …}` 必填、缺 ⇒ 400。
    // 这一条打在**另一条 CREATED 任务**上：若复用上面那条，未收紧时它会先把任务排队掉，
    // golden path 只剩一个 409 —— 断言照样"过"，但它验的东西已经变了（同一条命令两用是假绿的一种）。
    const task2 = await raw('POST', '/api/v1/tasks', 't56-task2',
      { title: 't56 nokey leg', project_id: projectId, session_id: sid, mode: 'MANAGED', priority: 'NORMAL' });
    expect(task2.status, await task2.clone().text()).toBe(201);
    const t2 = await task2.json() as { id: string };
    const qNoKey = await raw('POST', `/api/v1/tasks/${t2.id}:queue`, 't56-queue-nokey');
    if (qNoKey.status === 400) {
      expect((await qNoKey.json()).error.code, 'C43 已收紧：缺 key 只能是 validation_failed').toBe('validation_failed');
    } else {
      // 两支都有断言，且不钉别人的进度：未收紧时这里合法地放行，但**必须喊出来**，不许静等价。
      expect([200, 409, 404], `queue 缺 key 回了 ${qNoKey.status}`).toContain(qNoKey.status);
      console.log(`  C43 未收紧：不带 adapter_key 的 :queue 得到 ${qNoKey.status}（欠工在 backend 那侧，不记我这侧的红）`);
    }
    console.log(`  :queue 带 key → ${q.status}，缺 key → ${qNoKey.status}`);

    // REQ-LK-1（09-25 裁甲）的两条边界要**分开**量：② 给了 `session_id` 但库里没有那个会话 ⇒ 404 `session_not_found`；
    // ③ 缺省或 null ⇒ 不写 link，201 完全合法 —— 所以这里不给"缺字段"编一条 400 断言（那是替契约发明要求）。
    const taskBogus = await raw('POST', '/api/v1/tasks', 't56-task-bogus-session',
      { title: 't56 lk-1', project_id: projectId, session_id: '01TESTNOTEXIST', mode: 'MANAGED', priority: 'NORMAL' });
    if (taskBogus.status === 404) {
      expect((await taskBogus.json()).error.code, 'REQ-LK-1 ② 落地后只能报会话不存在').toBe('session_not_found');
    } else {
      expect(taskBogus.status, `假 session_id 回了 ${taskBogus.status}：只许 201（未落地）或 404（已落地）`).toBe(201);
      console.log('  REQ-LK-1 未落地：`session_id` 指向不存在的会话仍然 201 ⇒ 键还在校验之前被丢，欠工在 backend T-286');
    }
    console.log(`  POST /tasks + 假 session_id → ${taskBogus.status}`);

    const ex = await raw('GET', `/api/v1/executions?task_id=${t.id}`);
    if (ex.status === 501) {
      expect((await ex.json()).error.code).toBe('not_implemented');
      console.log(`  第 7 格 blocked：GET /executions?task_id=${t.id} → 501（读面被摘掉了，09-25 18:05 它是 200）`);
      return;                                       // 到这里为止每一跳都真断言过，不是空跑
    }
    expect(ex.status, await ex.clone().text()).toBe(200);
    type ExecRow = Partial<Record<'id' | 'task_id' | 'status' | 'attempt_no' | 'trigger_type' | 'state_confidence' | 'version', unknown>>;
    const page = await ex.json() as { items: ExecRow[]; next_cursor: string };
    expect(Array.isArray(page.items)).toBe(true);
    expect(typeof page.next_cursor).toBe('string'); // REQ-F-19：分页侧只许字符串，空串=没有下一页
    // 契约③ §4「Create Execution」：`:queue` 200 ⇒ 同一笔事务里就该有 execution 行。
    // 只在 q 真 200 的那一支才追加这条要求（409=重复排队，本来就不该新增行）。
    if (q.status === 200) expect(page.items.length, '入队 200 却拿不到 execution 行').toBeGreaterThan(0);
    const row = page.items[0];
    if (!row) {
      console.log('  入队后还没有 execution 行：send / output 记 blocked（0 条是合法结果，不猜 id）');
      return;
    }
    // 必填七键逐个核（yaml `Execution.required`）—— 少一个键就是对方给不出契约形状，不是我的红可以糊过去
    for (const k of ['id', 'task_id', 'status', 'attempt_no', 'trigger_type', 'state_confidence', 'version'] as const) {
      expect(k in row, `execution 行缺必填键 ${k}：${JSON.stringify(row)}`).toBe(true);
    }
    expect(row.task_id, 'execution 要挂回刚才那条任务').toBe(t.id);
    const exId = row.id as string;
    console.log(`  第 7 格 ok：GET /executions?task_id= → 200，items=${page.items.length}，`
      + `status=${row.status} attempt_no=${row.attempt_no} trigger=${row.trigger_type} 七键齐`);

    const detail = await raw('GET', `/api/v1/executions/${exId}`);
    if (detail.status === 200) {
      const d = await detail.json() as Record<string, unknown>;
      for (const k of ['model_snapshot', 'permission_snapshot', 'environment_snapshot']) {
        expect(k in d, `§3 :141 要的三份快照缺 ${k}（现有键 ${Object.keys(d)}）`).toBe(true);
      }
      // 成功那一支也要留打印（这已经是我第三次补同一种洞：只让 blocked 说话，绿就无声）
      console.log('  7b ok：GET /executions/{id} → 200，三份快照齐（model/permission/environment）');
    } else {
      expect([501], `GET /executions/{id} 回了 ${detail.status}`).toContain(detail.status);
      const e = (await detail.json()).error;
      expect(e.code, '单条详情未接线只能是 not_implemented').toBe('not_implemented');
      console.log(`  execution 详情 blocked：501 ${e.code}「${e.message}」`);
    }

    // 09-25 三次独立复现（两次 vitest + 一发直连探针 `probe-t286lk.txt` 第 14~16 行）：
    //   ① 这一格 2.5s / 4s 期限内无响应；② 紧接着 `POST /projects` 也无响应；③ 同时 `GET /health` 仍 200
    //   ⇒ 被 hold 的是**写路径**（单写连接），不是监听。挂住时 fetch 抛 AbortError，这里接住并把"是谁拖死了后面"打出来。
    //   我不写"adapter 退了"这类归因：那段时间的 core 日志只有 3 条 `kind=STARTED`、没有任何退出事件，
    //   而 `exit=0` 是 STARTED 事件上没赋值的零值（`supervisor.go:86/:138` 只在退出路径填它）——拿它当"进程退了"是我读错。
    let send: Response;
    try {
      send = await raw('POST', `/api/v1/executions/${exId}/send`, 't56-send', { text: 't56', delivery: 'start_now' });
    } catch (e) {
      console.log('  第 8 格 send 无响应（2.5s 期限到：' + (e as Error).name + '）⇒ 单写连接被这条腿持有，'
        + '本文件后续用例的超时是**同一因**，不要按 N 个缺陷读。 measured 事实只有三条：'
        + 'send 不回应 / 之后 POST /projects 也不回应 / GET /health 仍 200；欠期限与持连接的位置归 backend 读代码');
      throw new Error('§4 第 8 格 send 无响应：Provider 腿挂住（见上一行日志与 probe-t286lk.txt）');
    }
    expect([200, 202, 404, 409, 503], `send 回了 ${send.status}`).toContain(send.status);
    if (send.status === 200 || send.status === 202) {
      const s = await send.json() as { delivery?: string; accepted?: boolean; message_id?: string | null };
      expect(typeof s.accepted, 'SendResult.accepted 必须是布尔').toBe('boolean');
      expect(typeof s.delivery, 'SendResult.delivery 必须是字符串').toBe('string');
      // 成功那一支也要留打印：不然"验到了"和"没走到这一支"在计数里长得一样（我自己登记过的老坑，09-27 又差点犯）
      console.log(`  第 8 格 ok：send → ${send.status}，delivery=${s.delivery} accepted=${s.accepted} message_id=${JSON.stringify(s.message_id ?? null)}`
        + `（REQ-S-1 修复的判据就是这一支不再 hold 写面）`);
    } else {
      const e = (await send.json()).error;
      expect(e?.code, `send 回了 ${send.status} 而不是契约形状`).toBeTruthy();
      // 09-25 这一支曾是 **404 session_not_found**，根因不在 send 腿：`createTaskBody` 不收 `session_id` +
      // `task_session_links` 全仓 0 处 INSERT ⇒ execution 的 session_id 恒空。REQ-LK-1（甲）+ T-286 已把它修掉，
      // 09-27 13:14 实测走的是上面那一支；这条 else 留着是给"link 又断了"这种情况说话的，不是历史装饰。
      console.log(`  第 8 格 blocked：send → ${send.status} ${e.code}「${e.message}」（task↔session link 断了？）`);
    }

    const out = await raw('GET', `/api/v1/executions/${exId}/output`);
    expect([200, 501], `output 回了 ${out.status}`).toContain(out.status);
    if (out.status === 501) {
      const e = (await out.json()).error;
      expect(e.code).toBe('not_implemented');
      console.log(`  第 9 格 blocked：output → 501 ${e.code}「${e.message}」`);
    } else {
      const o = await out.json() as { items: unknown[]; next_cursor: string };
      expect(Array.isArray(o.items), 'output 200 必须是分页形状').toBe(true);
      expect(typeof o.next_cursor).toBe('string');
    }

    const approvals = await raw('GET', '/api/v1/approvals?state=PENDING');
    expect([200, 501], `approvals 列表回了 ${approvals.status}`).toContain(approvals.status);
    const apErr = approvals.status === 501 ? (await approvals.json()).error : null;
    if (apErr) expect(apErr.code, 'approvals 未接线只能是 not_implemented').toBe('not_implemented');
    console.log(`  第 10 格 ${approvals.status === 501 ? 'blocked' : 'ok'}：GET /approvals → ${approvals.status}${apErr ? ` ${apErr.code}` : ''}`);

    // F-8 那条读的**形状**由上面那条独立用例负责（九键齐）；这里只管一件事实：**拿它当 If-Match**（契约③ :124）。
    // 形状断言不在这里重做一遍，免得两处判据各自漂。
    const det = await raw('GET', `/api/v1/tasks/${t.id}`);
    expect([200, 404, 501], `GET /tasks/{id} 回了 ${det.status}`).toContain(det.status);
    let latestVersion: number | undefined;
    if (det.status === 200) {
      const tk = await det.json() as { version?: unknown };
      expect(typeof tk.version, 'If-Match 的输入必须是数字版本').toBe('number');
      latestVersion = tk.version as number;
      console.log(`  链里用上 F-8 读面：version=${latestVersion} 当 If-Match 打 :accept`);
    } else {
      console.log(`  F-8 读面这格是 ${det.status} ⇒ accept 不带 If-Match（别把"没读面"混成"版本冲突"）`);
    }

    const acc = await raw('POST', `/api/v1/tasks/${t.id}:accept`, 't56-accept', {}, latestVersion);
    expect([200, 409], `accept 回了 ${acc.status}`).toContain(acc.status);
    if (acc.status === 409) {
      const e = (await acc.json()).error;
      expect(e.code, '链上没跑到 AGENT_COMPLETED 之前，验收只能被 §5 矩阵挡住').toBe('illegal_transition');
      console.log(`  第 11 格 blocked：accept → 409 ${e.code}「${e.message}」`);
    } else {
      const a = await acc.json() as { status?: string; version?: number };
      console.log(`  第 11 格 ok：accept → ${acc.status}，status=${a.status} version=${a.version}（If-Match 用的是 F-8 读来的版本）`);
    }
  });

  /**
   * §4 第 2/3/4 格的真链（T-222 起 discover 已通，样本不再用"期望 501"挡着）。
   * 合法顺序照契约③ §3：`probe(key)` → 回执 `adapter.id`（UUIDv7）→ `discover(id)` → `items[0].session_id` → `:manage` / `:sync`。
   * 两个坑一起钉住：
   *   - 寻址：discover 走 `parameters/Id`，**拿 adapter_key 打必须是 404 `not_found`**（09-24 07:10 实测正是 404，
   *     所以这一行从今天起是"会说话的守卫"，不再是空期望）；
   *   - 装配：Core 必须以 `-adapter fake=<路径>` 起，没配就 501（`main.go:91` 的 flag 说明自己写了）——
   *     那是**我的 harness 缺件**，所以这里 skip 并报出原因，不记成对方的红。
   */
  it('§4 第 2~4 格真链：probe 取实例 id → discover 去重 upsert → manage 通 → sync 允许 200/503 两种真相', async (ctx) => {
    const probe = await raw('POST', '/api/v1/adapters/fake/probe', 'live-t222-probe');
    if (probe.status === 501) {
      ctx.skip();   // 没配 -adapter fake= ⇒ 这一格无从跑起，skip 而不是红，也不是绿
    }
    expect(probe.status, await probe.clone().text()).toBe(200);
    const inst = await probe.json() as { id?: string; health?: string };
    expect(typeof inst.id).toBe('string');

    const disc = (idem: string) => raw('POST', `/api/v1/adapters/${inst.id}/sessions:discover`, idem);
    const a = await disc('live-t222-a');
    expect(a.status, await a.clone().text()).toBe(200);
    const items = (await a.json() as { items: { session_id: string; remote_session_id: string }[] }).items;
    expect(Array.isArray(items)).toBe(true);
    if (!items.length) {
      // 清单空是合法结果（Provider 没历史），但那样第 3/4 格就没法串，如实记下而不是猜一个 id
      console.log('  discover 返回空清单：第 3/4 格记 blocked（无 id 可用）');
      return;
    }
    // 实例作用域去重：换一把新的 Idempotency-Key 再拉一次，必须回到同一批本地行，不产生新行
    const b = await disc(`live-t222-b-${Date.now()}`);
    expect(b.status).toBe(200);
    const again = (await b.json() as { items: { session_id: string }[] }).items.map((x) => x.session_id).sort();
    expect(again).toEqual(items.map((x) => x.session_id).sort());

    // 错路守卫：key 打 id 位
    const wrong = await raw('POST', '/api/v1/adapters/fake/sessions:discover', 'live-t222-key');
    expect(wrong.status, '按 adapter_key 寻址不得被悄悄接受').toBe(404);
    expect((await wrong.json()).error.code).toBe('not_found');

    const sid = items[0].session_id;
    // 这行日志是给自己看的：空清单会提前 return，"通过"和"什么也没验"在计数里长得一样，必须留下走的是哪条分支。
    console.log(`  真链走通到第 4 格：实例 ${inst.id} 拿到 ${items.length} 条会话，用 ${sid} 串 manage/sync`);
    const mg = await raw('POST', `/api/v1/sessions/${sid}:manage`, 'live-t222-manage', { managed: true });
    expect(mg.status, await mg.clone().text()).toBe(200);
    expect((await mg.json()).managed).toBe(true);

    // `GET /sessions/{id}/messages` 的读通道 09-25 已装配（`main.go:338` 注入 `SessionMessages`）⇒ 这格从 501 变真跑。
    // 空清单合法（send 那格还没通，本来就该没有消息），但**分页形状必须齐**：items 是数组、next_cursor 是字符串。
    const msg = await raw('GET', `/api/v1/sessions/${sid}/messages`);
    expect([200, 501], `messages 回了 ${msg.status}`).toContain(msg.status);
    if (msg.status === 200) {
      const p = await msg.json() as { items?: unknown[]; next_cursor?: unknown };
      expect(Array.isArray(p.items), 'messages 200 但 items 不是数组').toBe(true);
      expect(typeof p.next_cursor, 'REQ-F-19：没有下一页只有空串一种写法').toBe('string');
      console.log(`  messages → 200（${p.items?.length} 条，next_cursor="${p.next_cursor}"）`);
    } else {
      console.log(`  messages 仍 501：${JSON.stringify(await msg.json()).slice(0, 140)}`);
    }

    const sy = await raw('POST', `/api/v1/sessions/${sid}:sync`, 'live-t222-sync');
    // 09-24 07:10 实测是 503 `adapter_unresponsive`（C25 那一支：无可用 Provider 也不许假"已同步"）。
    // 两种都是真相：200 必须给得出同步游标；503 必须 retryable=true。501/404/405 才说明这格又漂了。
    expect([200, 503], `sync 回了 ${sy.status}`).toContain(sy.status);
    if (sy.status === 200) {
      const r = await sy.json() as { sync_state?: string; next_cursor?: string | null };
      expect(typeof r.sync_state).toBe('string');
      // T-96 口径：同步游标只许"非空串"或 null；空串是第三种"没有"，非法
      expect(r.next_cursor === null || (typeof r.next_cursor === 'string' && r.next_cursor !== ''),
        `next_cursor=${JSON.stringify(r.next_cursor)} 不符同步游标域`).toBe(true);
    } else {
      const err = (await sy.json()).error;
      expect(err.code).toBe('adapter_unresponsive');
      expect(err.retryable, '503 必须可重试，否则面板会把它当终态').toBe(true);
    }
  });
});

/* ── §4 第一格：POST /projects ───────────────────────────────────────────────
   T-140 现场核过（不是转述）：`handler.go:120` 调 `registerProjectRoutes`，`projects.go:79` 同一 path 按方法分流。
   当时是"POST 已接、GET 仍 501"；**09-23 03:26 复测：GET 也已接（200，items/next_cursor 齐）**，那半句按历史读。
   所以这一格今天就能真跑。第 2/3/4 格的现状见上面 T-189/T-197 那段：**discover 与 probe 仍 501；manage 与 sync 已通到业务层、
   对合成 id 回 404 `session_not_found`** —— 端到端还跑不了，因为 Core 里没有任何一条路径能创建 session 行。 */
describe.skipIf(!live)('真 Core 联调 · §4 第一格（POST /projects，T-140）', () => {
  const uniq = `t140-${Date.now()}`;
  const pathOf = (leaf: string) => `D:/Project/Lucero/.scratch/frontend/proj-${uniq}/${leaf}`;
  const post = (body: Record<string, unknown>, headers: Record<string, string> = {}) =>
    fetch(`${base}/api/v1/projects`, {
      method: 'POST',
      headers: { authorization: `Bearer ${token}`, 'x-lucero-principal': 'local-user',
        'content-type': 'application/json', ...headers },
      body: JSON.stringify(body),
    });

  it('建项目 → 201，Project 的必填字段一个不少、created_at 是 Unix 毫秒', async () => {
    const created = await core().createProject({ name: `p-${uniq}`, canonical_path: pathOf('a') });
    expect(created.id).toBeTruthy();
    expect(created.name).toBe(`p-${uniq}`);
    expect(created.status).toBe('ACTIVE');          // 000001 的初始态，不是面板自己推断的
    expect(created.version).toBe(1);
    expect(typeof created.created_at).toBe('number');
    expect(created.created_at).toBeGreaterThan(1_700_000_000_000);   // 毫秒，不是秒
    console.log(`  项目已建：id=${created.id} status=${created.status} version=${created.version}`);
  });

  it('同一 Idempotency-Key 重放 → 原样回首次响应，且不产生第二个项目', async () => {
    const key = `idem-${uniq}`;
    const body = { name: `r-${uniq}`, canonical_path: pathOf('r') };
    const a = await core().createProject(body, key);
    const b = await core().createProject(body, key);
    expect(b.id).toBe(a.id);                        // id 由 handler 生成、重试复用同一个
    expect(b.created_at).toBe(a.created_at);
    expect(b.version).toBe(1);                      // 重放不推进版本、不再落一行副作用
  });

  it('不带 Idempotency-Key → 403 idempotency_key_required（T-139 收编的那条码在线上真出现）', async () => {
    const res = await post({ name: `k-${uniq}` }, { 'idempotency-key': '' });
    expect(res.status).toBe(403);
    expect((await res.json()).error.code).toBe('idempotency_key_required');
  });

  it('canonicalize 后同路径撞车 → 409 duplicate_project_path（两种拼写算同一个）', async () => {
    const p = pathOf('dup');
    const first = await core().createProject({ name: `d1-${uniq}`, canonical_path: p });
    // 换个写法：斜杠 + 大小写盘符 + 多余分隔符，canonicalize 之后必须与上一条同一路径
    const same = `d:\\Project\\Lucero\\.scratch\\frontend\\proj-${uniq}\\dup\\`;
    const res = await post({ name: `d2-${uniq}`, canonical_path: same }, { 'idempotency-key': `dup-${uniq}` });
    expect(res.status).toBe(409);
    const err = (await res.json()).error;
    expect(err.code).toBe('duplicate_project_path');
    expect(err.retryable).toBe(false);
    console.log(`  冲突如实回 409：首个项目 ${first.id}，未被第二条请求改写`);
  });

  it('C28② 线上实证：同一条 path 的 GET 要么是 200（已接），要么是 501，**永远不许 405**', async () => {
    // 09-23 03:26 复测：`GET /api/v1/projects` 已经脱 501（200，items/next_cursor 齐）。
    // 所以这条不再钉"必须是 501"（那是把进度钉成红），改钉 C28② 的那句话本身：不许用 405 表达未接线。
    const res = await fetch(`${base}/api/v1/projects`, { headers: { authorization: `Bearer ${token}` } });
    expect([200, 501], `405 是假话：它说"这条路径不接受 GET"，而 yaml 里有这个操作（状态 ${res.status}）`)
      .toContain(res.status);
    if (res.status === 501) {
      const err = (await res.json()).error;
      expect(err.code).toBe('not_implemented');
      expect(err.retryable).toBe(true);
    }
  });

  it('垂直切片：先建连 → 建项目 → WS 真收到 project_created → 面板第一格点亮', async () => {
    let state = '';
    const got: EventEntry[] = [];
    const s = openEventStream({
      url: async (fromSeq) => {
        const { ticket } = await core().wsTicket();
        return `${wsBase}/ws/v1/events?ticket=${ticket}` + (fromSeq === null ? '' : `&from_seq=${fromSeq}`);
      },
      fromSeq: null,
      onEvent: (e) => got.push(e),
      onResync: () => {},
      onState: (x) => { state = x; },
    });
    await new Promise((r) => setTimeout(r, 800));
    expect(state).toBe('live');                     // 还没就绪就写，等于测了个空窗口
    const created = await core().createProject({ name: `v-${uniq}`, canonical_path: pathOf('v') });
    for (let i = 0; i < 40 && !got.some((e) => e.event_type === 'project_created'); i++) {
      await new Promise((r) => setTimeout(r, 100));
    }
    const ev = got.find((e) => e.event_type === 'project_created');
    expect(ev, 'REST 写进去的东西必须能从事件面看见，否则面板只能猜').toBeDefined();
    expect(ev!.aggregate_id).toBe(created.id);      // 事件说的是同一个聚合
    // 面板第一格：只由"这个事件出现过"判定，不在 UI 里算状态机（硬约束 1）
    data.seen.add(ev!.event_type);
    expect(stepDone(CHAIN[0])).toBe(true);
    s.close();
    console.log(`  切片成立：seq=${ev!.seq} ${ev!.event_type} → 第一格 ok，面板无一处自造状态`);
  }, 20_000);
});
