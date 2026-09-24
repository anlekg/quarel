// Electron main process: one window running the web UI, with the renderer
// sandboxed (no Node access). The only bridge is the small API in preload.ts.
import { app, BrowserWindow, desktopCapturer, ipcMain, safeStorage, shell, session } from 'electron'
import { hostname } from 'node:os'
import { join } from 'node:path'
import { readFile, writeFile, rename, mkdir } from 'node:fs/promises'
import { boundServerID, checkServer } from './tlsbind'

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

// --- community servers with a self-signed certificate bound to their identity ---
// The UI pins, per host name, the server IDs it expects (from invite links).
// A certificate that the system does not trust is accepted only if its
// binding proves one of those IDs.

const pinsFile = () => join(app.getPath('userData'), 'server-pins.json')
let pins: Record<string, string[]> = {}

async function loadPins() {
  try {
    pins = JSON.parse(await readFile(pinsFile(), 'utf8'))
  } catch {
    pins = {}
  }
}

ipcMain.handle('tls:pin', async (e, host, sid) => {
  fromApp(e)
  if (typeof host !== 'string' || typeof sid !== 'string' || !/^[a-z2-7]{26}$/.test(sid)) throw new Error('invalid pin')
  host = host.toLowerCase()
  const list = pins[host] ?? []
  if (!list.includes(sid)) {
    pins[host] = [...list, sid]
    await mkdir(app.getPath('userData'), { recursive: true })
    await writeFile(pinsFile(), JSON.stringify(pins), { mode: 0o600 })
  }
})

// Chromium caches certificate decisions, refusals included, for the whole
// session: a mistyped link would block the right one until a restart. So a
// new server is first checked here, with its own connection; Chromium only
// talks to it once its certificate is known to match.
ipcMain.handle('tls:check', async (e, host, port, sid) => {
  fromApp(e)
  if (typeof host !== 'string' || typeof port !== 'number' || typeof sid !== 'string') throw new Error('invalid check')
  return checkServer(host, port, sid)
})

function installVerifier() {
  session.defaultSession.setCertificateVerifyProc((req, cb) => {
    if (req.verificationResult === 'net::OK') return cb(-3) // valid for a public authority
    const expected = pins[req.hostname.toLowerCase()]
    const sid = expected?.length ? boundServerID(req.certificate.data) : null
    cb(sid && expected.includes(sid) ? 0 : -2)
  })
}

// --- screen sharing: the UI lists sources, the user picks one, and the next
// getDisplayMedia() request gets it (with system audio on Windows) ---

let chosenScreen = ''

ipcMain.handle('screen:sources', async (e) => {
  fromApp(e)
  const sources = await desktopCapturer.getSources({ types: ['screen', 'window'], thumbnailSize: { width: 320, height: 180 } })
  return sources.map((s) => ({ id: s.id, name: s.name, thumbnail: s.thumbnail.toDataURL() }))
})

ipcMain.handle('screen:choose', (e, id) => {
  fromApp(e)
  if (typeof id !== 'string') throw new Error('invalid source')
  chosenScreen = id
})

app.whenReady().then(async () => {
  session.defaultSession.setDisplayMediaRequestHandler(async (_req, cb) => {
    const sources = await desktopCapturer.getSources({ types: ['screen', 'window'] })
    const source = sources.find((s) => s.id === chosenScreen) ?? sources.find((s) => s.id.startsWith('screen:'))
    chosenScreen = ''
    if (!source) return cb({})
    cb(process.platform === 'win32' ? { video: source, audio: 'loopback' } : { video: source })
  })
  await loadPins()
  installVerifier()

  // Linux without a keychain (no Secret Service / KWallet): secrets are then
  // only obfuscated on disk; the UI is told through info().secureStorage.
  if (process.platform === 'linux' && !safeStorage.isEncryptionAvailable()) {
    safeStorage.setUsePlainTextEncryption(true)
  }
  // Microphone, camera and notifications are needed later (voice, calls).
  const allowed = new Set(['media', 'display-capture', 'notifications', 'clipboard-sanitized-write', 'fullscreen'])
  session.defaultSession.setPermissionRequestHandler((_wc, permission, cb) => cb(allowed.has(permission)))
  createWindow()
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})
