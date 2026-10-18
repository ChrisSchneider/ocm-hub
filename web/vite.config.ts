/// <reference types="vitest/config" />
import { writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The Go server embeds this directory (internal/webui).
const outDir = fileURLToPath(new URL('../internal/webui/dist', import.meta.url))

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      // emptyOutDir removes dist/.keep, which go:embed needs when there is no build.
      name: 'keep-go-embed-dir',
      apply: 'build',
      closeBundle() {
        writeFileSync(`${outDir}/.keep`, '')
      },
    },
  ],
  build: { outDir, emptyOutDir: true },
  server: { proxy: { '/v1': 'http://localhost:8080', '/healthz': 'http://localhost:8080' } },
  test: { environment: 'jsdom', globals: true, setupFiles: ['./src/test/setup.ts'] },
})
