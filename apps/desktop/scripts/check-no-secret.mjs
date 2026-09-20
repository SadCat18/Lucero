// 负向断言（契约③ §1 第 2.2 项 / T-53）：构建产物里不得出现凭据通路。
// 用法：cd apps/desktop && npm run check:no-secret
// 带口令真值跑法：LUCERO_CORE_TOKEN=… npm run check:no-secret —— 那样会连具体 token 一起扫。
import { readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../dist');
// 注意不含 x-lucero-principal：§1.4 要求写操作由客户端自己带 local-user，它不是秘密，进产物是对的。
const FORBIDDEN = ['Bearer', 'core_token', 'LUCERO_CORE_TOKEN'];
const secret = process.env.LUCERO_CORE_TOKEN ?? '';
if (secret) FORBIDDEN.push(secret);
else console.log('  note  LUCERO_CORE_TOKEN 未提供 → 跳过"口令真值"这一项，其余四项照扫');

const walk = (d) => readdirSync(d).flatMap((f) => {
  const p = path.join(d, f);
  return statSync(p).isDirectory() ? walk(p) : [p];
});

let hits = 0;
const files = walk(dist);
for (const f of files) {
  const text = readFileSync(f, 'utf8');
  for (const needle of FORBIDDEN) {
    const at = text.indexOf(needle);
    if (at >= 0) {
      hits++;
      console.log(`  FAIL ${path.relative(dist, f)} 含 "${needle === secret ? '口令真值' : needle}"` +
        ` @${at}：${JSON.stringify(text.slice(Math.max(0, at - 30), at + 50))}`);
    }
  }
}
console.log(hits
  ? `\n${hits} 处命中：凭据或凭据通路进了构建产物（§1 第 2.2 项禁止）`
  : `\n全部通过：${files.length} 个产物文件里扫不到 ${FORBIDDEN.length} 项禁用串`);
process.exit(hits ? 1 : 0);
