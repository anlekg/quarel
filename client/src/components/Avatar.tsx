import { useState } from 'react'

const palette = ['#2e7d6b', '#b5651d', '#5b6fd6', '#7a4fb5', '#a8456b', '#3d7fa6', '#8a7a2e']

function colorFor(id: string) {
  let h = 0
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return palette[h % palette.length]
}

// Profile picture from the identity service, or the first letter on a color
// derived from the account id.
export function Avatar({ id, name, src, size = 36 }: { id: string; name: string; src?: string; size?: number }) {
  const [failed, setFailed] = useState(false)
  const style = { width: size, height: size, fontSize: Math.round(size * 0.4), background: colorFor(id) }
  if (src && !failed) {
    return <img className="avatar" style={{ ...style, objectFit: 'cover' }} src={src} alt="" onError={() => setFailed(true)} />
  }
  return <span className="avatar" style={style} aria-hidden="true">{(name[0] ?? '?').toUpperCase()}</span>
}
