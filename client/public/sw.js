// Quarel web app service worker: makes the app installable and quick to open.
// Only the app's own files are cached: built assets (names change with their
// content) once fetched, the page itself network first (the cached copy is
// used offline). Nothing from identity services or community servers.
const CACHE = 'quarel-app-v1'

self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (e) => {
  e.waitUntil((async () => {
    for (const k of await caches.keys()) if (k !== CACHE) await caches.delete(k)
    await self.clients.claim()
  })())
})

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url)
  if (e.request.method !== 'GET' || url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/assets/') || url.pathname.startsWith('/icons/')) {
    e.respondWith((async () => {
      const cache = await caches.open(CACHE)
      const hit = await cache.match(e.request)
      if (hit) return hit
      const res = await fetch(e.request)
      if (res.ok) cache.put(e.request, res.clone())
      return res
    })())
  } else if (e.request.mode === 'navigate') {
    e.respondWith((async () => {
      const cache = await caches.open(CACHE)
      try {
        const res = await fetch(e.request)
        if (res.ok) cache.put('/', res.clone())
        return res
      } catch {
        return (await cache.match('/')) ?? Response.error()
      }
    })())
  }
})
