// Client for a community server (see site/src/content/docs/wiki/developper/api.md).
import { request } from './http'

// Automatic moderation rules (manage_server).
export interface AutoMod {
  words: string[] // banned words or phrases ("mot*": words starting with "mot")
  block_links: boolean
  max_mentions: number // 0: no limit
  duplicates: boolean // the same message a third time within 30 s
  timeout: number // seconds of automatic timeout after 3 refusals in 10 min (0: none)
}

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

export type ChannelType = 'text' | 'voice' | 'category' | 'announcement' | 'thread' | 'forum'

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
  webhook?: { id: string; name: string } // posted by this incoming webhook
  interaction?: { name: string; member_id: string } // a bot's reply to this slash command, run by this member
}

// A bot's slash command.
export interface CommandOption {
  name: string
  description: string
  type: 'string' | 'integer' | 'boolean' | 'member' | 'channel'
  required: boolean
}

export interface BotCommand {
  bot_id: string
  name: string
  description: string
  options: CommandOption[]
}

// A command reply only its author sees (never stored).
export interface EphemeralReply {
  interaction_id: string
  channel_id: number
  bot_id: string
  name: string
  content: string
  at: number // received (ms)
}

// A post of a forum channel: a thread with a title, started by its first message.
export interface ForumPost {
  channel: Channel
  author_id: string
  excerpt: string
  message_count: number
  last_message_at: string
}

// Incoming webhook of a channel; token only in the creation answer.
export interface Webhook {
  id: string
  channel_id: number
  name: string
  created_by: string | null
  created_at: string
  token?: string
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

export type NotifyLevel = 'default' | 'all' | 'mentions' | 'none'

export interface NotificationSetting {
  channel_id: number // 0: whole server
  level: NotifyLevel
  muted_until: string | null
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
  notification_settings?: NotificationSetting[]
  emojis?: Emoji[]
}

// A custom emoji of the server, written <:name:id> in messages and reactions.
export interface Emoji {
  id: string
  name: string
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

  login(req: { identity_token: string; nonce: string; proof: string; host: string; tls: 'binding' | 'authority'; invite?: string; claim?: string }) {
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

  // --- pins, threads, search, notifications ---

  pins(channel: number) {
    return this.call<Message[]>('GET', '/v1/channels/' + channel + '/pins')
  }

  pin(channel: number, id: number, on: boolean) {
    return this.call<void>(on ? 'PUT' : 'DELETE', '/v1/channels/' + channel + '/pins/' + id)
  }

  createThread(channel: number, message: number, name?: string) {
    return this.call<Channel>('POST', '/v1/channels/' + channel + '/messages/' + message + '/threads', name ? { name } : {})
  }

  search(q: string, opts: { channel_id?: number; before?: number } = {}) {
    const p = new URLSearchParams({ q, limit: '25' })
    if (opts.channel_id) p.set('channel_id', String(opts.channel_id))
    if (opts.before) p.set('before', String(opts.before))
    return this.call<Message[]>('GET', '/v1/search?' + p)
  }

  setNotification(channel: number, level: NotifyLevel, muteFor = 0) {
    return this.call<NotificationSetting[]>('PUT', '/v1/notification-settings/' + channel, { level, mute_for: muteFor })
  }

  // --- administration and moderation ---

  updateServer(body: Partial<Pick<ServerInfo, 'name' | 'access' | 'rules' | 'require_phone'>>) {
    return this.call<ServerInfo>('PATCH', '/v1/server', body)
  }

  posts(forum: number) {
    return this.call<ForumPost[]>('GET', '/v1/channels/' + forum + '/posts')
  }

  createPost(forum: number, title: string, content: string) {
    return this.call<ForumPost>('POST', '/v1/channels/' + forum + '/posts', { title, content })
  }

  emojis() {
    return this.call<Emoji[]>('GET', '/v1/emojis')
  }

  uploadEmoji(name: string, image: Blob) {
    return this.call<Emoji>('POST', '/v1/emojis?name=' + encodeURIComponent(name), image)
  }

  deleteEmoji(id: string) {
    return this.call<void>('DELETE', '/v1/emojis/' + id)
  }

  commands() {
    return this.call<BotCommand[]>('GET', '/v1/commands')
  }

  runCommand(channel: number, botId: string, name: string, options: Record<string, unknown>) {
    return this.call<{ id: string }>('POST', '/v1/channels/' + channel + '/commands', { bot_id: botId, name, options })
  }

  webhooks(channel: number) {
    return this.call<Webhook[]>('GET', '/v1/channels/' + channel + '/webhooks')
  }

  createWebhook(channel: number, name: string) {
    return this.call<Webhook>('POST', '/v1/channels/' + channel + '/webhooks', { name })
  }

  deleteWebhook(id: string) {
    return this.call<void>('DELETE', '/v1/webhooks/' + id)
  }

  autoMod() {
    return this.call<AutoMod>('GET', '/v1/server/automod')
  }

  setAutoMod(body: AutoMod) {
    return this.call<AutoMod>('PUT', '/v1/server/automod', body)
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

  // The owner hands the server over (they stay a member, without special rights).
  transferOwnership(member: string) {
    return this.call<void>('POST', '/v1/members/' + encodeURIComponent(member) + '/transfer-ownership')
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
