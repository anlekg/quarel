// Limits set by an identity service: sign-up by invitation, approved
// community servers only, block list. Real app, real services, real
// administration pages.
import { expect, test, type Page } from '@playwright/test'
import { adminAPI, Community, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(18780, { QUAREL_REGISTRATION: 'invite', QUAREL_SERVER_POLICY: 'approved', QUAREL_USER_INVITES: '1', QUAREL_ADMIN_ADDR: '127.0.0.1:18781' })
const srv = new Community(18790, 'localhost:18780', { QUAREL_ADMIN_ADDR: '127.0.0.1:18791' })
const password = 'motdepasse-solide'

test.beforeAll(async () => {
  await id.start()
  await srv.start()
})
test.afterAll(() => {
  srv.stop()
  id.stop()
})

async function useTestIdentity(page: Page) {
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
}

test('identity limits: invitations, approved servers, block list', async () => {
  const idAdmin = await adminAPI('http://127.0.0.1:18781')
  const srvAdmin = await adminAPI('http://127.0.0.1:18791')
  const inv = await idAdmin<{ code: string }>('POST', '/x/invites', { note: 'CP', max_uses: 1, valid_days: 7 })

  let { app, page } = await launchApp(tempDir('quarel-app-'))
  await useTestIdentity(page)

  // Sign up by invitation.
  await page.getByRole('button', { name: 'Créer un compte' }).click()
  const code = page.getByLabel("Code d'invitation")
  await expect(code).toBeVisible()
  await code.fill('mauvaiscode')
  await page.getByLabel('Email').fill('hote@example.com')
  await page.getByLabel('Pseudo').fill('hote')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Créer mon compte' }).click()
  await expect(page.getByText('Invitation inconnue, expirée')).toBeVisible()
  await code.fill(inv.code)
  await page.getByRole('button', { name: 'Créer mon compte' }).click()
  await expect(page.getByRole('heading', { name: 'Vérifier votre email' })).toBeVisible()
  await page.getByLabel('Code reçu par email').fill(id.lastCode())
  await page.getByRole('button', { name: 'Vérifier' }).click()
  await expect(page.getByRole('heading', { name: 'Bienvenue, hote' })).toBeVisible()

  // The user's own invitation (quota 1).
  await page.getByRole('button', { name: 'Paramètres' }).click()
  await page.getByRole('button', { name: 'Invitations' }).click()
  await expect(page.getByText('1 invitation restante')).toBeVisible()
  await page.getByRole('button', { name: 'Créer une invitation' }).click()
  await expect(page.getByTestId('my-invites').locator('.card-row')).toHaveCount(1)
  await expect(page.getByRole('button', { name: 'Créer une invitation' })).toBeDisabled()
  await page.getByRole('button', { name: 'Fermer les paramètres' }).click()

  // The community server is not approved yet.
  const sid = /server_id=([a-z2-7]{26})/.exec(srv.log)![1]
  const link = `quarel://localhost:${srv.port}/${srv.claimCode()}?sid=${sid}&claim=1`
  await page.getByRole('button', { name: 'Rejoindre un serveur' }).click()
  await page.getByLabel("Lien d'invitation").fill(link)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await page.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await expect(page.getByText("n’est pas approuvé par votre service d’identité")).toBeVisible()

  // The host asks for approval from its administration page; the operator approves.
  let services = await srvAdmin<{ issuer: string; server_policy: string; status: string }[]>('GET', '/x/identity')
  expect(services[0]).toMatchObject({ issuer: 'localhost:18780', server_policy: 'approved', status: 'none' })
  await srvAdmin('POST', '/x/identity/request', { issuer: 'localhost:18780', contact: 'hote@example.com' })
  const known = await idAdmin<{ servers: { id: string; status: string; contact: string }[] }>('GET', '/x/servers')
  expect(known.servers).toEqual([expect.objectContaining({ id: sid, status: 'pending', contact: 'hote@example.com' })])
  await idAdmin('POST', `/x/servers/${sid}/approve`)
  services = await srvAdmin('GET', '/x/identity')
  expect(services[0].status).toBe('approved')

  // Now joining works (with a token sealed for this server).
  await page.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await expect(page.getByLabel('Message pour #général')).toBeVisible()

  // Blocked by the operator: the app refuses the server at next start.
  await idAdmin('POST', '/x/servers/block', { id: sid, reason: 'contenus illicites' })
  await app.close()
  ;({ app, page } = await launchApp(tempDir('quarel-app-2-')))
  await useTestIdentity(page)
  await page.getByLabel('Email ou pseudo').fill('hote')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await page.getByRole('button', { name: 'Rejoindre un serveur' }).click()
  await page.getByLabel("Lien d'invitation").fill(`quarel://localhost:${srv.port}/abcdefghij?sid=${sid}`)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await expect(page.getByText('Ce serveur est bloqué par votre service d’identité. Raison : contenus illicites')).toBeVisible()
  await app.close()
})
