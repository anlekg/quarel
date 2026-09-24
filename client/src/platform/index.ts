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

interface DesktopBridge {
  secrets: Secrets
  info(): Promise<AppInfo>
  pinServer(host: string, sid: string): Promise<void>
  screenSources(): Promise<ScreenSource[]>
  chooseScreenSource(id: string): Promise<void>
  checkServer(host: string, port: number, sid: string): Promise<'authority' | 'binding' | 'mismatch' | 'unreachable'>
}

declare global {
  interface Window {
    quarelDesktop?: DesktopBridge
  }
}

const desktop = typeof window !== 'undefined' ? window.quarelDesktop : undefined

// Web fallback: browser storage, readable by anything running on the page.
// Acceptable for the web client, whose sessions are expected to be shorter-lived.
const webSecrets: Secrets = {
  async get(key) {
    return localStorage.getItem('quarel.secret.' + key)
  },
  async set(key, value) {
    localStorage.setItem('quarel.secret.' + key, value)
  },
  async delete(key) {
    localStorage.removeItem('quarel.secret.' + key)
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

// Desktop screen sharing: list the screens and windows, then choose the one
// the next screen-share request will get.
export async function screenSources(): Promise<ScreenSource[]> {
  return desktop ? desktop.screenSources() : []
}

export async function chooseScreenSource(id: string) {
  await desktop?.chooseScreenSource(id)
}
