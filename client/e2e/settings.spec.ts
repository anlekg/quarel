// Step 6a of the desktop client: the person's own settings — profile, pseudo,
// email, password, two-factor authentication, presence, privacy and blocked
// people, microphone/camera choice, account deletion.
import { expect, test, type Page } from '@playwright/test'
import { Ctl, Identity, launchApp, tempDir, totp } from './fixtures'

const id = new Identity(19480)
const ctl = new Ctl(id)
const password = 'motdepasse-solide'
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64')

test.beforeAll(async () => {
  await id.start()
  await ctl.account('alice')
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
  await expect(page.getByRole('button', { name: 'Ajouter un ami' })).toBeVisible()
}

test('settings: profile, security, presence, privacy, devices, deletion', async () => {
  const { app, page } = await launchApp(tempDir('quarel-app-'))
  await signIn(page)
  ctl.run('alice', 'friend-add', 'bob')
  await page.getByRole('button', { name: /En attente/ }).click()
  await page.getByRole('button', { name: 'Accepter' }).click()

  // Presence: alice sees bob as "do not disturb".
  await page.getByRole('button', { name: 'Statut : En ligne' }).click()
  await page.getByRole('menuitemradio', { name: /Ne pas déranger/ }).click()
  await expect(page.getByRole('button', { name: 'Statut : Ne pas déranger' })).toBeVisible()
  await expect.poll(() => ctl.run('alice', 'friends')).toContain('ne pas déranger')

  const settings = page.getByRole('dialog', { name: 'Paramètres' })
  await page.getByRole('button', { name: 'Paramètres' }).click()

  // Profile: picture, pseudo (once a day), about me.
  await settings.getByLabel('Image de profil').setInputFiles({ name: 'moi.png', mimeType: 'image/png', buffer: png })
  await expect(settings.locator('img.avatar').first()).toBeVisible()
  await expect.poll(() => ctl.run('alice', 'profile', 'bob')).not.toContain('(aucun)')
  await settings.getByLabel('Pseudo').fill('bobby')
  await settings.getByRole('button', { name: 'Enregistrer le pseudo' }).click()
  await expect(settings).toContainText('Pseudo changé')
  await expect(page.locator('.userbar .name')).toHaveText('bobby')
  await settings.getByLabel('Pseudo').fill('robert')
  await settings.getByRole('button', { name: 'Enregistrer le pseudo' }).click()
  await expect(settings).toContainText('une fois par jour')
  await settings.getByLabel('À propos de moi').fill('Joueur de tarot')
  await settings.getByRole('button', { name: 'Enregistrer la présentation' }).click()
  await expect(settings).toContainText('Présentation enregistrée')
  expect(ctl.run('alice', 'profile', 'bobby')).toContain('Joueur de tarot')

  // Security: email, password, two-factor authentication.
  await settings.getByRole('button', { name: 'Sécurité' }).click()
  await settings.getByRole('button', { name: 'Modifier' }).first().click()
  let dialog = page.getByRole('dialog', { name: "Changer d'email" })
  await dialog.getByLabel('Nouvelle adresse email').fill('robert@example.com')
  await dialog.getByLabel('Mot de passe actuel').fill('mauvais-mot-de-passe')
  await dialog.getByRole('button', { name: 'Envoyer un code' }).click()
  await expect(dialog).toContainText('Mot de passe incorrect')
  await dialog.getByLabel('Mot de passe actuel').fill(password)
  await dialog.getByRole('button', { name: 'Envoyer un code' }).click()
  await expect(dialog.getByLabel('Code reçu par email')).toBeVisible()
  await dialog.getByLabel('Code reçu par email').fill(id.lastCode())
  await dialog.getByRole('button', { name: 'Confirmer' }).click()
  await expect(settings).toContainText('robert@example.com')

  const newPassword = 'nouveau-mot-de-passe-solide'
  await settings.getByRole('button', { name: 'Modifier' }).nth(1).click()
  dialog = page.getByRole('dialog', { name: 'Changer de mot de passe' })
  await dialog.getByLabel('Mot de passe actuel').fill(password)
  await dialog.getByLabel('Nouveau mot de passe', { exact: true }).fill(newPassword)
  await dialog.getByLabel('Nouveau mot de passe (encore)').fill(newPassword)
  await dialog.getByRole('button', { name: 'Changer' }).click()
  await expect(settings).toContainText('Mot de passe changé')

  await settings.getByRole('button', { name: 'Activer' }).click()
  dialog = page.getByRole('dialog', { name: 'Activer la double authentification' })
  await dialog.getByLabel('Mot de passe actuel').fill(newPassword)
  await dialog.getByRole('button', { name: 'Continuer' }).click()
  await expect(dialog.getByRole('img', { name: /QR code/ })).toBeVisible()
  const secret = (await dialog.getByTestId('totp-secret').textContent())!
  await dialog.getByLabel('Code à 6 chiffres').fill(totp(secret))
  await dialog.getByRole('button', { name: 'Activer' }).click()
  const codes = page.getByRole('dialog', { name: 'Codes de secours' }).getByTestId('backup-codes').locator('li')
  await expect(codes).toHaveCount(10)
  const backup = (await codes.first().textContent())!
  await page.getByRole('button', { name: 'Terminer' }).click()
  await expect(settings).toContainText('Activée')
  await settings.getByRole('button', { name: 'Désactiver' }).click()
  dialog = page.getByRole('dialog', { name: 'Désactiver la double authentification' })
  await dialog.getByLabel('Mot de passe actuel').fill(newPassword)
  await dialog.getByLabel(/Code de l'application/).fill(backup)
  await dialog.getByRole('button', { name: 'Désactiver' }).click()
  await expect(settings).toContainText('Double authentification désactivée')

  // Privacy: typing indicator off, block alice (removed from friends), unblock.
  await settings.getByRole('button', { name: 'Confidentialité' }).click()
  const typingBox = settings.getByRole('checkbox', { name: /Montrer quand j'écris/ })
  await expect(typingBox).toBeChecked()
  await typingBox.click()
  await expect(typingBox).not.toBeChecked()
  expect(ctl.run('alice', 'friends')).toContain('bobby')
  await settings.getByLabel('Pseudo à bloquer').fill('alice')
  await settings.getByRole('button', { name: 'Bloquer', exact: true }).click()
  await expect(settings.getByTestId('blocked')).toContainText('alice')
  expect(ctl.run('alice', 'friends')).not.toContain('bobby')
  await settings.getByTestId('blocked').getByRole('button', { name: 'Débloquer' }).click()
  await expect(settings).toContainText('Personne n’est bloqué'.replace('’', "'"))

  // Microphone, speakers and camera (fake devices).
  await settings.getByRole('button', { name: 'Voix et vidéo' }).click()
  await settings.getByRole('button', { name: 'Tester le micro' }).click()
  await expect(settings.getByRole('meter', { name: 'Niveau du micro' })).toBeVisible()
  await settings.getByRole('button', { name: 'Arrêter le test' }).click()
  await expect(settings.getByLabel('Micro').locator('option')).not.toHaveCount(1)
  await settings.getByRole('button', { name: 'Aperçu de la caméra' }).click()
  await expect.poll(() => settings.locator('video.cam-preview').evaluate((v: HTMLVideoElement) => v.videoWidth)).toBeGreaterThan(0)
  const cams = settings.getByLabel('Caméra').locator('option')
  await settings.getByLabel('Caméra').selectOption({ index: (await cams.count()) - 1 })
  expect(await page.evaluate(() => localStorage.getItem('quarel.pref.media.videoinput'))).not.toBe('""')

  // Account deletion: back to the sign-in screen; the account is gone.
  await settings.getByRole('button', { name: 'Sécurité' }).click()
  await settings.getByRole('button', { name: 'Supprimer', exact: true }).click()
  dialog = page.getByRole('dialog', { name: 'Supprimer mon compte' })
  await dialog.getByLabel('Mot de passe', { exact: true }).fill(newPassword)
  await dialog.getByLabel('Tapez « bobby » pour confirmer').fill('bobby')
  await dialog.getByRole('button', { name: 'Supprimer définitivement' }).click()
  await expect(page.getByRole('button', { name: 'Se connecter' })).toBeVisible()
  expect(ctl.run('alice', 'profile', 'bobby')).toMatch(/introuvable|not_found|404/i)
  await app.close()
})
