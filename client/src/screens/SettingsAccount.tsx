// Settings of the account: profile, security (email, password, two-factor
// authentication, deletion) and privacy (typing, read receipts, blocked people).
import { useEffect, useRef, useState } from 'react'
import qrcode from 'qrcode-generator'
import type { Passkey, Privacy, Profile, PublicUser } from '../api/identity'
import { ApiError } from '../api/http'
import { Avatar, bumpAvatar } from '../components/Avatar'
import { Alert, Dialog, Field, PasswordField, Submit } from '../components/ui'
import { errorMessage } from '../lib/errors'
import { identityClient, signOut, updateUser, type Account } from '../state/account'
import { refreshBlocks } from '../state/social'

const api = (a: Account) => identityClient(a)

// Wrong password when re-checking it: the login message would be misleading here.
function secMessage(e: unknown) {
  if (e instanceof ApiError && e.code === 'invalid_credentials') return 'Mot de passe incorrect.'
  return errorMessage(e)
}

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
      setError(secMessage(e))
      return false
    } finally {
      setBusy(false)
    }
  }
  return { busy, error, info, run, setError }
}

// --- profile ---

// Images larger than the service accepts (1 MB) or than useful are reduced here.
async function prepareAvatar(file: File): Promise<Blob> {
  const bitmap = await createImageBitmap(file).catch(() => null)
  if (!bitmap) throw new Error('image')
  if (file.size <= 1 << 20 && bitmap.width <= 1024 && bitmap.height <= 1024) return file
  const side = Math.min(512, Math.max(bitmap.width, bitmap.height))
  const scale = side / Math.max(bitmap.width, bitmap.height)
  const canvas = document.createElement('canvas')
  canvas.width = Math.round(bitmap.width * scale)
  canvas.height = Math.round(bitmap.height * scale)
  canvas.getContext('2d')!.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
  return new Promise((resolve, reject) => canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('image'))), 'image/webp', 0.9))
}

export function ProfileSection({ account }: { account: Account }) {
  const u = account.user
  const [profile, setProfile] = useState<Profile | null>(null)
  const [pseudo, setPseudo] = useState(u.pseudo)
  const [bio, setBio] = useState('')
  const pic = useAction()
  const name = useAction()
  const about = useAction()
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    api(account).profile(u.id).then((p) => {
      setProfile(p)
      setBio(p.bio)
    }, () => {})
  }, [account, u.id])

  const avatarSrc = profile?.avatar_url ? account.identity + '/v1/users/' + u.id + '/avatar' : undefined

  return (
    <>
      <h2>Profil</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>Visible par vos amis et par les membres des serveurs que vous rejoignez.</p>
      <div className="card" style={{ padding: 16, display: 'flex', gap: 16, alignItems: 'center' }}>
        <Avatar id={u.id} name={u.pseudo} src={avatarSrc} size={80} />
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn btn-primary btn-sm" disabled={pic.busy} onClick={() => input.current?.click()}>Changer l&apos;image</button>
            {profile?.avatar_url && (
              <button className="btn btn-ghost btn-sm" disabled={pic.busy} onClick={() => pic.run(async () => {
                await api(account).deleteAvatar()
                setProfile({ ...profile, avatar_url: null })
                bumpAvatar(u.id)
              })}>Retirer</button>
            )}
          </div>
          <span className="muted small">PNG, JPEG, GIF ou WebP. Les grandes images sont réduites.</span>
          <input ref={input} type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden aria-label="Image de profil" onChange={(e) => {
            const f = e.target.files?.[0]
            e.target.value = ''
            if (!f) return
            pic.run(async () => {
              let img: Blob
              try {
                img = await prepareAvatar(f)
              } catch {
                throw new ApiError(400, 'invalid_image', '')
              }
              setProfile(await api(account).setAvatar(img))
              bumpAvatar(u.id)
            })
          }} />
        </div>
      </div>
      <Alert kind="error">{pic.error}</Alert>

      <form className="settings-form" onSubmit={(e) => {
        e.preventDefault()
        name.run(async () => {
          await updateUser(await api(account).changePseudo(pseudo.trim()))
          return 'Pseudo changé. Les serveurs le mettront à jour à votre prochaine connexion.'
        })
      }}>
        <Field label="Pseudo" value={pseudo} onChange={(e) => setPseudo(e.target.value)} maxLength={32}
          hint={'Identifiant complet : ' + u.handle + '. Modifiable une fois par jour ; vos amis et serveurs vous gardent.'} />
        <Alert kind="error">{name.error}</Alert>
        <Alert kind="info">{name.info}</Alert>
        <div><Submit busy={name.busy} className="btn btn-primary btn-sm" disabled={pseudo.trim() === u.pseudo || !pseudo.trim()}>Enregistrer le pseudo</Submit></div>
      </form>

      <form className="settings-form" onSubmit={(e) => {
        e.preventDefault()
        about.run(async () => {
          setProfile(await api(account).updateBio(bio))
          return 'Présentation enregistrée.'
        })
      }}>
        <div className="field">
          <label htmlFor="bio">À propos de moi</label>
          <textarea id="bio" className="input" rows={4} maxLength={500} value={bio} onChange={(e) => setBio(e.target.value)} />
          <span className="field-hint">{bio.length} / 500</span>
        </div>
        <Alert kind="error">{about.error}</Alert>
        <Alert kind="info">{about.info}</Alert>
        <div><Submit busy={about.busy} className="btn btn-primary btn-sm" disabled={!profile || bio === profile.bio}>Enregistrer la présentation</Submit></div>
      </form>
    </>
  )
}

// --- security ---

export function SecuritySection({ account }: { account: Account }) {
  const u = account.user
  const [dialog, setDialog] = useState<'email' | 'password' | '2fa-on' | '2fa-off' | 'delete' | null>(null)
  const [done, setDone] = useState('')
  const close = (msg = '') => {
    setDialog(null)
    setDone(msg)
  }
  return (
    <>
      <h2>Sécurité</h2>
      <Alert kind="info">{done}</Alert>
      <div className="card">
        <div className="card-row">
          <div className="grow"><span className="sub">Email</span><span className="title">{u.email}</span></div>
          <button className="btn btn-ghost btn-sm" onClick={() => setDialog('email')}>Modifier</button>
        </div>
        <div className="card-row">
          <div className="grow"><span className="sub">Mot de passe</span><span className="title">••••••••••</span></div>
          <button className="btn btn-ghost btn-sm" onClick={() => setDialog('password')}>Modifier</button>
        </div>
        <div className="card-row">
          <div className="grow">
            <span className="sub">Double authentification (application TOTP)</span>
            <span className="title">{u.totp_enabled ? 'Activée' : 'Désactivée'}</span>
          </div>
          {u.totp_enabled
            ? <button className="btn btn-ghost btn-sm" onClick={() => setDialog('2fa-off')}>Désactiver</button>
            : <button className="btn btn-primary btn-sm" onClick={() => setDialog('2fa-on')}>Activer</button>}
        </div>
      </div>
      {u.totp_enabled && <Passkeys account={account} />}
      <h3 className="settings-sub danger">Zone sensible</h3>
      <div className="card">
        <div className="card-row">
          <div className="grow">
            <span className="title">Supprimer mon compte</span>
            <span className="sub">Immédiat et définitif : amis, conversations, clés et sauvegarde sont effacés.</span>
          </div>
          <button className="btn btn-danger btn-sm" onClick={() => setDialog('delete')}>Supprimer</button>
        </div>
      </div>
      {dialog === 'email' && <EmailDialog account={account} onClose={close} />}
      {dialog === 'password' && <PasswordDialog account={account} onClose={close} />}
      {dialog === '2fa-on' && <Enable2FADialog account={account} onClose={close} />}
      {dialog === '2fa-off' && <Disable2FADialog account={account} onClose={close} />}
      {dialog === 'delete' && <DeleteDialog account={account} onClose={() => setDialog(null)} />}
    </>
  )
}

function EmailDialog({ account, onClose }: { account: Account; onClose: (msg?: string) => void }) {
  const [step, setStep] = useState<'ask' | 'code'>('ask')
  const [password, setPassword] = useState('')
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const a = useAction()
  return (
    <Dialog title="Changer d'email" onClose={() => onClose()}>
      <form style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={(e) => {
        e.preventDefault()
        if (step === 'ask') a.run(async () => {
          await api(account).changeEmail(password, email.trim())
          setStep('code')
        })
        else a.run(async () => {
          await updateUser(await api(account).confirmEmail(code.trim()))
          onClose('Adresse email changée. L’ancienne adresse a été prévenue.')
        })
      }}>
        {step === 'ask' ? (
          <>
            <Field label="Nouvelle adresse email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus autoComplete="email" />
            <PasswordField label="Mot de passe actuel" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
          </>
        ) : (
          <>
            <p className="muted small">Un code a été envoyé à <b>{email}</b>.</p>
            <Field label="Code reçu par email" value={code} onChange={(e) => setCode(e.target.value)} autoFocus inputMode="numeric" autoComplete="one-time-code" />
          </>
        )}
        <Alert kind="error">{a.error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => onClose()}>Annuler</button>
          <Submit busy={a.busy} className="btn btn-primary btn-sm">{step === 'ask' ? 'Envoyer un code' : 'Confirmer'}</Submit>
        </div>
      </form>
    </Dialog>
  )
}

function PasswordDialog({ account, onClose }: { account: Account; onClose: (msg?: string) => void }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const a = useAction()
  return (
    <Dialog title="Changer de mot de passe" onClose={() => onClose()}>
      <form style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={(e) => {
        e.preventDefault()
        if (next !== again) return a.setError('Les deux nouveaux mots de passe sont différents.')
        a.run(async () => {
          await api(account).changePassword(current, next)
          onClose('Mot de passe changé. Vos autres appareils ont été déconnectés.')
        })
      }}>
        <PasswordField label="Mot de passe actuel" value={current} onChange={(e) => setCurrent(e.target.value)} autoFocus autoComplete="current-password" />
        <PasswordField label="Nouveau mot de passe" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" hint="10 caractères au moins." />
        <PasswordField label="Nouveau mot de passe (encore)" value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" />
        <Alert kind="warn">Vos autres appareils seront déconnectés : il faudra les reconnecter et les valider à nouveau.</Alert>
        <Alert kind="error">{a.error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => onClose()}>Annuler</button>
          <Submit busy={a.busy} className="btn btn-primary btn-sm" disabled={!current || !next}>Changer</Submit>
        </div>
      </form>
    </Dialog>
  )
}

function QRCode({ text }: { text: string }) {
  const qr = qrcode(0, 'M')
  qr.addData(text)
  qr.make()
  const n = qr.getModuleCount()
  const cells: string[] = []
  for (let y = 0; y < n; y++) for (let x = 0; x < n; x++) if (qr.isDark(y, x)) cells.push(`M${x + 4} ${y + 4}h1v1h-1z`)
  return (
    <svg className="qr" viewBox={`0 0 ${n + 8} ${n + 8}`} role="img" aria-label="QR code à scanner avec l'application d'authentification">
      <rect width={n + 8} height={n + 8} fill="#fff" />
      <path d={cells.join('')} fill="#000" />
    </svg>
  )
}

function Enable2FADialog({ account, onClose }: { account: Account; onClose: (msg?: string) => void }) {
  const [password, setPassword] = useState('')
  const [setup, setSetup] = useState<{ secret: string; otpauth_uri: string } | null>(null)
  const [code, setCode] = useState('')
  const [codes, setCodes] = useState<string[] | null>(null)
  const a = useAction()
  if (codes) {
    return (
      <Dialog title="Codes de secours" onClose={() => onClose('Double authentification activée.')}>
        <p className="muted small" style={{ lineHeight: 1.5 }}>Chaque code sert une fois, si vous n&apos;avez plus votre téléphone. Gardez-les en lieu sûr : ils ne seront plus affichés.</p>
        <ul className="backup-codes" data-testid="backup-codes">{codes.map((c) => <li key={c}>{c}</li>)}</ul>
        <div className="dialog-actions">
          <button className="btn btn-ghost btn-sm" onClick={() => navigator.clipboard.writeText(codes.join('\n'))}>Copier</button>
          <button className="btn btn-primary btn-sm" onClick={() => onClose('Double authentification activée.')}>Terminer</button>
        </div>
      </Dialog>
    )
  }
  return (
    <Dialog title="Activer la double authentification" onClose={() => onClose()}>
      <form style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={(e) => {
        e.preventDefault()
        if (!setup) a.run(async () => setSetup(await api(account).setup2FA(password)))
        else a.run(async () => {
          const r = await api(account).enable2FA(code.trim())
          await updateUser({ ...account.user, totp_enabled: true })
          setCodes(r.backup_codes)
        })
      }}>
        {!setup ? (
          <PasswordField label="Mot de passe actuel" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus autoComplete="current-password" />
        ) : (
          <>
            <p className="muted small" style={{ lineHeight: 1.5 }}>Scannez ce QR code avec votre application d&apos;authentification (Aegis, 2FAS, Google Authenticator…), puis entrez le code à 6 chiffres qu&apos;elle affiche.</p>
            <div style={{ display: 'flex', justifyContent: 'center' }}><QRCode text={setup.otpauth_uri} /></div>
            <p className="muted small">Ou saisissez cette clé : <code className="secret" data-testid="totp-secret">{setup.secret}</code></p>
            <Field label="Code à 6 chiffres" value={code} onChange={(e) => setCode(e.target.value)} autoFocus inputMode="numeric" autoComplete="one-time-code" />
          </>
        )}
        <Alert kind="error">{a.error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => onClose()}>Annuler</button>
          <Submit busy={a.busy} className="btn btn-primary btn-sm">{setup ? 'Activer' : 'Continuer'}</Submit>
        </div>
      </form>
    </Dialog>
  )
}

// Passkeys (security keys, Windows Hello, phone…): a second factor next to
// TOTP, added on the identity service's page in the browser.
function Passkeys({ account }: { account: Account }) {
  const [list, setList] = useState<Passkey[] | null>(null)
  const [adding, setAdding] = useState(false)
  const [waiting, setWaiting] = useState(false)
  const a = useAction()
  const load = () => api(account).passkeys().then(setList, () => setList([]))
  useEffect(() => {
    load()
  }, [account]) // eslint-disable-line react-hooks/exhaustive-deps
  // After opening the page: check every 2 s (5 min) until the new key shows up.
  useEffect(() => {
    if (!waiting || !list) return
    const before = list.length
    const until = Date.now() + 5 * 60_000
    const t = setInterval(() => {
      if (Date.now() > until) return setWaiting(false)
      api(account).passkeys().then((l) => {
        if (l.length > before) {
          setList(l)
          setWaiting(false)
        }
      }, () => {})
    }, 2000)
    return () => clearInterval(t)
  }, [waiting]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <>
      <h3 className="settings-sub">Clés d&apos;accès</h3>
      <p className="muted small" style={{ lineHeight: 1.5 }}>
        À la connexion, une clé de sécurité, Windows Hello ou votre téléphone peuvent remplacer le code à 6 chiffres. Les codes de secours restent valables.
      </p>
      <Alert kind="error">{a.error}</Alert>
      {waiting && <Alert kind="info">Terminez dans la page qui s&apos;est ouverte dans votre navigateur…</Alert>}
      <div className="card" data-testid="passkeys">
        {list?.map((p) => (
          <div className="card-row" key={p.id}>
            <div className="grow">
              <span className="title">{p.name}</span>
              <span className="sub">Ajoutée le {new Date(p.created_at * 1000).toLocaleDateString('fr-FR')}{p.last_used_at ? ' · utilisée le ' + new Date(p.last_used_at * 1000).toLocaleDateString('fr-FR') : ''}</span>
            </div>
            <button className="btn btn-ghost btn-sm" aria-label={'Supprimer ' + p.name} onClick={() => a.run(async () => {
              await api(account).deletePasskey(p.id)
              await load()
            })}>Supprimer</button>
          </div>
        ))}
        <div className="card-row">
          <span className="grow muted small">{list?.length ? '' : 'Aucune clé pour l’instant.'}</span>
          <button className="btn btn-primary btn-sm" onClick={() => setAdding(true)}>Ajouter une clé</button>
        </div>
      </div>
      {adding && <AddPasskeyDialog account={account} onClose={() => setAdding(false)} onOpened={() => { setAdding(false); setWaiting(true) }} />}
    </>
  )
}

function AddPasskeyDialog({ account, onClose, onOpened }: { account: Account; onClose: () => void; onOpened: () => void }) {
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const a = useAction()
  return (
    <Dialog title="Ajouter une clé d'accès" onClose={onClose}>
      <form style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={(e) => {
        e.preventDefault()
        a.run(async () => {
          const { url } = await api(account).addPasskey(password, name.trim())
          window.open(url, '_blank', 'noopener')
          onOpened()
        })
      }}>
        <Field label="Nom de la clé" placeholder="Clé USB, téléphone…" value={name} maxLength={64} onChange={(e) => setName(e.target.value)} autoFocus />
        <PasswordField label="Mot de passe actuel" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
        <p className="muted small" style={{ lineHeight: 1.5 }}>Une page de votre service d&apos;identité va s&apos;ouvrir dans le navigateur pour enregistrer la clé.</p>
        <Alert kind="error">{a.error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
          <Submit busy={a.busy} className="btn btn-primary btn-sm">Continuer</Submit>
        </div>
      </form>
    </Dialog>
  )
}

function Disable2FADialog({ account, onClose }: { account: Account; onClose: (msg?: string) => void }) {
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const a = useAction()
  return (
    <Dialog title="Désactiver la double authentification" onClose={() => onClose()}>
      <form style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={(e) => {
        e.preventDefault()
        a.run(async () => {
          await api(account).disable2FA(password, code.trim())
          await updateUser({ ...account.user, totp_enabled: false })
          onClose('Double authentification désactivée.')
        })
      }}>
        <PasswordField label="Mot de passe actuel" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus autoComplete="current-password" />
        <Field label="Code de l'application (ou code de secours)" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" />
        <Alert kind="error">{a.error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => onClose()}>Annuler</button>
          <Submit busy={a.busy} className="btn btn-danger btn-sm" disabled={!password || !code}>Désactiver</Submit>
        </div>
      </form>
    </Dialog>
  )
}

function DeleteDialog({ account, onClose }: { account: Account; onClose: () => void }) {
  const u = account.user
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [confirm, setConfirm] = useState('')
  const a = useAction()
  return (
    <Dialog title="Supprimer mon compte" onClose={onClose}>
      <form style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={(e) => {
        e.preventDefault()
        a.run(async () => {
          await api(account).deleteAccount(password, code.trim() || undefined)
          await signOut({ remote: false })
        })
      }}>
        <Alert kind="warn">
          Tout est effacé immédiatement : amis, conversations privées, clés de chiffrement, sauvegarde. Les serveurs communautaires gardent vos anciens messages.
          Cette action est irréversible.
        </Alert>
        <PasswordField label="Mot de passe" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus autoComplete="current-password" />
        {u.totp_enabled && <Field label="Code de double authentification" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" />}
        <Field label={'Tapez « ' + u.pseudo + ' » pour confirmer'} value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        <Alert kind="error">{a.error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
          <Submit busy={a.busy} className="btn btn-danger btn-sm" disabled={!password || confirm !== u.pseudo}>Supprimer définitivement</Submit>
        </div>
      </form>
    </Dialog>
  )
}

// --- privacy ---

export function PrivacySection({ account }: { account: Account }) {
  const [privacy, setPrivacy] = useState<Privacy | null>(null)
  const [blocked, setBlocked] = useState<PublicUser[] | null>(null)
  const [pseudo, setPseudo] = useState('')
  const a = useAction()
  const load = () => {
    api(account).privacy().then(setPrivacy, (e) => a.setError(errorMessage(e)))
    api(account).blocks().then(setBlocked, () => {})
    refreshBlocks().catch(() => {})
  }
  useEffect(load, [account]) // eslint-disable-line react-hooks/exhaustive-deps

  const toggle = (k: 'typing' | 'read_receipts') => a.run(async () => setPrivacy(await api(account).setPrivacy({ [k]: !privacy![k] })))

  return (
    <>
      <h2>Confidentialité</h2>
      <Alert kind="error">{a.error}</Alert>
      {privacy && (
        <div className="card">
          <label className="check-line card-row">
            <input type="checkbox" checked={privacy.typing} onChange={() => toggle('typing')} />
            <span className="grow"><span className="title">Montrer quand j&apos;écris</span><span className="sub">« … écrit » dans les messages privés.</span></span>
          </label>
          <label className="check-line card-row">
            <input type="checkbox" checked={privacy.read_receipts} onChange={() => toggle('read_receipts')} />
            <span className="grow"><span className="title">Accusés de lecture</span><span className="sub">« Vu » sous les messages privés que vous avez lus.</span></span>
          </label>
          <label className="check-line card-row">
            <span className="grow"><span className="title">Qui peut vous demander en ami</span><span className="sub">Une demande que vous avez envoyée peut toujours être acceptée.</span></span>
            <select className="input" aria-label="Qui peut vous demander en ami" value={privacy.friend_requests} style={{ width: 'auto', height: 34, padding: '0 8px' }}
              onChange={(e) => {
                const v = e.target.value as Privacy['friend_requests']
                a.run(async () => setPrivacy(await api(account).setPrivacy({ friend_requests: v })))
              }}>
              <option value="everyone">Tout le monde</option>
              <option value="friends_of_friends">Amis de mes amis et membres de mes groupes</option>
              <option value="nobody">Personne</option>
            </select>
          </label>
        </div>
      )}
      <h3 className="settings-sub">Personnes bloquées</h3>
      <p className="muted small" style={{ lineHeight: 1.5 }}>
        Une personne bloquée ne peut plus vous demander en ami ni vous écrire, et ne sait pas qu&apos;elle est bloquée. Ses messages sont masqués sur les serveurs.
      </p>
      <form className="copy-row" onSubmit={(e) => {
        e.preventDefault()
        if (!pseudo.trim()) return
        a.run(async () => {
          await api(account).block(pseudo.trim().replace(/^@/, '').split('@')[0])
          setPseudo('')
          load()
        })
      }}>
        <input className="input" aria-label="Pseudo à bloquer" placeholder="pseudo" value={pseudo} onChange={(e) => setPseudo(e.target.value)} />
        <button className="btn btn-ghost btn-sm" style={{ height: 44 }} type="submit">Bloquer</button>
      </form>
      {blocked && blocked.length > 0 && (
        <div className="card" data-testid="blocked">
          {blocked.map((b) => (
            <div className="card-row" key={b.id}>
              <Avatar id={b.id} name={b.pseudo} src={account.identity + '/v1/users/' + b.id + '/avatar'} size={32} />
              <div className="grow"><span className="title">{b.pseudo}</span><span className="sub">{b.handle}</span></div>
              <button className="btn btn-ghost btn-sm" onClick={() => a.run(async () => {
                await api(account).unblock(b.id)
                load()
              })}>Débloquer</button>
            </div>
          ))}
        </div>
      )}
      {blocked && blocked.length === 0 && <p className="muted small">Personne n&apos;est bloqué.</p>}
    </>
  )
}
