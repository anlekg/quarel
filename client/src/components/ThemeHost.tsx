// Safe mode banner (state/themes.ts): custom CSS is off until turned back on.
import { setSafeMode, useThemePrefs } from '../state/themes'

export function ThemeHost() {
  const p = useThemePrefs()
  if (!p.safe) return null
  return (
    <div className="safe-banner" role="status">
      Mode sans échec : aucun CSS personnalisé (le vôtre ni ceux des serveurs).
      <button className="btn btn-ghost btn-sm" onClick={() => setSafeMode(false)}>Réactiver</button>
    </div>
  )
}
