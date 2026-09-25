// Invite links: quarel://host:port/CODE?sid=SERVER_ID (owner links add &claim=1). The server ID lets the
// app verify the server's certificate without any certificate authority.
// Shared as web links, https://<web app>/join#host:port/CODE?sid=… (the part
// after # never reaches the web app's host): they open the web app, which
// offers the desktop app (quarel://) or continues in the browser.

export interface Invite {
  base: string // https://host:port
  host: string // host name, without port
  code: string
  sid: string
  claim: boolean // owner link: code is the one-time claim code shown to the host
}

export function parseInvite(input: string): Invite {
  let s = input.trim()
  const web = /^https?:\/\/[^/#]+\/join#(.+)$/i.exec(s)
  if (web) s = 'quarel://' + decodeURIComponent(web[1])
  else if (/^https?:\/\//i.test(s)) s = s.replace(/^https?:/i, 'quarel:')
  if (!/^quarel:\/\//i.test(s)) throw new Error('bad_invite')
  const u = new URL(s.replace(/^quarel:/i, 'https:'))
  const code = u.pathname.replace(/^\/+|\/+$/g, '')
  const sid = (u.searchParams.get('sid') ?? '').toLowerCase()
  if (!/^[A-Za-z0-9]{4,32}$/.test(code) || !/^[a-z2-7]{26}$/.test(sid)) throw new Error('bad_invite')
  const claim = u.searchParams.get('claim') === '1'
  return { base: 'https://' + u.host, host: u.hostname.replace(/^\[|\]$/g, ''), code, sid, claim }
}

// Where web links point: the web app itself when running in a browser (a
// self-hosted copy links to itself), else the official one.
export const webApp = () =>
  typeof location !== 'undefined' && /^https?:$/.test(location.protocol) && !/^(localhost|127\.)/.test(location.hostname)
    ? location.origin
    : (import.meta.env.VITE_WEB_APP as string | undefined) || 'https://app.quarel.app'

export function inviteLink(base: string, code: string, sid: string) {
  return webApp() + '/join#' + new URL(base).host + '/' + code + '?sid=' + sid
}

// The same invite for the desktop app.
export const desktopLink = (link: string) => {
  const inv = parseInvite(link)
  return 'quarel://' + new URL(inv.base).host + '/' + inv.code + '?sid=' + inv.sid + (inv.claim ? '&claim=1' : '')
}
