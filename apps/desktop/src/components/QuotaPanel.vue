<script setup lang="ts">
import { data } from '../stores/panel';
import { quotaTier, statusText, when, UNKNOWN } from '@lucero/api-client/unknown';
import type { Schemas } from '@lucero/api-client/client';

type Obs = Schemas['QuotaObservation'];

const q = () => data.quota;
/** 单元格只出纯文本：三档的差别靠**文字 + 行 class + 那行注解**表达，不用嵌套模板 —— 嵌套会让读表格的判据读成空串（10-02 实测） */
const cell = (v: number | null | undefined, confidence: string | null | undefined, unit: string): string => {
  const tier = quotaTier(v, confidence);
  if (tier === 'absent') return UNKNOWN;
  if (tier === 'zero') return `0${unit}（低置信）`;
  return `${v}${unit}`;
};
const rowTier = (o: Obs) => quotaTier(o.used_percent, o.confidence);
const hasZeroTier = () => (q()?.observations ?? []).some((o) => rowTier(o) === 'zero');
</script>

<template>
  <section class="card">
    <h2>额度 <code>GET /quota</code></h2>
    <p v-if="!q()" class="unknown">未知 —— 尚未取到额度观测。</p>
    <template v-else>
      <p :class="{ unknown: q()!.overall_confidence === 'UNKNOWN' }">
        总体置信度 <code>{{ statusText(q()!.overall_confidence) }}</code>
      </p>
      <p v-if="!q()!.observations.length" class="unknown">
        未知 —— <code>observations: []</code> 且 <code>overall_confidence: UNKNOWN</code>。
        读不到不等于"用完了"，这里不显示任何数字。
      </p>
      <table v-else>
        <thead><tr><th>quota_key</th><th>已用</th><th>剩余</th><th>重置</th><th>kind</th><th>confidence</th></tr></thead>
        <tbody>
          <tr v-for="(o, i) in q()!.observations" :key="i" :class="'tier-' + rowTier(o)">
            <td><code>{{ o.quota_key }}</code></td>
            <td>{{ cell(o.used_percent, o.confidence, '%') }}</td>
            <td>{{ cell(o.remaining_percent, o.confidence, '%') }}</td>
            <td>{{ when(o.reset_at) }}</td>
            <td :class="{ warn: o.observation_kind === 'ESTIMATED' }">{{ o.observation_kind }}</td>
            <td>{{ statusText(o.confidence) }}</td>
          </tr>
        </tbody>
      </table>
    </template>
    <p v-if="hasZeroTier()" class="hedge">
      带「（低置信）」的那些 0 **既不能读成"用完"，也不能读成"免费"**：那是置信度不够，不是数值本身。
    </p>
    <p class="dim">ESTIMATED 是预测值，不得混进 OBSERVED（契约② §7）。三档长相：有数 / 0 且低置信（带注解、行变色）/ {{ UNKNOWN }}（读不到不产出数字）。</p>
  </section>
</template>

<style scoped>
.unknown { color: var(--warn); font-weight: 600; }
.warn { color: var(--warn); }
.hedge { color: var(--warn); font-size: 0.9em; }
tr.tier-zero { background: rgba(229, 181, 103, 0.12); }
</style>
