// The device key: an Ed25519 pair generated on first use and kept in the
// secret store, one per identity service (so two accounts on different
// services cannot be linked through it). Its public half is registered with each login; community
// servers check a signature by it (anti-replay of identity tokens).
import * as ed from '@noble/ed25519'
import { secrets } from '../platform'
import { fromBase64url, toBase64url } from './base64'

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
  const publicKey = await ed.getPublicKeyAsync(seed)
  return { seed, publicKey, publicKeyB64: toBase64url(publicKey) }
}

export function signWithDevice(key: DeviceKey, message: Uint8Array): Promise<Uint8Array> {
  return ed.signAsync(message, key.seed)
}
