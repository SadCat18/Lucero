<script setup lang="ts">
import { data } from '../stores/panel';
import { statusText } from '@lucero/api-client/unknown';
</script>

<template>
  <section class="card">
    <h2>任务 <code>GET /tasks</code></h2>
    <p v-if="!data.tasks.length" class="dim">未知 —— 列表为空或未连通。</p>
    <table v-else>
      <thead><tr><th>title</th><th>status</th><th>priority</th><th>risk</th><th>ver</th></tr></thead>
      <tbody>
        <tr v-for="t in data.tasks" :key="t.id">
          <td>{{ t.title }}</td>
          <td :class="{ unknown: t.status === 'UNKNOWN' }">{{ statusText(t.status) }}</td>
          <td>{{ statusText(t.priority) }}</td>
          <td>{{ statusText(t.risk_level) }}</td>
          <td>{{ t.version }}</td>
        </tr>
      </tbody>
    </table>
    <p class="dim">UNKNOWN 不折算成 FAILED；AGENT_COMPLETED 也不等于 ACCEPTED。</p>
  </section>
</template>

<style scoped>
.unknown { color: var(--warn); font-weight: 600; }
</style>
