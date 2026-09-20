// 契约③ §3 端点表 + §4 链映射 + §2/§2.1/§5 约定 是否被 yaml 逐条投影；
// 外加契约① §6/§7、契约② §15.2 与 T-17/T-26/T-30 裁定的落地检查。
// 用法：cd apps/desktop && npm run validate
import SwaggerParser from '@apidevtools/swagger-parser';
import { fileURLToPath } from 'node:url';
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

// §2 幂等豁免表 + T-30 的免 Principal 表：两张表都只有一条，漂移即红
const IDEMPOTENCY_EXEMPT = ['/api/v1/ws-ticket'];
const PRINCIPAL_EXEMPT = ['/api/v1/ws-ticket'];

// 契约② §15.2 左列 15 条（闭集，不得放开）
const RPC_CODES = ['protocol_not_initialized', 'protocol_version_mismatch', 'capability_unavailable',
  'write_entry_none', 'delivery_mode_unsupported', 'local_data_unavailable', 'session_not_found',
  'auth_required', 'rate_limited', 'dangerous_flag_refused', 'cancelled', 'timeout', 'provider_error',
  'internal', 'version_conflict'];
// Core transport 自有码（实测自 api/http/handler.go），§15.2 尚未收编 → REQ-F-14
const TRANSPORT_CODES = ['unauthorized', 'origin_not_allowed', 'method_not_allowed', 'not_implemented'];

// T-30 裁定：/health 的 state 词表，无 DEGRADED
const HEALTH_STATES = ['BOOTING', 'READY', 'STOPPING', 'STOPPED', 'UNKNOWN'];

// 契约① §7 垂直链事件序列
const CHAIN_EVENTS = [
  'project_created', 'session_discovered', 'session_managed', 'task_created', 'task_queued',
  'task_preparing', 'execution_created', 'execution_started', 'task_running', 'message_recorded',
  'execution_output_flushed', 'execution_approval_requested', 'task_waiting_approval',
  'approval_requested', 'notification_created', 'approval_resolved', 'grant_issued',
  'execution_approval_resolved', 'execution_succeeded', 'task_agent_completed',
  'task_waiting_acceptance', 'task_accepted',
];

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
// 契约② §15.2：闭集、覆盖 15 条、无大写形、error.code 指向它
const codes = S.ErrorCode?.enum;
Array.isArray(codes) && !S.ErrorCode.anyOf && !S.ErrorCode.pattern
  ? ok(`ErrorCode 是闭集（${codes.length} 条，未放开 free string）`)
  : bad('ErrorCode 不是闭集（§15 明令不得放开）');
const missCodes = RPC_CODES.filter((c) => !codes?.includes(c));
missCodes.length ? bad('§15.2 缺码：' + missCodes.join(', ')) : ok(`§15.2 的 15 条全在`);
const extraCodes = (codes ?? []).filter((c) => !RPC_CODES.includes(c));
extraCodes.length
  ? (extraCodes.every((c) => TRANSPORT_CODES.includes(c))
      ? ok(`另含 transport 自有码 ${extraCodes.length} 条（${extraCodes.join(', ')}）→ REQ-F-14 待 §15.2 收编`)
      : bad('ErrorCode 混进未登记的码：' + extraCodes.join(', ')))
  : bad('ErrorCode 缺 Core transport 自有码（实测会返回，闭集必须覆盖）');
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
String(wf?.['cursor-default']).includes('禁止') && String(wf?.['cursor-default']).includes('全量重放')
  ? ok('from_seq 缺省语义钉死（F-9）') : bad('cursor-default 未写（F-9）');
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

// 契约① §6/§7
const miss = CHAIN_EVENTS.filter((e) => !S.EventType.enum.includes(e));
miss.length ? bad('EventType 目录漏链事件：' + miss.join(', '))
  : ok(`契约① §7 链的 ${CHAIN_EVENTS.length} 类事件全在目录内（目录 ${S.EventType.enum.length} 条）`);

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
