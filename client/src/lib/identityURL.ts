// Turns what the user types ("identity.quarel.app", "localhost:8080",
// "https://id.example.org/") into the service's base URL. Same rule as the
// community server: plain HTTP only for the local machine.

export const DEFAULT_IDENTITY = 'identity.quarel.app'

// allowLAN (development builds only) also accepts plain HTTP on private
// network addresses, to test from another machine of the local network.
export function identityBaseURL(input: string, allowLAN = false): string {
  let s = input.trim()
  if (!s) throw new Error('empty')
  const plainOK = (host: string) => isLoopback(host) || (allowLAN && isPrivate(host))
  if (!/^[a-z]+:\/\//i.test(s)) {
    s = (plainOK(s) ? 'http://' : 'https://') + s
  }
  const u = new URL(s)
  if (u.protocol !== 'https:' && !(u.protocol === 'http:' && plainOK(u.host))) {
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

function isPrivate(hostport: string): boolean {
  const host = hostport.replace(/:\d+$/, '')
  return /^10\./.test(host) || /^192\.168\./.test(host) || /^172\.(1[6-9]|2\d|3[01])\./.test(host) || host.endsWith('.local')
}
