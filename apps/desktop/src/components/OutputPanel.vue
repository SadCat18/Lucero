<script setup lang="ts">
import { computed } from 'vue';
import { data } from '../stores/panel';

const rows = computed(() => data.output.slice(-200));
</script>

<template>
  <section class="card">
    <h2>输出 <code>GET /executions/{id}/output</code></h2>
    <p v-if="!rows.length" class="dim">未知 —— 还没收到 <code>execution_output_flushed</code>。
      delta 不进 event_journal，只能靠 chunk 表补拉（契约② §6.3）。</p>
    <pre v-else class="stream"><span v-for="c in rows" :key="c.execution_id + ':' + c.sequence_no"
      ><span class="ch">{{ c.channel }}</span>{{ c.content }}</span
      ></pre>
  </section>
</template>

<style scoped>
.stream { max-height: 220px; overflow: auto; white-space: pre-wrap; font-size: 12px; margin: 0; }
.ch { color: var(--dim); margin-right: 4px; }
</style>
