<script setup lang="ts">
import { CHAIN, chain, data, runChain, stepState, writability } from '../stores/panel';

/** 只读档 / 档位未知的说法（T-449 ①）：读得到就拦，读不到就明说读不到 —— 两种都不许"先可点再报错" */
const levels = () => Object.keys(data.caps).map((id) => [id, writability(id)] as const);
const blocked = () => levels().some(([, w]) => w === 'read_only' || w === 'unsupported');
const unknownOnes = () => levels().filter(([, w]) => w === 'unknown').length + Object.keys(data.capsError).length;
</script>

<template>
  <section class="card">
    <h2>垂直链（事件契约 §7）<button :disabled="chain.running || blocked()" @click="runChain()">
        {{ chain.running ? '执行中…' : blocked() ? '该 Adapter 为只读 —— 不发写请求' : '驱动整条链' }}</button></h2>
    <p v-if="blocked()" class="warn">已读到能力档位：这一家不接写链（§16.7 第 1、2 点）⇒ 控件不出现成可点。</p>
    <p v-else-if="unknownOnes()" class="warn">
      档位读不到（{{ unknownOnes() }} 个实例，{{ Object.values(data.capsError)[0] ?? '未列原因' }}）——
      这里**不声称可写**，点了若被拒会在下面按真实出局显示，不折算成成功。
    </p>
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
