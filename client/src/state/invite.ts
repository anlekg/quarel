// An invite link opened from outside the app (web link, or quarel:// handed
// over by the system), waiting to be shown in the "join" dialog.
import { useSyncExternalStore } from 'react'
import { parseInvite } from '../lib/invite'
import { onInviteOpened, takeLaunchInvite } from '../platform'

let pending = ''
const listeners = new Set<() => void>()

export function setPendingInvite(link: string) {
  try {
    parseInvite(link)
  } catch {
    return // not an invite: ignored
  }
  pending = link
  for (const l of listeners) l()
}

export function clearPendingInvite() {
  pending = ''
  for (const l of listeners) l()
}

export function usePendingInvite(): string {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => pending,
  )
}

export function watchInvites() {
  takeLaunchInvite().then((l) => l && setPendingInvite(l), () => {})
  return onInviteOpened(setPendingInvite)
}
