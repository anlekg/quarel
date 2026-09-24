// Device validation and recovery phrase: banners, dialogs and the settings
// panels that use them.
import { useEffect, useState } from 'react'
import type { OwnDevice } from '../e2e/engine'
import { Alert, Dialog, Field, Submit } from '../components/ui'
import { errorMessage } from '../lib/errors'
import { prefs } from '../platform'
import { approveDevice, createRecovery, listDevices, restoreFromPhrase, useSocial } from '../state/social'

const plural = (n: number, word: string) => n + ' ' + word + (n > 1 ? 's' : '')

// Shown above the private messages: this device waits for validation,
// another one does, or the account has no recovery phrase yet.
export function SecurityBanner() {
  const s = useSocial()
  const [dialog, setDialog] = useState<'restore' | 'approve' | 'recovery' | null>(null)
  const [hideRecovery, setHideRecovery] = useState(() => prefs.get('hide-recovery-hint', false))
  if (s.status !== 'ready' && s.status !== 'offline') return null
  let banner = null
  if (!s.validated) {
    banner = (
      <Alert kind="warn">
        <div data-testid="unvalidated">
          Cet appareil n&apos;est pas encore validé : il ne peut ni envoyer ni lire les messages privés. Sur un appareil déjà validé,
          ouvrez Paramètres › Appareils et saisissez ce code : <span className="code-box" data-testid="own-code">{s.code}</span>
          {s.backup !== 'none' && s.backup !== 'unknown' && (
            <div className="banner-actions"><button className="btn btn-ghost btn-sm" onClick={() => setDialog('restore')}>Utiliser ma phrase de récupération</button></div>
          )}
        </div>
      </Alert>
    )
  } else if (s.pendingDevices > 0) {
    banner = (
      <Alert kind="info">
        <div>
          {s.pendingDevices > 1 ? s.pendingDevices + ' appareils attendent' : 'Un appareil attend'} d&apos;être validé{s.pendingDevices > 1 ? 's' : ''} pour lire vos messages privés.
          <div className="banner-actions"><button className="btn btn-primary btn-sm" onClick={() => setDialog('approve')}>Valider un appareil</button></div>
        </div>
      </Alert>
    )
  } else if (s.backup === 'none' && !hideRecovery) {
    banner = (
      <Alert kind="info">
        <div>
          Créez une phrase de récupération : sans elle, perdre tous vos appareils ferait perdre vos messages privés.
          <div className="banner-actions">
            <button className="btn btn-primary btn-sm" onClick={() => setDialog('recovery')}>Créer ma phrase</button>
            <button className="btn btn-ghost btn-sm" onClick={() => { prefs.set('hide-recovery-hint', true); setHideRecovery(true) }}>Plus tard</button>
          </div>
        </div>
      </Alert>
    )
  }
  return (
    <>
      {banner && <div className="unvalidated">{banner}</div>}
      {dialog === 'restore' && <RestoreDialog onClose={() => setDialog(null)} />}
      {dialog === 'approve' && <ApproveDialog onClose={() => setDialog(null)} />}
      {dialog === 'recovery' && <RecoveryDialog replace={false} onClose={() => setDialog(null)} />}
    </>
  )
}

// Validates another device of the account: the user types the code it shows.
export function ApproveDialog({ device, onClose }: { device?: OwnDevice; onClose: () => void }) {
  const [devices, setDevices] = useState<OwnDevice[] | null>(device ? [device] : null)
  const [picked, setPicked] = useState(device?.device_id ?? '')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState<number | null>(null)

  useEffect(() => {
    if (device) return
    listDevices().then((l) => {
      const pending = l.filter((d) => !d.trusted)
      setDevices(pending)
      if (pending.length === 1) setPicked(pending[0].device_id)
    }, (e) => setError(errorMessage(e)))
  }, [device])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!picked || !code.trim()) return
    setBusy(true)
    setError('')
    try {
      setDone(await approveDevice(picked, code))
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const target = devices?.find((d) => d.device_id === picked)
  if (done !== null) {
    return (
      <Dialog title="Appareil validé" onClose={onClose}>
        <Alert kind="info">« {target?.device_name} » est validé : il a reçu la clé du compte et {plural(done, 'message')} d&apos;historique.</Alert>
        <div className="dialog-actions"><button className="btn btn-primary btn-sm" onClick={onClose}>Terminer</button></div>
      </Dialog>
    )
  }
  return (
    <Dialog title="Valider un appareil" onClose={onClose}>
      <form onSubmit={submit} style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <p className="muted small" style={{ lineHeight: 1.5 }}>
          L&apos;appareil à valider affiche un code dans ses messages privés. Tapez-le ici : s&apos;il correspond, cet appareil recevra la clé de
          votre compte et l&apos;historique de vos conversations. Ne validez jamais un appareil que vous n&apos;avez pas sous les yeux.
        </p>
        {devices && devices.length === 0 && <Alert kind="info">Aucun appareil n&apos;attend de validation.</Alert>}
        {devices && devices.length > 1 && (
          <div className="checklist" role="radiogroup" aria-label="Appareil à valider">
            {devices.map((d) => (
              <label key={d.device_id}>
                <input type="radio" name="device" checked={picked === d.device_id} onChange={() => setPicked(d.device_id)} />
                {d.device_name}
              </label>
            ))}
          </div>
        )}
        {target && devices!.length === 1 && <p><b>{target.device_name}</b></p>}
        {target && (
          <Field label="Code affiché sur l'appareil" inputClass="code-input" placeholder="XXXX-XXXX-XXXX-XXXX" value={code} autoFocus
            autoComplete="off" spellCheck={false} onChange={(e) => setCode(e.target.value.toUpperCase())} />
        )}
        <Alert kind="error">{error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
          {target && <Submit busy={busy} className="btn btn-primary btn-sm" disabled={!code.trim()}>Valider</Submit>}
        </div>
      </form>
    </Dialog>
  )
}

// Creates (or replaces) the recovery phrase and shows it once.
export function RecoveryDialog({ replace, onClose }: { replace: boolean; onClose: () => void }) {
  const [phrase, setPhrase] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [noted, setNoted] = useState(false)

  async function create() {
    setBusy(true)
    setError('')
    try {
      setPhrase(await createRecovery(replace))
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  if (!phrase) {
    return (
      <Dialog title={replace ? 'Nouvelle phrase de récupération' : 'Phrase de récupération'} onClose={onClose}>
        <p className="muted small" style={{ lineHeight: 1.5 }}>
          Douze mots qui permettent de retrouver la clé de votre compte et vos messages privés si vous perdez tous vos appareils.
          Une sauvegarde chiffrée avec cette phrase est gardée par votre service d&apos;identité, qui ne peut pas la lire. Elle se met à jour automatiquement.
        </p>
        {replace && <Alert kind="warn">L&apos;ancienne phrase ne servira plus.</Alert>}
        <Alert kind="error">{error}</Alert>
        <div className="dialog-actions">
          <button className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
          <button className="btn btn-primary btn-sm" disabled={busy} onClick={create}>{busy ? <span className="spinner" /> : 'Créer la phrase'}</button>
        </div>
      </Dialog>
    )
  }
  const words = phrase.split(' ')
  return (
    <Dialog title="Notez votre phrase" onClose={() => noted && onClose()}>
      <p className="muted small" style={{ lineHeight: 1.5 }}>Écrivez ces mots sur papier, dans l&apos;ordre. Ils ne seront plus jamais affichés, et personne ne peut les retrouver pour vous.</p>
      <ol className="phrase-grid" data-testid="recovery-phrase">
        {words.map((w, i) => <li key={i}><span className="n">{i + 1}</span>{w}</li>)}
      </ol>
      <Alert kind="warn">Quiconque possède cette phrase peut lire vos messages privés : ne la stockez pas en ligne et ne la communiquez à personne.</Alert>
      <label className="check-line">
        <input type="checkbox" checked={noted} onChange={(e) => setNoted(e.target.checked)} />J&apos;ai noté ma phrase de récupération
      </label>
      <div className="dialog-actions">
        <button className="btn btn-primary btn-sm" disabled={!noted} onClick={onClose}>Terminer</button>
      </div>
    </Dialog>
  )
}

// Validates this device with the recovery phrase.
export function RestoreDialog({ onClose }: { onClose: () => void }) {
  const [phrase, setPhrase] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState<number | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      setDone(await restoreFromPhrase(phrase))
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  if (done !== null) {
    return (
      <Dialog title="Compte restauré" onClose={onClose}>
        <Alert kind="info">Cet appareil est validé : {plural(done, 'message')} retrouvé{done > 1 ? 's' : ''}.</Alert>
        <p className="muted small" style={{ lineHeight: 1.5 }}>Si vous avez perdu d&apos;autres appareils, déconnectez-les dans Paramètres › Appareils.</p>
        <div className="dialog-actions"><button className="btn btn-primary btn-sm" onClick={onClose}>Terminer</button></div>
      </Dialog>
    )
  }
  return (
    <Dialog title="Phrase de récupération" onClose={onClose}>
      <form onSubmit={submit} style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <p className="muted small" style={{ lineHeight: 1.5 }}>Saisissez les 12 mots dans l&apos;ordre, séparés par des espaces. Les accents et les majuscules ne comptent pas.</p>
        <textarea className="input" rows={3} aria-label="Les 12 mots" value={phrase} autoFocus spellCheck={false} autoComplete="off"
          onChange={(e) => setPhrase(e.target.value)} />
        <Alert kind="error">{error}</Alert>
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
          <Submit busy={busy} className="btn btn-primary btn-sm" disabled={phrase.trim().split(/\s+/).length !== 12}>Restaurer</Submit>
        </div>
      </form>
    </Dialog>
  )
}

// Settings › Récupération.
export function RecoverySection() {
  const s = useSocial()
  const [dialog, setDialog] = useState<'create' | 'replace' | 'restore' | null>(null)
  const date = s.backupAt ? new Intl.DateTimeFormat('fr-FR', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(s.backupAt)) : ''
  return (
    <>
      <h2>Phrase de récupération</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>
        Vos messages privés ne sont lisibles que sur vos appareils validés. La phrase de récupération (12 mots) permet de tout retrouver
        si vous les perdez tous : elle chiffre une sauvegarde que votre service d&apos;identité garde sans pouvoir la lire.
      </p>
      <div className="card" data-testid="recovery-status">
        <div className="card-row">
          <div className="grow">
            <span className="title">
              {s.backup === 'active' ? 'Sauvegarde active' : s.backup === 'no_key' ? 'Sauvegarde existante' : s.backup === 'none' ? 'Aucune phrase de récupération' : '…'}
            </span>
            <span className="sub">
              {s.backup === 'active' ? 'Mise à jour le ' + date + ', automatiquement.'
                : s.backup === 'no_key' ? 'Cet appareil n’en a pas la clé : il la reçoit quand un appareil validé le valide, ou avec la phrase.'
                  : s.backup === 'none' ? 'Si vous perdez tous vos appareils, vos messages privés seront perdus.' : ''}
            </span>
          </div>
          {s.validated && s.backup === 'none' && <button className="btn btn-primary btn-sm" onClick={() => setDialog('create')}>Créer ma phrase</button>}
          {s.validated && s.backup !== 'none' && s.backup !== 'unknown' && <button className="btn btn-ghost btn-sm" onClick={() => setDialog('replace')}>Nouvelle phrase</button>}
          {!s.validated && s.backup !== 'none' && s.backup !== 'unknown' && <button className="btn btn-primary btn-sm" onClick={() => setDialog('restore')}>Utiliser ma phrase</button>}
        </div>
      </div>
      {!s.validated && s.backup === 'none' && <p className="muted small">Cet appareil n&apos;est pas validé : créez la phrase depuis un appareil validé.</p>}
      <p className="muted small" style={{ lineHeight: 1.5 }}>Phrase perdue mais un appareil encore validé ? Créez-en une nouvelle : l&apos;ancienne ne servira plus.</p>
      {dialog === 'create' && <RecoveryDialog replace={false} onClose={() => setDialog(null)} />}
      {dialog === 'replace' && <RecoveryDialog replace onClose={() => setDialog(null)} />}
      {dialog === 'restore' && <RestoreDialog onClose={() => setDialog(null)} />}
    </>
  )
}
