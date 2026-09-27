// Profile cards (cosmetics block): banner, avatar, name, roles, bio, under
// the person's theme. On a server: their profile there when they set one,
// else their identity service's. The theme's CSS is filtered (lib/themecss)
// and enclosed in the card; the card's buttons are the app's (data-qnotheme).
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import type { Member, MemberProfile, Ready } from '../api/community'
import type { Profile } from '../api/identity'
import { memberAvatar, memberIdentity } from '../lib/community'
import { parseTheme, themeCSS, type Theme } from '../lib/themecss'
import { othersCSS } from '../state/themes'
import type { ServerConn } from '../state/servers'
import { Avatar } from './Avatar'

export interface CardView {
  id: string // for the avatar's colour
  name: string
  handle?: string
  avatar?: string
  banner?: string // blob: or https: URL, shown by an <img> (never through the theme's CSS)
  bio?: string
  theme?: Theme | null
  roles?: { name: string; color: number }[]
}

let cards = 0

// The card itself; preview: my own theme being edited (shown even with the
// others' themes turned off).
export function ProfileCardView({ view, preview, children }: { view: CardView; preview?: boolean; children?: ReactNode }) {
  const [scope] = useState(() => 'card' + ++cards)
  const sel = `[data-qscope="${scope}"]`
  const opts = { surfaces: [[':scope', '--bg-2']] as [string, string][] }
  const css = preview ? themeCSS(view.theme, sel, scope, opts).css : othersCSS(view.theme ?? null, sel, scope, opts).css
  return (
    <div className="profile-card" data-qscope={scope}>
      {css && <style>{css}</style>}
      <div className="pc-banner">{view.banner && <img src={view.banner} alt="" />}</div>
      <div className="pc-body">
        <span className="pc-avatar"><Avatar id={view.id} name={view.name} src={view.avatar} size={76} /></span>
        <div className="pc-name">{view.name}</div>
        {view.handle && <div className="pc-handle">{view.handle}</div>}
        {!!view.roles?.length && (
          <div className="pc-roles">
            {view.roles.map((r) => <span key={r.name} className="pc-role"><i style={{ background: r.color ? '#' + r.color.toString(16).padStart(6, '0') : 'var(--text-4)' }} />{r.name}</span>)}
          </div>
        )}
        {view.bio && <p className="pc-bio">{view.bio}</p>}
      </div>
      {children && <div className="pc-actions" data-qnotheme="">{children}</div>}
    </div>
  )
}

// A card that opens next to where one clicked, over everything (outside any
// themed area), closed by Escape or a click elsewhere.
export function CardPopover({ x, y, onClose, children }: { x: number; y: number; onClose: () => void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ left: x, top: y })
  useLayoutEffect(() => {
    const r = ref.current?.getBoundingClientRect()
    if (!r) return
    setPos({ left: Math.max(8, Math.min(x, innerWidth - r.width - 8)), top: Math.max(8, Math.min(y, innerHeight - r.height - 8)) })
  }, [x, y])
  useEffect(() => {
    const key = (e: KeyboardEvent) => e.key === 'Escape' && !document.querySelector('.dialog') && onClose()
    const down = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && !(e.target as Element).closest?.('.dialog, .backdrop') && onClose()
    window.addEventListener('keydown', key)
    window.addEventListener('mousedown', down)
    return () => { window.removeEventListener('keydown', key); window.removeEventListener('mousedown', down) }
  }, [onClose])
  return createPortal(
    <div ref={ref} className="card-popover" role="dialog" aria-label="Profil" style={pos}>{children}</div>,
    document.body,
  )
}

// Asks ServerView to show someone's card.
export function openProfile(e: { clientX: number; clientY: number }, member: Member) {
  window.dispatchEvent(new CustomEvent('quarel:profile', { detail: { member, x: e.clientX + 12, y: e.clientY - 40 } }))
}

// Someone's public profile on their identity service (for members of any service).
export async function identityProfile(base: string, id: string): Promise<Profile | null> {
  try {
    const r = await fetch(base + '/v1/users/' + encodeURIComponent(id) + '/profile')
    return r.ok ? ((await r.json()) as Profile) : null
  } catch {
    return null
  }
}

// A member's card on a server.
export function useMemberCard(conn: ServerConn, ready: Ready, m: Member): CardView {
  const [server, setServer] = useState<MemberProfile | null>(null)
  const [identity, setIdentity] = useState<Profile | null>(null)
  const base = memberIdentity(m)
  useEffect(() => {
    let live = true
    conn.api((c) => c.memberProfile(m.id)).then((p) => live && setServer(p), () => {})
    return () => { live = false }
  }, [conn, m.id, m.profile_v, m.banner_v])
  useEffect(() => {
    let live = true
    if (base) identityProfile(base, m.subject).then((p) => live && setIdentity(p))
    return () => { live = false }
  }, [base, m.subject])
  const banner = m.banner_v ? conn.imageURL('/v1/members/' + m.id + '/banner?v=' + m.banner_v)
    : identity?.banner_url && base ? base + identity.banner_url : undefined
  const roles = ready.roles.filter((r) => m.roles.includes(r.id)).sort((a, b) => b.position - a.position)
  return {
    id: m.subject || m.id,
    name: m.display_name,
    handle: m.bot ? 'Bot' : m.handle,
    avatar: memberAvatar(m, conn),
    banner,
    bio: server?.bio || identity?.bio || '',
    theme: parseTheme(server?.theme) ?? parseTheme(identity?.theme),
    roles: roles.map((r) => ({ name: r.name, color: r.color })),
  }
}

export function MemberCard({ conn, ready, member, children }: { conn: ServerConn; ready: Ready; member: Member; children?: ReactNode }) {
  const view = useMemberCard(conn, ready, member)
  return <ProfileCardView view={view}>{children}</ProfileCardView>
}
