import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    // Loopback-only for local development: keep the dev server off LAN/VPN
    // interfaces (vite's `host: true` bound to enp8s0 / tailscale0 and exposed
    // the unauthenticated front-end build to the network).
    host: '127.0.0.1',
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
        // Server-Sent Events must stream to the browser. The dev proxy
        // otherwise holds the gateway's response headers, so the client never
        // opens the stream and the Live indicator stays on "Connecting...".
        // Flushing the headers as soon as the gateway answers fixes the root
        // cause without touching the event contract or authentication.
        configure: (proxy: any) => {
          proxy.on('proxyRes', (_proxyRes: any, _req: any, res: any) => {
            if (typeof res.flushHeaders === 'function') {
              res.flushHeaders();
            }
          });
        },
      },
      // Health server (port 8080): canonical liveness.
      '/health': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
      // Gateway (port 8081): /status and /ready are served by the API gateway,
      // NOT the health server. Routing them to 8080 returns 404 in local dev.
      '/status': {
        target: 'http://localhost:8081',
        changeOrigin: true,
        secure: false,
      },
      '/ready': {
        target: 'http://localhost:8081',
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