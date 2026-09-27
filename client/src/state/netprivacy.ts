// Who may learn this device's IP address through a direct (peer-to-peer)
// connection: calls, and the files of private conversations.
//   - 'auto' (default): my friends and my own devices connect directly; the
//     members of a group who are not my friends only through the identity
//     service's relay (calls; files: the server copy), when it has one.
//   - 'always': through the relay with everyone (calls fail without one; files:
//     only the server copy, except between my own devices).
//   - 'never': the relay is refused (direct connections only; the IP address
//     is visible to the other side).
import { prefs } from '../platform'
import { socialState, engine } from './social'

export type RelayMode = 'auto' | 'always' | 'never'

export function relayMode(): RelayMode {
  const m = prefs.get<string>('call-relay-mode', '')
  if (m === 'auto' || m === 'always' || m === 'never') return m
  return prefs.get('call-relay', true) ? 'auto' : 'never' // setting of 0.4.0 and before
}

export function setRelayMode(m: RelayMode) {
  prefs.set('call-relay-mode', m)
}

const isFriend = (userId: string) => socialState().friends.friends.some((f) => f.id === userId)

// Whether this device may connect directly to a device of userId.
export function directOK(userId: string): boolean {
  if (userId === engine()?.userId) return true
  const m = relayMode()
  if (m === 'always') return false
  return m === 'never' || isFriend(userId)
}
