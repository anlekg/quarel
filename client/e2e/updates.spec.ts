// Automatic updates: the app reads the signed description of the latest release
// from its feed and refuses anything the release key did not sign.
import { expect, test, type Page } from '@playwright/test'
import { generateKeyPairSync, sign } from 'node:crypto'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { launchApp, tempDir } from './fixtures'

const { privateKey, publicKey } = generateKeyPairSync('ed25519')
const testKey = publicKey.export({ format: 'der', type: 'spki' }).subarray(-32).toString('base64')
const signYml = (yml: string) => sign(null, Buffer.concat([Buffer.from('quarel-update-v1\0'), Buffer.from(yml)]), privateKey).toString('base64')

// What the feed serves, changed by each step.
let files: Record<string, string> = {}
let feed: Server

test.beforeAll(async () => {
  feed = createServer((req, res) => {
    const body = files[(req.url ?? '').replace(/^\/updates\//, '')]
    if (body === undefined) return res.writeHead(404).end()
    res.writeHead(200, { 'Content-Type': 'text/plain' }).end(body)
  })
  await new Promise<void>((r) => feed.listen(0, '127.0.0.1', r))
})
test.afterAll(() => feed.close())

function release(version: string) {
  const name = 'Quarel-' + version + '-x86_64.AppImage'
  return 'version: ' + version + '\nfiles:\n  - url: ' + name + '\n    sha512: c2hhNTEy\n    size: 1000\npath: ' + name + '\nsha512: c2hhNTEy\nreleaseDate: 2026-09-26T10:00:00.000Z\n'
}

const check = (page: Page) =>
  page.evaluate(() => (window as unknown as { quarelDesktop: { updates: { check(): Promise<{ status: string; version?: string; download?: string; error?: string; current: string }> } } }).quarelDesktop.updates.check())

test('updates: signed feed, tampering refused', async () => {
  const port = (feed.address() as AddressInfo).port
  process.env.QUAREL_UPDATE_URL = 'http://127.0.0.1:' + port + '/updates'
  process.env.QUAREL_UPDATE_KEY = testKey
  const { app, page } = await launchApp(tempDir('quarel-app-'))
  try {
    // A newer signed release: this copy (not an AppImage) cannot replace itself → download link.
    const yml = release('9.9.9')
    files = { 'latest-linux.yml': yml, 'latest-linux.yml.sig': signYml(yml) }
    let s = await check(page)
    expect(s.status).toBe('manual')
    expect(s.version).toBe('9.9.9')
    expect(s.download).toBe(process.env.QUAREL_UPDATE_URL + '/Quarel-9.9.9-x86_64.AppImage')

    // The description changed after signing (another file to install): refused.
    files['latest-linux.yml'] = yml.replace(/c2hhNTEy/g, 'YXV0cmU=')
    s = await check(page)
    expect(s).toMatchObject({ status: 'error', error: 'unsigned' })

    // Signed by another key: refused.
    const other = generateKeyPairSync('ed25519').privateKey
    files = { 'latest-linux.yml': yml, 'latest-linux.yml.sig': sign(null, Buffer.concat([Buffer.from('quarel-update-v1\0'), Buffer.from(yml)]), other).toString('base64') }
    s = await check(page)
    expect(s).toMatchObject({ status: 'error', error: 'unsigned' })

    // No signature at all.
    files = { 'latest-linux.yml': yml }
    s = await check(page)
    expect(s.status).toBe('error')

    // Same version as the app: up to date.
    const same = release(s.current)
    files = { 'latest-linux.yml': same, 'latest-linux.yml.sig': signYml(same) }
    s = await check(page)
    expect(s.status).toBe('none')

    // Feed gone.
    feed.closeAllConnections()
    files = {}
    s = await check(page)
    expect(s).toMatchObject({ status: 'error', error: 'unreachable' })
  } finally {
    delete process.env.QUAREL_UPDATE_URL
    delete process.env.QUAREL_UPDATE_KEY
    await app.close()
  }
})
