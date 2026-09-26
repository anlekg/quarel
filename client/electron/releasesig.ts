// Signature of release descriptions (latest.yml…): Ed25519 over
// "quarel-update-v1\0" + file bytes, base64 in <file>.sig. Kept free of
// Electron so it can be unit tested.
import { createPublicKey, verify } from 'node:crypto'

// Public half of the release signing key (the private key never enters the repository).
export const RELEASE_KEY = 'HUCc3pGLWJUpElO0b3xENE/rhnCtmmE3iAvIP+VMglg='

export function verifyRelease(yml: Buffer, sigB64: string, keyB64 = RELEASE_KEY): boolean {
  const der = Buffer.concat([Buffer.from('302a300506032b6570032100', 'hex'), Buffer.from(keyB64, 'base64')])
  const key = createPublicKey({ key: der, format: 'der', type: 'spki' })
  const msg = Buffer.concat([Buffer.from('quarel-update-v1\0'), yml])
  try {
    return verify(null, msg, key, Buffer.from(sigB64.trim(), 'base64'))
  } catch {
    return false
  }
}

// Semantic version comparison (x.y.z).
export function newer(a: string, b: string): boolean {
  const pa = a.split(/[.-]/).map(Number)
  const pb = b.split(/[.-]/).map(Number)
  for (let i = 0; i < 3; i++) if ((pa[i] || 0) !== (pb[i] || 0)) return (pa[i] || 0) > (pb[i] || 0)
  return false
}
