// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// VITE_API_PROXY_TARGET=http://localhost:8080 forwards /api → that host so
// real-backend requests are same-origin in dev. Leave unset to skip the
// proxy entirely (mock-only dev needs no proxy).
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const proxyTarget = env.VITE_API_PROXY_TARGET
  return {
    plugins: [vue()],
    resolve: {
      alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
    },
    server: {
      port: 5173,
      proxy: proxyTarget
        ? {
            '/api': {
              target: proxyTarget,
              changeOrigin: true,
              ws: true,
              rewrite: (path) => path.replace(/^\/api/, '/v1'),
            },
          }
        : undefined,
    },
  }
})
