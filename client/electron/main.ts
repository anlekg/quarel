// Electron main process: one window running the web UI, with the renderer
// sandboxed (no Node access). The only bridge is the small API in preload.ts.
import { app, BrowserWindow, ipcMain, safeStorage, shell, session } from 'electron'
import { hostname } from 'node:os'
import { join } from 'node:path'
import { readFile, writeFile, rename, mkdir } from 'node:fs/promises'

const devURL = process.env.QUAREL_DEV_URL
if (process.env.QUAREL_USER_DATA) app.setPath('userData', process.env.QUAREL_USER_DATA)

let win: BrowserWindow | null = null

function createWindow() {
  win = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 940,
    minHeight: 600,
    backgroundColor: '#111317',
    title: 'Quarel',
    autoHideMenuBar: true,
    webPreferences: {
      preload: join(__dirname, 'preload.cjs'),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      spellcheck: true,
    },
  })
  win.webContents.setWindowOpenHandler(({ url }) => {
    openExternal(url)
    return { action: 'deny' }
  })
  win.webContents.on('will-navigate', (e, url) => {
    if (!isAppURL(url)) {
      e.preventDefault()
      openExternal(url)
    }
  })
  if (devURL) win.loadURL(devURL)
  else win.loadFile(join(__dirname, '..', 'dist', 'index.html'))
}

function isAppURL(url: string) {
  return devURL ? url.startsWith(devURL) : url.startsWith('file://')
}

function openExternal(url: string) {
  if (/^https?:\/\//i.test(url)) shell.openExternal(url)
}

// --- secrets: encrypted with the OS keychain (safeStorage), kept in userData ---

const secretsFile = () => join(app.getPath('userData'), 'secrets.json')
let secrets: Record<string, string> | null = null

async function loadSecrets() {
  if (secrets) return secrets
  try {
    secrets = JSON.parse(await readFile(secretsFile(), 'utf8'))
  } catch {
    secrets = {}
  }
  return secrets!
}

async function saveSecrets() {
  await mkdir(app.getPath('userData'), { recursive: true })
  const tmp = secretsFile() + '.tmp'
  await writeFile(tmp, JSON.stringify(secrets), { mode: 0o600 })
  await rename(tmp, secretsFile())
}

function checkKey(key: unknown): string {
  if (typeof key !== 'string' || !/^[a-z0-9:._-]{1,128}$/i.test(key)) throw new Error('invalid secret key')
  return key
}

function fromApp(e: Electron.IpcMainInvokeEvent) {
  const url = e.senderFrame?.url ?? ''
  if (!isAppURL(url)) throw new Error('IPC from an unexpected page')
}

ipcMain.handle('secrets:get', async (e, key) => {
  fromApp(e)
  const all = await loadSecrets()
  const v = all[checkKey(key)]
  if (v === undefined) return null
  return safeStorage.decryptString(Buffer.from(v, 'base64'))
})

ipcMain.handle('secrets:set', async (e, key, value) => {
  fromApp(e)
  if (typeof value !== 'string') throw new Error('secret must be a string')
  const all = await loadSecrets()
  all[checkKey(key)] = safeStorage.encryptString(value).toString('base64')
  await saveSecrets()
})

ipcMain.handle('secrets:delete', async (e, key) => {
  fromApp(e)
  const all = await loadSecrets()
  delete all[checkKey(key)]
  await saveSecrets()
})

ipcMain.handle('app:info', (e) => {
  fromApp(e)
  return {
    platform: process.platform,
    version: app.getVersion(),
    deviceName: hostname(),
    // "basic_text" on Linux without a keychain: secrets are then only obfuscated.
    secureStorage: process.platform !== 'linux' ||
      !['basic_text', 'unknown'].includes(safeStorage.getSelectedStorageBackend()),
  }
})

app.whenReady().then(() => {
  // Linux without a keychain (no Secret Service / KWallet): secrets are then
  // only obfuscated on disk; the UI is told through info().secureStorage.
  if (process.platform === 'linux' && !safeStorage.isEncryptionAvailable()) {
    safeStorage.setUsePlainTextEncryption(true)
  }
  // Microphone, camera and notifications are needed later (voice, calls).
  const allowed = new Set(['media', 'notifications', 'clipboard-sanitized-write', 'fullscreen'])
  session.defaultSession.setPermissionRequestHandler((_wc, permission, cb) => cb(allowed.has(permission)))
  createWindow()
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})
