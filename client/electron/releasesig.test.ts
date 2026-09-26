import { generateKeyPairSync, sign } from 'node:crypto'
import { describe, expect, it } from 'vitest'
import { newer, verifyRelease } from './releasesig'

describe('release signatures', () => {
  const { privateKey, publicKey } = generateKeyPairSync('ed25519')
  const pub = publicKey.export({ format: 'der', type: 'spki' }).subarray(-32).toString('base64')
  const yml = Buffer.from('version: 0.2.0\npath: Quarel-Setup-0.2.0.exe\nsha512: abc\n')
  const sig = sign(null, Buffer.concat([Buffer.from('quarel-update-v1\0'), yml]), privateKey).toString('base64')
  it('accepts a signed description', () => expect(verifyRelease(yml, sig, pub)).toBe(true))
  it('refuses any change', () => {
    expect(verifyRelease(Buffer.from(yml.toString().replace('abc', 'abd')), sig, pub)).toBe(false)
    expect(verifyRelease(yml, sig, 'HUCc3pGLWJUpElO0b3xENE/rhnCtmmE3iAvIP+VMglg=')).toBe(false) // another key
    expect(verifyRelease(yml, 'pas-une-signature', pub)).toBe(false)
  })
  it('compares versions', () => {
    expect(newer('0.2.0', '0.1.9')).toBe(true)
    expect(newer('0.10.0', '0.9.0')).toBe(true)
    expect(newer('0.1.0', '0.1.0')).toBe(false)
  })
})
