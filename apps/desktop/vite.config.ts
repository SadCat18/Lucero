/// <reference types="vitest/config" />
import { fileURLToPath } from 'node:url';
import vue from '@vitejs/plugin-vue';
import { defineConfig, loadEnv, type ProxyOptions } from 'vite';

const here = fileURLToPath(new URL('.', import.meta.url));
const apiClient = fileURLToPath(new URL('../../packages/api-client/src', import.meta.url));

// LUCERO_CORE_ORIGIN = dev 反代的转发目标；LUCERO_CORE_TOKEN = 注入用的口令。
// 契约③ §1 第 2.2 项（REQ-F-18）：dev 期的 Authorization 由这里注入，面板代码与构建产物都不含口令
// （只从进程环境读，不写 .env、不进 import.meta.env；`npm run check:no-secret` 是负向断言）。
// WS 半边不经此注入：仍按 §2.1 用一次性短票，代理只做转发。
const injectAuth: NonNullable<ProxyOptions['configure']> = (proxy) => {
  const token = process.env.LUCERO_CORE_TOKEN ?? '';
  proxy.on('proxyReq', (proxyReq) => {
    if (token) proxyReq.setHeader('authorization', `Bearer ${token}`);
  });
};

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, here, '');
  const target = env.LUCERO_CORE_ORIGIN ?? process.env.LUCERO_CORE_ORIGIN ?? 'http://127.0.0.1:17323';
  return {
    plugins: [vue()],
    resolve: { alias: { '@lucero/api-client': apiClient } },
    server: {
      port: 5173,
      proxy: {
        '/api': { target, changeOrigin: true, configure: injectAuth },
        '/ws': { target, ws: true, changeOrigin: true },
      },
    },
    test: { environment: 'node', include: ['src/**/*.test.ts'] },
  };
});
