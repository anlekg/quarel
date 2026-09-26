// Electron main process: one window running the web UI, with the renderer
// sandboxed (no Node access). The only bridge is the small API in preload.ts.
import { app, BrowserWindow, desktopCapturer, ipcMain, protocol, safeStorage, shell, session } from 'electron'
import { hostname } from 'node:os'
import { extname, join, normalize, sep } from 'node:path'
import { readFile, writeFile, rename, mkdir, rm } from 'node:fs/promises'
import { boundServerID, checkServer } from './tlsbind'
import { setupUpdates } from './updater'

const devURL = process.env.QUAREL_DEV_URL
if (process.env.QUAREL_USER_DATA) app.setPath('userData', process.env.QUAREL_USER_DATA)

let win: BrowserWindow | null = null

// --- quarel:// invite links opened from the browser or another app ---
// One instance per data folder: a second launch (the system opening a link)
// hands the link to the running one.
const findInvite = (args: string[]) => args.find((a) => /^quarel:\/\//i.test(a))
let pendingInvite = findInvite(process.argv) ?? ''
if (!app.requestSingleInstanceLock()) app.exit(0)
app.on('second-instance', (_e, argv) => {
  const link = findInvite(argv)
  if (link) deliverInvite(link)
  if (win) {
    if (win.isMinimized()) win.restore()
    win.focus()
  }
})
app.on('open-url', (e, url) => { // macOS
  e.preventDefault()
  deliverInvite(url)
})
function deliverInvite(link: string) {
  if (link.length > 2048) return
  pendingInvite = link
  win?.webContents.send('invite:open', link)
}
if (!process.env.QUAREL_USER_DATA) { // not while testing: it would change the system's link handler
  if (process.defaultApp && process.argv[1]) app.setAsDefaultProtocolClient('quarel', process.execPath, [join(process.cwd(), process.argv[1])])
  else app.setAsDefaultProtocolClient('quarel')
}

// The UI is served from app://quarel/ (not file://): a real origin, fetch()
// works (WebAssembly), and nothing outside the built UI can be read.
const APP_ORIGIN = 'app://quarel'
protocol.registerSchemesAsPrivileged([{ scheme: 'app', privileges: { standard: true, secure: true, supportFetchAPI: true } }])

const mimeTypes: Record<string, string> = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.wasm': 'application/wasm',
  '.woff': 'font/woff', '.woff2': 'font/woff2', '.svg': 'image/svg+xml', '.png': 'image/png', '.json': 'application/json',
  '.webmanifest': 'application/manifest+json',
}

function serveApp() {
  const root = join(__dirname, '..', 'dist')
  protocol.handle('app', async (req) => {
    const url = new URL(req.url)
    const path = normalize(join(root, decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname)))
    if (url.host !== 'quarel' || (path !== root && !path.startsWith(root + sep))) return new Response('not found', { status: 404 })
    try {
      const body = await readFile(path)
      return new Response(body, { headers: { 'Content-Type': mimeTypes[extname(path)] ?? 'application/octet-stream' } })
    } catch {
      return new Response('not found', { status: 404 })
    }
  })
}

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
  win.loadURL(devURL || APP_ORIGIN + '/index.html')
}

function isAppURL(url: string) {
  return devURL ? url.startsWith(devURL) : url.startsWith(APP_ORIGIN + '/')
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
  if (typeof key !== 'string' || !/^[a-z0-9:._-]{1,128}$/i.test(key) || key.startsWith('.')) throw new Error('invalid secret key')
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

// --- vault: larger encrypted state (end-to-end encryption keys, decrypted
// message history), one file per entry, encrypted with the OS keychain ---

const vaultDir = () => join(app.getPath('userData'), 'vault')

ipcMain.handle('vault:get', async (e, key) => {
  fromApp(e)
  try {
    const data = await readFile(join(vaultDir(), checkKey(key)))
    return safeStorage.decryptString(data)
  } catch {
    return null
  }
})

ipcMain.handle('vault:set', async (e, key, value) => {
  fromApp(e)
  if (typeof value !== 'string') throw new Error('vault value must be a string')
  await mkdir(vaultDir(), { recursive: true, mode: 0o700 })
  const path = join(vaultDir(), checkKey(key))
  await writeFile(path + '.tmp', safeStorage.encryptString(value), { mode: 0o600 })
  await rename(path + '.tmp', path)
})

// --- files of private conversations: ciphertexts (end-to-end encrypted,
// their keys live in the vault), kept to be served to other devices ---

const filesDir = () => join(app.getPath('userData'), 'files')

function checkFileId(id: unknown): string {
  if (typeof id !== 'string' || !/^[a-z0-9]{8,64}$/.test(id)) throw new Error('invalid file id')
  return id
}

ipcMain.handle('files:get', async (e, id) => {
  fromApp(e)
  try {
    return new Uint8Array(await readFile(join(filesDir(), checkFileId(id))))
  } catch {
    return null
  }
})

ipcMain.handle('files:put', async (e, id, data) => {
  fromApp(e)
  if (!(data instanceof Uint8Array)) throw new Error('file data must be bytes')
  await mkdir(filesDir(), { recursive: true, mode: 0o700 })
  const path = join(filesDir(), checkFileId(id))
  await writeFile(path + '.tmp', data, { mode: 0o600 })
  await rename(path + '.tmp', path)
})

ipcMain.handle('files:delete', async (e, id) => {
  fromApp(e)
  await rm(join(filesDir(), checkFileId(id)), { force: true })
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
// A host whose certificate proved a server's identity (quarel://binding, see
// tlsbind.ts) is bound to that server ID: from then on Chromium accepts only
// that server's bound certificate for it, never an ordinary one, so no one
// holding a valid certificate for the same name can step in. The login proof
// says whether the connection was checked this way ("binding") or only by an
// authority ("authority": the server then checks that the name is its own).

const pinsFile = () => join(app.getPath('userData'), 'server-pins.json')
interface Pins {
  v: 2
  bindings: Record<string, string> // host → the one server ID its certificate proves
  legacy: Record<string, string[]> // server IDs expected before this format: become a binding on first use
}
let pins: Pins = { v: 2, bindings: {}, legacy: {} }
// Identities Chromium accepted for each host in this session ('ca' or a
// server ID). Chromium caches its certificate decisions for the session, so
// a host that changed identity meanwhile cannot be trusted until a restart.
const accepted = new Map<string, Set<string>>()

async function loadPins() {
  try {
    const raw = JSON.parse(await readFile(pinsFile(), 'utf8'))
    pins = raw?.v === 2 ? { v: 2, bindings: raw.bindings ?? {}, legacy: raw.legacy ?? {} } : { v: 2, bindings: {}, legacy: raw ?? {} }
  } catch {
    pins = { v: 2, bindings: {}, legacy: {} }
  }
}

async function savePins() {
  await mkdir(app.getPath('userData'), { recursive: true })
  await writeFile(pinsFile(), JSON.stringify(pins), { mode: 0o600 })
}

function bind(host: string, sid: string) {
  if (pins.bindings[host] === sid) return
  pins.bindings[host] = sid
  delete pins.legacy[host]
  savePins().catch(() => {})
}

function note(host: string, identity: string) {
  let set = accepted.get(host)
  if (!set) accepted.set(host, (set = new Set()))
  set.add(identity)
}

const validHost = (h: unknown): h is string => typeof h === 'string' && h.length > 0 && h.length <= 253
const validSID = (s: unknown): s is string => typeof s === 'string' && /^[a-z2-7]{26}$/.test(s)
const normHost = (h: string) => h.toLowerCase().replace(/^\[|\]$/g, '').replace(/\.$/, '')

// The UI expects server sid at host (a saved server, or one shared by my own
// device): its bound certificate will be accepted there, and the host bound to it.
ipcMain.handle('tls:pin', async (e, host, sid) => {
  fromApp(e)
  if (!validHost(host) || !validSID(sid)) throw new Error('invalid pin')
  host = normHost(host)
  if (pins.bindings[host]) return
  const list = pins.legacy[host] ?? []
  if (!list.includes(sid)) {
    pins.legacy[host] = [...list, sid]
    await savePins()
  }
})

// Chromium caches certificate decisions, refusals included, for the whole
// session: a mistyped link would block the right one until a restart. So a
// new server is first checked here, with its own connection; Chromium only
// talks to it once its certificate is known to match.
ipcMain.handle('tls:check', async (e, host, port, sid) => {
  fromApp(e)
  if (!validHost(host) || typeof port !== 'number' || !validSID(sid)) throw new Error('invalid check')
  host = normHost(host)
  const res = await checkServer(host, port, sid)
  if (res === 'binding' && pins.bindings[host] !== sid) {
    bind(host, sid)
    session.defaultSession.closeAllConnections() // no connection set up under the previous rule is reused
  }
  // A host bound to a server ID with an ordinary certificate now: someone else, or a
  // server that changed its setup (leaving it releases the host, see tls:forget).
  if (res === 'authority' && pins.bindings[host]) return 'mismatch'
  return res
})

// How connections to host were checked, for the login proof: 'binding' when
// the host is bound to sid and Chromium accepted nothing else for it in this
// session, 'authority' when it is not bound to sid, 'conflict' otherwise.
ipcMain.handle('tls:mode', (e, host, sid) => {
  fromApp(e)
  if (!validHost(host) || !validSID(sid)) throw new Error('invalid host')
  host = normHost(host)
  const seen = accepted.get(host) ?? new Set<string>()
  if (pins.bindings[host] === sid) return [...seen].every((x) => x === sid) ? 'binding' : 'conflict'
  return 'authority'
})

// The UI no longer uses any server at host (left): its binding is released.
ipcMain.handle('tls:forget', async (e, host) => {
  fromApp(e)
  if (!validHost(host)) throw new Error('invalid host')
  host = normHost(host)
  delete pins.bindings[host]
  delete pins.legacy[host]
  await savePins()
})

function installVerifier() {
  session.defaultSession.setCertificateVerifyProc((req, cb) => {
    const host = normHost(req.hostname)
    const bound = pins.bindings[host]
    if (req.verificationResult === 'net::OK') { // valid for a public authority
      if (bound) return cb(-2) // this host proved a server identity: it must keep doing so
      note(host, 'ca')
      return cb(-3)
    }
    const sid = boundServerID(req.certificate.data)
    if (!sid) return cb(-2)
    if (!bound) {
      // A saved server first seen since this format: its host becomes bound,
      // unless something else was already accepted for it in this session.
      const seen = accepted.get(host)
      if (!pins.legacy[host]?.includes(sid) || (seen && [...seen].some((x) => x !== sid))) return cb(-2)
      bind(host, sid)
    } else if (sid !== bound) return cb(-2)
    note(host, sid)
    cb(0)
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

// The link the app was started with (or received before the UI listened).
ipcMain.handle('invite:take', (e) => {
  fromApp(e)
  const link = pendingInvite
  pendingInvite = ''
  return link
})

ipcMain.handle('screen:choose', (e, id) => {
  fromApp(e)
  if (typeof id !== 'string') throw new Error('invalid source')
  chosenScreen = id
})

app.whenReady().then(async () => {
  serveApp()
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
  setupUpdates(fromApp)
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})
