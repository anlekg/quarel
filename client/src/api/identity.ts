// Client for a Quarel Identity service (accounts, sessions).
import { request } from './http'

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

  myInvites() {
    return this.call<MyInvites>('GET', '/v1/me/invites')
  }

  createInvite() {
    return this.call<{ code: string }>('POST', '/v1/me/invites')
  }

  sessions() {
    return this.call<SessionInfo[]>('GET', '/v1/me/sessions')
  }

  revokeSession(id: string) {
    return this.call<void>('DELETE', '/v1/me/sessions/' + encodeURIComponent(id))
  }
}
