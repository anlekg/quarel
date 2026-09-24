import { useEffect, useState } from 'react'
import { restoreAccount, useAccount } from './state/account'
import { Auth } from './screens/Auth'
import { Shell } from './screens/Shell'

export function App() {
  const account = useAccount()
  const [ready, setReady] = useState(false)
  const [notice, setNotice] = useState('')

  useEffect(() => {
    restoreAccount().then((r) => {
      if (r.state === 'expired') setNotice('Votre session a expiré ou a été fermée depuis un autre appareil. Reconnectez-vous.')
      setReady(true)
    })
  }, [])

  if (!ready) return <div className="splash">Chargement…</div>
  if (!account) return <Auth notice={notice} />
  return <Shell account={account} />
}
