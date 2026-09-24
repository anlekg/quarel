import { useEffect, useState, useSyncExternalStore } from 'react'

const palette = ['#2e7d6b', '#b5651d', '#5b6fd6', '#7a4fb5', '#a8456b', '#3d7fa6', '#8a7a2e']

function colorFor(id: string) {
  let h = 0
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return palette[h % palette.length]
}

// Avatars changed during this session (the browser caches the old image).
const versions = new Map<string, number>()
const listeners = new Set<() => void>()
let tick = 0

export function bumpAvatar(userId: string) {
  versions.set(userId, Date.now())
  tick++
  for (const l of listeners) l()
}

function useVersion(id: string) {
  useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => tick,
  )
  return versions.get(id)
}

// Profile picture from the identity service, or the first letter on a color
// derived from the account id.
export function Avatar({ id, name, src, size = 36 }: { id: string; name: string; src?: string; size?: number }) {
  const v = useVersion(id)
  const url = src && v ? src + (src.includes('?') ? '&' : '?') + 'v=' + v : src
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [url])
  const style = { width: size, height: size, fontSize: Math.round(size * 0.4), background: colorFor(id) }
  if (url && !failed) {
    return <img className="avatar" style={{ ...style, objectFit: 'cover' }} src={url} alt="" onError={() => setFailed(true)} />
  }
  return <span className="avatar" style={style} aria-hidden="true">{(name[0] ?? '?').toUpperCase()}</span>
}
