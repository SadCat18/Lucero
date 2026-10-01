/**
 * T-432 ②：审批面的**只验渲染**用例。
 *
 * 主控裁语写死在这里，免得读者以为链通了：**这一格不验 `POST /approvals/{id}:resolve` 那个写口。**
 * 写口的证据只能在真后台那条链上给（`live-panel-chain.test.ts`），而"真待审行"要等 backend 的 T-430 落地
 * （14:5x 与本轮 15:2x 两次实测：非测试码里 `approval.Service.Request` 调用点都是 0）。
 * 这里喂的是行数据本身、不是服务端响应 ⇒ 断的只有一件事：`ApprovalPanel.vue` 的模板会不会替 Core 猜（§12 P0）。
 * 另断一件"我没顺手碰写口"：整个用例期间 `fetch` 一次都没被调用。
 */
import { describe, expect, it, beforeAll, afterAll } from 'vitest';
import { createSSRApp } from 'vue';
import { renderToString } from 'vue/server-renderer';
import ApprovalPanel from '../components/ApprovalPanel.vue';
import { data } from '../stores/panel';
import type { Schemas } from '@lucero/api-client/client';

type Approval = Schemas['Approval'];

const row = (over: Partial<Approval> = {}): Approval => ({
  id: 'apr-render-full', approval_type: 'tool', action: 'write_file', state: 'PENDING',
  risk_level: 'HIGH', requested_at: 1_700_000_000_000, resource: 'src/a.ts', expires_at: 1_700_000_060_000,
  ...over,
});

const render = async () => await renderToString(createSSRApp(ApprovalPanel));

describe('ApprovalPanel 渲染（只验渲染，不验 :resolve 写口）', () => {
  const saved = data.approvals;
  let fetchCalls: string[] = [];
  const realFetch = globalThis.fetch;

  beforeAll(() => {
    // 只为"我没碰写口"这一条断言服务：任何一次 fetch 都会被记下来
    globalThis.fetch = ((input: RequestInfo | URL) => {
      fetchCalls.push(String(input));
      return Promise.reject(new Error('本用例不许发请求'));
    }) as typeof fetch;
  });
  afterAll(() => {
    globalThis.fetch = realFetch;
    data.approvals = saved;
  });

  it('有行就逐行渲染；null / UNKNOWN 一律显示"未知"或"—"，不许折成 LOW / 空页 / 确定时间', async () => {
    data.approvals = [
      row(),
      row({ id: 'apr-render-unknown', resource: null, risk_level: 'UNKNOWN', expires_at: null }),
      row({ id: 'apr-render-critical', risk_level: 'CRITICAL' }),
    ];
    const html = await render();

    expect(html).toContain('write_file');
    expect(html).toContain('src/a.ts');
    expect(html).toContain('</td>');
    // 那一行的 resource 是 null ⇒ 显示破折号，不显示空串也不显示"null"
    expect(html).toContain('—');
    expect(html).not.toMatch(/>null</);
    // 两条"未知"：risk_level 的 UNKNOWN 一格 + expires_at 的 null 一格（第三行 CRITICAL 走的是原样显示）
    expect((html.match(/未知/g) ?? []).length, `未知 的格数不对：${html}`).toBe(2);
    expect(html).not.toMatch(/>UNKNOWN</);           // 枚举原词不许直接吐给观众
    expect(html).not.toContain('LOW');               // 不许把 UNKNOWN 折成最低风险
    expect(html).not.toMatch(/Offline|离线/i);
    expect(html).toContain('CRITICAL');
    expect(html).toMatch(/class="[^"]*bad[^"]*"[^>]*>CRITICAL</);   // 只有 CRITICAL 该带高亮
    expect(html).not.toMatch(/class="[^"]*bad[^"]*"[^>]*>HIGH</);
    // 写口的按钮该在（渲染），但本用例一次都不点它
    expect(html).toContain('批准');
    expect(html).toContain('拒绝');
    expect(fetchCalls, `只验渲染的用例发了请求：${fetchCalls.join(' | ')}`).toHaveLength(0);
  });

  it('0 行渲染成明写的"当前无待决审批"，不是空表、也不是"未知"', async () => {
    data.approvals = [];
    const html = await render();
    expect(html).toContain('当前无待决审批');
    expect(html).not.toContain('<table');
    expect(html).not.toContain('未知');
    expect((html.match(/apr-/g) ?? []).length, '面板没拿到过任何行，渲染里却冒出审批 id = 编的').toBe(0);
    expect(fetchCalls, '空清单那一支也不许发请求').toHaveLength(0);
  });
});
