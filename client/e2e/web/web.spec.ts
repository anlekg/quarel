// Step 7: the web client, in Chromium (not Electron), as served by app.quarel.app.
import { devices, expect, test } from '@playwright/test'
import { Community, Ctl, Identity } from '../fixtures'

const id = new Identity(19680)
const srv = new Community(19690, 'localhost:19680')
const ctl = new Ctl(id)
const password = 'motdepasse-solide'
let sid = ''

test.beforeAll(async () => {
  await id.start()
  await srv.start()
  await ctl.account('alice')
  ctl.run('alice', 'claim', 'localhost:' + srv.port, srv.claimCode())
  sid = /server_id=([a-z2-7]{26})/.exec(srv.log)![1]
  await id.api('POST', '/v1/auth/register', { email: 'bob@example.com', pseudo: 'bob', password })
  await id.api('POST', '/v1/auth/verify-email', { email: 'bob@example.com', code: id.lastCode() })
  await id.api('POST', '/v1/auth/register', { email: 'dave@example.com', pseudo: 'dave', password })
  await id.api('POST', '/v1/auth/verify-email', { email: 'dave@example.com', code: id.lastCode() })
  await id.api('POST', '/v1/auth/register', { email: 'carol@example.com', pseudo: 'carol', password })
  await id.api('POST', '/v1/auth/verify-email', { email: 'carol@example.com', code: id.lastCode() })
})
test.afterAll(() => {
  srv.stop()
  id.stop()
  ctl.stop()
})

test('web invite link: landing, sign in, join', async ({ page }) => {
  const code = /quarel:\/\/\S+\/([A-Za-z0-9]+)\?/.exec(ctl.run('alice', 'invite', '5'))![1]
  await page.goto(`/join#localhost:${srv.port}/${code}?sid=${sid}`)
  await expect(page.getByRole('heading', { name: 'Invitation sur un serveur Quarel' })).toBeVisible()
  await expect(page.getByRole('link', { name: "Ouvrir dans l'application Quarel" })).toHaveAttribute('href', `quarel://localhost:${srv.port}/${code}?sid=${sid}`)
  expect(new URL(page.url()).pathname).toBe('/') // the link left the address bar
  await page.getByRole('button', { name: 'Continuer dans le navigateur' }).click()
  await expect(page.getByText('pour rejoindre le serveur localhost')).toBeVisible()
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill('bob')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  const join = page.getByRole('dialog', { name: 'Rejoindre un serveur' })
  await expect(join.getByRole('button', { name: 'Rejoindre', exact: true })).toBeVisible()
  await join.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await expect(page.getByLabel('Membres').getByText('alice')).toBeVisible()
})

test('web storage: session and private messages encrypted at rest', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill('carol')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByRole('button', { name: 'Ajouter un ami' })).toBeVisible()
  // Friends and a private message, so that history exists.
  ctl.run('alice', 'e2e')
  ctl.run('alice', 'friend-add', 'carol')
  await page.getByRole('button', { name: /En attente/ }).click()
  await page.getByRole('button', { name: 'Accepter' }).click()
  await expect.poll(() => ctl.run('alice', 'dm', 'carol', 'secret-du-jour-42')).toContain('chiffré de bout en bout')
  await page.locator('.dm-item', { hasText: 'alice' }).click()
  await expect(page.getByRole('log')).toContainText('secret-du-jour-42')

  // Nothing readable in the browser's storage.
  const dump = await page.evaluate(async () => {
    const out: string[] = [JSON.stringify({ ...localStorage })]
    const db = await new Promise<IDBDatabase>((res, rej) => {
      const r = indexedDB.open('quarel')
      r.onsuccess = () => res(r.result)
      r.onerror = () => rej(r.error)
    })
    for (const store of ['secrets', 'vault']) {
      const values = await new Promise<unknown[]>((res) => {
        const r = db.transaction(store).objectStore(store).getAll()
        r.onsuccess = () => res(r.result)
      })
      for (const v of values) out.push(typeof v === 'string' ? v : new TextDecoder().decode((v as { ct: ArrayBuffer }).ct))
    }
    return out.join('\n')
  })
  expect(dump).not.toContain('secret-du-jour-42')
  expect(dump).not.toContain('session')
  expect(dump).not.toContain('carol@example.com')
  // And the page still reads it after a reload.
  await page.reload()
  await page.locator('.dm-item', { hasText: 'alice' }).click()
  await expect(page.getByRole('log')).toContainText('secret-du-jour-42')
})

test('web app is installable: manifest, icons, service worker', async ({ page, request }) => {
  await page.goto('/')
  const manifest = await (await request.get('/manifest.webmanifest')).json()
  expect(manifest).toMatchObject({ name: 'Quarel', display: 'standalone', start_url: '/' })
  for (const icon of manifest.icons) expect((await request.get(icon.src)).ok()).toBe(true)
  await expect.poll(() => page.evaluate(async () => !!(await navigator.serviceWorker.getRegistration())?.active)).toBe(true)
  // Offline, the page still opens from the cache.
  await page.reload()
  await page.context().setOffline(true)
  await page.reload()
  await expect(page.getByRole('button', { name: 'Se connecter' })).toBeVisible()
  await page.context().setOffline(false)
})

test.describe('phone', () => {
  const { defaultBrowserType: _, ...pixel } = devices['Pixel 7']
  test.use(pixel)

  test('phone layout: one pane at a time, back to the lists', async ({ page }) => {
    const shot = (n: string) => process.env.QUAREL_SHOTS ? page.screenshot({ path: process.env.QUAREL_SHOTS + '/m-' + n + '.png' }) : null
    const code = /quarel:\/\/\S+\/([A-Za-z0-9]+)\?/.exec(ctl.run('alice', 'invite', '5'))![1]
    await page.goto(`/join#localhost:${srv.port}/${code}?sid=${sid}`)
    await shot('1-landing')
    await page.getByRole('button', { name: 'Continuer dans le navigateur' }).click()
    await page.getByRole('button', { name: 'Changer' }).click()
    await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
    await page.getByRole('button', { name: 'Utiliser ce service' }).click()
    await page.getByLabel('Email ou pseudo').fill('dave')
    await page.getByLabel('Mot de passe', { exact: true }).fill(password)
    await shot('2-login')
    await page.getByRole('button', { name: 'Se connecter' }).click()
    const join = page.getByRole('dialog', { name: 'Rejoindre un serveur' })
    await join.getByRole('button', { name: 'Rejoindre', exact: true }).click()
    ctl.run('alice', 'send', 'général', 'Bienvenue sur le serveur !')
    // Lists first: the channel is not shown yet.
    const channels = page.getByRole('navigation', { name: 'Salons' })
    await expect(channels).toBeVisible()
    await expect(page.getByPlaceholder(/Écrire dans #général/)).toBeHidden()
    await shot('3-channels')
    await channels.getByRole('button', { name: 'général', exact: true }).click()
    await expect(page.getByRole('log')).toContainText('Bienvenue sur le serveur')
    await expect(channels).toBeHidden()
    await shot('4-channel')
    await page.getByRole('button', { name: 'Afficher les membres' }).click()
    await shot('5-members')
    await page.getByRole('button', { name: 'Masquer les membres' }).click()
    await page.getByRole('button', { name: 'Retour aux listes' }).click()
    await expect(channels).toBeVisible()
    // Private messages and settings.
    await page.getByRole('button', { name: /Messages privés/ }).click()
    await shot('6-home')
    await page.getByRole('button', { name: 'Paramètres' }).click()
    await expect(page.getByRole('dialog', { name: 'Paramètres' })).toBeVisible()
    await shot('7-settings')
    // No horizontal scrolling anywhere.
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  })
})
