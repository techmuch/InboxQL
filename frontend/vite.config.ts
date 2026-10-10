/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
  ],
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