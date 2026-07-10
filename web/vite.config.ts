import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// Vite dev server proxies /api/* to the Go backend on :6201.
// host: true 监听 0.0.0.0，支持局域网访问；strictPort 端口被占直接报错。
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    host: true,
    port: 6200,
    strictPort: true,
    proxy: {
      '/api': {
        target: 'http://localhost:6201',
        changeOrigin: true,
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
  },
})
