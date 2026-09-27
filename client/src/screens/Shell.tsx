// Signed-in layout: server rail, then either the home (private messages,
// coming with step 4) or a community server.
import { useEffect, useState } from 'react'
import type { Account } from '../state/account'
import { Avatar } from '../components/Avatar'
import { Chat, Gear, Plus } from '../components/icons'
import { prefs } from '../platform'
import { closeServers, openServers, useServerState, useServers, type ServerConn } from '../state/servers'
import { initials, JoinDialog } from './JoinDialog'
import { ServerView } from './ServerView'
import { VoiceBar } from './Voice'
import { leaveVoice } from '../state/voice'
import { closeSocial, openSocial, setPresence, useSocial } from '../state/social'
import type { Presence } from '../api/identity'
import { Home } from './Home'
import { clearPendingInvite, usePendingInvite } from '../state/invite'
import { showNav, useMobilePane } from '../state/mobile'
import { Settings } from './Settings'
import { CallBar, IncomingCall } from './Call'
import { UpdateBanner } from './Update'
import { closeCalls } from '../state/calls'

export function Shell({ account }: { account: Account }) {
  const [settings, setSettings] = useState(false)
  const [joining, setJoining] = useState(false)
  const invite = usePendingInvite()
  const pane = useMobilePane()
  const [selected, setSelected] = useState<string>(() => prefs.get('selected', 'home'))
  const servers = useServers()

  useEffect(() => {
    openServers(account)
    openSocial(account)
    return () => {
      closeCalls()
      leaveVoice()
      closeServers()
      closeSocial()
    }
  }, [account.user.id]) // eslint-disable-line react-hooks/exhaustive-deps

  // Notifications (and search results) open a channel or a conversation.
  useEffect(() => {
    const gotoChannel = (e: Event) => {
      const d = (e as CustomEvent<{ sid: string; channelId: number; messageId?: number }>).detail
      select(d.sid)
      setTimeout(() => window.dispatchEvent(new CustomEvent('quarel:open-channel', { detail: d })), 0)
    }
    const gotoDM = (e: Event) => {
      const { dmId } = (e as CustomEvent<{ dmId: string }>).detail
      select('home')
      setTimeout(() => window.dispatchEvent(new CustomEvent('quarel:open-conversation', { detail: { dmId } })), 0)
    }
    const gotoServer = (e: Event) => select((e as CustomEvent<{ sid: string }>).detail.sid) // an invite card
    window.addEventListener('quarel:goto-channel', gotoChannel)
    window.addEventListener('quarel:goto-dm', gotoDM)
    window.addEventListener('quarel:goto-server', gotoServer)
    return () => {
      window.removeEventListener('quarel:goto-channel', gotoChannel)
      window.removeEventListener('quarel:goto-dm', gotoDM)
      window.removeEventListener('quarel:goto-server', gotoServer)
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const select = (id: string) => {
    setSelected(id)
    prefs.set('selected', id)
    showNav()
  }
  const current = servers.find((s) => s.saved.sid === selected)

  const userbar = (
    <>
    <UpdateBanner />
    <CallBar onOpen={(c) => {
      select('home')
      if (c.convId) window.dispatchEvent(new CustomEvent('quarel:open-conversation', { detail: { dmId: c.convId } }))
      else window.dispatchEvent(new CustomEvent('quarel:open-dm', { detail: { userId: c.peer.id } }))
    }} />
    <VoiceBar onOpen={(conn, channelId) => {
      prefs.set('channel:' + conn.saved.sid, channelId)
      select(conn.saved.sid)
      window.dispatchEvent(new CustomEvent('quarel:open-channel', { detail: { sid: conn.saved.sid, channelId } }))
    }} />
    <div className="userbar">
      <PresenceButton account={account} />
      <button className="icon-btn" aria-label="Paramètres" title="Paramètres" onClick={() => setSettings(true)}><Gear size={18} /></button>
    </div>
    </>
  )

  return (
    <div className={'shell pane-' + pane}>
      <nav className="rail" aria-label="Serveurs">
        <HomeButton active={!current} onClick={() => select('home')} />
        <span className="rail-sep" />
        {servers.map((s) => <RailServer key={s.saved.sid} conn={s} active={s === current} onClick={() => select(s.saved.sid)} />)}
        <button className="rail-btn add" aria-label="Ajouter un serveur" title="Ajouter un serveur" onClick={() => setJoining(true)}>
          <Plus />
        </button>
      </nav>
      {current ? (
        <ServerView key={current.saved.sid} conn={current} userbar={userbar} />
      ) : (
        <Home account={account} userbar={userbar} />
      )}
      {(joining || invite) && (
        <JoinDialog key={invite} account={account} initialLink={invite} onClose={() => { setJoining(false); clearPendingInvite() }}
          onJoined={(c) => { setJoining(false); clearPendingInvite(); select(c.saved.sid) }} />
      )}
      {settings && <Settings account={account} onClose={() => setSettings(false)} />}
      <IncomingCall account={account} />
    </div>
  )
}

function RailServer({ conn, active, onClick }: { conn: ServerConn; active: boolean; onClick: () => void }) {
  const st = useServerState(conn)
  const reads = Object.values(st.reads)
  const unread = reads.some((r) => r.last_message_id > r.last_read)
  const mentions = reads.reduce((n, r) => n + r.mentions, 0)
  const name = st.ready?.server.name ?? conn.saved.name
  const label = name + (mentions ? `, ${mentions} mention(s)` : unread ? ', messages non lus' : '') +
    (st.status === 'offline' ? ', hors ligne' : st.status === 'removed' ? ', plus membre' : '')
  return (
    <button className={'rail-btn server' + (active ? ' active' : '') + (st.status !== 'ready' && st.status !== 'connecting' ? ' offline' : '')}
      aria-label={label} title={label} aria-current={active ? 'page' : undefined} onClick={onClick}>
      {unread && !active && <span className="dot" />}
      {initials(name)}
      {mentions > 0 && <span className="badge">{mentions > 99 ? '99+' : mentions}</span>}
    </button>
  )
}

function HomeButton({ active, onClick }: { active: boolean; onClick: () => void }) {
  const s = useSocial()
  const pending = s.friends.incoming.length
  return (
    <button className={'rail-btn server' + (active ? ' active' : '')} aria-label={'Messages privés' + (pending ? ', ' + pending + ' demande(s) d\u2019ami' : '')}
      title="Messages privés" aria-current={active ? 'page' : undefined} onClick={onClick}>
      <Chat />
      {pending > 0 && <span className="badge">{pending}</span>}
    </button>
  )
}

const presences: { id: Presence; label: string; sub?: string; dot: string }[] = [
  { id: 'online', label: 'En ligne', dot: 'online' },
  { id: 'idle', label: 'Absent', dot: 'idle' },
  { id: 'dnd', label: 'Ne pas déranger', sub: 'Pas de notifications', dot: 'dnd' },
  { id: 'invisible', label: 'Invisible', sub: 'Vous apparaissez hors ligne', dot: 'offline' },
]

// The signed-in person, with their presence; a click chooses it.
function PresenceButton({ account }: { account: Account }) {
  const s = useSocial()
  const [open, setOpen] = useState(false)
  const u = account.user
  const cur = presences.find((p) => p.id === s.presence_setting) ?? presences[0]
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => !(e.target as HTMLElement).closest('.presence-menu, .me-btn') && setOpen(false)
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    window.addEventListener('mousedown', close)
    window.addEventListener('keydown', esc)
    return () => {
      window.removeEventListener('mousedown', close)
      window.removeEventListener('keydown', esc)
    }
  }, [open])
  return (
    <>
      <button className="me-btn" aria-haspopup="menu" aria-expanded={open} aria-label={'Statut : ' + cur.label} title="Changer de statut" onClick={() => setOpen(!open)}>
        <span className="presence-wrap">
          <Avatar id={u.id} name={u.pseudo} src={account.identity + '/v1/users/' + u.id + '/avatar'} />
          <span className={'presence ' + cur.dot} />
        </span>
        <span className="who">
          <span className="name">{u.pseudo}</span>
          <span className="handle" title={u.handle}>{cur.label}</span>
        </span>
      </button>
      {open && (
        <div className="presence-menu" role="menu" aria-label="Statut">
          {presences.map((p) => (
            <button key={p.id} role="menuitemradio" aria-checked={p.id === cur.id} className={p.id === cur.id ? 'active' : ''}
              onClick={() => { setOpen(false); setPresence(p.id).catch(() => {}) }}>
              <span className={'presence ' + p.dot} />
              <span>{p.label}{p.sub && <span className="sub">{p.sub}</span>}</span>
            </button>
          ))}
        </div>
      )}
    </>
  )
}
