import { describe, expect, it } from 'vitest'
import { identityBaseURL, identityLabel } from './identityURL'

describe('identityBaseURL', () => {
  it('adds https to public hosts', () => {
    expect(identityBaseURL('identity.quarel.app')).toBe('https://identity.quarel.app')
    expect(identityBaseURL(' https://id.example.org/ ')).toBe('https://id.example.org')
  })
  it('uses http only on the local machine', () => {
    expect(identityBaseURL('localhost:8080')).toBe('http://localhost:8080')
    expect(identityBaseURL('127.0.0.1:8080')).toBe('http://127.0.0.1:8080')
    expect(() => identityBaseURL('http://id.example.org')).toThrow('insecure')
  })
  it('refuses paths and empty input', () => {
    expect(() => identityBaseURL('id.example.org/api')).toThrow('path')
    expect(() => identityBaseURL('  ')).toThrow('empty')
  })
  it('labels by host', () => {
    expect(identityLabel('http://localhost:8080')).toBe('localhost:8080')
  })
})
