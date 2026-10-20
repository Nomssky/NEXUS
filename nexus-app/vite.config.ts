import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    host: true,
    proxy: {
      // Gateway (port 8081): all /api/v1/* routes and SSE
      '/api': {
        target: 'http://localhost:8081',
        changeOrigin: true,
        secure: false,
      },
      '/events': {
        target: 'http://localhost:8081',
        changeOrigin: true,
        secure: false,
      },
      // Health server (port 8080): /health, /status, /ready
      '/health': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
      '/status': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
      '/ready': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
  },
  resolve: {
    alias: {
      '@': '/src',
    },
  },
});