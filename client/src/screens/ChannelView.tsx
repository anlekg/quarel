// An open text channel: history, composer, typing indicator, read state.
import { Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { fetchBlob } from '../api/http'
import type { Attachment, Channel, Message, Ready } from '../api/community'
import { Avatar } from '../components/Avatar'
import { CustomEmoji, encodeEmojis, encodeMentions, MessageContent, type MentionNames } from '../components/MessageContent'
import { parseCommand, suggestCommands, usage } from '../lib/commands'
import { Alert, BackButton, Dialog } from '../components/ui'
import { Bell, BellOff, Close, Download, FileIcon, Hash, Megaphone, Paperclip, Pencil, Pin, Reply, Search, Send, Smile, Thread, Trash, Users } from '../components/icons'
import { channelNotify, setActive } from '../state/notify'
import { NotifyMenu } from './NotifyMenu'
import { can, canPost, memberAvatar, memberColor } from '../lib/community'
import { errorMessage } from '../lib/errors'
import { formatDay, formatFull, formatSize, formatStamp, formatTime, roleColor, sameDay } from '../lib/format'
import type { ServerConn, ServerState } from '../state/servers'
import { isBlockedMember, useSocial } from '../state/social'

const QUICK_EMOJIS = ['👍', '❤️', '😂', '😮', '😢', '🎉', '✅', '👀']
const GROUP_MS = 7 * 60 * 1000

export function ChannelView({ conn, ready, state, channel, members, showMembers, onToggleMembers, jump, onJumped, onOpenChannel }: {
  conn: ServerConn
  ready: Ready
  state: ServerState
  channel: Channel
  members: ReactNode
  showMembers: boolean
  onToggleMembers: () => void
  jump?: number | null // message to show (search result, pin, notification)
  onJumped?: () => void
  onOpenChannel: (id: number, messageId?: number) => void
}) {
  const [panel, setPanel] = useState<'pins' | 'search' | null>(null)
  const [notifOpen, setNotifOpen] = useState(false)
  useEffect(() => {
    setActive({ kind: 'channel', sid: conn.saved.sid, channel: channel.id })
    return () => setActive(null)
  }, [conn.saved.sid, channel.id])
  const notify = channelNotify(ready, channel.id)
  const [replyTo, setReplyTo] = useState<Message | null>(null)
  const [error, setError] = useState('')
  const cm = state.messages[channel.id]

  useEffect(() => {
    conn.loadMessages(channel.id).catch((e) => setError(errorMessage(e)))
  }, [conn, channel.id, cm === undefined]) // reload after a reconnection cleared the cache

  const names: MentionNames = useMemo(() => ({
    member: (id) => ready.members.find((m) => m.id === id)?.display_name,
    role: (id) => {
      const r = ready.roles.find((x) => x.id === id)
      return r && { name: r.name, color: roleColor(r.color) }
    },
    me: ready.member.id,
    emoji: (id) => conn.emojiURL(id),
  }), [ready.members, ready.roles, ready.member.id, conn, state]) // state: an emoji image arrived

  const typing = conn.typingIn(channel.id).map((id) => names.member(id) ?? '…')

  return (
    <div className="channel-body">
      <div className="channel-main">
        <header className="channel-head">
          <BackButton />
          {channel.type === 'announcement' ? <Megaphone /> : channel.type === 'thread' ? <Thread /> : <Hash />}
          <span className="title">{channel.name}</span>
          {channel.topic ? <span className="topic" title={channel.topic}>{channel.topic}</span> : <span style={{ flex: 1 }} />}
          <span className="head-tools">
            <button className={'icon-btn' + (notify.muted || notify.level === 'none' ? ' dim' : '')} aria-label="Notifications du salon" title="Notifications"
              aria-expanded={notifOpen} onClick={() => setNotifOpen(!notifOpen)}>{notify.muted || notify.level === 'none' ? <BellOff /> : <Bell />}</button>
            {notifOpen && <NotifyMenu conn={conn} ready={ready} channelId={channel.type === 'thread' && channel.parent_id ? channel.parent_id : channel.id} onClose={() => setNotifOpen(false)} />}
          </span>
          <button className="icon-btn" aria-pressed={panel === 'pins'} aria-label="Messages épinglés" title="Messages épinglés"
            onClick={() => setPanel(panel === 'pins' ? null : 'pins')}><Pin /></button>
          <button className="icon-btn" aria-pressed={panel === 'search'} aria-label="Rechercher" title="Rechercher"
            onClick={() => setPanel(panel === 'search' ? null : 'search')}><Search /></button>
          <button className="icon-btn" onClick={() => { setPanel(null); if (panel === null || !showMembers) onToggleMembers() }} aria-pressed={showMembers && !panel}
            aria-label={showMembers && !panel ? 'Masquer les membres' : 'Afficher les membres'} title="Membres"><Users /></button>
        </header>
        {error && <div style={{ padding: '8px 16px' }}><Alert kind="error">{error}</Alert></div>}
        <MessageList conn={conn} ready={ready} channel={channel} messages={cm?.list ?? []} hasMore={cm?.hasMore ?? true}
          loading={cm?.loading ?? true} names={names} onReply={setReplyTo} onError={setError}
          jump={jump ?? null} onJumped={onJumped} onOpenChannel={onOpenChannel} />
        <Composer conn={conn} ready={ready} channel={channel} replyTo={replyTo} onCancelReply={() => setReplyTo(null)} names={names} />
        <div className="typing-line" aria-live="polite">
          {typing.length === 1 && <><b>{typing[0]}</b> écrit…</>}
          {typing.length === 2 && <><b>{typing[0]}</b> et <b>{typing[1]}</b> écrivent…</>}
          {typing.length > 2 && <>Plusieurs personnes écrivent…</>}
        </div>
      </div>
      {panel === 'pins' ? <PinsPanel conn={conn} ready={ready} channel={channel} names={names} onClose={() => setPanel(null)} onOpen={(id) => onOpenChannel(channel.id, id)} />
        : panel === 'search' ? <SearchPanel conn={conn} ready={ready} channel={channel} names={names} onClose={() => setPanel(null)} onOpen={onOpenChannel} />
          : members}
    </div>
  )
}

function MessageList({ conn, ready, channel, messages, hasMore, loading, names, onReply, onError, jump, onJumped, onOpenChannel }: {
  conn: ServerConn
  ready: Ready
  channel: Channel
  messages: Message[]
  hasMore: boolean
  loading: boolean
  names: MentionNames
  onReply: (m: Message) => void
  onError: (e: string) => void
  jump: number | null
  onJumped?: () => void
  onOpenChannel: (id: number, messageId?: number) => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const atBottom = useRef(true)
  const prevHeight = useRef(0)
  const prevFirst = useRef<number | undefined>(undefined)
  const [editing, setEditing] = useState<number | null>(null)
  const [deleting, setDeleting] = useState<Message | null>(null)
  const [flash, setFlash] = useState<number | null>(null)
  const [lightbox, setLightbox] = useState<string | null>(null)

  // Keep the view anchored: at the bottom for new messages, in place when older ones are prepended.
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const first = messages[0]?.id
    if (prevFirst.current !== undefined && first !== prevFirst.current && prevHeight.current) {
      el.scrollTop += el.scrollHeight - prevHeight.current
    } else if (atBottom.current) {
      el.scrollTop = el.scrollHeight
    }
    prevFirst.current = first
    prevHeight.current = el.scrollHeight
  }, [messages])

  const markRead = useCallback(() => {
    if (atBottom.current && document.hasFocus()) conn.ack(channel.id).catch(() => {})
  }, [conn, channel.id])

  useEffect(() => {
    const t = setTimeout(markRead, 400)
    return () => clearTimeout(t)
  }, [messages, markRead])
  useEffect(() => {
    window.addEventListener('focus', markRead)
    return () => window.removeEventListener('focus', markRead)
  }, [markRead])

  const onScroll = () => {
    const el = ref.current!
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    prevHeight.current = el.scrollHeight
    if (el.scrollTop < 300 && hasMore && !loading) conn.loadMessages(channel.id, true).catch((e) => onError(errorMessage(e)))
    if (atBottom.current) markRead()
  }

  const jumpTo = (id: number) => {
    const el = ref.current?.querySelector(`[data-mid="${id}"]`)
    if (!el) return
    el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    setFlash(id)
    setTimeout(() => setFlash(null), 1300)
  }

  // A message to show: load older pages until it is there, then scroll to it.
  useEffect(() => {
    if (jump == null) return
    let live = true
    atBottom.current = false
    conn.ensureMessage(channel.id, jump).then((found) => {
      if (!live) return
      if (found) requestAnimationFrame(() => jumpTo(jump))
      else onError('Ce message n\u2019existe plus.')
      onJumped?.()
    }, (e) => onError(errorMessage(e)))
    return () => {
      live = false
    }
  }, [jump, channel.id]) // eslint-disable-line react-hooks/exhaustive-deps

  const manage = can(ready, channel.id, 'manage_messages')
  const post = canPost(ready, channel) && channel.type !== 'thread'
  const react = can(ready, channel.id, 'add_reactions')

  return (
    <div className="messages" ref={ref} onScroll={onScroll} role="log" aria-label={'Messages de ' + channel.name}>
      {!hasMore && (
        <div className="messages-start">
          <h3>Bienvenue dans #{channel.name}</h3>
          <p className="muted">C&apos;est le début de ce salon.{channel.topic ? ' ' + channel.topic : ''}</p>
        </div>
      )}
      {loading && messages.length === 0 && <div className="empty-state"><span className="spinner" /></div>}
      {messages.map((m, i) => {
        const prev = messages[i - 1]
        const newDay = !prev || !sameDay(prev.created_at, m.created_at)
        const grouped = !!prev && !newDay && prev.author_id === m.author_id && !m.reply_to &&
          Date.parse(m.created_at) - Date.parse(prev.created_at) < GROUP_MS
        return (
          <Fragment key={m.id}>
            {newDay && <div className="day-sep" role="separator">{formatDay(m.created_at)}</div>}
            <MessageItem m={m} grouped={grouped} ready={ready} conn={conn} names={names}
              mine={m.author_id === ready.member.id} canManage={manage} canReact={react}
              editing={editing === m.id} flash={flash === m.id}
              onEdit={() => setEditing(m.id)} onEditDone={() => setEditing(null)}
              onDelete={(skipConfirm) => skipConfirm ? conn.api((c) => c.remove(channel.id, m.id)).catch((e) => onError(errorMessage(e))) : setDeleting(m)}
              onReply={() => onReply(m)} onJump={jumpTo} onImage={setLightbox} onError={onError}
              canThread={post} onOpenChannel={onOpenChannel} />
          </Fragment>
        )
      })}
      {(conn.state.ephemeral[channel.id] ?? []).map((e) => (
        <div className="msg head ephemeral" key={e.interaction_id} data-testid="ephemeral-reply">
          <div className="gutter"><Avatar id={e.bot_id} name={names.member(e.bot_id) ?? '?'} size={40} /></div>
          <div className="body">
            <div className="interaction-ref">/{e.name}</div>
            <div className="meta"><span className="author">{names.member(e.bot_id) ?? 'Bot'}</span><span className="bot-tag">BOT</span>
              <span className="when">Visible uniquement par vous</span></div>
            <div className="text"><MessageContent text={e.content} names={names} /></div>
          </div>
        </div>
      ))}
      {deleting && (
        <Dialog title="Supprimer le message ?" onClose={() => setDeleting(null)}>
          <p className="muted" style={{ lineHeight: 1.5 }}>Il disparaîtra pour tout le monde. Astuce : Maj + clic sur la corbeille supprime sans confirmation.</p>
          <div className="dialog-actions">
            <button className="btn btn-ghost btn-sm" onClick={() => setDeleting(null)}>Annuler</button>
            <button className="btn btn-danger btn-sm" onClick={() => {
              const m = deleting
              setDeleting(null)
              conn.api((c) => c.remove(channel.id, m.id)).catch((e) => onError(errorMessage(e)))
            }}>Supprimer</button>
          </div>
        </Dialog>
      )}
      {lightbox && <div className="lightbox" onClick={() => setLightbox(null)}><img src={lightbox} alt="" /></div>}
    </div>
  )
}

function MessageItem({ m, grouped, ready, conn, names, mine, canManage, canReact, editing, flash, onEdit, onEditDone, onDelete, onReply, onJump, onImage, onError, canThread, onOpenChannel }: {
  m: Message
  grouped: boolean
  ready: Ready
  conn: ServerConn
  names: MentionNames
  mine: boolean
  canManage: boolean
  canReact: boolean
  editing: boolean
  flash: boolean
  onEdit: () => void
  onEditDone: () => void
  onDelete: (skipConfirm: boolean) => void
  onReply: () => void
  onJump: (id: number) => void
  onImage: (url: string) => void
  onError: (e: string) => void
  canThread: boolean
  onOpenChannel: (id: number, messageId?: number) => void
}) {
  const [picking, setPicking] = useState(false)
  const [threading, setThreading] = useState(false)
  const [reveal, setReveal] = useState(false)
  useSocial() // re-render when the block list changes
  const author = ready.members.find((x) => x.id === m.author_id)
  if (author && !reveal && isBlockedMember(author.issuer, author.subject)) {
    return (
      <div className={'msg blocked' + (grouped ? '' : ' head')} data-mid={m.id}>
        <div className="gutter" />
        <div className="body"><span className="muted small">Message d&apos;une personne bloquée · <button className="link small" onClick={() => setReveal(true)}>Afficher</button></span></div>
      </div>
    )
  }
  const name = m.webhook?.name ?? author?.display_name ?? 'Ancien membre'
  const mentioned = conn.mentionsMe(m) && !mine
  const toggle = (emoji: string, on: boolean) =>
    conn.api((c) => c.react(m.channel_id, m.id, emoji, on)).catch((e) => onError(errorMessage(e)))

  return (
    <div className={'msg' + (grouped ? '' : ' head') + (mentioned ? ' mentioned' : '') + (flash ? ' flash' : '')} data-mid={m.id}
      onMouseLeave={() => setPicking(false)}>
      <div className="gutter">
        {grouped
          ? <span className="t" title={formatFull(m.created_at)}>{formatTime(m.created_at)}</span>
          : author
            ? <Avatar id={author.subject || author.id} name={name} src={memberAvatar(author)} size={40} />
            : <Avatar id={m.author_id} name={m.webhook?.name ?? '?'} size={40} />}
      </div>
      <div className="body">
        {m.interaction && (
          <div className="interaction-ref" data-testid="interaction">{names.member(m.interaction.member_id) ?? 'Ancien membre'} a utilisé /{m.interaction.name}</div>
        )}
        {m.referenced && (
          <div className="reply-ref" onClick={() => onJump(m.referenced!.id)} title="Aller au message">
            <b>@{names.member(m.referenced.author_id) ?? 'ancien membre'}</b>
            <span>{m.referenced.content ? <MessageContent text={m.referenced.content.replace(/\n/g, ' ')} names={names} /> : 'Pièce jointe'}</span>
          </div>
        )}
        {!grouped && (
          <div className="meta">
            <span className="author" style={{ color: memberColor(ready, author) }}>{name}</span>
            {m.webhook ? <span className="bot-tag">WEBHOOK</span> : author?.bot && <span className="bot-tag">BOT</span>}
            <span className="when" title={formatFull(m.created_at)}>{formatStamp(m.created_at)}</span>
          </div>
        )}
        {editing ? (
          <EditBox m={m} conn={conn} onDone={onEditDone} onError={onError} />
        ) : m.content ? (
          <div className="text">
            <MessageContent text={m.content} names={names} />
            {m.edited_at && <span className="edited" title={formatFull(m.edited_at)}>(modifié)</span>}
          </div>
        ) : null}
        {m.attachments.map((a) => <AttachmentView key={a.id} a={a} conn={conn} onImage={onImage} />)}
        {m.pinned_at && <span className="pin-tag" title={'Épinglé le ' + formatFull(m.pinned_at)}><Pin size={12} />Épinglé</span>}
        {m.thread_id && (() => {
          const th = ready.channels.find((c) => c.id === m.thread_id)
          return th ? <button className="thread-link" onClick={() => onOpenChannel(th.id)}><Thread size={14} />{th.name}<span className="muted small">Ouvrir le fil</span></button> : null
        })()}
        {threading && <ThreadDialog conn={conn} m={m} onClose={() => setThreading(false)} onCreated={(id) => { setThreading(false); onOpenChannel(id) }} />}
        {m.embeds.map((e) => (
          <div className="embed" key={e.url}>
            {e.site_name && <span className="site">{e.site_name}</span>}
            {e.title && <a href={e.url} target="_blank" rel="noreferrer noopener">{e.title}</a>}
            {e.description && <span className="desc">{e.description}</span>}
          </div>
        ))}
        {m.reactions.length > 0 && (
          <div className="reactions">
            {m.reactions.map((r) => (
              <button key={r.emoji} className={'reaction' + (r.me ? ' me' : '')} disabled={!canReact && !r.me}
                onClick={() => toggle(r.emoji, !r.me)} aria-pressed={r.me} aria-label={`${r.emoji} ${r.count}`}>
                <span>{r.emoji.startsWith('<:') ? <CustomEmoji code={r.emoji} names={names} /> : r.emoji}</span><span>{r.count}</span>
              </button>
            ))}
          </div>
        )}
      </div>
      {!editing && (
        <div className={'msg-tools' + (picking ? ' open' : '')}>
          {canReact && <button aria-label="Réagir" title="Réagir" onClick={() => setPicking(!picking)}><Smile size={17} /></button>}
          <button aria-label="Répondre" title="Répondre" onClick={onReply}><Reply size={17} /></button>
          {mine && <button aria-label="Modifier" title="Modifier" onClick={onEdit}><Pencil size={16} /></button>}
          {canManage && (
            <button aria-label={m.pinned_at ? 'Désépingler' : 'Épingler'} title={m.pinned_at ? 'Désépingler' : 'Épingler'}
              onClick={() => conn.api((c) => c.pin(m.channel_id, m.id, !m.pinned_at)).catch((e) => onError(errorMessage(e)))}><Pin size={16} /></button>
          )}
          {canThread && !m.thread_id && (
            <button aria-label="Créer un fil" title="Créer un fil" onClick={() => setThreading(true)}><Thread size={16} /></button>
          )}
          {(mine || canManage) && (
            <button className="danger" aria-label="Supprimer" title="Supprimer" onClick={(e) => onDelete(e.shiftKey)}><Trash size={16} /></button>
          )}
        </div>
      )}
      {picking && (
        <div className="emoji-pick" role="menu" aria-label="Réactions">
          {[...QUICK_EMOJIS, ...(ready.emojis ?? []).slice(0, 24).map((x) => '<:' + x.name + ':' + x.id + '>')].map((e) => (
            <button key={e} role="menuitem" aria-label={e.startsWith('<:') ? ':' + e.split(':')[1] + ':' : e} onClick={() => {
              setPicking(false)
              const mineAlready = m.reactions.some((r) => r.emoji === e && r.me)
              toggle(e, !mineAlready)
            }}>{e.startsWith('<:') ? <CustomEmoji code={e} names={names} /> : e}</button>
          ))}
        </div>
      )}
    </div>
  )
}

function EditBox({ m, conn, onDone, onError }: { m: Message; conn: ServerConn; onDone: () => void; onError: (e: string) => void }) {
  const [text, setText] = useState(m.content)
  const save = async () => {
    const t = text.trim()
    if (t === m.content) return onDone()
    try {
      await conn.api((c) => c.edit(m.channel_id, m.id, t))
      onDone()
    } catch (e) {
      onError(errorMessage(e))
    }
  }
  return (
    <div className="edit-box">
      <div className="composer">
        <div className="composer-row">
          <AutoTextarea value={text} onChange={setText} autoFocus aria-label="Modifier le message"
            onKeyDown={(e) => {
              if (e.key === 'Escape') onDone()
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                save()
              }
            }} />
        </div>
      </div>
      <span className="hint">Échap pour annuler · Entrée pour enregistrer</span>
    </div>
  )
}

// Attachments need the session token: downloaded into a local object URL.
function useBlobURL(conn: ServerConn, a: Attachment, enabled: boolean) {
  const [url, setURL] = useState<string | null>(null)
  useEffect(() => {
    if (!enabled) return
    let revoked = false
    let obj = ''
    fetchBlob(conn.saved.base + a.url, conn.saved.token).then((b) => {
      if (revoked) return
      obj = URL.createObjectURL(b)
      setURL(obj)
    }, () => {})
    return () => {
      revoked = true
      if (obj) URL.revokeObjectURL(obj)
    }
  }, [conn, a.url, enabled])
  return url
}

function AttachmentView({ a, conn, onImage }: { a: Attachment; conn: ServerConn; onImage: (url: string) => void }) {
  const image = /^image\/(png|jpeg|gif|webp)$/.test(a.content_type)
  const url = useBlobURL(conn, a, image)
  if (image) {
    return url
      ? <img className="attach-img" src={url} alt={a.filename} onClick={() => onImage(url)} />
      : <div className="attach-img" style={{ width: 240, height: 160 }} aria-label={'Chargement de ' + a.filename} />
  }
  const download = async () => {
    const b = await fetchBlob(conn.saved.base + a.url, conn.saved.token)
    const obj = URL.createObjectURL(b)
    const link = document.createElement('a')
    link.href = obj
    link.download = a.filename
    link.click()
    setTimeout(() => URL.revokeObjectURL(obj), 60_000)
  }
  return (
    <div className="attach-file">
      <FileIcon size={28} />
      <div className="info">
        <span style={{ fontWeight: 600 }}>{a.filename}</span>
        <span className="muted small">{formatSize(a.size)}</span>
      </div>
      <button className="icon-btn" aria-label={'Télécharger ' + a.filename} title="Télécharger" onClick={download}><Download size={18} /></button>
    </div>
  )
}

function AutoTextarea({ value, onChange, ...rest }: Omit<React.TextareaHTMLAttributes<HTMLTextAreaElement>, 'onChange'> & {
  value: string
  onChange: (v: string) => void
}) {
  const ref = useRef<HTMLTextAreaElement>(null)
  const fit = () => {
    const el = ref.current
    if (!el || !el.offsetParent) return // hidden (phone layout): measured once shown
    el.style.height = 'auto'
    el.style.height = el.scrollHeight + 'px'
  }
  useLayoutEffect(fit, [value])
  useEffect(() => {
    const ro = new ResizeObserver(() => fit())
    if (ref.current) ro.observe(ref.current)
    return () => ro.disconnect()
  }, [])
  return <textarea ref={ref} rows={1} value={value} onChange={(e) => onChange(e.target.value)} {...rest} />
}

interface Pending {
  key: number
  file: File
  id?: string
  error?: string
}

function Composer({ conn, ready, channel, replyTo, onCancelReply, names }: {
  conn: ServerConn
  ready: Ready
  channel: Channel
  replyTo: Message | null
  onCancelReply: () => void
  names: MentionNames
}) {
  const [text, setText] = useState('')
  const [files, setFiles] = useState<Pending[]>([])
  const [error, setError] = useState('')
  const [sending, setSending] = useState(false)
  const lastTyping = useRef(0)
  const input = useRef<HTMLInputElement>(null)
  const area = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (replyTo) area.current?.querySelector('textarea')?.focus()
  }, [replyTo])

  if (ready.restriction === 'timed_out') {
    const until = ready.member.timeout_until
    return <div className="composer-wrap"><div className="readonly-note">Vous êtes exclu temporairement{until ? ' jusqu’au ' + formatFull(until) : ''} : lecture seule.</div></div>
  }
  if (!canPost(ready, channel)) {
    return <div className="composer-wrap"><div className="readonly-note">{channel.type === 'announcement' ? 'Seule la modération publie dans ce salon d’annonces.' : 'Vous ne pouvez pas écrire dans ce salon.'}</div></div>
  }
  const canAttach = can(ready, channel.id, 'attach_files')

  function addFiles(list: FileList | File[]) {
    const room = 10 - files.length
    const picked = [...list].slice(0, room).map((file) => ({ key: Math.random(), file }))
    setFiles((f) => [...f, ...picked])
    for (const p of picked) {
      conn.api((c) => c.upload(channel.id, p.file)).then(
        (a) => setFiles((f) => f.map((x) => (x.key === p.key ? { ...x, id: a.id } : x))),
        (e) => setFiles((f) => f.map((x) => (x.key === p.key ? { ...x, error: errorMessage(e) } : x))),
      )
    }
  }

  const commands = conn.state.commands
  const suggestions = suggestCommands(text, commands)
  const emojiPrefix = /(?:^|\s):([a-z0-9_]{2,32})$/.exec(text)?.[1]
  const emojiMatches = emojiPrefix ? (ready.emojis ?? []).filter((e) => e.name.startsWith(emojiPrefix)).slice(0, 8) : []
  const pickEmoji = (name: string) => {
    setText(text.slice(0, text.length - emojiPrefix!.length - 1) + ':' + name + ': ')
    area.current?.querySelector('textarea')?.focus()
  }
  const botName = (id: string) => ready.members.find((m) => m.id === id)?.display_name ?? 'Bot'

  async function runCommand(): Promise<boolean> {
    const parsed = parseCommand(text, commands, {
      member: (ref) => {
        const id = /^<@([A-Za-z0-9]+)>$/.exec(ref)?.[1]
        if (id) return ready.members.find((m) => m.id === id)?.id
        const name = ref.replace(/^@/, '').toLowerCase()
        return ready.members.find((m) => m.display_name.toLowerCase() === name || m.handle.toLowerCase() === name || m.handle.toLowerCase().split('@')[0] === name)?.id
      },
      channel: (ref) => ready.channels.find((c) => c.name.toLowerCase() === ref.replace(/^#/, '').toLowerCase() && c.type !== 'category')?.id,
    })
    if (!parsed) return false
    if ('error' in parsed) {
      setError(parsed.error)
      return true
    }
    if (files.length) {
      setError('Une commande ne prend pas de fichier.')
      return true
    }
    setSending(true)
    setError('')
    try {
      await conn.api((c) => c.runCommand(channel.id, parsed.cmd.bot_id, parsed.cmd.name, parsed.options))
      setText('')
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setSending(false)
    }
    return true
  }

  async function send() {
    if (text.trim().startsWith('/') && (await runCommand())) return
    const content = encodeEmojis(encodeMentions(text.trim(), ready.members), ready.emojis ?? [])
    const ready_ = files.filter((f) => f.id)
    if (files.some((f) => !f.id && !f.error)) return setError('Envoi des fichiers en cours…')
    if (!content && ready_.length === 0) return
    setSending(true)
    setError('')
    try {
      const m = await conn.api((c) => c.send(channel.id, {
        content,
        reply_to: replyTo?.id,
        attachments: ready_.length ? ready_.map((f) => f.id!) : undefined,
      }))
      conn.addOwn(m)
      setText('')
      setFiles([])
      onCancelReply()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setSending(false)
    }
  }

  function onInput(v: string) {
    setText(v)
    const now = Date.now()
    if (v && now - lastTyping.current > 3500) {
      lastTyping.current = now
      conn.api((c) => c.typing(channel.id)).catch(() => {})
    }
  }

  return (
    <div className="composer-wrap" ref={area}
      onDragOver={(e) => canAttach && e.preventDefault()}
      onDrop={(e) => {
        if (!canAttach || !e.dataTransfer.files.length) return
        e.preventDefault()
        addFiles(e.dataTransfer.files)
      }}>
      {error && <div style={{ marginBottom: 8 }}><Alert kind="error">{error}</Alert></div>}
      {emojiMatches.length > 0 && (
        <div className="cmd-suggest" role="listbox" aria-label="Emojis du serveur">
          {emojiMatches.map((e) => (
            <button key={e.id} role="option" aria-selected={false} onClick={() => pickEmoji(e.name)}>
              <CustomEmoji code={'<:' + e.name + ':' + e.id + '>'} names={names} /><b>:{e.name}:</b>
            </button>
          ))}
        </div>
      )}
      {suggestions.length > 0 && (
        <div className="cmd-suggest" role="listbox" aria-label="Commandes">
          {suggestions.map((c) => (
            <button key={c.bot_id + c.name} role="option" aria-selected={false} onClick={() => {
              setText('/' + c.name + (c.options.length ? ' ' : ''))
              area.current?.querySelector('textarea')?.focus()
            }}>
              <b>{usage(c)}</b><span className="muted">{c.description}</span><span className="cmd-bot">{botName(c.bot_id)}</span>
            </button>
          ))}
        </div>
      )}
      <div className="composer">
        {replyTo && (
          <div className="composer-bar">
            <span className="grow">Réponse à <b>{names.member(replyTo.author_id) ?? 'ancien membre'}</b></span>
            <button className="icon-btn" style={{ width: 24, height: 24 }} aria-label="Annuler la réponse" onClick={onCancelReply}><Close size={16} /></button>
          </div>
        )}
        {files.length > 0 && (
          <div className="pending-files">
            {files.map((f) => (
              <div key={f.key} className={'pending-file' + (f.error ? ' error' : '')} title={f.error}>
                {f.id || f.error ? <FileIcon size={16} /> : <span className="spinner" style={{ width: 14, height: 14 }} />}
                <span>{f.file.name}</span>
                <button className="icon-btn" style={{ width: 22, height: 22 }} aria-label={'Retirer ' + f.file.name}
                  onClick={() => setFiles((l) => l.filter((x) => x.key !== f.key))}><Close size={14} /></button>
              </div>
            ))}
          </div>
        )}
        <div className="composer-row">
          {canAttach && (
            <>
              <button className="icon-btn" aria-label="Joindre un fichier" title="Joindre un fichier" onClick={() => input.current?.click()}><Paperclip /></button>
              <input ref={input} type="file" multiple hidden onChange={(e) => {
                if (e.target.files) addFiles(e.target.files)
                e.target.value = ''
              }} />
            </>
          )}
          <AutoTextarea value={text} onChange={onInput} placeholder={'Écrire dans #' + channel.name} aria-label={'Message pour #' + channel.name}
            onPaste={(e) => {
              if (canAttach && e.clipboardData.files.length) {
                e.preventDefault()
                addFiles(e.clipboardData.files)
              }
            }}
            onKeyDown={(e) => {
              if (e.key === 'Tab' && emojiMatches.length) {
                e.preventDefault()
                pickEmoji(emojiMatches[0].name)
                return
              }
              if (e.key === 'Tab' && suggestions.length) {
                e.preventDefault()
                setText('/' + suggestions[0].name + (suggestions[0].options.length ? ' ' : ''))
                return
              }
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                if (!sending) send()
              }
              if (e.key === 'Escape' && replyTo) onCancelReply()
            }} />
          <button className="icon-btn" aria-label="Envoyer" title="Envoyer (Entrée)" disabled={sending} onClick={send}><Send size={18} /></button>
        </div>
      </div>
    </div>
  )
}

function ThreadDialog({ conn, m, onClose, onCreated }: { conn: ServerConn; m: Message; onClose: () => void; onCreated: (id: number) => void }) {
  const [name, setName] = useState(() => m.content.replace(/<[@#&!]+[a-z0-9]+>/gi, '').trim().slice(0, 60))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <Dialog title="Créer un fil" onClose={onClose}>
      <form style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        try {
          const th = await conn.api((c) => c.createThread(m.channel_id, m.id, name.trim() || undefined))
          onCreated(th.id)
        } catch (err) {
          setError(errorMessage(err))
          setBusy(false)
        }
      }}>
        <p className="muted small" style={{ lineHeight: 1.5 }}>Une discussion à part, à partir de ce message, avec les mêmes droits que le salon.</p>
        <label className="field"><span>Nom du fil</span>
          <input className="input" value={name} maxLength={100} autoFocus onChange={(e) => setName(e.target.value)} aria-label="Nom du fil" />
        </label>
        <Alert kind="error">{error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
          <button className="btn btn-primary btn-sm" disabled={busy}>Créer le fil</button>
        </div>
      </form>
    </Dialog>
  )
}

// A short, readable excerpt of a message (mentions as names).
function Excerpt({ m, names }: { m: Message; names: MentionNames }) {
  return <MessageContent text={m.content.length > 300 ? m.content.slice(0, 300) + '…' : m.content || (m.attachments[0] ? '📎 ' + m.attachments[0].filename : '')} names={names} />
}

function ResultItem({ m, ready, names, onOpen, where }: { m: Message; ready: Ready; names: MentionNames; onOpen: () => void; where?: string }) {
  const author = ready.members.find((x) => x.id === m.author_id)
  return (
    <button className="result" onClick={onOpen}>
      <span className="meta">
        <b style={{ color: memberColor(ready, author) }}>{author?.display_name ?? 'Ancien membre'}</b>
        {where && <span className="muted small">#{where}</span>}
        <span className="muted small">{formatStamp(m.created_at)}</span>
      </span>
      <span className="text"><Excerpt m={m} names={names} /></span>
    </button>
  )
}

function PinsPanel({ conn, ready, channel, names, onClose, onOpen }: {
  conn: ServerConn; ready: Ready; channel: Channel; names: MentionNames; onClose: () => void; onOpen: (id: number) => void
}) {
  const [list, setList] = useState<Message[] | null>(null)
  const [error, setError] = useState('')
  // Reloaded when a message of the channel changes (pinned or unpinned elsewhere).
  const version = conn.state.messages[channel.id]?.list.filter((m) => m.pinned_at).map((m) => m.id).join(',')
  useEffect(() => {
    let current = true // switching channels quickly: only the latest answer counts
    conn.api((c) => c.pins(channel.id)).then((l) => current && setList(l), (e) => current && setError(errorMessage(e)))
    return () => {
      current = false
    }
  }, [conn, channel.id, version])
  return (
    <aside className="side-panel" aria-label="Messages épinglés">
      <div className="side-head"><Pin size={16} /><b>Épinglés</b><span style={{ flex: 1 }} /><button className="icon-btn" aria-label="Fermer" onClick={onClose}><Close size={16} /></button></div>
      <Alert kind="error">{error}</Alert>
      {list && list.length === 0 && <p className="muted small" style={{ padding: 12 }}>Aucun message épinglé dans ce salon.</p>}
      {list?.map((m) => <ResultItem key={m.id} m={m} ready={ready} names={names} onOpen={() => onOpen(m.id)} />)}
    </aside>
  )
}

function SearchPanel({ conn, ready, channel, names, onClose, onOpen }: {
  conn: ServerConn; ready: Ready; channel: Channel; names: MentionNames; onClose: () => void; onOpen: (channel: number, id: number) => void
}) {
  const [q, setQ] = useState('')
  const [here, setHere] = useState(false)
  const [results, setResults] = useState<Message[] | null>(null)
  const [more, setMore] = useState(false)
  const [error, setError] = useState('')
  const run = async (before?: number) => {
    if (!q.trim()) return
    setError('')
    try {
      const page = await conn.api((c) => c.search(q.trim(), { channel_id: here ? channel.id : undefined, before }))
      setResults((r) => (before && r ? [...r, ...page] : page))
      setMore(page.length === 25)
    } catch (e) {
      setError(errorMessage(e))
    }
  }
  return (
    <aside className="side-panel" aria-label="Recherche">
      <div className="side-head"><Search size={16} /><b>Rechercher</b><span style={{ flex: 1 }} /><button className="icon-btn" aria-label="Fermer" onClick={onClose}><Close size={16} /></button></div>
      <form className="search-form" onSubmit={(e) => { e.preventDefault(); run() }}>
        <input className="input" type="search" placeholder="Mots à chercher" aria-label="Mots à chercher" value={q} autoFocus onChange={(e) => setQ(e.target.value)} />
        <label className="check-line small"><input type="checkbox" checked={here} onChange={(e) => setHere(e.target.checked)} />Seulement dans #{channel.name}</label>
      </form>
      <Alert kind="error">{error}</Alert>
      {results && results.length === 0 && <p className="muted small" style={{ padding: 12 }}>Aucun résultat.</p>}
      {results?.map((m) => (
        <ResultItem key={m.id} m={m} ready={ready} names={names} where={ready.channels.find((c) => c.id === m.channel_id)?.name}
          onOpen={() => onOpen(m.channel_id, m.id)} />
      ))}
      {more && <button className="btn btn-ghost btn-sm" style={{ margin: 12 }} onClick={() => run(results![results!.length - 1].id)}>Plus de résultats</button>}
    </aside>
  )
}
