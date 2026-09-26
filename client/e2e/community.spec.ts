// Step 2 of the desktop client: community servers, channels, messages.
// Real app, real Identity service, real community server (self-signed
// certificate); alice acts through the command-line client.
import { expect, test, type Page } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { Community, Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(18480)
const srv = new Community(18490, 'localhost:18480')
const ctl = new Ctl(id)
const userData = tempDir('quarel-app-')
const password = 'motdepasse-solide'

test.beforeAll(async () => {
  await id.start()
  await srv.start()
  await ctl.account('alice')
  ctl.run('alice', 'claim', 'localhost:18490', srv.claimCode())
  ctl.run('alice', 'channel-create', 'hors-sujet')
  ctl.run('alice', 'rules', 'Soyez courtois.')
  // bob: created here, signs in through the app.
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
  await expect(page.locator('.userbar .name', { hasText: 'bob' })).toBeVisible()
}

const lastID = (out: string) => /\[(\d+)\]|\] (\d+)|#(\d+)/.exec(out)?.slice(1).find(Boolean)

test('servers: join, rules, messages, replies, reactions, files, unread, kick', async () => {
  let { app, page } = await launchApp(userData)
  await signIn(page)
  const link = /quarel:\/\/\S+/.exec(ctl.run('alice', 'invite', '5'))![0]

  // A link whose server ID does not match: the certificate is refused.
  await page.getByRole('button', { name: 'Ajouter un serveur' }).click()
  const wrong = link.replace(/sid=[a-z2-7]+/, 'sid=' + 'a'.repeat(26))
  await page.getByLabel("Lien d'invitation").fill(wrong)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await expect(page.getByText('ne correspond pas au lien d\u2019invitation')).toBeVisible()

  // The real link: server authenticated, then joined.
  await page.getByLabel("Lien d'invitation").fill(link)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await expect(page.getByText('Serveur authentifié')).toBeVisible()
  await expect(page.getByText('il faudra accepter ses règles')).toBeVisible()
  await page.getByRole('button', { name: 'Rejoindre', exact: true }).click()

  // Rules first.
  await expect(page.getByRole('heading', { name: 'Avant de participer' })).toBeVisible()
  await expect(page.getByText('Soyez courtois.')).toBeVisible()
  await page.getByLabel("J'ai lu et j'accepte les règles").check()
  await page.getByRole('button', { name: 'Entrer dans le serveur' }).click()

  // The first channel opens (hors-sujet, outside any category); go to général.
  const channels = page.getByRole('navigation', { name: 'Salons' })
  await expect(page.getByLabel('Message pour #hors-sujet')).toBeVisible()
  await channels.getByRole('button', { name: 'général', exact: true }).click()
  // alice's message arrives live, mentioning bob.
  const composer = page.getByLabel('Message pour #général')
  await expect(composer).toBeVisible()
  await expect(page.getByLabel('Membres').getByText('alice')).toBeVisible()
  const m1 = lastID(ctl.run('alice', 'send', 'général', 'Salut @bob, tu viens samedi ?'))!
  const log = page.getByRole('log')
  const msg1 = log.locator(`[data-mid="${m1}"]`)
  await expect(msg1).toContainText('Salut @bob, tu viens samedi ?')
  await expect(msg1).toHaveClass(/mentioned/)

  // alice is typing.
  ctl.run('alice', 'typing', 'général')
  await expect(page.getByText('alice écrit…')).toBeVisible()

  // Reply with a mention (typed as @alice).
  await msg1.hover()
  await msg1.getByRole('button', { name: 'Répondre' }).click()
  await expect(page.getByText('Réponse à alice')).toBeVisible()
  await composer.fill('Oui @alice, avec **plaisir** !')
  await composer.press('Enter')
  const mine = log.locator('.msg').last()
  await expect(mine).toContainText('Oui @alice, avec plaisir !')
  await expect(mine.locator('strong')).toHaveText('plaisir')
  await expect(mine.locator('.reply-ref')).toContainText('Salut @bob')
  await expect.poll(() => ctl.run('alice', 'history', 'général'), { timeout: 15_000 }).toContain('↱ alice : Salut @bob')

  // Edit, react, then delete.
  await mine.hover()
  await mine.getByRole('button', { name: 'Modifier' }).click()
  await page.getByLabel('Modifier le message').fill('Oui, avec plaisir !')
  await page.getByLabel('Modifier le message').press('Enter')
  await expect(mine).toContainText('(modifié)')
  await msg1.hover()
  await msg1.getByRole('button', { name: 'Réagir' }).click()
  await page.getByRole('menuitem', { name: '👍' }).click()
  await expect(msg1.getByRole('button', { name: '👍 1' })).toHaveAttribute('aria-pressed', 'true')
  ctl.run('alice', 'react', 'général', m1, '👍')
  await expect(msg1.getByRole('button', { name: '👍 2' })).toBeVisible()
  const count = await log.locator('.msg').count()
  await mine.hover()
  await mine.getByRole('button', { name: 'Supprimer' }).click()
  await page.getByRole('button', { name: 'Supprimer', exact: true }).last().click()
  await expect(log.locator('.msg')).toHaveCount(count - 1)

  // Attach an image.
  const png = join(ctl.dir, 'point.png')
  writeFileSync(png, Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==', 'base64'))
  await page.locator('input[type=file]').setInputFiles(png)
  await expect(page.locator('.pending-file').getByText('point.png')).toBeVisible()
  await expect(page.locator('.pending-file .spinner')).toHaveCount(0)
  await composer.fill('La feuille de scores')
  await composer.press('Enter')
  await expect(log.locator('img.attach-img[alt="point.png"]')).toBeVisible()

  // Unread in another channel, with a mention badge.
  ctl.run('alice', 'send', 'hors-sujet', 'Hé @bob, regarde ça')
  const other = channels.getByRole('button', { name: /hors-sujet/ })
  await expect(other).toHaveClass(/unread/)
  await expect(other.getByLabel('1 mention(s)')).toBeVisible()
  await other.click()
  await expect(page.getByRole('log')).toContainText('Hé @bob, regarde ça')
  await expect(other.getByLabel('1 mention(s)')).toHaveCount(0)

  // Restart: still a member, connected, same channel.
  await app.close()
  ;({ app, page } = await launchApp(userData))
  await expect(page.getByRole('log')).toContainText('Hé @bob, regarde ça')

  // Kicked by alice.
  ctl.run('alice', 'kick', 'bob', 'test')
  await expect(page.getByText('Vous avez été expulsé de ce serveur.')).toBeVisible()
  await page.getByRole('button', { name: 'Retirer de ma liste' }).click()
  await expect(page.getByRole('button', { name: 'Ajouter un serveur' })).toBeVisible()
  await app.close()
})
