// Update banner (above the user bar) and the "À propos" settings section.
import { useEffect, useState } from 'react'
import { Alert } from '../components/ui'
import { checkUpdate, installUpdate, useUpdate } from '../state/updates'
import { closeToTray, type UpdateState } from '../platform'

export function UpdateBanner() {
  const u = useUpdate()
  if (u?.status !== 'ready') return null
  return (
    <div className="update-banner" role="status">
      <span>Mise à jour {u.version} prête</span>
      <button className="btn btn-primary btn-sm" onClick={() => installUpdate()}>Redémarrer</button>
    </div>
  )
}

function statusText(u: UpdateState): string {
  switch (u.status) {
    case 'disabled': return 'Mises à jour automatiques désactivées (version de développement).'
    case 'idle': return 'Recherche automatique au démarrage puis toutes les 6 heures.'
    case 'checking': return 'Recherche d’une mise à jour…'
    case 'none': return 'Quarel est à jour.'
    case 'downloading': return 'Téléchargement de la version ' + u.version + '… ' + (u.progress ?? 0) + ' %'
    case 'ready': return 'La version ' + u.version + ' est prête : elle s’installe au redémarrage (ou à la prochaine fermeture de Quarel).'
    case 'manual': return 'La version ' + u.version + ' est disponible. Ce paquet (.deb) ne se met pas à jour seul : installez la nouvelle version.'
    case 'error': return u.error === 'unsigned'
      ? 'Mise à jour refusée : elle n’est pas signée par Quarel. Réessayez plus tard ; si cela persiste, signalez-le.'
      : 'Serveur de mises à jour injoignable. Nouvel essai plus tard.'
  }
}

export function AboutSection({ version }: { version: string }) {
  const u = useUpdate()
  const [busy, setBusy] = useState(false)
  return (
    <>
      <h2>À propos</h2>
      <div className="card">
        <div className="card-row"><div className="grow"><span className="sub">Version</span><span className="title" data-testid="app-version">{u?.current ?? version}</span></div></div>
        {u && (
          <div className="card-row" data-testid="update-status" data-status={u.status}>
            <div className="grow"><span className="sub">Mises à jour</span><span className="title" style={{ fontWeight: 500 }}>{statusText(u)}</span></div>
            {u.status === 'ready' ? (
              <button className="btn btn-primary btn-sm" onClick={() => installUpdate()}>Redémarrer</button>
            ) : u.status === 'manual' && u.download ? (
              <a className="btn btn-primary btn-sm" href={u.download} target="_blank" rel="noreferrer">Télécharger</a>
            ) : u.status !== 'disabled' && (
              <button className="btn btn-ghost btn-sm" disabled={busy || u.status === 'checking' || u.status === 'downloading'}
                onClick={async () => { setBusy(true); await checkUpdate(); setBusy(false) }}>Rechercher</button>
            )}
          </div>
        )}
      </div>
      <TraySetting />
      {!u && <Alert kind="info">Version web : toujours à jour (rechargez la page pour la dernière version).</Alert>}
      <p className="muted small" style={{ lineHeight: 1.5 }}>
        Logiciel libre (licence Apache-2.0). Les mises à jour sont signées par Quarel : l&apos;application refuse tout fichier qui ne l&apos;est pas.
      </p>
    </>
  )
}

// Desktop app: the close button leaves Quarel in the notification area.
function TraySetting() {
  const [on, setOn] = useState<boolean | null>(null)
  useEffect(() => {
    closeToTray().then(setOn, () => setOn(null))
  }, [])
  if (on === null) return null
  return (
    <label className="check-line card" style={{ padding: '14px 16px', marginTop: 12 }}>
      <input type="checkbox" checked={on} onChange={(e) => closeToTray(e.target.checked).then((v) => setOn(v ?? false))} />
      <span className="grow">
        <span className="title">Réduire dans la zone de notification à la fermeture</span>
        <span className="sub">La croix cache Quarel près de l&apos;horloge : messages, notifications et appels continuent d&apos;arriver. « Quitter Quarel » dans le menu de l&apos;icône le ferme vraiment.</span>
      </span>
    </label>
  )
}
