// 形状不依赖端点能否跑（T-96）：把真 Core 会回的载荷形状直接喂给 schema，要求
// 「缺键」与「同步侧空串」必须判红 —— 判据住在 schema 里，不是我记在脑子里。
import { beforeAll, describe, expect, it } from 'vitest';
import SwaggerParser from '@apidevtools/swagger-parser';
import Ajv, { type ValidateFunction } from 'ajv';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

type Json = Record<string, any>;
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../..');
const compiled = new Map<string, ValidateFunction>();
let schemas: Record<string, Json> = {};

beforeAll(async () => {
  const api = await SwaggerParser.dereference(path.join(root, 'specs', 'openapi', 'core-v1.yaml'));
  schemas = (api as unknown as { components: { schemas: Record<string, Json> } }).components.schemas;
});

/** 用 schema 判实例，而不是用我的记忆判实例 */
function validate(name: string): (data: unknown) => boolean {
  let fn = compiled.get(name);
  if (!fn) {
    fn = new Ajv({ strict: false, logger: false }).compile(schemas[name]);
    compiled.set(name, fn);
  }
  return (data) => fn!(data) as boolean;
}

/** 在内存里降级某个 schema，用来自证断言的判别力 */
function degraded(name: string, tweak: (schema: Json) => void): (data: unknown) => boolean {
  const clone = structuredClone(schemas[name]) as Json;
  tweak(clone);
  const fn = new Ajv({ strict: false, logger: false }).compile(clone);
  return (data) => fn(data) as boolean;
}

const okSession = () => ({
  id: `s1`, adapter_instance_id: `a1`, managed: true, sync_state: `SYNCED`,
  state_confidence: `HIGH`, sync_cursor: `20`,
});
const { sync_cursor: _sc, ...sessionMissingCursor } = okSession();   // 故意缺 sync_cursor

describe('游标两域的形状（契约③ §2「同步游标」/「分页」+ T-89/T-96）', () => {
  it('合法形状通过：sync_cursor 只许"非空串"或 null', () => {
    expect(validate(`Session`)(okSession())).toBe(true);
    expect(validate(`Session`)({ ...okSession(), sync_cursor: null })).toBe(true);
  });

  it('缺键必须判红：省略 sync_cursor 是非法的第三种"没有"', () => {
    expect(validate(`Session`)(sessionMissingCursor)).toBe(false);
  });

  it('空串在同步侧必须判红（会被读成"从头开始"= §5 禁止的缺省全量重放）', () => {
    expect(validate(`Session`)({ ...okSession(), sync_cursor: `` })).toBe(false);
    expect(validate(`SyncResult`)({ sync_state: `SYNCED`, next_cursor: `` })).toBe(false);
  });

  it('SyncResult 同样不许缺键；null 只作"从未同步"', () => {
    expect(validate(`SyncResult`)({ sync_state: `SYNCED` })).toBe(false);
    expect(validate(`SyncResult`)({ sync_state: `SYNCED`, next_cursor: `20` })).toBe(true);
    expect(validate(`SyncResult`)({ sync_state: `UNKNOWN`, next_cursor: null })).toBe(true);
  });

  it('分页侧反过来：空串合法、null 非法 —— 两域语义相反，不许同形', () => {
    expect(validate(`TaskList`)({ items: [], next_cursor: `` })).toBe(true);
    expect(validate(`TaskList`)({ items: [], next_cursor: null })).toBe(false);
    expect(validate(`TaskList`)({ items: [] })).toBe(false);
  });

  /**
   * T-66④ 的自证：把规则抽掉，非法载荷必须变成被接受 —— 否则上面那些"必须判红"可能在验别的东西。
   * 我上一轮靠改磁盘做变异就撞过这个坑（改动没落地、测试仍全绿），所以这里在进程内做。
   */
  it('自证判别力：抽掉 minLength / required 后，非法载荷必须被接受', () => {
    const noMinLen = degraded(`Session`, (c) => { delete c.properties.sync_cursor.minLength; });
    expect(noMinLen({ ...okSession(), sync_cursor: `` })).toBe(true);
    expect(validate(`Session`)({ ...okSession(), sync_cursor: `` })).toBe(false);

    const noRequired = degraded(`Session`, (c) => {
      c.required = (c.required as string[]).filter((k) => k !== `sync_cursor`);
    });
    expect(noRequired(sessionMissingCursor)).toBe(true);
    expect(validate(`Session`)(sessionMissingCursor)).toBe(false);

    const pageNoRequired = degraded(`TaskList`, (c) => {
      c.allOf = (c.allOf as Json[]).map((part) => (part.required ? { ...part, required: [] } : part));
    });
    expect(pageNoRequired({ items: [] })).toBe(true);
  });
});
