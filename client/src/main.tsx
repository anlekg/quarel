import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/app.css'
import { App } from './App'
import { ErrorBoundary } from './components/ErrorBoundary'
import { ContextMenuHost } from './components/ContextMenu'
import { ConfirmHost } from './components/ConfirmDialog'
import { ThemeHost } from './components/ThemeHost'
import { isDesktop } from './platform'
import { startThemes } from './state/themes'

startThemes()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>
      <App />
      <ContextMenuHost />
      <ConfirmHost />
      <ThemeHost />
    </ErrorBoundary>
  </StrictMode>,
)

// Web: installable app (see public/sw.js). Not in the desktop app.
if (!isDesktop && 'serviceWorker' in navigator && (location.protocol === 'https:' || location.hostname === 'localhost')) {
  navigator.serviceWorker.register('/sw.js').catch(() => {})
}
