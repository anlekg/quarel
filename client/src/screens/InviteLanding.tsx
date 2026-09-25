// Web: an invite link was opened. Offer the desktop app (which also reaches
// servers with a self-signed certificate) or continue in the browser.
import { desktopLink, parseInvite } from '../lib/invite'
import { clearPendingInvite } from '../state/invite'

export function InviteLanding({ link, onContinue }: { link: string; onContinue: () => void }) {
  const inv = parseInvite(link)
  return (
    <div className="landing">
      <div className="landing-card">
        <div className="brand"><span className="brand-mark">Q</span><span className="brand-name">Quarel</span></div>
        <h1>Invitation sur un serveur Quarel</h1>
        <p className="muted">Serveur : <b>{inv.host}</b>{inv.claim && ' — lien propriétaire'}</p>
        <a className="btn btn-primary" href={desktopLink(link)}>Ouvrir dans l&apos;application Quarel</a>
        <button className="btn btn-ghost" onClick={onContinue}>Continuer dans le navigateur</button>
        <p className="muted small" style={{ lineHeight: 1.5 }}>
          Dans le navigateur, seuls les serveurs avec un certificat reconnu sont accessibles. L&apos;application de bureau
          rejoint tous les serveurs, y compris ceux hébergés à la maison.
        </p>
        <button className="link small" onClick={() => { clearPendingInvite(); onContinue() }}>Ignorer l&apos;invitation</button>
      </div>
    </div>
  )
}
