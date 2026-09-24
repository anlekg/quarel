// Recovery phrase and backup encryption, as pkg/recovery (Go): 12 words of
// the French BIP-39 list (128 bits + checksum), HKDF-SHA256 backup key bound
// to the user ID, XChaCha20-Poly1305.
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import list from './french.txt?raw'

const words = list.split(/\s+/).filter(Boolean).map((w) => w.normalize('NFC'))
const fold = (w: string) => w.trim().toLowerCase().normalize('NFKD').replace(/\p{Mn}/gu, '')
const index = new Map(words.map((w, i) => [fold(w), i]))
if (words.length !== 2048 || index.size !== 2048) throw new Error('recovery: corrupted word list')

const enc = new TextEncoder()

export function encodePhrase(secret: Uint8Array): string {
  const bits = new Uint8Array(17)
  bits.set(secret)
  bits[16] = sha256(secret)[0]
  const out: string[] = []
  for (let i = 0; i < 12; i++) {
    let v = 0
    for (let j = 0; j < 11; j++) {
      const pos = i * 11 + j
      if (bits[pos >> 3] & (0x80 >> (pos % 8))) v |= 1 << (10 - j)
    }
    out.push(words[v])
  }
  return out.join(' ')
}

export function generatePhrase(): { phrase: string; secret: Uint8Array } {
  const secret = crypto.getRandomValues(new Uint8Array(16))
  return { phrase: encodePhrase(secret), secret }
}

export class PhraseError extends Error {}

// Checks a typed phrase (case and accents ignored) and returns its secret.
export function parsePhrase(phrase: string): Uint8Array {
  const fields = phrase.split(/\s+/).filter(Boolean)
  if (fields.length !== 12) throw new PhraseError(`La phrase doit compter 12 mots (${fields.length} saisis).`)
  const bits = new Uint8Array(17)
  fields.forEach((w, i) => {
    const v = index.get(fold(w))
    if (v === undefined) throw new PhraseError(`Mot inconnu : « ${w} » (mot ${i + 1}).`)
    for (let j = 0; j < 11; j++) {
      if (v & (1 << (10 - j))) {
        const pos = i * 11 + j
        bits[pos >> 3] |= 0x80 >> (pos % 8)
      }
    }
  })
  const secret = bits.slice(0, 16)
  if ((bits[16] & 0xf0) !== (sha256(secret)[0] & 0xf0)) throw new PhraseError('La phrase contient une faute de frappe (somme de contrôle incorrecte).')
  return secret
}

export const isWord = (w: string) => index.has(fold(w))

export function backupKey(secret: Uint8Array, userId: string): Uint8Array {
  return hkdf(sha256, secret, enc.encode('quarel-backup-salt-v1'), enc.encode('quarel-backup-key-v1\0' + userId), 32)
}

const ad = (userId: string) => enc.encode('quarel-backup-v1\0' + userId)

export function sealBackup(key: Uint8Array, userId: string, plaintext: Uint8Array): Uint8Array {
  const nonce = crypto.getRandomValues(new Uint8Array(24))
  const ct = xchacha20poly1305(key, nonce, ad(userId)).encrypt(plaintext)
  const out = new Uint8Array(24 + ct.length)
  out.set(nonce)
  out.set(ct, 24)
  return out
}

export class WrongKeyError extends Error {}

export function openBackup(key: Uint8Array, userId: string, data: Uint8Array): Uint8Array {
  if (data.length < 24) throw new WrongKeyError()
  try {
    return xchacha20poly1305(key, data.subarray(0, 24), ad(userId)).decrypt(data.subarray(24))
  } catch {
    throw new WrongKeyError()
  }
}
