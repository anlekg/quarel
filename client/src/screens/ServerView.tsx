// One community server: channel list, the open channel, members, and the
// screens a new member must go through (rules, phone).
import { useEffect, useMemo, useRef, useState } from 'react'
import type { Channel, Member, Ready } from '../api/community'
import { Avatar } from '../components/Avatar'
import { Alert, Dialog, Field } from '../components/ui'
import { ChevronDown, Crown, Hash, Megaphone, Speaker, Thread } from '../components/icons'
import { canServer, channelTree, firstTextChannel, memberAvatar, memberColor } from '../lib/community'
import { errorMessage } from '../lib/errors'
import { inviteLink } from '../lib/invite'
import { prefs } from '../platform'
import { leaveServer, useServerState, type ServerConn, type ServerState } from '../state/servers'
import { ChannelView } from './ChannelView'

export function ServerView({ conn, userbar }: { conn: ServerConn; userbar: React.ReactNode }) {
  const state = useServerState(conn)
  const r = state.ready
  const [channelID, setChannelID] = useState<number>(() => prefs.get('channel:' + conn.saved.sid, 0))
  const [showMembers, setShowMembers] = useState(() => prefs.get('show-members', true))

  const channel = r?.channels.find((c) => c.id === channelID && c.type !== 'category' && c.type !== 'voice')
  useEffect(() => {
    if (r && !channel) {
      const first = firstTextChannel(r.channels)
      if (first) setChannelID(first.id)
    }
  }, [r, channel])
  useEffect(() => {
    if (channel) prefs.set('channel:' + conn.saved.sid, channel.id)
  }, [channel, conn.saved.sid])

  return (
    <>
      <aside className="sidebar">
        <ServerMenu conn={conn} ready={r} />
        {state.status === 'offline' && <div className="conn-banner" role="status">Connexion perdue, nouvelle tentative…</div>}
        <div className="sidebar-body">
          {r && <ChannelList ready={r} state={state} active={channel?.id} onPick={setChannelID} />}
        </div>
        {userbar}
      </aside>
      <main className="content">
        {state.status === 'removed' ? (
          <Removed conn={conn} reason={state.removed} blockedReason={state.blockedReason} />
        ) : !r ? (
          <div className="empty-state"><span className="spinner" />Connexion à {conn.saved.name}…</div>
        ) : r.restriction === 'rules_not_accepted' ? (
          <RulesGate conn={conn} ready={r} />
        ) : r.restriction === 'phone_not_verified' ? (
          <PhoneGate conn={conn} />
        ) : channel ? (
          <ChannelView key={channel.id} conn={conn} ready={r} state={state} channel={channel}
            showMembers={showMembers}
            onToggleMembers={() => { setShowMembers(!showMembers); prefs.set('show-members', !showMembers) }}
            members={showMembers ? <MemberList ready={r} /> : null} />
        ) : (
          <div className="empty-state"><h2>Aucun salon textuel</h2><p>Vous ne voyez encore aucun salon sur ce serveur.</p></div>
        )}
      </main>
    </>
  )
}

function ServerMenu({ conn, ready }: { conn: ServerConn; ready?: Ready }) {
  const [open, setOpen] = useState(false)
  const [dialog, setDialog] = useState<'invite' | 'leave' | null>(null)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    window.addEventListener('mousedown', close)
    return () => window.removeEventListener('mousedown', close)
  }, [open])
  const canInvite = ready && canServer(ready, 'create_invite')
  const owner = ready?.member.owner
  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <button className="sidebar-head server" onClick={() => setOpen(!open)} aria-expanded={open} aria-haspopup="menu">
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{ready?.server.name ?? conn.saved.name}</span>
        <ChevronDown size={18} />
      </button>
      {open && (
        <div className="menu" role="menu" style={{ left: 10, right: 10, top: 52 }}>
          {canInvite && <button role="menuitem" onClick={() => { setOpen(false); setDialog('invite') }}>Inviter des personnes</button>}
          {!owner && <button role="menuitem" className="danger" onClick={() => { setOpen(false); setDialog('leave') }}>Quitter le serveur</button>}
          {owner && !canInvite && <span className="muted small" style={{ padding: 8 }}>Aucune action disponible</span>}
        </div>
      )}
      {dialog === 'invite' && <InviteDialog conn={conn} onClose={() => setDialog(null)} />}
      {dialog === 'leave' && <LeaveDialog conn={conn} onClose={() => setDialog(null)} />}
    </div>
  )
}

function InviteDialog({ conn, onClose }: { conn: ServerConn; onClose: () => void }) {
  const [link, setLink] = useState('')
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    conn.api((c) => c.createInvite(0)).then(
      (inv) => setLink(inviteLink(conn.saved.base, inv.code, conn.saved.sid)),
      (e) => setError(errorMessage(e)),
    )
  }, [conn])
  return (
    <Dialog title={'Inviter sur ' + conn.saved.name} onClose={onClose}>
      <p className="muted small" style={{ lineHeight: 1.5 }}>Envoyez ce lien aux personnes à inviter. Il est valable 7 jours.</p>
      <Alert kind="error">{error}</Alert>
      <div className="copy-row">
        <input className="input" readOnly value={link || 'Création…'} aria-label="Lien d'invitation" onFocus={(e) => e.target.select()} />
        <button className="btn btn-primary btn-sm" style={{ height: 44 }} disabled={!link}
          onClick={() => navigator.clipboard.writeText(link).then(() => setCopied(true))}>
          {copied ? 'Copié' : 'Copier'}
        </button>
      </div>
      <div className="dialog-actions"><button className="btn btn-ghost btn-sm" onClick={onClose}>Fermer</button></div>
    </Dialog>
  )
}

function LeaveDialog({ conn, onClose }: { conn: ServerConn; onClose: () => void }) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <Dialog title={'Quitter ' + conn.saved.name + ' ?'} onClose={onClose}>
      <p className="muted" style={{ lineHeight: 1.5 }}>Vous ne recevrez plus ses messages. Pour revenir, il faudra une nouvelle invitation (sauf serveur public).</p>
      <Alert kind="error">{error}</Alert>
      <div className="dialog-actions">
        <button className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
        <button className="btn btn-danger btn-sm" disabled={busy} onClick={async () => {
          setBusy(true)
          try {
            await leaveServer(conn)
          } catch (e) {
            setError(errorMessage(e))
            setBusy(false)
          }
        }}>Quitter</button>
      </div>
    </Dialog>
  )
}

function channelIcon(c: Channel) {
  if (c.type === 'voice') return <Speaker size={18} />
  if (c.type === 'announcement') return <Megaphone size={18} />
  if (c.type === 'thread') return <Thread size={16} />
  return <Hash size={18} />
}

function ChannelList({ ready, state, active, onPick }: {
  ready: Ready
  state: ServerState
  active?: number
  onPick: (id: number) => void
}) {
  const tree = useMemo(() => channelTree(ready.channels), [ready.channels])
  const name = (id: string) => ready.members.find((m) => m.id === id)?.display_name ?? '…'
  const button = (c: Channel) => {
    const rs = state.reads[c.id]
    const unread = !!rs && rs.last_message_id > rs.last_read && c.id !== active
    if (c.type === 'voice') {
      const inside = ready.voice_states.filter((v) => v.channel_id === c.id)
      return (
        <div key={c.id}>
          <button className="ch" disabled title="Le vocal arrive à l'étape suivante">{channelIcon(c)}<span className="name">{c.name}</span></button>
          {inside.length > 0 && (
            <div className="voice-users">{inside.map((v) => <span key={v.member_id}>{name(v.member_id)}</span>)}</div>
          )}
        </div>
      )
    }
    return (
      <button key={c.id} className={'ch' + (c.id === active ? ' active' : '') + (unread ? ' unread' : '') + (c.type === 'thread' ? ' thread' : '')}
        onClick={() => onPick(c.id)} aria-current={c.id === active ? 'page' : undefined}>
        {channelIcon(c)}
        <span className="name">{c.name}</span>
        {rs && rs.mentions > 0 && c.id !== active && <span className="ch-badge" aria-label={rs.mentions + ' mention(s)'}>{rs.mentions}</span>}
      </button>
    )
  }
  return (
    <nav aria-label="Salons">
      {tree.map((g) => (
        <div key={g.category?.id ?? 0}>
          {g.category && <div className="ch-group">{g.category.name}</div>}
          {g.items.map((n) => (
            <div key={n.channel.id}>
              {button(n.channel)}
              {n.threads.map(button)}
            </div>
          ))}
        </div>
      ))}
    </nav>
  )
}

export function MemberList({ ready }: { ready: Ready }) {
  const groups = useMemo(() => {
    const hoisted = ready.roles.filter((r) => r.hoist && r.id !== 1).sort((a, b) => b.position - a.position)
    const byName = (a: Member, b: Member) => a.display_name.localeCompare(b.display_name, 'fr')
    const placed = new Set<string>()
    const out: { title: string; members: Member[] }[] = []
    for (const role of hoisted) {
      const ms = ready.members.filter((m) => !placed.has(m.id) && m.roles.includes(role.id)).sort(byName)
      ms.forEach((m) => placed.add(m.id))
      if (ms.length) out.push({ title: role.name, members: ms })
    }
    const rest = ready.members.filter((m) => !placed.has(m.id)).sort(byName)
    if (rest.length) out.push({ title: 'Membres', members: rest })
    return out
  }, [ready.members, ready.roles])
  return (
    <aside className="members" aria-label="Membres">
      {groups.map((g) => (
        <div key={g.title}>
          <div className="group">{g.title} — {g.members.length}</div>
          {g.members.map((m) => (
            <div className="member" key={m.id} title={m.handle}>
              <Avatar id={m.subject || m.id} name={m.display_name} src={memberAvatar(m)} size={32} />
              <span className="name" style={{ color: memberColor(ready, m) }}>{m.display_name}</span>
              {m.bot && <span className="bot-tag">BOT</span>}
              {m.owner && <span title="Propriétaire" style={{ color: '#f0b37e', display: 'flex' }}><Crown size={14} /></span>}
            </div>
          ))}
        </div>
      ))}
    </aside>
  )
}

function Removed({ conn, reason, blockedReason }: { conn: ServerConn; reason?: ServerState['removed']; blockedReason?: string }) {
  const text = {
    blocked: 'Ce serveur est bloqué par votre service d\u2019identité' + (blockedReason ? ' : ' + blockedReason : '') + '.',
    not_approved: 'Ce serveur n\u2019est pas (ou plus) approuvé par votre service d\u2019identité : vous ne pouvez pas vous y connecter.',
    kicked: 'Vous avez été expulsé de ce serveur. Vous pourrez revenir avec une nouvelle invitation.',
    banned: 'Vous avez été banni de ce serveur.',
    left: 'Vous avez quitté ce serveur.',
    disabled: 'Votre compte a été désactivé par votre service d’identité.',
  }[reason ?? 'kicked']
  return (
    <div className="empty-state">
      <h2>{conn.saved.name}</h2>
      <p>{text}</p>
      <button className="btn btn-ghost btn-sm" onClick={() => leaveServer(conn, false)}>Retirer de ma liste</button>
    </div>
  )
}

function RulesGate({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const [ok, setOk] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <div className="gate">
      <div className="gate-card">
        <h2>Avant de participer</h2>
        <p className="muted" style={{ lineHeight: 1.5 }}>{ready.server.name} demande d&apos;accepter ses règles.</p>
        <pre className="rules-text">{ready.server.rules}</pre>
        {ready.server.require_phone && !ready.member.phone_verified && (
          <p className="muted small">Ce serveur demande aussi un numéro de téléphone vérifié (étape suivante).</p>
        )}
        <label className="check"><input type="checkbox" checked={ok} onChange={(e) => setOk(e.target.checked)} />J&apos;ai lu et j&apos;accepte les règles</label>
        <Alert kind="error">{error}</Alert>
        <button className="btn btn-primary" disabled={!ok || busy} onClick={async () => {
          setBusy(true)
          try {
            await conn.api((c) => c.acceptRules())
          } catch (e) {
            setError(errorMessage(e))
          } finally {
            setBusy(false)
          }
        }}>Entrer dans le serveur</button>
      </div>
    </div>
  )
}

function PhoneGate({ conn }: { conn: ServerConn }) {
  const [phone, setPhone] = useState('')
  const [code, setCode] = useState('')
  const [sent, setSent] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function run(fn: () => Promise<void>) {
    setBusy(true)
    setError('')
    try {
      await fn()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="gate">
      <form className="gate-card" noValidate onSubmit={(e) => {
        e.preventDefault()
        if (!sent) run(async () => { await conn.api((c) => c.phoneStart(phone.trim())); setSent(true) })
        else run(() => conn.api((c) => c.phoneVerify(phone.trim(), code.trim())))
      }}>
        <h2>Numéro de téléphone</h2>
        <p className="muted" style={{ lineHeight: 1.5 }}>Ce serveur demande un numéro vérifié pour limiter les comptes en double. Il ne garde jamais votre numéro, seulement une empreinte.</p>
        <Field label="Numéro (format international)" value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="+33 6 12 34 56 78" disabled={sent} autoFocus />
        {sent && <Field label="Code reçu par SMS" inputMode="numeric" maxLength={6} inputClass="code" value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} autoFocus />}
        <Alert kind="error">{error}</Alert>
        <button type="submit" className="btn btn-primary" disabled={busy}>{sent ? 'Vérifier' : 'Recevoir un code'}</button>
        {sent && <button type="button" className="link small" style={{ alignSelf: 'flex-start' }} onClick={() => { setSent(false); setCode('') }}>Changer de numéro</button>}
      </form>
    </div>
  )
}
