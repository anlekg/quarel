import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// The renderer is a plain web app: the same build serves the desktop app
// (loaded from disk by Electron) and, later, the web client.
export default defineConfig({
  plugins: [react()],
  base: './',
  // Assets stay separate files: the page's CSP only allows fonts from itself.
  build: { outDir: 'dist', emptyOutDir: true, assetsInlineLimit: 0 },
  server: { port: 5190 }, // next free port if taken
  test: { environment: 'node', include: ['src/**/*.test.ts', 'electron/**/*.test.ts'] },
})
