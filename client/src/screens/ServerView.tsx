// One community server: channel list, the open channel, members, and the
// screens a new member must go through (rules, phone).
import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useServerTheme } from '../state/themes'
import type { Channel, Member, Ready } from '../api/community'
import { Avatar } from '../components/Avatar'
import { Alert, Dialog, Field } from '../components/ui'
import { ChevronDown, Crown, Hash, Megaphone, Speaker, Thread, Forum, Stage } from '../components/icons'
import { canServer, channelTree, firstTextChannel, memberAvatar, memberColor } from '../lib/community'
import { errorMessage } from '../lib/errors'
import { inviteLink } from '../lib/invite'
import { prefs } from '../platform'
import { leaveServer, useServerState, type ServerConn, type ServerState } from '../state/servers'
import { ChannelView } from './ChannelView'
import { ForumView } from './ForumView'
import { VoiceMembers, VoiceView } from './Voice'
import { joinVoice, useVoice } from '../state/voice'
import { can } from '../lib/community'
import { showContent } from '../state/mobile'
import { askDeleteChannel, ChannelDialog, MemberDialog, sectionsFor, ServerSettings } from './ServerSettings'
import { NotifyMenu } from './NotifyMenu'
import { editMyServerProfile, memberMenuItems } from './MemberMenu'
import { CardPopover, MemberCard, openProfile } from '../components/ProfileCard'
import { ignoreServerThemeItem, ServerProfileDialog } from './Appearance'
import { menuRun, showMenu } from '../components/ContextMenu'
import { channelNotify } from '../state/notify'
import { Gear } from '../components/icons'
import { saveJSON } from '../lib/download'
import type { PublicUser } from '../api/identity'
import { addFriend, openDirect, sendText, useSocial } from '../state/social'
import { useAccount } from '../state/account'

export function ServerView({ conn, userbar }: { conn: ServerConn; userbar: React.ReactNode }) {
  const state = useServerState(conn)
  const r = state.ready
  useServerTheme(conn.saved.sid, r?.theme, (p) => conn.imageURL(p))
  const [channelID, setChannelID] = useState<number>(() => prefs.get('channel:' + conn.saved.sid, 0))
  // Phones: the member list covers the channel, so it starts hidden and is not remembered.
  const narrow = typeof matchMedia !== 'undefined' && matchMedia('(max-width: 700px)').matches
  const [showMembers, setShowMembers] = useState(() => !narrow && prefs.get('show-members', true))

  const channel = r?.channels.find((c) => c.id === channelID && c.type !== 'category') ?? (channelID != null ? state.archived[channelID] : undefined)
  const [jump, setJump] = useState<number | null>(null) // message to show once the channel is open
  // "Profil et modération…" from a message author's right-click menu.
  const [memberDialog, setMemberDialog] = useState<Member | null>(null)
  useEffect(() => {
    const open = (e: Event) => setMemberDialog((e as CustomEvent<Member>).detail)
    window.addEventListener('quarel:member-dialog', open)
    return () => window.removeEventListener('quarel:member-dialog', open)
  }, [])
  // Profile cards (components/ProfileCard.tsx) and my profile on this server.
  const [card, setCard] = useState<{ member: Member; x: number; y: number } | null>(null)
  const [myProfile, setMyProfile] = useState(false)
  useEffect(() => {
    const open = (e: Event) => setCard((e as CustomEvent<{ member: Member; x: number; y: number }>).detail)
    const mine = () => { setCard(null); setMyProfile(true) }
    window.addEventListener('quarel:profile', open)
    window.addEventListener('quarel:my-server-profile', mine)
    return () => { window.removeEventListener('quarel:profile', open); window.removeEventListener('quarel:my-server-profile', mine) }
  }, [])
  const openChannel = (id: number, messageId?: number) => {
    setChannelID(id)
    setJump(messageId ?? null)
    showContent()
  }
  // The "Vocal connecté" bar can bring us back to the voice channel.
  useEffect(() => {
    const open = (e: Event) => {
      const d = (e as CustomEvent<{ sid: string; channelId: number; messageId?: number }>).detail
      if (d.sid === conn.saved.sid) {
        setChannelID(d.channelId)
        setJump(d.messageId ?? null)
        showContent()
      }
    }
    window.addEventListener('quarel:open-channel', open)
    return () => window.removeEventListener('quarel:open-channel', open)
  }, [conn.saved.sid])
  // The open channel is gone (deleted, no longer visible): back to its parent
  // when it was a thread or a forum post, else to the first text channel.
  const parentOf = useRef<number | null>(null)
  useEffect(() => {
    if (channel) parentOf.current = channel.type === 'thread' ? channel.parent_id : null
  }, [channel])
  useEffect(() => {
    if (r && !channel) {
      const back = r.channels.find((c) => c.id === parentOf.current && c.type !== 'category')
      const first = back ?? firstTextChannel(r.channels)
      if (first) setChannelID(first.id)
    }
  }, [r, channel])
  useEffect(() => {
    if (channel) prefs.set('channel:' + conn.saved.sid, channel.id)
  }, [channel, conn.saved.sid])

  return (
    <div className="server-zone" data-qscope="server">
      <aside className="sidebar">
        <ServerMenu conn={conn} ready={r} />
        {state.status === 'offline' && <div className="conn-banner" role="status" data-qnotheme="">{state.problem ?? 'Connexion perdue, nouvelle tentative…'}</div>}
        <div className="sidebar-body">
          {r && <ChannelList conn={conn} ready={r} state={state} active={channel?.id} onPick={(c) => {
            setChannelID(c.id)
            showContent()
            if (c.type === 'voice' && can(r, c.id, 'connect')) joinVoice(conn, c.id)
          }} />}
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
        ) : channel?.type === 'voice' ? (
          <VoiceView conn={conn} ready={r} channel={channel} />
        ) : channel?.type === 'forum' ? (
          <ForumView key={channel.id} conn={conn} ready={r} state={state} channel={channel} onOpenChannel={(id) => openChannel(id)} />
        ) : channel ? (
          <ChannelView key={channel.id} conn={conn} ready={r} state={state} channel={channel}
            jump={jump} onJumped={() => setJump(null)} onOpenChannel={openChannel}
            showMembers={showMembers}
            onToggleMembers={() => { setShowMembers(!showMembers); if (!narrow) prefs.set('show-members', !showMembers) }}
            members={showMembers ? <MemberList ready={r} conn={conn} /> : null} />
        ) : (
          <div className="empty-state"><h2>Aucun salon textuel</h2><p>Vous ne voyez encore aucun salon sur ce serveur.</p></div>
        )}
      </main>
      {memberDialog && r && <MemberDialog conn={conn} ready={r} member={memberDialog} onClose={() => setMemberDialog(null)} />}
      {card && r && (
        <CardPopover x={card.x} y={card.y} onClose={() => setCard(null)}>
          <MemberCard conn={conn} ready={r} member={r.members.find((m) => m.id === card.member.id) ?? card.member}>
            <CardActions ready={r} member={card.member} onDone={() => setCard(null)} onDetails={(m) => { setCard(null); setMemberDialog(m) }} />
          </MemberCard>
        </CardPopover>
      )}
      {myProfile && r && <ServerProfileDialog conn={conn} ready={r} onClose={() => setMyProfile(false)} />}
    </div>
  )
}

// The buttons of a member's card: the app's, never the theme's.
function CardActions({ ready, member, onDone, onDetails }: { ready: Ready; member: Member; onDone: () => void; onDetails: (m: Member) => void }) {
  const account = useAccount()
  const social = useSocial()
  if (member.id === ready.member.id) {
    return <button className="btn btn-primary btn-sm" onClick={editMyServerProfile}>Modifier mon profil ici</button>
  }
  const sameService = !!account && member.issuer === account.issuer && !member.bot
  const friend = sameService && social.friends.friends.some((f) => f.id === member.subject)
  return (
    <>
      {friend && <button className="btn btn-primary btn-sm" onClick={() => {
        onDone()
        menuRun(openDirect(member.subject).then((c) => { window.dispatchEvent(new CustomEvent('quarel:goto-dm', { detail: { dmId: c.id } })) }))
      }}>Message privé</button>}
      {sameService && !friend && <button className="btn btn-ghost btn-sm" onClick={() => menuRun(addFriend(member.handle.split('@')[0]), 'Demande d’ami envoyée.')}>Demander en ami</button>}
      <button className="btn btn-ghost btn-sm" onClick={() => onDetails(member)}>Détails et modération…</button>
    </>
  )
}

function ServerMenu({ conn, ready }: { conn: ServerConn; ready?: Ready }) {
  const [open, setOpen] = useState(false)
  const [dialog, setDialog] = useState<'invite' | 'leave' | 'settings' | 'channel' | 'notify' | null>(null)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    window.addEventListener('mousedown', close)
    return () => window.removeEventListener('mousedown', close)
  }, [open])
  const canInvite = ready && canServer(ready, 'create_invite')
  const owner = ready?.member.owner
  const canAdmin = !!ready && sectionsFor(ready).length > 0
  const canChannels = !!ready && canServer(ready, 'manage_channels')
  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <button className="sidebar-head server" onClick={() => setOpen(!open)} aria-expanded={open} aria-haspopup="menu">
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{ready?.server.name ?? conn.saved.name}</span>
        <ChevronDown size={18} />
      </button>
      {open && (
        <div className="menu" role="menu" data-qnotheme="" style={{ left: 10, right: 10, top: 52 }}>
          {canInvite && <button role="menuitem" onClick={() => { setOpen(false); setDialog('invite') }}>Inviter des personnes</button>}
          {canAdmin && <button role="menuitem" onClick={() => { setOpen(false); setDialog('settings') }}>Paramètres du serveur</button>}
          {canChannels && <button role="menuitem" onClick={() => { setOpen(false); setDialog('channel') }}>Créer un salon</button>}
          {ready && <button role="menuitem" onClick={() => { setOpen(false); setDialog('notify') }}>Notifications</button>}
          {ready && <button role="menuitem" onClick={() => { setOpen(false); editMyServerProfile() }}>Mon profil sur ce serveur</button>}
          {(ready?.theme?.theme || ready?.theme?.background_v) ? (() => {
            const t = ignoreServerThemeItem(conn.saved.sid)
            return <button role="menuitem" onClick={() => { setOpen(false); t.run() }}>{t.label}</button>
          })() : null}
          {ready && <button role="menuitem" onClick={() => {
            setOpen(false)
            menuRun(conn.api((c) => c.exportMine()).then((d) => saveJSON('quarel-' + (ready.server.name || 'serveur') + '-donnees.json', d)), 'Données téléchargées.')
          }}>Mes données sur ce serveur</button>}
          {!owner && <button role="menuitem" className="danger" onClick={() => { setOpen(false); setDialog('leave') }}>Quitter le serveur</button>}

        </div>
      )}
      {dialog === 'invite' && <InviteDialog conn={conn} onClose={() => setDialog(null)} />}
      {dialog === 'leave' && <LeaveDialog conn={conn} onClose={() => setDialog(null)} />}
      {dialog === 'settings' && createPortal(<ServerSettings conn={conn} onClose={() => setDialog(null)} />, document.body)}
      {dialog === 'notify' && ready && <div className="server-notify"><NotifyMenu conn={conn} ready={ready} channelId={0} onClose={() => setDialog(null)} /></div>}
      {dialog === 'channel' && ready && <ChannelDialog conn={conn} ready={ready} channel={null} onClose={() => setDialog(null)} />}
    </div>
  )
}

// "Inviter sur…": friends (same identity service) get a single-use invite in a
// private message, shown to them as a card; or a link to copy (7 days).
function InviteDialog({ conn, onClose }: { conn: ServerConn; onClose: () => void }) {
  const [link, setLink] = useState('')
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const [q, setQ] = useState('')
  const [sent, setSent] = useState<Record<string, string>>({}) // friend → "sending", "sent" or an error
  const st = useServerState(conn)
  const social = useSocial()
  const account = useAccount()
  useEffect(() => {
    conn.api((c) => c.createInvite(0)).then(
      (inv) => setLink(inviteLink(conn.saved.base, inv.code, conn.saved.sid)),
      (e) => setError(errorMessage(e)),
    )
  }, [conn])
  const name = st.ready?.server.name ?? conn.saved.name
  const member = (userId: string) => !!st.ready?.members.some((m) => m.issuer === account?.issuer && m.subject === userId)
  const needle = q.trim().toLowerCase()
  const friends = social.friends.friends.filter((f) => !needle || f.pseudo.toLowerCase().includes(needle))
  const canDM = social.status === 'ready' && social.validated
  const invite = async (f: PublicUser) => {
    setSent((x) => ({ ...x, [f.id]: 'sending' }))
    try {
      const inv = await conn.api((c) => c.createInvite(1)) // single use: for this friend only
      const conv = await openDirect(f.id)
      await sendText(conv, 'Invitation à rejoindre « ' + name + ' » : ' + inviteLink(conn.saved.base, inv.code, conn.saved.sid))
      setSent((x) => ({ ...x, [f.id]: 'sent' }))
    } catch (e) {
      setSent((x) => ({ ...x, [f.id]: errorMessage(e) }))
    }
  }
  return (
    <Dialog title={'Inviter sur ' + name} onClose={onClose}>
      {social.friends.friends.length > 0 && (
        <>
          {!canDM && <Alert kind="info">Validez d’abord cet appareil (Messages privés) pour inviter vos amis par message. Le lien ci-dessous marche aussi.</Alert>}
          {social.friends.friends.length > 5 && (
            <input className="input" type="search" placeholder="Chercher un ami" aria-label="Chercher un ami" value={q} onChange={(e) => setQ(e.target.value)} />
          )}
          <ul className="invite-friends" aria-label="Amis à inviter">
            {friends.map((f) => {
              const state = sent[f.id]
              const already = member(f.id)
              return (
                <li key={f.id}>
                  <Avatar id={f.id} name={f.pseudo} src={account ? account.identity + '/v1/users/' + f.id + '/avatar' : undefined} size={32} />
                  <span className="grow"><b>{f.pseudo}</b>{state && state !== 'sending' && state !== 'sent' && <span className="field-error">{state}</span>}</span>
                  {already ? <span className="muted small">Déjà membre</span>
                    : state === 'sent' ? <span className="muted small">Invitation envoyée</span>
                      : <button className="btn btn-ghost btn-sm" disabled={!canDM || state === 'sending'} aria-label={'Inviter ' + f.pseudo} onClick={() => invite(f)}>Inviter</button>}
                </li>
              )
            })}
            {friends.length === 0 && <li className="muted small">Aucun ami ne correspond.</li>}
          </ul>
        </>
      )}
      <p className="muted small" style={{ lineHeight: 1.5 }}>{social.friends.friends.length ? 'Ou envoyez ce lien' : 'Envoyez ce lien aux personnes à inviter'}. Il est valable 7 jours.</p>
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
  if (c.type === 'voice') return c.stage ? <Stage size={18} /> : <Speaker size={18} />
  if (c.type === 'announcement') return <Megaphone size={18} />
  if (c.type === 'thread') return <Thread size={16} />
  if (c.type === 'forum') return <Forum size={18} />
  return <Hash size={18} />
}

function ChannelList({ conn, ready, state, active, onPick }: {
  conn: ServerConn
  ready: Ready
  state: ServerState
  active?: number
  onPick: (c: Channel) => void
}) {
  const tree = useMemo(() => channelTree(ready.channels), [ready.channels])
  const voice = useVoice()
  const button = (c: Channel) => {
    const rs = state.reads[c.id]
    const unread = c.type === 'forum' // a forum: unread when one of its posts is
      ? ready.channels.some((t) => t.type === 'thread' && t.parent_id === c.id && (state.reads[t.id]?.last_message_id ?? 0) > (state.reads[t.id]?.last_read ?? 0))
      : !!rs && rs.last_message_id > rs.last_read && c.id !== active
    if (c.type === 'voice') {
      const joined = voice?.conn === conn && voice.channelId === c.id
      return (
        <div key={c.id}>
          <button className={'ch' + (c.id === active ? ' active' : '') + (joined ? ' voice-active' : '')} onClick={() => onPick(c)}
            aria-current={c.id === active ? 'page' : undefined} title={joined ? 'Vous êtes dans ce salon vocal' : 'Rejoindre le salon vocal'}>
            {channelIcon(c)}<span className="name">{c.name}</span>
          </button>
          <VoiceMembers ready={ready} channel={c} conn={conn} />
        </div>
      )
    }
    return (
      <button key={c.id} className={'ch' + (c.id === active ? ' active' : '') + (unread ? ' unread' : '') + (c.type === 'thread' ? ' thread' : '') + (channelNotify(ready, c.id).muted ? ' muted' : '')}
        onClick={() => onPick(c)} aria-current={c.id === active ? 'page' : undefined}>
        {channelIcon(c)}
        <span className="name">{c.name}</span>
        {rs && rs.mentions > 0 && c.id !== active && <span className="ch-badge" aria-label={rs.mentions + ' mention(s)'}>{rs.mentions}</span>}
      </button>
    )
  }
  // Editing a channel: a gear next to it, for whoever manages channels or their permissions.
  // A thread: its channel's manage_channels (overrides do not apply to threads).
  const editable = (c: Channel) => can(ready, c.id, 'manage_channels') || (c.type !== 'thread' && (canServer(ready, 'manage_roles') || can(ready, c.id, 'manage_webhooks')))
  const gear = (c: Channel) => editable(c) && (
    <button className="ch-edit" aria-label={'Modifier ' + c.name} title={'Modifier ' + (c.type === 'thread' ? 'le fil' : c.type === 'category' ? 'la catégorie' : 'le salon')}
      onClick={() => setEditing(c)}><Gear size={14} /></button>
  )
  const [editing, setEditing] = useState<Channel | null>(null)
  const channelMenu = (c: Channel) => (e: React.MouseEvent) => {
    const rs = state.reads[c.id]
    showMenu(e, [
      { title: c.name },
      c.type === 'voice' && can(ready, c.id, 'connect') && { label: 'Rejoindre le vocal', onClick: () => onPick(c) },
      c.type !== 'voice' && c.type !== 'category' && { label: 'Ouvrir', onClick: () => onPick(c) },
      !!rs && rs.last_message_id > rs.last_read && { label: 'Marquer comme lu', onClick: () => menuRun(conn.api((cl) => cl.ack(c.id))) },
      editable(c) && c.type !== 'thread' && { label: c.type === 'category' ? 'Modifier la catégorie' : 'Modifier le salon', onClick: () => setEditing(c) },
      editable(c) && c.type === 'thread' && { label: 'Renommer le fil', onClick: () => setEditing(c) },
      can(ready, c.id, 'manage_channels') && { separator: true },
      can(ready, c.id, 'manage_channels') && { label: 'Supprimer ' + (c.type === 'thread' ? 'le fil' : c.type === 'category' ? 'la catégorie' : 'le salon') + '…', danger: true, onClick: () => menuRun(askDeleteChannel(conn, ready, c)) },
    ])
  }
  return (
    <nav aria-label="Salons">
      {tree.map((g) => (
        <div key={g.category?.id ?? 0}>
          {g.category && <div className="ch-group ch-wrap" onContextMenu={channelMenu(g.category)}>{g.category.name}{gear(g.category)}</div>}
          {g.items.map((n) => (
            <div key={n.channel.id}>
              <div className="ch-wrap" onContextMenu={channelMenu(n.channel)}>{button(n.channel)}{n.channel.type !== 'thread' && gear(n.channel)}</div>
              {n.threads.map((t) => <div key={t.id} className="ch-wrap" onContextMenu={channelMenu(t)}>{button(t)}{gear(t)}</div>)}
            </div>
          ))}
        </div>
      ))}
      {editing && <ChannelDialog conn={conn} ready={ready} channel={editing} onClose={() => setEditing(null)} />}
    </nav>
  )
}

export function MemberList({ ready, conn }: { ready: Ready; conn: ServerConn }) {
  const [open, setOpen] = useState<Member | null>(null)
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
            <div className="member" key={m.id} title={m.handle} role="button" tabIndex={0} aria-label={m.display_name}
              onClick={(e) => openProfile(e, m)} onKeyDown={(e) => {
                if (e.key !== 'Enter') return
                const r = e.currentTarget.getBoundingClientRect()
                openProfile({ clientX: r.left - 320, clientY: r.top }, m)
              }}
              onContextMenu={(e) => showMenu(e, memberMenuItems(conn, ready, m, setOpen))}>
              <Avatar id={m.subject || m.id} name={m.display_name} src={memberAvatar(m, conn)} size={32} />
              <span className="name" style={{ color: memberColor(ready, m) }}>{m.display_name}</span>
              {m.bot && <span className="bot-tag">BOT</span>}
              {m.owner && <span title="Propriétaire" style={{ color: '#f0b37e', display: 'flex' }}><Crown size={14} /></span>}
            </div>
          ))}
        </div>
      ))}
      {open && <MemberDialog conn={conn} ready={ready} member={open} onClose={() => setOpen(null)} />}
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
