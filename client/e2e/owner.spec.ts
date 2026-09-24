// The host becomes owner from the app with the owner link of the administration page.
import { expect, test } from '@playwright/test'
import { Community, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(18680)
const srv = new Community(18690, 'localhost:18680')
const password = 'motdepasse-solide'

test.beforeAll(async () => {
  await id.start()
  await srv.start()
  await id.api('POST', '/v1/auth/register', { email: 'hote@example.com', pseudo: 'hote', password })
  await id.api('POST', '/v1/auth/verify-email', { email: 'hote@example.com', code: id.lastCode() })
})
test.afterAll(() => {
  srv.stop()
  id.stop()
})

test('owner link: the host claims the server from the app', async () => {
  const { app, page } = await launchApp(tempDir('quarel-app-'))
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill('hote')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()

  const sid = /server_id=([a-z2-7]{26})/.exec(srv.log)![1]
  const link = `quarel://localhost:18690/${srv.claimCode()}?sid=${sid}&claim=1`

  await page.getByRole('button', { name: 'Ajouter un serveur' }).click()
  await page.getByLabel("Lien d'invitation").fill(link)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await expect(page.getByText('Lien propriétaire : vous deviendrez propriétaire de ce serveur.')).toBeVisible()
  await page.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await expect(page.getByLabel('Membres').getByTitle('Propriétaire')).toBeVisible()
  await page.locator('button.sidebar-head').click()
  await expect(page.getByRole('menuitem', { name: 'Inviter des personnes' })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: 'Quitter le serveur' })).toHaveCount(0)
  await app.close()
})
