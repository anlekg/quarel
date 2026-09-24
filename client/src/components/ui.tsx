import { useEffect, useId, useRef, useState, type InputHTMLAttributes, type ReactNode } from 'react'
import { Eye, EyeOff } from './icons'

type InputProps = InputHTMLAttributes<HTMLInputElement> & {
  label: string
  hint?: ReactNode
  error?: string | null
  inputClass?: string
}

export function Field({ label, hint, error, inputClass, ...input }: InputProps) {
  const id = useId()
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input id={id} className={'input ' + (inputClass ?? '')} aria-invalid={error ? true : undefined}
        aria-describedby={error || hint ? id + '-d' : undefined} {...input} />
      {error ? <span id={id + '-d'} className="field-error">{error}</span>
        : hint ? <span id={id + '-d'} className="field-hint">{hint}</span> : null}
    </div>
  )
}

export function PasswordField({ label, hint, error, ...input }: InputProps) {
  const id = useId()
  const [shown, setShown] = useState(false)
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <div className="pw-wrap">
        <input id={id} className="input" type={shown ? 'text' : 'password'} aria-invalid={error ? true : undefined}
          aria-describedby={error || hint ? id + '-d' : undefined} {...input} />
        <button type="button" className="pw-toggle" onClick={() => setShown(!shown)}
          aria-label={shown ? 'Masquer le mot de passe' : 'Afficher le mot de passe'}>
          {shown ? <EyeOff size={18} /> : <Eye size={18} />}
        </button>
      </div>
      {error ? <span id={id + '-d'} className="field-error">{error}</span>
        : hint ? <span id={id + '-d'} className="field-hint">{hint}</span> : null}
    </div>
  )
}

export function Submit({ busy, children, className = 'btn btn-primary', disabled }: {
  busy: boolean
  children: ReactNode
  className?: string
  disabled?: boolean
}) {
  return (
    <button type="submit" className={className} disabled={busy || disabled}>
      {busy ? <span className="spinner" aria-label="En cours" /> : children}
    </button>
  )
}

export function Alert({ kind, children }: { kind: 'error' | 'info' | 'warn'; children: ReactNode }) {
  if (!children) return null
  return <div className={'alert alert-' + kind} role={kind === 'error' ? 'alert' : 'status'}>{children}</div>
}

export function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const close = useRef(onClose)
  close.current = onClose
  // Focus once when opening, not at every re-render of the caller.
  useEffect(() => {
    if (ref.current?.contains(document.activeElement)) return // a field with autoFocus
    ref.current?.querySelector<HTMLElement>('input, select, textarea, button')?.focus()
  }, [])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Only the topmost dialog closes.
      const all = document.querySelectorAll('.dialog')
      if (e.key === 'Escape' && all[all.length - 1] === ref.current) {
        e.preventDefault() // full-screen settings behind stay open
        close.current()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  return (
    <div className="backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog" role="dialog" aria-modal="true" aria-label={title} ref={ref}>
        <h3>{title}</h3>
        {children}
      </div>
    </div>
  )
}

// Counts down after an action that the server rate-limits (e.g. resending a code).
export function useCooldown(): [number, (seconds: number) => void] {
  const [left, setLeft] = useState(0)
  useEffect(() => {
    if (left <= 0) return
    const t = setTimeout(() => setLeft(left - 1), 1000)
    return () => clearTimeout(t)
  }, [left])
  return [left, setLeft]
}
