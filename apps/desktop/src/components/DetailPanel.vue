<script setup lang="ts">
import { detail } from '../stores/panel';
import { num, statusText, when } from '@lucero/api-client/unknown';

/** 值只按"契约给了什么"显示：*_at 走 when()、数字走 num()（0 是真值，null 才是未知）、字符串走 statusText()、嵌套对象原样给 JSON */
function cell(k: string, v: unknown): string {
  if (v === null || v === undefined) return k.endsWith('_at') ? when(null) : '—';
  if (typeof v === 'number') return k.endsWith('_at') ? when(v) : num(v);
  if (typeof v === 'string') return statusText(v);
  return JSON.stringify(v);
}
const keys = () => (detail.row ? Object.keys(detail.row).sort() : []);
</script>

<template>
  <section class="card">
    <h2>详情 <code>{{ detail.endpoint || 'GET /api/v1/{entity}/{id}' }}</code></h2>
    <p v-if="!detail.kind" class="dim">还没读任何一行 —— 点列表里的「详情」才会发那一次 by-id GET。</p>
    <p v-else-if="detail.loading" class="dim">读取中…</p>
    <template v-else-if="detail.row">
      <p class="dim">来源 <code>{{ detail.endpoint }}</code> · id <code>{{ detail.id }}</code>（一行数据 = 一次 by-id 读，面板不自己拼）</p>
      <table>
        <tbody>
          <tr v-for="k in keys()" :key="k">
            <th>{{ k }}</th>
            <td><code>{{ cell(k, (detail.row as Record<string, unknown>)[k]) }}</code></td>
          </tr>
        </tbody>
      </table>
    </template>
    <p v-else class="bad">读不到：<code>{{ detail.error }}</code> —— 这一格留空，不拿空对象或 0 冒充有值。</p>
  </section>
</template>

<style scoped>
th { text-align: left; color: var(--dim); font-weight: 400; padding-right: 8px; white-space: nowrap; }
.bad { color: var(--bad); }
</style>
