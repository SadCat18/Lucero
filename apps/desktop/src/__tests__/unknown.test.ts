// §12 P0 红线：无法确认时不得显示确定的 Running/Failed/0 quota。
// 这里既断言纯函数，也断言组件真的渲染成"未知"（不是只测工具函数）。
import { describe, expect, it } from 'vitest';
import { createSSRApp } from 'vue';
import { renderToString } from 'vue/server-renderer';
import QuotaPanel from '../components/QuotaPanel.vue';
import TaskPanel from '../components/TaskPanel.vue';
import SessionPanel from '../components/SessionPanel.vue';
import StatusHeader from '../components/StatusHeader.vue';
import { data } from '../stores/panel';
import type { Schemas } from '@lucero/api-client/client';
import { num, observed, pct, statusText } from '@lucero/api-client/unknown';

const render = (c: Parameters<typeof createSSRApp>[0]) => renderToString(createSSRApp(c));
/** 只取数据格的内容：组件里的说明文字不算渲染结果 */
const cells = (html: string) => [...html.matchAll(/<td[^>]*>(?:<code[^>]*>)?([^<]*)/g)].map((m) => m[1]);

const task = (over: Partial<Schemas['Task']> = {}): Schemas['Task'] => ({
  id: 't1', title: 'x', status: 'RUNNING', priority: 'NORMAL', risk_level: 'LOW', mode: 'EXECUTE',
  version: 1, created_at: 1, updated_at: 1, ...over,
});
const session = (over: Partial<Schemas['Session']> = {}): Schemas['Session'] => ({
  id: 's1', adapter_instance_id: 'a1', managed: true, sync_state: 'SYNCED',
  state_confidence: 'HIGH', ...over,
});
const observation = (over: Partial<Schemas['QuotaObservation']> = {}): Schemas['QuotaObservation'] => ({
  quota_key: 'weekly', quota_type: 'PERCENT', observation_kind: 'OBSERVED',
  confidence: 'HIGH', ...over,
});
const adapter = (over: Partial<Schemas['AdapterInstance']> = {}): Schemas['AdapterInstance'] => ({
  id: 'a1', adapter_key: 'fake', display_name: 'Fake', health: 'HEALTHY', enabled: true, ...over,
});

data.health = null; data.adapters = []; data.tasks = []; data.sessions = []; data.quota = null;

describe('显示层纯函数', () => {
  it('null / undefined → 未知，真实的 0 仍然是 0', () => {
    expect(num(null)).toBe('未知');
    expect(num(undefined)).toBe('未知');
    expect(num(NaN)).toBe('未知');
    expect(num(0)).toBe('0');
    expect(pct(0)).toBe('0%');
    expect(pct(null)).toBe('未知');
  });
  it('置信度 UNKNOWN 时，有值也不显示', () => {
    expect(observed(62.5, 'UNKNOWN', '%')).toBe('未知');
    expect(observed(62.5, 'HIGH', '%')).toBe('62.5%');
  });
  it('枚举不折算：UNKNOWN 与 UNAVAILABLE/STOPPED/FAILED 各自独立', () => {
    expect(statusText('UNKNOWN')).toBe('未知');
    expect(statusText('UNAVAILABLE')).toBe('UNAVAILABLE');
    expect(statusText('STOPPED')).toBe('STOPPED');
    expect(statusText('FAILED')).toBe('FAILED');
    expect(statusText(null)).toBe('未知');
  });
});

describe('额度面板', () => {
  it('读不到（observations 空 + overall UNKNOWN）→ 显示未知，且不出现任何百分比', async () => {
    data.quota = { observations: [], overall_confidence: 'UNKNOWN' };
    const html = await render(QuotaPanel);
    expect(html).toContain('未知');
    expect(cells(html)).toEqual([]);
  });
  it('字段为 null 时该行显示未知而不是 0%', async () => {
    data.quota = {
      observations: [observation({ used_percent: null, remaining_percent: null, confidence: 'UNKNOWN' })],
      overall_confidence: 'UNKNOWN',
    };
    expect(cells(await render(QuotaPanel))).toEqual(['weekly', '未知', '未知', '未知', 'OBSERVED', '未知']);
  });
  it('真实观测到的 0% 必须照常显示为 0%', async () => {
    data.quota = {
      observations: [observation({ used_percent: 0, remaining_percent: 100 })],
      overall_confidence: 'HIGH',
    };
    expect(cells(await render(QuotaPanel))).toEqual(['weekly', '0%', '100%', '未知', 'OBSERVED', 'HIGH']);
  });
});

describe('状态列不猜测', () => {
  it('Task UNKNOWN 显示未知，不是 FAILED', async () => {
    data.tasks = [task({ status: 'UNKNOWN' })];
    const c = cells(await render(TaskPanel));
    expect(c).toContain('未知');
    expect(c.join('|')).not.toMatch(/FAILED|Offline/);
  });
  it('Task INTERRUPTED 原样显示，不被折成 FAILED/CANCELLED', async () => {
    data.tasks = [task({ status: 'INTERRUPTED' })];
    expect(cells(await render(TaskPanel))).toContain('INTERRUPTED');
  });
  it('Session sync_state UNKNOWN 显示未知，不是 Offline/不可用', async () => {
    data.sessions = [session({ sync_state: 'UNKNOWN', state_confidence: 'UNKNOWN' })];
    const c = cells(await render(SessionPanel));
    expect(c).toContain('未知');
    expect(c.join('|')).not.toMatch(/Offline|离线|UNAVAILABLE|FAILED/i);
  });
  it('Adapter health UNKNOWN 显示未知，不是 STOPPED/UNAVAILABLE', async () => {
    data.health = { state: 'READY', ready: true };
    data.adapters = [adapter({ health: 'UNKNOWN' })];
    const html = await render(StatusHeader);
    expect(html).toContain('未知');
    expect(html).not.toMatch(/STOPPED|UNAVAILABLE|Offline/i);
  });
});
