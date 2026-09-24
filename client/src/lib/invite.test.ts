import { describe, expect, it } from 'vitest'
import { inviteLink, parseInvite } from './invite'

const sid = 'kzdvvj2umnduyauf35o36k6kw4'

describe('invites', () => {
  it('parses quarel:// links', () => {
    expect(parseInvite(` quarel://tarot.example.fr:8090/k7d2m9x4qp?sid=${sid} `)).toEqual({
      base: 'https://tarot.example.fr:8090', host: 'tarot.example.fr', code: 'k7d2m9x4qp', sid,
    })
  })
  it('round-trips', () => {
    const link = inviteLink('https://192.168.1.25:8090', 'abcdef1234', sid)
    expect(parseInvite(link).base).toBe('https://192.168.1.25:8090')
  })
  it('requires the server ID', () => {
    expect(() => parseInvite('quarel://host:8090/abcdef1234')).toThrow('bad_invite')
    expect(() => parseInvite('bonjour')).toThrow('bad_invite')
  })
})
