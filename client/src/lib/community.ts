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
