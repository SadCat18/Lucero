// 契约③ §3 端点表 + §4 链映射 + §2/§2.1/§5 约定 是否被 yaml 逐条投影；
// 外加事件契约 §6/§7、契约② §15.2 与 T-17/T-26/T-30/T-127 裁定的落地检查。
// 用法：cd apps/desktop && npm run validate
import SwaggerParser from '@apidevtools/swagger-parser';
import { fileURLToPath } from 'node:url';
import fs from 'node:fs';
import path from 'node:path';

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

/** 事件契约 §7 垂直链：读围栏代码块里的全名，不再手抄 22 条。
 *  ⚠ §6 事件目录**故意不解析**：那张表有 `execution_cancelled / _failed / _succeeded` 这种合并族行，
 *  展开要靠"同前缀"推断，猜错就是一次静默的半张表 —— 那比快照更坏。EventType 仍是我手抄的投影，
 *  重核命令见 CHAIN 下面那条注释。 */
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
  if (new Set(rows.map((r) => r.code)).size !== rows.length) broken('§15.2 左列有重码 ⇒ 集合运算不可信');
  const shape = (r) => r.from.trimStart();
  const dashed = rows.filter((r) => shape(r).startsWith('—'));
  const named = rows.filter((r) => shape(r).startsWith('`'));
  const odd = rows.filter((r) => !shape(r).startsWith('—') && !shape(r).startsWith('`'));
  if (odd.length) broken(`§15.2 有 ${odd.length} 行『来自』两种形状都不像（${odd.map((r) => r.code).join(', ')}）`
    + ' ⇒ "按形状豁免"这条判据本身失效了');
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
 *  ⚠ 采样面是**整个 `core/api/**` 递归**，不是某个文件 —— REQ-F-14 报成"4 条"就是因为只扫了 handler.go。 */
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
  const set = new Set();
  for (const f of files) {
    const t = fs.readFileSync(f, 'utf8').replace(/\r\n?/g, '\n');
    for (const m of t.matchAll(/writeError\(w, http\.Status[A-Za-z]+, "([a-z_]+)"/g)) set.add(m[1]);
  }
  return { files: files.length, codes: [...set].sort() };
}

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
  if (!go.codes.length) broken(`扫了 core/api 的 ${go.files} 个非测试 .go 却零命中 writeError ⇒ 扫描正则坏了，不能拿"没扫到"当"没有"`);
  const dashedSet = new Set(c2.dashed.map((r) => r.code));
  const goNotDoc = go.codes.filter((c) => !dashedSet.has(c));
  const docNotGo = [...dashedSet].filter((c) => !go.codes.includes(c));
  goNotDoc.length || docNotGo.length
    ? bad(`实现侧与 §15.2 的 — 行不等：Core 真发而文档未列=[${goNotDoc.join(',')}]；文档列而 Core 不发=[${docNotGo.join(',')}]`)
    : ok(`Core 实测 ${go.codes.length} 条 transport 码（扫 ${go.files} 个文件）与 §15.2 的 ${dashedSet.size} 行 — 逐字相等`);
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
// 事件契约 §7 链 ↔ yaml EventType 目录（链当场解析；目录侧仍是我的投影，见下面那条 note）
const chainEvents = parseChainEvents();
const miss = chainEvents.filter((e) => !S.EventType.enum.includes(e));
miss.length ? bad('EventType 目录漏链事件：' + miss.join(', '))
  : ok(`事件契约 §7 链的 ${chainEvents.length} 类事件全在目录内（目录 ${S.EventType.enum.length} 条）`);

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
