// The device key: an Ed25519 pair generated on first use and kept in the
// secret store, one per identity service (so two accounts on different
// services cannot be linked through it). Its public half is registered with each login; community
// servers check a signature by it (anti-replay of identity tokens).
import * as ed from '@noble/ed25519'
import { sha512 } from '@noble/hashes/sha2.js'
import { secrets } from '../platform'
import { fromBase64url, toBase64url } from './base64'

// Pure JS hashing: works even where WebCrypto is unavailable (plain-HTTP pages).
ed.hashes.sha512 = sha512

export interface DeviceKey {
  seed: Uint8Array
  publicKey: Uint8Array
  publicKeyB64: string
}

export async function deviceKey(identity: string): Promise<DeviceKey> {
  const name = 'device-seed:' + identity
  let stored = await secrets.get(name)
  if (!stored) {
    stored = toBase64url(ed.utils.randomSecretKey())
    await secrets.set(name, stored)
  }
  const seed = fromBase64url(stored)
  const publicKey = ed.getPublicKey(seed)
  return { seed, publicKey, publicKeyB64: toBase64url(publicKey) }
}

export function signWithDevice(key: DeviceKey, message: Uint8Array): Uint8Array {
  return ed.sign(message, key.seed)
}
