<script setup lang="ts">
import { computed } from 'vue';
import { data, link } from '../stores/panel';
import { when } from '@lucero/api-client/unknown';

const rows = computed(() => [...data.events].reverse());
</script>

<template>
  <section class="card">
    <h2>事件时间线（/ws/v1/events）<span class="dim" v-if="link.resync">缺口已标注</span></h2>
    <p v-if="!rows.length" class="dim">未知 —— 尚未收到任何事件帧（连接未就绪或 Core 无事件）。</p>
    <table v-else>
      <thead><tr><th>seq</th><th>时间</th><th>event_type</th><th>聚合</th><th>ver</th></tr></thead>
      <tbody>
        <tr v-for="e in rows" :key="e.seq">
          <td>{{ e.seq }}</td>
          <td>{{ when(e.created_at) }}</td>
          <td><code>{{ e.event_type }}</code></td>
          <td>{{ e.aggregate_type }}/{{ e.aggregate_id.slice(0, 8) }}</td>
          <td>{{ e.aggregate_version }}</td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

<style scoped>
table { font-size: 12px; }
td:nth-child(4) { color: var(--dim); }
</style>
