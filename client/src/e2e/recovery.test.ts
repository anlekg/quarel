import { describe, expect, it } from 'vitest'
import { backupKey, encodePhrase, generatePhrase, openBackup, parsePhrase, PhraseError, sealBackup, WrongKeyError } from './recovery'
import { unb64 } from './keys'

const hex = (b: Uint8Array) => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
const secret = Uint8Array.from({ length: 16 }, (_, i) => i * 0x11)

describe('recovery', () => {
  it('matches pkg/recovery (Go)', () => {
    // Values computed by pkg/recovery for the secret 00112233…ff and the user "USER1".
    const phrase = 'abaisser justice légume junior envahir crucial branche coincer pliage filière horizon xénon'
    expect(encodePhrase(secret)).toBe(phrase)
    expect(hex(backupKey(secret, 'USER1'))).toBe('4a5c922cc15a94d386dee03b6d7c05a340383641c7a198e75812f6e797cc8a92')
    const ct = unb64('tfKmGeQE2SPn75hTxdjThFbjvTX9O6+XLsE6jZKPAKP6D+R4kN1h0d2z5ijvqms=')
    expect(new TextDecoder().decode(openBackup(backupKey(secret, 'USER1'), 'USER1', ct))).toBe('bonjour')
    expect(() => openBackup(backupKey(secret, 'USER1'), 'USER2', ct)).toThrow(WrongKeyError)
  })

  it('parses loosely and detects typos', () => {
    const { phrase, secret: s } = generatePhrase()
    expect(hex(parsePhrase('  ' + phrase.toUpperCase().normalize('NFKD').replace(/\p{Mn}/gu, '') + ' '))).toBe(hex(s))
    expect(hex(parsePhrase('ABAISSER justice legume junior envahir crucial branche coincer pliage filiere horizon xenon'))).toBe(hex(secret))
    expect(() => parsePhrase('abaisser justice')).toThrow(PhraseError)
    expect(() => parsePhrase('abaisser justice légume junior envahir crucial branche coincer pliage filière horizon abaisser')).toThrow(/somme de contrôle/)
  })

  it('round-trips a backup', () => {
    const k = backupKey(secret, 'u')
    const data = sealBackup(k, 'u', new TextEncoder().encode('x'.repeat(1000)))
    expect(openBackup(k, 'u', data).length).toBe(1000)
  })
})
