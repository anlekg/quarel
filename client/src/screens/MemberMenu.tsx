// Right-click menu on a member of a community server (member list, voice
// participants): their volume for me, message, friend request, and the
// moderation my permissions allow (the server checks everything again).
import type { Member, Ready } from '../api/community'
import { menuRun, menuToast, VolumeSlider, type MenuItem } from '../components/ContextMenu'
import { can, isAdmin, outranks } from '../lib/community'
import { errorMessage } from '../lib/errors'
import { mutedForMe, personKey, setMutedForMe, setVolume, volumeOf } from '../lib/volume'
import { currentAccount } from '../state/account'
import type { ServerConn } from '../state/servers'
import { addFriend, openDirect, socialState } from '../state/social'

const run = (p: Promise<unknown>, done?: string) => menuRun(p.catch((e) => Promise.reject(new Error(errorMessage(e)))), done)

export function memberMenuItems(conn: ServerConn, ready: Ready, m: Member, openDialog: (m: Member) => void): MenuItem[] {
  const me = m.id === ready.member.id
  const account = currentAccount()
  const sameService = !!account && m.issuer === account.issuer
  const friend = sameService && socialState().friends.friends.some((f) => f.id === m.subject)
  const key = personKey(m.issuer, m.subject)
  const vs = ready.voice_states.find((s) => s.member_id === m.id)
  const inVoice = (p: string) => !!vs && can(ready, vs.channel_id, p) && (me || outranks(ready, m))
  const above = outranks(ready, m)
  const items: MenuItem[] = [{ title: m.display_name }]
  if (!me) {
    items.push(
      { custom: <VolumeSlider key={key} label="Volume pour moi" value={volumeOf(key)} onChange={(v) => setVolume(key, v)} /> },
      { label: 'Rendre muet pour moi', checked: mutedForMe(key), onClick: () => setMutedForMe(key, !mutedForMe(key)) },
    )
  }
  items.push({ separator: true }, { label: me ? 'Mon profil sur ce serveur…' : 'Profil et modération…', onClick: () => openDialog(m) })
  if (!me && sameService && !m.bot) {
    if (friend) {
      items.push({ label: 'Envoyer un message privé', onClick: () => run(openDirect(m.subject).then((c) => {
        window.dispatchEvent(new CustomEvent('quarel:goto-dm', { detail: { dmId: c.id } }))
      })) })
    } else {
      items.push({ label: 'Demander en ami', onClick: () => run(addFriend(m.handle.split('@')[0]), 'Demande d’ami envoyée à ' + m.display_name + '.') })
    }
  }
  // Moderation.
  const mod: MenuItem[] = []
  if (vs && inVoice('mute_members')) {
    mod.push({ label: vs.server_mute ? 'Rétablir son micro' : 'Couper son micro (modération)', onClick: () => run(conn.api((c) => c.moderateVoice(m.id, { mute: !vs.server_mute }))) })
  }
  if (vs && inVoice('deafen_members')) {
    mod.push({ label: vs.server_deaf ? 'Rétablir son son' : 'Couper son son (modération)', onClick: () => run(conn.api((c) => c.moderateVoice(m.id, { deaf: !vs.server_deaf }))) })
  }
  if (vs && inVoice('move_members')) {
    mod.push({ label: 'Déconnecter du vocal', danger: true, onClick: () => run(conn.api((c) => c.disconnectVoice(m.id))) })
  }
  if (!me && above && (ready.permissions.server.includes('moderate_members') || isAdmin(ready))) {
    if (m.timeout_until && Date.parse(m.timeout_until) > Date.now()) {
      mod.push({ label: 'Lever l’exclusion temporaire', onClick: () => run(conn.api((c) => c.removeTimeout(m.id))) })
    } else if (!m.bot) {
      mod.push(
        { label: 'Exclure 10 minutes', onClick: () => run(conn.api((c) => c.timeout(m.id, 600)), m.display_name + ' est exclu·e 10 minutes.') },
        { label: 'Exclure 1 heure', onClick: () => run(conn.api((c) => c.timeout(m.id, 3600)), m.display_name + ' est exclu·e 1 heure.') },
      )
    }
  }
  if (!me && above && (ready.permissions.server.includes('kick_members') || isAdmin(ready))) {
    mod.push({ label: 'Expulser…', danger: true, onClick: () => {
      if (confirm('Expulser ' + m.display_name + ' ? Cette personne pourra revenir avec une invitation.')) run(conn.api((c) => c.kick(m.id)), m.display_name + ' a été expulsé·e.')
    } })
  }
  if (!me && above && (ready.permissions.server.includes('ban_members') || isAdmin(ready))) {
    mod.push({ label: 'Bannir…', danger: true, onClick: () => openDialog(m) })
  }
  if (mod.length) items.push({ separator: true }, { title: 'Modération' }, ...mod)
  items.push({ separator: true }, { label: 'Copier l’identifiant complet', onClick: () => {
    navigator.clipboard.writeText(m.handle).then(() => menuToast('Copié : ' + m.handle), () => {})
  } })
  return items
}
