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
import { closeSocial, openSocial, useSocial } from '../state/social'
import { Home } from './Home'
import { Settings } from './Settings'

export function Shell({ account }: { account: Account }) {
  const [settings, setSettings] = useState(false)
  const [joining, setJoining] = useState(false)
  const [selected, setSelected] = useState<string>(() => prefs.get('selected', 'home'))
  const servers = useServers()

  useEffect(() => {
    openServers(account)
    openSocial(account)
    return () => {
      leaveVoice()
      closeServers()
      closeSocial()
    }
  }, [account.user.id]) // eslint-disable-line react-hooks/exhaustive-deps

  const select = (id: string) => {
    setSelected(id)
    prefs.set('selected', id)
  }
  const current = servers.find((s) => s.saved.sid === selected)

  const u = account.user
  const userbar = (
    <>
    <VoiceBar onOpen={(conn, channelId) => {
      prefs.set('channel:' + conn.saved.sid, channelId)
      select(conn.saved.sid)
      window.dispatchEvent(new CustomEvent('quarel:open-channel', { detail: { sid: conn.saved.sid, channelId } }))
    }} />
    <div className="userbar">
      <Avatar id={u.id} name={u.pseudo} src={account.identity + '/v1/users/' + u.id + '/avatar'} />
      <div className="who">
        <span className="name">{u.pseudo}</span>
        <span className="handle" title={u.handle}>{u.handle}</span>
      </div>
      <button className="icon-btn" aria-label="Paramètres" title="Paramètres" onClick={() => setSettings(true)}><Gear size={18} /></button>
    </div>
    </>
  )

  return (
    <div className="shell">
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
      {joining && (
        <JoinDialog account={account} onClose={() => setJoining(false)}
          onJoined={(c) => { setJoining(false); select(c.saved.sid) }} />
      )}
      {settings && <Settings account={account} onClose={() => setSettings(false)} />}
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
