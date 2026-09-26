// Login proofs for community servers (idtoken.SignProofV2 in Go): the
// device key signs the server ID, its challenge, the host the app connected
// to and how that connection was checked. A server relaying the login to the
// one it pretends to be cannot use it: that server checks the host (see
// internal/community/auth.go, checkProof).

export type TLSMode = 'binding' | 'authority'

// The host name a proof names (idtoken.NormalizeHost): lowercase, no port,
// no IPv6 brackets, no trailing dot.
export function proofHost(base: string): string {
  return new URL(base).hostname.toLowerCase().replace(/^\[|\]$/g, '').replace(/\.$/, '')
}

export function proofMessage(sid: string, nonce: string, host: string, mode: TLSMode): Uint8Array {
  return new TextEncoder().encode(['quarel-auth-v2', sid, nonce, host, mode].join('\0'))
}
