import type { components, operations } from './gen/schema';

export type Schemas = components['schemas'];

export class CoreError extends Error {
  constructor(readonly status: number, readonly code: string, message: string,
    readonly retryable = false, readonly details?: unknown) {
    super(message || code);
  }
}

export interface CoreClientConfig {
  /** 同源（浏览器走 dev 反代）时留空 */
  baseUrl?: string;
  /**
   * 只给**非浏览器宿主**用（node 侧联调、未来的 Tauri shell）：原样并入请求头。
   * 浏览器面板一个都不传 —— 口令由反代/shell 注入（契约③ §1 第 2.2 项），
   * 所以 `Bearer` 这个字面量不会进构建产物（有负向断言钉着）。
   */
  extraHeaders?: Record<string, string>;
}

type Query = Record<string, string | number | undefined>;

export function createCoreClient(cfg: CoreClientConfig = {}) {
  const headers = (extra: Record<string, string> = {}) => ({
    ...(cfg.extraHeaders ?? {}),
    ...extra,
  });

  async function call<T>(method: string, path: string, opts: {
    query?: Query; body?: unknown; ifMatch?: number; idempotencyKey?: string; anonymous?: boolean;
    noPrincipal?: boolean;
  } = {}): Promise<T> {
    const q = Object.entries(opts.query ?? {}).filter(([, v]) => v !== undefined && v !== '');
    const url = (cfg.baseUrl ?? '') + path + (q.length ? '?' + new URLSearchParams(
      q.map(([k, v]) => [k, String(v)])).toString() : '');
    const writing = method !== 'GET';
    const res = await fetch(url, {
      method,
      headers: headers({
        ...(writing && !opts.anonymous && !opts.noPrincipal ? { 'x-lucero-principal': 'local-user' } : {}),
        ...(opts.idempotencyKey ? { 'idempotency-key': opts.idempotencyKey } : {}),
        ...(opts.ifMatch !== undefined ? { 'if-match': String(opts.ifMatch) } : {}),
        ...(opts.body !== undefined ? { 'content-type': 'application/json' } : {}),
      }),
      ...(opts.body !== undefined ? { body: JSON.stringify(opts.body) } : {}),
    });
    const text = await res.text();
    let payload: { error?: Partial<Record<'code' | 'message' | 'retryable' | 'details', unknown>> } | null = null;
    try {
      payload = text ? JSON.parse(text) : null;
    } catch {
      payload = null;
    }
    // 非 2xx，或 2xx 但不是 JSON —— 都不是契约③ §2 的统一错误体，说明对端不是 Core
    if (!res.ok || payload === null) {
      const e = payload?.error ?? {};
      throw new CoreError(res.status,
        (e.code as string) ?? 'not_core_error_body',
        (e.message as string) ?? (text.slice(0, 160) || res.statusText),
        (e.retryable as boolean) ?? false, e.details);
    }
    return payload as T;
  }

  /** 事件契约 §8：高风险写必须带 Idempotency-Key；调用方可自带 */
  const key = (): string => globalThis.crypto.randomUUID();

  return {
    call,
    newIdempotencyKey: key,

    health: () => call<Schemas['Health']>('GET', '/api/v1/health', { anonymous: true }),

    createProject: (body: Schemas['CreateProjectRequest'], idempotencyKey = key()) =>
      call<Schemas['Project']>('POST', '/api/v1/projects', { body, idempotencyKey }),
    listProjects: (query?: operations['listProjects']['parameters']['query']) =>
      call<Schemas['ProjectList']>('GET', '/api/v1/projects', { query }),

    listAdapters: (query?: operations['listAdapters']['parameters']['query']) =>
      call<Schemas['AdapterList']>('GET', '/api/v1/adapters', { query }),
    probeAdapter: (adapterKey: string, idempotencyKey = key()) =>
      call<Schemas['AdapterInstance']>('POST', `/api/v1/adapters/${adapterKey}/probe`, { idempotencyKey }),
    adapterCapabilities: (id: string, query?: operations['listAdapterCapabilities']['parameters']['query']) =>
      call<Schemas['AdapterCapabilityList']>('GET', `/api/v1/adapters/${id}/capabilities`, { query }),
    listModels: (query?: operations['listModels']['parameters']['query']) =>
      call<Schemas['ModelList']>('GET', '/api/v1/models', { query }),

    listSessions: (query?: operations['listSessions']['parameters']['query']) =>
      call<Schemas['SessionList']>('GET', '/api/v1/sessions', { query }),
    /** §2.1 + T-30：免 Principal（不改业务状态），但仍必须带 Bearer */
    wsTicket: () => call<Schemas['WsTicket']>('POST', '/api/v1/ws-ticket', { noPrincipal: true }),

    discoverSessions: (adapterId: string, idempotencyKey = key()) =>
      call<Schemas['DiscoveredSessionList']>('POST', `/api/v1/adapters/${adapterId}/sessions:discover`,
        { idempotencyKey }),
    manageSession: (id: string, body: Schemas['ManageSessionRequest'], idempotencyKey = key()) =>
      call<Schemas['Session']>('POST', `/api/v1/sessions/${id}:manage`, { body, idempotencyKey }),
    syncSession: (id: string, idempotencyKey = key()) =>
      call<Schemas['SyncResult']>('POST', `/api/v1/sessions/${id}:sync`, { idempotencyKey }),
    sessionMessages: (id: string, query?: operations['listSessionMessages']['parameters']['query']) =>
      call<Schemas['MessageList']>('GET', `/api/v1/sessions/${id}/messages`, { query }),

    createTask: (body: Schemas['CreateTaskRequest'], idempotencyKey = key()) =>
      call<Schemas['Task']>('POST', '/api/v1/tasks', { body, idempotencyKey }),
    listTasks: (query?: operations['listTasks']['parameters']['query']) =>
      call<Schemas['TaskList']>('GET', '/api/v1/tasks', { query }),
    getTask: (id: string) => call<Schemas['Task']>('GET', `/api/v1/tasks/${id}`),
    updateTask: (id: string, body: Schemas['UpdateTaskRequest'], ifMatch: number) =>
      call<Schemas['Task']>('PATCH', `/api/v1/tasks/${id}`, { body, ifMatch }),
    taskAction: (id: string, verb: 'queue' | 'pause' | 'resume' | 'cancel' | 'accept',
      opts: { ifMatch?: number; reason?: string; idempotencyKey?: string } = {}) =>
      call<Schemas['Task']>('POST', `/api/v1/tasks/${id}:${verb}`,
        { ifMatch: opts.ifMatch, idempotencyKey: opts.idempotencyKey ?? key(),
          ...(opts.reason !== undefined ? { body: { reason: opts.reason } } : {}) }),
    transferTask: (id: string, body: Schemas['TransferRequest'], opts: { ifMatch?: number; idempotencyKey?: string } = {}) =>
      call<Schemas['Handoff']>('POST', `/api/v1/tasks/${id}:transfer`,
        { body, ifMatch: opts.ifMatch, idempotencyKey: opts.idempotencyKey ?? key() }),

    listExecutions: (query: operations['listExecutions']['parameters']['query']) =>
      call<Schemas['ExecutionList']>('GET', '/api/v1/executions', { query }),
    getExecution: (id: string) => call<Schemas['Execution']>('GET', `/api/v1/executions/${id}`),
    executionOutput: (id: string, query?: operations['getExecutionOutput']['parameters']['query']) =>
      call<Schemas['OutputChunkList']>('GET', `/api/v1/executions/${id}/output`, { query }),
    send: (id: string, body: Schemas['SendMessageRequest'], idempotencyKey = key()) =>
      call<Schemas['SendResult']>('POST', `/api/v1/executions/${id}/send`, { body, idempotencyKey }),
    cancelExecution: (id: string, opts: { ifMatch?: number; idempotencyKey?: string } = {}) =>
      call<Schemas['Execution']>('POST', `/api/v1/executions/${id}:cancel`,
        { ifMatch: opts.ifMatch, idempotencyKey: opts.idempotencyKey ?? key() }),

    listApprovals: (query?: operations['listApprovals']['parameters']['query']) =>
      call<Schemas['ApprovalList']>('GET', '/api/v1/approvals', { query }),
    getApproval: (id: string) => call<Schemas['Approval']>('GET', `/api/v1/approvals/${id}`),
    resolveApproval: (id: string, body: Schemas['ResolveApprovalRequest'],
      opts: { ifMatch?: number; idempotencyKey?: string } = {}) =>
      call<Schemas['Approval']>('POST', `/api/v1/approvals/${id}:resolve`,
        { body, ifMatch: opts.ifMatch, idempotencyKey: opts.idempotencyKey ?? key() }),

    quota: (query?: operations['getQuota']['parameters']['query']) =>
      call<Schemas['QuotaResponse']>('GET', '/api/v1/quota', { query }),

    listNotifications: (query?: operations['listNotifications']['parameters']['query']) =>
      call<Schemas['NotificationList']>('GET', '/api/v1/notifications', { query }),
    notificationAction: (id: string, verb: 'read' | 'handle', idempotencyKey = key()) =>
      call<Schemas['Notification']>('POST', `/api/v1/notifications/${id}:${verb}`, { idempotencyKey }),

    search: (query: operations['search']['parameters']['query']) =>
      call<Schemas['SearchList']>('GET', '/api/v1/search', { query }),
  };
}

export type CoreClient = ReturnType<typeof createCoreClient>;
