// Group calls (P2): three apps in a group conversation, a peer-to-peer mesh.
// bob starts the call, carol and dave join from the ringing card; everyone
// hears the two others; dave leaves; bob shares his screen; carol leaves and
// bob waits alone.
import { expect, test, type Page } from '@playwright/test'
import { Identity, launchApp, tempDir } from './fixtures'

// A relay on the loopback: carol and dave (not friends) talk only through it.
// Chromium allocates a relay address per network interface and connection
// (every app gathers relay candidates): a machine with many interfaces
// (Docker bridges) exhausts a small port range (TURN error 508).
const id = new Identity(19980, {
  QUAREL_TURN: 'on', QUAREL_TURN_LISTEN: '127.0.0.1:19979', QUAREL_TURN_PUBLIC_IP: '127.0.0.1', QUAREL_TURN_PORTS: '50200-50599',
  QUAREL_TURN_ALLOW_PRIVATE: '1',
})
const password = 'motdepasse-solide'

async function account(pseudo: string) {
  await id.api('POST', '/v1/auth/register', { email: pseudo + '@example.com', pseudo, password })
  await id.api('POST', '/v1/auth/verify-email', { email: pseudo + '@example.com', code: id.lastCode() })
}

test.beforeAll(async () => {
  await id.start()
  for (const p of ['bob', 'carol', 'dave']) await account(p)
})
test.afterAll(() => id.stop())

async function signIn(page: Page, pseudo: string) {
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill(pseudo)
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByRole('button', { name: 'Ajouter un ami' })).toBeVisible()
}

async function befriend(from: Page, to: Page, pseudo: string) {
  await from.locator('.dm-item', { hasText: 'Amis' }).click()
  await from.getByRole('button', { name: 'Ajouter un ami' }).click()
  await from.getByLabel("Pseudo de l'ami").fill(pseudo)
  await from.getByRole('button', { name: 'Envoyer la demande' }).click()
  await to.locator('.dm-item', { hasText: 'Amis' }).click()
  await to.getByRole('button', { name: /En attente/ }).click()
  await to.getByRole('button', { name: 'Accepter' }).click()
}

const peers = (page: Page) => page.getByTestId('callbar').getAttribute('data-peers').then(Number)
const received = (page: Page) => page.getByTestId('callbar').getAttribute('data-received').then(Number)

test('group calls: mesh of three apps, join, leave, screen', async () => {
  test.setTimeout(240_000)
  const [bob, carol, dave] = await Promise.all(['bob', 'carol', 'dave'].map(() => launchApp(tempDir('quarel-app-'))))
  await signIn(bob.page, 'bob')
  await signIn(carol.page, 'carol')
  await signIn(dave.page, 'dave')
  await befriend(bob.page, carol.page, 'carol')
  await befriend(bob.page, dave.page, 'dave')

  // bob creates the group; carol and dave see it.
  await bob.page.getByRole('button', { name: 'Nouveau groupe' }).click()
  await bob.page.getByLabel('Nom du groupe (facultatif)').fill('Équipe')
  await bob.page.getByRole('dialog').getByText('carol').click()
  await bob.page.getByRole('dialog').getByText('dave').click()
  await bob.page.getByRole('button', { name: 'Créer le groupe' }).click()
  const msg = bob.page.getByLabel('Message pour Équipe')
  await msg.fill('On s’appelle ?')
  await msg.press('Enter')
  for (const p of [carol.page, dave.page]) {
    await p.locator('.dm-item', { hasText: 'Équipe' }).click()
    await expect(p.getByText('On s’appelle ?')).toBeVisible({ timeout: 15000 })
  }

  // bob starts the call: it rings for carol and dave.
  await bob.page.getByRole('button', { name: 'Appeler le groupe' }).click()
  await expect(bob.page.getByTestId('callbar')).toHaveAttribute('data-status', 'ringing')
  for (const p of [carol.page, dave.page]) {
    const ring = p.getByRole('alertdialog', { name: 'Appel de groupe Équipe' })
    await expect(ring).toBeVisible({ timeout: 15000 })
    await expect(ring).toContainText('bob lance un appel de groupe')
    await ring.getByRole('button', { name: 'Rejoindre' }).click()
  }
  for (const p of [bob.page, carol.page, dave.page]) {
    await expect(p.getByTestId('callbar')).toHaveAttribute('data-status', 'active', { timeout: 30000 })
    await expect.poll(() => peers(p), { timeout: 30000 }).toBe(2)
    await expect(p.getByTestId('call-peer')).toHaveCount(2)
  }
  // Everyone hears both others (fake microphones).
  for (const p of [bob.page, carol.page, dave.page]) await expect.poll(() => received(p), { timeout: 15000 }).toBeGreaterThan(4000)
  await expect(bob.page.getByTestId('callbar')).toContainText('3 personnes')
  // carol and dave are not friends: their link goes through the relay (neither
  // learns the other's address); each one's link with bob, a friend, is direct.
  const tile = (p: Page, who: string) => p.locator(`[data-testid="call-peer"][data-user="${who}"]`)
  await expect(tile(carol.page, 'dave')).toHaveAttribute('data-path', 'relay', { timeout: 15000 })
  await expect(tile(dave.page, 'carol')).toHaveAttribute('data-path', 'relay')
  await expect(tile(carol.page, 'bob')).toHaveAttribute('data-path', /^(local|direct)$/)
  await expect(tile(bob.page, 'dave')).toHaveAttribute('data-path', /^(local|direct)$/)
  // Each person's round trip on their tile, the slowest one in the call bar.
  for (const who of ['carol', 'dave']) await expect(bob.page.getByTestId('call-panel').getByRole('img', { name: new RegExp('Ping avec ' + who + ' : \\d+ ms') })).toBeVisible({ timeout: 10000 })
  await expect(bob.page.getByTestId('callbar').getByRole('img', { name: /Ping \(la liaison la plus lente\) : \d+ ms/ })).toBeVisible()

  // dave's camera reaches both others.
  await dave.page.getByRole('button', { name: 'Activer la caméra' }).click()
  for (const p of [bob.page, carol.page]) await expect(p.getByTestId('call-panel').locator('.call-tile video')).toHaveCount(1, { timeout: 10000 })

  // dave leaves: the two others stay connected.
  await dave.page.getByRole('button', { name: 'Raccrocher' }).click()
  for (const p of [bob.page, carol.page]) await expect.poll(() => peers(p), { timeout: 15000 }).toBe(1)
  await expect(bob.page.getByTestId('call-panel').locator('.call-tile video')).toHaveCount(0)
  // The call is still going on: dave can see it and join again.
  await expect(dave.page.getByRole('button', { name: /Rejoindre l’appel \(2\)/ })).toBeVisible({ timeout: 10000 })

  // bob shares his screen: no renegotiation needed in a group.
  await bob.page.getByRole('button', { name: 'Partager l’écran' }).click()
  await bob.page.getByRole('dialog', { name: "Partager l'écran" }).locator('.picker-grid button').first().click()
  await expect(bob.page.getByTestId('call-my-screen')).toBeVisible({ timeout: 10000 })
  await expect.poll(() => carol.page.getByTestId('call-remote-screen').locator('video').evaluate((v: HTMLVideoElement) => v.videoWidth), { timeout: 15000 }).toBeGreaterThan(0)

  // dave comes back while bob shares: he gets the screen at once.
  await dave.page.getByRole('button', { name: /Rejoindre l’appel/ }).click()
  await expect.poll(() => peers(dave.page), { timeout: 30000 }).toBe(2)
  await expect.poll(() => dave.page.getByTestId('call-remote-screen').locator('video').evaluate((v: HTMLVideoElement) => v.videoWidth), { timeout: 15000 }).toBeGreaterThan(0)

  // carol and dave leave: bob waits alone, then hangs up.
  await carol.page.getByRole('button', { name: 'Raccrocher' }).click()
  await dave.page.getByRole('button', { name: 'Raccrocher' }).click()
  await expect(bob.page.getByTestId('callbar')).toHaveAttribute('data-status', 'ringing', { timeout: 15000 })
  await bob.page.getByRole('button', { name: 'Raccrocher' }).click()
  await expect(bob.page.getByTestId('callbar')).toHaveAttribute('data-status', 'ended')
  await expect(carol.page.getByRole('button', { name: 'Appeler le groupe' })).toBeVisible({ timeout: 10000 }) // nobody left in the call

  await Promise.all([bob, carol, dave].map((a) => a.app.close()))
})
