// Step 5 of the desktop client: calls between friends. The app calls the Go
// test client and the other way round (audio both ways, direct or through the
// TURN relay), and two apps call each other with the camera on and share
// their screens.
import { expect, test, type Page } from '@playwright/test'
import { Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(19380, {
  QUAREL_TURN: 'on', QUAREL_TURN_LISTEN: '127.0.0.1:19478', QUAREL_TURN_PUBLIC_IP: '127.0.0.1', QUAREL_TURN_PORTS: '50100-50150',
  QUAREL_TURN_ALLOW_PRIVATE: '1', // tests run on the loopback address
})
const ctl = new Ctl(id)
const password = 'motdepasse-solide'

async function account(pseudo: string) {
  await id.api('POST', '/v1/auth/register', { email: pseudo + '@example.com', pseudo, password })
  await id.api('POST', '/v1/auth/verify-email', { email: pseudo + '@example.com', code: id.lastCode() })
}

test.beforeAll(async () => {
  await id.start()
  await ctl.account('alice')
  ctl.run('alice', 'e2e')
  await account('bob')
  await account('carol')
})
test.afterAll(() => {
  id.stop()
  ctl.stop()
})

async function signIn(page: Page, pseudo: string) {
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill(pseudo)
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByRole('button', { name: 'Ajouter un ami' })).toBeVisible()
}

async function acceptFriend(page: Page, pseudo: string) {
  await page.getByRole('button', { name: /En attente/ }).click()
  await page.getByRole('button', { name: 'Accepter' }).click()
  await page.getByRole('button', { name: 'Tous', exact: true }).click()
  await page.getByRole('button', { name: 'Écrire à ' + pseudo }).click()
}

test('calls: app and Go client both ways, relay, app to app with video', async () => {
  const bob = await launchApp(tempDir('quarel-app-'))
  await signIn(bob.page, 'bob')
  ctl.run('alice', 'friend-add', 'bob')
  await acceptFriend(bob.page, 'alice')
  const bar = bob.page.getByTestId('callbar')

  // 1. The app calls alice (Go client answering automatically): audio both ways, direct.
  const listener = ctl.listen('alice', 'call-listen', '--once', '--seconds', '6')
  await new Promise((r) => setTimeout(r, 800))
  await bob.page.getByRole('button', { name: 'Appeler alice' }).click()
  await expect(bar).toHaveAttribute('data-status', 'active', { timeout: 20000 })
  await expect(bob.page.getByTestId('call-panel')).toBeVisible()
  await expect(bar).toHaveAttribute('data-path', /local|direct/)
  await expect.poll(async () => Number(await bar.getAttribute('data-received'))).toBeGreaterThan(1000)
  await expect(bar).toHaveAttribute('data-status', 'ended', { timeout: 15000 }) // alice hangs up after 6 s
  await expect.poll(() => listener.out()).toMatch(/audio reçu ✔/)
  listener.stop()
  await expect(bar).toBeHidden({ timeout: 10000 })

  // 2. alice calls the app through the relay only; bob answers.
  const caller = ctl.listen('alice', 'call', 'bob', '--seconds', '6', '--relay-only')
  const incoming = bob.page.getByRole('alertdialog', { name: 'Appel de alice' })
  await expect(incoming).toBeVisible({ timeout: 15000 })
  await incoming.getByRole('button', { name: 'Répondre' }).click()
  await expect(bar).toHaveAttribute('data-status', 'active', { timeout: 20000 })
  await expect(bar).toHaveAttribute('data-path', 'relay')
  await expect.poll(async () => Number(await bar.getAttribute('data-received'))).toBeGreaterThan(1000)
  await expect.poll(() => caller.out(), { timeout: 20000 }).toContain('chemin par le relais TURN')
  expect(caller.out()).toMatch(/audio reçu ✔/)
  caller.stop()
  await expect(bar).toBeHidden({ timeout: 15000 })

  // 3. A declined call.
  const declined = ctl.listen('alice', 'call', 'bob', '--seconds', '3')
  await expect(incoming).toBeVisible({ timeout: 15000 })
  await incoming.getByRole('button', { name: 'Refuser' }).click()
  await expect.poll(() => declined.out(), { timeout: 10000 }).toContain('a refusé')
  declined.stop()

  // 4. Two apps: bob calls carol, both turn their camera on.
  const carol = await launchApp(tempDir('quarel-app-'))
  await signIn(carol.page, 'carol')
  await bob.page.locator('.dm-item', { hasText: 'Amis' }).click()
  await bob.page.getByRole('button', { name: 'Ajouter un ami' }).click()
  await bob.page.getByLabel("Pseudo de l'ami").fill('carol')
  await bob.page.getByRole('button', { name: 'Envoyer la demande' }).click()
  await acceptFriend(carol.page, 'bob')
  await bob.page.getByRole('button', { name: 'Tous', exact: true }).click()
  await bob.page.getByRole('button', { name: 'Écrire à carol' }).click()
  await bob.page.getByRole('button', { name: 'Appeler carol' }).click()
  const ring = carol.page.getByRole('alertdialog', { name: 'Appel de bob' })
  await expect(ring).toBeVisible({ timeout: 15000 })
  await ring.getByRole('button', { name: 'Répondre' }).click()
  const carolBar = carol.page.getByTestId('callbar')
  await expect(bar).toHaveAttribute('data-status', 'active', { timeout: 20000 })
  await expect(carolBar).toHaveAttribute('data-status', 'active', { timeout: 20000 })
  await expect.poll(async () => Number(await carolBar.getAttribute('data-received'))).toBeGreaterThan(1000)
  await bob.page.getByRole('button', { name: 'Activer la caméra' }).click()
  await expect(carol.page.getByTestId('call-panel').locator('video')).toHaveCount(1, { timeout: 10000 }) // bob's camera
  await expect.poll(() => carol.page.getByTestId('call-panel').locator('video').evaluate((v: HTMLVideoElement) => v.videoWidth)).toBeGreaterThan(0)
  await bob.page.getByRole('button', { name: 'Couper le micro' }).click()
  await expect(carol.page.getByTestId('call-panel').locator('.call-name').first()).toHaveText('bob') // mute icon shown next to the name
  await bob.page.getByRole('button', { name: 'Couper la caméra' }).click()
  await expect(carol.page.getByTestId('call-panel').locator('video')).toHaveCount(0)
  // Screen sharing (renegotiated over the call's data channel), each way in
  // turn: under Xvfb, a second app finds no screen while the first captures it.
  const shareScreen = async (page: Page) => {
    await page.getByRole('button', { name: 'Partager l’écran' }).click()
    await page.getByRole('dialog', { name: "Partager l'écran" }).locator('.picker-grid button').first().click()
    await expect(page.getByTestId('call-my-screen')).toBeVisible({ timeout: 10000 })
  }
  const screenWidth = (page: Page) => page.getByTestId('call-remote-screen').locator('video').evaluate((v: HTMLVideoElement) => v.videoWidth)
  await shareScreen(bob.page)
  await expect(carol.page.getByTestId('call-remote-screen')).toBeVisible({ timeout: 10000 })
  await expect.poll(() => screenWidth(carol.page), { timeout: 15000 }).toBeGreaterThan(0)
  await bob.page.getByRole('button', { name: 'Arrêter le partage d’écran' }).click()
  await expect(carol.page.getByTestId('call-remote-screen')).toHaveCount(0)
  await shareScreen(carol.page) // reuses bob's screen transceiver the other way
  await expect.poll(() => screenWidth(bob.page), { timeout: 15000 }).toBeGreaterThan(0)
  await carol.page.getByRole('button', { name: 'Arrêter le partage d’écran' }).click()
  await expect(bob.page.getByTestId('call-remote-screen')).toHaveCount(0)
  await shareScreen(bob.page) // no new negotiation
  await expect.poll(() => screenWidth(carol.page), { timeout: 15000 }).toBeGreaterThan(0)
  await expect.poll(async () => Number(await carolBar.getAttribute('data-received'))).toBeGreaterThan(1000) // voice still flows
  await carol.page.getByRole('button', { name: 'Raccrocher' }).click()
  await expect(bar).toHaveAttribute('data-status', 'ended', { timeout: 10000 })
  await bob.app.close()
  await carol.app.close()
})
