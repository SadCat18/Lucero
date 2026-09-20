<script setup lang="ts">
import { data } from '../stores/panel';
import { observed, statusText, when } from '@lucero/api-client/unknown';

const q = () => data.quota;
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
          <tr v-for="(o, i) in q()!.observations" :key="i">
            <td><code>{{ o.quota_key }}</code></td>
            <td>{{ observed(o.used_percent, o.confidence, '%') }}</td>
            <td>{{ observed(o.remaining_percent, o.confidence, '%') }}</td>
            <td>{{ when(o.reset_at) }}</td>
            <td :class="{ warn: o.observation_kind === 'ESTIMATED' }">{{ o.observation_kind }}</td>
            <td>{{ statusText(o.confidence) }}</td>
          </tr>
        </tbody>
      </table>
    </template>
    <p class="dim">ESTIMATED 是预测值，不得混进 OBSERVED（契约② §7）。</p>
  </section>
</template>

<style scoped>
.unknown { color: var(--warn); font-weight: 600; }
.warn { color: var(--warn); }
</style>
