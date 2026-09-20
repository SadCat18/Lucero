/// <reference types="vitest/config" />
import { fileURLToPath } from 'node:url';
import vue from '@vitejs/plugin-vue';
import { defineConfig, loadEnv } from 'vite';

const here = fileURLToPath(new URL('.', import.meta.url));
const apiClient = fileURLToPath(new URL('../../packages/api-client/src', import.meta.url));

// LUCERO_CORE_ORIGIN 只决定 dev proxy 的转发目标。鉴权怎么带到浏览器 + WS 仍待主控裁定
// （REQ-F-2 / REQ-F-3），所以这里不注入 token，也不把它写进前端产物。
export default defineConfig(({ mode }) => {
  const target = loadEnv(mode, here, '').LUCERO_CORE_ORIGIN ?? 'http://127.0.0.1:17323';
  return {
    plugins: [vue()],
    resolve: { alias: { '@lucero/api-client': apiClient } },
    server: {
      port: 5173,
      proxy: {
        '/api': { target, changeOrigin: true },
        '/ws': { target, ws: true, changeOrigin: true },
      },
    },
    test: { environment: 'node', include: ['src/**/*.test.ts'] },
  };
});
