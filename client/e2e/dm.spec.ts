// Step 4a of the desktop client: friends and end-to-end encrypted private
// messages. The app (bob, vodozemac in WebAssembly) talks with alice on the Go
// test client (goolm) through a real Identity service: both implementations
// must understand each other.
import { expect, test, type Page } from '@playwright/test'
import { Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(19080)
const ctl = new Ctl(id)
const password = 'motdepasse-solide'
const userData = tempDir('quarel-app-')

test.beforeAll(async () => {
  await id.start()
  await ctl.account('alice')
  ctl.run('alice', 'e2e') // alice's keys, first device: master key
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

const history = () => ctl.run('alice', 'dm-history', 'bob')

test('private messages: friends, encrypted with the Go client both ways', async () => {
  let { app, page } = await launchApp(userData)
  await signIn(page)
  await expect(page.getByRole('heading', { name: 'Amis' }).or(page.locator('.channel-head .title', { hasText: 'Amis' }))).toBeVisible()

  // alice asks bob to be friends; bob accepts in the app.
  ctl.run('alice', 'friend-add', 'bob')
  await expect(page.getByRole('button', { name: /Messages privés, 1 demande/ })).toBeVisible()
  await page.getByRole('button', { name: /En attente/ }).click()
  await page.getByRole('button', { name: 'Accepter' }).click()
  await page.getByRole('button', { name: 'Tous', exact: true }).click()
  await page.getByRole('button', { name: 'Écrire à alice' }).click()

  // bob → alice (vodozemac → goolm).
  const composer = page.getByLabel('Message pour alice')
  await expect(page.getByText('Chiffré de bout en bout')).toBeVisible()
  await composer.fill('Salut alice, c’est chiffré ?')
  await composer.press('Enter')
  const log = page.getByRole('log')
  await expect(log).toContainText('Salut alice, c’est chiffré ?')
  expect(history()).toContain('bob : Salut alice, c’est chiffré ?')
  // alice's device received it: bob's message shows "Distribué".
  await expect(log.locator('.receipt')).toHaveText('Distribué', { timeout: 10000 })

  // alice → bob (goolm → vodozemac), live.
  ctl.run('alice', 'dm', 'bob', 'Oui, de bout en bout !')
  await expect(log).toContainText('Oui, de bout en bout !')

  // Typing and read receipts.
  ctl.run('alice', 'dm-typing', 'bob')
  await expect(page.getByText('alice écrit…')).toBeVisible()
  await composer.fill('Parfait')
  await composer.press('Enter')
  await expect(log).toContainText('Parfait')
  ctl.run('alice', 'dm-sync')
  ctl.run('alice', 'dm-read', 'bob')
  await expect(log.locator('.receipt')).toHaveText('Vu')

  // Edit and delete (encrypted events, applied by alice's client).
  const mine = log.locator('.msg', { hasText: 'Parfait' })
  await mine.hover()
  await mine.getByRole('button', { name: 'Modifier' }).click()
  await composer.fill('Parfait, merci')
  await composer.press('Enter')
  await expect(log).toContainText('Parfait, merci(modifié)')
  expect(history()).toContain('Parfait, merci (modifié)')
  const first = log.locator('.msg', { hasText: 'Salut alice' })
  await first.hover()
  await first.getByRole('button', { name: 'Supprimer' }).click()
  await expect(log).not.toContainText('Salut alice')
  expect(history()).not.toContain('Salut alice')

  // A group created in the app.
  await page.getByRole('button', { name: 'Nouveau groupe' }).click()
  await page.getByLabel('Nom du groupe (facultatif)').fill('Tarot')
  await page.getByRole('dialog').getByText('alice').click()
  await page.getByRole('button', { name: 'Créer le groupe' }).click()
  const g = page.getByLabel('Message pour Tarot')
  await g.fill('Bienvenue dans le groupe')
  await g.press('Enter')
  expect(ctl.run('alice', 'dm-history', 'Tarot')).toContain('bob : Bienvenue dans le groupe')
  ctl.run('alice', 'dm', 'Tarot', 'Merci !')
  await expect(page.getByRole('log')).toContainText('Merci !')

  // History and keys survive a restart; new messages still decrypt.
  await app.close()
  ;({ app, page } = await launchApp(userData))
  await page.locator('.dm-item', { hasText: 'alice' }).click()
  await expect(page.getByRole('log')).toContainText('Oui, de bout en bout !')
  ctl.run('alice', 'dm', 'bob', 'Toujours là ?')
  await expect(page.getByRole('log')).toContainText('Toujours là ?')
  await app.close()
})
