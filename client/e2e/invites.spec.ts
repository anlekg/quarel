// Inviting friends to a server from the app: a single-use invite sent in a
// private message (alice, Go test client, joins with it), and invite links
// received in a conversation shown as cards ("Rejoindre", "Ouvrir").
import { expect, test } from '@playwright/test'
import { Community, Ctl, Identity, launchApp, tempDir } from './fixtures'

const id = new Identity(20180)
const srv = new Community(20190, 'localhost:20180') // bob's (the app)
const other = new Community(20195, 'localhost:20180') // alice's
const ctl = new Ctl(id)
const password = 'motdepasse-solide'

test.beforeAll(async () => {
  await id.start()
  await srv.start()
  await other.start()
  await ctl.account('alice')
  ctl.run('alice', 'e2e')
  await id.api('POST', '/v1/auth/register', { email: 'bob@example.com', pseudo: 'bob', password })
  await id.api('POST', '/v1/auth/verify-email', { email: 'bob@example.com', code: id.lastCode() })
})
test.afterAll(() => {
  srv.stop()
  other.stop()
  id.stop()
  ctl.stop()
})

const sidOf = (c: Community) => /server_id=([a-z2-7]{26})/.exec(c.log)![1]

test('invite a friend from the server menu; invite cards in private messages', async () => {
  const { app, page } = await launchApp(tempDir('quarel-app-'))
  await page.getByRole('button', { name: 'Changer' }).click()
  await page.getByLabel('Adresse du service').fill('localhost:' + id.port)
  await page.getByRole('button', { name: 'Utiliser ce service' }).click()
  await page.getByLabel('Email ou pseudo').fill('bob')
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()

  // alice and bob are friends.
  ctl.run('alice', 'friend-add', 'bob')
  await expect(page.getByRole('button', { name: /Messages privés, 1 demande/ })).toBeVisible()
  await page.getByRole('button', { name: /En attente/ }).click()
  await page.getByRole('button', { name: 'Accepter' }).click()

  // bob owns a server.
  await page.getByRole('button', { name: 'Ajouter un serveur' }).click()
  await page.getByLabel("Lien d'invitation").fill(`quarel://localhost:${srv.port}/${srv.claimCode()}?sid=${sidOf(srv)}&claim=1`)
  await page.getByRole('button', { name: 'Continuer' }).click()
  await page.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await expect(page.getByLabel('Membres').getByTitle('Propriétaire')).toBeVisible()

  // Server menu › Inviter des personnes › Inviter alice: a private message with a single-use invite.
  await page.locator('button.sidebar-head').click()
  await page.getByRole('menuitem', { name: 'Inviter des personnes' }).click()
  const dialog = page.getByRole('dialog', { name: /^Inviter sur/ })
  await dialog.getByRole('button', { name: 'Inviter alice' }).click()
  await expect(dialog.getByRole('list', { name: 'Amis à inviter' })).toContainText('Invitation envoyée')
  let link = ''
  await expect.poll(() => {
    link = /https?:\/\/\S+\/join#\S+/.exec(ctl.run('alice', 'dm-history', 'bob'))?.[0] ?? ''
    return link
  }, { timeout: 15_000 }).toContain(`/join#localhost:${srv.port}/`)
  expect(ctl.run('alice', 'join', link)).toMatch(/Rejoint|membre/i)
  await expect(page.getByLabel('Membres').getByText('alice')).toBeVisible()
  await dialog.getByRole('button', { name: 'Fermer' }).click()
  await page.locator('button.sidebar-head').click()
  await page.getByRole('menuitem', { name: 'Inviter des personnes' }).click()
  await expect(dialog.getByRole('list', { name: 'Amis à inviter' })).toContainText('Déjà membre')
  await dialog.getByRole('button', { name: 'Fermer' }).click()

  // alice invites bob to her own server with a link in a message: a card, "Rejoindre".
  // Another host name: the app accepts one self-signed server per name.
  ctl.run('alice', 'claim', `quarel://127.0.0.1:${other.port}/?sid=${sidOf(other)}`, other.claimCode())
  const created = ctl.run('alice', 'invite')
  const theirs = /Lien à partager : (\S+)/.exec(created)![1]
  // The same server under "localhost" first: that name is bob's self-signed server's.
  ctl.run('alice', 'dm', 'bob', 'Par ici : ' + theirs.replace('127.0.0.1', 'localhost'))
  ctl.run('alice', 'dm', 'bob', 'Viens sur mon serveur : ' + theirs)
  await page.getByRole('button', { name: /Messages privés/ }).click()
  await page.locator('.dm-item', { hasText: 'alice' }).click()
  const cards = page.getByRole('log').getByTestId('invite-card')
  await expect(cards).toHaveCount(3)
  await expect(cards.first().getByRole('button', { name: 'Ouvrir' })).toBeVisible() // the one bob sent: already a member
  await cards.nth(1).getByRole('button', { name: 'Rejoindre' }).click()
  const join = page.getByRole('dialog', { name: 'Rejoindre un serveur' }) // checked at once: the server's preview
  await expect(join).toContainText('Quarel n’en suit qu’un par adresse')
  await join.getByRole('button', { name: 'Annuler' }).click()
  await expect(cards.last()).toContainText('127.0.0.1:' + other.port)
  await cards.last().getByRole('button', { name: 'Rejoindre' }).click()
  await expect(join).toContainText('Serveur authentifié')
  await join.getByRole('button', { name: 'Rejoindre', exact: true }).click()
  await expect(page.getByLabel('Membres').getByText('alice')).toBeVisible()
  await expect(page.getByLabel('Membres').getByTitle('Propriétaire')).toBeVisible() // alice owns this one

  // Back to the conversation: the card now opens the server.
  await page.getByRole('button', { name: /Messages privés/ }).click()
  await page.locator('.dm-item', { hasText: 'alice' }).click()
  await expect(cards.last().getByRole('button', { name: 'Ouvrir' })).toBeVisible()
  await cards.first().getByRole('button', { name: 'Ouvrir' }).click()
  // bob's own server still works (the refused link did not take its host).
  await expect(page.getByLabel('Membres').getByText('alice')).toBeVisible()
  const composer = page.getByLabel('Message pour #général')
  await composer.fill('Bienvenue alice')
  await composer.press('Enter')
  ctl.run('alice', 'use', `https://localhost:${srv.port}`) // alice's test client was on her own server
  await expect.poll(() => ctl.run('alice', 'history', 'général'), { timeout: 15_000 }).toContain('Bienvenue alice')
  await app.close()
})
