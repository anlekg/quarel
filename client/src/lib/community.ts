// Helpers over a community server's READY state.
import type { Channel, Member, Ready } from '../api/community'
import { roleColor } from './format'

export function can(r: Ready, channel: number, perm: string) {
  return r.permissions.channels[String(channel)]?.includes(perm) ?? false
}

export function canServer(r: Ready, perm: string) {
  return r.permissions.server.includes(perm) || r.permissions.server.includes('administrator')
}

export function canPost(r: Ready, c: Channel) {
  return can(r, c.id, 'send_messages') && (c.type !== 'announcement' || can(r, c.id, 'manage_messages'))
}

const byPos = (a: Channel, b: Channel) => a.position - b.position || a.id - b.id

export interface ChannelNode {
  channel: Channel
  threads: Channel[]
}

export interface ChannelGroup {
  category: Channel | null
  items: ChannelNode[]
}

// Root channels first, then each category with its channels; threads under their channel.
export function channelTree(channels: Channel[]): ChannelGroup[] {
  const threads = (id: number) => channels.filter((c) => c.type === 'thread' && c.parent_id === id).sort(byPos)
  const node = (c: Channel): ChannelNode => ({ channel: c, threads: threads(c.id) })
  const inside = (id: number | null) =>
    channels.filter((c) => c.type !== 'category' && c.type !== 'thread' && c.parent_id === id).sort(byPos).map(node)
  const groups: ChannelGroup[] = []
  const roots = inside(null)
  if (roots.length) groups.push({ category: null, items: roots })
  for (const cat of channels.filter((c) => c.type === 'category').sort(byPos)) {
    groups.push({ category: cat, items: inside(cat.id) })
  }
  return groups
}

export function firstTextChannel(channels: Channel[]): Channel | undefined {
  for (const g of channelTree(channels)) {
    const c = g.items.find((n) => n.channel.type === 'text' || n.channel.type === 'announcement')
    if (c) return c.channel
  }
  return undefined
}

export function memberColor(r: Ready, m: Member | undefined): string | undefined {
  if (!m) return undefined
  const roles = r.roles.filter((x) => m.roles.includes(x.id) && x.color).sort((a, b) => b.position - a.position)
  return roleColor(roles[0]?.color ?? 0)
}

export function memberAvatar(m: Member): string | undefined {
  if (m.bot || m.issuer.startsWith('#')) return undefined
  const host = m.issuer.replace(/:\d+$/, '')
  const local = host === 'localhost' || /^127\./.test(host) || host === '[::1]'
  return (local ? 'http://' : 'https://') + m.issuer + '/v1/users/' + encodeURIComponent(m.subject) + '/avatar'
}

// --- administration ---

export const permGroups: { title: string; perms: [string, string][] }[] = [
  { title: 'Général', perms: [
    ['administrator', 'Administrateur (toutes les permissions, ignore les salons)'],
    ['manage_server', 'Gérer le serveur (nom, accès, règles, bots)'],
    ['manage_roles', 'Gérer les rôles et les droits des salons'],
    ['manage_channels', 'Gérer les salons'],
    ['view_audit_log', 'Voir le journal de modération'],
    ['create_invite', 'Créer des invitations'],
    ['view_channel', 'Voir les salons'],
  ] },
  { title: 'Membres', perms: [
    ['kick_members', 'Expulser'],
    ['ban_members', 'Bannir'],
    ['moderate_members', 'Exclure temporairement'],
  ] },
  { title: 'Messages', perms: [
    ['send_messages', 'Envoyer des messages'],
    ['manage_messages', 'Gérer les messages (supprimer, épingler, annonces)'],
    ['mention_everyone', 'Mentionner @everyone et tous les rôles'],
    ['add_reactions', 'Ajouter des réactions'],
    ['attach_files', 'Joindre des fichiers'],
  ] },
  { title: 'Vocal', perms: [
    ['connect', 'Se connecter'],
    ['speak', 'Parler'],
    ['stream', 'Caméra et partage d’écran'],
    ['mute_members', 'Couper le micro des autres'],
    ['deafen_members', 'Mettre les autres en sourdine'],
    ['move_members', 'Déplacer et déconnecter'],
  ] },
]

// Permissions that channels can override.
export const channelPerms = new Set(['view_channel', 'send_messages', 'manage_messages', 'mention_everyone', 'manage_channels', 'connect', 'speak',
  'stream', 'add_reactions', 'attach_files', 'mute_members', 'deafen_members', 'move_members'])

export const isAdmin = (r: Ready) => r.member.owner || r.permissions.server.includes('administrator')

// Position of my highest role (the owner is above everything).
export function myTop(r: Ready): number {
  if (r.member.owner) return Infinity
  return Math.max(0, ...r.roles.filter((x) => r.member.roles.includes(x.id)).map((x) => x.position))
}

export function memberTop(r: Ready, m: Member): number {
  if (m.owner) return Infinity
  return Math.max(0, ...r.roles.filter((x) => m.roles.includes(x.id)).map((x) => x.position))
}

// Whether I may act on this member (strictly above them, never on the owner or myself).
export const outranks = (r: Ready, m: Member) => !m.owner && m.id !== r.member.id && myTop(r) > memberTop(r, m)

// A permission I may grant (I must have it myself).
export const mayGrant = (r: Ready, perm: string) => isAdmin(r) || r.permissions.server.includes(perm)
