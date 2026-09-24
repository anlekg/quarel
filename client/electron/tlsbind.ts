// Verifies self-signed community server certificates, like pkg/tlsbind in Go:
// the certificate carries quarel://binding/<ed25519 key>/<signature over its
// public key>, and the server ID (in invite links) is derived from that key.
import { X509Certificate, createHash, createPublicKey, verify } from 'node:crypto'
import { isIP } from 'node:net'
import { connect } from 'node:tls'

const context = Buffer.from('quarel-tls-binding-v1\x00')

function b64url(s: string) {
  return Buffer.from(s.replace(/-/g, '+').replace(/_/g, '/'), 'base64')
}

export function serverID(pub: Buffer): string {
  const h = createHash('sha256').update(pub).digest().subarray(0, 16)
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const b of h) bits += b.toString(2).padStart(8, '0')
  let out = ''
  for (let i = 0; i < bits.length; i += 5) out += alphabet[parseInt(bits.slice(i, i + 5).padEnd(5, '0'), 2)]
  return out.toLowerCase()
}

// Returns the server ID proven by the certificate's binding, or null.
export function boundServerID(pem: string): string | null {
  let cert: X509Certificate
  try {
    cert = new X509Certificate(pem)
  } catch {
    return null
  }
  const m = /URI:quarel:\/\/binding\/([A-Za-z0-9_-]+)\/([A-Za-z0-9_-]+)/.exec(cert.subjectAltName ?? '')
  if (!m) return null
  const pub = b64url(m[1])
  const sig = b64url(m[2])
  if (pub.length !== 32 || sig.length !== 64) return null
  const now = Date.now()
  if (now < Date.parse(cert.validFrom) || now > Date.parse(cert.validTo)) return null
  const spki = cert.publicKey.export({ type: 'spki', format: 'der' })
  const key = createPublicKey({ key: { kty: 'OKP', crv: 'Ed25519', x: m[1] }, format: 'jwk' })
  if (!verify(null, Buffer.concat([context, spki]), key, sig)) return null
  return serverID(pub)
}

export type CheckResult = 'authority' | 'binding' | 'mismatch' | 'unreachable'

// Connects to host:port and tells whether its certificate is trusted by an
// authority, or bound to server ID sid, or neither.
export function checkServer(host: string, port: number, sid: string, timeoutMs = 8000): Promise<CheckResult> {
  return new Promise((resolve) => {
    const sock = connect({ host, port, servername: isIP(host) ? undefined : host, rejectUnauthorized: false })
    const done = (r: CheckResult) => {
      sock.destroy()
      resolve(r)
    }
    sock.setTimeout(timeoutMs, () => done('unreachable'))
    sock.once('error', () => done('unreachable'))
    sock.once('secureConnect', () => {
      if (sock.authorized) return done('authority')
      const cert = sock.getPeerX509Certificate()
      done(cert && boundServerID(cert.toString()) === sid ? 'binding' : 'mismatch')
    })
  })
}
