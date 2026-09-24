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

  gatewayURL() {
    return this.base.replace(/^http/, 'ws') + '/v1/gateway'
  }
}
