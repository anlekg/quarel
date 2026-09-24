import { describe, expect, it } from 'vitest'
import { masterPublic, masterSign, verificationCode, verify, deviceCert } from './keys'

describe('e2e keys', () => {
  it('verification codes match the Go implementation', () => {
    // Value computed by e2ekeys.VerificationCode in Go.
    expect(verificationCode('A6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg')).toBe('M32S-K4UN-HEVR-JVBL')
  })
  it('master key signatures verify', () => {
    const seed = 'AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8'
    const cert = deviceCert('u', 'd', 'k')
    expect(verify(masterPublic(seed), cert, masterSign(seed, cert))).toBe(true)
    expect(masterPublic(seed)).toBe('A6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg') // Go: ed25519.NewKeyFromSeed(00…1f)
  })
})
