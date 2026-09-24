// Step 1 of the desktop client: accounts. Real app, real Identity service.
import { expect, test } from '@playwright/test'
import { Identity, launchApp, tempDir, totp } from './fixtures'

const id = new Identity(18380)
const password = 'motdepasse-solide'
const userData = tempDir('quarel-app-')

test.beforeAll(() => id.start())
test.afterAll(() => id.stop())

test('accounts: register, verify, stay signed in, 2FA, devices, reset', async () => {
  let { app, page } = await launchApp(userData)

  // Point the app at the test identity service.
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('example.invalid')
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await expect(page.getByText('Aucun service d’identité Quarel ne répond à cette adresse.')).toBeVisible()
  await page.getByLabel('Adresse du service').fill('127.0.0.1:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await expect(page.getByTestId('identity-label')).toHaveText('127.0.0.1:' + id.port)

  // Create an account; client-side checks first.
  await page.getByRole('button', { name: 'Créer un compte' }).click()
  await page.getByLabel('Email').fill('alice@example.com')
  await page.getByLabel('Pseudo').fill('a')
  await page.getByLabel('Mot de passe', { exact: true }).fill('court')
  await page.getByRole('button', { name: 'Créer mon compte' }).click()
  await expect(page.getByText('Au moins 10 caractères.', { exact: true })).toBeVisible()
  await page.getByLabel('Pseudo').fill('alice')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Créer mon compte' }).click()

  // Verify the email: wrong code, then the right one signs in directly.
  await expect(page.getByRole('heading', { name: 'Vérifier votre email' })).toBeVisible()
  await page.getByLabel('Code reçu par email').fill('000000')
  await page.getByRole('button', { name: 'Vérifier' }).click()
  await expect(page.getByText('Code invalide ou expiré')).toBeVisible()
  await page.getByLabel('Code reçu par email').fill(id.lastCode())
  await page.getByRole('button', { name: 'Vérifier' }).click()
  await expect(page.locator('.userbar .name', { hasText: 'alice' })).toBeVisible()
  await expect(page.locator('.userbar .handle')).toHaveAttribute('title', 'alice@localhost:' + id.port)

  // The session survives a restart.
  await app.close()
  ;({ app, page } = await launchApp(userData))
  await expect(page.locator('.userbar .name', { hasText: 'alice' })).toBeVisible()

  // Another device signs in, then 2FA is turned on (through the API for now).
  const phone = await id.login('alice', password)
  const { secret } = await id.api<{ secret: string }>('POST', '/v1/me/2fa/setup', { password }, phone.session_token)
  const { backup_codes } = await id.api<{ backup_codes: string[] }>('POST', '/v1/me/2fa/enable', { code: totp(secret) }, phone.session_token)

  // Devices: both sessions listed; disconnect the other one.
  await page.getByRole('button', { name: 'Paramètres' }).click()
  await page.getByRole('button', { name: 'Appareils' }).click()
  const sessions = page.getByTestId('sessions')
  await expect(sessions.getByText('Cet appareil')).toBeVisible()
  await expect(sessions.getByText('Téléphone de test')).toBeVisible()
  await sessions.getByRole('button', { name: 'Déconnecter' }).click()
  await expect(sessions.getByText('Téléphone de test')).toHaveCount(0)

  // Sign out, then sign in again: wrong password, then 2FA.
  await page.getByRole('button', { name: 'Se déconnecter' }).click()
  await page.getByRole('dialog', { name: 'Se déconnecter ?' }).getByRole('button', { name: 'Se déconnecter' }).click()
  await expect(page.getByRole('heading', { name: 'Connexion' })).toBeVisible()
  await expect(page.getByLabel('Email ou pseudo')).toHaveValue('alice@example.com')
  await page.getByLabel('Mot de passe', { exact: true }).fill('mauvais-mot-de-passe')
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByText('Identifiant ou mot de passe incorrect.')).toBeVisible()
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByRole('heading', { name: 'Double authentification' })).toBeVisible()
  // The enabling code used the current step; the next one is accepted (±1 step).
  await page.getByLabel('Code', { exact: true }).fill(totp(secret, Date.now() + 30_000))
  await page.getByRole('button', { name: 'Valider' }).click()
  await expect(page.locator('.userbar .name', { hasText: 'alice' })).toBeVisible()

  // Session closed elsewhere: the app notices at the next start.
  const other = await id.login('alice', password, backup_codes[0])
  const list = await id.api<{ id: string; current: boolean }[]>('GET', '/v1/me/sessions', undefined, other.session_token)
  for (const s of list.filter((s) => !s.current)) {
    await id.api('DELETE', '/v1/me/sessions/' + s.id, undefined, other.session_token)
  }
  await app.close()
  ;({ app, page } = await launchApp(userData))
  await expect(page.getByText('Votre session a expiré ou a été fermée depuis un autre appareil.')).toBeVisible()

  // Forgotten password: code by email, and 2FA still required.
  await page.getByRole('button', { name: 'Mot de passe oublié ?' }).click()
  await page.getByLabel('Email du compte').fill('alice@example.com')
  await page.getByRole('button', { name: 'Recevoir un code' }).click()
  await expect(page.getByRole('heading', { name: 'Nouveau mot de passe' })).toBeVisible()
  await page.getByLabel('Code reçu par email').fill(id.lastCode())
  await page.getByLabel('Nouveau mot de passe').fill('nouveau-motdepasse')
  await page.getByRole('button', { name: 'Changer le mot de passe' }).click()
  await expect(page.getByText('Votre compte est protégé par la double authentification : entrez aussi un code.')).toBeVisible()
  await page.getByLabel(/Code de double authentification/).fill(backup_codes[1])
  await page.getByRole('button', { name: 'Changer le mot de passe' }).click()
  await expect(page.getByText('Mot de passe changé.')).toBeVisible()

  // New password + a backup code.
  await page.getByLabel('Mot de passe', { exact: true }).fill('nouveau-motdepasse')
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await page.getByRole('button', { name: 'Utiliser un code de secours' }).click()
  await page.getByLabel('Code de secours').fill(backup_codes[2])
  await page.getByRole('button', { name: 'Valider' }).click()
  await expect(page.locator('.userbar .name', { hasText: 'alice' })).toBeVisible()

  await app.close()
})
