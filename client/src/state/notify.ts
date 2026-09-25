// Desktop notifications for new messages: community servers (following the
// member's notification settings, stored by the server and applied here) and
// private conversations. Nothing while the message is in view, nothing in
// "do not disturb", nothing from blocked people.
import type { Message, NotificationSetting, NotifyLevel, Ready } from '../api/community'
import { prefs } from '../platform'
import { engine, isBlockedMember, onEngineEvent, socialState } from './social'

// What the person is looking at.
type Active = { kind: 'channel'; sid: string; channel: number } | { kind: 'dm'; id: string } | null
let active: Active = null

export function setActive(a: Active) {
  active = a
}

const visible = () => document.visibilityState === 'visible' && document.hasFocus()

// --- settings ---

export const DEFAULT_LEVEL: Exclude<NotifyLevel, 'default'> = 'mentions'
export const desktopEnabled = () => prefs.get('notify.desktop', true)
export const setDesktopEnabled = (on: boolean) => prefs.set('notify.desktop', on)

export const isMuted = (s?: NotificationSetting) => !!s?.muted_until && Date.parse(s.muted_until) > Date.now()

// Effective level of a channel (threads follow their parent), and whether it is muted.
export function channelNotify(ready: Ready, channelId: number): { level: Exclude<NotifyLevel, 'default'>; muted: boolean } {
  const ch = ready.channels.find((c) => c.id === channelId)
  const id = ch?.type === 'thread' && ch.parent_id ? ch.parent_id : channelId
  const list = ready.notification_settings ?? []
  const own = list.find((s) => s.channel_id === id)
  const server = list.find((s) => s.channel_id === 0)
  const level = own && own.level !== 'default' ? own.level : server && server.level !== 'default' ? server.level : DEFAULT_LEVEL
  return { level, muted: isMuted(own) || isMuted(server) }
}

// --- showing ---

function show(title: string, body: string, onClick: () => void, tag: string) {
  if (!desktopEnabled() || typeof Notification === 'undefined' || Notification.permission !== 'granted') return
  if (socialState().presence_setting === 'dnd') return
  try {
    const n = new Notification(title, { body: body.length > 180 ? body.slice(0, 180) + '…' : body, tag, icon: '/icons/icon-192.png', silent: false })
    n.onclick = () => {
      window.focus()
      onClick()
      n.close()
    }
  } catch {
    /* notifications unavailable */
  }
}

// A community server message just arrived.
export function notifyServerMessage(sid: string, serverName: string, ready: Ready, m: Message, mentionsMe: boolean) {
  if (m.author_id === ready.member.id) return
  if (active?.kind === 'channel' && active.sid === sid && active.channel === m.channel_id && visible()) return
  const { level, muted } = channelNotify(ready, m.channel_id)
  if (muted || level === 'none' || (level === 'mentions' && !mentionsMe)) return
  const author = ready.members.find((x) => x.id === m.author_id)
  if (author && isBlockedMember(author.issuer, author.subject)) return
  const channel = ready.channels.find((c) => c.id === m.channel_id)
  const names = new Map(ready.members.map((x) => [x.id, x.display_name]))
  const text = (m.content || (m.attachments.length ? '📎 ' + m.attachments[0].filename : ''))
    .replace(/<@&(\d+)>/g, (_, r) => '@' + (ready.roles.find((x) => String(x.id) === r)?.name ?? 'rôle'))
    .replace(/<@([a-z0-9]+)>/g, (_, id) => '@' + (names.get(id) ?? 'membre'))
  show((author?.display_name ?? 'Quelqu’un') + ' · #' + (channel?.name ?? '') + ' · ' + serverName, text, () => {
    window.dispatchEvent(new CustomEvent('quarel:goto-channel', { detail: { sid, channelId: m.channel_id, messageId: m.id } }))
  }, 'srv-' + sid + '-' + m.channel_id)
}

// Private messages: the engine tells which conversation changed.
const lastSeen = new Map<string, number>()
onEngineEvent((ev) => {
  if (ev.kind !== 'history') return
  const e = engine()
  const list = e?.history(ev.dmId) ?? []
  const last = list[list.length - 1]
  if (!e || !last) return
  const prev = lastSeen.get(ev.dmId) ?? 0
  if (last.event_id <= prev) return
  lastSeen.set(ev.dmId, last.event_id)
  if (last.from === e.userId || Date.now() - Date.parse(last.at) > 60_000) return // mine, or history catching up
  if (active?.kind === 'dm' && active.id === ev.dmId && visible()) return
  const conv = socialState().conversations.find((c) => c.id === ev.dmId)
  const who = e.name(last.from)
  const title = conv?.kind === 'group' ? who + ' · ' + (conv.name || 'Groupe') : who
  show(title, last.file ? '📎 ' + last.file.name + (last.text ? ' — ' + last.text : '') : last.text, () => {
    window.dispatchEvent(new CustomEvent('quarel:goto-dm', { detail: { dmId: ev.dmId } }))
  }, 'dm-' + ev.dmId)
})

// Web: the browser asks once; the desktop app always allows.
export async function askPermission(): Promise<NotificationPermission | 'unsupported'> {
  if (typeof Notification === 'undefined') return 'unsupported'
  if (Notification.permission === 'default') return Notification.requestPermission()
  return Notification.permission
}
