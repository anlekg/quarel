// Signed-in layout: server rail, sidebar, content, user bar (see the mockups).
// Servers, friends and messages arrive in the next steps.
import { useState } from 'react'
import type { Account } from '../state/account'
import { Avatar } from '../components/Avatar'
import { Chat, Gear, Plus } from '../components/icons'
import { Settings } from './Settings'

export function Shell({ account }: { account: Account }) {
  const [settings, setSettings] = useState(false)
  const u = account.user
  return (
    <div className="shell">
      <nav className="rail" aria-label="Serveurs">
        <button className="rail-btn active" aria-label="Messages privés" title="Messages privés"><Chat /></button>
        <span className="rail-sep" />
        <button className="rail-btn add" aria-label="Ajouter un serveur" title="Ajouter un serveur (bientôt)" disabled>
          <Plus />
        </button>
      </nav>
      <aside className="sidebar">
        <div className="sidebar-head">Messages privés</div>
        <div className="sidebar-body">
          <p className="muted small" style={{ padding: '4px 8px', lineHeight: 1.5 }}>Vos amis et conversations apparaîtront ici.</p>
        </div>
        <div className="userbar">
          <Avatar id={u.id} name={u.pseudo} src={account.identity + '/v1/users/' + u.id + '/avatar'} />
          <div className="who">
            <span className="name">{u.pseudo}</span>
            <span className="handle" title={u.handle}>{u.handle}</span>
          </div>
          <button className="icon-btn" aria-label="Paramètres" title="Paramètres" onClick={() => setSettings(true)}><Gear size={18} /></button>
        </div>
      </aside>
      <main className="content">
        <div className="empty-state">
          <h2>Bienvenue, {u.pseudo}</h2>
          <p>Votre compte est prêt. Rejoindre des serveurs, ajouter des amis et discuter arrivent dans les prochaines étapes.</p>
        </div>
      </main>
      {settings && <Settings account={account} onClose={() => setSettings(false)} />}
    </div>
  )
}
