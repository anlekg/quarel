// Notification settings of a channel (or of the whole server, channel 0):
// level and temporary mute. Stored by the server, applied by the app.
import { useEffect, useRef, useState } from 'react'
import type { NotifyLevel, Ready } from '../api/community'
import { errorMessage } from '../lib/errors'
import { DEFAULT_LEVEL, isMuted } from '../state/notify'
import type { ServerConn } from '../state/servers'

const levelLabels: Record<Exclude<NotifyLevel, 'default'>, string> = {
  all: 'Tous les messages',
  mentions: 'Seulement les @mentions',
  none: 'Rien',
}

const mutes: [number, string][] = [[900, '15 minutes'], [3600, '1 heure'], [8 * 3600, '8 heures'], [86400, '24 heures'], [-1, 'Jusqu’à réactivation']]

export function NotifyMenu({ conn, ready, channelId, onClose }: { conn: ServerConn; ready: Ready; channelId: number; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && onClose()
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    setTimeout(() => window.addEventListener('mousedown', close), 0)
    window.addEventListener('keydown', esc)
    return () => {
      window.removeEventListener('mousedown', close)
      window.removeEventListener('keydown', esc)
    }
  }, [onClose])
  const list = ready.notification_settings ?? []
  const own = list.find((s) => s.channel_id === channelId)
  const server = list.find((s) => s.channel_id === 0)
  const level = own?.level ?? 'default'
  const muted = isMuted(own)
  const inherited = channelId === 0 ? levelLabels[DEFAULT_LEVEL] : levelLabels[server && server.level !== 'default' ? server.level : DEFAULT_LEVEL]
  const set = (lvl: NotifyLevel, muteFor: number) =>
    conn.api((c) => c.setNotification(channelId, lvl, muteFor)).catch((e) => setError(errorMessage(e)))
  const keepMute = () => (!muted ? 0 : own?.muted_until?.startsWith('9999') ? -1 : Math.max(1, Math.round((Date.parse(own!.muted_until!) - Date.now()) / 1000)))
  return (
    <div className="menu notify-menu" role="menu" ref={ref} aria-label={channelId === 0 ? 'Notifications du serveur' : 'Notifications du salon'}>
      <span className="menu-title">{channelId === 0 ? 'Notifications du serveur' : 'Notifications du salon'}</span>
      {(['default', 'all', 'mentions', 'none'] as NotifyLevel[]).map((l) => (
        <button key={l} role="menuitemradio" aria-checked={level === l} className={level === l ? 'active' : ''} onClick={() => set(l, keepMute())}>
          {l === 'default' ? (channelId === 0 ? 'Par défaut (' + inherited.toLowerCase() + ')' : 'Comme le serveur (' + inherited.toLowerCase() + ')') : levelLabels[l]}
        </button>
      ))}
      <span className="menu-sep" />
      {muted ? (
        <>
          <span className="menu-note">Sourdine {own!.muted_until!.startsWith('9999') ? 'jusqu’à réactivation' : 'jusqu’à ' + new Date(own!.muted_until!).toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' })}</span>
          <button role="menuitem" onClick={() => set(level, 0)}>Réactiver</button>
        </>
      ) : (
        mutes.map(([sec, label]) => <button key={sec} role="menuitem" onClick={() => set(level, sec)}>Sourdine : {label}</button>)
      )}
      {error && <span className="menu-note error">{error}</span>}
    </div>
  )
}
