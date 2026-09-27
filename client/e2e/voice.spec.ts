// Step 3 of the desktop client: voice channels. The app (bob, fake microphone
// and camera) meets alice, who uses the server's voice test page in Chromium.
import { chromium, expect, test, type Browser } from '@playwright/test'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { Community, Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(18980)
// Voice on its own ports: the live test server uses the default ones.
const srv = new Community(18990, 'localhost:18980', {
  QUAREL_VOICE: 'embedded', QUAREL_LIVEKIT_BIN: join(homedir(), '.local/bin/livekit-server'), QUAREL_VOICE_PUBLIC_IP: 'local',
  QUAREL_VOICE_SIGNAL_PORT: '27880', QUAREL_VOICE_TCP_PORT: '27881', QUAREL_VOICE_UDP_PORT: '27882',
})
const ctl = new Ctl(id)
const password = 'motdepasse-solide'
let browser: Browser

test.beforeAll(async () => {
  await id.start()
  await srv.start()
  for (let i = 0; i < 100 && !/voice ready/.test(srv.log); i++) await new Promise((r) => setTimeout(r, 100))
  await ctl.account('alice')
  ctl.run('alice', 'claim', 'localhost:18990', srv.claimCode())
  await id.api('POST', '/v1/auth/register', { email: 'bob@example.com', pseudo: 'bob', password })
  await id.api('POST', '/v1/auth/verify-email', { email: 'bob@example.com', code: id.lastCode() })
  browser = await chromium.launch({ args: ['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream', '--autoplay-policy=no-user-gesture-required'] })
})
test.afterAll(async () => {
  await browser?.close()
  srv.stop()
  id.stop()
  ctl.stop()
})

test('voice: join, hear, mute, camera, moderation, leave', async () => {
  // alice in the voice channel "Général", from the test page.
  const alice = await (await browser.newContext({ ignoreHTTPSErrors: true })).newPage()
  const url = /https:\/\/\S+voice-test\S+/.exec(ctl.run('alice', 'voice-test'))![0]
  await alice.goto(url)
  await alice.getByRole('button', { name: 'Rejoindre' }).first().click()
  await expect(alice.getByRole('button', { name: 'Connecté' })).toBeVisible()

  // bob, in the app.
  const { app, page } = await launchApp(tempDir('quarel-app-'))
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill('bob')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  const link = /quarel:\/\/\S+/.exec(ctl.run('alice', 'invite', '5'))![0]
  await page.getByRole('button', { name: 'Ajouter un serveur' }).click()
  await page.getByLabel("Lien d'invitation").fill(link)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await page.getByRole('button', { name: 'Rejoindre', exact: true }).click()

  // alice is listed under the voice channel before bob joins.
  const channels = page.getByRole('navigation', { name: 'Salons' })
  await expect(channels.locator('.voice-users').getByText('alice')).toBeVisible()
  await channels.getByRole('button', { name: 'Général', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Vocal' }).getByText('Vocal connecté')).toBeVisible({ timeout: 15000 })
  // The round trip to the voice server, next to "Vocal connecté".
  await expect(page.getByRole('region', { name: 'Vocal' }).getByRole('img', { name: /Ping vers le serveur vocal : \d+ ms/ })).toBeVisible({ timeout: 10000 })
  const grid = page.getByRole('region', { name: 'Participants' })
  await expect(grid.locator('.tile')).toHaveCount(2)
  await expect(grid.getByText('bob (vous)')).toBeVisible()
  // bob receives alice's audio (her fake microphone), and it flows.
  await expect(page.locator('audio[data-quarel-voice]')).toHaveCount(1, { timeout: 15000 })
  await expect.poll(() => page.evaluate(() => {
    const el = document.querySelector('audio[data-quarel-voice]') as HTMLAudioElement | null
    return !!el && !el.paused && (el.srcObject as MediaStream | null)?.getAudioTracks()[0]?.readyState === 'live'
  }), { timeout: 10000 }).toBe(true)
  // alice sees bob in the channel.
  await expect(alice.locator('li', { hasText: 'bob' })).toBeVisible()

  // Right click on alice: her volume for bob (150 %: played through Web Audio), muted for him.
  await grid.locator('.tile', { hasText: 'alice' }).click({ button: 'right' })
  let menu = page.getByRole('menu')
  await expect(menu).toContainText('alice')
  await menu.getByRole('slider', { name: 'Volume pour moi' }).fill('150')
  await expect(menu).toContainText('150 %')
  const pref = (k: string) => page.evaluate((k) => Object.keys(localStorage).filter((x) => x.startsWith('quarel.pref.' + k)).map((x) => localStorage.getItem(x)), k)
  await expect.poll(() => pref('vol:')).toEqual(['1.5'])
  await expect.poll(() => page.evaluate(() => (document.querySelector('audio[data-quarel-voice]') as HTMLAudioElement).muted)).toBe(true)
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await grid.locator('.tile', { hasText: 'alice' }).click({ button: 'right' })
  menu = page.getByRole('menu')
  await menu.getByRole('menuitemcheckbox', { name: 'Rendre muet pour moi' }).click()
  await expect.poll(() => pref('mute:')).toEqual(['true'])
  await grid.locator('.tile', { hasText: 'alice' }).click({ button: 'right' })
  await expect(page.getByRole('menuitemcheckbox', { name: 'Rendre muet pour moi' })).toHaveAttribute('aria-checked', 'true')
  await page.getByRole('menuitemcheckbox', { name: 'Rendre muet pour moi' }).click()

  // Mute: the server learns it, everybody sees the icon.
  const controls = page.locator('.voice-controls')
  await controls.getByRole('button', { name: 'Couper le micro' }).click()
  await expect(channels.locator('.vu', { hasText: 'bob' }).getByLabel('micro coupé')).toBeVisible()
  await controls.getByRole('button', { name: 'Réactiver le micro' }).click()
  await expect(channels.locator('.vu', { hasText: 'bob' }).getByLabel('micro coupé')).toHaveCount(0)

  // Camera: a video tile appears, alice receives it.
  await controls.getByRole('button', { name: 'Activer la caméra' }).click()
  await expect(grid.locator('.tile video')).toHaveCount(1)
  await expect(alice.locator('#videos video')).toHaveCount(1, { timeout: 15000 })
  const bobRow = channels.locator('.vu', { hasText: 'bob' })
  await expect(bobRow.getByLabel('caméra')).toBeVisible({ timeout: 10000 })
  await controls.getByRole('button', { name: 'Couper la caméra' }).click()
  await expect(grid.locator('.tile video')).toHaveCount(0)
  // Really off: the server and alice know it (not just a paused track).
  await expect(bobRow.getByLabel('caméra')).toHaveCount(0, { timeout: 10000 })
  await expect(alice.locator('#videos video')).toHaveCount(0, { timeout: 10000 })

  // Moderation: alice (owner) mutes bob's microphone.
  ctl.run('alice', 'voice-mute', 'bob')
  await expect(grid.getByText('Micro coupé par la modération')).toBeVisible()
  await expect(controls.locator('button[aria-label="Couper le micro"], button[aria-label="Réactiver le micro"]')).toBeDisabled()
  ctl.run('alice', 'voice-mute', 'bob', 'off')
  await expect(grid.getByText('Micro coupé par la modération')).toHaveCount(0)

  // A stage: bob moves to the audience, raises his hand, alice (owner) invites him to speak.
  ctl.run('alice', 'channel-edit', 'Général', 'stage=on')
  const audience = page.getByRole('region', { name: 'Public' })
  await expect(audience.getByTestId('listener')).toContainText('bob (vous)', { timeout: 10000 })
  await expect(controls.locator('button[aria-label="Couper le micro"], button[aria-label="Réactiver le micro"]')).toBeDisabled()
  await controls.getByRole('button', { name: 'Lever la main' }).click()
  await expect(audience.getByLabel('main levée')).toBeVisible()
  expect(ctl.run('alice', 'voice-speaker', 'bob')).toContain('peut parler')
  await expect(page.getByRole('region', { name: 'Sur scène' }).getByText('bob (vous)')).toBeVisible({ timeout: 10000 })
  await expect(controls.getByRole('button', { name: 'Couper le micro' })).toBeEnabled()
  await controls.getByRole('button', { name: 'Quitter la scène' }).click()
  await expect(audience.getByTestId('listener')).toContainText('bob (vous)', { timeout: 10000 })
  ctl.run('alice', 'channel-edit', 'Général', 'stage=off')
  await expect(grid.getByText('bob (vous)')).toBeVisible({ timeout: 10000 })

  // Leave.
  await controls.getByRole('button', { name: 'Quitter' }).click()
  await expect(page.getByRole('region', { name: 'Vocal' })).toHaveCount(0)
  await expect(channels.locator('.voice-users').getByText('bob')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Rejoindre le vocal' })).toBeVisible()
  await app.close()
})
