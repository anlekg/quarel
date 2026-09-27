// Step 6a of the desktop client: the person's own settings — profile, pseudo,
// email, password, two-factor authentication, presence, privacy and blocked
// people, microphone/camera choice, account deletion.
import { chromium, expect, test, type Page } from '@playwright/test'
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
  await expect.poll(() => ctl.run('alice', 'profile', 'bobby'), { timeout: 15_000 }).toContain('Joueur de tarot')

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

  // A passkey: added on the identity service's page (in Chromium with a
  // virtual authenticator), then used to sign in from another app.
  const browser = await chromium.launch()
  const keyPage = await (await browser.newContext()).newPage()
  const cdp = await keyPage.context().newCDPSession(keyPage)
  await cdp.send('WebAuthn.enable')
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: {
    protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true,
  } })
  const captureOpen = (p: Page) => p.evaluate(() => {
    const w = window as unknown as { __opened?: string; open: (u: string) => null }
    w.__opened = ''
    w.open = (u: string) => { w.__opened = u; return null }
  })
  const opened = (p: Page) => p.evaluate(() => (window as unknown as { __opened: string }).__opened)
  await captureOpen(page)
  await settings.getByRole('button', { name: 'Ajouter une clé' }).click()
  dialog = page.getByRole('dialog', { name: "Ajouter une clé d'accès" })
  await dialog.getByLabel('Nom de la clé').fill('Clé de test')
  await dialog.getByLabel('Mot de passe actuel').fill(newPassword)
  await dialog.getByRole('button', { name: 'Continuer' }).click()
  await expect.poll(() => opened(page)).toMatch(/\/passkey\/#/)
  await keyPage.goto(await opened(page))
  await keyPage.getByRole('button', { name: 'Utiliser ma clé' }).click()
  await expect(keyPage.getByRole('status')).toContainText('Clé ajoutée')
  await expect(settings.getByTestId('passkeys')).toContainText('Clé de test', { timeout: 10_000 })

  const other = await launchApp(tempDir('quarel-app-'))
  await other.page.getByRole('button', { name: 'Changer' }).click()
  await other.page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await other.page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await other.page.getByLabel('Email ou pseudo').fill('bobby')
  await other.page.getByLabel('Mot de passe', { exact: true }).fill(newPassword)
  await other.page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(other.page.getByText('Double authentification')).toBeVisible()
  await captureOpen(other.page)
  await other.page.getByRole('button', { name: 'Utiliser une clé d’accès' }).click()
  await expect(other.page.getByTestId('passkey-wait')).toBeVisible()
  await expect.poll(() => opened(other.page)).toMatch(/\/passkey\/#/)
  await keyPage.goto(await opened(other.page))
  await keyPage.getByRole('button', { name: 'Utiliser ma clé' }).click()
  await expect(keyPage.getByRole('status')).toContainText('C’est fait')
  await expect(other.page.getByRole('button', { name: 'Ajouter un ami' })).toBeVisible({ timeout: 15_000 }) // signed in
  await other.app.close()
  await browser.close()
  await settings.getByRole('button', { name: 'Profil' }).click()
  await settings.getByRole('button', { name: 'Sécurité' }).click()
  await expect(settings.getByTestId('passkeys')).toContainText('utilisée le')
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
  await expect.poll(() => ctl.run('alice', 'friends'), { timeout: 15_000 }).toContain('bobby')
  await settings.getByLabel('Pseudo à bloquer').fill('alice')
  await settings.getByRole('button', { name: 'Bloquer', exact: true }).click()
  await expect(settings.getByTestId('blocked')).toContainText('alice')
  await expect.poll(() => ctl.run('alice', 'friends'), { timeout: 15_000 }).not.toContain('bobby')
  await settings.getByTestId('blocked').getByRole('button', { name: 'Débloquer' }).click()
  await expect(settings).toContainText('Personne n’est bloqué'.replace('’', "'"))
  // Nobody may send a friend request: alice (no longer a friend) is refused, then allowed again.
  const who = settings.getByLabel('Qui peut vous demander en ami')
  await expect(who).toHaveValue('everyone')
  await who.selectOption('nobody')
  await expect.poll(() => ctl.run('alice', 'friend-add', 'bobby'), { timeout: 10_000 }).toMatch(/friend_requests_closed|does not accept/)
  await who.selectOption('everyone')
  expect(ctl.run('alice', 'friend-add', 'bobby')).toContain('demande envoyée')

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

  // Closing leaves Quarel in the notification area (option, off in tests by default).
  await settings.getByRole('button', { name: 'À propos' }).click()
  const tray = settings.getByRole('checkbox', { name: /Réduire dans la zone de notification/ })
  await expect(tray).not.toBeChecked()
  await tray.click()
  await expect(tray).toBeChecked()
  const visible = () => app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().map((w) => w.isVisible()))
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].close())
  await expect.poll(visible).toEqual([false]) // hidden, still running
  await page.evaluate(() => (window as unknown as { quarelDesktop: { showWindow(): Promise<void> } }).quarelDesktop.showWindow())
  await expect.poll(visible).toEqual([true])
  await tray.click()
  await expect(tray).not.toBeChecked()

  // Account deletion: back to the sign-in screen; the account is gone.
  await settings.getByRole('button', { name: 'Sécurité' }).click()
  await settings.getByRole('button', { name: 'Supprimer', exact: true }).click()
  dialog = page.getByRole('dialog', { name: 'Supprimer mon compte' })
  await dialog.getByLabel('Mot de passe', { exact: true }).fill(newPassword)
  await dialog.getByLabel('Tapez « bobby » pour confirmer').fill('bobby')
  await dialog.getByRole('button', { name: 'Supprimer définitivement' }).click()
  await expect(page.getByRole('button', { name: 'Se connecter' })).toBeVisible()
  await expect.poll(() => ctl.run('alice', 'profile', 'bobby'), { timeout: 15_000 }).toMatch(/introuvable|not_found|404/i)
  await app.close()
})
