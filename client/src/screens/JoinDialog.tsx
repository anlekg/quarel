// "Rejoindre un serveur": paste an invite link, check the server, join.
import { useState } from 'react'
import { ApiError } from '../api/http'
import type { ServerInfo } from '../api/community'
import { Alert, Dialog, Field, Submit } from '../components/ui'
import { Shield } from '../components/icons'
import { errorMessage } from '../lib/errors'
import { parseInvite, type Invite } from '../lib/invite'
import { isDesktop } from '../platform'
import type { Account } from '../state/account'
import { joinServer, previewServer, type ServerConn } from '../state/servers'

export function initials(name: string) {
  const words = name.trim().split(/\s+/).filter((w) => !/^(de|du|des|la|le|les|l'|d')$/i.test(w))
  return ((words[0]?.[0] ?? '?') + (words[1]?.[0] ?? '')).toUpperCase()
}

export function JoinDialog({ account, onClose, onJoined }: {
  account: Account
  onClose: () => void
  onJoined: (conn: ServerConn) => void
}) {
  const [link, setLink] = useState('')
  const [preview, setPreview] = useState<{ inv: Invite; info: ServerInfo } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function run(fn: () => Promise<void>) {
    setBusy(true)
    setError('')
    try {
      await fn()
    } catch (e) {
      if (e instanceof Error && e.message === 'bad_invite') setError(errorMessage(new ApiError(0, 'bad_invite', '')))
      else if (e instanceof ApiError && e.code === 'network') {
        setError(isDesktop
          ? 'Serveur injoignable, ou son certificat ne correspond pas au lien d’invitation.'
          : 'Serveur injoignable. Dans un navigateur, seuls les serveurs avec un certificat reconnu (nom de domaine) sont accessibles : utilisez l’application.')
      } else setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog title="Rejoindre un serveur" onClose={onClose}>
      {!preview ? (
        <form className="join-card" noValidate onSubmit={(e) => {
          e.preventDefault()
          run(async () => {
            const inv = parseInvite(link)
            setPreview({ inv, info: await previewServer(inv) })
          })
        }}>
          <p className="muted small" style={{ lineHeight: 1.5 }}>Collez le lien d&apos;invitation reçu d&apos;un membre du serveur.</p>
          <Field label="Lien d'invitation" value={link} onChange={(e) => setLink(e.target.value)}
            placeholder="quarel://hôte:port/CODE?sid=…" autoFocus />
          <Alert kind="error">{error}</Alert>
          <div className="dialog-actions">
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button>
            <Submit busy={busy} className="btn btn-primary btn-sm">Continuer</Submit>
          </div>
        </form>
      ) : (
        <div className="join-card">
          <div className="join-server">
            <span className="server-mark">{initials(preview.info.name)}</span>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              <span style={{ fontFamily: 'var(--font-display)', fontWeight: 600, fontSize: 22 }}>{preview.info.name}</span>
              <span className="muted small">
                {preview.info.member_count} membre{preview.info.member_count > 1 ? 's' : ''} · {preview.info.access === 'public' ? 'public' : 'sur invitation'}
              </span>
            </div>
          </div>
          <div className="verified">
            <Shield />
            <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              <b>Serveur authentifié</b>
              <span>Son identité correspond au lien d&apos;invitation. Il est hébergé par un particulier ou une association, qui peut lire les messages des salons.</span>
            </div>
          </div>
          {(preview.info.rules || preview.info.require_phone) && (
            <p className="muted small">
              Avant de participer, il faudra {preview.info.rules ? 'accepter ses règles' : ''}
              {preview.info.rules && preview.info.require_phone ? ' et ' : ''}
              {preview.info.require_phone ? 'vérifier un numéro de téléphone' : ''}.
            </p>
          )}
          <Alert kind="error">{error}</Alert>
          <div className="dialog-actions">
            <button type="button" className="btn btn-ghost btn-sm" onClick={() => setPreview(null)}>Retour</button>
            <button type="button" className="btn btn-primary btn-sm" disabled={busy}
              onClick={() => run(async () => onJoined(await joinServer(account, preview.inv)))}>
              {busy ? <span className="spinner" /> : 'Rejoindre'}
            </button>
          </div>
        </div>
      )}
    </Dialog>
  )
}
