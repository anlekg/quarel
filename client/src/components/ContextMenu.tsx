// Right-click menus: one open at a time, at the pointer, kept inside the
// window; Escape, a click elsewhere or scrolling closes it. Items run and
// close; a "custom" item (e.g. a volume slider) stays open while used.
import { useEffect, useLayoutEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from 'react'

export type MenuItem =
  | { label: string; onClick: () => void; danger?: boolean; disabled?: boolean; checked?: boolean }
  | { separator: true }
  | { title: string }
  | { custom: ReactNode }

let openMenu: ((m: { x: number; y: number; items: MenuItem[] } | null) => void) | null = null
let showToast: ((text: string) => void) | null = null

// A short message after a menu action (an error, "copied"…).
export function menuToast(text: string) {
  showToast?.(text)
}

// Runs a menu action; its failure is shown as a toast.
export function menuRun(p: Promise<unknown>, done?: string) {
  p.then(() => done && menuToast(done), (e: unknown) => menuToast(e instanceof Error ? e.message : String(e)))
}

// Opens a menu for a contextmenu event (does nothing when there are no items).
export function showMenu(e: ReactMouseEvent | MouseEvent, items: (MenuItem | false | null | undefined)[]) {
  const list = items.filter(Boolean) as MenuItem[]
  if (!list.length || !openMenu) return
  e.preventDefault()
  e.stopPropagation()
  openMenu({ x: e.clientX, y: e.clientY, items: list })
}

export function closeMenu() {
  openMenu?.(null)
}

// Mounted once, at the root of the app.
export function ContextMenuHost() {
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null)
  const [pos, setPos] = useState({ x: 0, y: 0 })
  const [toast, setToast] = useState('')
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    openMenu = setMenu
    let t: ReturnType<typeof setTimeout>
    showToast = (text) => {
      setToast(text)
      clearTimeout(t)
      t = setTimeout(() => setToast(''), 4000)
    }
    return () => {
      openMenu = null
      showToast = null
    }
  }, [])
  useLayoutEffect(() => {
    if (!menu || !ref.current) return
    const r = ref.current.getBoundingClientRect()
    setPos({ x: Math.max(4, Math.min(menu.x, innerWidth - r.width - 4)), y: Math.max(4, Math.min(menu.y, innerHeight - r.height - 4)) })
    ref.current.querySelector<HTMLElement>('[role=menuitem]:not([disabled]), [role=menuitemcheckbox]')?.focus()
  }, [menu])
  useEffect(() => {
    if (!menu) return
    const close = (e: Event) => {
      if (e.type === 'keydown' && (e as KeyboardEvent).key !== 'Escape') return
      if (e.type === 'mousedown' && ref.current?.contains(e.target as Node)) return
      if (e.type === 'blur' && e.target !== window) return // focus moving inside the page (e.g. to the slider)
      setMenu(null)
    }
    for (const t of ['mousedown', 'keydown', 'blur', 'resize'] as const) window.addEventListener(t, close, true)
    const scroll = (e: Event) => !ref.current?.contains(e.target as Node) && setMenu(null)
    window.addEventListener('scroll', scroll, true)
    return () => {
      for (const t of ['mousedown', 'keydown', 'blur', 'resize'] as const) window.removeEventListener(t, close, true)
      window.removeEventListener('scroll', scroll, true)
    }
  }, [menu])
  if (!menu) return toast ? <div className="ctx-toast" role="status">{toast}</div> : null
  return (
    <div ref={ref} className="ctx-menu" role="menu" style={{ left: pos.x, top: pos.y }} onContextMenu={(e) => e.preventDefault()}
      onKeyDown={(e) => {
        if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
        e.preventDefault()
        const items = [...(ref.current?.querySelectorAll<HTMLElement>('[role=menuitem]:not([disabled]), [role=menuitemcheckbox]') ?? [])]
        const i = items.indexOf(document.activeElement as HTMLElement)
        items[(i + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length]?.focus()
      }}>
      {menu.items.map((it, i) => {
        if ('separator' in it) return <div key={i} className="ctx-sep" role="separator" />
        if ('title' in it) return <div key={i} className="ctx-title">{it.title}</div>
        if ('custom' in it) return <div key={i} className="ctx-custom">{it.custom}</div>
        return (
          <button key={i} role={it.checked === undefined ? 'menuitem' : 'menuitemcheckbox'} aria-checked={it.checked} disabled={it.disabled}
            className={'ctx-item' + (it.danger ? ' danger' : '')} onClick={() => {
              setMenu(null)
              it.onClick()
            }}>
            {it.checked !== undefined && <span className="ctx-check">{it.checked ? '✓' : ''}</span>}
            {it.label}
          </button>
        )
      })}
    </div>
  )
}

// A volume slider for a menu (0–200 %).
export function VolumeSlider({ label, value, onChange }: { label: string; value: number; onChange: (v: number) => void }) {
  const [v, setV] = useState(value)
  return (
    <label className="ctx-volume">
      <span>{label} <b>{Math.round(v * 100)} %</b></span>
      <input type="range" min={0} max={200} step={5} value={Math.round(v * 100)} aria-label={label}
        onChange={(e) => {
          const n = Number(e.target.value) / 100
          setV(n)
          onChange(n)
        }} />
    </label>
  )
}
