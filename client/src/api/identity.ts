// Client for a Quarel Identity service (accounts, sessions).
import { fetchBlob, request } from './http'

export interface User {
  id: string
  handle: string
  pseudo: string
  email: string
  email_verified: boolean
  totp_enabled: boolean
  created_at: string
}

export interface SessionInfo {
  id: string
  device_name: string
  created_at: string
  last_seen_at: string
  current: boolean
}

export interface LoginResult {
  session_id: string
  session_token: string
  user: User
}

export interface Policy {
  issuer: string
  registration: 'open' | 'invite' | 'closed'
  email_domains: string[]
  server_policy: 'open' | 'approved'
}

export interface MyInvites {
  quota: number
  created: number
  registration: Policy['registration']
  invites: { code: string; uses: number; max_uses: number; created_at: string; expires_at: string | null }[]
}

export interface PublicUser {
  id: string
  handle: string
  pseudo: string
  presence?: 'online' | 'idle' | 'dnd' | 'offline'
}

export interface FriendLists {
  friends: PublicUser[]
  incoming: PublicUser[]
  outgoing: PublicUser[]
}

export type Relation = 'friends' | 'incoming' | 'outgoing' | 'none'

export interface Conversation {
  id: string
  kind: 'direct' | 'group'
  name: string
  owner_id: string | null
  members: PublicUser[]
  user?: PublicUser // direct: the other participant
  created_at: string
}

export interface DeviceInfo {
  device_id: string
  device_name: string
  curve25519: string
  ed25519: string
  signature: string
  master_signature: string | null
  verified: boolean
  verification_code: string
}

export interface UserKeys {
  user: PublicUser
  master_key: string | null
  devices: DeviceInfo[]
}

export interface InboxItem {
  id: number
  kind: 'to_device' | 'dm' | 'receipt'
  sender_user: string
  sender_device: string
  dm_id?: string
  event_id?: number
  payload: string
  created_at: string
}

export type Presence = 'online' | 'idle' | 'dnd' | 'invisible'

export interface Profile {
  id: string
  handle: string
  pseudo: string
  bio: string
  avatar_url: string | null
}

export interface Privacy {
  typing: boolean
  read_receipts: boolean
  friend_requests: 'everyone' | 'friends_of_friends' | 'nobody'
}

export interface ServerBackup {
  version: number
  data: string // standard base64 of the encrypted backup
  updated_at: string
}

export interface KeySet {
  issuer: string
  keys: unknown[]
}

export class IdentityClient {
  constructor(
    public base: string,
    public token?: string,
  ) {}

  private call<T>(method: string, path: string, body?: unknown) {
    return request<T>(this.base, method, path, { token: this.token, body })
  }

  keySet() {
    return this.call<KeySet>('GET', '/.well-known/quarel-identity')
  }

  policy() {
    return this.call<Policy>('GET', '/v1/policy')
  }

  blockedServers() {
    return this.call<{ servers: { id: string; reason?: string }[] }>('GET', '/v1/servers/blocked')
  }

  register(email: string, pseudo: string, password: string, invite?: string) {
    return this.call<{ user_id: string; handle: string }>('POST', '/v1/auth/register', { email, pseudo, password, invite })
  }

  verifyEmail(email: string, code: string) {
    return this.call<void>('POST', '/v1/auth/verify-email', { email, code })
  }

  resendVerification(email: string) {
    return this.call<void>('POST', '/v1/auth/resend-verification', { email })
  }

  login(req: { login: string; password: string; totp_code?: string; device_name: string; device_key: string }) {
    return this.call<LoginResult>('POST', '/v1/auth/login', req)
  }

  forgotPassword(email: string) {
    return this.call<void>('POST', '/v1/auth/forgot-password', { email })
  }

  resetPassword(req: { email: string; code: string; password: string; totp_code?: string }) {
    return this.call<void>('POST', '/v1/auth/reset-password', req)
  }

  logout() {
    return this.call<void>('POST', '/v1/auth/logout')
  }

  me() {
    return this.call<User>('GET', '/v1/me')
  }

  // audience: the community server the token is for, only sent when the
  // service requires it (approved servers only).
  identityToken(audience?: string) {
    return this.call<{ token: string; expires_at: string }>('POST', '/v1/identity/token', audience ? { audience } : undefined)
  }

  // --- friends ---

  friends() {
    return this.call<FriendLists>('GET', '/v1/friends')
  }

  addFriend(pseudo: string) {
    return this.call<{ user: PublicUser; status: Relation }>('POST', '/v1/friends', { pseudo })
  }

  acceptFriend(id: string) {
    return this.call<{ user: PublicUser; status: Relation }>('POST', '/v1/friends/' + id + '/accept')
  }

  removeFriend(id: string) {
    return this.call<void>('DELETE', '/v1/friends/' + id)
  }

  // --- end-to-end keys ---

  publishDevice(body: Record<string, string>) {
    return this.call<unknown>('POST', '/v1/keys/device', body)
  }

  certifyDevice(deviceId: string, masterSignature: string) {
    return this.call<void>('POST', '/v1/keys/certify', { device_id: deviceId, master_signature: masterSignature })
  }

  uploadOneTimeKeys(keys: { id: string; key: string; signature: string }[]) {
    return this.call<{ one_time_keys: number }>('POST', '/v1/keys/one-time', { keys })
  }

  claimKeys(deviceIds: string[]) {
    return this.call<Record<string, { id: string; key: string; signature: string }>>('POST', '/v1/keys/claim', { device_ids: deviceIds })
  }

  userKeys(userId: string) {
    return this.call<UserKeys>('GET', '/v1/users/' + encodeURIComponent(userId) + '/keys')
  }

  sendToDevice(messages: { device_id: string; payload: string }[]) {
    return this.call<void>('POST', '/v1/to-device', { messages })
  }

  inbox() {
    return this.call<InboxItem[]>('GET', '/v1/inbox')
  }

  ackInbox(ids: number[]) {
    return this.call<void>('POST', '/v1/inbox/ack', { ids })
  }

  // --- conversations ---

  conversations() {
    return this.call<Conversation[]>('GET', '/v1/dms')
  }

  openDirect(userId: string) {
    return this.call<Conversation>('POST', '/v1/dms', { user_id: userId })
  }

  createGroup(userIds: string[], name: string) {
    return this.call<Conversation>('POST', '/v1/dms', { user_ids: userIds, name })
  }

  renameGroup(id: string, name: string) {
    return this.call<Conversation>('PATCH', '/v1/dms/' + id, { name })
  }

  // Every device and the recovery phrase lost: the account's encryption starts over.
  resetMasterKey(password: string, totp_code?: string) {
    return this.call<void>('POST', '/v1/keys/master/reset', { password, totp_code })
  }

  addToGroup(id: string, userId: string) {
    return this.call<Conversation>('PUT', '/v1/dms/' + id + '/members/' + userId)
  }

  removeFromGroup(id: string, userId: string) {
    return this.call<void>('DELETE', '/v1/dms/' + id + '/members/' + userId)
  }

  leaveGroup(id: string) {
    return this.call<void>('DELETE', '/v1/dms/' + id + '/members/@me')
  }

  sendDM(id: string, payload: string) {
    return this.call<{ event_id: number; recipient_devices: number }>('POST', '/v1/dms/' + id + '/messages', { payload })
  }

  dmTyping(id: string) {
    return this.call<void>('POST', '/v1/dms/' + id + '/typing')
  }

  dmRead(id: string, eventId: number) {
    return this.call<void>('POST', '/v1/dms/' + id + '/read', { event_id: eventId })
  }

  gatewayURL() {
    return this.base.replace(/^http/, 'ws') + '/v1/gateway'
  }

  myInvites() {
    return this.call<MyInvites>('GET', '/v1/me/invites')
  }

  createInvite() {
    return this.call<{ code: string }>('POST', '/v1/me/invites')
  }

  sessions() {
    return this.call<SessionInfo[]>('GET', '/v1/me/sessions')
  }

  iceServers() {
    return this.call<{ ice_servers: { urls: string[]; username?: string; credential?: string }[]; relay: boolean }>('GET', '/v1/calls/ice-servers')
  }

  // Encrypted server copy of a conversation file, for the devices not served peer to peer.
  uploadDMFile(convId: string, data: Uint8Array, devices: string[]) {
    return this.call<{ id: string; size: number; pending_devices: number; expires_at: string }>(
      'POST', '/v1/dms/' + convId + '/files?for=' + encodeURIComponent(devices.join(',')), data)
  }

  async downloadDMFile(convId: string, fileId: string): Promise<Uint8Array> {
    const blob = await fetchBlob(this.base + '/v1/dms/' + convId + '/files/' + encodeURIComponent(fileId), this.token ?? '')
    return new Uint8Array(await blob.arrayBuffer())
  }

  ackDMFile(convId: string, fileId: string) {
    return this.call<void>('POST', '/v1/dms/' + convId + '/files/' + encodeURIComponent(fileId) + '/ack')
  }

  deleteDMFile(convId: string, fileId: string) {
    return this.call<void>('DELETE', '/v1/dms/' + convId + '/files/' + encodeURIComponent(fileId))
  }

  // --- account settings ---

  changePseudo(pseudo: string) {
    return this.call<User>('PATCH', '/v1/me', { pseudo })
  }

  changePassword(current_password: string, new_password: string) {
    return this.call<void>('POST', '/v1/me/password', { current_password, new_password })
  }

  changeEmail(password: string, new_email: string) {
    return this.call<void>('POST', '/v1/me/email', { password, new_email })
  }

  confirmEmail(code: string) {
    return this.call<User>('POST', '/v1/me/email/confirm', { code })
  }

  deleteAccount(password: string, totp_code?: string) {
    return this.call<void>('DELETE', '/v1/me', { password, totp_code })
  }

  profile(userId: string) {
    return this.call<Profile>('GET', '/v1/users/' + encodeURIComponent(userId) + '/profile')
  }

  updateBio(bio: string) {
    return this.call<Profile>('PATCH', '/v1/me/profile', { bio })
  }

  setAvatar(image: Blob) {
    return this.call<Profile>('PUT', '/v1/me/avatar', image)
  }

  deleteAvatar() {
    return this.call<void>('DELETE', '/v1/me/avatar')
  }

  setPresence(status: Presence) {
    return this.call<void>('PUT', '/v1/me/presence', { status })
  }

  blocks() {
    return this.call<PublicUser[]>('GET', '/v1/blocks')
  }

  block(pseudo: string) {
    return this.call<PublicUser>('POST', '/v1/blocks', { pseudo })
  }

  blockUser(id: string) {
    return this.call<PublicUser>('PUT', '/v1/blocks/' + encodeURIComponent(id))
  }

  unblock(id: string) {
    return this.call<void>('DELETE', '/v1/blocks/' + encodeURIComponent(id))
  }

  privacy() {
    return this.call<Privacy>('GET', '/v1/me/privacy')
  }

  setPrivacy(p: Partial<Privacy>) {
    return this.call<Privacy>('PATCH', '/v1/me/privacy', p)
  }

  setup2FA(password: string) {
    return this.call<{ secret: string; otpauth_uri: string }>('POST', '/v1/me/2fa/setup', { password })
  }

  enable2FA(code: string) {
    return this.call<{ backup_codes: string[] }>('POST', '/v1/me/2fa/enable', { code })
  }

  disable2FA(password: string, code: string) {
    return this.call<void>('POST', '/v1/me/2fa/disable', { password, code })
  }

  getBackup() {
    return this.call<ServerBackup>('GET', '/v1/backup')
  }

  putBackup(version: number, data: string) {
    return this.call<{ version: number }>('PUT', '/v1/backup', { version, data })
  }

  revokeSession(id: string) {
    return this.call<void>('DELETE', '/v1/me/sessions/' + encodeURIComponent(id))
  }
}
