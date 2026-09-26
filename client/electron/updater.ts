// Automatic updates of the desktop app (electron-updater, "generic" feed at
// https://app.quarel.app/updates). Each release's description (latest.yml,
// latest-linux.yml) is signed with Quarel's Ed25519 release key: the app only
// downloads what that signature covers, and only installs a file whose SHA-512
// is the signed one. Whoever controls the web host alone cannot ship code.
//
// Windows (NSIS) and Linux AppImage update themselves; the .deb package cannot:
// the app then only says that a new version exists.
import { app, BrowserWindow, ipcMain, net } from 'electron'
import { autoUpdater, type UpdateInfo } from 'electron-updater'
import { load } from 'js-yaml'
import { newer, verifyRelease } from './releasesig'

const FEED = process.env.QUAREL_UPDATE_URL || 'https://app.quarel.app/updates'
const CHECK_EVERY = 6 * 3600_000
// Tests sign their own releases: another public key only with a test data folder.
const KEY = (process.env.QUAREL_USER_DATA && process.env.QUAREL_UPDATE_KEY) || undefined

export type UpdateStatus = 'disabled' | 'idle' | 'checking' | 'none' | 'downloading' | 'ready' | 'manual' | 'error'

export interface UpdateState {
  status: UpdateStatus
  current: string
  version?: string // the new version
  progress?: number // 0-100 while downloading
  download?: string // manual: where to get it
  error?: string
}

let state: UpdateState = { status: 'idle', current: app.getVersion() }
let signed: { version: string; sha512: string; path: string } | null = null

function set(patch: Partial<UpdateState>) {
  state = { ...state, ...patch }
  for (const w of BrowserWindow.getAllWindows()) w.webContents.send('update:state', state)
}

const channelFile = () => (process.platform === 'win32' ? 'latest.yml' : process.platform === 'darwin' ? 'latest-mac.yml' : 'latest-linux.yml')

async function fetchBytes(url: string): Promise<Buffer> {
  const res = await net.fetch(url, { cache: 'no-store' })
  if (!res.ok) throw new Error('HTTP ' + res.status)
  return Buffer.from(await res.arrayBuffer())
}

// Downloads and checks the signed description of the latest release.
async function signedRelease() {
  const file = channelFile()
  const [yml, sig] = await Promise.all([fetchBytes(FEED + '/' + file), fetchBytes(FEED + '/' + file + '.sig')])
  if (!verifyRelease(yml, sig.toString('utf8'), KEY)) throw new Error('signature')
  const doc = load(yml.toString('utf8')) as { version: string; path: string; sha512: string; files?: { url: string; sha512: string }[] }
  const main = doc.files?.[0] ?? { url: doc.path, sha512: doc.sha512 }
  return { version: String(doc.version), sha512: main.sha512, path: main.url }
}

// A package that can replace itself: the NSIS install on Windows, an AppImage on Linux.
const selfUpdating = () => process.platform === 'win32' || (process.platform === 'linux' && !!process.env.APPIMAGE)

let busy = false
export async function checkForUpdates() {
  if (busy || state.status === 'disabled' || state.status === 'ready') return state
  busy = true
  set({ status: 'checking', error: undefined })
  try {
    signed = await signedRelease()
    if (!newer(signed.version, app.getVersion())) {
      set({ status: 'none', version: undefined })
      return state
    }
    if (!selfUpdating()) {
      set({ status: 'manual', version: signed.version, download: FEED + '/' + signed.path })
      return state
    }
    const res = await autoUpdater.checkForUpdates()
    const info = res?.updateInfo
    if (!info || !matches(info)) throw new Error('mismatch')
    set({ status: 'downloading', version: info.version, progress: 0 })
    await autoUpdater.downloadUpdate()
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    set({ status: 'error', error: msg === 'signature' || msg === 'mismatch' ? 'unsigned' : 'unreachable' })
  } finally {
    busy = false
  }
  return state
}

// What electron-updater will install must be exactly the signed release.
function matches(info: UpdateInfo) {
  return !!signed && info.version === signed.version && info.files?.[0]?.sha512 === signed.sha512
}

export function setupUpdates(fromApp: (e: Electron.IpcMainInvokeEvent) => void) {
  ipcMain.handle('update:state', (e) => {
    fromApp(e)
    return state
  })
  ipcMain.handle('update:check', (e) => {
    fromApp(e)
    return checkForUpdates()
  })
  ipcMain.handle('update:install', (e) => {
    fromApp(e)
    if (state.status === 'ready') setImmediate(() => autoUpdater.quitAndInstall(false, true))
  })
  // Not in development, nor in tests (unless a test feed is given).
  if ((!app.isPackaged && !process.env.QUAREL_UPDATE_URL) || (process.env.QUAREL_USER_DATA && !process.env.QUAREL_UPDATE_URL)) {
    set({ status: 'disabled' })
    return
  }
  autoUpdater.setFeedURL({ provider: 'generic', url: FEED })
  autoUpdater.autoDownload = false
  autoUpdater.autoInstallOnAppQuit = false // only once the downloaded file is known to be the signed one
  autoUpdater.logger = null
  autoUpdater.on('download-progress', (p) => set({ progress: Math.round(p.percent) }))
  autoUpdater.on('update-downloaded', (info) => {
    if (!matches(info)) return set({ status: 'error', error: 'unsigned' })
    autoUpdater.autoInstallOnAppQuit = true // installs at the next quit if the person does not restart now
    set({ status: 'ready', version: info.version, progress: 100 })
  })
  autoUpdater.on('error', () => {
    if (state.status === 'downloading' || state.status === 'checking') set({ status: 'error', error: 'unreachable' })
  })
  setTimeout(() => checkForUpdates(), 15_000)
  setInterval(() => checkForUpdates(), CHECK_EVERY)
}
