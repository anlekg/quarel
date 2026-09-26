// Desktop app updates, as reported by the main process (electron/updater.ts).
import { useEffect, useState } from 'react'
import { updates, type UpdateState } from '../platform'

export function useUpdate(): UpdateState | null {
  const [s, setS] = useState<UpdateState | null>(null)
  useEffect(() => {
    if (!updates) return
    updates.state().then(setS, () => {})
    return updates.onState(setS)
  }, [])
  return s
}

export const checkUpdate = () => updates?.check()
export const installUpdate = () => updates?.install()
