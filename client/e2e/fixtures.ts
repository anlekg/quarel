// Test helpers: a throwaway Identity service, the desktop app, TOTP codes.
import { _electron as electron, type ElectronApplication, type Page } from '@playwright/test'
import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import { createHmac, generateKeyPairSync } from 'node:crypto'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const repo = resolve(here, '..', '..')
const client = resolve(here, '..')

export class Identity {
  proc!: ChildProcess
  log = ''
  dir = mkdtempSync(join(tmpdir(), 'quarel-id-'))
  constructor(public port: number, public env: Record<string, string> = {}) {}

  get url() {
    return 'http://127.0.0.1:' + this.port
  }

  async start() {
    this.proc = spawn(join(repo, 'bin', 'quarel-identity'), [], {
      env: {
        ...process.env,
        QUAREL_ADDR: '127.0.0.1:' + this.port,
        QUAREL_ISSUER: 'localhost:' + this.port,
        QUAREL_DATA_DIR: this.dir,
        QUAREL_RATE_LIMITS: 'off',
        QUAREL_ADMIN_ADDR: 'off',
        QUAREL_UPNP: 'off', // never open ports on the router in tests
        ...this.env,
      },
    })
    this.proc.stdout!.on('data', (d) => (this.log += d))
    this.proc.stderr!.on('data', (d) => (this.log += d))
    for (let i = 0; i < 50; i++) {
      try {
        if ((await fetch(this.url + '/v1/health')).ok) return
      } catch {
        /* not up yet */
      }
      await new Promise((r) => setTimeout(r, 100))
    }
    throw new Error('identity did not start:\n' + this.log)
  }

  stop() {
    this.proc?.kill()
    rmSync(this.dir, { recursive: true, force: true })
  }

  // Last emailed code (dev mode writes emails to the log).
  lastCode(): string {
    const all = [...this.log.matchAll(/Quarel : (\d{6})/g)]
    if (!all.length) throw new Error('no code in log:\n' + this.log)
    return all[all.length - 1][1]
  }

  async api<T>(method: string, path: string, body?: unknown, token?: string): Promise<T> {
    const res = await fetch(this.url + path, {
      method,
      headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    const text = await res.text()
    if (!res.ok) throw new Error(method + ' ' + path + ': ' + res.status + ' ' + text)
    return (text ? JSON.parse(text) : undefined) as T
  }

  // Signs in from "another device" through the API.
  async login(login: string, password: string, totp?: string) {
    const pub = generateKeyPairSync('ed25519').publicKey.export({ format: 'jwk' }).x!
    return this.api<{ session_id: string; session_token: string }>('POST', '/v1/auth/login', {
      login, password, totp_code: totp, device_name: 'Téléphone de test', device_key: pub,
    })
  }
}

export async function launchApp(userData: string): Promise<{ app: ElectronApplication; page: Page }> {
  const app = await electron.launch({
    // Fake microphone and camera (a beep and a test pattern), no permission prompt.
    args: [client, '--password-store=basic', '--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'],
    env: { ...process.env, QUAREL_USER_DATA: userData, QUAREL_DEV_URL: '' },
  })
  const page = await app.firstWindow()
  await page.waitForLoadState('domcontentloaded')
  return { app, page }
}

export function tempDir(prefix: string) {
  return mkdtempSync(join(tmpdir(), prefix))
}

// RFC 6238 code (SHA1, 30 s, 6 digits) for a base32 secret.
export function totp(secretB32: string, at = Date.now()): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const c of secretB32.replace(/=+$/, '').toUpperCase()) bits += alphabet.indexOf(c).toString(2).padStart(5, '0')
  const key = Buffer.from(bits.match(/.{8}/g)!.map((b) => parseInt(b, 2)))
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 30_000)))
  const h = createHmac('sha1', key).update(counter).digest()
  const o = h[h.length - 1] & 0xf
  return ((h.readUInt32BE(o) & 0x7fffffff) % 1_000_000).toString().padStart(6, '0')
}

// A community server (self-signed certificate bound to its identity).
export class Community {
  proc!: ChildProcess
  log = ''
  dir = mkdtempSync(join(tmpdir(), 'quarel-srv-'))
  constructor(public port: number, public issuer: string, public env: Record<string, string> = {}) {}

  async start() {
    this.proc = spawn(join(repo, 'bin', 'quarel-server'), [], {
      env: {
        ...process.env,
        QUAREL_ADDR: '127.0.0.1:' + this.port,
        QUAREL_TRUSTED_ISSUERS: this.issuer,
        QUAREL_DATA_DIR: this.dir,
        QUAREL_UPNP: 'off',
        QUAREL_VOICE: 'off',
        QUAREL_RATE_LIMITS: 'off',
        QUAREL_LINK_PREVIEWS: 'off',
        QUAREL_ADMIN_ADDR: 'off',
        ...this.env,
      },
    })
    this.proc.stdout!.on('data', (d) => (this.log += d))
    this.proc.stderr!.on('data', (d) => (this.log += d))
    for (let i = 0; i < 50 && !/unique\) : [a-z0-9]+/.test(this.log); i++) await new Promise((r) => setTimeout(r, 100))
  }

  claimCode() {
    return /unique\) : ([a-z0-9]+)/.exec(this.log)![1]
  }

  stop() {
    this.proc?.kill()
    rmSync(this.dir, { recursive: true, force: true })
  }
}

// The command-line test client, acting as another person.
export class Ctl {
  dir = mkdtempSync(join(tmpdir(), 'quarelctl-'))
  constructor(public identity: Identity) {}

  run(profile: string, ...args: string[]): string {
    try {
      return execFileSync(join(repo, 'bin', 'quarelctl'), ['-s', this.identity.url, '-p', profile, ...args], {
        cwd: this.dir,
        env: { ...process.env, XDG_CONFIG_HOME: join(this.dir, 'cfg'), QUAREL_PASSWORD: 'motdepasse-solide' },
        encoding: 'utf8',
      })
    } catch (e) {
      const err = e as { stdout?: string; stderr?: string }
      return (err.stdout ?? '') + (err.stderr ?? '')
    }
  }

  // A long-running command (dm-listen…) in the background; its output accumulates in out().
  listen(profile: string, ...args: string[]) {
    const p = spawn(join(repo, 'bin', 'quarelctl'), ['-s', this.identity.url, '-p', profile, ...args], {
      cwd: this.dir,
      env: { ...process.env, XDG_CONFIG_HOME: join(this.dir, 'cfg'), QUAREL_PASSWORD: 'motdepasse-solide' },
    })
    let out = ''
    p.stdout.on('data', (d) => (out += d))
    p.stderr.on('data', (d) => (out += d))
    return { out: () => out, stop: () => p.kill() }
  }

  // run() blocks the event loop: logs are read only once it is free again.
  async account(name: string) {
    this.run(name, 'register', name + '@example.com', name)
    await new Promise((r) => setTimeout(r, 300))
    this.run(name, 'verify-email', name + '@example.com', this.identity.lastCode())
    this.run(name, 'login', name, 'pc-' + name)
  }

  stop() {
    rmSync(this.dir, { recursive: true, force: true })
  }
}

// Signs in to a server's administration page (choosing its password on first
// use, allowed from this machine) and returns a JSON caller for its API.
export async function adminAPI(base: string) {
  let cookie = ''
  const call = async <T>(method: string, path: string, body?: unknown): Promise<T> => {
    const res = await fetch(base + '/api' + path, {
      method,
      headers: { 'X-Quarel-Admin': '1', 'Content-Type': 'application/json', Cookie: cookie },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    const set = res.headers.get('set-cookie')
    if (set) cookie = set.split(';')[0]
    const text = await res.text()
    if (!res.ok) throw new Error(method + ' ' + path + ': ' + res.status + ' ' + text)
    return (text ? JSON.parse(text) : undefined) as T
  }
  for (let i = 0; i < 50; i++) {
    try {
      await fetch(base + '/api/session')
      break
    } catch {
      await new Promise((r) => setTimeout(r, 100))
    }
  }
  await call('POST', '/setup', { password: 'motdepasse-admin' })
  return call
}
