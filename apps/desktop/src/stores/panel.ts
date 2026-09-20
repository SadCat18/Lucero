import { reactive, shallowRef } from 'vue';
import { createCoreClient, CoreError, type Schemas } from '@lucero/api-client/client';
import { openEventStream, type EventEntry, type EventStream, type LinkState } from '@lucero/api-client/events';

/** 契约① §7 垂直链 → 面板上可点的 11 步。每步只由"该步的事件是否出现过"判定，
 *  状态机裁决全在 Core，这里不做任何推断。 */
export interface ChainStep {
  key: string;
  label: string;
  events: string[];
}

export const CHAIN: ChainStep[] = [
  { key: 'project', label: 'Create Project', events: ['project_created'] },
  { key: 'discover', label: 'Discover Fake Session', events: ['session_discovered'] },
  { key: 'managed', label: 'Mark Managed', events: ['session_managed'] },
  { key: 'task', label: 'Create Task', events: ['task_created'] },
  { key: 'execution', label: 'Create Execution',
    events: ['task_queued', 'task_preparing', 'execution_created'] },
  { key: 'send', label: 'Send Message', events: ['execution_started', 'task_running', 'message_recorded'] },
  { key: 'output', label: 'Stream Output', events: ['execution_output_flushed'] },
  { key: 'approval', label: 'Request Approval',
    events: ['execution_approval_requested', 'task_waiting_approval', 'approval_requested', 'notification_created'] },
  { key: 'resolve', label: 'Resolve Approval',
    events: ['approval_resolved', 'grant_issued', 'execution_approval_resolved'] },
  { key: 'succeed', label: 'Continue → Succeeded',
    events: ['execution_succeeded', 'task_agent_completed', 'task_waiting_acceptance'] },
  { key: 'accept', label: 'User Accept', events: ['task_accepted'] },
];

const WS_EVENT_PATH = '/ws/v1/events';
const TIMELINE_CAP = 500;
const OUTPUT_CAP = 2000;
/** Core API 未落地时不要无限重试打空枪 */
const CHAIN_WAIT_MS = 20_000;

export type CallOutcome = 'ok' | 'blocked' | 'error';

export interface ChainCall {
  step: string;
  method: string;
  outcome: CallOutcome;
  detail: string;
}

export const core = createCoreClient({});

export const link = reactive({
  state: 'connecting' as LinkState,
  detail: '',
  seq: null as number | null,
  resync: false,
  reason: '',
});

export const data = reactive({
  health: null as Schemas['Health'] | null,
  bootError: '',
  adapters: [] as Schemas['AdapterInstance'][],
  events: [] as EventEntry[],
  tasks: [] as Schemas['Task'][],
  sessions: [] as Schemas['Session'][],
  approvals: [] as Schemas['Approval'][],
  notifications: [] as Schemas['Notification'][],
  quota: null as Schemas['QuotaResponse'] | null,
  output: [] as Schemas['OutputChunk'][],
  seen: new Set<string>(),
});

export const chain = reactive({
  running: false,
  calls: [] as ChainCall[],
});

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

export function stepDone(step: ChainStep): boolean {
  return step.events.some((e) => data.seen.has(e));
}

export function stepState(step: ChainStep, index: number): 'done' | 'current' | 'pending' {
  if (stepDone(step)) return 'done';
  return CHAIN.slice(0, index).every((s) => stepDone(s)) ? 'current' : 'pending';
}

function recordEvent(e: EventEntry) {
  data.seen.add(e.event_type);
  data.events.push(e);
  if (data.events.length > TIMELINE_CAP) data.events.splice(0, data.events.length - TIMELINE_CAP);
  if (e.event_type === 'execution_output_flushed' && e.aggregate_id) pullOutput(e.aggregate_id).catch(() => {});
}

/** journal 被裁剪 → 只能走 REST 全量；REST 没覆盖的 timeline 缺口必须显式标注，不得假装完整 */
function markResync() {
  link.resync = true;
  link.reason = 'event journal 已裁剪，无法从当前游标补发';
  data.seen.clear();
  data.events.length = 0;
  refresh().catch(() => {});
}

/** 把 REST 失败翻成面板能说的话。403 基本都是 Origin 没进 Core 的白名单（§1.3 + F-3 裁定） */
function describeFailure(err: unknown): string {
  if (err instanceof CoreError) {
    return err.status === 403
      ? '403 —— 面板来源不在 Core 的 LUCERO_DEV_ORIGIN 白名单内（§1.3 / F-3：未配置即 403）'
      : `${err.status} ${err.code}`;
  }
  return String(err);
}

export async function refresh() {
  const [health, adapters, tasks, sessions, approvals,notifications, quota] = await Promise.allSettled([
    core.health(), core.listAdapters({ limit: 20 }), core.listTasks({ limit: 50 }),
    core.listSessions({ limit: 50 }),
    core.listApprovals({ state: 'PENDING', limit: 50 }),
    core.listNotifications({ state: 'UNREAD', limit: 50 }), core.quota({}),
  ]);
  data.bootError = health.status === 'rejected' ? describeFailure(health.reason) : '';
  if (health.status === 'fulfilled') data.health = health.value;
  if (adapters.status === 'fulfilled') data.adapters = adapters.value.items;
  if (tasks.status === 'fulfilled') data.tasks = tasks.value.items;
  if (sessions.status === 'fulfilled') data.sessions = sessions.value.items;
  if (approvals.status === 'fulfilled') data.approvals = approvals.value.items;
  if (notifications.status === 'fulfilled') data.notifications = notifications.value.items;
  if (quota.status === 'fulfilled') data.quota = quota.value;
  return { health, adapters, tasks, sessions, approvals, notifications, quota };
}

async function pullOutput(executionId: string) {
  const last = data.output.length ? data.output[data.output.length - 1].sequence_no : undefined;
  const page = await core.executionOutput(executionId, { after_seq: last });
  for (const c of page.items) {
    if (data.output.some((x) => x.execution_id === c.execution_id && x.sequence_no === c.sequence_no)) continue;
    data.output.push(c);
  }
  if (data.output.length > OUTPUT_CAP) data.output.splice(0, data.output.length - OUTPUT_CAP);
}

async function call<T>(step: string, method: string, fn: () => Promise<T>): Promise<T | undefined> {
  try {
    const v = await fn();
    chain.calls.push({ step, method, outcome: 'ok', detail: 'ok' });
    return v;
  } catch (err) {
    const ce = err as CoreError;
    // 404/501 = 端点还没实现；连不上或对端不是 Core = 本阶段常态。都算 blocked，不是面板自己的错
    const blocked = err instanceof TypeError
      || (ce instanceof CoreError && ([404, 501].includes(ce.status) || ce.code === 'not_core_error_body'));
    chain.calls.push({
      step, method,
      outcome: blocked ? 'blocked' : 'error',
      detail: ce instanceof CoreError ? `${ce.status} ${ce.code}` : String(err),
    });
    if (blocked) chain.running = false;
    throw err;
  }
}

/** 等某个事件出现（事件是事实来源，不看 HTTP 返回值猜状态） */
async function waitEvent(types: string[], timeoutMs = CHAIN_WAIT_MS): Promise<boolean> {
  const until = Date.now() + timeoutMs;
  while (Date.now() < until) {
    if (types.some((t) => data.seen.has(t))) return true;
    await wait(200);
  }
  return false;
}

/** 契约③ §4 的 11 步逐条调用。任一步 blocked/error 即停，失败点原样进 calls。 */
export async function runChain() {
  if (chain.running) return;
  chain.running = true;
  chain.calls.length = 0;
  const stamp = Date.now();
  const idem = (op: string) => `panel-${stamp}-${op}`;
  try {
    const adapter = await call('adapter', 'POST /adapters/fake/probe',
      () => core.probeAdapter('fake', idem('probe')));
    if (!adapter) return;

    const project = await call('project', 'POST /projects',
      () => core.createProject({ name: `panel-${stamp}`, canonical_path: `/evidence/panel-${stamp}` }, idem('project')));

    const discovered = await call('discover', `POST /adapters/${adapter.id}/sessions:discover`,
      () => core.discoverSessions(adapter.id, idem('discover')));
    const found = discovered?.items[0];
    if (!found) {
      chain.calls.push({
        step: 'discover', method: 'POST /adapters/{id}/sessions:discover',
        outcome: 'blocked', detail: 'Provider 侧没有可发现的会话',
      });
      return;
    }

    await call('managed', `POST /sessions/${found.session_id}:manage`,
      () => core.manageSession(found.session_id, { managed: true, project_id: project?.id }, idem('manage')));

    const task = await call('task', 'POST /tasks',
      () => core.createTask({ title: `panel chain ${stamp}`, project_id: project?.id }, idem('task')));
    if (!task) return;

    const queued = await call('execution', `POST /tasks/${task.id}:queue`,
      () => core.taskAction(task.id, 'queue', { ifMatch: task.version, idempotencyKey: idem('queue') }));

    const executions = await call('execution', 'GET /executions?task_id=',
      () => core.listExecutions({ task_id: task.id! }));
    const execution = executions?.items.at(-1);
    if (!execution) {
      chain.calls.push({ step: 'execution', method: 'GET /executions', outcome: 'blocked', detail: '没有 Execution 被创建出来' });
      return;
    }

    await call('send', `POST /executions/${execution.id}/send`,
      () => core.send(execution.id, { text: 'panel: 请触发一次审批然后完成', delivery: 'start_now' }, idem('send')));

    if (!(await waitEvent(['execution_output_flushed']))) {
      chain.calls.push({ step: 'output', method: 'WS execution_output_flushed', outcome: 'blocked', detail: '超时未见输出' });
      return;
    }
    const approvals = await call('approval', 'GET /approvals?state=PENDING',
      () => core.listApprovals({ state: 'PENDING' }));
    const pending = approvals?.items.find((a) => a.execution_id === execution.id) ?? approvals?.items[0];
    if (!pending) {
      chain.calls.push({ step: 'approval', method: 'GET /approvals', outcome: 'blocked', detail: 'FakeAdapter 未产生待决审批' });
      return;
    }
    await call('approval', `GET /executions/${execution.id}`,
      () => core.getExecution(execution.id));

    await call('resolve', `POST /approvals/${pending.id}:resolve`,
      () => core.resolveApproval(pending.id, { state: 'APPROVED' }, { idempotencyKey: idem('resolve') }));
    void queued;

    if (!(await waitEvent(['execution_succeeded', 'task_agent_completed'], CHAIN_WAIT_MS))) {
      chain.calls.push({ step: 'succeed', method: 'WS execution_succeeded', outcome: 'blocked', detail: '超时未见成功' });
      return;
    }

    const latest = await call('accept', `GET /tasks/${task.id}`, () => core.getTask(task.id));
    if (!latest) return;
    await call('accept', `POST /tasks/${latest.id}:accept`,
      () => core.taskAction(latest.id, 'accept', { ifMatch: latest.version, idempotencyKey: idem('accept') }));
    await waitEvent(['task_accepted'], 5_000).catch(() => false);
  } catch {
    // 失败已经写进 chain.calls，这里不再重复抛
  } finally {
    chain.running = false;
    refresh().catch(() => {});
  }
}

export async function resolveApproval(id: string, state: 'APPROVED' | 'REJECTED', ifMatch?: number) {
  await core.resolveApproval(id, { state }, { ifMatch });
  await refresh();
}

export const stream = shallowRef<EventStream | null>(null);

/** §2.1：票单次使用，所以每次（重）连都重新换一张，不能复用上一次握手用过的 */
async function ticketedUrl(fromSeq: number | null) {
  const { ticket } = await core.wsTicket();
  const q = new URLSearchParams({ ticket });
  if (fromSeq !== null) q.set('from_seq', String(fromSeq));
  return `${location.origin.replace(/^http/, 'ws')}${WS_EVENT_PATH}?${q.toString()}`;
}

export function connectEvents(opts: {
  url?: (fromSeq: number | null) => string | Promise<string>;
  socketFactory?: (url: string) => WebSocket;
} = {}) {
  stream.value?.close();
  stream.value = openEventStream({
    url: opts.url ?? ticketedUrl,
    socketFactory: opts.socketFactory,
    fromSeq: link.seq,
    onEvent: recordEvent,
    onResync: markResync,
    onState: (state, detail) => {
      link.state = state;
      link.detail = detail ?? '';
      if (state === 'live') link.resync = false;
    },
    onSeq: (seq) => { link.seq = seq; },
  });
  return stream.value;
}
