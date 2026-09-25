import { defineConfig } from '@playwright/test'

// Drives the real desktop app (Electron) against a real Identity service.
export default defineConfig({
  testDir: 'e2e',
  timeout: 120_000,
  workers: 1,
  expect: { timeout: 10_000 }, // first steps (password hashing, app start) are slow on a loaded machine
  reporter: [['list']],
  projects: [
    // The desktop app (Electron): e2e/*.spec.ts.
    { name: 'desktop', testIgnore: /web\// },
    // The web client in Chromium, served like app.quarel.app (built UI, SPA fallback).
    // Community servers in tests have a self-signed certificate: accepted here as if it came from an authority.
    { name: 'web', testDir: 'e2e/web', use: { browserName: 'chromium', baseURL: 'http://localhost:19700', ignoreHTTPSErrors: true } },
  ],
  webServer: { command: 'npx vite preview --port 19700 --strictPort', port: 19700, reuseExistingServer: true },
})
