<script setup lang="ts">
import { CHAIN, chain, runChain, stepState } from '../stores/panel';
</script>

<template>
  <section class="card">
    <h2>垂直链（契约① §7）<button :disabled="chain.running" @click="runChain()">
        {{ chain.running ? '执行中…' : '驱动整条链' }}</button></h2>
    <ol class="rail">
      <li v-for="(s, i) in CHAIN" :key="s.key" :class="stepState(s, i)">
        <span class="idx">{{ i + 1 }}</span>{{ s.label }}
      </li>
    </ol>
    <table v-if="chain.calls.length" class="calls">
      <thead><tr><th>步骤</th><th>调用</th><th>结果</th></tr></thead>
      <tbody>
        <tr v-for="(c, i) in chain.calls" :key="i" :class="c.outcome">
          <td>{{ c.step }}</td><td><code>{{ c.method }}</code></td>
          <td>{{ c.outcome === 'blocked' ? 'blocked' : c.outcome === 'error' ? 'error' : 'ok' }}
            <span class="dim">{{ c.detail }}</span></td>
        </tr>
      </tbody>
    </table>
    <p class="dim">链的完成判定只看事件是否出现过；状态迁移全在 Core。</p>
  </section>
</template>

<style scoped>
.rail { list-style: none; display: flex; flex-wrap: wrap; gap: 6px; padding: 0; margin: 8px 0; }
.rail li { border: 1px solid var(--line); border-radius: 14px; padding: 3px 10px 3px 4px; font-size: 12px; display: flex; gap: 6px; align-items: center; }
.rail .idx { width: 18px; height: 18px; border-radius: 50%; background: var(--line); display: grid; place-items: center; font-size: 11px; }
.rail li.done { border-color: var(--ok); color: var(--ok); }
.rail li.done .idx { background: var(--ok); color: #000; }
.rail li.current { border-color: var(--warn); }
.calls { width: 100%; font-size: 12px; }
.calls td.blocked, .calls td.error { color: var(--bad); }
</style>
