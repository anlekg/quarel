// User settings. Account details and devices for now; editing the profile,
// password, email and 2FA comes with the settings step.
import { useCallback, useEffect, useState } from 'react'
import type { MyInvites, SessionInfo } from '../api/identity'
import { ApiError } from '../api/http'
import { Alert, Dialog } from '../components/ui'
import { Close, Monitor } from '../components/icons'
import { errorMessage } from '../lib/errors'
import { identityLabel } from '../lib/identityURL'
import { identityClient, signOut, type Account } from '../state/account'
import { canInstall, install, onInstallChange } from '../platform'
import type { OwnDevice } from '../e2e/engine'
import { listDevices, useSocial } from '../state/social'
import { ApproveDialog, RecoverySection } from './Security'
import { PrivacySection, ProfileSection, SecuritySection } from './SettingsAccount'
import { MediaSection } from './SettingsMedia'

type Section = 'profile' | 'security' | 'devices' | 'recovery' | 'privacy' | 'media' | 'invites'

const dateFmt = new Intl.DateTimeFormat('fr-FR', { dateStyle: 'long' })
const dateTimeFmt = new Intl.DateTimeFormat('fr-FR', { dateStyle: 'medium', timeStyle: 'short' })

export function Settings({ account, onClose }: { account: Account; onClose: () => void }) {
  const [section, setSection] = useState<Section>('profile')
  const [confirmLogout, setConfirmLogout] = useState(false)
  const nav = (id: Section, label: string) => <button className={section === id ? 'active' : ''} onClick={() => setSection(id)}>{label}</button>

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !e.defaultPrevented && !document.querySelector('.dialog') && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, confirmLogout])

  return (
    <div className="settings" role="dialog" aria-modal="true" aria-label="Paramètres">
      <nav className="settings-nav" aria-label="Sections">
        <span className="group">MON COMPTE</span>
        {nav('profile', 'Profil')}
        {nav('security', 'Sécurité')}
        {nav('devices', 'Appareils')}
        {nav('recovery', 'Récupération')}
        {nav('privacy', 'Confidentialité')}
        {nav('invites', 'Invitations')}
        <span className="group">APPLICATION</span>
        {nav('media', 'Voix et vidéo')}
        <InstallButton />
        <span className="group" />
        <button className="danger" onClick={() => setConfirmLogout(true)}>Se déconnecter</button>
      </nav>
      <main className="settings-main">
        {section === 'profile' ? <><ProfileSection account={account} /><AccountInfo account={account} /></>
          : section === 'security' ? <SecuritySection account={account} />
            : section === 'devices' ? <DevicesSection account={account} />
              : section === 'recovery' ? <RecoverySection />
                : section === 'privacy' ? <PrivacySection account={account} />
                  : section === 'media' ? <MediaSection /> : <InvitesSection account={account} />}
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

function AccountInfo({ account }: { account: Account }) {
  const u = account.user
  return (
    <>
      <h3 className="settings-sub">Compte</h3>
      <div className="card">
        <Row title="Identifiant complet (pour vous ajouter en ami)" value={u.handle} />
        <Row title="Service d'identité" value={identityLabel(account.identity)} />
        <Row title="Compte créé le" value={dateFmt.format(new Date(u.created_at))} />
      </div>
    </>
  )
}

function DevicesSection({ account }: { account: Account }) {
  const s = useSocial()
  const [list, setList] = useState<SessionInfo[] | null>(null)
  const [keys, setKeys] = useState<Record<string, OwnDevice>>({})
  const [approving, setApproving] = useState<OwnDevice | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')

  const load = useCallback(async () => {
    try {
      setList(await identityClient(account).sessions())
      const devs = await listDevices().catch(() => [])
      setKeys(Object.fromEntries(devs.map((d) => [d.device_id, d])))
      setError('')
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return signOut({ remote: false })
      setError(errorMessage(e))
    }
  }, [account])

  useEffect(() => {
    load()
  }, [load, s.pendingDevices, s.validated])

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
        leurs clés de chiffrement sont aussi supprimées. Un nouvel appareil doit être <b>validé</b> par un appareil qui l&apos;est déjà
        (ou avec la phrase de récupération) pour lire vos messages privés.
      </p>
      <Alert kind="error">{error}</Alert>
      {list && (
        <div className="card" data-testid="sessions">
          {list.map((x) => (
            <div className="card-row" key={x.id}>
              <Monitor />
              <div className="grow">
                <span className="title">
                  {x.device_name} {x.current && <span className="tag">Cet appareil</span>}{' '}
                  {keys[x.id] && (keys[x.id].trusted ? <span className="tag">Validé</span> : <span className="tag tag-warn">Non validé</span>)}
                </span>
                <span className="sub">Connecté le {dateTimeFmt.format(new Date(x.created_at))} · dernière activité le {dateTimeFmt.format(new Date(x.last_seen_at))}</span>
              </div>
              {!x.current && keys[x.id] && !keys[x.id].trusted && s.validated && (
                <button className="btn btn-primary btn-sm" onClick={() => setApproving(keys[x.id])}>Valider</button>
              )}
              {!x.current && (
                <button className="btn btn-ghost btn-sm" disabled={busy === x.id} onClick={() => revoke(x.id)}>Déconnecter</button>
              )}
            </div>
          ))}
        </div>
      )}
      {approving && <ApproveDialog device={approving} onClose={() => { setApproving(null); load() }} />}
    </>
  )
}

function InvitesSection({ account }: { account: Account }) {
  const [data, setData] = useState<MyInvites | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    identityClient(account).myInvites().then(setData, (e) => setError(errorMessage(e)))
  }, [account])
  useEffect(load, [load])
  const left = data ? Math.max(0, data.quota - data.created) : 0
  return (
    <>
      <h2>Invitations</h2>
      <Alert kind="error">{error}</Alert>
      {data && (
        <>
          <p className="muted" style={{ lineHeight: 1.5 }}>
            {data.registration === 'invite'
              ? 'Votre service d\u2019identité est sur invitation : pour qu\u2019une personne crée un compte, donnez-lui un code (usage unique, valable 30 jours).'
              : data.registration === 'closed' ? 'Votre service d\u2019identité n\u2019accepte plus de nouveaux comptes.'
                : 'Votre service d\u2019identité est ouvert : pas besoin d\u2019invitation pour créer un compte.'}
          </p>
          {data.registration !== 'closed' && data.quota > 0 && (
            <div className="card-row" style={{ border: '1px solid var(--line)', borderRadius: 10 }}>
              <div className="grow"><span className="title">{left} invitation{left > 1 ? 's' : ''} restante{left > 1 ? 's' : ''}</span><span className="sub">sur {data.quota}</span></div>
              <button className="btn btn-primary btn-sm" disabled={left === 0} onClick={() =>
                identityClient(account).createInvite().then(load, (e) => setError(errorMessage(e)))}>Créer une invitation</button>
            </div>
          )}
          {data.quota === 0 && data.registration === 'invite' && <p className="muted small">Seule l&apos;équipe du service peut inviter.</p>}
          {data.invites.length > 0 && (
            <div className="card" data-testid="my-invites">
              {data.invites.map((i) => (
                <div className="card-row" key={i.code}>
                  <div className="grow">
                    <span className="title" style={{ fontFamily: 'ui-monospace, monospace' }}>{i.code}</span>
                    <span className="sub">{i.uses >= i.max_uses && i.max_uses > 0 ? 'Utilisée' : i.expires_at && Date.parse(i.expires_at) < Date.now() ? 'Expirée' : 'Valable jusqu\u2019au ' + dateFmt.format(new Date(i.expires_at!))}</span>
                  </div>
                  <button className="btn btn-ghost btn-sm" onClick={() => navigator.clipboard.writeText(i.code)}>Copier</button>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </>
  )
}

// Web: install the app (icon, own window) when the browser offers it.
function InstallButton() {
  const [can, setCan] = useState(canInstall)
  useEffect(() => onInstallChange(() => setCan(canInstall())), [])
  if (!can) return null
  return <button onClick={() => install()}>Installer l&apos;application</button>
}
