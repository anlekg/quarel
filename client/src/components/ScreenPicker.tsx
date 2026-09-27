// Desktop app: choose the screen or window to share (browsers show their own
// picker). Used by voice channels and calls.
import { useEffect, useState } from 'react'
import { Dialog } from './ui'
import { screenSources, type ScreenSource } from '../platform'

export function ScreenPicker({ onPick, onClose }: { onPick: (id: string) => void; onClose: () => void }) {
  const [list, setList] = useState<ScreenSource[] | null>(null)
  useEffect(() => {
    let live = true
    // The system sometimes lists nothing the first time (X11): ask again.
    const load = async () => {
      for (let i = 0; i < 4 && live; i++) {
        const l = await screenSources().catch(() => [])
        if (l.length || i === 3) return live && setList(l)
        await new Promise((r) => setTimeout(r, 500))
      }
    }
    load()
    return () => {
      live = false
    }
  }, [])
  return (
    <Dialog title="Partager l'écran" onClose={onClose}>
      {!list ? <span className="spinner" /> : list.length === 0 ? <p className="muted">Aucun écran disponible.</p> : (
        <div className="picker-grid">
          {list.map((s) => (
            <button key={s.id} onClick={() => onPick(s.id)}>
              <img src={s.thumbnail} alt="" />
              <span>{s.name}</span>
            </button>
          ))}
        </div>
      )}
      <div className="dialog-actions"><button className="btn btn-ghost btn-sm" onClick={onClose}>Annuler</button></div>
    </Dialog>
  )
}
