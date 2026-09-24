// Home: friends and end-to-end encrypted private conversations.
import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { Conversation, PublicUser } from '../api/identity'
import { Avatar } from '../components/Avatar'
import { MessageContent, type MentionNames } from '../components/MessageContent'
import { Alert, Dialog, Field } from '../components/ui'
import { Chat, Download, FileIcon, Lock, Paperclip, Pencil, Plus, Send, Trash, Users } from '../components/icons'
import { errorMessage } from '../lib/errors'
import { formatDay, formatFull, formatSize, formatStamp, formatTime, sameDay } from '../lib/format'
import { prefs } from '../platform'
import { SecurityBanner } from './Security'
import type { Account } from '../state/account'
import type { FileRef, HistMsg } from '../e2e/engine'
import { FileError, MAX_FILE } from '../e2e/files'
import {
  acceptFriend, addFriend, createGroup, deleteMessage, editText, engine, leaveGroup, markRead, openDirect, openFile, removeFriend, sendFile, sendText,
  typing, typingIn, useSocial,
} from '../state/social'

const presenceLabel: Record<string, string> = { online: 'En ligne', idle: 'Absent', dnd: 'Ne pas déranger', offline: 'Hors ligne' }

function convName(c: Conversation, me: string) {
  if (c.kind === 'direct') return c.user?.pseudo ?? c.members.find((m) => m.id !== me)?.pseudo ?? 'Conversation'
  return c.name || c.members.filter((m) => m.id !== me).map((m) => m.pseudo).join(', ')
}

function UserAvatar({ account, u, size = 32, presence }: { account: Account; u: PublicUser; size?: number; presence?: string }) {
  return (
    <span className="presence-wrap">
      <Avatar id={u.id} name={u.pseudo} src={account.identity + '/v1/users/' + u.id + '/avatar'} size={size} />
      {presence && <span className={'presence ' + presence} aria-label={presenceLabel[presence]} />}
    </span>
  )
}

export function Home({ account, userbar }: { account: Account; userbar: ReactNode }) {
  const s = useSocial()
  const [view, setView] = useState<string>(() => prefs.get('home-view', 'friends'))
  const [newGroup, setNewGroup] = useState(false)
  const go = (v: string) => {
    setView(v)
    prefs.set('home-view', v)
  }
  const conv = s.conversations.find((c) => c.id === view)
  useEffect(() => {
    if (view !== 'friends' && s.status === 'ready' && !conv) go('friends')
  }, [view, conv, s.status])
  const me = account.user.id

  return (
    <>
      <aside className="sidebar">
        <div className="sidebar-head">Messages privés</div>
        <div className="sidebar-body">
          <button className={'dm-item' + (view === 'friends' ? ' active' : '')} onClick={() => go('friends')}>
            <Users size={20} /><span className="n"><span>Amis</span></span>
            {s.friends.incoming.length > 0 && <span className="ch-badge" aria-label={s.friends.incoming.length + ' demande(s)'}>{s.friends.incoming.length}</span>}
          </button>
          <div className="dm-head">
            <span>Conversations</span>
            <button className="icon-btn" style={{ width: 22, height: 22 }} aria-label="Nouveau groupe" title="Nouveau groupe" onClick={() => setNewGroup(true)}><Plus size={16} /></button>
          </div>
          {s.conversations.map((c) => {
            const other = c.kind === 'direct' ? (c.user ?? c.members.find((m) => m.id !== me)) : undefined
            return (
              <button key={c.id} className={'dm-item' + (view === c.id ? ' active' : '')} onClick={() => go(c.id)}>
                {other ? <UserAvatar account={account} u={other} presence={s.presence[other.id] ?? 'offline'} />
                  : <span className="group-mark">{convName(c, me).slice(0, 1).toUpperCase()}</span>}
                <span className="n">
                  <span>{convName(c, me)}</span>
                  {c.kind === 'group' && <span className="sub">{c.members.length} membres</span>}
                </span>
              </button>
            )
          })}
          {s.status === 'ready' && s.conversations.length === 0 && <p className="muted small" style={{ padding: '4px 8px', lineHeight: 1.5 }}>Aucune conversation. Ajoutez des amis pour discuter.</p>}
        </div>
        {userbar}
      </aside>
      <main className="content">
        {s.status === 'starting' ? <div className="empty-state"><span className="spinner" />Préparation du chiffrement…</div>
          : s.status === 'error' ? <div className="empty-state"><Alert kind="error">{'Messages privés indisponibles : ' + s.error}</Alert></div>
            : view === 'friends' || !conv ? <FriendsView account={account} onOpen={(c) => go(c.id)} />
              : <ConversationView key={conv.id} account={account} conv={conv} onLeft={() => go('friends')} />}
      </main>
      {newGroup && <NewGroupDialog account={account} onClose={() => setNewGroup(false)} onCreated={(c) => { setNewGroup(false); go(c.id) }} />}
    </>
  )
}

function FriendsView({ account, onOpen }: { account: Account; onOpen: (c: Conversation) => void }) {
  const s = useSocial()
  const [tab, setTab] = useState<'online' | 'all' | 'pending' | 'add'>(() => (s.friends.incoming.length ? 'pending' : 'online'))
  const [pseudo, setPseudo] = useState('')
  const [info, setInfo] = useState('')
  const [error, setError] = useState('')
  const act = (fn: () => Promise<unknown>) => fn().catch((e) => setError(errorMessage(e)))
  const online = s.friends.friends.filter((f) => (s.presence[f.id] ?? 'offline') !== 'offline')
  const list = tab === 'online' ? online : s.friends.friends
  const message = (u: PublicUser) => act(async () => onOpen(await openDirect(u.id)))

  return (
    <div className="channel-main">
      <header className="channel-head">
        <Users />
        <span className="title">Amis</span>
        <nav className="friends-tabs" aria-label="Amis">
          <button className={tab === 'online' ? 'active' : ''} onClick={() => setTab('online')}>En ligne</button>
          <button className={tab === 'all' ? 'active' : ''} onClick={() => setTab('all')}>Tous</button>
          <button className={tab === 'pending' ? 'active' : ''} onClick={() => setTab('pending')}>
            En attente{s.friends.incoming.length ? ' (' + s.friends.incoming.length + ')' : ''}
          </button>
          <button className={'add' + (tab === 'add' ? ' active' : '')} onClick={() => setTab('add')}>Ajouter un ami</button>
        </nav>
      </header>
      <SecurityBanner />
      <div className="friends-body">
        <Alert kind="error">{error}</Alert>
        {tab === 'add' ? (
          <form style={{ display: 'flex', flexDirection: 'column', gap: 12, maxWidth: 560 }} onSubmit={(e) => {
            e.preventDefault()
            setError('')
            setInfo('')
            if (!pseudo.trim()) return
            act(async () => {
              const r = await addFriend(pseudo)
              setInfo(r.status === 'friends' ? 'Vous êtes maintenant amis avec ' + r.user.pseudo + '.' : 'Demande envoyée à ' + r.user.pseudo + '.')
              setPseudo('')
            })
          }}>
            <h2 style={{ fontSize: 18 }}>Ajouter un ami</h2>
            <p className="muted small">Entrez son pseudo. Amis, messages privés et appels ne fonctionnent qu&apos;entre comptes du même service d&apos;identité.</p>
            <div className="copy-row">
              <input className="input" aria-label="Pseudo de l'ami" placeholder="pseudo" value={pseudo} onChange={(e) => setPseudo(e.target.value)} autoFocus />
              <button className="btn btn-primary btn-sm" style={{ height: 44 }} type="submit">Envoyer la demande</button>
            </div>
            <Alert kind="info">{info}</Alert>
          </form>
        ) : tab === 'pending' ? (
          <>
            {s.friends.incoming.length + s.friends.outgoing.length === 0 && <p className="muted">Aucune demande en attente.</p>}
            {s.friends.incoming.map((u) => (
              <div className="friend-row" key={u.id}>
                <UserAvatar account={account} u={u} />
                <div className="n"><b>{u.pseudo}</b><span className="sub">Demande reçue · {u.handle}</span></div>
                <div className="acts">
                  <button className="btn btn-primary btn-sm" onClick={() => act(() => acceptFriend(u))}>Accepter</button>
                  <button className="btn btn-ghost btn-sm" onClick={() => act(() => removeFriend(u))}>Refuser</button>
                </div>
              </div>
            ))}
            {s.friends.outgoing.map((u) => (
              <div className="friend-row" key={u.id}>
                <UserAvatar account={account} u={u} />
                <div className="n"><b>{u.pseudo}</b><span className="sub">Demande envoyée · {u.handle}</span></div>
                <div className="acts"><button className="btn btn-ghost btn-sm" onClick={() => act(() => removeFriend(u))}>Annuler</button></div>
              </div>
            ))}
          </>
        ) : (
          <>
            <div className="dm-head" style={{ padding: '0 8px 6px' }}>{tab === 'online' ? 'En ligne' : 'Tous les amis'} — {list.length}</div>
            {list.length === 0 && <p className="muted" style={{ padding: 8 }}>{tab === 'online' ? 'Aucun ami en ligne.' : 'Pas encore d’amis : « Ajouter un ami ».'}</p>}
            {list.map((u) => (
              <div className="friend-row" key={u.id}>
                <UserAvatar account={account} u={u} presence={s.presence[u.id] ?? 'offline'} />
                <div className="n"><b>{u.pseudo}</b><span className="sub">{presenceLabel[s.presence[u.id] ?? 'offline']} · {u.handle}</span></div>
                <div className="acts">
                  <button className="icon-btn" aria-label={'Écrire à ' + u.pseudo} title="Message" onClick={() => message(u)}><Chat size={18} /></button>
                  <button className="btn btn-ghost btn-sm" onClick={() => { if (confirm('Retirer ' + u.pseudo + ' de vos amis ?')) act(() => removeFriend(u)) }}>Retirer</button>
                </div>
              </div>
            ))}
          </>
        )}
      </div>
    </div>
  )
}

function NewGroupDialog({ account, onClose, onCreated }: { account: Account; onClose: () => void; onCreated: (c: Conversation) => void }) {
  const s = useSocial()
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  return (
    <Dialog title="Nouveau groupe" onClose={onClose}>
      <p className="muted small">Choisissez des amis (10 membres au plus). Les messages du groupe sont chiffrés de bout en bout.</p>
      <Field label="Nom du groupe (facultatif)" value={name} onChange={(e) => setName(e.target.value)} />
      <div className="checklist">
        {s.friends.friends.length === 0 && <p className="muted small">Ajoutez d&apos;abord des amis.</p>}
        {s.friends.friends.map((f) => (
          <label key={f.id}>
            <input type="checkbox" checked={picked.has(f.id)} onChange={(e) => {
              const n = new Set(picked)
              if (e.target.checked) n.add(f.id)
              else n.delete(f.id)
              setPicked(n)
            }} />
            <UserAvatar account={account} u={f} size={24} />{f.pseudo}
          </label>
        ))}
      </div>
      <Alert kind="error">{error}</Alert>
      <div className="dialog-actions">
        <button className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
        <button className="btn btn-primary btn-sm" disabled={picked.size === 0}
          onClick={() => createGroup([...picked], name.trim()).then(onCreated, (e) => setError(errorMessage(e)))}>Créer le groupe</button>
      </div>
    </Dialog>
  )
}

function ConversationView({ account, conv, onLeft }: { account: Account; conv: Conversation; onLeft: () => void }) {
  const s = useSocial()
  const e = engine()
  const me = account.user.id
  const history = useMemo(() => e?.history(conv.id) ?? [], [e, conv.id, s.version]) // eslint-disable-line react-hooks/exhaustive-deps
  const [text, setText] = useState('')
  const [editing, setEditing] = useState<HistMsg | null>(null)
  const [error, setError] = useState('')
  const [info, setInfo] = useState('')
  const [uploads, setUploads] = useState<{ key: number; name: string }[]>([])
  const [lightbox, setLightbox] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const names = useMemo(() => {
    const byId = new Map(conv.members.map((m) => [m.id, m.pseudo]))
    return { member: (id: string) => byId.get(id), role: () => undefined, me } as MentionNames
  }, [conv.members, me])
  const name = (id: string) => conv.members.find((m) => m.id === id)?.pseudo ?? e?.name(id) ?? '…'

  useLayoutEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [history.length])

  // Read receipt for the newest message from someone else.
  useEffect(() => {
    const last = [...history].reverse().find((m) => m.from !== me)
    const mark = () => last && document.hasFocus() && markRead(conv, last.event_id)
    mark()
    window.addEventListener('focus', mark)
    return () => window.removeEventListener('focus', mark)
  }, [history, conv, me])

  const run = (p: Promise<unknown>) => p.catch((err) => setError(errorMessage(err)))
  const send = () => {
    const t = text.trim()
    if (!t) return
    setError('')
    if (editing) run(editText(conv, editing.event_id, t).then(() => setEditing(null)))
    else run(sendText(conv, t))
    setText('')
  }

  // Files: encrypted here, sent peer to peer to the devices online, the rest via the server.
  const attach = (list: FileList | File[]) => {
    if (!s.validated) return
    setError('')
    setInfo('')
    const caption = text.trim()
    let first = true
    for (const f of Array.from(list)) {
      if (f.size > MAX_FILE) {
        setError('« ' + f.name + ' » est trop volumineux (100 Mo au maximum).')
        continue
      }
      const key = Date.now() + Math.random()
      setUploads((u) => [...u, { key, name: f.name }])
      const t = first && !editing ? caption : ''
      first = false
      sendFile(conv, f, t)
        .then((r) => {
          if (r.missed) setInfo('« ' + f.name + ' » est trop gros pour la copie du serveur : ' + r.missed + ' appareil(s) hors ligne le recevront en direct plus tard, quand vous serez connecté·e en même temps.')
        })
        .catch((err) => setError(errorMessage(err)))
        .finally(() => setUploads((u) => u.filter((x) => x.key !== key)))
    }
    if (caption && !editing) setText('')
  }

  const reads = s.reads[conv.id] ?? {}
  const lastMine = [...history].reverse().find((m) => m.from === me)
  const readers = lastMine ? Object.entries(reads).filter(([u, ev]) => u !== me && ev >= lastMine.event_id).map(([u]) => name(u)) : []
  const typers = typingIn(conv.id).filter((u) => u !== me).map(name)
  const title = convName(conv, me)

  return (
    <div className="channel-body">
      <div className="channel-main">
        <header className="channel-head">
          {conv.kind === 'group' ? <Users /> : <Chat />}
          <span className="title">{title}</span>
          <span className="e2e-badge" title="Seuls les appareils validés des participants peuvent lire ces messages. Le serveur ne voit que du chiffré."><Lock size={13} />Chiffré de bout en bout</span>
          <span style={{ flex: 1 }} />
          {conv.kind === 'group' && (
            <button className="btn btn-ghost btn-sm" onClick={() => { if (confirm('Quitter le groupe « ' + title + ' » ?')) run(leaveGroup(conv).then(onLeft)) }}>Quitter le groupe</button>
          )}
        </header>
        <SecurityBanner />
        {error && <div style={{ padding: '8px 16px 0' }}><Alert kind="error">{error}</Alert></div>}
        {s.warnings.length > 0 && <div style={{ padding: '8px 16px 0' }}><Alert kind="warn">{s.warnings[s.warnings.length - 1]}</Alert></div>}
        {info && <div style={{ padding: '8px 16px 0' }}><Alert kind="info">{info}</Alert></div>}
        <div className={'messages' + (dragging ? ' drop-target' : '')} ref={listRef} role="log" aria-label={'Conversation avec ' + title}
          onDragOver={(ev) => { if (s.validated && ev.dataTransfer.types.includes('Files')) { ev.preventDefault(); setDragging(true) } }}
          onDragLeave={() => setDragging(false)}
          onDrop={(ev) => { ev.preventDefault(); setDragging(false); attach(ev.dataTransfer.files) }}>
          <div className="messages-start">
            <h3>{title}</h3>
            <p className="muted">Début de votre conversation. Les messages sont chiffrés de bout en bout et gardés sur vos appareils.</p>
          </div>
          {history.map((m, i) => {
            const prev = history[i - 1]
            const newDay = !prev || !sameDay(prev.at, m.at)
            const grouped = !!prev && !newDay && prev.from === m.from && Date.parse(m.at) - Date.parse(prev.at) < 7 * 60_000
            const mine = m.from === me
            const u = conv.members.find((x) => x.id === m.from)
            return (
              <Fragment key={m.event_id}>
                {newDay && <div className="day-sep" role="separator">{formatDay(m.at)}</div>}
                <div className={'msg' + (grouped ? '' : ' head')} data-eid={m.event_id}>
                  <div className="gutter">
                    {grouped ? <span className="t" title={formatFull(m.at)}>{formatTime(m.at)}</span>
                      : <Avatar id={m.from} name={name(m.from)} src={u ? account.identity + '/v1/users/' + u.id + '/avatar' : undefined} size={40} />}
                  </div>
                  <div className="body">
                    {!grouped && <div className="meta"><span className="author">{name(m.from)}</span><span className="when" title={formatFull(m.at)}>{formatStamp(m.at)}</span></div>}
                    {m.file && <DMFile conv={conv} ref_={m.file} onImage={setLightbox} />}
                    <div className="text">
                      <MessageContent text={m.text} names={names} />
                      {m.edited && <span className="edited">(modifié)</span>}
                      {mine && m === lastMine && (
                        <span className="receipt">{readers.length ? (conv.kind === 'direct' ? 'Vu' : 'Vu par ' + readers.join(', ')) : m.delivered ? 'Distribué' : 'Envoyé'}</span>
                      )}
                    </div>
                  </div>
                  {mine && (
                    <div className="msg-tools">
                      {!m.file && <button aria-label="Modifier" title="Modifier" onClick={() => { setEditing(m); setText(m.text) }}><Pencil size={16} /></button>}
                      <button className="danger" aria-label="Supprimer" title="Supprimer" onClick={() => run(deleteMessage(conv, m.event_id))}><Trash size={16} /></button>
                    </div>
                  )}
                </div>
              </Fragment>
            )
          })}
          {uploads.map((u) => (
            <div className="msg upload-pending" key={u.key}>
              <div className="gutter" />
              <div className="body"><div className="attach-file"><span className="spinner" /><div className="info"><span style={{ fontWeight: 600 }}>{u.name}</span><span className="muted small">Chiffrement et envoi…</span></div></div></div>
            </div>
          ))}
        </div>
        {lightbox && <div className="lightbox" onClick={() => setLightbox(null)}><img src={lightbox} alt="" /></div>}
        <div className="composer-wrap">
          <div className="composer">
            {editing && (
              <div className="composer-bar">
                <span className="grow">Modification du message</span>
                <button className="link small" onClick={() => { setEditing(null); setText('') }}>Annuler</button>
              </div>
            )}
            <div className="composer-row">
              <button className="icon-btn" aria-label="Joindre un fichier" title="Joindre un fichier (chiffré de bout en bout)" disabled={!s.validated || !!editing}
                onClick={() => input.current?.click()}><Paperclip /></button>
              <input ref={input} type="file" multiple hidden aria-label="Fichier à envoyer" onChange={(ev) => { if (ev.target.files) attach(ev.target.files); ev.target.value = '' }} />
              <textarea rows={1} value={text} disabled={!s.validated} aria-label={'Message pour ' + title}
                placeholder={s.validated ? 'Écrire à ' + title : 'Appareil non validé'}
                onChange={(ev) => { setText(ev.target.value); if (ev.target.value) typing(conv) }}
                onKeyDown={(ev) => {
                  if (ev.key === 'Enter' && !ev.shiftKey && !ev.nativeEvent.isComposing) {
                    ev.preventDefault()
                    send()
                  }
                  if (ev.key === 'Escape' && editing) {
                    setEditing(null)
                    setText('')
                  }
                }} />
              <button className="icon-btn" aria-label="Envoyer" disabled={!s.validated} onClick={send}><Send size={18} /></button>
            </div>
          </div>
        </div>
        <div className="typing-line" aria-live="polite">
          {typers.length === 1 && <><b>{typers[0]}</b> écrit…</>}
          {typers.length > 1 && <>{typers.join(', ')} écrivent…</>}
        </div>
      </div>
    </div>
  )
}

const fileErrors: Record<string, string> = {
  unavailable: 'Fichier indisponible pour l’instant : aucun appareil qui le possède n’est en ligne, et le serveur n’en a plus de copie.',
  tampered: 'Fichier altéré ou mauvaise clé : refusé.',
  bad_key: 'Clé de fichier invalide.',
}

// A file of a private conversation: images are shown (decrypted here), other
// files are decrypted when downloaded.
function DMFile({ conv, ref_, onImage }: { conv: Conversation; ref_: FileRef; onImage: (url: string) => void }) {
  const image = /^image\/(png|jpeg|gif|webp)$/.test(ref_.mime) && ref_.size <= 20 << 20
  const [url, setUrl] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const load = async () => {
    setBusy(true)
    setError('')
    try {
      const data = await openFile(conv, ref_)
      return URL.createObjectURL(new Blob([data as BlobPart], { type: image ? ref_.mime : 'application/octet-stream' }))
    } catch (e) {
      setError(e instanceof FileError ? fileErrors[e.code] ?? e.code : errorMessage(e))
      return null
    } finally {
      setBusy(false)
    }
  }
  useEffect(() => {
    if (!image) return
    let obj: string | null = null
    let live = true
    load().then((u) => {
      obj = u
      if (live) setUrl(u)
      else if (u) URL.revokeObjectURL(u)
    })
    return () => {
      live = false
      if (obj) URL.revokeObjectURL(obj)
    }
  }, [conv.id, ref_.id]) // eslint-disable-line react-hooks/exhaustive-deps
  const download = async () => {
    const obj = url ?? (await load())
    if (!obj) return
    const link = document.createElement('a')
    link.href = obj
    link.download = ref_.name
    link.click()
    if (!url) setTimeout(() => URL.revokeObjectURL(obj), 60_000)
  }
  if (image && url) return <img className="attach-img" src={url} alt={ref_.name} onClick={() => onImage(url)} />
  return (
    <div className="attach-file" data-testid="dm-file">
      {busy ? <span className="spinner" /> : <FileIcon size={28} />}
      <div className="info">
        <span style={{ fontWeight: 600 }}>{ref_.name}</span>
        <span className="muted small">{error || formatSize(ref_.size) + ' · chiffré de bout en bout'}</span>
      </div>
      <button className="icon-btn" aria-label={'Télécharger ' + ref_.name} title="Télécharger" disabled={busy} onClick={download}><Download size={18} /></button>
    </div>
  )
}
