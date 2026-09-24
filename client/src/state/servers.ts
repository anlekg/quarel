// Community servers joined by the signed-in account: sessions (kept in the
// secret store), real-time connection, and the live state of each server.
import { useSyncExternalStore } from 'react'
import { ApiError } from '../api/http'
import {
  CommunityClient, type Channel, type LoginResult, type Member, type Message, type ReadState, type Ready,
  type Role, type ServerInfo, type VoiceState,
} from '../api/community'
import { toBase64url } from '../lib/base64'
import { deviceKey, signWithDevice } from '../lib/device'
import { identityLabel } from '../lib/identityURL'
import type { Invite } from '../lib/invite'
import { checkServer, pinServer, secrets } from '../platform'
import { identityClient, serverRules, signOut, type Account } from './account'

export interface SavedServer {
  sid: string
  base: string
  host: string
  name: string
  token: string
  expiresAt: string
  memberId: string
}

export interface ChannelMessages {
  list: Message[]
  hasMore: boolean
  loading: boolean
}

export type Status = 'connecting' | 'ready' | 'offline' | 'removed'

export interface ServerState {
  status: Status
  removed?: 'kicked' | 'banned' | 'left' | 'disabled' | 'blocked' | 'not_approved'
  blockedReason?: string
  ready?: Ready
  reads: Record<number, ReadState>
  messages: Record<number, ChannelMessages>
  typing: Record<number, Record<string, number>> // channel → member → shown until (ms)
}

const PAGE = 50
const TYPING_MS = 8000

function utf8(s: string) {
  return new TextEncoder().encode(s)
}

// Signs in to a community server: identity token + proof of the device key
// over the server's nonce. The server ID it announces must be the expected one
// (the TLS layer already checked the certificate against it).
export async function communityLogin(account: Account, base: string, sid: string, invite?: string, claim?: string): Promise<LoginResult> {
  const c = new CommunityClient(base)
  const rules = await serverRules(account)
  if (rules.blocked.has(sid)) throw new ApiError(0, 'server_blocked', rules.blocked.get(sid) ?? '')
  let idToken: string
  try {
    const audience = rules.policy.server_policy === 'approved' ? sid : undefined
    idToken = (await identityClient(account).identityToken(audience)).token
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) await signOut({ remote: false })
    throw e
  }
  const [ch, dk] = await Promise.all([c.challenge(), deviceKey(identityLabel(account.identity))])
  if (ch.server_id !== sid) throw new ApiError(0, 'server_mismatch', 'server identity changed')
  const proof = toBase64url(signWithDevice(dk, utf8('quarel-auth-v1\0' + sid + '\0' + ch.nonce)))
  return c.login({ identity_token: idToken, nonce: ch.nonce, proof, invite, claim })
}

// Public information shown before joining; also proves the certificate.
export async function previewServer(account: Account, inv: Invite): Promise<ServerInfo> {
  const rules = await serverRules(account, true)
  if (rules.blocked.has(inv.sid)) throw new ApiError(0, 'server_blocked', rules.blocked.get(inv.sid) ?? '')
  const url = new URL(inv.base)
  const check = await checkServer(inv.host, Number(url.port || 443), inv.sid)
  if (check === 'unreachable') throw new ApiError(0, 'network', 'server unreachable')
  if (check === 'mismatch') throw new ApiError(0, 'server_mismatch', 'certificate does not match the invite')
  await pinServer(inv.host, inv.sid)
  const info = await new CommunityClient(inv.base).info()
  if (info.id !== inv.sid) throw new ApiError(0, 'server_mismatch', 'server identity changed')
  return info
}

export class ServerConn {
  state: ServerState = { status: 'connecting', reads: {}, messages: {}, typing: {} }
  private listeners = new Set<() => void>()
  private ws: WebSocket | null = null
  private retry = 0
  private retryTimer: ReturnType<typeof setTimeout> | undefined
  private closed = false
  private relogin: Promise<void> | null = null

  constructor(
    public saved: SavedServer,
    private account: Account,
    private persist: () => void,
  ) {}

  subscribe = (l: () => void) => {
    this.listeners.add(l)
    return () => this.listeners.delete(l)
  }

  private set(patch: Partial<ServerState>) {
    this.state = { ...this.state, ...patch }
    for (const l of this.listeners) l()
  }

  get client() {
    return new CommunityClient(this.saved.base, this.saved.token)
  }

  get me(): Member | undefined {
    return this.state.ready?.member
  }

  // --- session ---

  private async refreshSession() {
    this.relogin ??= (async () => {
      try {
        const res = await communityLogin(this.account, this.saved.base, this.saved.sid)
        this.saved = { ...this.saved, token: res.session_token, expiresAt: res.expires_at, name: res.server.name, memberId: res.member.id }
        this.persist()
      } catch (e) {
        if (e instanceof ApiError && ['banned', 'invite_required', 'account_disabled', 'server_blocked', 'server_not_approved'].includes(e.code)) {
          const reason = ({ banned: 'banned', account_disabled: 'disabled', server_blocked: 'blocked', server_not_approved: 'not_approved' } as const)[e.code as 'banned'] ?? 'kicked'
          this.markRemoved(reason, e.code === 'server_blocked' ? e.message : undefined)
        }
        throw e
      } finally {
        this.relogin = null
      }
    })()
    return this.relogin
  }

  // Calls the REST API, signing in again once if the session has expired.
  async api<T>(fn: (c: CommunityClient) => Promise<T>): Promise<T> {
    if (Date.parse(this.saved.expiresAt) - Date.now() < 60_000) await this.refreshSession()
    try {
      return await fn(this.client)
    } catch (e) {
      if (e instanceof ApiError && e.status === 401 && e.code !== 'invalid_proof') {
        await this.refreshSession()
        return fn(this.client)
      }
      throw e
    }
  }

  // --- real time ---

  start() {
    this.closed = false
    this.connect()
  }

  stop() {
    this.closed = true
    clearTimeout(this.retryTimer)
    this.ws?.close()
    this.ws = null
  }

  private async connect() {
    if (this.closed) return
    try {
      if (Date.parse(this.saved.expiresAt) - Date.now() < 60_000) await this.refreshSession()
    } catch {
      return this.scheduleRetry()
    }
    if (this.closed || this.state.status === 'removed') return
    const ws = new WebSocket(this.client.gatewayURL())
    this.ws = ws
    ws.onopen = () => ws.send(JSON.stringify({ op: 'auth', token: this.saved.token }))
    ws.onmessage = (ev) => {
      try {
        const { t, d } = JSON.parse(ev.data)
        this.handle(t, d)
      } catch (e) {
        console.error('gateway event', e)
      }
    }
    ws.onclose = (ev) => {
      if (this.ws !== ws) return
      this.ws = null
      if (this.closed || this.state.status === 'removed') return
      if (this.state.status === 'ready') this.set({ status: 'offline' })
      if (ev.code === 4001) {
        this.refreshSession().then(() => this.connect(), () => this.scheduleRetry())
        return
      }
      this.scheduleRetry()
    }
  }

  private scheduleRetry() {
    if (this.closed || this.state.status === 'removed') return
    if (this.state.status !== 'offline') this.set({ status: 'offline' })
    const delays = [1000, 2000, 5000, 10000, 30000]
    const delay = delays[Math.min(this.retry++, delays.length - 1)]
    clearTimeout(this.retryTimer)
    this.retryTimer = setTimeout(() => this.connect(), delay)
  }

  markRemoved(reason: ServerState['removed'], blockedReason?: string) {
    this.set({ status: 'removed', removed: reason, blockedReason })
    this.stop()
  }

  private handle(t: string, d: any) {
    const r = this.state.ready
    switch (t) {
      case 'READY': {
        this.retry = 0
        const ready = d as Ready
        const reads: Record<number, ReadState> = {}
        for (const rs of ready.read_states ?? []) reads[rs.channel_id] = rs
        // Messages may have been missed while disconnected: reload on demand.
        this.set({ status: 'ready', ready, reads, messages: {}, typing: {} })
        if (ready.server.name !== this.saved.name) {
          this.saved = { ...this.saved, name: ready.server.name }
          this.persist()
        }
        return
      }
      case 'MESSAGE_CREATE':
        return this.onMessage(d as Message)
      case 'MESSAGE_UPDATE':
        return this.mapMessages(d.channel_id, (list) => list.map((m) => (m.id === d.id ? keepMine(m, d) : m)))
      case 'MESSAGE_DELETE':
        return this.mapMessages(d.channel_id, (list) => list.filter((m) => m.id !== d.id))
      case 'MESSAGE_DELETE_BULK': {
        const ids = new Set<number>(d.ids)
        return this.mapMessages(d.channel_id, (list) => list.filter((m) => !ids.has(m.id)))
      }
      case 'REACTION_ADD':
      case 'REACTION_REMOVE':
        return this.onReaction(d, t === 'REACTION_ADD')
      case 'TYPING_START': {
        if (d.member_id === this.me?.id) return
        const ch = { ...(this.state.typing[d.channel_id] ?? {}), [d.member_id]: Date.now() + TYPING_MS }
        this.set({ typing: { ...this.state.typing, [d.channel_id]: ch } })
        setTimeout(() => this.set({}), TYPING_MS + 100)
        return
      }
      case 'READ_STATE_UPDATE':
        return this.set({ reads: { ...this.state.reads, [d.channel_id]: d } })
    }
    if (!r) return
    switch (t) {
      case 'CHANNEL_CREATE':
      case 'CHANNEL_UPDATE': {
        const c = d as Channel
        const channels = r.channels.some((x) => x.id === c.id) ? r.channels.map((x) => (x.id === c.id ? c : x)) : [...r.channels, c]
        return this.set({ ready: { ...r, channels } })
      }
      case 'CHANNEL_DELETE':
        return this.set({ ready: { ...r, channels: r.channels.filter((x) => x.id !== d.id) } })
      case 'CHANNELS_SYNC':
        return this.set({ ready: { ...r, ...d } })
      case 'MEMBER_JOIN':
      case 'MEMBER_UPDATE': {
        const m = d as Member
        const members = r.members.some((x) => x.id === m.id) ? r.members.map((x) => (x.id === m.id ? m : x)) : [...r.members, m]
        return this.set({ ready: { ...r, members, member: m.id === r.member.id ? m : r.member } })
      }
      case 'MEMBER_LEAVE':
        if (d.id === r.member.id) return this.markRemoved(d.reason === 'banned' ? 'banned' : d.reason === 'left' ? 'left' : 'kicked')
        return this.set({ ready: { ...r, members: r.members.filter((x) => x.id !== d.id) } })
      case 'ROLES_UPDATE':
        return this.set({ ready: { ...r, roles: d as Role[] } })
      case 'ROLE_DELETE':
        return this.set({ ready: { ...r, roles: r.roles.filter((x) => x.id !== d.id) } })
      case 'SERVER_UPDATE':
        return this.set({ ready: { ...r, server: d as ServerInfo } })
      case 'VOICE_STATE_UPDATE': {
        const v = d as VoiceState & { channel_id: number | null }
        const others = r.voice_states.filter((x) => x.member_id !== v.member_id)
        return this.set({ ready: { ...r, voice_states: v.channel_id === null ? others : [...others, v] } })
      }
    }
  }

  private mapMessages(channel: number, fn: (list: Message[]) => Message[]) {
    const cm = this.state.messages[channel]
    if (!cm) return
    this.set({ messages: { ...this.state.messages, [channel]: { ...cm, list: fn(cm.list) } } })
  }

  private onMessage(m: Message) {
    const cm = this.state.messages[m.channel_id]
    const patch: Partial<ServerState> = {}
    if (cm && !cm.list.some((x) => x.id === m.id)) {
      patch.messages = { ...this.state.messages, [m.channel_id]: { ...cm, list: [...cm.list, m] } }
    }
    const typing = this.state.typing[m.channel_id]
    if (typing?.[m.author_id]) {
      const { [m.author_id]: _, ...rest } = typing
      patch.typing = { ...this.state.typing, [m.channel_id]: rest }
    }
    const rs = this.state.reads[m.channel_id] ?? { channel_id: m.channel_id, last_read: 0, last_message_id: 0, unread: 0, mentions: 0 }
    const mine = m.author_id === this.me?.id
    patch.reads = {
      ...this.state.reads,
      [m.channel_id]: mine
        ? { ...rs, last_message_id: m.id, last_read: m.id, unread: 0, mentions: 0 }
        : { ...rs, last_message_id: m.id, unread: Math.min(rs.unread + 1, 100), mentions: rs.mentions + (this.mentionsMe(m) ? 1 : 0) },
    }
    this.set(patch)
  }

  mentionsMe(m: Message) {
    const me = this.me
    if (!me) return false
    return m.mention_everyone || m.mentions?.includes(me.id) || (m.mention_roles ?? []).some((r) => me.roles.includes(r))
  }

  private onReaction(d: { channel_id: number; message_id: number; emoji: string; member_id: string }, add: boolean) {
    const mine = d.member_id === this.me?.id
    this.mapMessages(d.channel_id, (list) =>
      list.map((m) => {
        if (m.id !== d.message_id) return m
        const found = m.reactions.find((x) => x.emoji === d.emoji)
        let reactions: Message['reactions']
        if (add) {
          reactions = found
            ? m.reactions.map((x) => (x.emoji === d.emoji ? { ...x, count: x.count + 1, me: x.me || mine } : x))
            : [...m.reactions, { emoji: d.emoji, count: 1, me: mine }]
        } else {
          reactions = m.reactions
            .map((x) => (x.emoji === d.emoji ? { ...x, count: x.count - 1, me: mine ? false : x.me } : x))
            .filter((x) => x.count > 0)
        }
        return { ...m, reactions }
      }),
    )
  }

  // --- messages ---

  async loadMessages(channel: number, older = false) {
    const cm = this.state.messages[channel]
    if (cm?.loading || (older && !cm?.hasMore) || (!older && cm)) return
    this.set({ messages: { ...this.state.messages, [channel]: { list: cm?.list ?? [], hasMore: cm?.hasMore ?? true, loading: true } } })
    try {
      const before = older ? cm?.list[0]?.id : undefined
      const page = await this.api((c) => c.messages(channel, { before, limit: PAGE }))
      const cur = this.state.messages[channel]?.list ?? []
      const known = new Set(cur.map((m) => m.id))
      const list = older ? [...page.filter((m) => !known.has(m.id)), ...cur] : mergeByID(page, cur)
      this.set({ messages: { ...this.state.messages, [channel]: { list, hasMore: page.length === PAGE, loading: false } } })
    } catch (e) {
      this.set({ messages: { ...this.state.messages, [channel]: { list: cm?.list ?? [], hasMore: cm?.hasMore ?? true, loading: false } } })
      throw e
    }
  }

  // Adds a message returned by the API (it also arrives through the gateway).
  addOwn(m: Message) {
    this.onMessage(m)
  }

  async ack(channel: number) {
    const rs = this.state.reads[channel]
    if (!rs || (rs.unread === 0 && rs.mentions === 0 && rs.last_read >= rs.last_message_id)) return
    const res = await this.api((c) => c.ack(channel))
    this.set({ reads: { ...this.state.reads, [channel]: res } })
  }

  typingIn(channel: number): string[] {
    const now = Date.now()
    return Object.entries(this.state.typing[channel] ?? {})
      .filter(([, until]) => until > now)
      .map(([id]) => id)
  }
}

// Reactions' "me" is only set in direct API responses: keep ours on updates.
function keepMine(old: Message, next: Message): Message {
  const mine = new Set(old.reactions.filter((r) => r.me).map((r) => r.emoji))
  return { ...next, reactions: next.reactions.map((r) => ({ ...r, me: r.me || mine.has(r.emoji) })) }
}

function mergeByID(a: Message[], b: Message[]) {
  const byID = new Map<number, Message>()
  for (const m of [...a, ...b]) byID.set(m.id, m)
  return [...byID.values()].sort((x, y) => x.id - y.id)
}

// --- the list of joined servers ---

let conns: ServerConn[] = []
let owner: Account | null = null
const listeners = new Set<() => void>()

function emit() {
  conns = [...conns]
  for (const l of listeners) l()
}

const storeKey = (a: Account) => 'servers:' + a.user.id

async function save() {
  if (owner) await secrets.set(storeKey(owner), JSON.stringify(conns.map((c) => c.saved)))
}

export function useServers(): ServerConn[] {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => conns,
  )
}

export function useServerState(conn: ServerConn): ServerState {
  return useSyncExternalStore(conn.subscribe, () => conn.state)
}

// Loads the account's servers and connects to each.
export async function openServers(account: Account) {
  closeServers()
  owner = account
  let saved: SavedServer[] = []
  try {
    saved = JSON.parse((await secrets.get(storeKey(account))) ?? '[]')
  } catch {
    saved = []
  }
  for (const s of saved) await pinServer(s.host, s.sid)
  conns = saved.map((s) => new ServerConn(s, account, save))
  emit()
  let blocked = new Map<string, string>()
  try {
    blocked = (await serverRules(account, true)).blocked
  } catch {
    /* identity service unreachable: servers still connect with their current session */
  }
  for (const c of conns) {
    if (blocked.has(c.saved.sid)) c.markRemoved('blocked', blocked.get(c.saved.sid))
    else c.start()
  }
}

export function closeServers() {
  for (const c of conns) c.stop()
  conns = []
  owner = null
  emit()
}

export async function joinServer(account: Account, inv: Invite): Promise<ServerConn> {
  const existing = conns.find((c) => c.saved.sid === inv.sid)
  if (existing) return existing
  const res = inv.claim
    ? await communityLogin(account, inv.base, inv.sid, undefined, inv.code)
    : await communityLogin(account, inv.base, inv.sid, inv.code)
  const conn = new ServerConn(
    { sid: inv.sid, base: inv.base, host: inv.host, name: res.server.name, token: res.session_token, expiresAt: res.expires_at, memberId: res.member.id },
    account,
    save,
  )
  conns.push(conn)
  await save()
  emit()
  conn.start()
  return conn
}

// Leaves the server (if still a member) and forgets it.
export async function leaveServer(conn: ServerConn, remote = true) {
  if (remote && conn.state.status !== 'removed') await conn.api((c) => c.leave())
  conn.stop()
  conns = conns.filter((c) => c !== conn)
  await save()
  emit()
}
