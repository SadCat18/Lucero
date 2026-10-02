<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue';
import StatusHeader from './components/StatusHeader.vue';
import ChainRail from './components/ChainRail.vue';
import EventTimeline from './components/EventTimeline.vue';
import ApprovalPanel from './components/ApprovalPanel.vue';
import QuotaPanel from './components/QuotaPanel.vue';
import TaskPanel from './components/TaskPanel.vue';
import SessionPanel from './components/SessionPanel.vue';
import OutputPanel from './components/OutputPanel.vue';
import DetailPanel from './components/DetailPanel.vue';
import { connectEvents, refresh, stream } from './stores/panel';

onMounted(() => {
  refresh().catch(() => {});
  connectEvents();
});
onUnmounted(() => stream.value?.close());
</script>

<template>
  <main class="panel">
    <StatusHeader />
    <ChainRail />
    <div class="grid">
      <TaskPanel />
      <SessionPanel />
      <ApprovalPanel />
      <QuotaPanel />
      <OutputPanel />
      <DetailPanel />
      <EventTimeline />
    </div>
  </main>
</template>

<style>
:root {
  --bg: #12151a; --card: #191d24; --line: #2b323c; --fg: #dfe4ea; --dim: #8b95a3;
  --ok: #4ec9a0; --warn: #e5b567; --bad: #e06c75;
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 14px/1.5 system-ui, "Segoe UI", sans-serif; }
.panel { padding: 14px; display: grid; gap: 12px; }
.grid { display: grid; gap: 12px; grid-template-columns: repeat(auto-fit, minmax(330px, 1fr)); align-items: start; }
.card { background: var(--card); border: 1px solid var(--line); border-radius: 8px; padding: 10px 12px; }
.card h2 { font-size: 13px; margin: 0 0 8px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
table { width: 100%; border-collapse: collapse; }
th, td { text-align: left; padding: 3px 6px; border-bottom: 1px solid var(--line); font-size: 12px; }
th { color: var(--dim); font-weight: 500; }
code { font-family: ui-monospace, Consolas, monospace; font-size: 11px; }
button { background: #232a34; color: var(--fg); border: 1px solid var(--line); border-radius: 5px; padding: 2px 9px; font-size: 12px; cursor: pointer; }
button:disabled { opacity: .5; cursor: default; }
.dim { color: var(--dim); font-weight: 400; }
.bad { color: var(--bad); }
</style>
