// Server settings, for the members allowed to manage it: overview, roles,
// channels (and their permissions), members (roles, moderation), invites,
// bans, audit log, bots. Each section shows only with its permission; the
// server checks everything again (hierarchy included).
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ServerAppearanceSection } from './Appearance'
import type { AuditEntry, AutoMod, Ban, Channel, Invite, Member, Ready, Role, Webhook } from '../api/community'
import { Avatar } from '../components/Avatar'
import { Alert, Dialog, Field, Submit } from '../components/ui'
import { CustomEmoji } from '../components/MessageContent'
import { Close } from '../components/icons'
import { can, canServer, channelPerms, channelTree, isAdmin, mayGrant, memberAvatar, memberColor, myTop, outranks, permGroups } from '../lib/community'
import { errorMessage } from '../lib/errors'
import { roleColor } from '../lib/format'
import { inviteLink } from '../lib/invite'
import { useServerState, type ServerConn } from '../state/servers'
import { confirmAction } from '../components/ConfirmDialog'

type Section = 'overview' | 'roles' | 'channels' | 'members' | 'invites' | 'bans' | 'automod' | 'emojis' | 'audit' | 'bots' | 'appearance'

const dateTime = new Intl.DateTimeFormat('fr-FR', { dateStyle: 'medium', timeStyle: 'short' })

function useAction() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [info, setInfo] = useState('')
  const run = async (fn: () => Promise<string | void>) => {
    setBusy(true)
    setError('')
    setInfo('')
    try {
      const msg = await fn()
      if (msg) setInfo(msg)
      return true
    } catch (e) {
      setError(errorMessage(e))
      return false
    } finally {
      setBusy(false)
    }
  }
  return { busy, error, info, run }
}

export function sectionsFor(r: Ready): Section[] {
  const out: Section[] = []
  if (canServer(r, 'manage_server')) out.push('overview')
  if (canServer(r, 'manage_roles')) out.push('roles')
  if (canServer(r, 'manage_channels') || canServer(r, 'manage_roles')) out.push('channels')
  if (['manage_roles', 'kick_members', 'ban_members', 'moderate_members'].some((p) => canServer(r, p))) out.push('members')
  if (canServer(r, 'create_invite') || canServer(r, 'manage_server')) out.push('invites')
  if (canServer(r, 'ban_members')) out.push('bans')
  if (canServer(r, 'manage_server')) out.push('automod', 'emojis', 'appearance')
  if (canServer(r, 'view_audit_log')) out.push('audit')
  if (canServer(r, 'manage_server')) out.push('bots')
  return out
}

const labels: Record<Section, string> = {
  overview: 'Vue d’ensemble', roles: 'Rôles', channels: 'Salons', members: 'Membres', invites: 'Invitations', bans: 'Bannissements',
  automod: 'Modération automatique', emojis: 'Emojis', appearance: 'Apparence', audit: 'Journal de modération', bots: 'Bots',
}

export function ServerSettings({ conn, onClose, initial }: { conn: ServerConn; onClose: () => void; initial?: Section }) {
  const st = useServerState(conn)
  const r = st.ready
  const sections = r ? sectionsFor(r) : []
  const [section, setSection] = useState<Section>(initial ?? sections[0] ?? 'invites')
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !e.defaultPrevented && !document.querySelector('.dialog') && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  useEffect(() => {
    if (r && sections.length && !sections.includes(section)) setSection(sections[0])
    if (r && !sections.length) onClose() // permissions lost meanwhile
  }, [r, sections, section, onClose])
  if (!r) return null
  return (
    <div className="settings" role="dialog" aria-modal="true" aria-label="Paramètres du serveur">
      <nav className="settings-nav" aria-label="Sections">
        <span className="group">{r.server.name.toUpperCase()}</span>
        {sections.map((s) => <button key={s} className={section === s ? 'active' : ''} onClick={() => setSection(s)}>{labels[s]}</button>)}
      </nav>
      <main className="settings-main">
        {section === 'overview' && <Overview conn={conn} ready={r} />}
        {section === 'roles' && <Roles conn={conn} ready={r} />}
        {section === 'channels' && <Channels conn={conn} ready={r} />}
        {section === 'members' && <Members conn={conn} ready={r} />}
        {section === 'invites' && <Invites conn={conn} ready={r} />}
        {section === 'bans' && <Bans conn={conn} ready={r} />}
        {section === 'automod' && <AutoModSection conn={conn} />}
        {section === 'emojis' && <EmojisSection conn={conn} ready={r} />}
        {section === 'appearance' && <ServerAppearanceSection conn={conn} ready={r} />}
        {section === 'audit' && <Audit conn={conn} ready={r} />}
        {section === 'bots' && <Bots conn={conn} ready={r} />}
      </main>
      <button className="icon-btn settings-close" aria-label="Fermer les paramètres du serveur" title="Fermer (Échap)" onClick={onClose}><Close /></button>
    </div>
  )
}

// --- overview ---

function Overview({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const s = ready.server
  const [name, setName] = useState(s.name)
  const [access, setAccess] = useState(s.access)
  const [rules, setRules] = useState(s.rules)
  const [phone, setPhone] = useState(s.require_phone)
  const a = useAction()
  const changed = name !== s.name || access !== s.access || rules !== s.rules || phone !== s.require_phone
  return (
    <>
      <h2>Vue d&apos;ensemble</h2>
      <form className="settings-form" onSubmit={(e) => {
        e.preventDefault()
        a.run(async () => {
          await conn.api((c) => c.updateServer({ name: name.trim(), access, rules, require_phone: phone }))
          return 'Enregistré.'
        })
      }}>
        <Field label="Nom du serveur" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} />
        <div className="field">
          <label htmlFor="srv-access">Accès</label>
          <select id="srv-access" className="input" value={access} onChange={(e) => setAccess(e.target.value as 'private' | 'public')}>
            <option value="private">Privé : invitation nécessaire</option>
            <option value="public">Public : quiconque a l&apos;adresse peut entrer</option>
          </select>
        </div>
        <div className="field">
          <label htmlFor="srv-rules">Règles</label>
          <textarea id="srv-rules" className="input" rows={6} maxLength={4000} value={rules} onChange={(e) => setRules(e.target.value)}
            placeholder="Aucune règle : les membres entrent directement." />
          <span className="field-hint">Les nouveaux membres doivent les accepter avant de participer. Les membres présents sont considérés comme d&apos;accord.</span>
        </div>
        <label className="check-line">
          <input type="checkbox" checked={phone} disabled={!s.phone_verification && !phone} onChange={(e) => setPhone(e.target.checked)} />
          <span>Exiger un numéro de téléphone vérifié
            <span className="muted small" style={{ display: 'block' }}>{s.phone_verification ? 'Contre les comptes jetables : un numéro par membre.' : 'Indisponible : aucun fournisseur de SMS n’est configuré (page d’administration du serveur).'}</span></span>
        </label>
        <Alert kind="error">{a.error}</Alert>
        <Alert kind="info">{a.info}</Alert>
        <div><Submit busy={a.busy} className="btn btn-primary btn-sm" disabled={!changed || !name.trim()}>Enregistrer</Submit></div>
      </form>
    </>
  )
}

// --- roles ---

function ColorInput({ value, onChange }: { value: number; onChange: (c: number) => void }) {
  return (
    <div className="color-row">
      <input type="color" aria-label="Couleur du rôle" value={roleColor(value) ?? '#99aab5'} onChange={(e) => onChange(parseInt(e.target.value.slice(1), 16))} />
      <button type="button" className="btn btn-ghost btn-sm" disabled={!value} onClick={() => onChange(0)}>Sans couleur</button>
    </div>
  )
}

function PermissionList({ ready, value, onChange, disabled }: { ready: Ready; value: string[]; onChange: (p: string[]) => void; disabled: boolean }) {
  return (
    <div className="perm-groups">
      {permGroups.map((g) => (
        <fieldset key={g.title} className="perm-group">
          <legend>{g.title}</legend>
          {g.perms.map(([p, label]) => (
            <label key={p} className="check-line" title={mayGrant(ready, p) ? '' : 'Vous n’avez pas cette permission : vous ne pouvez pas la donner.'}>
              <input type="checkbox" checked={value.includes(p)} disabled={disabled || !mayGrant(ready, p)}
                onChange={(e) => onChange(e.target.checked ? [...value, p] : value.filter((x) => x !== p))} />
              <span>{label}</span>
            </label>
          ))}
        </fieldset>
      ))}
    </div>
  )
}

function Roles({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const roles = useMemo(() => [...ready.roles].sort((a, b) => b.position - a.position), [ready.roles])
  const [picked, setPicked] = useState<number>(() => roles.find((r) => r.id !== 1 && r.position < myTop(ready))?.id ?? 1)
  const role = roles.find((r) => r.id === picked) ?? roles[roles.length - 1]
  const a = useAction()
  const top = myTop(ready)
  return (
    <>
      <h2>Rôles</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>
        Les membres cumulent les permissions de leurs rôles. Un rôle plus haut dans la liste est plus puissant ; vous ne gérez que les rôles
        <b> sous votre rôle le plus haut</b>, et ne donnez que des permissions que vous avez.
      </p>
      <Alert kind="error">{a.error}</Alert>
      <div className="roles-layout">
        <div className="role-list" role="listbox" aria-label="Rôles">
          {roles.map((r) => (
            <button key={r.id} role="option" aria-selected={r.id === role.id} className={r.id === role.id ? 'active' : ''} onClick={() => setPicked(r.id)}>
              <span className="role-dot" style={{ background: roleColor(r.color) ?? 'var(--text-4)' }} />
              <span className="grow">{r.id === 1 ? '@everyone' : r.name}</span>
              <span className="muted small">{ready.members.filter((m) => m.roles.includes(r.id)).length || ''}</span>
            </button>
          ))}
          <button className="btn btn-ghost btn-sm" disabled={a.busy} onClick={() => a.run(async () => {
            const r = await conn.api((c) => c.createRole({ name: 'Nouveau rôle' }))
            setPicked(r.id)
          })}>+ Nouveau rôle</button>
        </div>
        <RoleEditor key={role.id} conn={conn} ready={ready} role={role} editable={role.id === 1 ? isAdmin(ready) || top > 0 : role.position < top}
          roles={roles} onDeleted={() => setPicked(1)} />
      </div>
    </>
  )
}

function RoleEditor({ conn, ready, role, editable, roles, onDeleted }: {
  conn: ServerConn; ready: Ready; role: Role; editable: boolean; roles: Role[]; onDeleted: () => void
}) {
  const everyone = role.id === 1
  const [name, setName] = useState(role.name)
  const [color, setColor] = useState(role.color)
  const [perms, setPerms] = useState(role.permissions)
  const [hoist, setHoist] = useState(role.hoist)
  const [mentionable, setMentionable] = useState(role.mentionable)
  const a = useAction()
  const changed = name !== role.name || color !== role.color || hoist !== role.hoist || mentionable !== role.mentionable ||
    perms.length !== role.permissions.length || perms.some((p) => !role.permissions.includes(p))
  // Moving: swap with the neighbour, within what I may manage.
  const idx = roles.findIndex((r) => r.id === role.id)
  const above = roles[idx - 1]
  const below = roles[idx + 1]
  const top = myTop(ready)
  const move = (to: number) => a.run(async () => void (await conn.api((c) => c.updateRole(role.id, { position: to }))))
  return (
    <form className="role-editor" onSubmit={(e) => {
      e.preventDefault()
      a.run(async () => {
        await conn.api((c) => c.updateRole(role.id, everyone ? { permissions: perms } : { name: name.trim(), color, permissions: perms, hoist, mentionable }))
        return 'Rôle enregistré.'
      })
    }}>
      {!editable && <Alert kind="warn">Ce rôle est au niveau du vôtre ou au-dessus : vous ne pouvez pas le modifier.</Alert>}
      {everyone ? (
        <p className="muted small">@everyone : permissions de base de tous les membres.</p>
      ) : (
        <>
          <Field label="Nom du rôle" value={name} maxLength={100} disabled={!editable} onChange={(e) => setName(e.target.value)} />
          <div className="field"><label>Couleur</label><ColorInput value={color} onChange={setColor} /></div>
          <label className="check-line"><input type="checkbox" checked={hoist} disabled={!editable} onChange={(e) => setHoist(e.target.checked)} />Afficher ses membres à part dans la liste</label>
          <label className="check-line"><input type="checkbox" checked={mentionable} disabled={!editable} onChange={(e) => setMentionable(e.target.checked)} />Tout le monde peut le mentionner</label>
        </>
      )}
      <PermissionList ready={ready} value={perms} onChange={setPerms} disabled={!editable} />
      <Alert kind="error">{a.error}</Alert>
      <Alert kind="info">{a.info}</Alert>
      <div className="dialog-actions" style={{ justifyContent: 'flex-start', flexWrap: 'wrap' }}>
        <Submit busy={a.busy} className="btn btn-primary btn-sm" disabled={!editable || !changed || (!everyone && !name.trim())}>Enregistrer</Submit>
        {!everyone && editable && (
          <>
            <button type="button" className="btn btn-ghost btn-sm" disabled={!above || above.position >= top} onClick={() => move(above.position)}>Monter</button>
            <button type="button" className="btn btn-ghost btn-sm" disabled={!below || below.id === 1} onClick={() => move(below.position)}>Descendre</button>
            <button type="button" className="btn btn-danger btn-sm" onClick={async () => {
              if (await confirmAction({ title: 'Supprimer le rôle « ' + role.name + ' » ?', message: 'Les membres qui l’ont le perdent.', confirm: 'Supprimer', danger: true })) a.run(async () => {
                await conn.api((c) => c.deleteRole(role.id))
                onDeleted()
              })
            }}>Supprimer</button>
          </>
        )}
      </div>
    </form>
  )
}

// --- channels ---

const typeLabels: Record<string, string> = { text: 'Textuel', voice: 'Vocal', announcement: 'Annonces', category: 'Catégorie', thread: 'Fil' }

function Channels({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const tree = useMemo(() => channelTree(ready.channels), [ready.channels])
  const [editing, setEditing] = useState<Channel | 'new' | null>(null)
  const canManage = canServer(ready, 'manage_channels')
  return (
    <>
      <h2>Salons</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>Chaque salon peut avoir ses propres droits, par rôle ou par membre, qui s&apos;ajoutent à ceux du serveur.</p>
      {canManage && <div><button className="btn btn-primary btn-sm" onClick={() => setEditing('new')}>Créer un salon</button></div>}
      <div className="card">
        {tree.map((g) => (
          <div key={g.category?.id ?? 0}>
            {g.category && (
              <div className="card-row channel-row cat">
                <span className="grow"><span className="title">{g.category.name.toUpperCase()}</span><span className="sub">Catégorie</span></span>
                <button className="btn btn-ghost btn-sm" onClick={() => setEditing(g.category)}>Modifier</button>
              </div>
            )}
            {g.items.map(({ channel: c }) => (
              <div className="card-row channel-row" key={c.id}>
                <span className="grow"><span className="title">{(c.type === 'voice' ? '🔊 ' : '# ') + c.name}</span><span className="sub">{typeLabels[c.type]}{c.topic ? ' · ' + c.topic : ''}</span></span>
                <button className="btn btn-ghost btn-sm" aria-label={'Modifier ' + c.name} onClick={() => setEditing(c)}>Modifier</button>
              </div>
            ))}
          </div>
        ))}
      </div>
      {editing && <ChannelDialog conn={conn} ready={ready} channel={editing === 'new' ? null : editing} onClose={() => setEditing(null)} />}
    </>
  )
}

// What a channel is, for titles: "Salon", "Catégorie", "Fil" or "Post" (a thread of a forum).
export function channelKind(ready: Ready, c: Channel) {
  if (c.type === 'category') return 'Catégorie'
  if (c.type !== 'thread') return 'Salon'
  return ready.channels.find((p) => p.id === c.parent_id)?.type === 'forum' ? 'Post' : 'Fil'
}

// Asks (in the app), then deletes a channel, a category, a thread or a forum post.
export async function askDeleteChannel(conn: ServerConn, ready: Ready, c: Channel): Promise<boolean> {
  const kind = channelKind(ready, c)
  const title = 'Supprimer ' + ({ Catégorie: 'la catégorie', Salon: 'le salon', Fil: 'le fil', Post: 'le post' } as Record<string, string>)[kind] + ' « ' + c.name + ' » ?'
  const message = c.type === 'category' ? 'Ses salons remontent à la racine.'
    : c.type === 'forum' ? 'Tous ses posts et leurs messages seront supprimés.'
      : c.type === 'thread' ? 'Tous ses messages seront supprimés.'
        : c.type === 'voice' ? 'Les personnes présentes seront déconnectées.' : 'Tous ses messages et ses fils seront supprimés.'
  if (!(await confirmAction({ title, message, confirm: 'Supprimer', danger: true }))) return false
  await conn.api((cl) => cl.deleteChannel(c.id))
  return true
}

export function ChannelDialog({ conn, ready, channel, onClose, parent }: {
  conn: ServerConn; ready: Ready; channel: Channel | null; onClose: () => void; parent?: number | null
}) {
  const [tab, setTab] = useState<'general' | 'perms' | 'hooks'>('general')
  const [type, setType] = useState(channel?.type ?? 'text')
  const [name, setName] = useState(channel?.name ?? '')
  const [topic, setTopic] = useState(channel?.topic ?? '')
  const [parentId, setParentId] = useState<number | null>(channel ? channel.parent_id : (parent ?? null))
  const [stage, setStage] = useState(!!channel?.stage)
  const a = useAction()
  const categories = ready.channels.filter((c) => c.type === 'category')
  const canManage = canServer(ready, 'manage_channels') || (channel ? ready.permissions.channels[String(channel.id)]?.includes('manage_channels') : false)
  const canPerms = canServer(ready, 'manage_roles')
  const canHooks = !!channel && (canServer(ready, 'manage_webhooks') || !!ready.permissions.channels[String(channel.id)]?.includes('manage_webhooks')) &&
    (channel.type === 'text' || channel.type === 'announcement')
  const thread = channel?.type === 'thread' // a thread or a forum post: its name only (its rights are its channel's)
  return (
    <Dialog title={channel ? channelKind(ready, channel) + ' ' + channel.name : 'Créer un salon'} onClose={onClose}>
      {channel && !thread && (canPerms || canHooks) && (
        <div className="tabs" role="tablist">
          <button role="tab" aria-selected={tab === 'general'} className={tab === 'general' ? 'active' : ''} onClick={() => setTab('general')}>Général</button>
          {canPerms && <button role="tab" aria-selected={tab === 'perms'} className={tab === 'perms' ? 'active' : ''} onClick={() => setTab('perms')}>Permissions</button>}
          {canHooks && <button role="tab" aria-selected={tab === 'hooks'} className={tab === 'hooks' ? 'active' : ''} onClick={() => setTab('hooks')}>Webhooks</button>}
        </div>
      )}
      {tab === 'perms' && channel ? (
        <OverridesEditor conn={conn} ready={ready} channel={channel} />
      ) : tab === 'hooks' && channel ? (
        <WebhooksEditor conn={conn} channel={channel} />
      ) : (
        <form style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={(e) => {
          e.preventDefault()
          a.run(async () => {
            if (channel && thread) await conn.api((c) => c.updateChannel(channel.id, { name: name.trim() }))
            else if (channel) await conn.api((c) => c.updateChannel(channel.id, { name: name.trim(), topic, ...(channel.type !== 'category' ? { parent_id: parentId } : {}), ...(channel.type === 'voice' ? { stage } : {}) }))
            else await conn.api((c) => c.createChannel({ type, name: name.trim(), topic, parent_id: type === 'category' ? null : parentId, ...(type === 'voice' ? { stage } : {}) }))
            onClose()
          })
        }}>
          {!channel && (
            <div className="field">
              <label htmlFor="ch-type">Type</label>
              <select id="ch-type" className="input" value={type} onChange={(e) => setType(e.target.value as Channel['type'])}>
                <option value="text">Textuel</option>
                <option value="voice">Vocal</option>
                <option value="announcement">Annonces (seuls les modérateurs écrivent)</option>
                <option value="forum">Forum (des posts avec un titre, chacun sa discussion)</option>
                <option value="category">Catégorie</option>
              </select>
            </div>
          )}
          <Field label="Nom" value={name} maxLength={100} autoFocus disabled={!canManage} onChange={(e) => setName(e.target.value)} />
          {type !== 'category' && type !== 'voice' && type !== 'thread' && (
            <Field label="Sujet (facultatif)" value={topic} maxLength={1024} disabled={!canManage} onChange={(e) => setTopic(e.target.value)} />
          )}
          {type !== 'category' && type !== 'thread' && (
            <div className="field">
              <label htmlFor="ch-parent">Catégorie</label>
              <select id="ch-parent" className="input" value={parentId ?? ''} disabled={!canManage} onChange={(e) => setParentId(e.target.value ? Number(e.target.value) : null)}>
                <option value="">Aucune</option>
                {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
            </div>
          )}
          {type === 'voice' && (
            <label className="check-line">
              <input type="checkbox" checked={stage} disabled={!canManage} onChange={(e) => setStage(e.target.checked)} />
              <span>Scène : seules les personnes invitées à parler ont la parole, les autres écoutent et peuvent lever la main</span>
            </label>
          )}
          <Alert kind="error">{a.error}</Alert>
          <div className="dialog-actions">
            {channel && canManage && (
              <button type="button" className="btn btn-danger btn-sm" style={{ marginRight: 'auto' }} onClick={async () => {
                a.run(async () => {
                  if (await askDeleteChannel(conn, ready, channel)) onClose()
                })
              }}>Supprimer</button>
            )}
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
            {canManage && <Submit busy={a.busy} className="btn btn-primary btn-sm" disabled={!name.trim()}>{channel ? 'Enregistrer' : 'Créer'}</Submit>}
          </div>
        </form>
      )}
    </Dialog>
  )
}

// Per-channel permissions: for a role or a member, each permission is allowed, denied or inherited.
function OverridesEditor({ conn, ready, channel }: { conn: ServerConn; ready: Ready; channel: Channel }) {
  const overrides = ready.channels.find((c) => c.id === channel.id)?.overrides ?? channel.overrides ?? []
  const [target, setTarget] = useState<string>(() => overrides[0] ? overrides[0].type + ':' + overrides[0].id : 'role:1')
  const [type, id] = target.split(':') as ['role' | 'member', string]
  const current = overrides.find((o) => o.type === type && o.id === id)
  const [allow, setAllow] = useState<string[]>(current?.allow ?? [])
  const [deny, setDeny] = useState<string[]>(current?.deny ?? [])
  useEffect(() => {
    setAllow(current?.allow ?? [])
    setDeny(current?.deny ?? [])
  }, [target]) // eslint-disable-line react-hooks/exhaustive-deps
  const a = useAction()
  const top = myTop(ready)
  const roleName = (rid: string) => (rid === '1' ? '@everyone' : ready.roles.find((r) => String(r.id) === rid)?.name ?? 'Rôle ' + rid)
  const memberName = (mid: string) => ready.members.find((m) => m.id === mid)?.display_name ?? 'Membre'
  const set = (p: string, v: 'allow' | 'inherit' | 'deny') => {
    setAllow((l) => (v === 'allow' ? [...l.filter((x) => x !== p), p] : l.filter((x) => x !== p)))
    setDeny((l) => (v === 'deny' ? [...l.filter((x) => x !== p), p] : l.filter((x) => x !== p)))
  }
  const editable = type === 'role' ? (ready.roles.find((r) => String(r.id) === id)?.position ?? 0) < top || isAdmin(ready)
    : outranks(ready, ready.members.find((m) => m.id === id) ?? ready.member)
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div className="field">
        <label htmlFor="ov-target">Pour</label>
        <select id="ov-target" className="input" value={target} onChange={(e) => setTarget(e.target.value)}>
          <optgroup label="Rôles">
            {[...ready.roles].sort((x, y) => y.position - x.position).map((r) => (
              <option key={r.id} value={'role:' + r.id}>{roleName(String(r.id))}{overrides.some((o) => o.type === 'role' && o.id === String(r.id)) ? ' •' : ''}</option>
            ))}
          </optgroup>
          <optgroup label="Membres">
            {ready.members.map((m) => (
              <option key={m.id} value={'member:' + m.id}>{memberName(m.id)}{overrides.some((o) => o.type === 'member' && o.id === m.id) ? ' •' : ''}</option>
            ))}
          </optgroup>
        </select>
        <span className="field-hint">• = a déjà des droits particuliers dans ce salon.</span>
      </div>
      <div className="ov-list">
        {permGroups.flatMap((g) => g.perms).filter(([p]) => channelPerms.has(p)).map(([p, label]) => {
          const v = allow.includes(p) ? 'allow' : deny.includes(p) ? 'deny' : 'inherit'
          return (
            <div className="ov-row" key={p}>
              <span className="grow">{label}</span>
              <div className="tri" role="radiogroup" aria-label={label}>
                <button type="button" role="radio" aria-checked={v === 'deny'} aria-label="Refuser" title="Refuser" className={'deny' + (v === 'deny' ? ' on' : '')}
                  disabled={!editable || !mayGrant(ready, p)} onClick={() => set(p, 'deny')}>✕</button>
                <button type="button" role="radio" aria-checked={v === 'inherit'} aria-label="Hériter" title="Hériter (réglage du serveur ou de la catégorie)" className={'inherit' + (v === 'inherit' ? ' on' : '')}
                  disabled={!editable || !mayGrant(ready, p)} onClick={() => set(p, 'inherit')}>/</button>
                <button type="button" role="radio" aria-checked={v === 'allow'} aria-label="Autoriser" title="Autoriser" className={'allow' + (v === 'allow' ? ' on' : '')}
                  disabled={!editable || !mayGrant(ready, p)} onClick={() => set(p, 'allow')}>✓</button>
              </div>
            </div>
          )
        })}
      </div>
      <Alert kind="error">{a.error}</Alert>
      <Alert kind="info">{a.info}</Alert>
      <div className="dialog-actions">
        {current && <button type="button" className="btn btn-ghost btn-sm" style={{ marginRight: 'auto' }} disabled={!editable} onClick={() => a.run(async () => {
          await conn.api((c) => c.deleteOverride(channel.id, type, id))
          return 'Droits particuliers retirés.'
        })}>Tout remettre par défaut</button>}
        <button type="button" className="btn btn-primary btn-sm" disabled={!editable || a.busy} onClick={() => a.run(async () => {
          if (!allow.length && !deny.length) await conn.api((c) => c.deleteOverride(channel.id, type, id)).catch(() => {})
          else await conn.api((c) => c.setOverride(channel.id, type, id, allow, deny))
          return 'Droits du salon enregistrés.'
        })}>Enregistrer</button>
      </div>
    </div>
  )
}

// --- members ---

const durations: [number, string][] = [[60, '1 minute'], [300, '5 minutes'], [600, '10 minutes'], [3600, '1 heure'], [86400, '1 jour'], [604800, '1 semaine']]

function Members({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState<Member | null>(null)
  const list = ready.members.filter((m) => !q || (m.display_name + ' ' + m.handle).toLowerCase().includes(q.toLowerCase()))
    .sort((x, y) => x.display_name.localeCompare(y.display_name, 'fr'))
  return (
    <>
      <h2>Membres</h2>
      <input className="input" placeholder="Rechercher un membre" aria-label="Rechercher un membre" value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="card">
        {list.map((m) => (
          <div className="card-row" key={m.id}>
            <Avatar id={m.subject || m.id} name={m.display_name} src={memberAvatar(m, conn)} size={32} />
            <div className="grow">
              <span className="title" style={{ color: memberColor(ready, m) }}>{m.display_name}{m.owner && ' 👑'}{m.bot && <span className="bot-tag">BOT</span>}</span>
              <span className="sub">{m.handle}{m.timeout_until && Date.parse(m.timeout_until) > Date.now() ? ' · exclu jusqu’au ' + dateTime.format(new Date(m.timeout_until)) : ''}</span>
            </div>
            <span className="role-chips">
              {ready.roles.filter((r) => m.roles.includes(r.id)).sort((a, b) => b.position - a.position).map((r) => (
                <span key={r.id} className="role-chip"><span className="role-dot" style={{ background: roleColor(r.color) ?? 'var(--text-4)' }} />{r.name}</span>
              ))}
            </span>
            <button className="btn btn-ghost btn-sm" aria-label={'Gérer ' + m.display_name} onClick={() => setOpen(m)}>Gérer</button>
          </div>
        ))}
      </div>
      {open && <MemberDialog conn={conn} ready={ready} member={open} onClose={() => setOpen(null)} />}
    </>
  )
}

// Roles and moderation of one member (also opened from the member list).
export function MemberDialog({ conn, ready, member, onClose }: { conn: ServerConn; ready: Ready; member: Member; onClose: () => void }) {
  const m = ready.members.find((x) => x.id === member.id) ?? member
  const a = useAction()
  const [confirmBan, setConfirmBan] = useState(false)
  const [pending, setPending] = useState<Record<number, boolean>>({})
  // Forget what the server confirmed.
  useEffect(() => {
    setPending((p) => Object.fromEntries(Object.entries(p).filter(([rid, on]) => m.roles.includes(Number(rid)) !== on)))
  }, [m.roles])
  const [reason, setReason] = useState('')
  const [purge, setPurge] = useState(0)
  const [duration, setDuration] = useState(600)
  const top = myTop(ready)
  const above = outranks(ready, m)
  const self = m.id === ready.member.id
  const timedOut = !!m.timeout_until && Date.parse(m.timeout_until) > Date.now()
  const assignable = [...ready.roles].filter((r) => r.id !== 1).sort((x, y) => y.position - x.position)
  const voice = ready.voice_states.find((v) => v.member_id === m.id)
  const inVoice = (p: string) => !!voice && can(ready, voice.channel_id, p)
  const voiceMute = inVoice('mute_members')
  const voiceDeaf = inVoice('deafen_members')
  const voiceMove = inVoice('move_members')
  return (
    <Dialog title={m.display_name} onClose={onClose}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <Avatar id={m.subject || m.id} name={m.display_name} src={memberAvatar(m, conn)} size={48} />
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <b>{m.handle}</b>
          <span className="muted small">Membre depuis le {dateTime.format(new Date(m.joined_at))}</span>
        </div>
      </div>
      {canServer(ready, 'manage_roles') && (
        <div className="field">
          <label>Rôles</label>
          <div className="checklist">
            {assignable.map((r) => (
              <label key={r.id} title={r.position >= top ? 'Rôle au niveau du vôtre ou au-dessus' : ''}>
                <input type="checkbox" checked={pending[r.id] ?? m.roles.includes(r.id)} disabled={a.busy || r.position >= top || (!above && !self)}
                  onChange={(e) => {
                    const on = e.target.checked
                    setPending((p) => ({ ...p, [r.id]: on })) // shown at once, confirmed by the server's MEMBER_UPDATE
                    a.run(async () => void (await conn.api((c) => c.setMemberRole(m.id, r.id, on)))).then((ok) => !ok && setPending((p) => {
                      const { [r.id]: _, ...rest } = p
                      return rest
                    }))
                  }} />
                <span className="role-dot" style={{ background: roleColor(r.color) ?? 'var(--text-4)' }} />{r.name}
              </label>
            ))}
            {!assignable.length && <span className="muted small">Aucun rôle : créez-en dans « Rôles ».</span>}
          </div>
        </div>
      )}
      {!self && !above && <Alert kind="info">{m.owner ? 'Le propriétaire est intouchable.' : 'Cette personne est au niveau de votre rôle ou au-dessus : pas de modération possible.'}</Alert>}
      {!self && above && (
        <>
          <Field label="Raison (facultative, visible dans le journal)" value={reason} maxLength={512} onChange={(e) => setReason(e.target.value)} />
          <div className="mod-actions">
            {canServer(ready, 'moderate_members') && (timedOut ? (
              <button className="btn btn-ghost btn-sm" disabled={a.busy} onClick={() => a.run(async () => {
                await conn.api((c) => c.removeTimeout(m.id))
                return 'Exclusion temporaire levée.'
              })}>Lever l&apos;exclusion</button>
            ) : (
              <span className="copy-row" style={{ gap: 6 }}>
                <select className="input" aria-label="Durée de l'exclusion" value={duration} onChange={(e) => setDuration(Number(e.target.value))} style={{ height: 34, padding: '0 8px' }}>
                  {durations.map(([s, l]) => <option key={s} value={s}>{l}</option>)}
                </select>
                <button className="btn btn-ghost btn-sm" disabled={a.busy} onClick={() => a.run(async () => {
                  await conn.api((c) => c.timeout(m.id, duration, reason || undefined))
                  return 'Exclu temporairement : lecture seule.'
                })}>Exclure temporairement</button>
              </span>
            ))}
            {canServer(ready, 'kick_members') && (
              <button className="btn btn-ghost btn-sm danger-text" disabled={a.busy} onClick={async () => {
                if (await confirmAction({ title: 'Expulser ' + m.display_name + ' ?', message: 'Cette personne pourra revenir avec une invitation.', confirm: 'Expulser', danger: true })) a.run(async () => {
                  await conn.api((c) => c.kick(m.id, reason || undefined))
                  onClose()
                })
              }}>Expulser</button>
            )}
            {canServer(ready, 'ban_members') && <button className="btn btn-danger btn-sm" disabled={a.busy} onClick={() => setConfirmBan(true)}>Bannir</button>}
          </div>
          {confirmBan && (
            <div className="ban-box">
              <p className="small" style={{ lineHeight: 1.5 }}>Bannir <b>{m.display_name}</b> : retiré·e du serveur, sans retour possible quelle que soit l&apos;invitation (jusqu&apos;à levée du bannissement).</p>
              <div className="field">
                <label htmlFor="ban-purge">Supprimer ses messages</label>
                <select id="ban-purge" className="input" value={purge} onChange={(e) => setPurge(Number(e.target.value))}>
                  <option value={0}>Aucun</option>
                  <option value={3600}>De la dernière heure</option>
                  <option value={86400}>Des dernières 24 heures</option>
                  <option value={604800}>Des 7 derniers jours</option>
                  <option value={-1}>Tous</option>
                </select>
              </div>
              <div className="dialog-actions">
                <button className="btn btn-ghost btn-sm" onClick={() => setConfirmBan(false)}>Annuler</button>
                <button className="btn btn-danger btn-sm" disabled={a.busy} onClick={() => a.run(async () => {
                  await conn.api((c) => c.ban(m.id, reason || undefined, purge))
                  onClose()
                })}>Bannir définitivement</button>
              </div>
            </div>
          )}
        </>
      )}
      {voice && (above || self) && (voiceMute || voiceDeaf || voiceMove) && (
        <div className="field">
          <label>Vocal (dans « {ready.channels.find((c) => c.id === voice.channel_id)?.name} »)</label>
          <div className="mod-actions">
            {voiceMute && <button className="btn btn-ghost btn-sm" disabled={a.busy} onClick={() => a.run(async () => void (await conn.api((c) => c.moderateVoice(m.id, { mute: !voice.server_mute }))))}>
              {voice.server_mute ? 'Rendre le micro' : 'Couper son micro'}</button>}
            {voiceDeaf && <button className="btn btn-ghost btn-sm" disabled={a.busy} onClick={() => a.run(async () => void (await conn.api((c) => c.moderateVoice(m.id, { deaf: !voice.server_deaf }))))}>
              {voice.server_deaf ? 'Rendre le son' : 'Mettre en sourdine'}</button>}
            {voiceMove && (
              <select className="input" aria-label="Déplacer vers" value="" style={{ height: 34, width: 'auto', padding: '0 8px' }}
                onChange={(e) => e.target.value && a.run(async () => void (await conn.api((c) => c.moderateVoice(m.id, { channel_id: Number(e.target.value) }))))}>
                <option value="">Déplacer vers…</option>
                {ready.channels.filter((c) => c.type === 'voice' && c.id !== voice.channel_id).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
            )}
            {voiceMove && <button className="btn btn-ghost btn-sm danger-text" disabled={a.busy} onClick={() => a.run(async () => void (await conn.api((c) => c.disconnectVoice(m.id))))}>Déconnecter</button>}
          </div>
        </div>
      )}
      {ready.member.owner && !self && !m.bot && (
        <div className="field">
          <label>Propriété du serveur</label>
          <div className="mod-actions">
            <button className="btn btn-ghost btn-sm danger-text" disabled={a.busy} onClick={async () => {
              if (await confirmAction({ title: 'Transmettre la propriété à ' + m.display_name + ' ?', message: 'Vous resterez membre, sans droits particuliers : seule cette personne pourra vous la rendre.', confirm: 'Transférer', danger: true })) a.run(async () => {
                await conn.api((c) => c.transferOwnership(m.id))
                return m.display_name + ' est maintenant propriétaire du serveur.'
              })
            }}>Transférer la propriété</button>
          </div>
        </div>
      )}
      <Alert kind="error">{a.error}</Alert>
      <Alert kind="info">{a.info}</Alert>
      <div className="dialog-actions"><button className="btn btn-ghost btn-sm" onClick={onClose}>Fermer</button></div>
    </Dialog>
  )
}

// --- invites ---

function Invites({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const [list, setList] = useState<Invite[] | null>(null)
  const [maxUses, setMaxUses] = useState(0)
  const [expires, setExpires] = useState(604800)
  const [copied, setCopied] = useState('')
  const a = useAction()
  const load = useCallback(() => {
    conn.api((c) => c.invites()).then(setList, () => {})
  }, [conn])
  useEffect(load, [load])
  const name = (id: string | null) => (id ? ready.members.find((m) => m.id === id)?.display_name ?? 'Ancien membre' : '—')
  const link = (code: string) => inviteLink(conn.saved.base, code, conn.saved.sid)
  const all = canServer(ready, 'manage_server')
  return (
    <>
      <h2>Invitations</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>{all ? 'Toutes les invitations du serveur.' : 'Vos invitations.'} Une invitation révoquée ne marche plus ; les membres déjà entrés restent.</p>
      {canServer(ready, 'create_invite') && (
        <div className="copy-row" style={{ flexWrap: 'wrap', gap: 8 }}>
          <select className="input" aria-label="Utilisations" value={maxUses} onChange={(e) => setMaxUses(Number(e.target.value))} style={{ width: 'auto' }}>
            <option value={0}>Utilisations illimitées</option>
            <option value={1}>1 utilisation</option>
            <option value={5}>5 utilisations</option>
            <option value={10}>10 utilisations</option>
            <option value={25}>25 utilisations</option>
          </select>
          <select className="input" aria-label="Validité" value={expires} onChange={(e) => setExpires(Number(e.target.value))} style={{ width: 'auto' }}>
            <option value={3600}>1 heure</option>
            <option value={86400}>1 jour</option>
            <option value={604800}>7 jours</option>
            <option value={2592000}>30 jours</option>
            <option value={0}>Sans limite</option>
          </select>
          <button className="btn btn-primary btn-sm" disabled={a.busy} onClick={() => a.run(async () => {
            const inv = await conn.api((c) => c.createInvite(maxUses, expires))
            await navigator.clipboard.writeText(link(inv.code)).catch(() => {})
            load()
            return 'Invitation créée, lien copié.'
          })}>Créer une invitation</button>
        </div>
      )}
      <Alert kind="error">{a.error}</Alert>
      <Alert kind="info">{a.info}</Alert>
      {list && list.length === 0 && <p className="muted small">Aucune invitation active.</p>}
      {list && list.length > 0 && (
        <div className="card" data-testid="invites">
          {list.map((i) => (
            <div className="card-row" key={i.code}>
              <div className="grow">
                <span className="title" style={{ fontFamily: 'ui-monospace, monospace' }}>{i.code}</span>
                <span className="sub">
                  Par {name(i.creator_id)} · {i.uses}{i.max_uses ? ' / ' + i.max_uses : ''} utilisation{i.uses > 1 ? 's' : ''} · {i.expires_at ? 'expire le ' + dateTime.format(new Date(i.expires_at)) : 'sans expiration'}
                </span>
              </div>
              <button className="btn btn-ghost btn-sm" onClick={() => navigator.clipboard.writeText(link(i.code)).then(() => setCopied(i.code))}>{copied === i.code ? 'Copié' : 'Copier le lien'}</button>
              <button className="btn btn-ghost btn-sm danger-text" onClick={() => a.run(async () => {
                await conn.api((c) => c.deleteInvite(i.code))
                load()
              })}>Révoquer</button>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

// --- bans ---

function Bans({ conn }: { conn: ServerConn; ready: Ready }) {
  const [list, setList] = useState<Ban[] | null>(null)
  const a = useAction()
  const load = useCallback(() => {
    conn.api((c) => c.bans()).then(setList, () => {})
  }, [conn])
  useEffect(load, [load])
  return (
    <>
      <h2>Bannissements</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>Un bannissement vise l&apos;identité de la personne : elle ne peut pas revenir, même avec une invitation ou sous un autre pseudo.</p>
      <Alert kind="error">{a.error}</Alert>
      {list && list.length === 0 && <p className="muted small">Personne n&apos;est banni.</p>}
      {list && list.length > 0 && (
        <div className="card" data-testid="bans">
          {list.map((b) => (
            <div className="card-row" key={b.member.id}>
              <Avatar id={b.member.subject || b.member.id} name={b.member.display_name} src={memberAvatar(b.member, conn)} size={32} />
              <div className="grow">
                <span className="title">{b.member.display_name}</span>
                <span className="sub">{b.member.handle} · le {dateTime.format(new Date(b.created_at))}{b.reason ? ' · ' + b.reason : ''}</span>
              </div>
              <button className="btn btn-ghost btn-sm" onClick={() => a.run(async () => {
                await conn.api((c) => c.unban(b.member.id))
                load()
              })}>Lever le bannissement</button>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

// --- incoming webhooks of a channel ---

function WebhooksEditor({ conn, channel }: { conn: ServerConn; channel: Channel }) {
  const [list, setList] = useState<Webhook[] | null>(null)
  const [name, setName] = useState('')
  const [created, setCreated] = useState<{ name: string; url: string } | null>(null)
  const [copied, setCopied] = useState(false)
  const a = useAction()
  const load = useCallback(() => {
    conn.api((c) => c.webhooks(channel.id)).then(setList, () => setList([]))
  }, [conn, channel.id])
  useEffect(load, [load])
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <p className="muted small" style={{ lineHeight: 1.5 }}>
        Une adresse secrète qui publie des messages dans ce salon (supervision, intégration continue, formulaires…) :
        <code> POST {'{"content": "…"}'}</code>. Qui connaît l&apos;adresse peut écrire ici.
      </p>
      <Alert kind="error">{a.error}</Alert>
      {created && (
        <div className="card" data-testid="webhook-created">
          <div className="card-row">
            <div className="grow">
              <span className="title">Adresse de « {created.name} » (affichée une seule fois)</span>
              <code className="sub" style={{ wordBreak: 'break-all' }}>{created.url}</code>
            </div>
            <button className="btn btn-primary btn-sm" onClick={() => navigator.clipboard.writeText(created.url).then(() => setCopied(true))}>{copied ? 'Copiée' : 'Copier'}</button>
          </div>
        </div>
      )}
      <form className="copy-row" onSubmit={(e) => {
        e.preventDefault()
        if (!name.trim()) return
        a.run(async () => {
          const h = await conn.api((c) => c.createWebhook(channel.id, name.trim()))
          setCreated({ name: h.name, url: conn.saved.base + '/v1/webhooks/' + h.id + '/' + h.token })
          setCopied(false)
          setName('')
          load()
        })
      }}>
        <input className="input" aria-label="Nom du webhook" placeholder="Nom (affiché sur ses messages)" maxLength={32} value={name} onChange={(e) => setName(e.target.value)} />
        <button className="btn btn-ghost btn-sm" style={{ height: 44 }} type="submit" disabled={a.busy}>Créer un webhook</button>
      </form>
      {list && list.length > 0 && (
        <div className="card" data-testid="webhooks">
          {list.map((h) => (
            <div className="card-row" key={h.id}>
              <div className="grow"><span className="title">{h.name}</span><span className="sub">créé le {dateTime.format(new Date(h.created_at))}</span></div>
              <button className="btn btn-ghost btn-sm" aria-label={'Supprimer ' + h.name} onClick={() => a.run(async () => {
                await conn.api((c) => c.deleteWebhook(h.id))
                load()
              })}>Supprimer</button>
            </div>
          ))}
        </div>
      )}
      {list && list.length === 0 && <p className="muted small">Aucun webhook dans ce salon.</p>}
    </div>
  )
}

// --- custom emojis ---

function EmojisSection({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const [name, setName] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const input = useRef<HTMLInputElement>(null)
  const a = useAction()
  const list = ready.emojis ?? []
  const names = { emoji: (id: string) => conn.emojiURL(id) }
  return (
    <>
      <h2>Emojis</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>
        Des images à utiliser dans les messages (<code>:nom:</code>) et les réactions. PNG, GIF ou WebP, 256 Ko au plus, 100 emojis par serveur ;
        noms de 2 à 32 caractères (a-z, 0-9, _).
      </p>
      <Alert kind="error">{a.error}</Alert>
      <form className="copy-row" onSubmit={(e) => {
        e.preventDefault()
        if (!file || !name) return
        a.run(async () => {
          await conn.api((c) => c.uploadEmoji(name, file))
          setName('')
          setFile(null)
          if (input.current) input.current.value = ''
        })
      }}>
        <input className="input" aria-label="Nom de l'emoji" placeholder="nom" maxLength={32} value={name}
          onChange={(e) => setName(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, '_'))} />
        <input ref={input} className="input" type="file" accept="image/png,image/gif,image/webp" aria-label="Image de l'emoji"
          onChange={(e) => {
            const f = e.target.files?.[0] ?? null
            setFile(f)
            if (f && !name) setName(f.name.replace(/\.[^.]+$/, '').toLowerCase().replace(/[^a-z0-9_]/g, '_').slice(0, 32))
          }} />
        <button className="btn btn-ghost btn-sm" style={{ height: 44 }} type="submit" disabled={a.busy || !file || name.length < 2}>Ajouter</button>
      </form>
      {list.length > 0 ? (
        <div className="card" data-testid="emojis">
          {list.map((e) => (
            <div className="card-row" key={e.id}>
              <CustomEmoji code={'<:' + e.name + ':' + e.id + '>'} names={names} />
              <span className="grow title">:{e.name}:</span>
              <button className="btn btn-ghost btn-sm" aria-label={'Supprimer :' + e.name + ':'} onClick={() => a.run(async () => {
                await conn.api((c) => c.deleteEmoji(e.id))
              })}>Supprimer</button>
            </div>
          ))}
        </div>
      ) : <p className="muted small">Aucun emoji pour l&apos;instant.</p>}
    </>
  )
}

// --- automatic moderation ---

const ruleLabels: Record<string, string> = { word: 'mot interdit', link: 'lien', mentions: 'trop de mentions', duplicate: 'message répété' }

const timeouts: [number, string][] = [[0, 'Jamais'], [60, '1 minute'], [300, '5 minutes'], [600, '10 minutes'], [3600, '1 heure'], [86400, '1 jour']]

function AutoModSection({ conn }: { conn: ServerConn }) {
  const [cfg, setCfg] = useState<AutoMod | null>(null)
  const [words, setWords] = useState('')
  const a = useAction()
  useEffect(() => {
    conn.api((c) => c.autoMod()).then((c) => {
      setCfg(c)
      setWords(c.words.join('\n'))
    }, () => {})
  }, [conn])
  if (!cfg) return <><h2>Modération automatique</h2><span className="spinner" /></>
  const set = (p: Partial<AutoMod>) => setCfg({ ...cfg, ...p })
  return (
    <>
      <h2>Modération automatique</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>
        Les messages qui enfreignent une règle sont refusés et notés dans le journal de modération (sans leur texte).
        Les personnes qui peuvent gérer les messages d&apos;un salon n&apos;y sont pas soumises.
      </p>
      <Alert kind="error">{a.error}</Alert>
      <Alert kind="info">{a.info}</Alert>
      <div className="field">
        <label htmlFor="automod-words">Mots interdits (un par ligne)</label>
        <textarea id="automod-words" className="input" rows={6} value={words} onChange={(e) => setWords(e.target.value)} />
        <span className="field-hint">Majuscules et accents ignorés, mots entiers. « arnaq* » : tous les mots qui commencent par « arnaq ». Plusieurs mots : cette suite exacte.</span>
      </div>
      <div className="card">
        <label className="check-line card-row">
          <input type="checkbox" checked={cfg.block_links} onChange={(e) => set({ block_links: e.target.checked })} />
          <span className="grow"><span className="title">Refuser les liens</span></span>
        </label>
        <label className="check-line card-row">
          <input type="checkbox" checked={cfg.duplicates} onChange={(e) => set({ duplicates: e.target.checked })} />
          <span className="grow"><span className="title">Refuser les messages répétés</span><span className="sub">Le même message une troisième fois en 30 secondes.</span></span>
        </label>
        <label className="check-line card-row">
          <span className="grow"><span className="title">Mentions par message</span><span className="sub">0 : pas de limite. @everyone compte pour une.</span></span>
          <input className="input" type="number" min={0} max={100} aria-label="Mentions par message" style={{ width: 90 }} value={cfg.max_mentions}
            onChange={(e) => set({ max_mentions: Math.max(0, Math.min(100, Number(e.target.value) || 0)) })} />
        </label>
        <label className="check-line card-row">
          <span className="grow"><span className="title">Exclusion automatique</span><span className="sub">Après 3 messages refusés en 10 minutes.</span></span>
          <select className="input" aria-label="Exclusion automatique" style={{ width: 'auto', height: 34, padding: '0 8px' }} value={cfg.timeout}
            onChange={(e) => set({ timeout: Number(e.target.value) })}>
            {timeouts.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
          </select>
        </label>
      </div>
      <div>
        <button className="btn btn-primary btn-sm" disabled={a.busy} onClick={() => a.run(async () => {
          const saved = await conn.api((c) => c.setAutoMod({ ...cfg, words: words.split('\n').map((w) => w.trim()).filter(Boolean) }))
          setCfg(saved)
          setWords(saved.words.join('\n'))
          return 'Règles enregistrées.'
        })}>Enregistrer</button>
      </div>
    </>
  )
}

// --- audit log ---

const actions: Record<string, string> = {
  member_kick: 'a expulsé', member_ban: 'a banni', member_unban: 'a levé le bannissement de', member_timeout: 'a exclu temporairement',
  member_timeout_remove: 'a levé l’exclusion de', member_role_add: 'a donné un rôle à', member_role_remove: 'a retiré un rôle à',
  messages_delete: 'a supprimé des messages de', role_create: 'a créé un rôle', role_update: 'a modifié un rôle', role_delete: 'a supprimé un rôle',
  channel_create: 'a créé un salon', channel_update: 'a modifié un salon', channel_delete: 'a supprimé un salon',
  override_update: 'a changé les droits d’un salon', override_delete: 'a retiré des droits particuliers d’un salon',
  server_update: 'a modifié le serveur', invite_delete: 'a révoqué une invitation', bot_create: 'a créé un bot', bot_delete: 'a supprimé un bot',
  bot_token_reset: 'a renouvelé le jeton d’un bot', voice_mute: 'a coupé le micro de', voice_deafen: 'a mis en sourdine',
  voice_move: 'a déplacé', voice_disconnect: 'a déconnecté du vocal',
  automod_block: 'a refusé un message de', emoji_create: 'a ajouté un emoji', emoji_delete: 'a supprimé un emoji', member_profile_reset: 'a réinitialisé le profil sur ce serveur de', server_theme: 'a modifié le thème du serveur', webhook_create: 'a créé un webhook', webhook_delete: 'a supprimé un webhook', owner_transfer: 'a transmis la propriété du serveur à', owner_reset: 'a retiré le propriétaire (nouveau lien propriétaire créé)',
}

function Audit({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const [list, setList] = useState<AuditEntry[]>([])
  const [done, setDone] = useState(false)
  const [error, setError] = useState('')
  const more = useCallback((before?: number) => {
    conn.api((c) => c.auditLog({ before })).then((page) => {
      setList((l) => (before ? [...l, ...page] : page))
      setDone(page.length < 50)
    }, (e) => setError(errorMessage(e)))
  }, [conn])
  useEffect(() => more(), [more])
  const who = (id: string | null, fallback?: string, e?: AuditEntry) => (id ? ready.members.find((m) => m.id === id)?.display_name ?? (fallback || 'ancien membre')
    : e && (e.action === 'automod_block' || e.reason === 'Modération automatique') ? 'Modération automatique' : 'L’hébergeur')
  const target = (e: AuditEntry) => {
    if (e.action.startsWith('webhook_') || e.action.startsWith('emoji_')) return '« ' + String(e.details?.name ?? '') + ' »'
    if (!e.target_id) return ''
    if (e.action === 'member_role_add' || e.action === 'member_role_remove') return who(e.target_id, e.target_name) + ' (« ' + String(e.details?.role ?? '') + ' »)'
    if (e.action === 'automod_block') return who(e.target_id, e.target_name) + ' (' + (ruleLabels[String(e.details?.rule)] ?? String(e.details?.rule ?? '')) + ')'
    if (e.action.startsWith('member_') || e.action.startsWith('voice_') || e.action === 'messages_delete' || e.action === 'owner_transfer') return who(e.target_id, e.target_name)
    if (e.action.startsWith('role_')) return '« ' + (ready.roles.find((r) => String(r.id) === e.target_id)?.name ?? String(e.details?.name ?? e.target_id)) + ' »'
    if (e.action.startsWith('channel_') || e.action.startsWith('override_')) return '« ' + (ready.channels.find((c) => String(c.id) === e.target_id)?.name ?? String(e.details?.name ?? e.target_id)) + ' »'
    return ''
  }
  return (
    <>
      <h2>Journal de modération</h2>
      <p className="muted small">Gardé 90 jours.</p>
      <Alert kind="error">{error}</Alert>
      <div className="card" data-testid="audit">
        {list.map((e) => (
          <div className="card-row" key={e.id}>
            <div className="grow">
              <span className="title"><b>{who(e.actor_id, e.actor_name, e)}</b> {actions[e.action] ?? e.action} {target(e)}</span>
              <span className="sub">{dateTime.format(new Date(e.created_at))}{e.reason ? ' · « ' + e.reason + ' »' : ''}</span>
            </div>
          </div>
        ))}
        {!list.length && <div className="card-row"><span className="muted small">Rien pour l&apos;instant.</span></div>}
      </div>
      {!done && list.length > 0 && <div><button className="btn btn-ghost btn-sm" onClick={() => more(list[list.length - 1].id)}>Plus ancien</button></div>}
    </>
  )
}

// --- bots ---

function Bots({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const [list, setList] = useState<Member[] | null>(null)
  const [name, setName] = useState('')
  const [token, setToken] = useState<{ name: string; token: string } | null>(null)
  const a = useAction()
  const load = useCallback(() => {
    conn.api((c) => c.bots()).then(setList, () => {})
  }, [conn])
  useEffect(load, [load])
  return (
    <>
      <h2>Bots</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>
        Un bot est un membre piloté par un programme (voir la documentation de l&apos;API). Son jeton donne toutes ses permissions :
        donnez-lui un rôle avec le strict nécessaire, et gardez le jeton secret.
      </p>
      <form className="copy-row" onSubmit={(e) => {
        e.preventDefault()
        if (!name.trim()) return
        a.run(async () => {
          const r = await conn.api((c) => c.createBot(name.trim()))
          setToken({ name: r.member.display_name, token: r.token })
          setName('')
          load()
        })
      }}>
        <input className="input" aria-label="Nom du bot" placeholder="Nom du bot" maxLength={32} value={name} onChange={(e) => setName(e.target.value)} />
        <button className="btn btn-primary btn-sm" style={{ height: 44 }} type="submit" disabled={a.busy}>Créer un bot</button>
      </form>
      <Alert kind="error">{a.error}</Alert>
      {list && list.length > 0 && (
        <div className="card" data-testid="bots">
          {list.map((b) => (
            <div className="card-row" key={b.id}>
              <Avatar id={b.id} name={b.display_name} size={32} />
              <div className="grow"><span className="title">{b.display_name} <span className="bot-tag">BOT</span></span><span className="sub">Créé le {dateTime.format(new Date(b.joined_at))}</span></div>
              <button className="btn btn-ghost btn-sm" disabled={!outranks(ready, b)} onClick={async () => {
                if (await confirmAction({ title: 'Nouveau jeton pour ' + b.display_name + ' ?', message: 'L’ancien cesse de marcher : le bot devra utiliser le nouveau.', confirm: 'Renouveler', danger: true })) a.run(async () => {
                  const r = await conn.api((c) => c.resetBotToken(b.id))
                  setToken({ name: b.display_name, token: r.token })
                })
              }}>Nouveau jeton</button>
              <button className="btn btn-ghost btn-sm danger-text" disabled={!outranks(ready, b)} onClick={async () => {
                if (await confirmAction({ title: 'Supprimer le bot ' + b.display_name + ' ?', message: 'Son jeton cesse de marcher ; ses messages restent.', confirm: 'Supprimer', danger: true })) a.run(async () => {
                  await conn.api((c) => c.deleteBot(b.id))
                  load()
                })
              }}>Supprimer</button>
            </div>
          ))}
        </div>
      )}
      {token && (
        <Dialog title={'Jeton de ' + token.name} onClose={() => setToken(null)}>
          <Alert kind="warn">Copiez-le maintenant : il ne sera plus affiché. Quiconque l&apos;a peut agir comme ce bot.</Alert>
          <div className="copy-row">
            <input className="input" readOnly value={token.token} aria-label="Jeton du bot" onFocus={(e) => e.target.select()} style={{ fontFamily: 'ui-monospace, monospace' }} />
            <button className="btn btn-primary btn-sm" style={{ height: 44 }} onClick={() => navigator.clipboard.writeText(token.token)}>Copier</button>
          </div>
          <div className="dialog-actions"><button className="btn btn-ghost btn-sm" onClick={() => setToken(null)}>Fermer</button></div>
        </Dialog>
      )}
    </>
  )
}
