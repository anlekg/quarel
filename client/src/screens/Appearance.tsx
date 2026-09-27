// Cosmetics block: my own CSS (Settings › Appearance), the theme editor used
// for a server, my identity profile card and my profile on a server.
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { Member, Ready } from '../api/community'
import type { Profile } from '../api/identity'
import { Alert, Dialog, Field } from '../components/ui'
import { ProfileCardView, useMemberCard } from '../components/ProfileCard'
import { errorMessage } from '../lib/errors'
import { imageLimits, prepareImage } from '../lib/image'
import { colorVar, filterCSS, isColor, parseTheme, themeFonts, type Theme, type ThemeColor, type ThemeFont } from '../lib/themecss'
import { identityClient, type Account } from '../state/account'
import type { ServerConn } from '../state/servers'
import { serverThemeIgnored, setServerThemeIgnored, setThemePref, useThemePrefs } from '../state/themes'

function useBusy() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [info, setInfo] = useState('')
  const run = async (fn: () => Promise<string | void>) => {
    setBusy(true), setError(''), setInfo('')
    try {
      setInfo((await fn()) || '')
    } catch (e) {
      setError(e instanceof Error && e.message === 'image' ? 'Cette image ne peut pas être lue.' : errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return { busy, error, info, run }
}

// --- my CSS ---

const example = `/* Exemple : des messages en bulles et un accent orange */
:root { --accent: #f08a4b; --accent-text: #f6b58c; }
.message { border-radius: 10px; }
`

export function AppearanceSection() {
  const p = useThemePrefs()
  const [css, setCSS] = useState(p.myCSS)
  const check = (label: string, sub: string, on: boolean, set: (v: boolean) => void) => (
    <label className="check-line card-row">
      <input type="checkbox" checked={on} onChange={(e) => set(e.target.checked)} />
      <span className="grow"><span className="title">{label}</span><span className="sub">{sub}</span></span>
    </label>
  )
  return (
    <>
      <h2>Apparence</h2>
      <div className="card">
        {check('Afficher les thèmes des serveurs et des profils', 'Couleurs, images et CSS choisis par les serveurs et par les personnes, sur leurs salons et leurs cartes de profil.',
          p.others, (v) => setThemePref('others', v))}
        {check('Le thème d’un serveur passe avant mon CSS', 'Décoché : votre CSS l’emporte, et les couleurs qu’il définit restent les vôtres même sur un serveur à thème.',
          p.serverFirst, (v) => setThemePref('serverFirst', v))}
        {check('Animations des thèmes', 'Décoché : aucune animation ni transition venant d’un thème.', p.animations, (v) => setThemePref('animations', v))}
      </div>

      <h3 className="settings-sub">Mon CSS</h3>
      <p className="muted" style={{ lineHeight: 1.5 }}>
        Appliqué à toute l’application, <b>sur cet appareil seulement</b>, sans aucun filtre : n’y collez que du CSS que vous comprenez.
        Les couleurs de l’application sont des variables (<code>--bg</code>, <code>--bg-1</code>, <code>--bg-2</code>, <code>--text</code>, <code>--accent</code>…).
      </p>
      <p className="muted" style={{ lineHeight: 1.5 }}>
        L’application devient inutilisable ? <b>Ctrl + Maj + 0</b> coupe tous les CSS personnalisés jusqu’au prochain lancement
        (ou lancez-la avec <code>--safe-mode</code> ; dans le navigateur, ajoutez <code>?safe</code> à l’adresse).
      </p>
      <div className="field">
        <label htmlFor="my-css">CSS</label>
        <textarea id="my-css" className="input css-code" rows={12} spellCheck={false} value={css} onChange={(e) => setCSS(e.target.value)} placeholder={example} />
      </div>
      <div style={{ display: 'flex', gap: 8 }}>
        <button className="btn btn-primary btn-sm" disabled={css === p.myCSS} onClick={() => setThemePref('myCSS', css)}>Appliquer</button>
        {!css && <button className="btn btn-ghost btn-sm" onClick={() => setCSS(example)}>Exemple</button>}
        {p.myCSS && <button className="btn btn-ghost btn-sm" onClick={() => { setCSS(''); setThemePref('myCSS', '') }}>Retirer mon CSS</button>}
      </div>
      {p.safe && <Alert kind="warn">Mode sans échec : votre CSS n’est pas appliqué pour l’instant.</Alert>}
    </>
  )
}

// --- the theme editor ---

const colorLabels: [ThemeColor, string][] = [
  ['bg', 'Fond principal'], ['bg_1', 'Fond des colonnes'], ['bg_2', 'Fond des cartes'], ['bg_3', 'Fond des champs et boutons'],
  ['line', 'Bordures'], ['text', 'Texte'], ['text_3', 'Texte discret'], ['accent', 'Accent'], ['accent_text', 'Liens et accent sur fond sombre'],
]
const fontLabels: Record<ThemeFont, string> = { manrope: 'Manrope (celle de l’application)', 'space-grotesk': 'Space Grotesk', system: 'Police du système', serif: 'Avec empattements', mono: 'Chasse fixe' }
const shortHex = (v: string) => (v.length === 4 ? '#' + [...v.slice(1)].map((c) => c + c).join('') : v.slice(0, 7))
// The app's own value of a colour, shown while the theme keeps the default.
function appColor(c: ThemeColor) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(colorVar(c)).trim()
  return isColor(v) ? shortHex(v) : '#000000'
}

export function ThemeEditor({ value, onChange, what }: { value: Theme; onChange: (t: Theme) => void; what: string }) {
  const dropped = useMemo(() => filterCSS(value.css ?? '', '[data-x]', 'x').dropped, [value.css])
  const setColor = (c: ThemeColor, v: string | null) => {
    const colors = { ...value.colors }
    if (v) colors[c] = v
    else delete colors[c]
    onChange({ ...value, colors })
  }
  const g = value.gradient
  return (
    <div className="theme-editor">
      <div className="theme-colors">
        {colorLabels.map(([c, label]) => (
          <label key={c} className="theme-color">
            <input type="color" aria-label={label} value={isColor(value.colors?.[c]) ? shortHex(value.colors![c]!) : appColor(c)} onChange={(e) => setColor(c, e.target.value)} />
            <span className="grow">{label}</span>
            {value.colors?.[c] ? <button type="button" className="link small" onClick={() => setColor(c, null)}>par défaut</button> : <span className="muted small">par défaut</span>}
          </label>
        ))}
      </div>
      <label className="check-line">
        <input type="checkbox" checked={!!g} onChange={(e) => onChange({ ...value, gradient: e.target.checked ? { angle: 160, stops: ['#1e3a34', '#16191f'] } : undefined })} />
        Dégradé en fond
      </label>
      {g && (
        <div className="theme-gradient">
          <input type="color" aria-label="Couleur de départ du dégradé" value={shortHex(g.stops[0])} onChange={(e) => onChange({ ...value, gradient: { ...g, stops: [e.target.value, g.stops[1]] } })} />
          <input type="color" aria-label="Couleur d’arrivée du dégradé" value={shortHex(g.stops[1])} onChange={(e) => onChange({ ...value, gradient: { ...g, stops: [g.stops[0], e.target.value] } })} />
          <input type="range" min={0} max={360} value={g.angle} aria-label="Angle du dégradé" onChange={(e) => onChange({ ...value, gradient: { ...g, angle: Number(e.target.value) } })} />
          <span className="muted small">{g.angle}°</span>
        </div>
      )}
      <div className="field">
        <label htmlFor="theme-font">Police</label>
        <select id="theme-font" className="input" value={value.font ?? ''} onChange={(e) => onChange({ ...value, font: (e.target.value || undefined) as ThemeFont | undefined })}>
          <option value="">Par défaut</option>
          {(Object.keys(themeFonts) as ThemeFont[]).map((f) => <option key={f} value={f}>{fontLabels[f]}</option>)}
        </select>
      </div>
      <div className="field">
        <label htmlFor="theme-css">CSS ({what})</label>
        <textarea id="theme-css" className="input css-code" rows={8} spellCheck={false} value={value.css ?? ''} onChange={(e) => onChange({ ...value, css: e.target.value })}
          placeholder={'.message { border-radius: 10px; }'} />
        <span className="field-hint">
          Filtré par chaque application qui l’affiche : ni image extérieure (<code>url()</code>), ni <code>@import</code>, ni texte ajouté (<code>content</code>),
          ni position fixe, et rien hors {what}. 16 Ko au plus.
        </span>
      </div>
      {dropped.length > 0 && (
        <Alert kind="warn">Sera ignoré : {dropped.join(' ; ')}.</Alert>
      )}
    </div>
  )
}

// What the server keeps of a theme draft ({} removes it).
function cleaned(t: Theme): Theme {
  const out: Theme = {}
  if (t.colors && Object.keys(t.colors).length) out.colors = t.colors
  if (t.gradient) out.gradient = t.gradient
  if (t.font) out.font = t.font
  if (t.css?.trim()) out.css = t.css
  return out
}

function ImagePicker({ label, has, busy, onPick, onRemove, hint }: { label: string; has: boolean; busy: boolean; onPick: (f: File) => void; onRemove: () => void; hint: string }) {
  const input = useRef<HTMLInputElement>(null)
  return (
    <div className="image-picker">
      <span className="grow"><span className="title">{label}</span><span className="sub muted small">{hint}</span></span>
      <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={() => input.current?.click()}>{has ? 'Changer' : 'Choisir'}</button>
      {has && <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={onRemove}>Retirer</button>}
      <input ref={input} type="file" hidden accept="image/png,image/jpeg,image/gif,image/webp" aria-label={label} onChange={(e) => {
        const f = e.target.files?.[0]
        e.target.value = ''
        if (f) onPick(f)
      }} />
    </div>
  )
}

// --- a server's theme (Server settings › Appearance) ---

export function ServerAppearanceSection({ conn, ready }: { conn: ServerConn; ready: Ready }) {
  const [draft, setDraft] = useState<Theme>(() => parseTheme(ready.theme?.theme) ?? {})
  const a = useBusy()
  const bg = useBusy()
  const hasBackground = !!ready.theme?.background_v
  return (
    <>
      <h2>Apparence</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>
        Le thème du serveur s’applique à ses salons, ses messages, la liste des membres et le vocal, chez chaque membre qui ne l’a pas désactivé.
        Fermez les paramètres pour voir le résultat.
      </p>
      <div className="card" style={{ padding: 14 }}>
        <ImagePicker label="Image de fond" has={hasBackground} busy={bg.busy} hint="PNG, JPEG, GIF ou WebP ; les grandes images sont réduites."
          onPick={(f) => bg.run(async () => { await conn.api(async (c) => c.setBackground(await prepareImage(f, ...imageLimits.background))) })}
          onRemove={() => bg.run(async () => { await conn.api((c) => c.setBackground(null)) })} />
      </div>
      <Alert kind="error">{bg.error}</Alert>
      <ThemeEditor value={draft} onChange={setDraft} what="la zone du serveur" />
      <div style={{ display: 'flex', gap: 8 }}>
        <button className="btn btn-primary btn-sm" disabled={a.busy} onClick={() => a.run(async () => {
          await conn.api((c) => c.setTheme(cleaned(draft)))
          return 'Thème enregistré.'
        })}>Enregistrer le thème</button>
        <button className="btn btn-ghost btn-sm" disabled={a.busy} onClick={() => a.run(async () => {
          setDraft({})
          await conn.api((c) => c.setTheme({}))
          return 'Thème retiré.'
        })}>Retirer le thème</button>
      </div>
      <Alert kind="error">{a.error}</Alert>
      <Alert kind="info">{a.info}</Alert>
    </>
  )
}

// --- my identity profile card (Settings › Profile) ---

export function IdentityThemeSection({ account, profile, onProfile }: { account: Account; profile: Profile | null; onProfile: (p: Profile) => void }) {
  const [draft, setDraft] = useState<Theme>(() => parseTheme(profile?.theme) ?? {})
  const [loaded, setLoaded] = useState(!!profile)
  useEffect(() => {
    if (profile && !loaded) setDraft(parseTheme(profile.theme) ?? {}), setLoaded(true)
  }, [profile, loaded])
  const a = useBusy()
  const img = useBusy()
  const u = account.user
  const api = identityClient(account)
  return (
    <>
      <h3 className="settings-sub">Carte de profil</h3>
      <p className="muted" style={{ lineHeight: 1.5 }}>Ce que voient les personnes qui cliquent sur vous, partout où vous n’avez pas de profil propre au serveur.</p>
      <div className="theme-with-preview">
        <div className="grow">
          <div className="card" style={{ padding: 14 }}>
            <ImagePicker label="Bannière" has={!!profile?.banner_url} busy={img.busy} hint="Au-dessus de votre avatar ; les grandes images sont réduites."
              onPick={(f) => img.run(async () => onProfile(await api.setBanner(await prepareImage(f, ...imageLimits.banner))))}
              onRemove={() => img.run(async () => { await api.deleteBanner(); if (profile) onProfile({ ...profile, banner_url: null }) })} />
          </div>
          <Alert kind="error">{img.error}</Alert>
          {loaded ? <ThemeEditor value={draft} onChange={setDraft} what="votre carte" /> : <span className="spinner" />}
        </div>
        <ProfileCardView preview view={{
          id: u.id, name: u.pseudo, handle: u.handle, bio: profile?.bio,
          avatar: profile?.avatar_url ? account.identity + profile.avatar_url : undefined,
          banner: profile?.banner_url ? account.identity + profile.banner_url : undefined, theme: draft,
        }} />
      </div>
      <div style={{ display: 'flex', gap: 8 }}>
        <button className="btn btn-primary btn-sm" disabled={a.busy} onClick={() => a.run(async () => {
          onProfile(await api.updateTheme(cleaned(draft)))
          return 'Carte enregistrée.'
        })}>Enregistrer la carte</button>
      </div>
      <Alert kind="error">{a.error}</Alert>
      <Alert kind="info">{a.info}</Alert>
    </>
  )
}

// --- my profile on a server ---

export function ServerProfileDialog({ conn, ready, onClose }: { conn: ServerConn; ready: Ready; onClose: () => void }) {
  const me = ready.members.find((m) => m.id === ready.member.id) ?? ready.member
  const [nickname, setNickname] = useState(me.nickname ?? '')
  const [bio, setBio] = useState('')
  const [draft, setDraft] = useState<Theme>({})
  const [loaded, setLoaded] = useState(false)
  useEffect(() => {
    conn.api((c) => c.memberProfile(me.id)).then((p) => {
      setBio(p.bio)
      setDraft(parseTheme(p.theme) ?? {})
      setLoaded(true)
    }, () => setLoaded(true))
  }, [conn, me.id])
  const a = useBusy()
  const img = useBusy()
  const pick = (kind: 'avatar' | 'banner') => (f: File) => img.run(async () => {
    const [max, side] = imageLimits[kind]
    const blob = await prepareImage(f, max, side)
    await conn.api(async (c) => { await c.setMyImage(kind, blob) })
  })
  const remove = (kind: 'avatar' | 'banner') => () => img.run(async () => { await conn.api(async (c) => { await c.setMyImage(kind, null) }) })
  const preview: Member = { ...me, nickname: nickname.trim() || undefined, display_name: nickname.trim() || me.display_name }
  return (
    <Dialog title={'Mon profil sur ' + ready.server.name} onClose={onClose} wide>
      <p className="muted" style={{ lineHeight: 1.5, marginTop: -6 }}>
        Seulement sur ce serveur. Ce que vous laissez vide reprend votre profil habituel.
      </p>
      <div className="theme-with-preview">
        <div className="grow" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Field label="Surnom sur ce serveur" value={nickname} maxLength={32} disabled={!loaded} onChange={(e) => setNickname(e.target.value)} />
          <div className="field">
            <label htmlFor="server-bio">Présentation sur ce serveur</label>
            <textarea id="server-bio" className="input" rows={3} maxLength={500} value={bio} disabled={!loaded} onChange={(e) => setBio(e.target.value)} />
          </div>
          <div className="card" style={{ padding: 14, display: 'flex', flexDirection: 'column', gap: 10 }}>
            <ImagePicker label="Image de profil" has={!!me.avatar_v} busy={img.busy} hint="Remplace votre image sur ce serveur." onPick={pick('avatar')} onRemove={remove('avatar')} />
            <ImagePicker label="Bannière" has={!!me.banner_v} busy={img.busy} hint="En haut de votre carte de profil." onPick={pick('banner')} onRemove={remove('banner')} />
          </div>
          <Alert kind="error">{img.error}</Alert>
          {loaded ? <ThemeEditor value={draft} onChange={setDraft} what="votre carte" /> : <span className="spinner" />}
        </div>
        <PreviewCard conn={conn} ready={ready} member={preview} bio={bio} theme={draft} />
      </div>
      <Alert kind="error">{a.error}</Alert>
      <Alert kind="info">{a.info}</Alert>
      <div className="dialog-actions">
        <button className="btn btn-ghost btn-sm" onClick={onClose}>Fermer</button>
        <button className="btn btn-primary btn-sm" disabled={a.busy || !loaded} onClick={() => a.run(async () => {
          await conn.api((c) => c.updateMe({ nickname: nickname.trim(), bio: bio.trim(), theme: cleaned(draft) }))
          return 'Profil enregistré.'
        })}>Enregistrer</button>
      </div>
    </Dialog>
  )
}

function PreviewCard({ conn, ready, member, bio, theme }: { conn: ServerConn; ready: Ready; member: Member; bio: string; theme: Theme }): ReactNode {
  const view = useMemberCard(conn, ready, member)
  return <ProfileCardView preview view={{ ...view, bio: bio.trim() || view.bio, theme }} />
}

// The server's menu: this server's theme on or off for me.
export function ignoreServerThemeItem(sid: string) {
  const ignored = serverThemeIgnored(sid)
  return { label: ignored ? 'Afficher le thème de ce serveur' : 'Ignorer le thème de ce serveur', run: () => setServerThemeIgnored(sid, !ignored) }
}
