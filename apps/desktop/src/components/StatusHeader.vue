<script setup lang="ts">
import { computed } from 'vue';
import { link, data } from '../stores/panel';
import { statusText } from '@lucero/api-client/unknown';

const dot = computed(() => ({
  live: 'ok', connecting: 'wait', reconnecting: 'wait', stalled: 'bad', closed: 'bad',
}[link.state] ?? 'bad'));
</script>

<template>
  <header class="bar">
    <span class="dot" :class="dot"></span>
    <strong>{{ link.state }}</strong>
    <span class="dim">{{ link.detail }}</span>
    <span>游标 seq=<code>{{ link.seq ?? '未知' }}</code></span>
    <span>已见事件 <code>{{ data.events.length }}</code></span>
    <span>Core <code>{{ statusText(data.health?.state) }}</code>
      <template v-if="data.health?.version"> v{{ data.health.version }}</template>
      / mig {{ data.health?.migration_version ?? statusText(null) }}</span>
    <span v-for="a in data.adapters" :key="a.id">
      {{ a.adapter_key }}
      <code :class="{ unknown: a.health === 'UNKNOWN' }">{{ statusText(a.health) }}</code>
    </span>
    <span v-if="data.health && !data.health.ready" class="bad">Core 未 Ready</span>
    <span v-if="link.resync" class="bad">resync_required：状态来自 REST 快照，时间线有缺口</span>
    <span v-if="!data.health" class="bad">
      Core API 未连通（core/api/** 尚未落地）<template v-if="data.bootError"> · {{ data.bootError }}</template>
    </span>
  </header>
</template>

<style scoped>
.bar { display: flex; gap: 14px; align-items: center; flex-wrap: wrap; font-size: 13px; }
.dim { color: var(--dim); }
.unknown { color: var(--warn); font-weight: 600; }
.dot { width: 9px; height: 9px; border-radius: 50%; display: inline-block; }
.dot.ok { background: var(--ok); }
.dot.wait { background: var(--warn); }
.dot.bad { background: var(--bad); }
</style>
