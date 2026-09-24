// Turns what the user types ("identity.quarel.app", "localhost:8080",
// "https://id.example.org/") into the service's base URL. Same rule as the
// community server: plain HTTP only for the local machine.

export const DEFAULT_IDENTITY = 'identity.quarel.app'

export function identityBaseURL(input: string): string {
  let s = input.trim()
  if (!s) throw new Error('empty')
  if (!/^[a-z]+:\/\//i.test(s)) {
    s = (isLoopback(s) ? 'http://' : 'https://') + s
  }
  const u = new URL(s)
  if (u.protocol !== 'https:' && !(u.protocol === 'http:' && isLoopback(u.host))) {
    throw new Error('insecure')
  }
  if (u.pathname !== '/' || u.search || u.hash || u.username) throw new Error('path')
  return u.origin
}

// The host part as shown to the user (and as it appears in handles).
export function identityLabel(base: string): string {
  return new URL(base).host
}

function isLoopback(hostport: string): boolean {
  const host = hostport.replace(/:\d+$/, '').replace(/^\[|\]$/g, '')
  return host === 'localhost' || host === '::1' || /^127\.\d+\.\d+\.\d+$/.test(host)
}
