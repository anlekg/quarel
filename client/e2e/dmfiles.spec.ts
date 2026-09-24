// Step 4c of the desktop client: encrypted files in private conversations,
// face to the Go test client. Peer to peer (WebRTC data channel) when the
// other device is online, server copy otherwise, and later from any device of
// the conversation that holds the file.
import { expect, test, type Page } from '@playwright/test'
import { randomBytes } from 'node:crypto'
import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(19280)
const ctl = new Ctl(id)
const password = 'motdepasse-solide'

// A tiny valid PNG (1×1).
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64')

test.beforeAll(async () => {
  await id.start()
  await ctl.account('alice')
  ctl.run('alice', 'e2e')
  await id.api('POST', '/v1/auth/register', { email: 'bob@example.com', pseudo: 'bob', password })
  await id.api('POST', '/v1/auth/verify-email', { email: 'bob@example.com', code: id.lastCode() })
})
test.afterAll(() => {
  id.stop()
  ctl.stop()
})

async function signIn(page: Page) {
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill('bob')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
}

const eventNumber = (history: string, text: string) => history.split('\n').find((l) => l.includes(text))?.match(/#(\d+)/)?.[1] ?? ''

test('private conversation files: peer to peer, server copy, later from a holder', async () => {
  const userData = tempDir('quarel-app-')
  let { app, page } = await launchApp(userData)
  await signIn(page)
  ctl.run('alice', 'friend-add', 'bob')
  await page.getByRole('button', { name: /En attente/ }).click()
  await page.getByRole('button', { name: 'Accepter' }).click()
  await page.getByRole('button', { name: 'Tous', exact: true }).click()
  await page.getByRole('button', { name: 'Écrire à alice' }).click()
  const log = page.getByRole('log')
  const upload = page.getByLabel('Fichier à envoyer')

  // 1. alice is online: the app sends peer to peer, no server copy.
  const listener = ctl.listen('alice', 'dm-listen')
  await new Promise((r) => setTimeout(r, 800))
  const doc = randomBytes(40_000)
  await page.getByLabel('Message pour alice').fill('le devis')
  await upload.setInputFiles({ name: 'devis.bin', mimeType: 'application/octet-stream', buffer: doc })
  await expect(log.getByTestId('dm-file')).toContainText('devis.bin')
  await expect(log).toContainText('le devis')
  await expect.poll(() => listener.out(), { timeout: 15000 }).toContain('fichier reçu en direct')
  listener.stop()
  let history = ctl.run('alice', 'dm-history', 'bob')
  const n1 = eventNumber(history, 'devis.bin')
  expect(ctl.run('alice', 'dm-download', 'bob', n1, 'devis-alice.bin')).toContain('depuis cet appareil')
  expect(readFileSync(join(ctl.dir, 'devis-alice.bin')).equals(doc)).toBe(true)

  // 2. alice is offline: the copy goes through the server.
  const photo = randomBytes(20_000)
  await upload.setInputFiles({ name: 'photo.bin', mimeType: 'application/octet-stream', buffer: photo })
  await expect(log.getByTestId('dm-file').filter({ hasText: 'photo.bin' })).toBeVisible()
  await expect(page.locator('.upload-pending')).toHaveCount(0, { timeout: 15000 })
  history = ctl.run('alice', 'dm-history', 'bob')
  expect(ctl.run('alice', 'dm-download', 'bob', eventNumber(history, 'photo.bin'), 'photo-alice.bin')).toMatch(/via le serveur|depuis cet appareil/)
  expect(readFileSync(join(ctl.dir, 'photo-alice.bin')).equals(photo)).toBe(true)

  // 3. alice sends an image while the app is online: received peer to peer and shown.
  writeFileSync(join(ctl.dir, 'point.png'), png)
  const sent = ctl.run('alice', 'dm-file', 'bob', 'point.png', 'une image')
  expect(sent).toContain('1 appareil(s) en direct')
  await expect(log.locator('img.attach-img[alt="point.png"]')).toBeVisible()
  await expect(log).toContainText('une image')

  // 4. The app is closed: alice's file waits on the server, the app gets it at
  // start, then the server copy is deleted (every device has it).
  await app.close()
  writeFileSync(join(ctl.dir, 'plan.png'), png)
  expect(ctl.run('alice', 'dm-file', 'bob', 'plan.png')).toContain('1 via le serveur')
  expect(readdirSync(join(id.dir, 'dm-files'))).toHaveLength(1)
  ;({ app, page } = await launchApp(userData))
  await page.getByRole('button', { name: 'alice' }).click()
  await expect(page.getByRole('log').locator('img.attach-img[alt="plan.png"]')).toBeVisible()
  await expect.poll(() => readdirSync(join(id.dir, 'dm-files')).length).toBe(0)
  await expect(page.getByRole('log').getByTestId('dm-file').filter({ hasText: 'devis.bin' })).toContainText('chiffré de bout en bout')

  // 5. A new device of alice gets the first file later, peer to peer from the app.
  ctl.run('alice2', 'login', 'alice', 'pc-alice2')
  const code = ctl.run('alice2', 'e2e').match(/Code de vérification de cet appareil : (\S+)/)![1]
  const devId = ctl.run('alice', 'devices').split('\n').find((l) => l.includes(code))!.match(/id (\S+)/)![1]
  expect(ctl.run('alice', 'device-approve', devId, code)).toContain('validé')
  ctl.run('alice2', 'dm-sync')
  const got = ctl.run('alice2', 'dm-download', 'bob', n1, 'devis-alice2.bin')
  expect(got).toContain('en direct depuis un autre appareil')
  expect(readFileSync(join(ctl.dir, 'devis-alice2.bin')).equals(doc)).toBe(true)

  // Deleting a file message removes it for alice too.
  const mine = page.getByRole('log').locator('.msg', { hasText: 'photo.bin' })
  await mine.hover()
  await mine.getByRole('button', { name: 'Supprimer' }).click()
  await expect(page.getByRole('log')).not.toContainText('photo.bin')
  expect(ctl.run('alice', 'dm-history', 'bob')).not.toContain('photo.bin')
  await app.close()
})
