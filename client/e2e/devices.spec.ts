// Step 4b of the desktop client: device validation and recovery phrase.
// bob uses three app instances and command-line devices (goolm): the app
// validates a Go device and the other way round, and a phrase created in the
// app restores both an app and a Go device.
import { expect, test, type Page } from '@playwright/test'
import { Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(19180)
const ctl = new Ctl(id)
const password = 'motdepasse-solide'

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

// The id of bob's device showing this verification code, as listed by a Go device.
function deviceWithCode(profile: string, code: string) {
  const line = ctl.run(profile, 'devices').split('\n').find((l) => l.includes(code))
  return line?.match(/id (\S+)/)?.[1] ?? ''
}

test('devices: validation both ways, history transfer, recovery phrase', async () => {
  // First device (app A): friends with alice, a few messages, a recovery phrase.
  const a = await launchApp(tempDir('quarel-app-'))
  await signIn(a.page)
  ctl.run('alice', 'friend-add', 'bob')
  await a.page.getByRole('button', { name: /En attente/ }).click()
  await a.page.getByRole('button', { name: 'Accepter' }).click()
  await a.page.getByRole('button', { name: 'Tous', exact: true }).click()
  await a.page.getByRole('button', { name: 'Écrire à alice' }).click()
  const composer = a.page.getByLabel('Message pour alice')
  await composer.fill('Premier message')
  await composer.press('Enter')
  await expect(a.page.getByRole('log')).toContainText('Premier message')
  ctl.run('alice', 'dm', 'bob', 'Bien reçu')
  await expect(a.page.getByRole('log')).toContainText('Bien reçu')

  await a.page.getByRole('button', { name: 'Créer ma phrase' }).click()
  await a.page.getByRole('button', { name: 'Créer la phrase' }).click()
  await expect(a.page.getByTestId('recovery-phrase').locator('li')).toHaveCount(12)
  const words = await a.page.getByTestId('recovery-phrase').locator('li').evaluateAll((lis) => lis.map((li) => li.lastChild!.textContent!))
  expect(words).toHaveLength(12)
  const phrase = words.join(' ')
  await a.page.getByText('J’ai noté ma phrase de récupération').or(a.page.getByText("J'ai noté ma phrase de récupération")).click()
  await a.page.getByRole('button', { name: 'Terminer' }).click()
  await expect(a.page.getByRole('button', { name: 'Créer ma phrase' })).toBeHidden()

  // A Go device of bob: the app validates it with its code; history follows.
  ctl.run('bob2', 'login', 'bob', 'pc-bob2')
  const status = ctl.run('bob2', 'e2e')
  expect(status).toContain('en attente de validation')
  const goCode = status.match(/Code de vérification de cet appareil : (\S+)/)![1]
  await a.page.getByRole('button', { name: 'Valider un appareil' }).click()
  const dialog = a.page.getByRole('dialog')
  await dialog.getByLabel('Code affiché sur l\'appareil').fill('AAAA-BBBB-CCCC-DDDD')
  await dialog.getByRole('button', { name: 'Valider' }).click()
  await expect(dialog).toContainText('ne correspond pas')
  await dialog.getByLabel('Code affiché sur l\'appareil').fill(goCode.toLowerCase())
  await dialog.getByRole('button', { name: 'Valider' }).click()
  await expect(dialog).toContainText('« pc-bob2 » est validé')
  await dialog.getByRole('button', { name: 'Terminer' }).click()
  const synced = ctl.run('bob2', 'dm-sync')
  expect(synced).toContain('cet appareil a été validé')
  const goHistory = ctl.run('bob2', 'dm-history', 'alice')
  expect(goHistory).toContain('bob : Premier message')
  expect(goHistory).toContain('alice : Bien reçu')
  expect(ctl.run('bob2', 'recovery-status')).toContain('Sauvegarde active') // the backup key came with the approval
  await a.app.close()

  // Second app (B): unvalidated, validated by the Go device.
  const b = await launchApp(tempDir('quarel-app-'))
  await signIn(b.page)
  await expect(b.page.getByTestId('unvalidated')).toBeVisible()
  const appCode = (await b.page.getByTestId('own-code').textContent())!
  ctl.run('bob2', 'device-approve', deviceWithCode('bob2', appCode), appCode)
  await expect(b.page.getByTestId('unvalidated')).toBeHidden()
  await b.page.locator('.dm-item', { hasText: 'alice' }).click()
  await expect(b.page.getByRole('log')).toContainText('Premier message')
  await expect(b.page.getByRole('log')).toContainText('Bien reçu')
  ctl.run('alice', 'dm', 'bob', 'Nouveau message')
  await expect(b.page.getByRole('log')).toContainText('Nouveau message')
  await b.page.getByLabel('Message pour alice').fill('Depuis le deuxième appareil')
  await b.page.getByLabel('Message pour alice').press('Enter')
  await expect(b.page.getByRole('log')).toContainText('Depuis le deuxième appareil')
  await expect.poll(() => ctl.run('alice', 'dm-history', 'bob')).toContain('bob : Depuis le deuxième appareil')
  await b.app.close()

  // Third app (C): restored with the phrase, alone.
  const c = await launchApp(tempDir('quarel-app-'))
  await signIn(c.page)
  await expect(c.page.getByTestId('unvalidated')).toBeVisible()
  await c.page.getByRole('button', { name: 'Utiliser ma phrase de récupération' }).click()
  const box = c.page.getByLabel('Les 12 mots')
  await box.fill(words.slice(0, 11).join(' ') + ' ' + (words[11] === words[0] ? words[1] : words[0]))
  await c.page.getByRole('button', { name: 'Restaurer' }).click()
  await expect(c.page.getByRole('dialog')).toContainText(/faute de frappe|n’ouvre pas|Mot inconnu/)
  await box.fill(phrase.toUpperCase())
  await c.page.getByRole('button', { name: 'Restaurer' }).click()
  await expect(c.page.getByRole('dialog')).toContainText('Cet appareil est validé')
  await c.page.getByRole('button', { name: 'Terminer' }).click()
  await expect(c.page.getByTestId('unvalidated')).toBeHidden()
  await c.page.locator('.dm-item', { hasText: 'alice' }).click()
  const log = c.page.getByRole('log')
  await expect(log).toContainText('Premier message')
  await expect(log).toContainText('Nouveau message') // backed up by B (or the Go device)
  await expect(log).toContainText('Depuis le deuxième appareil')

  // Settings list the devices with their status.
  await c.page.getByRole('button', { name: 'Paramètres' }).click()
  await c.page.getByRole('button', { name: 'Appareils' }).click()
  await expect(c.page.getByTestId('sessions').getByText('Validé', { exact: true })).toHaveCount(4)
  await c.page.getByRole('button', { name: 'Récupération' }).click()
  await expect(c.page.getByTestId('recovery-status')).toContainText('Sauvegarde active')
  await c.app.close()

  // The phrase made in the app also restores a Go device (vodozemac → goolm backup).
  ctl.run('bob3', 'login', 'bob', 'pc-bob3')
  ctl.run('bob3', 'e2e')
  const restored = ctl.run('bob3', 'recovery-restore', ...phrase.split(' '))
  expect(restored).toContain('Compte restauré')
  expect(ctl.run('bob3', 'dm-history', 'alice')).toContain('bob : Depuis le deuxième appareil')
})
