// 契约③ §3 端点表 + §4 链映射 + §2/§2.1/§5 约定 是否被 yaml 逐条投影；
// 外加事件契约 §6/§7、契约② §15.2 与 T-17/T-26/T-30/T-127 裁定的落地检查。
// 用法：cd apps/desktop && npm run validate
import SwaggerParser from '@apidevtools/swagger-parser';
import { fileURLToPath } from 'node:url';
import fs from 'node:fs';
import path from 'node:path';
import { STATUS_CODES } from 'node:http';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');
const yamlPath = path.join(root, 'specs', 'openapi', 'core-v1.yaml');

// 契约③ §3 端点表逐行（18:03 版：含 send 与 F-8 批准的两个详情读）+ §2.1 的 ws-ticket
const REQUIRED = [
  'get /api/v1/health', 'post /api/v1/ws-ticket',
  'post /api/v1/projects', 'get /api/v1/projects',
  'get /api/v1/projects/{id}', 'patch /api/v1/projects/{id}',
  'post /api/v1/adapters/{key}/probe', 'get /api/v1/adapters', 'get /api/v1/adapters/{id}',
  'get /api/v1/adapters/{id}/capabilities', 'get /api/v1/models',
  'post /api/v1/adapters/{id}/sessions:discover',
  'post /api/v1/sessions/{id}:manage', 'get /api/v1/sessions', 'get /api/v1/sessions/{id}',
  'get /api/v1/sessions/{id}/messages', 'post /api/v1/sessions/{id}:sync',
  'post /api/v1/tasks', 'get /api/v1/tasks', 'get /api/v1/tasks/{id}', 'patch /api/v1/tasks/{id}',
  'post /api/v1/tasks/{id}:queue', 'post /api/v1/tasks/{id}:pause', 'post /api/v1/tasks/{id}:resume',
  'post /api/v1/tasks/{id}:cancel', 'post /api/v1/tasks/{id}:accept', 'post /api/v1/tasks/{id}:transfer',
  'get /api/v1/executions', 'get /api/v1/executions/{id}', 'get /api/v1/executions/{id}/output',
  'post /api/v1/executions/{id}:cancel', 'post /api/v1/executions/{id}/send',
  'get /api/v1/approvals', 'get /api/v1/approvals/{id}', 'post /api/v1/approvals/{id}:resolve',
  'get /api/v1/quota',
  'get /api/v1/notifications', 'post /api/v1/notifications/{id}:read',
  'post /api/v1/notifications/{id}:handle', 'get /api/v1/search',
];

// 契约③ §4「客户端调用」列的 REST 半边（WS 半边是事件；`stream/delta` 按 F-5 不做 token 级转发）
const CHAIN_CALLS = [
  'post /api/v1/projects', 'post /api/v1/adapters/{id}/sessions:discover',
  'post /api/v1/sessions/{id}:manage', 'post /api/v1/tasks', 'post /api/v1/tasks/{id}:queue',
  'post /api/v1/executions/{id}/send', 'get /api/v1/executions/{id}/output',
  'post /api/v1/approvals/{id}:resolve', 'post /api/v1/tasks/{id}:accept',
];

/** 事件契约 §6 事件目录：按 2026-09-22 落文、23:5x 定稿的「合并行机械展开约定」①~⑤ + ③b 展开。
 *  我之前拿"合并族行要猜前缀"当理由留了一份手抄投影 —— 那份手抄**当场量出少 3 条**
 *  （`policy_created`/`policy_updated` 从 09-19 就在目录里，`notification_read` 是 09-22 补登记的）：
 *  "解析会猜错"不是留快照的理由，⑤/③b 那两条"必须报错、不许静默跳过"正是把猜错变成停下来的东西。 */
function parseEventCatalog(textOverride) {
  let raw = textOverride;
  if (raw === undefined) {
    try {
      raw = fs.readFileSync(EVENT_CONTRACT, 'utf8');
    } catch (e) {
      broken(`读不到事件契约：${EVENT_CONTRACT} —— ${e.message}`);
    }
  }
  const lines = raw.replace(/\r\n?/g, '\n').split('\n');
  const s6 = lines.findIndex((l) => /^## 6\. /.test(l));
  const e6 = lines.findIndex((l, i) => i > s6 && /^## 7\. /.test(l));
  if (s6 < 0 || e6 < 0) broken(`事件契约 §6 区间锚点没找准（§6=${s6 + 1}，§7=${e6 + 1}）`);
  const types = [];
  const lineAt = [];          // 与 types 同下标；重名要能点名到行
  for (let i = s6; i < e6; i++) {
    const line = lines[i];
    if (!/^\| `/.test(line)) continue;
    const m = line.match(/^\| `([a-z_]+)`((?: \/ `_[a-z]+`| \/ `[a-z_]+`)*) \|/);
    if (!m) {
      broken(`§6 第 ${i + 1} 行以反引号开头却没被接受（形状变了，静默跳过=白少收几个类型）：${line.slice(0, 70)}`);
    }
    // ①按 `/` 切 ②去反引号与空白
    const toks = `${m[1]}${m[2]}`.split('/').map((x) => x.replace(/[`\s]/g, '')).filter(Boolean);
    const fulls = toks.filter((t) => !t.startsWith('_'));
    const shorts = toks.filter((t) => t.startsWith('_'));
    // ③（09-22 23:5x 定稿）词干 = 该简写之前**最近一个**全名里最后一个 `_` 及其之前 —— 就是 db 的实际语义。
    // ③b 把"多个全名 + 后面还跟简写"这种混排判为**表格坏了**：这一条把"第一个还是最近一个"的歧义
    //    **取消**而不是裁决（混排非法 ⇒ 不存在能区分两种读法的输入）。探针 E 从此有契约依据，不用我自己扛。
    if (shorts.length && fulls.length > 1) {
      // ③b 只管"多全名 + 简写"；`fulls.length === 0` 那种留给 ⑤（孤儿简写），别让两条规则抢归因
      broken(`§6 第 ${i + 1} 行是 ③b 禁止的混排形态（${fulls.length} 个全名 + ${shorts.length} 个简写）：${line.slice(0, 70)}`);
    }
    const stemOf = (t) => { const at = t.lastIndexOf('_'); return at >= 0 ? t.slice(0, at + 1) : null; };
    let stem = null;                                  // 滚动更新 = "最近一个全名"
    for (const t of toks) {
      if (!t.startsWith('_')) {                       // ④ 全名原样收下
        types.push(t); lineAt.push(i + 1);
        stem = stemOf(t);
        continue;
      }
      if (stem === null) {                            // ⑤ 孤儿 `_xxx`：本行还没有可用的全名词干
        broken(`§6 第 ${i + 1} 行：孤儿 \`${t}\` 出现在本行还没有全名的位置（约定⑤ ⇒ 必须报错，不许静默跳过）`);
      }
      types.push(stem + t.slice(1)); lineAt.push(i + 1);
    }
  }
  if (!types.length) broken(`事件契约 §6 一个事件类型都没解出 ⇒ 解析器坏了`);
  // 重名**不是"我这台仪器坏了"**：集合并集仍然定义良好，所以我照常去重继续判 yaml，
  // 同时把重名交回调用方报**红**（点名 + 行号）。
  // 为什么不在这里 exit 2：停在这里会让后面那 14 条断言全部失明，而 T-165 要的恰恰是
  // "enum ↔ 目录 双向相等"这条**能跑到并自己抓到缺员** ——  blanket stop 反而把它挡在后面。
  const first = new Map();
  const dups = [];
  const uniq = [];
  types.forEach((t, k) => {
    if (first.has(t)) { dups.push({ type: t, firstLine: first.get(t), againLine: lineAt[k] }); return; }
    first.set(t, lineAt[k]);
    uniq.push(t);
  });
  return { types: uniq, dups };
}

/** 事件契约 §7 垂直链：读围栏代码块里的全名，不再手抄 22 条。
 *  ⚠ §6 的合并族行现在有约定可依（见 parseEventCatalog），§7 只有全名、更简单。 */
function parseChainEvents() {
  let raw;
  try {
    raw = fs.readFileSync(EVENT_CONTRACT, 'utf8');
  } catch (e) {
    broken(`读不到事件契约：${EVENT_CONTRACT} —— ${e.message}`);
  }
  const lines = raw.replace(/\r\n?/g, '\n').split('\n');
  const i7 = lines.findIndex((l) => /^## 7\. /.test(l));
  const i8 = lines.findIndex((l, i) => i > i7 && /^## 8\. /.test(l));
  if (i7 < 0 || i8 < 0) broken(`事件契约 §7 区间锚点没找准（§7=${i7 + 1}，§8=${i8 + 1}）`);
  const fence = lines.slice(i7, i8).join('\n').match(/```([\s\S]*?)```/);
  if (!fence) broken('事件契约 §7 里找不到围栏代码块 ⇒ 链 oracle 的形状变了');
  const evs = [...new Set(fence[1]
    .split(/[→\n]/)
    .map((s) => s.replace(/×\s*N/, '').trim())
    .filter((s) => /^[a-z_]+$/.test(s)))];
  if (evs.length < 10) broken(`事件契约 §7 只解出 ${evs.length} 个事件名（链不可能这么短）⇒ 解析器坏了`);
  return evs;
}

// §2 幂等豁免表 + T-30 的免 Principal 表：两张表都只有一条，漂移即红
const IDEMPOTENCY_EXEMPT = ['/api/v1/ws-ticket'];
const PRINCIPAL_EXEMPT = ['/api/v1/ws-ticket'];

/* ── 契约② §15.2 左列：**运行时解析，不内嵌副本**（T-127②）─────────────────
   为什么不能抄一份：REQ-F-14 就是抄的 —— 我那份 15 条快照漏了 §15.2 本来就有的
   read_only / adapter_unresponsive / execution_state_unknown，契约扩列时 validator 永远看不见。
   四个自坑一起防掉：CR 不剥→行尾正则全废；锚点不锚行首→`#### 15.2.1` 与正文示例会冒充标题；
   路径不绝对化→换 cwd 就静默读不到；解析空集不当错→拿半张表判人。 */
const CONTRACT2 = path.join(root, 'specs', 'adapter-protocol', 'adapter-protocol-v0.1.md');
const EVENT_CONTRACT = path.join(root, 'specs', 'events', 'event-contract-v0.1.md');

/** 判据不可用就停（exit 2）。`probing` 时改成 throw，好让下面那组"自破坏探针"就地验守门真的会拦。 */
let probing = false;
function broken(msg) {
  if (probing) throw new Error(msg);
  console.error(`  解析器坏了：${msg}`);
  console.error('  ⇒ 判据不可用。宁可 exit 2 停在这里，也不拿半张表去判 yaml（半张表会判出假的"缺码"）。');
  process.exit(2);
}
/** 闸门判**形状**，不写字面行数（T-139 建议；契约② §15.2.1 末尾同一条：数字只报给人看）。
 *  字面基线每合法扩列一次就红一次，久了训练人去"改数字"而不是读结果 —— 22:5x 那次就是数字过期。
 *  仍然"即停"的条件全部是**不自洽**：锚点歧义 / 区间没有终点 / 零行 / 重码 /
 *  长得像表格却被我的正则丢掉（near-miss）/ 『来自』两种形状都不像 / §15.1↔§15.2 码名集合双向不等。 */
function parseContract2(textOverride) {
  let raw = textOverride;
  if (raw === undefined) {
    try {
      raw = fs.readFileSync(CONTRACT2, 'utf8');
    } catch (e) {
      broken(`读不到契约②：${CONTRACT2} —— ${e.message}`);
    }
  }
  const lines = raw.replace(/\r\n?/g, '\n').split('\n');
  const hits = (re) => lines.map((l, i) => (re.test(l) ? i : -1)).filter((i) => i >= 0);
  const anchored = (re, name, after) => {
    const at = hits(re);
    if (at.length !== 1) broken(`${name} 标题锚点命中 ${at.length} 次（要恰好 1 次），命中处 1-based 行号=[${at.map((i) => i + 1)}]`);
    const stop = hits(after).find((i) => i > at[0]);
    if (stop === undefined) broken(`${name} 之后找不到区间终点（锚点 ${after}）⇒ 硬取文件尾会把别的表读进来`);
    return [at[0], stop];
  };
  // §15.1 的码列（拿来做双向集合，兼当"我切错区间"的探针）
  const [a1, b1] = anchored(/^### 15\.1 /, '§15.1', /^#### 15\.1\.1/);
  const codes151 = [];
  for (let i = a1; i < b1; i++) {
    const m = lines[i].match(/^\| `([A-Z][A-Z0-9_]+)` \|/);
    if (m) codes151.push(m[1]);
    else if (/^\| `[^`]+` \|/.test(lines[i]) && !/^\|[-\s|]+\|$/.test(lines[i]))
      broken(`§15.1 区间里有一行长得像码表却没被解出（第 ${i + 1} 行）：${lines[i].slice(0, 70)} ⇒ 表格形状变了，静默丢行=半张表`);
  }
  if (!codes151.length) broken(`§15.1 区间 ${a1 + 1}..${b1 + 1} 一个码都没解出 ⇒ 解析器坏了`);
  // §15.2 的左列（区间含 §15.2.1 收编的那张表，两张一起读）
  const [s2, e2] = anchored(/^### 15\.2 /, '§15.2', /^### 15\.3/);
  const rows = [];
  for (let i = s2; i < e2; i++) {
    const m = lines[i].match(/^\| `([a-z_]+)` \| (\d{3}) \| (.*) \|$/);
    if (m) rows.push({ code: m[1], status: Number(m[2]), from: m[3] });
    else if (/^\| `/.test(lines[i]))
      broken(`§15.2 区间里有一行以反引号开头却没被解出（第 ${i + 1} 行）：${lines[i].slice(0, 70)} ⇒ 合并族行/少列都会在这里静默漏`);
  }
  if (!rows.length) broken(`§15.2 区间 ${s2 + 1}..${e2 + 1} 解出 0 行表格（区间里有 ${e2 - s2} 行文本）⇒ 判据不可用`);
  // §15.2 的重码与 §6 的重名**处置不同，是有理由的**：这里『状态』列正是我要拿去比对的东西，
  // 同一个码两行、状态不同就没有"哪个是判据"可言 ⇒ 仪器真的无法决定，只能停（§6 的重名不污染集合判断，所以那边是报红继续）。
  const dupCode = rows.map((r) => r.code).filter((c, k) => rows.map((r) => r.code).indexOf(c) !== k);
  if (dupCode.length) broken(`§15.2 左列有重码（${[...new Set(dupCode)].join(', ')}）⇒ 同一码两行时『状态』判据没有唯一答案`);
  const shape = (r) => r.from.trimStart();
  const dashed = rows.filter((r) => shape(r).startsWith('—'));
  const named = rows.filter((r) => shape(r).startsWith('`'));
  const odd = rows.filter((r) => !shape(r).startsWith('—') && !shape(r).startsWith('`'));
  if (odd.length) broken(`§15.2 有 ${odd.length} 行『来自』两种形状都不像（${odd.map((r) => r.code).join(', ')}）`
    + ' ⇒ §15.2.2 明写只许两种合法形状（Adapter 码名 / 以 — 开头），第三种形状就是判据要防的那次误算');
  if (!dashed.length) broken('§15.2 解出 0 行带 — ⇒ HTTP 层专有码全消失，解析器静默空集');
  if (named.length + dashed.length !== rows.length) broken(`分区不闭合：${named.length} + ${dashed.length} ≠ ${rows.length}`);
  // 双向集合（契约② C8-1/C8-4 的形状；违反 = 契约自身不一致或我切错区间，两种都不许继续判 yaml）
  const upper = named.map((r) => (shape(r).match(/^`([A-Z][A-Z0-9_]+)`/)?.[1]) ?? '?');
  if (upper.includes('?')) broken('有行的『来自』以反引号开头却抽不出大写码名 ⇒ 我的正则跟不上表格措辞');
  const notIn151 = [...new Set(upper)].filter((n) => !codes151.includes(n));
  const notIn152 = codes151.filter((n) => !upper.includes(n));
  if (notIn151.length || notIn152.length)
    broken(`§15.1 ↔ §15.2『来自』双向不等：只在 15.2=[${notIn151}]；只在 15.1=[${notIn152}]`
      + ' ⇒ 要么契约自己缺一条（新增码要同批改两表），要么我把区间切错；都不许拿这份表判 yaml');
  return { rows, named, dashed, codes151, range: `${s2 + 1}..${e2 + 1}` };
}

/** Core 真发出来的 transport 码：现场扫 writeError 调用点，不抄 §15.2.1 正文那张表。
 *  ⚠ 采样面是**整个 `core/api/**` 递归**，不是某个文件 —— REQ-F-14 报成"4 条"就是因为只扫了 handler.go。
 *  连 HTTP 状态一起取：只比"码出现过的话"就漏了"码带错状态"这一整类漂移。 */
function scanGoTransportCodes() {
  const dir = path.join(root, 'core', 'api');
  const files = [];
  const walk = (d) => {
    let ents;
    try {
      ents = fs.readdirSync(d, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of ents) {
      const p = path.join(d, e.name);
      if (e.isDirectory()) walk(p);
      else if (e.name.endsWith('.go') && !e.name.endsWith('_test.go')) files.push(p);
    }
  };
  walk(dir);
  if (!files.length) return null;                     // 没有 core/ 源码的签出：这条不判，也不算绿
  const hits = new Map();                             // code → { goName, at, files:Set }
  for (const f of files) {
    const t = fs.readFileSync(f, 'utf8').replace(/\r\n?/g, '\n');
    for (const m of t.matchAll(/writeError\(w, http\.Status([A-Za-z]+), "([a-z_]+)"/g)) {
      const [, goName, code] = m;
      const e = hits.get(code) ?? { goName, at: `${path.basename(f)}:${t.slice(0, m.index).split('\n').length}`, files: new Set() };
      e.files.add(path.basename(f));
      hits.set(code, e);
    }
  }
  return { files: files.length, hits };
}

/** Go 的 `http.StatusXxx` 后缀 → 数字。反查表来自 node 自己的 STATUS_CODES，不另抄一份映射快照；
 *  对不上的（如 Go 用 StatusUnprocessableEntity 而 node 写 "Unprocessable Content"）一律返回 undefined，
 *  由调用方**跳过并打印**，不判红 —— 猜不出来就不许装作判过。
 *  ⚠ 这里曾经错过一次：正则取的是**去掉 `Status` 前缀**的后缀名，我拿带前缀的键去查 ⇒ 15 个全部"反查不到"，
 *  而下面那句 ok 却写着"逐条核过 … 一致" —— **零条被真正比对过还报绿**，比不判更坏。所以现在加了一条
 *  "比对数=0 就不许说核过"的判据。 */
const GO_STATUS = (() => {
  const m = new Map();
  for (const [code, name] of Object.entries(STATUS_CODES)) {
    m.set(String(name).replace(/[^A-Za-z]/g, ''), Number(code));
  }
  return m;
})();

// T-30 裁定：/health 的 state 词表，无 DEGRADED
const HEALTH_STATES = ['BOOTING', 'READY', 'STOPPING', 'STOPPED', 'UNKNOWN'];

// 事件契约 §7 的 22 类链事件不再内嵌（T-127② 同一病根），见 parseChainEvents()

const HTTP = ['get', 'post', 'patch', 'put', 'delete', 'options', 'head', 'trace'];
const fail = [];
const ok = (m) => console.log('  ok   ' + m);
const bad = (m) => { fail.push(m); console.log('  FAIL ' + m); };

let api;
try {
  api = await SwaggerParser.validate(yamlPath);
} catch (e) {
  bad('标准 OpenAPI 解析器读不过：');
  for (const d of e.details ?? [e]) {
    console.error(`     ${d.instancePath || '/'}  ${d.message}` +
      (d.params ? '  ' + JSON.stringify(d.params) : ''));
  }
  process.exit(1);
}
ok(`OpenAPI ${api.openapi} 解析通过：${Object.keys(api.paths).length} 个 path`);

const have = new Set();
for (const [p, item] of Object.entries(api.paths))
  for (const m of HTTP) if (item[m]) have.add(`${m} ${p}`);
for (const op of REQUIRED) have.has(op) ? ok(op) : bad('缺端点 ' + op);
const extra = [...have].filter((o) => !REQUIRED.includes(o)).sort();
extra.length ? bad('契约③ 之外的端点（越权投影）：' + extra.join(', '))
  : ok(`无契约外端点（§3 的 ${REQUIRED.length} 条逐一对上）`);
for (const op of CHAIN_CALLS) have.has(op) ? ok('§4 ' + op) : bad('§4 链上缺端点 ' + op);

// §1 信任边界 + 两张豁免表
for (const [p, item] of Object.entries(api.paths))
  for (const m of HTTP) {
    const op = item[m];
    if (!op) continue;
    const sec = op.security ?? api.security;
    const anon = !sec?.length;
    if (p === '/api/v1/health' && !anon) bad('/health 不应要求鉴权（§1.2 唯一免鉴权）');
    if (p !== '/api/v1/health' && anon) bad(`${m.toUpperCase()} ${p} 缺鉴权声明（§2.1 只免握手头，不免 Bearer）`);
  }
const writes = Object.entries(api.paths).flatMap(([p, item]) =>
  HTTP.filter((m) => ['post', 'patch'].includes(m) && item[m]).map((m) => [m, p, item[m]]));
const idemExemptSeen = [], principalExemptSeen = [];
for (const [m, p, op] of writes) {
  // validate() 已把 $ref 展开成具体参数对象，所以按 header 名判定
  const names = new Set([...(op.parameters ?? []), ...(api.paths[p].parameters ?? [])].map((x) => x.name));
  const noIdem = IDEMPOTENCY_EXEMPT.includes(p);
  const noPrincipal = PRINCIPAL_EXEMPT.includes(p);
  if (noIdem) idemExemptSeen.push(`${m} ${p}`);
  if (noPrincipal) principalExemptSeen.push(`${m} ${p}`);
  if (!noPrincipal && !names.has('X-Lucero-Principal')) bad(`${m.toUpperCase()} ${p} 缺 X-Lucero-Principal（§1.4）`);
  if (noPrincipal && names.has('X-Lucero-Principal')) bad(`${p} 按 T-30 应免 Principal`);
  if (!noIdem && m === 'post' && !names.has('Idempotency-Key')) bad(`${m.toUpperCase()} ${p} 缺 Idempotency-Key（§2）`);
  if (noIdem && names.has('Idempotency-Key')) bad(`${p} 不该吃 Idempotency-Key（§2 豁免表：票会被幂等重放）`);
}
ok(`${writes.length} 个写操作头声明齐备`);
const want = (list) => list.map((p) => `post ${p}`).sort().join(',');
idemExemptSeen.sort().join(',') === want(IDEMPOTENCY_EXEMPT)
  ? ok('幂等豁免集与 §2 表相等：' + want(IDEMPOTENCY_EXEMPT))
  : bad(`幂等豁免集漂移：[${idemExemptSeen}] vs [${want(IDEMPOTENCY_EXEMPT)}]`);
principalExemptSeen.sort().join(',') === want(PRINCIPAL_EXEMPT)
  ? ok('免 Principal 集与 T-30 裁定相等：' + want(PRINCIPAL_EXEMPT))
  : bad(`免 Principal 集漂移：[${principalExemptSeen}] vs [${want(PRINCIPAL_EXEMPT)}]`);
const ticketOk = !!api.paths['/api/v1/ws-ticket']?.post?.responses?.['200'];
ticketOk ? ok('ws-ticket 成功码为 200（实测 Core 返回 200）') : bad('ws-ticket 缺 200 响应');

// §2 约定
const doc = JSON.stringify(api);
for (const [needle, label] of [
  ['Idempotency-Key', '幂等'], ['If-Match', '乐观并发'], ['X-Lucero-Principal', 'principal'],
  ['next_cursor', '游标分页'], ['retryable', '统一错误体'], ['vault://', 'Secret 只以引用出现'],
  ['expires_in', '§2.1 短票有效期'],
]) doc.includes(needle) ? ok(label) : bad('缺' + label);
const offsetParams = Object.entries(api.paths).flatMap(([p, item]) =>
  HTTP.filter((m) => item[m]).flatMap((m) => (item[m].parameters ?? [])
    .filter((x) => x.in === 'query' && x.name === 'offset').map(() => `${m} ${p}`)));
offsetParams.length ? bad('出现 offset 分页（§2 明令禁止）') : ok('无 offset 分页参数');

const S = api.components.schemas;
// 契约② §15.2 三方对拍：**当场解析的契约表** ↔ yaml enum ↔ Core 真发出来的码。
// 判据按 §15.2.1 的形状走（『来自』以 — 开头 = HTTP 层专有），不按名字写死 version_conflict。
const c2 = parseContract2();
// 数字只**打印**（T-139：字面基线一合法扩列就红，久了训练人改数字；契约② §15.2.1 末尾同一条）
ok(`契约② 现场解析：§15.2 第 ${c2.range} 行区间 = ${c2.rows.length} 行`
  + `（${c2.named.length} 带 §15.1 码名 + ${c2.dashed.length} 带 —，两者之和 = 总数已判）；§15.1 码列 ${c2.codes151.length} 个`);
/* 守门的自破坏探针（T-66④ + T-139）：**内存里**改文本，绝不落盘动 specs/（那是别人的写范围）。
   两组都要跑：A 组"不自洽必须停"，B 组"合法内容变化必须放行"—— 只跑 A 组会养成一个一惊一乍的守门，
   而 22:5x 那次红恰恰是 B 组的缺失造成的（行数写死 ⇒ 扩列即红）。 */
try {
  const real = fs.readFileSync(CONTRACT2, 'utf8');
  probing = true;
  const run = (tweak) => {
    try {
      parseContract2(tweak(real));
      return null;
    } catch (e) {
      return e.message;
    }
  };
  for (const [name, tweak] of [
    ['§15.2 标题被改名（区间锚不到）', (t) => t.replace(/^### 15\.2 .*$/m, '### 15.2x 改名了')],
    ['『来自』写成散文（两种形状都不像）', (t) => t.replace('| `not_found` | 404 | —', '| `not_found` | 404 | 见说明')],
    ['出现合并族行（长得像表格、会被静默丢掉）',
      (t) => t.replace(/^\| `timeout` \| 504 \| .*$/m, '| `timeout` / `provider_error` | 504 | —（合并族） |')],
    ['§15.1 那半张表整个没了（双向集合失去对照）', (t) => t.replace(/^### 15\.1 .*$/m, '### 15.1x 没了')],
  ]) {
    const caught = run(tweak);
    caught ? ok(`守门自证 A：${name} → 当场拒判（不拿半张表判 yaml）`)
      : bad(`守门失效 A：${name} 竟被放行 ⇒ 那道"不自洽即停"是装饰品`);
  }
  for (const [name, tweak] of [
    // 合法扩列（22:4x 的 duplicate_project_path 就是这个形状）：放行，缺码由 enum 对拍去红
    ['新收编一行 transport 码',
      (t) => t.replace(/^\| `internal_error` \| 500 \| .*$/m,
        (x) => `${x}\n| \`probe_unsupported\` | 501 | —（Core transport：探针不支持该能力） |`)],
    ['删掉一行（内容变化不是解析器坏了）', (t) => t.replace(/^\| `idempotency_replay` \| 409 \| .*$/m, '')],
  ]) {
    const caught = run(tweak);
    caught ? bad(`守门过敏 B：${name} 被判成"解析器坏了"（${caught.slice(0, 60)}）⇒ 合法扩列也会停，等于逼人改判据`)
      : ok(`守门自证 B：${name} → 放行（判据交给集合对拍，不交给行数）`);
  }
} finally {
  probing = false;
}
const codes = S.ErrorCode?.enum;
Array.isArray(codes) && !S.ErrorCode.anyOf && !S.ErrorCode.pattern
  ? ok(`ErrorCode 是闭集（${codes.length} 条，未放开 free string）`)
  : bad('ErrorCode 不是闭集（§15 明令不得放开）');
const c2Codes = c2.rows.map((r) => r.code);
const missCodes = c2Codes.filter((c) => !codes?.includes(c));
missCodes.length ? bad(`§15.2 有、enum 里没有（${missCodes.length} 条）：` + missCodes.join(', '))
  : ok(`§15.2 左列 ${c2Codes.length} 条全在 enum 里（数据源是当场解析，不是内嵌快照）`);
const extraCodes = (codes ?? []).filter((c) => !c2Codes.includes(c));
extraCodes.length ? bad('enum 里有契约之外的码（越权投影）：' + extraCodes.join(', '))
  : ok('enum 与 §15.2 左列**双向相等**（不多一条也不少一条）');
const go = scanGoTransportCodes();
if (!go) ok('本签出没有 core/api 源码 ⇒ 跳过实现侧对拍（跳过 ≠ 通过，这里明确记为未判）');
else {
  if (!go.hits.size) broken(`扫了 core/api 的 ${go.files} 个非测试 .go 却零命中 writeError ⇒ 扫描正则坏了，不能拿"没扫到"当"没有"`);
  // C8-11 的方向：Core 真发的每个字面码必须 ∈ §15.2 左列（这条才是会漏事的）
  const doc = new Map(c2.rows.map((r) => [r.code, r]));
  const notDoc = [...go.hits.keys()].filter((c) => !doc.has(c));
  notDoc.length
    ? bad(`Core 发出而 §15.2 没有行的码（新码必须同批改契约）：${notDoc.join(', ')}`)
    : ok(`Core 实测 ${go.hits.size} 个字面码（扫 ${go.files} 个文件）全部 ∈ §15.2 左列`);
  // 状态也要对得上：只比"码出现过"会漏掉"码带错状态"
  const wrongStatus = [], unknownStatus = [];
  for (const [code, h] of go.hits) {
    const want = GO_STATUS.get(h.goName);
    if (want === undefined) { unknownStatus.push(`${code}(${h.goName})`); continue; }
    if (doc.get(code).status !== want) wrongStatus.push(`${code}：文档 ${doc.get(code).status} / 代码 ${want} @ ${h.at}`);
  }
  const compared = go.hits.size - unknownStatus.length;
  if (wrongStatus.length) bad('HTTP 状态与 §15.2 不符：' + wrongStatus.join('；'));
  else if (compared === 0) bad(`状态一条都没比对上（${go.hits.size} 个常量名全部反查不到数字）⇒ 扫描/反查器坏了，不许报"一致"`);
  else ok(`HTTP 状态逐条核过：${compared}/${go.hits.size} 条比对一致`
    + (unknownStatus.length ? `（另 ${unknownStatus.length} 条常量名反查不到数字，跳过不判红：${unknownStatus.join(', ')}）` : ''));
  // §15.2.2 裁定（09-22 23:5x）：『来自』只答"这个名字属于哪套词汇"，不答"哪个包发的"
  // ⇒ Core 直接发一个 Adapter 词汇名是**合法的**，这条只打印事实、不再当待裁项挂着。
  const multi = [...go.hits].filter(([, h]) => h.files.size > 1);
  const vocabFromCore = [...go.hits.keys()].filter((c) => !doc.get(c)?.from.trimStart().startsWith('—'));
  ok(`多生产者事实（现场数，不引上一轮）：${multi.length} 个码由 2 个以上文件发`
    + (multi.length ? `（${multi.map(([c, h]) => `${c}×${h.files.size}`).join('、')}）` : '')
    + `；其中 ${vocabFromCore.length} 个用 Adapter 词汇名直接发（§15.2.2：合法，那列不答生产者）`);
}
(codes ?? []).every((c) => /^[a-z][a-z0-9_]*$/.test(c))
  ? ok('全部小写蛇形（RPC 面的大写形没混进来）') : bad('存在非小写蛇形码');
S.ErrorEnvelope.properties.error.properties.code?.enum
  ? ok('error.code 用 ErrorCode 闭集') : bad('error.code 未走 ErrorCode 闭集');
// T-30：/health 词表
const hs = S.Health.properties.state?.enum ?? [];
HEALTH_STATES.every((v) => hs.includes(v)) && !hs.includes('DEGRADED')
  ? ok(`Health.state 闭集 = ${hs.join('/')}（无 DEGRADED）`)
  : bad(`Health.state 词表不符 T-30：[${hs.join(',')}]`);
String(S.Health.properties.ready?.description ?? '').includes('READY')
  ? ok('ready 与 state==READY 的关系写明') : bad('ready 语义未写明');
// F-6 / F-1 / F-8
String(S.JsonMap.description).includes('已解码对象') ? ok('JsonMap 按 F-6 为已解码对象') : bad('JsonMap 未反映 F-6');
const sendProps = Object.keys(S.SendMessageRequest.properties).sort().join(',');
sendProps === 'attachments,delivery,text' ? ok('SendMessageRequest 与 §3 逐字一致')
  : bad(`SendMessageRequest 字段漂移：${sendProps}`);
const sessProj = (api.paths['/api/v1/sessions'].get.parameters ?? []).some((x) => x.name === 'project');
sessProj ? ok('GET /sessions?project= 在位（F-8）') : bad('缺 GET /sessions?project=');

// §5 WS 帧 + §2.1 握手 + F-9 缺省语义
const wf = api['x-websocket-frames'];
wf && ['hello', 'hello_ack', 'event', 'resync_required', 'ping'].every((f) => wf.frames[f])
  ? ok('WS 帧 hello/hello_ack/event/resync_required/ping 齐备') : bad('WS 帧不全（F-9 要求 hello_ack）');
String(wf?.handshake).includes('ticket') ? ok('WS 握手要求一次性短票') : bad('WS 握手未记 ticket（§2.1）');
String(wf?.['cursor-default']).includes('不回灌') && String(wf?.['cursor-authority']).includes('全量重放')
  ? ok('缺省语义已钉死（F-9 + REQ-F-20：省略≠0、禁缺省全量重放）')
  : bad('缺省/回灌口径断言不匹配新措辞（检查 cursor-default 与 cursor-authority）');
S.WsFrame.oneOf.some((x) => x.properties?.type?.enum?.[0] === 'hello_ack')
  ? ok('WsFrame 含 hello_ack') : bad('WsFrame 未含 hello_ack');
// F-16 / F-17
wf?.client_frames?.hello && S.WsClientHello
  ? ok('F-16：客户端 hello 帧已投影（面板连上先报游标）')
  : bad('缺 client_frames.hello / WsClientHello（F-16）');
String(wf?.['known-gap']).includes('F-17')
  ? ok('F-17 空 journal 不回帧的缺口已写进 yaml（不许被读成"连接不健康"）')
  : bad('known-gap 未记 F-17');
S.WsClientHello?.required?.join(',') === 'type' && String(S.WsClientHello.properties.from_seq.type).includes('null')
  ? ok('WsClientHello：from_seq 可省 = 只要此后新事件')
  : bad('WsClientHello 形态不符 F-16');
// T-30 实测：migration_version 是整数不是字符串
String(S.Health.properties.migration_version.type).includes('integer')
  ? ok('Health.migration_version 与实测一致（整数/null）')
  : bad('Health.migration_version 仍是字符串投影（实测 Core 返回整数）');
String(S.WsHelloAck.required) === 'type,server_seq' ? ok('WsHelloAck 必带 server_seq')
  : bad('WsHelloAck 缺 server_seq');

// T-80 两条裁定的落地检查（REQ-F-19 / REQ-F-20）
const nc = S.CursorPage.properties.next_cursor;
nc.type === 'string' && !Array.isArray(nc.type)
  ? ok('CursorPage.next_cursor 收紧为空串唯一合法（null/省略都不允许）')
  : bad(`CursorPage.next_cursor 仍可空（${JSON.stringify(nc.type)}）—— 违反 T-80/REQ-F-19`);
// T-96：两侧对称 —— Session.sync_cursor 也必须 required（上一轮我只收了 minLength，是漏的）
const sessReq = S.Session.required ?? [];
sessReq.includes('sync_cursor')
  ? ok('T-96 Session.sync_cursor 在 required 里（不许拿"省略"当第三种"没有"）')
  : bad('T-96 Session.sync_cursor 可省略 → 与 SyncResult 不对称');
String(S.Session.properties.sync_cursor.type).includes('null') && S.Session.properties.sync_cursor.minLength === 1
  ? ok('T-96 同侧形状 = 可空 + minLength 1（与分页口径相反）')
  : bad('T-96 Session.sync_cursor 形状不是"非空串 | null"');
// REQ-F-21（T-89）：同步游标与分页游标语义相反，四条口径各自钉住
const syncCur = S.SyncResult.properties.next_cursor;
const pageCur = S.CursorPage.properties.next_cursor;
S.SyncResult.required?.includes('next_cursor')
  ? ok('④-a SyncResult.next_cursor 必填（不许拿"省略"当第三种"没有"）')
  : bad('④-a SyncResult.next_cursor 可省略 → 又回到三种"没有"并存');
String(syncCur.type).includes('null')
  ? ok('③ 允许 null（唯一含义=库里 NULL=从未同步）')
  : bad('③ 同步游标被抄成分页口径（非空 string）—— REQ-F-21 明确禁止');
syncCur.minLength === 1
  ? ok('④-b minLength:1 判空串非法（不会发出"从头开始"的假信号）')
  : bad('④-b 缺 minLength:1 → 空串仍合法 = §5 禁止的缺省全量重放入口');
String(S.SyncResult.description).includes('相反')
  ? ok('② schema 里写明"与分页相反"，防后来者误抄')
  : bad('② 没写依据，下一个人还会来抄分页口径');
JSON.stringify(pageCur) !== JSON.stringify(syncCur) && pageCur.minLength === undefined
  ? ok('两个游标形状确实不同（分页=空串唯一 / 同步=非空串|null）')
  : bad('两个游标同形了 —— 正是要收掉的形状');
String(S.Session.properties.sync_cursor.minLength) === '1'
  ? ok('Session.sync_cursor 同域同口径（排除空串）')
  : bad('Session.sync_cursor 没跟着收口');
String(wf?.['cursor-authority']).includes('省略') && String(wf?.order).includes('唯一权威')
  ? ok('§5 的游标权威口径已投影（hello 帧优先，query 仅超时回落）')
  : bad('缺 cursor-authority / order 的 REQ-F-20 口径');
Array.isArray(wf?.criteria) || String(wf?.criteria).match(/[①②③]/)
  ? ok('§5 三条判据随 yaml 一起落地（防后来者只断一边）')
  : bad('未投影 §5 判据 ①②③');
// 事件契约 §7 链 ↔ yaml EventType 目录（链与目录都当场解析；yaml 侧是被判的那一个）
const chainEvents = parseChainEvents();
const { types: catalog, dups: catalogDups } = parseEventCatalog();
catalogDups.length
  ? bad(`事件契约 §6 有 ${catalogDups.length} 个类型被登记了两次（去重后 ${catalog.length} 类）：`
    + catalogDups.map((d) => `${d.type}（第 ${d.firstLine} 行 + 第 ${d.againLine} 行）`).join('；')
    + ' ⇒ 我不改 §6（那是主控该同步的那格），但这条红留着：重名两行的 payload 列哪天漂了，集合对拍看不见')
  : ok(`§6 目录展开无重名（${catalog.length} 类）`);
const catalogMissing = catalog.filter((t) => !(S.EventType.enum ?? []).includes(t));
const catalogExtra = (S.EventType.enum ?? []).filter((t) => !catalog.includes(t));
catalogMissing.length || catalogExtra.length
  ? bad(`EventType enum 与事件契约 §6 目录不等：目录有而 enum 无=[${catalogMissing.join(',')}]；`
      + `enum 有而目录无=[${catalogExtra.join(',')}]（目录外的 event_type 属 P0，两向都不许漂）`)
  : ok(`EventType enum 与 §6 目录**双向相等**（当场按约定①~⑤展开 ${catalog.length} 类，含合并族行）`);
const miss = chainEvents.filter((e) => !catalog.includes(e));
miss.length ? bad(`§7 链上的事件不在 §6 目录里（契约自己不一致）：${miss.join(', ')}`)
  : ok(`事件契约 §7 链的 ${chainEvents.length} 类事件全在目录内（目录 ${catalog.length} 条，两份都当场解析）`);
// 目录解析器的探针：约定⑤ 与"词干取最后一个 _"都要有可执行的证据
try {
  const real = fs.readFileSync(EVENT_CONTRACT, 'utf8');
  probing = true;
  const catRun = (tweak) => {
    try {
      const r = parseEventCatalog(tweak(real));
      return { types: r.types, dups: r.dups, err: null };
    } catch (e) {
      return { types: null, err: e.message };
    }
  };
  for (const [name, tweak] of [
    ['孤儿 `_read` 出现在本行没有全名的位置（约定⑤）',
      (t) => t.replace(/^\| `session_synced` \|/m, '| `_orphan_read` | notification | 探针行 | – |\n| `session_synced` |')],
    ['§6 标题被改名（区间锚不到）', (t) => t.replace(/^## 6\. .*$/m, '## 6x 改名了')],
    ['目录每一行都不再像表格（防静默空集）', (t) => t.replace(/^\| `([a-z_]+)`/gm, '| ($1) ')],
  ]) {
    const r = catRun(tweak);
    r.err ? ok(`守门自证 C：${name} → 当场拒判（${r.err.slice(0, 28)}…）`)
      : bad(`守门失效 C：${name} 竟被放行 ⇒ 约定⑤"必须报错、不许静默跳过"没落地`);
  }
  // 约定③ 的词干取法：`probe_alpha_beta / _failed` ⇒ 最后一个 `_` 之前 = `probe_alpha_` ⇒ probe_alpha_failed；
  // 取"第一个 `_`"会展开成 probe_failed —— 两个结果不同，这条探针就是用来分辨这两种读法的。
  const stemProbe = catRun((t) => t.replace(/^\| `session_synced` \|/m,
    '| `probe_alpha_beta` / `_failed` | probe | 探针行 | – |\n| `session_synced` |'));
  const got = stemProbe.types?.filter((x) => x.startsWith('probe_')) ?? [];
  stemProbe.err ? bad(`词干探针行没能解析：${stemProbe.err.slice(0, 40)}`)
    : got.includes('probe_alpha_failed') && !got.includes('probe_failed')
      ? ok('守门自证 D：词干按**最后一个 `_`** 取（`_failed` → probe_alpha_failed，不是按第一个 `_` 的 probe_failed）')
      : bad(`词干取法与约定③不符：解出 [${got.join(',')}]`);
  // ③b：合并格只许"全写全名"或"一个全名打头 + 若干简写"。混排 = 表格形状坏了，必须报错。
  // （探针 E 原来是我不替契约挑边而自设的守门；23:5x 定稿后它有契约依据了。）
  const amb = catRun((t) => t.replace(/^\| `session_synced` \|/m,
    '| `probe_alpha_beta` / `probe_x_y` / `_failed` | probe | 探针行 | – |\n| `session_synced` |'));
  const mixedBack = catRun((t) => t.replace(/^\| `session_synced` \|/m,
    '| `probe_alpha_beta` / `_short` / `probe_x_y` | probe | 探针行 | – |\n| `session_synced` |'));
  const ambOk = catRun((t) => t.replace(/^\| `session_synced` \|/m,
    '| `probe_alpha_beta` / `probe_x_y` | probe | 探针行 | – |\n| `session_synced` |'));
  amb.err ? ok('守门自证 E：多全名 + 简写（混排）→ 当场拒判（③b，把"第一个/最近一个"的歧义取消）')
    : bad('守门失效 E：混排行被放行 ⇒ 两种词干读法会各自展开出不同名字而无人报');
  // 归因也要对：孤儿属⑤、混排属③b，两条规则不许互相抢（抢错的下一次就是谁都不报）
  const orphan = catRun((t) => t.replace(/^\| `session_synced` \|/m,
    '| `_only_short` | probe | 探针行 | – |\n| `session_synced` |'));
  orphan.err && orphan.err.includes('⑤') && !orphan.err.includes('③b')
    ? ok('归因自证：孤儿简写报在**⑤**名下（没被 ③b 抢走），fulls=0 时 ③b 不该开口')
    : bad(`归因错乱：孤儿简写报成了 [${String(orphan.err).slice(0, 46)}]`);
  mixedBack.err && mixedBack.err.includes('混排')
    ? ok('守门自证 G：简写之后又出现全名 → 同一条 ③b 拒判（不只看行首）')
    : bad(`守门漏角 G：全名出现在简写之后的混排没被 ③b 拦住（err=${String(mixedBack.err).slice(0, 40)}）`);
  ambOk.err
    ? bad(`③b 过敏：两个全名但**没有简写**的行也被拦（${ambOk.err.slice(0, 40)}）⇒ 契约④那种整行写全的形态会被误杀`)
    : ok('守门自证 F：整行写全（无简写）的多全名行照常放行（约定④）');
  // 重名的处置：报红**并继续**（T-165 的实际形状：§6 把 project_archived 登了两次）。
  // 停在这里会让后面 14 条断言集体失明，而"enum ↔ 目录"正是该自己抓到缺员的那条。
  const dupProbe = catRun((t) => t.replace(/^\| `session_synced` \|/m,
    '| `session_managed` | session | 探针重复行 | – |\n| `session_synced` |'));
  dupProbe.err ? bad(`重名被当成"仪器坏了"停掉了（${dupProbe.err.slice(0, 40)}）⇒ 后面的集合判据再也跑不到`)
    : dupProbe.dups?.length === catalogDups.length + 1
      ? ok(`守门自证 H：重复登记的类型 → 去重继续判 + 交回调用方报红（不 blanket stop；基线本有 ${catalogDups.length} 条重复）`)
      : bad(`重名没被数出来（dups=${dupProbe.dups?.length}，基线=${catalogDups.length}+1）⇒ 那条红是空的`);} finally {
  probing = false;
}

for (const n of ['TaskStatus', 'ExecutionStatus', 'Confidence', 'SyncState', 'AdapterHealth',
  'ModelAvailability', 'RiskLevel', 'CapabilityHealth'])
  S[n].enum?.includes('UNKNOWN') ? ok(`${n} 含 UNKNOWN`) : bad(`${n} 丢了 UNKNOWN`);

for (const [sch, f] of [
  ['QuotaObservation', 'used_percent'], ['QuotaObservation', 'remaining_percent'],
  ['QuotaObservation', 'used_value'], ['QuotaObservation', 'remaining_value'],
  ['QuotaObservation', 'reset_at'], ['Session', 'sync_cursor'], ['Session', 'remote_revision'],
]) String(S[sch].properties[f].type).includes('null')
  ? ok(`${sch}.${f} 可为 null`) : bad(`${sch}.${f} 不可为 null → 会被渲染成 0`);

console.log(fail.length ? `\n${fail.length} 项不通过` : '\n全部通过');
process.exit(fail.length ? 1 : 0);
