// Web Push for the web app (not the desktop app, which keeps running): an
// EMPTY wake-up from the identity service when something arrives while no
// window of this device is connected; the service worker (public/sw.js) then
// shows a notification without any content. See internal/identity/webpush.go.
import { isDesktop, prefs } from '../platform'
import type { Account } from './account'
import { identityClient } from './account'

export const webPushSupported = () =>
  !isDesktop && typeof navigator !== 'undefined' && 'serviceWorker' in navigator && typeof window !== 'undefined' && 'PushManager' in window

export const webPushWanted = () => prefs.get('web-push', false)

async function registration() {
  return navigator.serviceWorker.ready
}

function keyBytes(b64: string) {
  const s = b64.replace(/-/g, '+').replace(/_/g, '/')
  return Uint8Array.from(atob(s + '='.repeat((4 - (s.length % 4)) % 4)), (c) => c.charCodeAt(0))
}

// Turns wake-ups on for this device (asks for the notification permission).
export async function enableWebPush(account: Account) {
  if ((await Notification.requestPermission()) !== 'granted') throw new Error('permission_denied')
  const api = identityClient(account)
  const { key } = await api.pushKey()
  const reg = await registration()
  let sub = await reg.pushManager.getSubscription()
  if (sub && sub.options.applicationServerKey && btoa(String.fromCharCode(...new Uint8Array(sub.options.applicationServerKey))) !== btoa(String.fromCharCode(...keyBytes(key)))) {
    await sub.unsubscribe() // another identity service's key
    sub = null
  }
  sub ??= await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(key) })
  await api.setPush(sub.endpoint)
  prefs.set('web-push', true)
}

export async function disableWebPush(account: Account) {
  prefs.set('web-push', false)
  await identityClient(account).deletePush().catch(() => {})
  const sub = await (await registration()).pushManager.getSubscription()
  await sub?.unsubscribe()
}

// At sign-in: a new session has no subscription yet on the service.
export async function restoreWebPush(account: Account) {
  if (!webPushSupported() || !webPushWanted() || Notification.permission !== 'granted') return
  const sub = await (await registration()).pushManager.getSubscription()
  if (sub) await identityClient(account).setPush(sub.endpoint).catch(() => {})
}
