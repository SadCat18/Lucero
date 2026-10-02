/**
 * T-449 ①②：只读档不得看起来可写 + 额度三档要三种长相。
 * 全部离线（SSR 渲染 + 纯函数分类）—— **不碰任何写口**，也不主张链通了：
 * 写链的证据在 `live-panel-chain.test.ts`（真 Core + 真 WS）里，那一格该等谁我在这儿不代答。
 */
import { describe, expect, it, afterAll } from 'vitest';
import { createSSRApp } from 'vue';
import { renderToString } from 'vue/server-renderer';
import ChainRail from '../components/ChainRail.vue';
import ApprovalPanel from '../components/ApprovalPanel.vue';
import QuotaPanel from '../components/QuotaPanel.vue';
import { data, writeBlocked, writability } from '../stores/panel';
import { quotaTier } from '@lucero/api-client/unknown';
import type { Schemas } from '@lucero/api-client/client';

type Cap = Schemas['AdapterCapability'];
type Obs = Schemas['QuotaObservation'];

const cap = (capability_name: string, support_level: Cap['support_level']): Cap =>
  ({ id: `cap-${capability_name}`, adapter_instance_id: 'ai-1', capability_name, support_level, health: 'HEALTHY' });

const obs = (over: Partial<Obs> = {}): Obs => ({
  quota_key: 'tokens', quota_type: 'REQUESTS', observation_kind: 'OBSERVED', confidence: 'HIGH',
  used_percent: 40, remaining_percent: 60, used_value: 4000, remaining_value: 6000, ...over,
});

const saved = { caps: data.caps, capsError: data.capsError, quota: data.quota, approvals: data.approvals };
const render = async (c: object) => await renderToString(createSSRApp(c as never));

afterAll(() => {
  data.caps = saved.caps; data.capsError = saved.capsError; data.quota = saved.quota; data.approvals = saved.approvals;
});

describe('额度三档（T-449 ②）：>0 高置信 / =0 低置信 / 读不到 —— 三种长相，不许折叠', () => {
  it('分类是纯函数四问：0+HIGH 才是"用完"，0+低置信是"不敢说"，null 与 UNKNOWN 都是"读不到"', () => {
    expect(quotaTier(40, 'HIGH')).toBe('value');
    expect(quotaTier(0, 'HIGH'), '0 且高置信 = 真用完，必须与低置信那一档分开').toBe('value');
    expect(quotaTier(0, 'LOW'), '0 且低置信不许读成"用完"或"免费"').toBe('zero');
    expect(quotaTier(0, 'MEDIUM')).toBe('zero');
    expect(quotaTier(null, 'HIGH')).toBe('absent');
    expect(quotaTier(0, 'UNKNOWN'), 'UNKNOWN 置信时连 0 都不该产出数字').toBe('absent');
    expect(quotaTier(Number.NaN, 'HIGH')).toBe('absent');
  });

  it('渲染出来是三副面孔：低置信那一格带明确 hedge，读不到那格一个数字都不出', async () => {
    data.quota = {
      overall_confidence: 'MEDIUM',
      observations: [
        obs(),
        obs({ quota_key: 'soft-limit', confidence: 'LOW', used_percent: 0, remaining_percent: 0, used_value: 0, remaining_value: 0 }),
        obs({ quota_key: 'hard', confidence: 'UNKNOWN', used_percent: null, remaining_percent: null, used_value: null, remaining_value: null }),
      ],
    } as never;
    const html = await render(QuotaPanel);
    expect(html).toContain('40%');
    expect(html).toContain('低置信');
    expect(html).toContain('用完');
    expect(html).toContain('免费');
    expect((html.match(/未知/g) ?? []).length, `读不到那一格必须显示未知：${html.slice(0, 200)}`).toBeGreaterThanOrEqual(2);
    expect(html).toMatch(/tier-zero/);          // 三种长相各有各的 class，观众一眼能分
    expect(html).toMatch(/tier-absent/);
    expect(html).toMatch(/tier-value/);
    expect(html).not.toMatch(/剩余 0%.*用完/);   // 不许把低置信的 0 拼成"用完"
  });
});

describe('只读档不得看起来可写（T-449 ①）：档位从读面推，不写前端局部规则', () => {
  it('writability 四答互不相混，且"没读到"绝不答 writable', () => {
    data.capsError = {};
    for (const [name, level, want] of [
      ['session.send', 'READ_ONLY', 'read_only'], ['session.send', 'NATIVE', 'writable'],
      ['session.send', 'UNSUPPORTED', 'unsupported'],
    ] as const) {
      data.caps = { 'ai-1': [cap(name, level), cap('session.list', 'NATIVE')] };
      expect(writability('ai-1'), `${name}=${level} 该答 ${want}`).toBe(want);
    }
    data.caps = { 'ai-1': [cap('session.list', 'NATIVE')] };
    expect(writability('ai-1'), '没有写能力那一行 = 档位未知，不许猜成可写也不许猜成只读').toBe('unknown');
    data.caps = {};
    data.capsError = { 'ai-1': '501 not_implemented' };
    expect(writability('ai-1'), '读不到档位时答案必须是 unknown').toBe('unknown');
    expect(writeBlocked(), '档位读不到 ⇒ 不能谎称只读，也不能谎称可写').toBe(false);
  });

  it('读到 READ_ONLY：驱动按钮 disabled 且文案改成"不发写请求"，批准/拒绝整块不出现', async () => {
    data.caps = { 'ai-1': [cap('session.send', 'READ_ONLY'), cap('approval.respond', 'READ_ONLY')] };
    data.capsError = {};
    data.approvals = [{ id: 'apr-1', approval_type: 'tool', action: 'write_file', state: 'PENDING',
      risk_level: 'HIGH', requested_at: 1_700_000_000_000 } as never];
    const rail = await render(ChainRail);
    expect(rail, `按钮没被 disabled：${rail.slice(0, 160)}`).toMatch(/<button[^>]*\bdisabled\b/);
    expect(rail).toContain('该 Adapter 为只读 —— 不发写请求');
    expect(rail).not.toContain('驱动整条链');
    const ap = await render(ApprovalPanel);
    expect(ap).not.toContain('批准');
    expect(ap).not.toContain('拒绝');
    expect(ap).toContain('该 Adapter 为只读 —— 不发决议');
    expect(ap).toContain('详情');                 // 读面照常给
  });

  it('档位读不到（501）：明写"档位读不到"，不谎称可写、也不谎称只读', async () => {
    data.caps = {};
    data.capsError = { 'ai-1': '501 not_implemented' };
    const html = await render(ChainRail);
    expect(html).toContain('档位读不到');
    expect(html).toContain('501 not_implemented');
    expect(html).not.toContain('该 Adapter 为只读');
    expect(html).not.toMatch(/<button[^>]*\bdisabled\b/);  // 没证据就不拦：拦下来就是拿未知当结论
  });

  it('可写档（NATIVE）：控件照常，不出现只读字样', async () => {
    data.caps = { 'ai-1': [cap('session.send', 'NATIVE'), cap('approval.respond', 'NATIVE')] };
    data.capsError = {};
    const html = await render(ChainRail);
    expect(html).toContain('驱动整条链');
    expect(html).not.toContain('该 Adapter 为只读');
    expect(writeBlocked()).toBe(false);
  });
});
