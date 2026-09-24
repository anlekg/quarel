// Invite links: quarel://host:port/CODE?sid=SERVER_ID. The server ID lets the
// app verify the server's certificate without any certificate authority.

export interface Invite {
  base: string // https://host:port
  host: string // host name, without port
  code: string
  sid: string
}

export function parseInvite(input: string): Invite {
  let s = input.trim()
  if (/^https?:\/\//i.test(s)) s = s.replace(/^https?:/i, 'quarel:')
  if (!/^quarel:\/\//i.test(s)) throw new Error('bad_invite')
  const u = new URL(s.replace(/^quarel:/i, 'https:'))
  const code = u.pathname.replace(/^\/+|\/+$/g, '')
  const sid = (u.searchParams.get('sid') ?? '').toLowerCase()
  if (!/^[A-Za-z0-9]{4,32}$/.test(code) || !/^[a-z2-7]{26}$/.test(sid)) throw new Error('bad_invite')
  return { base: 'https://' + u.host, host: u.hostname.replace(/^\[|\]$/g, ''), code, sid }
}

export function inviteLink(base: string, code: string, sid: string) {
  return 'quarel://' + new URL(base).host + '/' + code + '?sid=' + sid
}
