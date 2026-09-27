// Step 6b of the desktop client: administering a community server from the
// app. The owner (app) sets the server up, manages roles, channel
// permissions, invites and bots, and moderates alice (Go test client).
import { execFileSync, spawn } from 'node:child_process'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test, type Page } from '@playwright/test'
import { Community, Ctl, Identity, launchApp, tempDir } from './fixtures'

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const id = new Identity(19580)
const srv = new Community(19590, 'localhost:19580')
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

test('server administration: overview, invites, roles, channel permissions, moderation, audit, bots', async () => {
  const { app, page } = await launchApp(tempDir('quarel-app-'))
  await signIn(page)
  const sid = /server_id=([a-z2-7]{26})/.exec(srv.log)![1]
  await page.getByRole('button', { name: 'Ajouter un serveur' }).click()
  await page.getByLabel("Lien d'invitation").fill(`quarel://localhost:${srv.port}/${srv.claimCode()}?sid=${sid}&claim=1`)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await page.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await expect(page.getByLabel('Membres').getByTitle('Propriétaire')).toBeVisible()

  const open = async () => {
    await page.locator('button.sidebar-head').click()
    await page.getByRole('menuitem', { name: 'Paramètres du serveur' }).click()
  }
  const settings = page.getByRole('dialog', { name: 'Paramètres du serveur' })
  const nav = (name: string) => settings.getByRole('navigation').getByRole('button', { name, exact: true }).click()
  await open()

  // Overview: name and rules.
  await settings.getByLabel('Nom du serveur').fill('Club de tarot')
  await settings.getByLabel('Règles').fill('Soyez courtois.')
  await settings.getByRole('button', { name: 'Enregistrer' }).click()
  await expect(settings).toContainText('Enregistré.')
  await expect(page.locator('button.sidebar-head')).toContainText('Club de tarot')

  // Invites: create one, alice joins with it.
  await nav('Invitations')
  await settings.getByLabel('Utilisations').selectOption('5')
  await settings.getByRole('button', { name: 'Créer une invitation' }).click()
  await expect(settings.getByTestId('invites')).toContainText('0 / 5 utilisation')
  const code = (await settings.getByTestId('invites').locator('.title').first().textContent())!
  ctl.run('alice', 'join', `quarel://localhost:${srv.port}/${code}?sid=${sid}`)
  ctl.run('alice', 'accept-rules')
  await expect(page.getByLabel('Membres').getByText('alice')).toBeVisible()

  // Roles: a moderator role that can kick, given to alice.
  await nav('Rôles')
  await settings.getByRole('button', { name: '+ Nouveau rôle' }).click()
  await settings.getByLabel('Nom du rôle').fill('Modération')
  await settings.getByRole('checkbox', { name: 'Expulser' }).check()
  await settings.getByRole('button', { name: 'Enregistrer' }).click()
  await expect(settings).toContainText('Rôle enregistré.')
  await expect(settings.getByRole('option', { name: /Modération/ })).toBeVisible()
  await nav('Membres')
  await settings.getByRole('button', { name: 'Gérer alice' }).click()
  let member = page.getByRole('dialog', { name: 'alice' })
  await member.getByRole('checkbox', { name: 'Modération' }).check()
  await expect.poll(() => ctl.run('alice', 'my-perms')).toContain('kick_members')
  await member.getByRole('button', { name: 'Fermer' }).click()

  // Channel permissions: @everyone may no longer write in "général".
  await nav('Salons')
  await settings.getByRole('button', { name: 'Modifier général', exact: true }).click()
  let dialog = page.getByRole('dialog', { name: 'Salon général' })
  await dialog.getByRole('tab', { name: 'Permissions' }).click()
  await dialog.getByRole('radiogroup', { name: 'Envoyer des messages' }).getByRole('radio', { name: 'Refuser' }).click()
  await dialog.getByRole('button', { name: 'Enregistrer' }).click()
  await expect(dialog).toContainText('Droits du salon enregistrés.')
  await expect.poll(() => ctl.run('alice', 'send', 'général', 'coucou')).toMatch(/permission/i)
  await dialog.getByRole('button', { name: 'Tout remettre par défaut' }).click()
  await expect.poll(() => ctl.run('alice', 'send', 'général', 'me revoilà')).toContain('me revoilà')
  // An incoming webhook: its secret address posts in the channel, under its own name.
  await dialog.getByRole('tab', { name: 'Webhooks' }).click()
  await dialog.getByLabel('Nom du webhook').fill('Supervision')
  await dialog.getByRole('button', { name: 'Créer un webhook' }).click()
  const hookURL = (await dialog.getByTestId('webhook-created').locator('code').textContent())!.trim()
  expect(hookURL).toMatch(/\/v1\/webhooks\/\w+\/qw_/)
  execFileSync('curl', ['-skf', '-X', 'POST', '-H', 'Content-Type: application/json', '-d', '{"content":"Sauvegarde terminée"}', hookURL])
  await expect.poll(() => ctl.run('alice', 'history', 'général')).toContain('Sauvegarde terminée')
  await page.keyboard.press('Escape')

  // A new channel.
  await settings.getByRole('button', { name: 'Créer un salon' }).click()
  dialog = page.getByRole('dialog', { name: 'Créer un salon' })
  await dialog.getByLabel('Type').selectOption('announcement')
  await dialog.getByLabel('Nom').fill('annonces')
  await dialog.getByRole('button', { name: 'Créer' }).click()
  await expect(settings.getByRole('button', { name: 'Modifier annonces', exact: true })).toBeVisible()
  await expect.poll(() => ctl.run('alice', 'channels'), { timeout: 15_000 }).toContain('annonces')

  // Automatic moderation: a banned word, refused for alice (no manage_messages) and logged.
  await nav('Modération automatique')
  await settings.getByLabel('Mots interdits (un par ligne)').fill('arnaq*')
  await settings.getByRole('button', { name: 'Enregistrer' }).click()
  await expect(settings).toContainText('Règles enregistrées.')
  await expect.poll(() => ctl.run('alice', 'send', 'général', 'une Arnaque !')).toMatch(/automod_word|banned/)
  expect(ctl.run('alice', 'send', 'général', 'tout va bien')).toContain('tout va bien')

  // Moderation of alice: timeout, then ban (and unban).
  await nav('Membres')
  await settings.getByRole('button', { name: 'Gérer alice' }).click()
  member = page.getByRole('dialog', { name: 'alice' })
  await member.getByLabel(/Raison/).fill('flood')
  await member.getByLabel("Durée de l'exclusion").selectOption('60')
  await member.getByRole('button', { name: 'Exclure temporairement' }).click()
  await expect(member).toContainText('Exclu temporairement')
  expect(ctl.run('alice', 'send', 'général', 'encore')).toMatch(/exclu|timed_out/i)
  await member.getByRole('button', { name: "Lever l'exclusion" }).click()
  await expect(member).toContainText('levée')
  await member.getByRole('button', { name: 'Bannir' }).click()
  await member.getByRole('button', { name: 'Bannir définitivement' }).click()
  await expect(page.getByLabel('Membres').getByText('alice')).toHaveCount(0)
  expect(ctl.run('alice', 'join', `quarel://localhost:${srv.port}/${code}?sid=${sid}`)).toMatch(/banni|banned/i)
  await nav('Bannissements')
  await expect(settings.getByTestId('bans')).toContainText('alice')
  await settings.getByRole('button', { name: 'Lever le bannissement' }).click()
  await expect(settings).toContainText('Personne n')

  // Audit log.
  await nav('Journal de modération')
  await expect(settings.getByTestId('audit')).toContainText('bob a banni alice')
  await expect(settings.getByTestId('audit')).toContainText('« flood »')
  await expect(settings.getByTestId('audit')).toContainText('bob a créé un webhook « Supervision »')
  await expect(settings.getByTestId('audit')).toContainText('Modération automatique a refusé un message de alice (mot interdit)')

  // Bots: the token is shown once.
  await nav('Bots')
  await settings.getByLabel('Nom du bot').fill('pingbot')
  await settings.getByRole('button', { name: 'Créer un bot' }).click()
  const token = page.getByRole('dialog', { name: 'Jeton de pingbot' })
  await expect(token.getByLabel('Jeton du bot')).toHaveValue(/^qb_/)
  const botToken = await token.getByLabel('Jeton du bot').inputValue()
  await token.getByRole('button', { name: 'Fermer' }).click()
  await expect(settings.getByTestId('bots')).toContainText('pingbot')

  await settings.getByRole('button', { name: 'Fermer les paramètres du serveur' }).click()
  await expect(settings).toBeHidden()

  // A custom emoji: added in the settings, typed :name: in a message, used as a reaction.
  await open()
  await nav('Emojis')
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAGklEQVR4nGOI37H0PyWYYdSAUQNGDRguBgAASLW7H4dqiRIAAAAASUVORK5CYII=', 'base64') // 16×16, sea green
  await settings.getByLabel("Image de l'emoji").setInputFiles({ name: 'chat_vert.png', mimeType: 'image/png', buffer: png })
  await expect(settings.getByLabel("Nom de l'emoji")).toHaveValue('chat_vert')
  await settings.getByRole('button', { name: 'Ajouter' }).click()
  await expect(settings.getByTestId('emojis')).toContainText(':chat_vert:')
  await expect(settings.getByTestId('emojis').locator('img.md-emoji')).toBeVisible()
  await settings.getByRole('button', { name: 'Fermer les paramètres du serveur' }).click()

  // Slash commands: the example bot runs with this token and declares /ping and /echo.
  const bot = spawn(join(repo, 'bin', 'pingbot'), [], {
    env: { ...process.env, QUAREL_URL: `https://localhost:${srv.port}`, QUAREL_BOT_TOKEN: botToken, QUAREL_SERVER_ID: sid }, stdio: 'ignore',
  })
  try {
    await page.getByRole('navigation', { name: 'Salons' }).getByRole('button', { name: 'général', exact: true }).click()
    const composer = page.getByLabel('Message pour #général')
    await composer.fill('Miaou :chat')
    await expect(page.getByRole('listbox', { name: 'Emojis du serveur' })).toContainText(':chat_vert:')
    await composer.press('Tab')
    await expect(composer).toHaveValue('Miaou :chat_vert: ')
    await composer.press('Enter')
    const withEmoji = page.getByRole('log').locator('.msg').last()
    await expect(withEmoji.locator('img.md-emoji')).toHaveAttribute('alt', ':chat_vert:')
    await withEmoji.hover()
    await withEmoji.getByRole('button', { name: 'Réagir' }).click()
    await page.getByRole('menuitem', { name: ':chat_vert:' }).click()
    await expect(withEmoji.locator('.reaction img.md-emoji')).toBeVisible()
    await composer.fill('/p')
    await expect(page.getByRole('listbox', { name: 'Commandes' })).toContainText('/ping', { timeout: 15_000 })
    await composer.press('Tab')
    await expect(composer).toHaveValue('/ping')
    await composer.press('Enter')
    const reply = page.locator('.msg', { has: page.getByTestId('interaction') }).last()
    await expect(reply).toContainText('bob a utilisé /ping')
    await expect(reply).toContainText('pong')
    await composer.fill('/echo un secret pour moi')
    await composer.press('Enter')
    await expect(page.getByTestId('ephemeral-reply')).toContainText('un secret pour moi')
    await expect(page.getByTestId('ephemeral-reply')).toContainText('Visible uniquement par vous')
    await expect(page.getByRole('log').locator('.msg:not(.ephemeral)', { hasText: 'un secret pour moi' })).toHaveCount(0) // never stored as a message
    await composer.fill('/echo')
    await composer.press('Enter')
    await expect(page.getByRole('alert')).toContainText('Il manque « texte »')
  } finally {
    bot.kill()
  }
  await app.close()
})
