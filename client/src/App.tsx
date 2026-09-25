import { useEffect, useState } from 'react'
import { restoreAccount, useAccount } from './state/account'
import { Auth } from './screens/Auth'
import { Shell } from './screens/Shell'
import { InviteLanding } from './screens/InviteLanding'
import { usePendingInvite, watchInvites } from './state/invite'
import { isDesktop } from './platform'

export function App() {
  const account = useAccount()
  const [ready, setReady] = useState(false)
  const [notice, setNotice] = useState('')
  const invite = usePendingInvite()
  const [inBrowser, setInBrowser] = useState(false) // web: chose to continue here rather than in the desktop app

  useEffect(() => watchInvites(), [])

  useEffect(() => {
    restoreAccount().then((r) => {
      if (r.state === 'expired') setNotice('Votre session a expiré ou a été fermée depuis un autre appareil. Reconnectez-vous.')
      setReady(true)
    })
  }, [])

  if (!ready) return <div className="splash">Chargement…</div>
  if (invite && !isDesktop && !inBrowser) return <InviteLanding link={invite} onContinue={() => setInBrowser(true)} />
  if (!account) return <Auth notice={notice} />
  return <Shell account={account} />
}
