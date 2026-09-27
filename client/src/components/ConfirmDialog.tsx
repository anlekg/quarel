// Confirmations inside the app, never the browser's or the system's box:
// window.confirm() looks foreign, and in the desktop app (Windows above all)
// the page can lose keyboard focus after one (fields no longer take typing).
//
//   if (await confirmAction({ title: 'Supprimer le salon ?', message: '…', confirm: 'Supprimer', danger: true })) …
//
// Enter confirms, Escape or a click outside cancels; the focus goes back to
// where it was.
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Dialog } from './ui'

export interface ConfirmOptions {
  title: string
  message?: ReactNode
  confirm?: string // label of the confirming button ("Confirmer")
  danger?: boolean // destructive action: red button
}

type Request = ConfirmOptions & { resolve: (ok: boolean) => void }

let open: ((r: Request) => void) | null = null

// Asks for a confirmation; resolves to false when cancelled (or when no host is mounted).
export function confirmAction(opts: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    if (!open) return resolve(false)
    open({ ...opts, resolve })
  })
}

// Mounted once, at the root of the app (after it: above every other dialog).
export function ConfirmHost() {
  const [req, setReq] = useState<Request | null>(null)
  const previous = useRef<HTMLElement | null>(null)
  useEffect(() => {
    open = (r) => {
      previous.current = document.activeElement as HTMLElement | null
      setReq((cur) => {
        cur?.resolve(false) // a new question replaces an unanswered one
        return r
      })
    }
    return () => {
      open = null
    }
  }, [])
  if (!req) return null
  const answer = (ok: boolean) => {
    setReq(null)
    req.resolve(ok)
    const back = previous.current
    if (back?.isConnected) setTimeout(() => back.focus(), 0)
  }
  return (
    <Dialog title={req.title} onClose={() => answer(false)}>
      <form className="confirm-dialog" onSubmit={(e) => {
        e.preventDefault()
        answer(true)
      }}>
        {req.message && <p className="confirm-text">{req.message}</p>}
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => answer(false)}>Annuler</button>
          <button type="submit" className={'btn btn-sm ' + (req.danger ? 'btn-danger' : 'btn-primary')} autoFocus>{req.confirm ?? 'Confirmer'}</button>
        </div>
      </form>
    </Dialog>
  )
}
