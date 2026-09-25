// The list of joined community servers follows the account across devices:
// end-to-end encrypted, sent with the history when a device is validated,
// kept in the recovery backup, and updated live when joining or leaving.
import { expect, test, type Page } from '@playwright/test'
import { Community, Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(19880)
const srv = new Community(19890, 'localhost:19880')
const ctl = new Ctl(id)
const password = 'motdepasse-solide'

test.beforeAll(async () => {
  await id.start()
  await srv.start()
  await ctl.account('alice')
  ctl.run('alice', 'claim', 'localhost:' + srv.port, srv.claimCode())
  await id.api('POST', '/v1/auth/register', { email: 'bob@example.com', pseudo: 'bob', password })
  await id.api('POST', '/v1/auth/verify-email', { email: 'bob@example.com', code: id.lastCode() })
})
test.afterAll(() => {
  srv.stop()
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
  await expect(page.getByRole('button', { name: 'Ajouter un ami' })).toBeVisible()
}

const serverButton = (page: Page) => page.getByRole('navigation', { name: 'Serveurs' }).getByRole('button', { name: /Serveur Quarel/ })

test('joined servers follow the account to validated and restored devices', async () => {
  // Device A joins the server and creates a recovery phrase.
  const a = await launchApp(tempDir('quarel-app-'))
  await signIn(a.page)
  const link = /quarel:\/\/\S+/.exec(ctl.run('alice', 'invite', '5'))![0]
  await a.page.getByRole('button', { name: 'Ajouter un serveur' }).click()
  await a.page.getByLabel("Lien d'invitation").fill(link)
  await a.page.getByRole('button', { name: 'Continuer' }).click()
  await a.page.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await expect(a.page.getByLabel('Membres').getByText('alice')).toBeVisible()
  await a.page.getByRole('button', { name: /Messages privés/ }).click()
  await a.page.getByRole('button', { name: 'Créer ma phrase' }).click()
  await a.page.getByRole('button', { name: 'Créer la phrase' }).click()
  await expect(a.page.getByTestId('recovery-phrase').locator('li')).toHaveCount(12)
  const phrase = (await a.page.getByTestId('recovery-phrase').locator('li').evaluateAll((lis) => lis.map((li) => li.lastChild!.textContent!))).join(' ')
  await a.page.getByText(/ai noté ma phrase/).click()
  await a.page.getByRole('button', { name: 'Terminer' }).click()

  // Device B: validated by A, it gets the server without any invite.
  const b = await launchApp(tempDir('quarel-app-'))
  await signIn(b.page)
  await expect(serverButton(b.page)).toHaveCount(0)
  const code = (await b.page.getByTestId('own-code').textContent())!
  await a.page.getByRole('button', { name: 'Valider un appareil' }).click()
  await a.page.getByLabel("Code affiché sur l'appareil").fill(code)
  await a.page.getByRole('dialog').getByRole('button', { name: 'Valider' }).click()
  await a.page.getByRole('button', { name: 'Terminer' }).click()
  await expect(serverButton(b.page)).toBeVisible({ timeout: 15000 })
  await serverButton(b.page).click()
  await expect(b.page.getByLabel('Membres').getByText('alice')).toBeVisible()

  // Device C: restored with the phrase, it gets the server too.
  const c = await launchApp(tempDir('quarel-app-'))
  await signIn(c.page)
  await c.page.getByRole('button', { name: 'Utiliser ma phrase de récupération' }).click()
  await c.page.getByLabel('Les 12 mots').fill(phrase)
  await c.page.getByRole('button', { name: 'Restaurer' }).click()
  await c.page.getByRole('button', { name: 'Terminer' }).click()
  await expect(serverButton(c.page)).toBeVisible({ timeout: 15000 })

  // Leaving on B: A and C forget the server too.
  await b.page.locator('button.sidebar-head').click()
  await b.page.getByRole('menuitem', { name: 'Quitter le serveur' }).click()
  await b.page.getByRole('dialog').getByRole('button', { name: 'Quitter' }).click()
  await expect(serverButton(b.page)).toHaveCount(0)
  await expect(serverButton(a.page)).toHaveCount(0, { timeout: 15000 })
  await expect(serverButton(c.page)).toHaveCount(0, { timeout: 15000 })
  await Promise.all([a.app.close(), b.app.close(), c.app.close()])
})
