// Signed key messages and verification codes, as pkg/e2ekeys (Go).
import * as ed from '@noble/ed25519'
import { sha256, sha512 } from '@noble/hashes/sha2.js'

ed.hashes.sha512 = sha512

const enc = new TextEncoder()
const msg = (...parts: string[]) => enc.encode(parts.join('\0'))

export const deviceKeys = (userId: string, deviceId: string, curve: string, ed25519: string) =>
  msg('quarel-device-keys-v1', userId, deviceId, curve, ed25519)
export const deviceCert = (userId: string, deviceId: string, ed25519: string) =>
  msg('quarel-device-cert-v1', userId, deviceId, ed25519)
export const oneTimeKeyMsg = (deviceId: string, keyId: string, key: string) =>
  msg('quarel-one-time-key-v1', deviceId, keyId, key)

// Unpadded standard base64 (libolm), padded accepted when decoding.
export function b64(bytes: Uint8Array): string {
  let s = ''
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s).replace(/=+$/, '')
}

export function unb64(s: string): Uint8Array {
  const clean = s.replace(/-/g, '+').replace(/_/g, '/').replace(/=+$/, '')
  const bin = atob(clean + '==='.slice((clean.length + 3) % 4))
  return Uint8Array.from(bin, (c) => c.charCodeAt(0))
}

export function verify(publicKey: string, message: Uint8Array, signature: string): boolean {
  try {
    const pub = unb64(publicKey)
    const sig = unb64(signature)
    return pub.length === 32 && sig.length === 64 && ed.verify(sig, message, pub)
  } catch {
    return false
  }
}

// The account master key: Ed25519 from a 32-byte seed (goolm's PKSigning).
export function masterPublic(seedB64: string): string {
  return b64(ed.getPublicKey(unb64(seedB64)))
}

export function masterSign(seedB64: string, message: Uint8Array): string {
  return b64(ed.sign(message, unb64(seedB64)))
}

export function newMasterSeed(): string {
  return b64(ed.utils.randomSecretKey())
}

// 80 bits of the device key's hash, compared by the user between two devices.
export function verificationCode(deviceEd25519: string): string {
  const h = sha256(enc.encode(deviceEd25519)).subarray(0, 10)
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const b of h) bits += b.toString(2).padStart(8, '0')
  let s = ''
  for (let i = 0; i < 80; i += 5) s += alphabet[parseInt(bits.slice(i, i + 5), 2)]
  return s.slice(0, 4) + '-' + s.slice(4, 8) + '-' + s.slice(8, 12) + '-' + s.slice(12, 16)
}

export const normalizeCode = (c: string) => c.replace(/[-\s]/g, '').toUpperCase()

// The safety code two people compare to check that each app pinned the
// other's real master key (e2ekeys.ContactCode): 120 bits, six groups of four.
export function contactCode(userA: string, masterA: string, userB: string, masterB: string): string {
  if (userB < userA) [userA, masterA, userB, masterB] = [userB, masterB, userA, masterA]
  const h = sha256(msg('quarel-contact-code-v1', userA, masterA, userB, masterB)).subarray(0, 15)
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const b of h) bits += b.toString(2).padStart(8, '0')
  let s = ''
  for (let i = 0; i < 120; i += 5) s += alphabet[parseInt(bits.slice(i, i + 5), 2)]
  return s.match(/.{4}/g)!.join('-')
}
