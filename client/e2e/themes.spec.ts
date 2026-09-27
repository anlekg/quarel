// Cosmetics block: the server's theme on its area only (filtered), my own
// CSS, which one goes first, safe mode, ignoring a server's theme, profile
// cards with a member's theme on this server, my profile on this server
// (avatar, bio), its reset by the moderation, and my identity card's theme.
import { expect, test, type Page } from '@playwright/test'
import { deflateSync } from 'node:zlib'
import { Community, Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(20280)
const srv = new Community(20290, 'localhost:20280')
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

function png(): Buffer {
  const crc = (b: Buffer) => {
    let c = ~0
    for (const x of b) { c ^= x; for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1)) }
    return ~c >>> 0
  }
  const chunk = (t: string, d: Buffer) => {
    const len = Buffer.alloc(4); len.writeUInt32BE(d.length)
    const td = Buffer.concat([Buffer.from(t), d])
    const c = Buffer.alloc(4); c.writeUInt32BE(crc(td))
    return Buffer.concat([len, td, c])
  }
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(8, 0); ihdr.writeUInt32BE(8, 4); ihdr[8] = 8; ihdr[9] = 2
  const raw = Buffer.concat(Array.from({ length: 8 }, () => Buffer.concat([Buffer.from([0]), Buffer.from('e0555d'.repeat(8), 'hex')])))
  return Buffer.concat([Buffer.from('89504e470d0a1a0a', 'hex'), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))])
}

const style = (page: Page, sel: string, prop: string) => page.evaluate(([s, p]) => {
  const e = document.querySelector(s)
  return e ? getComputedStyle(e).getPropertyValue(p).trim() : null
}, [sel, prop])

test('themes: server area, my CSS, priority, safe mode, profile cards', async () => {
  test.setTimeout(120_000)
  const { app, page } = await launchApp(tempDir('quarel-app-'))
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill('bob')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  const sid = /server_id=([a-z2-7]{26})/.exec(srv.log)![1]
  await page.getByRole('button', { name: 'Ajouter un serveur' }).click()
  await page.getByLabel("Lien d'invitation").fill(`quarel://localhost:${srv.port}/${srv.claimCode()}?sid=${sid}&claim=1`)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await page.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await expect(page.getByLabel('Membres').getByTitle('Propriétaire')).toBeVisible()
  const composer = page.getByLabel('Message pour #général')
  await composer.fill('Bienvenue')
  await composer.press('Enter')
  await expect(page.locator('.msg').first()).toBeVisible()

  const zone = '.server-zone > .sidebar'
  const bar = '.userbar-zone'
  const msg = '.server-zone .msg'
  expect(await style(page, zone, '--accent')).toBe('#5fb8a5')

  // The server's theme, from its settings: a colour and CSS, part of it refused.
  const serverMenu = async (item: string) => {
    await page.locator('button.sidebar-head').click()
    await page.getByRole('menuitem', { name: item }).click()
  }
  await serverMenu('Paramètres du serveur')
  const settings = page.getByRole('dialog', { name: 'Paramètres du serveur' })
  await settings.getByRole('navigation').getByRole('button', { name: 'Apparence', exact: true }).click()
  await settings.getByLabel('Accent', { exact: true }).fill('#00ff00')
  await settings.getByLabel('CSS (la zone du serveur)').fill('.msg { outline: 3px solid rgb(1, 2, 3); position: fixed; content: "x" }\n.userbar { outline: 3px solid rgb(1, 2, 3) }')
  await expect(settings.getByText(/Sera ignoré : propriété « position » ; propriété « content »/)).toBeVisible()
  await settings.getByLabel('Dégradé en fond').check()
  await settings.getByRole('button', { name: 'Enregistrer le thème' }).click()
  await expect(settings.getByText('Thème enregistré.')).toBeVisible()
  await settings.getByRole('button', { name: 'Fermer les paramètres du serveur' }).click()

  await expect.poll(() => style(page, zone, '--accent')).toBe('#00ff00')
  expect(await style(page, msg, 'outline-color')).toBe('rgb(1, 2, 3)')
  expect(await style(page, msg, 'position')).toBe('relative') // the app's, not the theme's "fixed"
  expect(await style(page, bar, '--accent')).toBe('#5fb8a5') // the user bar keeps my colours…
  expect(await style(page, '.userbar', 'outline-style')).toBe('none') // …and the theme cannot reach it
  expect(await style(page, '.rail', '--accent')).toBe('#5fb8a5')

  // My own CSS: everywhere; the server's theme goes first by default.
  await page.getByRole('button', { name: 'Paramètres', exact: true }).click()
  const mine = page.getByRole('dialog', { name: 'Paramètres' })
  await mine.getByRole('navigation').getByRole('button', { name: 'Apparence' }).click()
  await mine.getByLabel('CSS', { exact: true }).fill(':root { --accent: #ff0000 }\n.msg { outline-color: rgb(9, 9, 9) }')
  await mine.getByRole('button', { name: 'Appliquer' }).click()
  expect(await style(page, bar, '--accent')).toBe('#ff0000')
  expect(await style(page, zone, '--accent')).toBe('#00ff00')
  expect(await style(page, msg, 'outline-color')).toBe('rgb(1, 2, 3)')
  // My CSS first: my colours win on the server too.
  await mine.getByLabel('Le thème d’un serveur passe avant mon CSS').uncheck()
  await expect.poll(() => style(page, zone, '--accent')).toBe('#ff0000')
  expect(await style(page, msg, 'outline-color')).toBe('rgb(9, 9, 9)')
  await mine.getByLabel('Le thème d’un serveur passe avant mon CSS').check()
  // The others' themes off.
  await mine.getByLabel('Afficher les thèmes des serveurs et des profils').uncheck()
  await expect.poll(() => style(page, zone, '--accent')).toBe('#ff0000')
  await mine.getByLabel('Afficher les thèmes des serveurs et des profils').check()
  await mine.getByRole('button', { name: 'Fermer les paramètres' }).click()
  await expect.poll(() => style(page, zone, '--accent')).toBe('#00ff00')

  // Safe mode: Ctrl+Shift+0, no custom CSS at all.
  await page.keyboard.press('Control+Shift+0')
  await expect(page.getByText('Mode sans échec')).toBeVisible()
  await expect.poll(() => style(page, zone, '--accent')).toBe('#5fb8a5')
  expect(await style(page, bar, '--accent')).toBe('#5fb8a5')
  await page.getByRole('button', { name: 'Réactiver' }).click()
  await expect.poll(() => style(page, zone, '--accent')).toBe('#00ff00')

  // Ignoring this server's theme (its menu), then showing it again.
  await serverMenu('Ignorer le thème de ce serveur')
  await expect.poll(() => style(page, msg, 'outline-color')).toBe('rgb(9, 9, 9)')
  await serverMenu('Afficher le thème de ce serveur')
  await expect.poll(() => style(page, msg, 'outline-color')).toBe('rgb(1, 2, 3)')

  // alice joins and gives herself a card on this server.
  await serverMenu('Paramètres du serveur')
  await settings.getByRole('navigation').getByRole('button', { name: 'Invitations', exact: true }).click()
  await settings.getByRole('button', { name: 'Créer une invitation' }).click()
  const code = (await settings.getByTestId('invites').locator('.title').first().textContent())!
  await settings.getByRole('button', { name: 'Fermer les paramètres du serveur' }).click()
  ctl.run('alice', 'join', `quarel://localhost:${srv.port}/${code}?sid=${sid}`)
  expect(ctl.run('alice', 'srv-profile', 'bio=Joueuse de tarot',
    'theme={"colors":{"bg_2":"#203040"},"css":".pc-name { color: rgb(7, 8, 9) } .pc-actions { display: none } .pc-bio { background: url(http://127.0.0.1:1/leak) }"}'))
    .toContain('invalid_theme') // the server refuses url() already
  expect(ctl.run('alice', 'srv-profile', 'bio=Joueuse de tarot',
    'theme={"colors":{"bg_2":"#203040"},"css":".pc-name { color: rgb(7, 8, 9) } .pc-actions { display: none }"}')).toContain('enregistré')
  await page.getByLabel('Membres').getByText('alice').click()
  const card = page.getByRole('dialog', { name: 'Profil' })
  await expect(card).toContainText('Joueuse de tarot')
  await expect.poll(() => style(page, '.profile-card .pc-name', 'color')).toBe('rgb(7, 8, 9)')
  expect(await style(page, '.profile-card', '--bg-2')).toBe('#203040')
  await expect(card.getByRole('button', { name: 'Détails et modération…' })).toBeVisible() // the app's buttons stay
  await page.keyboard.press('Escape')
  await expect(card).toHaveCount(0)

  // My profile on this server: a picture and a bio.
  await serverMenu('Mon profil sur ce serveur')
  const dlg = page.getByRole('dialog', { name: /Mon profil sur/ })
  await dlg.getByLabel('Présentation sur ce serveur').fill('Le patron')
  await dlg.locator('input[type=file][aria-label="Image de profil"]').setInputFiles({ name: 'moi.png', mimeType: 'image/png', buffer: png() })
  await expect.poll(() => page.locator('.members .member', { hasText: 'bob' }).locator('img').getAttribute('src')).toMatch(/^blob:/)
  await dlg.getByRole('button', { name: 'Enregistrer' }).click()
  await expect(dlg.getByText('Profil enregistré.')).toBeVisible()
  await dlg.getByRole('button', { name: 'Fermer' }).click()
  await page.getByLabel('Membres').getByText('bob').click()
  await expect(page.getByRole('dialog', { name: 'Profil' })).toContainText('Le patron')
  await page.keyboard.press('Escape')

  // The moderation resets alice's profile here.
  await page.getByLabel('Membres').getByText('alice').click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Réinitialiser son profil ici…' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Réinitialiser' }).click()
  await page.getByLabel('Membres').getByText('alice').click()
  await expect(card).toBeVisible()
  await expect(card).not.toContainText('Joueuse de tarot')
  await page.keyboard.press('Escape')

  // My identity card: its theme is kept by the identity service.
  await page.getByRole('button', { name: 'Paramètres', exact: true }).click()
  await mine.getByRole('navigation').getByRole('button', { name: 'Profil' }).click()
  await mine.getByLabel('CSS (votre carte)').fill('.pc-name { letter-spacing: 2px }')
  await mine.getByRole('button', { name: 'Enregistrer la carte' }).click()
  await expect(mine.getByText('Carte enregistrée.')).toBeVisible()
  await expect.poll(() => style(page, '.settings .profile-card .pc-name', 'letter-spacing')).toBe('2px') // the preview
  await mine.getByRole('navigation').getByRole('button', { name: 'Sécurité' }).click()
  await mine.getByRole('navigation').getByRole('button', { name: 'Profil' }).click()
  await expect(mine.getByLabel('CSS (votre carte)')).toHaveValue('.pc-name { letter-spacing: 2px }') // read back from the identity service
  await app.close()
})
