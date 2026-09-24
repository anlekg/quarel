// Client for a community server (see docs/api.md).
import { request } from './http'

export interface ServerInfo {
  id: string
  name: string
  access: 'private' | 'public'
  member_count: number
  rules: string
  require_phone: boolean
  phone_verification: boolean
}

export interface Member {
  id: string
  handle: string
  issuer: string
  subject: string
  nickname?: string
  display_name: string
  owner: boolean
  roles: number[]
  joined_at: string
  bot: boolean
  timeout_until: string | null
  rules_accepted: boolean
  phone_verified: boolean
}

export interface Role {
  id: number
  name: string
  color: number
  position: number
  permissions: string[]
  mentionable: boolean
  hoist: boolean
}

export type ChannelType = 'text' | 'voice' | 'category' | 'announcement' | 'thread'

export interface Channel {
  id: number
  type: ChannelType
  name: string
  topic: string
  parent_id: number | null
  position: number
  thread_starter?: number
  overrides?: Override[]
}

export interface Override {
  type: 'role' | 'member'
  id: string // role id (as a string) or member id
  allow: string[]
  deny: string[]
}

export interface Invite {
  code: string
  creator_id: string | null
  max_uses: number | null
  uses: number
  expires_at: string | null
  created_at: string
}

export interface Ban {
  member: Member
  reason: string
  banned_by: string | null
  created_at: string
}

export interface AuditEntry {
  id: number
  actor_id: string | null
  action: string
  target_id: string | null
  reason: string
  details: Record<string, unknown> | null
  created_at: string
  actor_name?: string // kept even after the member left
  target_name?: string
}

export interface Attachment {
  id: string
  filename: string
  content_type: string
  size: number
  url: string
}

export interface Embed {
  url: string
  title: string
  description: string
  site_name: string
  image_url: string
}

export interface Reaction {
  emoji: string
  count: number
  me: boolean
}

export interface Message {
  id: number
  channel_id: number
  author_id: string
  content: string
  mentions: string[]
  mention_roles: number[]
  mention_everyone: boolean
  reply_to: number | null
  referenced?: { id: number; author_id: string; content: string }
  attachments: Attachment[]
  embeds: Embed[]
  reactions: Reaction[]
  pinned_at: string | null
  thread_id: number | null
  created_at: string
  edited_at: string | null
}

export interface ReadState {
  channel_id: number
  last_read: number
  last_message_id: number
  unread: number
  mentions: number
}

export interface VoiceState {
  member_id: string
  channel_id: number
  self_mute: boolean
  self_deaf: boolean
  server_mute: boolean
  server_deaf: boolean
  video: boolean
  screen: boolean
}

export type Restriction = '' | 'timed_out' | 'rules_not_accepted' | 'phone_not_verified'

export interface Permissions {
  server: string[]
  channels: Record<string, string[]>
}

export interface Ready {
  member: Member
  server: ServerInfo
  roles: Role[]
  members: Member[]
  channels: Channel[]
  voice_states: VoiceState[]
  read_states: ReadState[]
  restriction: Restriction
  permissions: Permissions
}

export interface LoginResult {
  session_token: string
  expires_at: string
  member: Member
  server: ServerInfo
  joined: boolean
}

export class CommunityClient {
  constructor(
    public base: string,
    public token?: string,
  ) {}

  private call<T>(method: string, path: string, body?: unknown) {
    return request<T>(this.base, method, path, { token: this.token, body })
  }

  info() {
    return this.call<ServerInfo>('GET', '/v1/server')
  }

  challenge() {
    return this.call<{ server_id: string; nonce: string }>('POST', '/v1/auth/challenge')
  }

  login(req: { identity_token: string; nonce: string; proof: string; invite?: string; claim?: string }) {
    return this.call<LoginResult>('POST', '/v1/auth/login', req)
  }

  logout() {
    return this.call<void>('POST', '/v1/auth/logout')
  }

  leave() {
    return this.call<void>('DELETE', '/v1/members/@me')
  }

  messages(channel: number, opts: { before?: number; after?: number; limit?: number } = {}) {
    const q = new URLSearchParams()
    for (const [k, v] of Object.entries(opts)) if (v !== undefined) q.set(k, String(v))
    return this.call<Message[]>('GET', `/v1/channels/${channel}/messages?${q}`)
  }

  send(channel: number, body: { content: string; reply_to?: number; attachments?: string[] }) {
    return this.call<Message>('POST', `/v1/channels/${channel}/messages`, body)
  }

  edit(channel: number, id: number, content: string) {
    return this.call<Message>('PATCH', `/v1/channels/${channel}/messages/${id}`, { content })
  }

  remove(channel: number, id: number) {
    return this.call<void>('DELETE', `/v1/channels/${channel}/messages/${id}`)
  }

  react(channel: number, id: number, emoji: string, on: boolean) {
    return this.call<void>(on ? 'PUT' : 'DELETE', `/v1/channels/${channel}/messages/${id}/reactions/${encodeURIComponent(emoji)}`)
  }

  upload(channel: number, file: File) {
    const form = new FormData()
    form.append('file', file)
    return this.call<Attachment>('POST', `/v1/channels/${channel}/attachments`, form)
  }

  typing(channel: number) {
    return this.call<void>('POST', `/v1/channels/${channel}/typing`)
  }

  ack(channel: number, messageID?: number) {
    return this.call<ReadState>('POST', `/v1/channels/${channel}/ack`, messageID ? { message_id: messageID } : {})
  }

  createInvite(maxUses = 0, expiresIn?: number) {
    return this.call<{ code: string }>('POST', '/v1/invites', { max_uses: maxUses, expires_in: expiresIn })
  }

  acceptRules() {
    return this.call<void>('POST', '/v1/members/@me/accept-rules')
  }

  phoneStart(phone: string) {
    return this.call<void>('POST', '/v1/members/@me/phone', { phone })
  }

  phoneVerify(phone: string, code: string) {
    return this.call<void>('POST', '/v1/members/@me/phone/verify', { phone, code })
  }

  voiceJoin(channel: number) {
    return this.call<{ url: string; token: string; room: string; can_speak: boolean; can_stream: boolean; server_mute: boolean; server_deaf: boolean }>(
      'POST', `/v1/channels/${channel}/voice/join`)
  }

  voiceState(state: { self_mute: boolean; self_deaf: boolean }) {
    return this.call<void>('PATCH', '/v1/voice/state', state)
  }

  voiceLeave() {
    return this.call<void>('POST', '/v1/voice/leave')
  }

  // --- administration and moderation ---

  updateServer(body: Partial<Pick<ServerInfo, 'name' | 'access' | 'rules' | 'require_phone'>>) {
    return this.call<ServerInfo>('PATCH', '/v1/server', body)
  }

  createRole(body: { name: string; color?: number; permissions?: string[]; mentionable?: boolean; hoist?: boolean }) {
    return this.call<Role>('POST', '/v1/roles', body)
  }

  updateRole(id: number, body: Partial<Omit<Role, 'id'>>) {
    return this.call<Role>('PATCH', '/v1/roles/' + id, body)
  }

  deleteRole(id: number) {
    return this.call<void>('DELETE', '/v1/roles/' + id)
  }

  setMemberRole(member: string, role: number, on: boolean) {
    return this.call<void>(on ? 'PUT' : 'DELETE', '/v1/members/' + encodeURIComponent(member) + '/roles/' + role)
  }

  createChannel(body: { type: string; name: string; topic?: string; parent_id?: number | null; position?: number }) {
    return this.call<Channel>('POST', '/v1/channels', body)
  }

  updateChannel(id: number, body: { name?: string; topic?: string; parent_id?: number | null; position?: number }) {
    return this.call<Channel>('PATCH', '/v1/channels/' + id, body)
  }

  deleteChannel(id: number) {
    return this.call<void>('DELETE', '/v1/channels/' + id)
  }

  setOverride(channel: number, type: 'role' | 'member', target: string, allow: string[], deny: string[]) {
    return this.call<void>('PUT', '/v1/channels/' + channel + '/overrides/' + type + '/' + encodeURIComponent(target), { allow, deny })
  }

  deleteOverride(channel: number, type: 'role' | 'member', target: string) {
    return this.call<void>('DELETE', '/v1/channels/' + channel + '/overrides/' + type + '/' + encodeURIComponent(target))
  }

  kick(member: string, reason?: string) {
    return this.call<void>('POST', '/v1/members/' + encodeURIComponent(member) + '/kick', { reason })
  }

  ban(member: string, reason?: string, deleteMessages = 0) {
    return this.call<void>('PUT', '/v1/bans/' + encodeURIComponent(member), { reason, delete_messages: deleteMessages })
  }

  unban(member: string) {
    return this.call<void>('DELETE', '/v1/bans/' + encodeURIComponent(member))
  }

  bans() {
    return this.call<Ban[]>('GET', '/v1/bans')
  }

  timeout(member: string, duration: number, reason?: string) {
    return this.call<void>('PUT', '/v1/members/' + encodeURIComponent(member) + '/timeout', { duration, reason })
  }

  removeTimeout(member: string) {
    return this.call<void>('DELETE', '/v1/members/' + encodeURIComponent(member) + '/timeout')
  }

  invites() {
    return this.call<Invite[]>('GET', '/v1/invites')
  }

  deleteInvite(code: string) {
    return this.call<void>('DELETE', '/v1/invites/' + encodeURIComponent(code))
  }

  auditLog(opts: { before?: number; action?: string } = {}) {
    const q = new URLSearchParams({ limit: '50' })
    if (opts.before) q.set('before', String(opts.before))
    if (opts.action) q.set('action', opts.action)
    return this.call<AuditEntry[]>('GET', '/v1/audit-log?' + q)
  }

  async bots(): Promise<Member[]> {
    return (await this.call<{ member: Member }[]>('GET', '/v1/bots')).map((b) => b.member)
  }

  createBot(name: string) {
    return this.call<{ member: Member; token: string }>('POST', '/v1/bots', { name })
  }

  resetBotToken(id: string) {
    return this.call<{ member: Member; token: string }>('POST', '/v1/bots/' + encodeURIComponent(id) + '/token')
  }

  deleteBot(id: string) {
    return this.call<void>('DELETE', '/v1/bots/' + encodeURIComponent(id))
  }

  moderateVoice(member: string, body: { mute?: boolean; deaf?: boolean; channel_id?: number; reason?: string }) {
    return this.call<void>('PATCH', '/v1/voice/states/' + encodeURIComponent(member), body)
  }

  disconnectVoice(member: string) {
    return this.call<void>('DELETE', '/v1/voice/states/' + encodeURIComponent(member))
  }

  gatewayURL() {
    return this.base.replace(/^http/, 'ws') + '/v1/gateway'
  }
}
