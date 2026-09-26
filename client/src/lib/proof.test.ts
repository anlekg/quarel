import * as ed from '@noble/ed25519'
import { sha512 } from '@noble/hashes/sha2.js'
import { describe, expect, it } from 'vitest'
import { toBase64url } from './base64'
import { proofHost, proofMessage } from './proof'

ed.hashes.sha512 = sha512

// Signatures made by idtoken.SignProofV2 (Go) with the seed 01 02 … 20.
const seed = Uint8Array.from({ length: 32 }, (_, i) => i + 1)
const sid = 'kzdvvj2umnduyauf35o36k6kw4'

describe('login proofs', () => {
  it('name the host like the Go servers', () => {
    expect(proofHost('https://[2001:DB8::1]:8090')).toBe('2001:db8::1')
    expect(proofHost('https://Tarot.Example.fr.:8090/')).toBe('tarot.example.fr')
    expect(proofHost('http://127.0.0.1:8090')).toBe('127.0.0.1')
  })
  it('are signed exactly as the servers verify them', () => {
    const sign = (host: string, mode: 'binding' | 'authority') => toBase64url(ed.sign(proofMessage(sid, 'nonce-1', host, mode), seed))
    expect(sign(proofHost('https://[2001:DB8::1]:8090'), 'authority')).toBe('RoqnN5VR7iUQ9q-C52pruTcSiMV8Qe_uADz6rSLDzz-zWyqHpmZs4HXrr34zOXDNq5EWdz9F7IJLWwAqe0ddAQ')
    expect(sign('tarot.example.fr', 'binding')).toBe('XBPhQjNwAAyqkqHKFy7hFFYlu3n8Q9cjAdV4Rzgs8OF1p8dRiUQeboQmbq23NrrMSeBBrE4b4tiE61n1wdVkBQ')
  })
})
