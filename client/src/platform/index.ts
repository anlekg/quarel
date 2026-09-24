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

interface DesktopBridge {
  secrets: Secrets
  info(): Promise<AppInfo>
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
