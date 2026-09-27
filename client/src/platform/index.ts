// What the UI needs from its host: the Electron shell (preload.ts) or, for the
// web client, the browser alone.

export interface AppInfo {
  platform: string
  version: string
  deviceName: string
  secureStorage: boolean
}

interface Secrets {
  get(key: string): Promise<string | null>
  set(key: string, value: string): Promise<void>
  delete(key: string): Promise<void>
}

export interface ScreenSource {
  id: string
  name: string
  thumbnail: string // data URL
}

interface Vault {
  get(key: string): Promise<string | null>
  set(key: string, value: string): Promise<void>
}

// Desktop app updates (electron/updater.ts).
export interface UpdateState {
  status: 'disabled' | 'idle' | 'checking' | 'none' | 'downloading' | 'ready' | 'manual' | 'error'
  current: string
  version?: string
  progress?: number
  download?: string
  error?: string
}


interface FileStore {
  get(id: string): Promise<Uint8Array | null>
  put(id: string, data: Uint8Array): Promise<void>
  delete(id: string): Promise<void>
}

interface DesktopBridge {
  secrets: Secrets
  vault: Vault
  files: FileStore
  info(): Promise<AppInfo>
  takeInvite(): Promise<string>
  updates: {
    state(): Promise<UpdateState>
    check(): Promise<UpdateState>
    install(): Promise<void>
    onState(cb: (s: UpdateState) => void): () => void
  }
  onInvite(cb: (link: string) => void): () => void
  pinServer(host: string, sid: string): Promise<void>
  closeToTray(on?: boolean): Promise<boolean>
  showWindow(): Promise<void>
  screenSources(): Promise<ScreenSource[]>
  chooseScreenSource(id: string): Promise<void>
  checkServer(host: string, port: number, sid: string): Promise<'authority' | 'binding' | 'mismatch' | 'unreachable'>
  tlsMode(host: string, sid: string): Promise<'binding' | 'authority' | 'conflict'>
  forgetServer(host: string): Promise<void>
}

declare global {
  interface Window {
    quarelDesktop?: DesktopBridge
  }
}

const desktop = typeof window !== 'undefined' ? window.quarelDesktop : undefined

export const updates = desktop?.updates ?? null

// Web: secrets in IndexedDB, encrypted (see "at rest" below). Older versions
// kept them in localStorage in clear: moved on first read.
const webSecrets: Secrets = {
  async get(key) {
    const v = await idbGet<Sealed>('secrets', key)
    if (v) return unseal(v)
    const legacy = localStorage.getItem('quarel.secret.' + key)
    if (legacy !== null) {
      await webSecrets.set(key, legacy)
      localStorage.removeItem('quarel.secret.' + key)
    }
    return legacy
  },
  async set(key, value) {
    const sealed = await seal(value)
    await idbWrite('secrets', (st) => st.put(sealed, key))
  },
  async delete(key) {
    localStorage.removeItem('quarel.secret.' + key)
    await idbWrite('secrets', (st) => st.delete(key))
  },
}

export const isDesktop = !!desktop
export const secrets: Secrets = desktop?.secrets ?? webSecrets

export async function appInfo(): Promise<AppInfo> {
  if (desktop) return desktop.info()
  return { platform: 'web', version: '0.1.0', deviceName: browserName(), secureStorage: false }
}

function browserName() {
  const ua = navigator.userAgent
  if (/Firefox\//.test(ua)) return 'Firefox'
  if (/Edg\//.test(ua)) return 'Edge'
  if (/Chrome\//.test(ua)) return 'Chrome'
  if (/Safari\//.test(ua)) return 'Safari'
  return 'Navigateur'
}

// Non-secret preferences (last identity service, UI choices).
export const prefs = {
  get<T>(key: string, fallback: T): T {
    try {
      const v = localStorage.getItem('quarel.pref.' + key)
      return v === null ? fallback : (JSON.parse(v) as T)
    } catch {
      return fallback
    }
  },
  set(key: string, value: unknown) {
    try {
      localStorage.setItem('quarel.pref.' + key, JSON.stringify(value))
    } catch {
      /* storage unavailable: preference not kept */
    }
  },
}

// Lets the desktop app accept the self-signed certificate of this server
// (checked against its ID during every TLS handshake). Browsers cannot do
// this: the web client needs servers with a certificate from an authority.
export async function pinServer(host: string, sid: string) {
  await desktop?.pinServer(host, sid)
}

// Desktop: checks a new server's certificate before the UI connects to it.
// Browsers check it themselves (authority certificates only).
export async function checkServer(host: string, port: number, sid: string) {
  return desktop ? desktop.checkServer(host, port, sid) : 'authority'
}

// How the connections to host were checked, for the login proof: 'binding'
// (certificate bound to server sid, desktop only), 'authority' (ordinary
// certificate: the server checks that host is its own name), or 'conflict'
// (the desktop app accepted another identity for host in this session).
export async function tlsMode(host: string, sid: string): Promise<'binding' | 'authority' | 'conflict'> {
  return desktop ? desktop.tlsMode(host, sid) : 'authority'
}

// Desktop: no saved server uses host any more; its certificate binding is released.
export async function forgetServerTLS(host: string) {
  await desktop?.forgetServer(host)
}

// Desktop screen sharing: list the screens and windows, then choose the one
// the next screen-share request will get.
export async function screenSources(): Promise<ScreenSource[]> {
  return desktop ? desktop.screenSources() : []
}

export async function chooseScreenSource(id: string) {
  await desktop?.chooseScreenSource(id)
}

// Larger private state (end-to-end keys, decrypted history). Desktop: files
// encrypted by the OS keychain. Browser: IndexedDB (not encrypted at rest).
const idb = (): Promise<IDBDatabase> =>
  new Promise((resolve, reject) => {
    const req = indexedDB.open('quarel', 3)
    req.onupgradeneeded = () => {
      for (const store of ['vault', 'files', 'secrets', 'keys']) if (!req.result.objectStoreNames.contains(store)) req.result.createObjectStore(store)
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })

async function idbGet<T>(store: string, key: string): Promise<T | null> {
  const db = await idb()
  return new Promise((resolve, reject) => {
    const r = db.transaction(store).objectStore(store).get(key)
    r.onsuccess = () => resolve((r.result as T | undefined) ?? null)
    r.onerror = () => reject(r.error)
  })
}

async function idbWrite(store: string, fn: (s: IDBObjectStore) => void): Promise<void> {
  const db = await idb()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(store, 'readwrite')
    fn(tx.objectStore(store))
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

// --- at rest (web) ---
// Secrets and the private messages' state are encrypted (AES-GCM) with a key
// the page cannot read (a non-extractable WebCrypto key, kept by the browser
// in IndexedDB). Their content is no longer in clear in the browser's files or
// developer tools; code running in the page itself could still use the key.

interface Sealed {
  v: 1
  iv: Uint8Array<ArrayBuffer>
  ct: ArrayBuffer
}

let keyPromise: Promise<CryptoKey> | null = null

function storageKey(): Promise<CryptoKey> {
  keyPromise ??= (async () => {
    const existing = await idbGet<CryptoKey>('keys', 'at-rest')
    if (existing) return existing
    const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt'])
    try {
      await idbWrite('keys', (st) => st.add(key, 'at-rest')) // "add": another tab may have created one meanwhile
      return key
    } catch {
      return (await idbGet<CryptoKey>('keys', 'at-rest'))!
    }
  })()
  keyPromise.catch(() => (keyPromise = null))
  return keyPromise
}

async function seal(value: string): Promise<Sealed> {
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, await storageKey(), new TextEncoder().encode(value))
  return { v: 1, iv, ct }
}

async function unseal(s: Sealed): Promise<string | null> {
  try {
    return new TextDecoder().decode(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: s.iv }, await storageKey(), s.ct))
  } catch {
    return null // key lost with the browser's data: as if nothing was stored
  }
}

const webVault: Vault = {
  async get(key) {
    const v = await idbGet<Sealed | string>('vault', key)
    if (typeof v === 'string') { // written in clear by an older version
      await webVault.set(key, v)
      return v
    }
    return v ? unseal(v) : null
  },
  async set(key, value) {
    const sealed = await seal(value)
    await idbWrite('vault', (st) => st.put(sealed, key))
  },
}

export const vault: Vault = desktop?.vault ?? webVault

// Ciphertexts of private conversation files (their keys are in the vault).
const webFiles: FileStore = {
  get: (id) => idbGet<Uint8Array>('files', id),
  put: (id, data) => idbWrite('files', (s) => s.put(data, id)),
  delete: (id) => idbWrite('files', (s) => s.delete(id)),
}

export const files: FileStore = desktop?.files ?? webFiles

// Invite links opened from outside: quarel:// handed over by the system
// (desktop), or https://<web app>/join#… (web).
export async function takeLaunchInvite(): Promise<string> {
  if (desktop) return desktop.takeInvite()
  if (typeof location !== 'undefined' && location.pathname === '/join' && location.hash.length > 1) {
    const link = 'quarel://' + decodeURIComponent(location.hash.slice(1))
    history.replaceState(null, '', '/')
    return link
  }
  return ''
}

export function onInviteOpened(cb: (link: string) => void): () => void {
  return desktop ? desktop.onInvite(cb) : () => {}
}

// Web: the browser's "install this app" prompt, kept until the person asks.
interface InstallPrompt extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}
let installPrompt: InstallPrompt | null = null
const installListeners = new Set<() => void>()
if (!desktop && typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    installPrompt = e as InstallPrompt
    installListeners.forEach((l) => l())
  })
  window.addEventListener('appinstalled', () => {
    installPrompt = null
    installListeners.forEach((l) => l())
  })
}

export const canInstall = () => !!installPrompt
export function onInstallChange(l: () => void) {
  installListeners.add(l)
  return () => {
    installListeners.delete(l)
  }
}
export async function install() {
  const p = installPrompt
  if (!p) return false
  await p.prompt()
  const { outcome } = await p.userChoice
  if (outcome === 'accepted') installPrompt = null
  installListeners.forEach((l) => l())
  return outcome === 'accepted'
}

// Desktop: closing the window leaves Quarel in the notification area (null: web).
export async function closeToTray(on?: boolean): Promise<boolean | null> {
  return desktop ? desktop.closeToTray(on) : null
}

// Brings the window back (hidden in the notification area, minimized…).
export function showWindow() {
  if (desktop) desktop.showWindow().catch(() => {})
  else window.focus()
}
