import { defineConfig } from 'vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import tsConfigPaths from 'vite-tsconfig-paths'

export default defineConfig({
  // In development Vite serves the app and the Go server does the rest, so the
  // two halves are reachable on one origin — which is what the server's origin
  // checks require.
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:7777', changeOrigin: false },
    },
  },
  plugins: [
    tsConfigPaths({ projects: ['./tsconfig.json'] }),
    tailwindcss(),
    // Pure SPA: no server rendering at request time. The build prerenders only
    // the shell; every route renders on the client and reads static snapshot
    // JSON from /data. See docs/DECISIONS.md (Q2).
    tanstackStart({ spa: { enabled: true } }),
    viteReact(),
  ],
})
