<script setup lang="ts">
import { data } from '../stores/panel';
import { statusText, when } from '@lucero/api-client/unknown';
</script>

<template>
  <section class="card">
    <h2>会话 <code>GET /sessions</code></h2>
    <p v-if="!data.sessions.length" class="dim">未知 —— 列表为空或未连通。</p>
    <table v-else>
      <thead><tr><th>title</th><th>managed</th><th>sync_state</th><th>confidence</th><th>cursor</th><th>最后同步</th></tr></thead>
      <tbody>
        <tr v-for="s in data.sessions" :key="s.id">
          <td>{{ s.title ?? '—' }}</td>
          <td>{{ s.managed ? '受管理' : '未纳管' }}</td>
          <td :class="{ unknown: s.sync_state === 'UNKNOWN' }">{{ statusText(s.sync_state) }}</td>
          <td>{{ statusText(s.state_confidence) }}</td>
          <td><code>{{ s.sync_cursor ?? '—' }}</code></td>
          <td>{{ when(s.last_synced_at) }}</td>
        </tr>
      </tbody>
    </table>
    <p class="dim">UNKNOWN ≠ Offline：同步状态取不到时只说"未知"，不猜"离线"。</p>
  </section>
</template>

<style scoped>
.unknown { color: var(--warn); font-weight: 600; }
</style>
