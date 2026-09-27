// End-to-end encryption of private messages, as the Go test client
// (cmd/quarelctl/e2e.go), with vodozemac: same protocol, same JSON.
//
// Olm (one session per pair of devices) carries secrets between devices:
// Megolm conversation keys, device approvals, history. Megolm (one outbound
// session per conversation and sending device) encrypts the messages. Keys are
// only shared with devices certified by their account's master key, pinned on
// first contact. The server only relays ciphertexts.
import type { Crypto } from '../crypto'
import type { MegolmOutbound, OlmAccount, OlmSession } from '../crypto/wasm/quarel_crypto.js'
import type { DeviceInfo, IdentityClient, InboxItem, ServerBackup, UserKeys } from '../api/identity'
import { ApiError } from '../api/http'
import { vault } from '../platform'
import {
  b64, contactCode, deviceCert, deviceKeys, masterPublic, masterSign, newMasterSeed, normalizeCode, oneTimeKeyMsg, unb64, verificationCode, verify,
} from './keys'
import { sha256 } from '@noble/hashes/sha2.js'
import { backupKey, generatePhrase, openBackup, parsePhrase, sealBackup, WrongKeyError } from './recovery'

const OTK_TARGET = 20
const MEGOLM_MAX_MESSAGES = 100
const MEGOLM_MAX_AGE = 7 * 24 * 3600_000

export interface FileRef {
  id: string
  server_id?: string
  name: string
  mime: string
  size: number
  key: string
  nonce: string
}

export interface HistMsg {
  event_id: number
  from: string // user id
  text: string
  at: string // RFC 3339
  delivered?: boolean
  edited?: boolean
  file?: FileRef
  added?: string[] // "members" event: users `from` added to the group
  timer?: number // "timer" event: the new lifetime of messages (seconds, 0 = off)
  expires?: string // ephemeral message: deleted from this device then (RFC 3339)
}

// Ephemeral messages (as cmd/quarelctl/e2e.go): a "timer" event (any member)
// sets how long the conversation's next messages last; each text or file
// message carries its sender's setting (ttl) and every device deletes it that
// long after it was sent (a sending date in the future counts as now). Timer
// events stay in the history: the last one is the conversation's setting.
export const TIMER_CHOICES = [0, 300, 3600, 86400, 604800]
const MAX_TTL = 604800

export function expiry(sentAt: string, ttl: number, now = Date.now()) {
  return new Date(Math.min(Date.parse(sentAt) || now, now) + Math.min(ttl, MAX_TTL) * 1000).toISOString()
}

interface OutboundState {
  pickle: string
  shared_with: Record<string, boolean>
  created: string
  sent: number
}

interface InboundState {
  pickle: string
  sender_user: string
  sender_device: string
  dm_id: string
}

// Persisted state; field names as the Go client's (history transfers use them).
export interface E2EState {
  user_id: string
  device_id: string
  pickle_key: string
  account?: string
  master_seed?: string
  olm_sessions: Record<string, string[]> // peer curve25519 → pickles, newest first
  outbound: Record<string, OutboundState> // by conversation
  inbound: Record<string, InboundState> // by Megolm session id
  seen: Record<string, boolean> // "session:index", replay protection
  pinned: Record<string, string> // user id → master key
  verified?: Record<string, string> // user id → master key whose safety code was compared
  members?: Record<string, ConvMembers> // by conversation: members confirmed by the members themselves
  history: Record<string, HistMsg[]> // by conversation
  undecrypted: InboxItem[]
  names: Record<string, string>
  servers?: Record<string, SyncedServer> // community servers joined (or left) by any of my devices
  backup_key?: string // derived from the recovery phrase (unpadded base64)
  backup_version?: number
  backup_digest?: string
  backup_at?: string
}

// Members of a conversation this device encrypts for (see observe()).
export interface ConvMembers {
  kind: 'direct' | 'group'
  confirmed: string[]
}

// A conversation as the Identity service lists it.
export interface ConvInfo {
  id: string
  kind: 'direct' | 'group'
  members: { id: string }[]
  user?: { id: string }
}

// A community server in the list shared by my devices. The latest change of
// each server wins (joined or left, on whichever device).
export interface SyncedServer {
  sid: string
  base: string
  host: string
  name: string
  joined: boolean
  at: string // RFC 3339
}

export function mergeServers(into: Record<string, SyncedServer>, from: Record<string, SyncedServer> | undefined): boolean {
  let changed = false
  for (const [sid, s] of Object.entries(from ?? {})) {
    if (!s || typeof s.sid !== 'string' || s.sid !== sid || typeof s.base !== 'string' || !/^https:\/\//.test(s.base)) continue
    const cur = into[sid]
    if (!cur || Date.parse(s.at) > Date.parse(cur.at)) {
      into[sid] = { sid, base: s.base, host: String(s.host ?? ''), name: String(s.name ?? ''), joined: !!s.joined, at: s.at }
      changed = true
    }
  }
  return changed
}

// Encrypted account backup content, as the Go client's backupPayload.
interface BackupPayload {
  format: number
  master_seed: string
  history: Record<string, HistMsg[]>
  inbound: Record<string, InboundState> // pickle = exported session key
  pinned: Record<string, string>
  names: Record<string, string>
  servers?: Record<string, SyncedServer>
  created_at: string
}

export interface OwnDevice extends DeviceInfo {
  current: boolean
  trusted: boolean // certified by the account's master key
}

export type BackupStatus = 'none' | 'active' | 'no_key' // no_key: a backup exists, this device lacks its key

const padded = (b: Uint8Array) => {
  const s = b64(b)
  return s + '='.repeat((4 - (s.length % 4)) % 4)
}

interface OlmPlain {
  type: string
  sender_user: string
  sender_device: string
  sender_ed25519: string
  recipient_device: string
  recipient_ed25519: string
  content: unknown
}

export interface MegolmPlain {
  dm_id: string
  type?: '' | 'file' | 'edit' | 'delete' | 'members' | 'timer'
  target?: number
  ttl?: number // "timer": the new setting; text and file: the sender's setting (seconds)
  members?: string[] // "members": users the sender added to the group
  text: string
  file?: FileRef
  sender_user: string
  sender_device: string
  sent_at: string
}

// Olm "file" message (cmd/quarelctl/filexfer.go): offer | fetch | answer.
export interface FileSignal {
  action: 'offer' | 'fetch' | 'answer'
  transfer_id?: string
  conv_id: string
  file_id: string
  size?: number
  sdp?: string
  from_user: string // set on reception
  from_device: string
  at: number // reception (ms)
}

// Olm "call" message (cmd/quarelctl/calls.go): complete SDP, no trickle ICE.
// Group calls (app 0.4.0+, src/state/calls.ts) carry conv_id: join (start: it
// rings), offer/answer between two participants, leave, full.
export interface CallSignal {
  call_id: string
  action: 'invite' | 'answer' | 'reject' | 'hangup' | 'join' | 'offer' | 'leave' | 'full'
  conv_id?: string
  start?: boolean
  sdp?: string
  from_user: string // set on reception
  from_device: string
  at: number // reception (ms)
}

interface RoomKey {
  dm_id: string
  session_id: string
  session_key: string
}

export class E2EError extends Error {
  constructor(public code: string, message = code) {
    super(message)
  }
}

// What the UI hears about.
export type E2EEvent =
  | { kind: 'history'; dmId: string }
  | { kind: 'approved' } // this device was validated (by another one, or restored)
  | { kind: 'backup' } // backup state changed
  | { kind: 'servers' } // the shared list of community servers changed
  | { kind: 'file_signal'; signal: FileSignal } // peer-to-peer file transfer signalling, from a trusted device
  | { kind: 'file_received'; dmId: string; ref: FileRef } // a file event was decrypted
  | { kind: 'file_gone'; dmId: string; ref: FileRef } // a message with a file was deleted
  | { kind: 'call_signal'; signal: CallSignal } // call signalling, from a trusted device
  | { kind: 'warning'; text: string }

export class E2E {
  private queue: Promise<unknown> = Promise.resolve()
  private keysCache = new Map<string, { at: number; keys: UserKeys }>()
  private listeners = new Set<(e: E2EEvent) => void>()

  private constructor(
    private c: Crypto,
    private api: IdentityClient,
    private st: E2EState,
    private acc: OlmAccount,
    private storeKey: string,
  ) {}

  // --- lifecycle ---

  // Loads this account's state (or creates it) for the current device
  // (= Identity session) and publishes its keys when new.
  static async open(c: Crypto, api: IdentityClient, userId: string, pseudo: string, sessionId: string): Promise<E2E> {
    const storeKey = 'e2e-' + userId
    let st: E2EState | null = null
    try {
      const raw = await vault.get(storeKey)
      st = raw ? (JSON.parse(raw) as E2EState) : null
    } catch {
      st = null
    }
    st ??= {
      user_id: userId, device_id: '', pickle_key: b64(crypto.getRandomValues(new Uint8Array(32))), olm_sessions: {}, outbound: {},
      inbound: {}, seen: {}, pinned: {}, history: {}, undecrypted: [], names: {},
    }
    st.names[userId] = pseudo
    // A new login is a new device: new Olm account and sessions; history,
    // received keys, pinned contacts and the master key (if held) stay.
    let fresh = false
    if (st.device_id !== sessionId || !st.account) {
      st.device_id = sessionId
      st.account = undefined
      st.olm_sessions = {}
      st.outbound = {}
      fresh = true
    }
    const acc = fresh ? new c.OlmAccount() : c.OlmAccount.fromPickle(st.account!, unb64(st.pickle_key))
    const e = new E2E(c, api, st, acc, storeKey)
    if (fresh) await e.publish()
    else await e.replenish()
    await e.save()
    return e
  }

  on(l: (e: E2EEvent) => void) {
    this.listeners.add(l)
    return () => this.listeners.delete(l)
  }

  private emit(e: E2EEvent) {
    for (const l of this.listeners) l(e)
  }

  // Serializes operations on the state (sync, send, approve…).
  private run<T>(fn: () => Promise<T>): Promise<T> {
    const p = this.queue.then(fn, fn)
    this.queue = p.catch(() => {})
    return p
  }

  private get pk() {
    return unb64(this.st.pickle_key)
  }

  private async save() {
    this.st.account = this.acc.pickle(this.pk)
    await vault.set(this.storeKey, JSON.stringify(this.st))
  }

  // --- public state ---

  get userId() {
    return this.st.user_id
  }

  get deviceId() {
    return this.st.device_id
  }

  get validated() {
    return !!this.st.master_seed
  }

  get ed25519() {
    return this.acc.ed25519
  }

  history(dmId: string): HistMsg[] {
    return this.st.history[dmId] ?? []
  }

  // The conversation's current lifetime of messages (seconds, 0: off).
  timerOf(dmId: string) {
    const list = this.st.history[dmId] ?? []
    for (let i = list.length - 1; i >= 0; i--) if (list[i].timer !== undefined) return list[i].timer!
    return 0
  }

  // Deletes the ephemeral messages whose time is up (their files too).
  purgeExpired() {
    return this.run(async () => {
      const now = Date.now()
      let changed = false
      for (const [dm, list] of Object.entries(this.st.history)) {
        const gone = list.filter((h) => h.expires && Date.parse(h.expires) <= now)
        if (!gone.length) continue
        this.st.history[dm] = list.filter((h) => !gone.includes(h))
        for (const h of gone) if (h.file) this.emit({ kind: 'file_gone', dmId: dm, ref: h.file })
        this.emit({ kind: 'history', dmId: dm })
        changed = true
      }
      if (changed) {
        await this.save()
        await this.backupQuietly()
      }
    })
  }

  name(userId: string) {
    return this.st.names[userId] || userId
  }

  // --- keys ---

  private sign(message: Uint8Array) {
    return this.acc.sign(message)
  }

  // Uploads this device's identity keys. The account's first device creates the
  // master key and certifies itself; a device holding the master key does too.
  private async publish() {
    const { user_id: uid, device_id: did } = this.st
    const edKey = this.acc.ed25519
    const curve = this.acc.curve25519
    const body: Record<string, string> = { curve25519: curve, ed25519: edKey, signature: this.sign(deviceKeys(uid, did, curve, edKey)) }
    const keys = await this.keysOf(uid, true)
    const cert = deviceCert(uid, did, edKey)
    if (!keys.master_key) {
      const seed = newMasterSeed()
      this.st.master_seed = seed
      body.master_key = masterPublic(seed)
      body.master_signature = masterSign(seed, cert)
      this.st.pinned[uid] = body.master_key
    } else {
      this.pin(uid, keys.master_key)
      if (this.st.master_seed) {
        if (masterPublic(this.st.master_seed) === keys.master_key) body.master_signature = masterSign(this.st.master_seed, cert)
        else this.st.master_seed = undefined
      }
    }
    await this.api.publishDevice(body)
    await this.save()
    await this.replenish()
  }

  // Keeps enough signed one-time keys on the server for others to open Olm sessions.
  private async replenish() {
    const { one_time_keys: n } = await this.api.uploadOneTimeKeys([])
    if (n >= OTK_TARGET / 2) return
    this.acc.generateOneTimeKeys(OTK_TARGET - n)
    const otks = JSON.parse(this.acc.oneTimeKeys()) as Record<string, string>
    const keys = Object.entries(otks).map(([id, key]) => ({ id, key, signature: this.sign(oneTimeKeyMsg(this.st.device_id, id, key)) }))
    await this.api.uploadOneTimeKeys(keys)
    this.acc.markKeysAsPublished()
    await this.save()
  }

  async keysOf(userId: string, fresh = false): Promise<UserKeys> {
    const hit = this.keysCache.get(userId)
    if (!fresh && hit && Date.now() - hit.at < 30_000) return hit.keys
    const keys = await this.api.userKeys(userId)
    if (keys.user?.pseudo) this.st.names[userId] = keys.user.pseudo
    this.keysCache.set(userId, { at: Date.now(), keys })
    return keys
  }

  // Called on DEVICES_UPDATE: that user's devices changed.
  forgetKeys(userId: string) {
    this.keysCache.delete(userId)
  }

  // Remembers a user's master key the first time; refuses a different one later.
  private pin(userId: string, master: string) {
    const pinned = this.st.pinned[userId]
    if (!pinned) this.st.pinned[userId] = master
    else if (pinned !== master) throw new E2EError('master_key_changed', this.name(userId))
  }

  deviceTrusted(userId: string, d: DeviceInfo, master: string) {
    return !!d.master_signature &&
      verify(d.ed25519, deviceKeys(userId, d.device_id, d.curve25519, d.ed25519), d.signature) &&
      verify(master, deviceCert(userId, d.device_id, d.ed25519), d.master_signature)
  }

  async trusted(userId: string): Promise<DeviceInfo[]> {
    const k = await this.keysOf(userId)
    if (!k.master_key) return []
    this.pin(userId, k.master_key)
    return k.devices.filter((d) => this.deviceTrusted(userId, d, k.master_key!))
  }

  // --- group members confirmed by the members themselves ---
  // The Identity service keeps the member lists: a compromised service could
  // slip someone into a conversation and receive the next messages. So this
  // device only encrypts for members seen at its first sight of the
  // conversation, or announced by a confirmed member in an encrypted
  // "members" event after adding them (as cmd/quarelctl/e2e.go).

  // Records the members as the server lists them: the first time, all are
  // confirmed; afterwards, members gone are dropped, newcomers stay unconfirmed.
  observe(c: ConvInfo) {
    const all = (this.st.members ??= {})
    const listed = new Set(c.members.map((m) => m.id))
    const cm = all[c.id]
    if (!cm) {
      const other = c.user?.id ?? c.members.find((m) => m.id !== this.st.user_id)?.id
      all[c.id] = { kind: c.kind, confirmed: c.kind === 'direct' ? [this.st.user_id, ...(other ? [other] : [])] : [...listed] }
      return
    }
    cm.confirmed = cm.confirmed.filter((id) => listed.has(id) || id === this.st.user_id)
  }

  // observe() for conversations as they arrive, saved at once: the first
  // sight is what later additions are compared with.
  observeAll(convs: ConvInfo[]) {
    return this.run(async () => {
      for (const c of convs) this.observe(c)
      await this.save()
    })
  }

  isConfirmed(dmId: string, userId: string) {
    return !!this.st.members?.[dmId]?.confirmed.includes(userId)
  }

  private confirmMembers(dmId: string, sender: string, users: string[]) {
    const cm = this.st.members?.[dmId]
    if (!cm || cm.kind === 'direct' || !this.isConfirmed(dmId, sender)) return false
    for (const u of users) if (!cm.confirmed.includes(u)) cm.confirmed.push(u)
    return true
  }

  // The other members to encrypt for, and the unconfirmed ones.
  recipients(c: ConvInfo): { confirmed: string[]; unconfirmed: string[] } {
    this.observe(c)
    const others = c.members.map((m) => m.id).filter((id) => id !== this.st.user_id)
    return { confirmed: others.filter((id) => this.isConfirmed(c.id, id)), unconfirmed: others.filter((id) => !this.isConfirmed(c.id, id)) }
  }

  // After adding someone to a group: confirmed here, and announced (encrypted)
  // to the members so their apps encrypt for them too.
  announceMember(c: ConvInfo, userId: string) {
    this.observe(c)
    const cm = this.st.members![c.id]
    if (!cm.confirmed.includes(userId)) cm.confirmed.push(userId)
    return this.send(c.id, this.recipients(c).confirmed, { type: 'members', members: [userId], text: '' })
  }

  // --- safety codes between contacts ---

  // The code to compare with userId (e2ekeys.ContactCode), from the master
  // keys pinned here; changed: the server now shows another key for them.
  safetyCode(userId: string) {
    return this.run(async () => {
      if (!this.st.master_seed) throw new E2EError('device_not_validated')
      const k = await this.keysOf(userId, true)
      if (!k.master_key) throw new E2EError('no_keys_yet')
      const pinned = (this.st.pinned[userId] ??= k.master_key)
      const mine = masterPublic(this.st.master_seed)
      await this.save()
      return {
        code: contactCode(this.st.user_id, mine, userId, pinned),
        newCode: pinned !== k.master_key ? contactCode(this.st.user_id, mine, userId, k.master_key) : undefined,
        verified: this.st.verified?.[userId] === pinned,
      }
    })
  }

  // The codes matched: this contact is verified (for their current pinned key).
  markVerified(userId: string) {
    return this.run(async () => {
      (this.st.verified ??= {})[userId] = this.st.pinned[userId]
      await this.save()
    })
  }

  isVerified(userId: string) {
    return !!this.st.pinned[userId] && this.st.verified?.[userId] === this.st.pinned[userId]
  }

  // A contact reset their keys (and the new code was compared): the new
  // master key replaces the pinned one.
  trustNewKey(userId: string) {
    return this.run(async () => {
      const k = await this.keysOf(userId, true)
      if (!k.master_key) throw new E2EError('no_keys_yet')
      this.st.pinned[userId] = k.master_key
      delete this.st.verified?.[userId]
      await this.save()
    })
  }

  // After POST /v1/keys/master/reset: this device creates the account's new
  // master key and certifies itself (the old backup is gone).
  restartKeys() {
    return this.run(async () => {
      this.st.master_seed = undefined
      this.st.backup_key = undefined
      this.st.backup_version = undefined
      this.st.backup_digest = undefined
      delete this.st.pinned[this.st.user_id]
      this.keysCache.delete(this.st.user_id)
      await this.publish()
      this.emit({ kind: 'approved' })
      this.emit({ kind: 'backup' })
    })
  }

  // --- Olm (device to device) ---

  private storeOlm(curve: string, s: OlmSession) {
    const p = s.pickle(this.pk)
    const list = this.st.olm_sessions[curve] ?? []
    const i = list.findIndex((old) => {
      try {
        return this.c.OlmSession.fromPickle(old, this.pk).sessionId === s.sessionId
      } catch {
        return false
      }
    })
    if (i >= 0) list[i] = p
    else list.unshift(p)
    this.st.olm_sessions[curve] = list
  }

  private async olmSession(d: DeviceInfo) {
    const list = this.st.olm_sessions[d.curve25519]
    if (list?.length) return this.c.OlmSession.fromPickle(list[0], this.pk)
    const claimed = await this.api.claimKeys([d.device_id])
    const k = claimed[d.device_id]
    if (!k) throw new E2EError('no_one_time_key', d.device_name)
    if (!verify(d.ed25519, oneTimeKeyMsg(d.device_id, k.id, k.key), k.signature)) throw new E2EError('bad_one_time_key', d.device_name)
    const s = this.acc.outboundSession(d.curve25519, k.key)
    this.storeOlm(d.curve25519, s)
    return s
  }

  private async sendSecret(devs: DeviceInfo[], type: string, content: unknown) {
    if (!devs.length) return
    const messages: { device_id: string; payload: string }[] = []
    for (const d of devs) {
      const s = await this.olmSession(d)
      const plain: OlmPlain = {
        type, sender_user: this.st.user_id, sender_device: this.st.device_id, sender_ed25519: this.acc.ed25519,
        recipient_device: d.device_id, recipient_ed25519: d.ed25519, content,
      }
      const m = JSON.parse(s.encrypt(JSON.stringify(plain))) as { type: number; body: string }
      this.storeOlm(d.curve25519, s)
      messages.push({ device_id: d.device_id, payload: JSON.stringify({ algorithm: 'olm.v1', sender_key: this.acc.curve25519, type: m.type, body: m.body }) })
    }
    await this.save()
    await this.api.sendToDevice(messages)
  }

  // Decrypts an Olm message and checks who sent it and for whom.
  private async openSecret(it: InboxItem): Promise<{ plain: OlmPlain; sender: DeviceInfo; master: string }> {
    const env = JSON.parse(it.payload) as { algorithm: string; sender_key: string; type: number; body: string }
    if (env.algorithm !== 'olm.v1') throw new E2EError('unknown_format')
    let keys = await this.keysOf(it.sender_user)
    let sender = keys.devices.find((d) => d.device_id === it.sender_device)
    if (!sender) {
      keys = await this.keysOf(it.sender_user, true) // a device just added
      sender = keys.devices.find((d) => d.device_id === it.sender_device)
    }
    if (!sender || sender.curve25519 !== env.sender_key) throw new E2EError('unknown_sender_device')
    let pt: string | null = null
    for (const p of this.st.olm_sessions[env.sender_key] ?? []) {
      try {
        const s = this.c.OlmSession.fromPickle(p, this.pk)
        pt = s.decrypt(env.type, env.body)
        this.storeOlm(env.sender_key, s)
        break
      } catch {
        /* try the next session */
      }
    }
    if (pt === null) {
      if (env.type !== 0) throw new E2EError('no_olm_session')
      const r = this.acc.inboundSession(env.sender_key, env.body)
      pt = r.plaintext
      this.storeOlm(env.sender_key, r.takeSession()!)
    }
    const plain = JSON.parse(pt) as OlmPlain
    if (plain.sender_user !== it.sender_user || plain.sender_device !== it.sender_device || plain.sender_ed25519 !== sender.ed25519 ||
      plain.recipient_device !== this.st.device_id || plain.recipient_ed25519 !== this.acc.ed25519) {
      throw new E2EError('mismatched_envelope')
    }
    return { plain, sender, master: keys.master_key ?? '' }
  }

  // --- Megolm (conversation messages) ---

  // Encrypts an event for a conversation and sends it. The conversation key
  // goes to every verified device of every member lacking it; it is renewed
  // after 100 messages, 7 days, or when a device leaves.
  send(dmId: string, members: string[], content: Omit<MegolmPlain, 'dm_id' | 'sender_user' | 'sender_device' | 'sent_at'>) {
    return this.run(async () => {
      if (!this.st.master_seed) throw new E2EError('device_not_validated')
      const targets = new Map<string, DeviceInfo>()
      for (const u of new Set([...members, this.st.user_id])) {
        for (const d of await this.trusted(u)) if (d.device_id !== this.st.device_id) targets.set(d.device_id, d)
      }
      let ob = this.st.outbound[dmId]
      let rotate = !ob || ob.sent >= MEGOLM_MAX_MESSAGES || Date.now() - Date.parse(ob.created) > MEGOLM_MAX_AGE
      if (ob) for (const dev of Object.keys(ob.shared_with)) rotate ||= !targets.has(dev)
      let og: MegolmOutbound
      if (rotate) {
        og = new this.c.MegolmOutbound()
        ob = { pickle: '', shared_with: {}, created: new Date().toISOString(), sent: 0 }
      } else {
        og = this.c.MegolmOutbound.fromPickle(ob.pickle, this.pk)
      }
      const missing = [...targets.values()].filter((d) => !ob.shared_with[d.device_id])
      const key: RoomKey = { dm_id: dmId, session_id: og.sessionId, session_key: og.sessionKey }
      await this.sendSecret(missing, 'room_key', key)
      for (const d of missing) ob.shared_with[d.device_id] = true
      if (!content.type || content.type === 'file') {
        const ttl = this.timerOf(dmId)
        content = { ...content, ...(ttl ? { ttl } : {}) }
      }
      const plain: MegolmPlain = { ...content, dm_id: dmId, sender_user: this.st.user_id, sender_device: this.st.device_id, sent_at: new Date().toISOString() }
      const ct = og.encrypt(JSON.stringify(plain))
      ob.sent++
      ob.pickle = og.pickle(this.pk)
      this.st.outbound[dmId] = ob
      await this.save() // never reuse a message index, even if sending fails
      const env = { algorithm: 'megolm.v1', sender_key: this.acc.curve25519, session_id: og.sessionId, body: ct }
      const res = await this.api.sendDM(dmId, JSON.stringify(env))
      this.apply(res.event_id, plain)
      await this.save()
      this.emit({ kind: 'history', dmId })
      await this.backupQuietly()
      return res
    })
  }

  // Records a decrypted event. Edits and deletions only apply to messages of
  // their own sender. Returns whether the history changed.
  private apply(eventId: number, p: MegolmPlain): boolean {
    const list = this.st.history[p.dm_id] ?? []
    if (p.type === 'members') {
      // Not seen yet: its first sight will take the members as they are.
      if (this.st.members?.[p.dm_id] && !this.confirmMembers(p.dm_id, p.sender_user, p.members ?? [])) {
        this.emit({ kind: 'warning', text: this.name(p.sender_user) + ', qui n’est pas membre confirmé de cette conversation, a annoncé des membres : ignoré.' })
        return false
      }
      if (list.some((h) => h.event_id === eventId)) return false
      list.push({ event_id: eventId, from: p.sender_user, text: '', at: p.sent_at, added: p.members ?? [] })
      list.sort((a, b) => Date.parse(a.at) - Date.parse(b.at))
      this.st.history[p.dm_id] = list
      return true
    }
    if (p.type === 'timer') {
      if (!TIMER_CHOICES.includes(p.ttl ?? 0)) {
        this.emit({ kind: 'warning', text: this.name(p.sender_user) + ' a choisi une durée de messages éphémères inconnue : ignoré.' })
        return false
      }
      if (list.some((h) => h.event_id === eventId)) return false
      list.push({ event_id: eventId, from: p.sender_user, text: '', at: p.sent_at, timer: p.ttl ?? 0 })
      list.sort((a, b) => Date.parse(a.at) - Date.parse(b.at))
      this.st.history[p.dm_id] = list
      return true
    }
    if (p.type === 'edit' || p.type === 'delete') {
      const i = list.findIndex((h) => h.event_id === p.target)
      if (i < 0) return false
      if (list[i].from !== p.sender_user) {
        this.emit({ kind: 'warning', text: this.name(p.sender_user) + ' a tenté de modifier le message d’une autre personne : ignoré.' })
        return false
      }
      if (p.type === 'delete') {
        const [gone] = list.splice(i, 1)
        if (gone.file) this.emit({ kind: 'file_gone', dmId: p.dm_id, ref: gone.file })
      }
      else list[i] = { ...list[i], text: p.text, edited: true }
      this.st.history[p.dm_id] = list
      return true
    }
    if (list.some((h) => h.event_id === eventId)) return false
    const expires = p.ttl && p.ttl > 0 ? expiry(p.sent_at, p.ttl) : undefined
    if (expires && Date.parse(expires) <= Date.now()) return false // already gone
    list.push({ event_id: eventId, from: p.sender_user, text: p.text, at: p.sent_at, ...(p.file ? { file: p.file } : {}), ...(expires ? { expires } : {}) })
    if (p.file && p.sender_device !== this.st.device_id) this.emit({ kind: 'file_received', dmId: p.dm_id, ref: p.file })
    list.sort((a, b) => Date.parse(a.at) - Date.parse(b.at))
    this.st.history[p.dm_id] = list
    return true
  }

  private openDM(it: InboxItem): MegolmPlain | 'no_key' {
    const env = JSON.parse(it.payload) as { algorithm: string; sender_key: string; session_id: string; body: string }
    if (env.algorithm !== 'megolm.v1' || !it.dm_id) throw new E2EError('unknown_format')
    const is = this.st.inbound[env.session_id]
    if (!is) return 'no_key'
    if (is.sender_user !== it.sender_user || is.sender_device !== it.sender_device || is.dm_id !== it.dm_id) {
      throw new E2EError('key_misuse')
    }
    const ig = this.c.MegolmInbound.fromPickle(is.pickle, this.pk)
    const d = ig.decrypt(env.body)
    const seen = env.session_id + ':' + d.index
    if (this.st.seen[seen]) throw new E2EError('replayed')
    const plain = JSON.parse(d.plaintext) as MegolmPlain
    if (plain.dm_id !== it.dm_id || plain.sender_user !== it.sender_user || plain.sender_device !== it.sender_device) {
      throw new E2EError('mismatched_envelope')
    }
    this.st.seen[seen] = true
    return plain
  }

  // --- receiving ---

  // Fetches, decrypts and acknowledges everything waiting for this device.
  // items: already received through the gateway (INBOX), or undefined to fetch.
  sync(items?: InboxItem[]) {
    return this.run(() => this.doSync(items))
  }

  private async doSync(items?: InboxItem[]) {
    const changed = new Set<string>()
    let batch = items ?? (await this.api.inbox())
    while (batch.length) {
      for (const it of batch) await this.process(it, changed)
      await this.save() // keep what was decrypted before acknowledging
      await this.api.ackInbox(batch.map((i) => i.id))
      batch = items ? [] : await this.api.inbox()
      items = undefined
    }
    this.retryUndecrypted(changed)
    await this.replenish()
    await this.save()
    for (const dmId of changed) this.emit({ kind: 'history', dmId })
    await this.backupQuietly()
  }

  private async process(it: InboxItem, changed: Set<string>) {
    try {
      if (it.kind === 'to_device') {
        const { plain, sender, master } = await this.openSecret(it)
        this.handleSecret(plain, sender, master, it)
      } else if (it.kind === 'dm') {
        const p = this.openDM(it)
        if (p === 'no_key') {
          this.st.undecrypted.push(it)
          return
        }
        if (this.apply(it.event_id!, p)) changed.add(p.dm_id)
      } else if (it.kind === 'receipt') {
        const r = JSON.parse(it.payload) as { dm_id: string; event_id: number }
        const h = this.st.history[r.dm_id]?.find((m) => m.event_id === r.event_id)
        if (h && !h.delivered) {
          h.delivered = true
          changed.add(r.dm_id)
        }
      }
    } catch (e) {
      const what = e instanceof E2EError ? e.code : e instanceof ApiError ? e.code : String(e)
      this.emit({ kind: 'warning', text: 'Message de ' + this.name(it.sender_user) + ' illisible (' + what + ').' })
    }
  }

  private handleSecret(plain: OlmPlain, sender: DeviceInfo, master: string, it: InboxItem) {
    let trusted = false
    try {
      trusted = !!master && (this.pin(plain.sender_user, master), true) && this.deviceTrusted(plain.sender_user, sender, master)
    } catch {
      trusted = false
    }
    switch (plain.type) {
      case 'room_key': {
        const k = plain.content as RoomKey
        if (!trusted) {
          this.emit({ kind: 'warning', text: 'Clé de conversation refusée : l’appareil « ' + sender.device_name + ' » de ' + this.name(plain.sender_user) + ' n’est pas validé.' })
          return
        }
        const ig = new this.c.MegolmInbound(k.session_key)
        if (ig.sessionId !== k.session_id) return
        this.st.inbound[k.session_id] = { pickle: ig.pickle(this.pk), sender_user: plain.sender_user, sender_device: plain.sender_device, dm_id: k.dm_id }
        return
      }
      case 'device_approval': {
        const a = plain.content as { master_seed: string; backup_key?: string }
        if (plain.sender_user !== this.st.user_id || !trusted || masterPublic(a.master_seed) !== master) {
          this.emit({ kind: 'warning', text: 'Validation refusée : elle ne vient pas d’un de vos appareils validés.' })
          return
        }
        this.st.master_seed = a.master_seed
        if (a.backup_key && !this.st.backup_key) {
          this.st.backup_key = a.backup_key // version learnt on the first upload (conflict → merge)
          this.st.backup_version = 0
        }
        this.emit({ kind: 'approved' })
        return
      }
      case 'history': {
        const h = plain.content as { history: Record<string, HistMsg[]>; inbound: Record<string, InboundState>; keys: Record<string, string> }
        if (plain.sender_user !== this.st.user_id || !trusted) {
          this.emit({ kind: 'warning', text: 'Historique refusé : il ne vient pas d’un de vos appareils validés.' })
          return
        }
        const inbound: Record<string, InboundState> = {}
        for (const [sid, info] of Object.entries(h.inbound ?? {})) if (h.keys?.[sid]) inbound[sid] = { ...info, pickle: h.keys[sid] }
        this.mergeContent(h.history ?? {}, inbound)
        this.st.servers ??= {}
        if (mergeServers(this.st.servers, (plain.content as { servers?: Record<string, SyncedServer> }).servers)) this.emit({ kind: 'servers' })
        return
      }
      case 'file': {
        if (!trusted) {
          this.emit({ kind: 'warning', text: 'Transfert de fichier refusé : l’appareil « ' + sender.device_name + ' » de ' + this.name(plain.sender_user) + ' n’est pas validé.' })
          return
        }
        const at = Date.parse(it.created_at) || Date.now()
        if (Date.now() - at > 2 * 60_000) return // stale: the other side gave up
        const sig = plain.content as FileSignal
        this.emit({ kind: 'file_signal', signal: { ...sig, from_user: plain.sender_user, from_device: plain.sender_device, at } })
        return
      }
      case 'servers': {
        if (plain.sender_user !== this.st.user_id || !trusted) return // only from my own validated devices
        this.st.servers ??= {}
        if (mergeServers(this.st.servers, (plain.content as { servers?: Record<string, SyncedServer> }).servers)) this.emit({ kind: 'servers' })
        return
      }
      case 'call': {
        if (!trusted) {
          this.emit({ kind: 'warning', text: 'Signal d’appel refusé : l’appareil « ' + sender.device_name + ' » de ' + this.name(plain.sender_user) + ' n’est pas validé.' })
          return
        }
        const at = Date.parse(it.created_at) || Date.now()
        if (Date.now() - at > 2 * 60_000) return // stale
        const sig = plain.content as CallSignal
        this.emit({ kind: 'call_signal', signal: { ...sig, from_user: plain.sender_user, from_device: plain.sender_device, at } })
        return
      }
    }
  }

  private retryUndecrypted(changed: Set<string>) {
    const still: InboxItem[] = []
    for (const it of this.st.undecrypted) {
      try {
        const p = this.openDM(it)
        if (p === 'no_key') still.push(it)
        else if (this.apply(it.event_id!, p)) changed.add(p.dm_id)
      } catch {
        /* rejected */
      }
    }
    this.st.undecrypted = still
  }

  // Imports history and exported conversation keys (history transfer, backup).
  // Returns the number of new messages.
  private mergeContent(history: Record<string, HistMsg[]>, inbound: Record<string, InboundState>): number {
    let n = 0
    for (const [dm, msgs] of Object.entries(history)) {
      const list = this.st.history[dm] ?? []
      let added = false
      for (const m of msgs ?? []) {
        if (list.some((x) => x.event_id === m.event_id)) continue
        list.push(m)
        added = true
        n++
      }
      if (!added) continue
      list.sort((a, b) => Date.parse(a.at) - Date.parse(b.at))
      this.st.history[dm] = list
      this.emit({ kind: 'history', dmId: dm })
    }
    for (const [sid, info] of Object.entries(inbound)) {
      if (this.st.inbound[sid]) continue
      try {
        const ig = this.c.MegolmInbound.import(info.pickle)
        if (ig.sessionId !== sid) continue
        this.st.inbound[sid] = { pickle: ig.pickle(this.pk), sender_user: info.sender_user, sender_device: info.sender_device, dm_id: info.dm_id }
      } catch {
        /* skip a bad key */
      }
    }
    return n
  }

  // Conversation keys exported from their first known index.
  private exportedInbound(): Record<string, InboundState> {
    const out: Record<string, InboundState> = {}
    for (const [sid, info] of Object.entries(this.st.inbound)) {
      try {
        const ig = this.c.MegolmInbound.fromPickle(info.pickle, this.pk)
        const exp = ig.exportAt(ig.firstKnownIndex)
        if (exp) out[sid] = { ...info, pickle: exp }
      } catch {
        /* skip */
      }
    }
    return out
  }

  // --- files ---

  // Trusted devices of these users, except this one.
  devicesOf(userIds: string[]) {
    return this.run(async () => {
      const out: DeviceInfo[] = []
      for (const u of new Set(userIds)) for (const d of await this.trusted(u)) if (d.device_id !== this.st.device_id) out.push(d)
      return out
    })
  }

  sendFileSignal(devs: DeviceInfo[], signal: Omit<FileSignal, 'from_user' | 'from_device' | 'at'>) {
    return this.run(() => this.sendSecret(devs, 'file', signal))
  }

  sendCallSignal(devs: DeviceInfo[], signal: Omit<CallSignal, 'from_user' | 'from_device' | 'at'>) {
    return this.run(() => this.sendSecret(devs, 'call', signal))
  }

  // A trusted device of a user, by id (to answer a request).
  async trustedDevice(userId: string, deviceId: string) {
    return (await this.run(() => this.trusted(userId))).find((d) => d.device_id === deviceId)
  }

  // --- community servers shared by my devices ---

  syncedServers(): SyncedServer[] {
    return Object.values(this.st.servers ?? {})
  }

  // Records servers joined or left here and tells my other validated devices.
  recordServers(changes: SyncedServer[]) {
    return this.run(async () => {
      this.st.servers ??= {}
      const fresh: Record<string, SyncedServer> = {}
      for (const c of changes) fresh[c.sid] = c
      if (!mergeServers(this.st.servers, fresh)) return
      await this.save()
      if (!this.st.master_seed) return // not validated: my devices would refuse it; sent with the history later
      const mine = (await this.trusted(this.st.user_id)).filter((d) => d.device_id !== this.st.device_id)
      if (mine.length) await this.sendSecret(mine, 'servers', { servers: this.st.servers }).catch(() => {})
      await this.backupQuietly()
    })
  }

  // Sends the whole list to my other validated devices (once per launch: a
  // device that was offline, or on an older version, catches up).
  shareServerList() {
    return this.run(async () => {
      if (!this.st.master_seed || !Object.keys(this.st.servers ?? {}).length) return
      const mine = (await this.trusted(this.st.user_id)).filter((d) => d.device_id !== this.st.device_id)
      if (mine.length) await this.sendSecret(mine, 'servers', { servers: this.st.servers }).catch(() => {})
    })
  }

  // --- device approval ---

  // This account's devices, with their trust status.
  devices() {
    return this.run(async () => {
      const k = await this.keysOf(this.st.user_id, true)
      return k.devices.map((d): OwnDevice => ({
        ...d, current: d.device_id === this.st.device_id, trusted: !!k.master_key && this.deviceTrusted(this.st.user_id, d, k.master_key),
      }))
    })
  }

  // Certifies another device of the account once the user typed the code it
  // shows, then gives it the master key, the backup key and the history.
  // Returns the number of messages transferred.
  approve(deviceId: string, code: string) {
    return this.run(async () => {
      if (!this.st.master_seed) throw new E2EError('device_not_validated')
      await this.doSync() // catch up first so the transferred history is complete
      const keys = await this.keysOf(this.st.user_id, true)
      const d = keys.devices.find((x) => x.device_id === deviceId)
      if (!d) throw new E2EError('device_not_found')
      if (normalizeCode(code) !== normalizeCode(verificationCode(d.ed25519))) throw new E2EError('code_mismatch')
      if (!verify(d.ed25519, deviceKeys(this.st.user_id, d.device_id, d.curve25519, d.ed25519), d.signature)) throw new E2EError('bad_device_keys')
      const sig = masterSign(this.st.master_seed, deviceCert(this.st.user_id, d.device_id, d.ed25519))
      await this.api.certifyDevice(d.device_id, sig)
      const certified = { ...d, master_signature: sig }
      this.forgetKeys(this.st.user_id)
      await this.sendSecret([certified], 'device_approval', { master_seed: this.st.master_seed, backup_key: this.st.backup_key ?? '' })
      const inbound = this.exportedInbound()
      const h = { history: this.durableHistory(), inbound: {} as Record<string, Omit<InboundState, 'pickle'>>, keys: {} as Record<string, string>, servers: this.st.servers ?? {} }
      for (const [sid, info] of Object.entries(inbound)) {
        h.inbound[sid] = { sender_user: info.sender_user, sender_device: info.sender_device, dm_id: info.dm_id }
        h.keys[sid] = info.pickle
      }
      await this.sendSecret([certified], 'history', h)
      return Object.values(this.st.history).reduce((n, l) => n + l.length, 0)
    })
  }

  // --- recovery phrase and encrypted backup ---

  get backupEnabled() {
    return !!this.st.backup_key
  }

  // This device's copy of the private conversations, readable, for the
  // person's export of their data (ephemeral messages left out).
  exportHistory() {
    const out: Record<string, { from: string; at: string; text: string; file?: string; edited?: boolean }[]> = {}
    for (const [dm, list] of Object.entries(this.durableHistory())) {
      out[dm] = list.filter((h) => h.text || h.file).map((h) => ({
        from: this.name(h.from), at: h.at, text: h.text, ...(h.file ? { file: h.file.name } : {}), ...(h.edited ? { edited: true } : {}),
      }))
    }
    return out
  }

  // The history without its ephemeral messages: they never leave this device
  // (encrypted backup, history sent to a new device), as cmd/quarelctl does.
  private durableHistory(): Record<string, HistMsg[]> {
    const out: Record<string, HistMsg[]> = {}
    for (const [dm, list] of Object.entries(this.st.history)) out[dm] = list.filter((h) => !h.expires)
    return out
  }

  private digest() {
    const data = JSON.stringify([this.durableHistory(), Object.keys(this.st.inbound).sort(), this.st.pinned, !!this.st.master_seed, this.st.servers ?? {}])
    return b64(sha256(new TextEncoder().encode(data)))
  }

  private async fetchBackup(): Promise<ServerBackup | null> {
    try {
      return await this.api.getBackup()
    } catch (e) {
      if (e instanceof ApiError && e.code === 'no_backup') return null
      throw e
    }
  }

  private decryptBackup(b: ServerBackup, key: Uint8Array): BackupPayload {
    return JSON.parse(new TextDecoder().decode(openBackup(key, this.st.user_id, unb64(b.data)))) as BackupPayload
  }

  // Uploads the encrypted state when backups are on and something changed.
  // If another device saved in between, its backup is merged first.
  private async backup(force = false) {
    if (!this.st.backup_key || !this.st.master_seed) return
    if (!force && this.digest() === this.st.backup_digest) return
    const key = unb64(this.st.backup_key)
    for (let attempt = 0; attempt < 3; attempt++) {
      const p: BackupPayload = {
        format: 1, master_seed: this.st.master_seed, history: this.durableHistory(), inbound: this.exportedInbound(),
        pinned: this.st.pinned, names: this.st.names, servers: this.st.servers ?? {}, created_at: new Date().toISOString(),
      }
      const data = padded(sealBackup(key, this.st.user_id, new TextEncoder().encode(JSON.stringify(p))))
      try {
        const res = await this.api.putBackup(this.st.backup_version ?? 0, data)
        this.st.backup_version = res.version
        this.st.backup_digest = this.digest()
        this.st.backup_at = new Date().toISOString()
        await this.save()
        this.emit({ kind: 'backup' })
        return
      } catch (e) {
        if (!(e instanceof ApiError && e.code === 'version_conflict')) throw e
        const b = await this.fetchBackup()
        if (!b) {
          this.st.backup_version = 0
          continue
        }
        let other: BackupPayload
        try {
          other = this.decryptBackup(b, key)
        } catch {
          throw new E2EError('backup_other_phrase')
        }
        this.mergeBackup(other)
        this.st.backup_version = b.version
      }
    }
    throw new E2EError('backup_conflict')
  }

  private async backupQuietly() {
    try {
      await this.backup()
    } catch (e) {
      const code = e instanceof E2EError || e instanceof ApiError ? e.code : String(e)
      this.emit({ kind: 'warning', text: code === 'backup_other_phrase'
        ? 'La sauvegarde du compte a été créée avec une autre phrase de récupération : créez-en une nouvelle dans Paramètres › Appareils.'
        : 'Sauvegarde chiffrée non mise à jour (' + code + ').' })
    }
  }

  private mergeBackup(p: BackupPayload): number {
    const n = this.mergeContent(p.history ?? {}, p.inbound ?? {})
    for (const [u, k] of Object.entries(p.pinned ?? {})) this.st.pinned[u] ||= k
    for (const [u, name] of Object.entries(p.names ?? {})) this.st.names[u] ||= name
    this.st.servers ??= {}
    if (mergeServers(this.st.servers, p.servers)) this.emit({ kind: 'servers' })
    return n
  }

  backupStatus() {
    return this.run(async (): Promise<{ status: BackupStatus; updatedAt?: string }> => {
      const b = await this.fetchBackup()
      if (!b) return { status: 'none' }
      return { status: this.st.backup_key ? 'active' : 'no_key', updatedAt: b.updated_at }
    })
  }

  // Creates a new recovery phrase and uploads the first backup. replace: an
  // existing backup (older phrase, or unknown here) is overwritten.
  createRecovery(replace = false) {
    return this.run(async () => {
      if (!this.st.master_seed) throw new E2EError('device_not_validated')
      const existing = await this.fetchBackup()
      if (existing && !replace) throw new E2EError('backup_exists')
      const { phrase, secret } = generatePhrase()
      const old = { key: this.st.backup_key, version: this.st.backup_version, digest: this.st.backup_digest }
      this.st.backup_key = b64(backupKey(secret, this.st.user_id))
      this.st.backup_version = existing?.version ?? 0
      try {
        await this.backup(true)
      } catch (e) {
        Object.assign(this.st, { backup_key: old.key, backup_version: old.version, backup_digest: old.digest })
        throw e
      }
      return phrase
    })
  }

  // Restores the account key and history with the recovery phrase; this
  // device then certifies itself. Returns the number of messages restored.
  restore(phrase: string) {
    return this.run(async () => {
      const secret = parsePhrase(phrase)
      const b = await this.fetchBackup()
      if (!b) throw new E2EError('no_backup')
      const key = backupKey(secret, this.st.user_id)
      let p: BackupPayload
      try {
        p = this.decryptBackup(b, key)
      } catch (e) {
        if (e instanceof WrongKeyError) throw new E2EError('wrong_phrase')
        throw e
      }
      // The backup must hold the account's real master key.
      const keys = await this.keysOf(this.st.user_id, true)
      if (!p.master_seed || !keys.master_key || masterPublic(p.master_seed) !== keys.master_key) throw new E2EError('backup_master_mismatch')
      this.pin(this.st.user_id, keys.master_key)
      const sig = masterSign(p.master_seed, deviceCert(this.st.user_id, this.st.device_id, this.acc.ed25519))
      await this.api.certifyDevice(this.st.device_id, sig)
      this.st.master_seed = p.master_seed
      const n = this.mergeBackup(p)
      this.st.backup_key = b64(key)
      this.st.backup_version = b.version
      this.st.backup_digest = this.digest()
      this.forgetKeys(this.st.user_id)
      await this.save()
      this.emit({ kind: 'approved' })
      const changed = new Set<string>()
      this.retryUndecrypted(changed)
      await this.save()
      for (const dmId of changed) this.emit({ kind: 'history', dmId })
      return n
    })
  }
}
