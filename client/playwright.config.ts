import { defineConfig } from '@playwright/test'

// Drives the real desktop app (Electron) against a real Identity service.
export default defineConfig({
  testDir: 'e2e',
  timeout: 120_000,
  workers: 1,
  reporter: [['list']],
})
