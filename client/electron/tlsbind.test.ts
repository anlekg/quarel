import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { boundServerID, serverID } from './tlsbind'

// bound-cert.pem: made by Go's tlsbind.Generate with the Ed25519 seed 00 01 … 1f
// (valid until 2031).
const pem = readFileSync(new URL('./testdata/bound-cert.pem', import.meta.url), 'utf8')

describe('tlsbind', () => {
  it('derives server IDs like the Go server', () => {
    const pub = Buffer.from('03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8', 'hex')
    expect(serverID(pub)).toBe('kzdvvj2umnduyauf35o36k6kw4')
  })
  it('reads the server ID proven by a bound certificate', () => {
    expect(boundServerID(pem)).toBe('kzdvvj2umnduyauf35o36k6kw4')
  })
  it('rejects a binding copied into another certificate, and garbage', () => {
    // plain-cert.pem: openssl certificate naming the same Ed25519 key with a bogus signature.
    const copied = readFileSync(new URL('./testdata/plain-cert.pem', import.meta.url), 'utf8')
    expect(boundServerID(copied)).toBeNull()
    expect(boundServerID('not a certificate')).toBeNull()
  })
})
