// Development: Vite dev server for the UI, Electron pointed at it.
import { spawn } from 'node:child_process'
import { createServer } from 'vite'

await import('./build-electron.mjs')
const vite = await createServer({ configFile: 'vite.config.ts' })
await vite.listen()
const url = vite.resolvedUrls.local[0]
console.log(`Interface : ${url}`)

const electron = spawn('npx', ['electron', '.'], {
  stdio: 'inherit',
  env: { ...process.env, QUAREL_DEV_URL: url },
})
electron.on('exit', async (code) => {
  await vite.close()
  process.exit(code ?? 0)
})
