<script setup lang="ts">
import { data, openDetail, resolveApproval, writeBlocked } from '../stores/panel';
import { statusText, when } from '@lucero/api-client/unknown';

async function decide(id: string, state: 'APPROVED' | 'REJECTED') {
  if (writeBlocked()) return;      // 只读档不出现可点再报错的通路（T-449 ①）
  await resolveApproval(id, state);
}
</script>

<template>
  <section class="card">
    <h2>待决审批 <code>GET /approvals?state=PENDING</code></h2>
    <p v-if="!data.approvals.length" class="dim">当前无待决审批。</p>
    <table v-else>
      <thead><tr><th>action</th><th>resource</th><th>risk</th><th>requested</th><th>过期</th><th></th></tr></thead>
      <tbody>
        <tr v-for="a in data.approvals" :key="a.id">
          <td><code>{{ a.action }}</code></td>
          <td>{{ a.resource ?? '—' }}</td>
          <td :class="{ bad: a.risk_level === 'CRITICAL' }">{{ statusText(a.risk_level) }}</td>
          <td>{{ when(a.requested_at) }}</td>
          <td>{{ when(a.expires_at) }}</td>
          <td class="row-actions">
            <button @click="openDetail('approval', a.id)">详情</button>
            <span v-if="writeBlocked()" class="dim">该 Adapter 为只读 —— 不发决议</span>
            <template v-else>
              <button @click="decide(a.id, 'APPROVED')">批准</button>
              <button @click="decide(a.id, 'REJECTED')">拒绝</button>
            </template>
          </td>
        </tr>
      </tbody>
    </table>
    <p class="dim">面板只发决议；批不批、给什么 grant 由 Core 的 Approval / Permission Engine 裁决。</p>
  </section>
</template>

<style scoped>
.row-actions { display: flex; gap: 6px; }
</style>
