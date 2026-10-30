/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { readFileSync } from 'node:fs'

// The release version, from the one file it is written in. package.json
// cannot hold it: npm takes three numbers and a release is four
// (year.month.day.n).
const appVersion = readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim()

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
  ],
  define: {
    __APP_VERSION__: JSON.stringify(appVersion),
  },
  server: {
    proxy: {
      '/api': {
        // Where `iql start` listens by default. INBOXQL_ADDR points the
        // proxy elsewhere, matching the server's own override.
        target: `http://${process.env.INBOXQL_ADDR ?? 'localhost:8420'}`,
        changeOrigin: true,
        secure: false,
      }
    }
  },
  optimizeDeps: {
    exclude: ['nexus-shell'],
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/setupTests.ts'],
    css: true,
    server: {
      deps: {
        inline: ['nexus-shell', 'flexlayout-react'],
      },
    },
  }
})