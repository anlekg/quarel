import { describe, expect, it } from 'vitest'
import { desktopLink, inviteLink, parseInvite } from './invite'

const sid = 'kzdvvj2umnduyauf35o36k6kw4'

describe('invites', () => {
  it('parses quarel:// links', () => {
    expect(parseInvite(` quarel://tarot.example.fr:8090/k7d2m9x4qp?sid=${sid} `)).toEqual({
      base: 'https://tarot.example.fr:8090', host: 'tarot.example.fr', code: 'k7d2m9x4qp', sid, claim: false,
    })
  })
  it('round-trips', () => {
    const link = inviteLink('https://192.168.1.40:8090', 'abcdef1234', sid)
    expect(parseInvite(link).base).toBe('https://192.168.1.40:8090')
  })
  it('makes web links, readable by the app and convertible for the desktop app', () => {
    const link = inviteLink('https://test.quarel.app', 'abcdef1234', sid)
    expect(link).toBe(`https://app.quarel.app/join#test.quarel.app/abcdef1234?sid=${sid}`)
    expect(parseInvite(link)).toMatchObject({ base: 'https://test.quarel.app', code: 'abcdef1234', sid })
    expect(desktopLink(link)).toBe(`quarel://test.quarel.app/abcdef1234?sid=${sid}`)
    expect(parseInvite(`https://app.quarel.app/join#192.168.1.40%3A8090/abcdefghij12345?sid=${sid}&claim=1`).claim).toBe(true)
  })
  it('recognizes owner links', () => {
    expect(parseInvite(`quarel://192.168.1.40:8090/abcdefghij12345?sid=${sid}&claim=1`).claim).toBe(true)
  })
  it('requires the server ID', () => {
    expect(() => parseInvite('quarel://host:8090/abcdef1234')).toThrow('bad_invite')
    expect(() => parseInvite('bonjour')).toThrow('bad_invite')
  })
})
