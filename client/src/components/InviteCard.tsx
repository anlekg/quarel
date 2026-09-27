// Invite links in a message (private conversation or server channel) shown as
// a card: "Rejoindre" opens the join dialog, "Ouvrir" when this account is
// already a member. Nothing is fetched before a click: displaying a message
// never tells the invite's server that you saw it (nor your address).
import { parseInvite, type Invite } from '../lib/invite'
import { setPendingInvite } from '../state/invite'
import { useServers } from '../state/servers'

const linkRe = /(?:https?:\/\/[^\s<>"]+\/join#[^\s<>"]+|quarel:\/\/[^\s<>"]+)/gi

// The invite links of a text (3 at most, each once).
export function inviteLinks(text: string): { link: string; invite: Invite }[] {
  const out: { link: string; invite: Invite }[] = []
  for (const m of text.matchAll(linkRe)) {
    const link = m[0].replace(/[.,;:!?)»]+$/, '')
    try {
      const invite = parseInvite(link)
      if (!out.some((x) => x.invite.sid === invite.sid && x.invite.code === invite.code)) out.push({ link, invite })
    } catch {
      /* not an invite */
    }
    if (out.length === 3) break
  }
  return out
}

export function InviteCards({ text }: { text: string }) {
  const servers = useServers()
  const links = inviteLinks(text)
  if (!links.length) return null
  return (
    <>
      {links.map(({ link, invite }) => {
        const joined = servers.find((s) => s.saved.sid === invite.sid)
        const where = new URL(invite.base).host
        return (
          <div key={invite.sid + invite.code} className="invite-card" data-testid="invite-card">
            <span className="invite-mark" aria-hidden="true">{(joined?.saved.name ?? invite.host).slice(0, 1).toUpperCase()}</span>
            <span className="grow">
              <span className="title">{invite.claim ? 'Lien propriétaire d’un serveur' : 'Invitation sur un serveur'}</span>
              <span className="sub">{joined ? joined.saved.name + ' · ' : ''}{where}</span>
            </span>
            {joined ? (
              <button className="btn btn-ghost btn-sm" onClick={() => window.dispatchEvent(new CustomEvent('quarel:goto-server', { detail: { sid: invite.sid } }))}>Ouvrir</button>
            ) : (
              <button className="btn btn-primary btn-sm" onClick={() => setPendingInvite(link)}>Rejoindre</button>
            )}
          </div>
        )
      })}
    </>
  )
}
