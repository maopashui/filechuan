import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/ws': { target: 'ws://127.0.0.1:8080', ws: true },
      '/healthz': 'http://127.0.0.1:8080',
    },
  },
  build: {
    // 避免把路由懒加载 chunk 写进首页 modulepreload（否则仍会随首页下载）
    modulePreload: {
      resolveDependencies: (_filename, deps) =>
        deps.filter(
          (dep) =>
            !/(Admin|Login|Setup|Pickup|Lan|qrcode|dayjs|upload|form-|input-|message-|alert-|useForm-)/.test(
              dep,
            ),
        ),
    },
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/dayjs')) return 'dayjs'
          if (id.includes('node_modules/qrcode.react')) return 'qrcode'
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    globals: true,
  },
})

