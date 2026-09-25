// Channel features of the desktop app: search (jumping to an old message),
// pins, threads, and notifications following the member's settings.
import { expect, test, type Page } from '@playwright/test'
import { Community, Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(19780)
const srv = new Community(19790, 'localhost:19780')
const ctl = new Ctl(id)
const password = 'motdepasse-solide'

test.beforeAll(async () => {
  await id.start()
  await srv.start()
  await ctl.account('alice')
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

test('channels: search, pins, threads, notifications', async () => {
  const dir = tempDir('quarel-app-')
  let { app, page } = await launchApp(dir)
  // Desktop notifications are recorded instead of shown.
  const fakeNotifications = () => page.evaluate(() => {
    const shown: string[] = []
    ;(window as unknown as { __notes: string[] }).__notes = shown
    class FakeNotification {
      static permission = 'granted'
      static requestPermission = async () => 'granted'
      onclick: (() => void) | null = null
      constructor(title: string, o?: { body?: string }) { shown.push(title + ' | ' + (o?.body ?? '')) }
      close() {}
    }
    ;(window as unknown as { Notification: unknown }).Notification = FakeNotification
  })
  await fakeNotifications()
  const notes = () => page.evaluate(() => (window as unknown as { __notes: string[] }).__notes.slice())

  await signIn(page)
  const sid = /server_id=([a-z2-7]{26})/.exec(srv.log)![1]
  await page.getByRole('button', { name: 'Ajouter un serveur' }).click()
  await page.getByLabel("Lien d'invitation").fill(`quarel://localhost:${srv.port}/${srv.claimCode()}?sid=${sid}&claim=1`)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await page.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await page.locator('button.sidebar-head').click()
  await page.getByRole('menuitem', { name: 'Inviter des personnes' }).click()
  await expect(page.getByLabel("Lien d'invitation")).toHaveValue(/\/join#localhost:/)
  const link = await page.getByLabel("Lien d'invitation").inputValue()
  await page.getByRole('button', { name: 'Fermer' }).click()
  ctl.run('alice', 'join', link) // a web invite link works for quarelctl too
  await expect(page.getByLabel('Membres').getByText('alice')).toBeVisible()

  // Search: an old message, beyond the first page, is loaded and shown.
  ctl.run('alice', 'send', 'général', 'Le rendez-vous est à la gare de Lyon')
  for (let i = 1; i <= 60; i++) ctl.run('alice', 'send', 'général', 'bavardage ' + i)
  await app.close() // on restart, only the latest page of messages is loaded
  ;({ app, page } = await launchApp(dir))
  await fakeNotifications()
  await page.getByRole('button', { name: 'général', exact: true }).click()
  const log = page.getByRole('log')
  await expect(log).toContainText('bavardage 60')
  await expect(log).not.toContainText('rendez-vous')
  await page.getByRole('button', { name: 'Rechercher' }).click()
  await page.getByLabel('Mots à chercher').fill('rendez gare')
  await page.getByLabel('Mots à chercher').press('Enter')
  const result = page.getByRole('complementary', { name: 'Recherche' }).locator('.result')
  await expect(result).toHaveCount(1)
  await result.click()
  const old = log.locator('.msg', { hasText: 'rendez-vous est à la gare' })
  await expect(old).toBeInViewport()

  // Pin it.
  await old.hover()
  await old.getByRole('button', { name: 'Épingler', exact: true }).click()
  await expect(old.locator('.pin-tag')).toBeVisible()
  expect(ctl.run('alice', 'pins', 'général')).toContain('rendez-vous')
  await page.getByRole('button', { name: 'Messages épinglés' }).click()
  await expect(page.getByRole('complementary', { name: 'Messages épinglés' })).toContainText('rendez-vous')

  // A thread from the latest message.
  const last = log.locator('.msg', { hasText: 'bavardage 60' })
  await last.hover()
  await last.getByRole('button', { name: 'Créer un fil' }).click()
  await page.getByLabel('Nom du fil').fill('Organisation')
  await page.getByRole('button', { name: 'Créer le fil' }).click()
  await expect(page.locator('.channel-head .title')).toHaveText('Organisation')
  await page.getByLabel('Message pour #Organisation').fill('On prend le train de 9 h ?')
  await page.getByLabel('Message pour #Organisation').press('Enter')
  await expect(page.getByRole('log')).toContainText('train de 9 h')
  expect(ctl.run('alice', 'channels')).toContain('Organisation')

  // Notifications (bob is in the thread): a mention in "général" notifies…
  ctl.run('alice', 'send', 'général', '@bob tu viens ?')
  await expect.poll(async () => (await notes()).join('\n')).toContain('alice · #général · Serveur Quarel | @bob tu viens ?')
  ctl.run('alice', 'send', 'général', 'message sans mention')
  // …a plain message does not (default: mentions only)…
  await page.waitForTimeout(800)
  expect((await notes()).join('\n')).not.toContain('sans mention')
  // …"all messages" for the server: now it does.
  await page.locator('button.sidebar-head').click()
  await page.getByRole('menuitem', { name: 'Notifications' }).click()
  await page.getByRole('menuitemradio', { name: 'Tous les messages' }).click()
  ctl.run('alice', 'send', 'général', 'deuxième message sans mention')
  await expect.poll(async () => (await notes()).join('\n')).toContain('deuxième message sans mention')
  // "Général" set to nothing overrides the server.
  await page.keyboard.press('Escape')
  await page.getByRole('navigation', { name: 'Salons' }).locator('button.ch', { hasText: /^général/ }).click()
  await page.getByRole('button', { name: 'Notifications du salon' }).click()
  await page.getByRole('menuitemradio', { name: 'Rien' }).click()
  await page.getByRole('navigation', { name: 'Salons' }).locator('button.ch', { hasText: 'Organisation' }).click()
  const before = (await notes()).length
  ctl.run('alice', 'send', 'général', '@bob encore toi ?')
  await page.waitForTimeout(800)
  expect((await notes()).length).toBe(before)
  await app.close()
})
