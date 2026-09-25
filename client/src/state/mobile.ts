// Narrow screens (phones) show one pane at a time: the lists (servers,
// channels, conversations) or the open channel/conversation.
import { useSyncExternalStore } from 'react'

export type Pane = 'nav' | 'content'

let pane: Pane = 'nav'
const listeners = new Set<() => void>()

function set(p: Pane) {
  if (p === pane) return
  pane = p
  for (const l of listeners) l()
}

export const showContent = () => set('content')
export const showNav = () => set('nav')

export function useMobilePane(): Pane {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => pane,
  )
}
