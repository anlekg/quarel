// The signed-in account (one at a time for now), kept in the secret store.
import { useSyncExternalStore } from 'react'
import { IdentityClient, type Policy, type User } from '../api/identity'
import { ApiError } from '../api/http'
import { secrets } from '../platform'

export interface Account {
  identity: string // service base URL
  issuer: string
  sessionId: string
  token: string
  user: User
}

let current: Account | null = null
const listeners = new Set<() => void>()

function emit() {
  for (const l of listeners) l()
}

export function useAccount(): Account | null {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => current,
  )
}

export function identityClient(a: Account) {
  return new IdentityClient(a.identity, a.token)
}

export async function signIn(a: Account) {
  current = a
  await secrets.set('account', JSON.stringify(a))
  emit()
}

export async function updateUser(user: User) {
  if (!current) return
  await signIn({ ...current, user })
}

// Forgets the session locally; the server-side session is closed if reachable.
export async function signOut(opts: { remote: boolean } = { remote: true }) {
  const a = current
  current = null
  await secrets.delete('account')
  emit()
  if (a && opts.remote) {
    try {
      await identityClient(a).logout()
    } catch {
      /* already closed or unreachable: nothing more to do */
    }
  }
}

export type Restore = { state: 'none' } | { state: 'ok' } | { state: 'expired'; account: Account }

// Restores the saved session. An unreachable service keeps it (offline start);
// a rejected token drops it.
export async function restoreAccount(): Promise<Restore> {
  const raw = await secrets.get('account')
  if (!raw) return { state: 'none' }
  let a: Account
  try {
    a = JSON.parse(raw)
  } catch {
    await secrets.delete('account')
    return { state: 'none' }
  }
  try {
    a = { ...a, user: await identityClient(a).me() }
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      await signOut({ remote: false })
      return { state: 'expired', account: a }
    }
  }
  await signIn(a)
  return { state: 'ok' }
}

// The identity service's rules for community servers: its block list, and
// whether tokens must name the server they are for. Cached 10 minutes.
export interface ServerRules {
  policy: Policy
  blocked: Map<string, string> // server ID → reason
}

const rulesCache = new Map<string, { at: number; rules: ServerRules }>()

export async function serverRules(a: Account, fresh = false): Promise<ServerRules> {
  const hit = rulesCache.get(a.identity)
  if (!fresh && hit && Date.now() - hit.at < 10 * 60_000) return hit.rules
  const c = identityClient(a)
  const [policy, list] = await Promise.all([c.policy(), c.blockedServers()])
  const rules = { policy, blocked: new Map(list.servers.map((s) => [s.id, s.reason ?? ''])) }
  rulesCache.set(a.identity, { at: Date.now(), rules })
  return rules
}
