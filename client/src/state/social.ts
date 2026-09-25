// Friends and private conversations of the signed-in account: the Identity
// service's real-time connection, and the end-to-end encryption engine.
import { useSyncExternalStore } from 'react'
import type { Conversation, FriendLists, InboxItem, Presence, Profile, PublicUser } from '../api/identity'
import { bumpAvatar } from '../components/Avatar'
import { ApiError } from '../api/http'
import { loadCrypto } from '../crypto'
import { E2E, type BackupStatus, type E2EEvent, type FileRef } from '../e2e/engine'
import { Files, type SendResult } from '../e2e/files'
import { verificationCode } from '../e2e/keys'
import { identityClient, signOut, type Account } from './account'

export interface SocialState {
  status: 'starting' | 'ready' | 'offline' | 'error'
  error?: string
  validated: boolean // this device may send (holds the master key)
  code: string // this device's verification code
  pendingDevices: number // own devices waiting for validation (counted on validated devices)
  backup: 'unknown' | BackupStatus
  presence_setting: Presence // my own setting
  me: Partial<Profile> // my profile (bio, avatar), from USER_UPDATE
  blocked: string[] // ids of the people I blocked (their messages are hidden on servers)
  backupAt?: string
  friends: FriendLists
  presence: Record<string, string> // friend id → online | idle | dnd | offline
  conversations: Conversation[]
  typing: Record<string, Record<string, number>> // dm → user → until (ms)
  reads: Record<string, Record<string, number>> // dm → user → last event id read
  version: number // bumped when a history changes
  warnings: string[]
}

const empty: SocialState = {
  status: 'starting', validated: false, code: '', pendingDevices: 0, backup: 'unknown', presence_setting: 'online', me: {}, blocked: [], friends: { friends: [], incoming: [], outgoing: [] }, presence: {},
  conversations: [], typing: {}, reads: {}, version: 0, warnings: [],
}

let state: SocialState = empty
let e2e: E2E | null = null
let files: Files | null = null
let account: Account | null = null
let ws: WebSocket | null = null
let closed = true
let retry = 0
let retryTimer: ReturnType<typeof setTimeout> | undefined
const listeners = new Set<() => void>()

function set(patch: Partial<SocialState>) {
  state = { ...state, ...patch }
  for (const l of listeners) l()
}

export function useSocial(): SocialState {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => state,
  )
}

export function engine() {
  return e2e
}

export function socialState() {
  return state
}

// The Identity client of the signed-in account (null when signed out).
export function identityAPI() {
  return account ? identityClient(account) : null
}

const api = () => identityClient(account!)

async function refreshFriends() {
  const friends = await api().friends()
  const presence: Record<string, string> = {}
  for (const f of friends.friends) presence[f.id] = f.presence ?? 'offline'
  set({ friends, presence })
}

async function refreshConversations() {
  set({ conversations: await api().conversations() })
}

export async function openSocial(a: Account) {
  closeSocial()
  account = a
  closed = false
  set({ ...empty })
  try {
    const c = await loadCrypto()
    e2e = await E2E.open(c, api(), a.user.id, a.user.pseudo, a.sessionId)
    e2e.on(onEngine)
    for (const l of engineListeners) l({ kind: 'servers' }) // the engine is ready: servers can sync
    files = new Files(e2e, api(), (convId) => state.conversations.find((c) => c.id === convId)?.members.map((m) => m.id))
    set({ validated: e2e.validated, code: verificationCode(e2e.ed25519) })
    await Promise.all([refreshFriends(), refreshConversations(), refreshDevices(), refreshBackup(), refreshBlocks()])
    set({ status: 'ready' })
    connect()
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return signOut({ remote: false })
    set({ status: 'error', error: err instanceof Error ? err.message : String(err) })
  }
}

export function closeSocial() {
  closed = true
  clearTimeout(retryTimer)
  ws?.close()
  ws = null
  files?.close()
  files = null
  e2e = null
  account = null
}

const engineListeners = new Set<(ev: E2EEvent) => void>()

// Engine events for other modules (calls), across sign-ins.
export function onEngineEvent(l: (ev: E2EEvent) => void) {
  engineListeners.add(l)
  return () => engineListeners.delete(l)
}

function onEngine(ev: E2EEvent) {
  for (const l of engineListeners) l(ev)
  if (ev.kind === 'history') set({ version: state.version + 1 })
  if (ev.kind === 'approved') {
    set({ validated: true })
    refreshDevices().catch(() => {})
    refreshBackup().catch(() => {})
  }
  if (ev.kind === 'backup') refreshBackup().catch(() => {})
  if (ev.kind === 'warning') set({ warnings: [...state.warnings.slice(-4), ev.text] })
}

function connect() {
  if (closed || !account) return
  const sock = new WebSocket(api().gatewayURL())
  ws = sock
  sock.onopen = () => sock.send(JSON.stringify({ op: 'auth', token: account!.token }))
  sock.onmessage = (m) => {
    try {
      const { t, d } = JSON.parse(m.data)
      handle(t, d)
    } catch (err) {
      console.error('identity gateway', err)
    }
  }
  sock.onclose = (ev) => {
    if (ws !== sock || closed) return
    ws = null
    if (ev.code === 4001) return void signOut({ remote: false })
    set({ status: 'offline' })
    const delays = [1000, 2000, 5000, 10000, 30000]
    retryTimer = setTimeout(connect, delays[Math.min(retry++, delays.length - 1)])
  }
}

function handle(t: string, d: any) {
  switch (t) {
    case 'READY':
      retry = 0
      set({ status: 'ready', presence_setting: d?.presence || 'online' })
      e2e?.sync().catch(() => {})
      refreshFriends().catch(() => {})
      refreshConversations().catch(() => {})
      return
    case 'INBOX':
      // A message in a conversation this device does not know yet: fetch the list.
      if ((d as InboxItem[]).some((it) => it.dm_id && !state.conversations.some((c) => c.id === it.dm_id))) refreshConversations().catch(() => {})
      e2e?.sync(d as InboxItem[]).catch(() => {})
      return
    case 'FRIENDS_UPDATE':
    case 'USER_UPDATE':
      if (d?.id) bumpAvatar(d.id)
      if (d?.id === account?.user.id) set({ me: { ...state.me, ...d } })
      refreshFriends().catch(() => {})
      return
    case 'PRESENCE_SETTING':
      set({ presence_setting: d.status })
      return
    case 'PRESENCE_UPDATE':
      set({ presence: { ...state.presence, [d.user_id]: d.status } })
      return
    case 'DM_UPDATE': {
      const c = d as Conversation
      const others = state.conversations.filter((x) => x.id !== c.id)
      set({ conversations: [c, ...others] })
      return
    }
    case 'DM_REMOVED':
      set({ conversations: state.conversations.filter((x) => x.id !== d.id) })
      return
    case 'DM_TYPING': {
      const until = Date.now() + 8000
      set({ typing: { ...state.typing, [d.dm_id]: { ...(state.typing[d.dm_id] ?? {}), [d.user_id]: until } } })
      setTimeout(() => set({}), 8100)
      return
    }
    case 'DM_READ':
      set({ reads: { ...state.reads, [d.dm_id]: { ...(state.reads[d.dm_id] ?? {}), [d.user_id]: d.event_id } } })
      return
    case 'DEVICES_UPDATE':
      e2e?.forgetKeys(d.user_id)
      if (d.user_id === account?.user.id) refreshDevices().catch(() => {})
      return
  }
}

// Own devices not yet validated (only a validated device can validate them).
async function refreshDevices() {
  if (!e2e?.validated) return set({ pendingDevices: 0 })
  const list = await e2e.devices()
  set({ pendingDevices: list.filter((d) => !d.trusted).length })
}

async function refreshBackup() {
  if (!e2e) return
  const b = await e2e.backupStatus()
  set({ backup: b.status, backupAt: b.updatedAt })
}

// --- actions ---

export async function setPresence(status: Presence) {
  await api().setPresence(status)
  set({ presence_setting: status })
}

export async function refreshBlocks() {
  if (!account) return
  set({ blocked: (await api().blocks()).map((u) => u.id) })
}

export async function blockUser(u: PublicUser) {
  await api().blockUser(u.id)
  await Promise.all([refreshFriends(), refreshBlocks()])
}

// Whether a community server member is someone I blocked (same identity service).
export function isBlockedMember(issuer: string, subject: string) {
  return !!account && issuer === account.issuer && state.blocked.includes(subject)
}

export function listDevices() {
  return e2e!.devices()
}

export async function approveDevice(deviceId: string, code: string) {
  const n = await e2e!.approve(deviceId, code)
  await refreshDevices()
  return n
}

export async function createRecovery(replace = false) {
  const phrase = await e2e!.createRecovery(replace)
  await refreshBackup()
  return phrase
}

export async function restoreFromPhrase(phrase: string) {
  const n = await e2e!.restore(phrase)
  await refreshBackup()
  return n
}

export async function addFriend(pseudo: string) {
  const r = await api().addFriend(pseudo.trim().replace(/^@/, '').split('@')[0])
  await refreshFriends()
  return r
}

export async function acceptFriend(u: PublicUser) {
  await api().acceptFriend(u.id)
  await refreshFriends()
}

export async function removeFriend(u: PublicUser) {
  await api().removeFriend(u.id)
  await refreshFriends()
}

export async function openDirect(userId: string): Promise<Conversation> {
  const c = await api().openDirect(userId)
  if (!state.conversations.some((x) => x.id === c.id)) set({ conversations: [c, ...state.conversations] })
  return c
}

export async function createGroup(userIds: string[], name: string): Promise<Conversation> {
  const c = await api().createGroup(userIds, name)
  set({ conversations: [c, ...state.conversations.filter((x) => x.id !== c.id)] })
  return c
}

export async function leaveGroup(c: Conversation) {
  await api().leaveGroup(c.id)
  set({ conversations: state.conversations.filter((x) => x.id !== c.id) })
}

const othersOf = (c: Conversation) => c.members.map((m) => m.id).filter((id) => id !== account?.user.id)

export async function sendText(c: Conversation, text: string) {
  return e2e!.send(c.id, othersOf(c), { text })
}

export async function editText(c: Conversation, target: number, text: string) {
  return e2e!.send(c.id, othersOf(c), { type: 'edit', target, text })
}

export async function deleteMessage(c: Conversation, target: number) {
  const file = e2e!.history(c.id).find((m) => m.event_id === target)?.file
  const res = await e2e!.send(c.id, othersOf(c), { type: 'delete', target, text: '' })
  if (file) await files?.forget(c.id, file)
  return res
}

export function sendFile(c: Conversation, file: File, text: string): Promise<SendResult> {
  return files!.send(c.id, othersOf(c), file, text)
}

export function openFile(c: Conversation, ref: FileRef): Promise<Uint8Array> {
  return files!.open(c.id, ref)
}

let lastTyping = 0
export function typing(c: Conversation) {
  if (Date.now() - lastTyping < 3500) return
  lastTyping = Date.now()
  api().dmTyping(c.id).catch(() => {})
}

const sentRead: Record<string, number> = {}
export function markRead(c: Conversation, eventId: number) {
  if ((sentRead[c.id] ?? 0) >= eventId) return
  sentRead[c.id] = eventId
  api().dmRead(c.id, eventId).catch(() => {})
}

export function typingIn(dmId: string): string[] {
  const now = Date.now()
  return Object.entries(state.typing[dmId] ?? {}).filter(([, until]) => until > now).map(([id]) => id)
}
