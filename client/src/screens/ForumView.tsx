// A forum channel: its posts (threads with a title) by latest activity, and
// "Nouveau post". A post opens like any thread.
import { useEffect, useMemo, useState } from 'react'
import type { Channel, ForumPost, Ready } from '../api/community'
import { Avatar } from '../components/Avatar'
import { Alert, BackButton, Dialog } from '../components/ui'
import { Forum, Plus } from '../components/icons'
import { canPost, memberAvatar } from '../lib/community'
import { errorMessage } from '../lib/errors'
import { formatFull, formatStamp } from '../lib/format'
import type { ServerConn, ServerState } from '../state/servers'

export function ForumView({ conn, ready, state, channel, onOpenChannel }: {
  conn: ServerConn
  ready: Ready
  state: ServerState
  channel: Channel
  onOpenChannel: (id: number) => void
}) {
  const [posts, setPosts] = useState<ForumPost[] | null>(null)
  const [error, setError] = useState('')
  const [writing, setWriting] = useState(false)
  // Reload when a post appears or gets an answer.
  const activity = useMemo(() => ready.channels.filter((c) => c.type === 'thread' && c.parent_id === channel.id)
    .map((c) => c.id + ':' + (state.reads[c.id]?.last_message_id ?? 0)).join(','), [ready.channels, state.reads, channel.id])
  useEffect(() => {
    conn.api((c) => c.posts(channel.id)).then(setPosts, (e) => setError(errorMessage(e)))
  }, [conn, channel.id, activity])
  const post = canPost(ready, channel)
  const member = (id: string) => ready.members.find((m) => m.id === id)

  return (
    <div className="channel-body">
      <div className="channel-main">
        <header className="channel-head">
          <BackButton />
          <Forum />
          <span className="title">{channel.name}</span>
          {channel.topic && <span className="topic">{channel.topic}</span>}
          <span style={{ flex: 1 }} />
          {post && <button className="btn btn-primary btn-sm" onClick={() => setWriting(true)}><Plus size={16} />Nouveau post</button>}
        </header>
        <div className="forum" role="list" aria-label={'Posts de ' + channel.name}>
          <Alert kind="error">{error}</Alert>
          {posts === null && !error && <div className="empty-state"><span className="spinner" /></div>}
          {posts?.length === 0 && <div className="empty-state"><h3>Aucun post</h3><p className="muted">{post ? 'Lancez la première discussion : « Nouveau post ».' : 'Rien pour l’instant.'}</p></div>}
          {posts?.map((p) => {
            const m = member(p.author_id)
            const rs = state.reads[p.channel.id]
            const unread = !!rs && rs.last_message_id > rs.last_read
            return (
              <button key={p.channel.id} role="listitem" className={'forum-post' + (unread ? ' unread' : '')} onClick={() => onOpenChannel(p.channel.id)}>
                {m ? <Avatar id={m.subject || m.id} name={m.display_name} src={memberAvatar(m)} size={36} /> : <Avatar id={p.author_id} name="?" size={36} />}
                <span className="grow">
                  <b className="forum-title">{p.channel.name}</b>
                  <span className="forum-excerpt">{m?.display_name ?? 'Ancien membre'} : {p.excerpt}</span>
                </span>
                <span className="forum-meta" title={'Dernier message ' + formatFull(p.last_message_at)}>
                  {p.message_count - 1 === 0 ? 'Aucune réponse' : p.message_count - 1 + ' réponse' + (p.message_count > 2 ? 's' : '')}
                  <span>{formatStamp(p.last_message_at)}</span>
                </span>
              </button>
            )
          })}
        </div>
      </div>
      {writing && <NewPost conn={conn} forum={channel} onClose={() => setWriting(false)} onCreated={(id) => {
        setWriting(false)
        onOpenChannel(id)
      }} />}
    </div>
  )
}

function NewPost({ conn, forum, onClose, onCreated }: { conn: ServerConn; forum: Channel; onClose: () => void; onCreated: (id: number) => void }) {
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <Dialog title="Nouveau post" onClose={onClose}>
      <form style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        setError('')
        try {
          const p = await conn.api((c) => c.createPost(forum.id, title.trim(), content.trim()))
          onCreated(p.channel.id)
        } catch (err) {
          setError(errorMessage(err))
          setBusy(false)
        }
      }}>
        <label className="field"><span>Titre</span>
          <input className="input" value={title} maxLength={100} autoFocus onChange={(e) => setTitle(e.target.value)} aria-label="Titre du post" />
        </label>
        <label className="field"><span>Message</span>
          <textarea className="input" rows={6} maxLength={4000} value={content} onChange={(e) => setContent(e.target.value)} aria-label="Message du post" />
        </label>
        <Alert kind="error">{error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
          <button className="btn btn-primary btn-sm" disabled={busy || !title.trim() || !content.trim()}>Publier</button>
        </div>
      </form>
    </Dialog>
  )
}
