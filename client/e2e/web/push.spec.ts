// Web Push in the web app: the empty wake-up of the identity service (see
// internal/identity/webpush.go) turned on in the settings, and the service
// worker's notification without any content.
import { expect, test } from '@playwright/test'
import { Identity } from '../fixtures'

test.use({ channel: 'chromium' }) // the full browser: the headless shell has no notifications

const id = new Identity(19660, { QUAREL_PUSH_HOSTS: 'push.example' }) // the stubbed subscription's push service
const password = 'motdepasse-solide'

test.beforeAll(async () => {
  await id.start()
  await id.api('POST', '/v1/auth/register', { email: 'bob@example.com', pseudo: 'bob', password })
  await id.api('POST', '/v1/auth/verify-email', { email: 'bob@example.com', code: id.lastCode() })
})
test.afterAll(() => id.stop())

test('web push: empty wake-up, notification without content', async ({ page, context, baseURL }) => {
  // No real push service in tests: the browser's subscription is stubbed.
  await context.grantPermissions(['notifications'], { origin: new URL(baseURL!).origin })
  await page.addInitScript(() => {
    const sub = { endpoint: 'https://push.example/wake/bob', options: { applicationServerKey: null }, unsubscribe: async () => true }
    PushManager.prototype.subscribe = async () => sub as unknown as PushSubscription
    PushManager.prototype.getSubscription = async () => null
  })
  await page.goto('/')
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill('bob')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByRole('button', { name: 'Ajouter un ami' })).toBeVisible()
  await page.getByRole('button', { name: 'Paramètres' }).click()
  await page.getByRole('button', { name: 'Notifications' }).click()
  const put = page.waitForRequest((r) => r.url().endsWith('/v1/me/push') && r.method() === 'PUT')
  await page.getByRole('checkbox', { name: /Même quand l.application est fermée/ }).click()
  expect((await put).postDataJSON()).toEqual({ endpoint: 'https://push.example/wake/bob' })
  await expect(page.getByRole('checkbox', { name: /Même quand l.application est fermée/ })).toBeChecked()

  // A push reaches the service worker while no window is open: a notification with no content.
  const cdp = await context.newCDPSession(page)
  const registrationId = new Promise<string>((resolve) => cdp.on('ServiceWorker.workerRegistrationUpdated', (e) => {
    const r = e.registrations.find((x) => x.scopeURL.startsWith(baseURL!))
    if (r) resolve(r.registrationId)
  }))
  await cdp.send('ServiceWorker.enable')
  const reg = await registrationId
  await page.goto('about:blank')
  await cdp.send('ServiceWorker.deliverPushMessage', { origin: new URL(baseURL!).origin, registrationId: reg, data: '' })
  const other = await context.newPage()
  await other.goto('/')
  await expect.poll(() => other.evaluate(async () => (await (await navigator.serviceWorker.ready).getNotifications()).map((n) => n.title + ' | ' + n.body)))
    .toEqual(['Quarel | Du nouveau vous attend (message privé ou appel).'])
})
