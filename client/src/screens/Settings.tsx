// User settings. Account details and devices for now; editing the profile,
// password, email and 2FA comes with the settings step.
import { useCallback, useEffect, useState } from 'react'
import type { SessionInfo } from '../api/identity'
import { ApiError } from '../api/http'
import { Alert, Dialog } from '../components/ui'
import { Close, Monitor } from '../components/icons'
import { errorMessage } from '../lib/errors'
import { identityLabel } from '../lib/identityURL'
import { identityClient, signOut, type Account } from '../state/account'

type Section = 'account' | 'devices'

const dateFmt = new Intl.DateTimeFormat('fr-FR', { dateStyle: 'long' })
const dateTimeFmt = new Intl.DateTimeFormat('fr-FR', { dateStyle: 'medium', timeStyle: 'short' })

export function Settings({ account, onClose }: { account: Account; onClose: () => void }) {
  const [section, setSection] = useState<Section>('account')
  const [confirmLogout, setConfirmLogout] = useState(false)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !confirmLogout && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, confirmLogout])

  return (
    <div className="settings" role="dialog" aria-modal="true" aria-label="Paramètres">
      <nav className="settings-nav" aria-label="Sections">
        <span className="group">MON COMPTE</span>
        <button className={section === 'account' ? 'active' : ''} onClick={() => setSection('account')}>Compte</button>
        <button className={section === 'devices' ? 'active' : ''} onClick={() => setSection('devices')}>Appareils</button>
        <span className="group" />
        <button className="danger" onClick={() => setConfirmLogout(true)}>Se déconnecter</button>
      </nav>
      <main className="settings-main">
        {section === 'account' ? <AccountSection account={account} /> : <DevicesSection account={account} />}
      </main>
      <button className="icon-btn settings-close" aria-label="Fermer les paramètres" title="Fermer (Échap)" onClick={onClose}>
        <Close />
      </button>
      {confirmLogout && (
        <Dialog title="Se déconnecter ?" onClose={() => setConfirmLogout(false)}>
          <p className="muted" style={{ lineHeight: 1.5 }}>Cette session sera fermée sur cet appareil. Vous pourrez vous reconnecter avec votre mot de passe.</p>
          <div className="dialog-actions">
            <button className="btn btn-ghost btn-sm" onClick={() => setConfirmLogout(false)}>Annuler</button>
            <button className="btn btn-danger btn-sm" onClick={() => signOut()}>Se déconnecter</button>
          </div>
        </Dialog>
      )}
    </div>
  )
}

function Row({ title, value }: { title: string; value: React.ReactNode }) {
  return (
    <div className="card-row">
      <div className="grow">
        <span className="sub">{title}</span>
        <span className="title">{value}</span>
      </div>
    </div>
  )
}

function AccountSection({ account }: { account: Account }) {
  const u = account.user
  return (
    <>
      <h2>Compte</h2>
      <div className="card">
        <Row title="Pseudo" value={u.pseudo} />
        <Row title="Identifiant complet (pour vous ajouter en ami)" value={u.handle} />
        <Row title="Email" value={u.email} />
        <Row title="Double authentification" value={u.totp_enabled ? 'Activée' : 'Désactivée'} />
        <Row title="Service d'identité" value={identityLabel(account.identity)} />
        <Row title="Compte créé le" value={dateFmt.format(new Date(u.created_at))} />
      </div>
      <p className="muted small">La modification du profil, du mot de passe, de l&apos;email et de la double authentification arrive avec l&apos;étape « Paramètres ».</p>
    </>
  )
}

function DevicesSection({ account }: { account: Account }) {
  const [list, setList] = useState<SessionInfo[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')

  const load = useCallback(async () => {
    try {
      setList(await identityClient(account).sessions())
      setError('')
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return signOut({ remote: false })
      setError(errorMessage(e))
    }
  }, [account])

  useEffect(() => {
    load()
  }, [load])

  async function revoke(id: string) {
    setBusy(id)
    try {
      await identityClient(account).revokeSession(id)
      await load()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy('')
    }
  }

  return (
    <>
      <h2>Appareils</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>
        Chaque appareil connecté à votre compte a sa propre session. Déconnectez ceux que vous ne reconnaissez pas :
        leurs clés de chiffrement sont aussi supprimées.
      </p>
      <Alert kind="error">{error}</Alert>
      {list && (
        <div className="card" data-testid="sessions">
          {list.map((s) => (
            <div className="card-row" key={s.id}>
              <Monitor />
              <div className="grow">
                <span className="title">{s.device_name} {s.current && <span className="tag">Cet appareil</span>}</span>
                <span className="sub">Connecté le {dateTimeFmt.format(new Date(s.created_at))} · dernière activité le {dateTimeFmt.format(new Date(s.last_seen_at))}</span>
              </div>
              {!s.current && (
                <button className="btn btn-ghost btn-sm" disabled={busy === s.id} onClick={() => revoke(s.id)}>Déconnecter</button>
              )}
            </div>
          ))}
        </div>
      )}
    </>
  )
}
