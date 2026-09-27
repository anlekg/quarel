// Custom CSS and themes (cosmetics block, decided by the PM):
// - my own CSS, in this app only, not filtered (I only harm myself);
// - the theme of the server I am looking at, on its area (lib/themecss.ts
//   filters its CSS), and the theme of a profile card I open;
// - the order: a server's theme goes over my CSS by default ("low"/"high"
//   cascade layers of styles/app.css); with my CSS first, the colours my CSS
//   sets are also kept from the server's theme;
// - everyone's themes can be turned off, or one server's; animations too;
// - safe mode (Ctrl+Shift+0, ?safe in the address, --safe-mode on the
//   desktop): no custom CSS at all until the app restarts, to get out of a
//   CSS that makes the app unusable.
import { useEffect, useSyncExternalStore } from 'react'
import { prefs } from '../platform'
import { customProps, parseTheme, themeCSS, type Theme } from '../lib/themecss'
import type { ServerTheme } from '../api/community'

export interface ThemePrefs {
  myCSS: string
  others: boolean // show the themes of servers and profiles
  serverFirst: boolean // a server's theme goes over my CSS
  animations: boolean // animations and transitions of the others' themes
}

const listeners = new Set<() => void>()
let safe = typeof location !== 'undefined' && new URLSearchParams(location.search).has('safe')
let snapshot: ThemePrefs & { safe: boolean } = read()

function read() {
  return {
    myCSS: prefs.get('my-css', ''),
    others: prefs.get('themes-others', true),
    serverFirst: prefs.get('theme-server-first', true),
    animations: prefs.get('theme-animations', true),
    safe,
  }
}

function changed() {
  snapshot = read()
  applyMine()
  for (const l of listeners) l()
}

export function useThemePrefs() {
  return useSyncExternalStore((l) => (listeners.add(l), () => listeners.delete(l)), () => snapshot)
}

export function setThemePref<K extends keyof ThemePrefs>(key: K, value: ThemePrefs[K]) {
  const names: Record<keyof ThemePrefs, string> = { myCSS: 'my-css', others: 'themes-others', serverFirst: 'theme-server-first', animations: 'theme-animations' }
  prefs.set(names[key], value)
  changed()
}

export function setSafeMode(on: boolean) {
  safe = on
  changed()
}

// One server's theme ignored (the server's menu).
export const serverThemeIgnored = (sid: string) => prefs.get('theme-ignore:' + sid, false)
export function setServerThemeIgnored(sid: string, on: boolean) {
  prefs.set('theme-ignore:' + sid, on)
  changed()
}

// The layer of my CSS and of the others' themes.
export const layers = (p = snapshot) => (p.serverFirst ? { mine: 'low', others: 'high' } : { mine: 'high', others: 'low' })

function styleElement(id: string) {
  let el = document.getElementById(id) as HTMLStyleElement | null
  if (!el) {
    el = document.createElement('style')
    el.id = id
    document.head.appendChild(el)
  }
  return el
}

function applyMine() {
  if (typeof document === 'undefined') return
  const p = snapshot
  let css = ''
  if (!p.safe && p.myCSS.trim()) css += `@layer ${layers(p).mine} {\n${p.myCSS}\n}\n`
  // !important in the app's layer beats every normal declaration of the themes.
  if (!p.animations) css += '@layer app { [data-qscope], [data-qscope] * { animation: none !important; transition: none !important; } }\n'
  styleElement('q-my-css').textContent = css
}

// Styles of the others' themes: shown unless safe mode or turned off.
export const showOthers = (p = snapshot) => !p.safe && p.others

// The CSS of someone else's theme for an area, in the others' layer.
export function othersCSS(theme: Theme | null, scope: string, id: string, opts: Parameters<typeof themeCSS>[3] = {}) {
  const p = snapshot
  if (!showOthers(p) || !theme) return { css: '', dropped: [] as string[] }
  const keep = p.serverFirst || p.safe ? undefined : customProps(p.myCSS)
  const r = themeCSS(theme, scope, id, { ...opts, keep })
  return { css: r.css ? `@layer ${layers(p).others} {\n${r.css}}\n` : '', dropped: r.dropped }
}

export const SERVER_SCOPE = '.server-zone[data-qscope="server"]'

// The theme of the server whose area is on screen (ServerView).
export function useServerTheme(sid: string, st: ServerTheme | undefined, imageURL: (path: string) => string | undefined) {
  const p = useThemePrefs()
  const ignored = serverThemeIgnored(sid)
  const theme = ignored ? null : parseTheme(st?.theme)
  const background = !ignored && st?.background_v && showOthers(p) ? imageURL('/v1/server/theme/background?v=' + st.background_v) : undefined
  const css = othersCSS(theme ?? (background ? {} : null), SERVER_SCOPE, 'srv', {
    background,
    surfaces: [[':scope > .sidebar', '--bg-1'], [':scope > .content', '--bg']],
  }).css
  useEffect(() => {
    styleElement('q-server-theme').textContent = css
    return () => { styleElement('q-server-theme').textContent = '' }
  }, [css])
}

// Keyboard escape hatch, and my CSS at start.
export function startThemes() {
  applyMine()
  window.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === '0' || e.code === 'Digit0')) {
      e.preventDefault()
      setSafeMode(!safe)
    }
  })
}
