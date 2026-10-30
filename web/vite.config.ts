// SPDX-License-Identifier: AGPL-3.0-or-later
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import vuetify from 'vite-plugin-vuetify'

// The Go API runs on :8080 during development (see docs/DEVELOPMENT.md).
const apiTarget = process.env.PC_API_TARGET ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [vue(), vuetify({ autoImport: true })],
  server: {
    proxy: {
      '/api': apiTarget,
      '/healthz': apiTarget,
      '/readyz': apiTarget,
    },
  },
  build: {
    sourcemap: true,
  },
  test: {
    environment: 'jsdom',
    server: { deps: { inline: ['vuetify'] } },
  },
})
