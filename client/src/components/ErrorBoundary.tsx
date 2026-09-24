// A rendering error shows a message instead of a blank window.
import { Component, type ReactNode } from 'react'

export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error) {
    console.error('render error', error)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="empty-state" role="alert" style={{ height: '100vh' }}>
        <h2>Une erreur d&apos;affichage est survenue</h2>
        <p className="muted">Vos données ne sont pas touchées. Rechargez l&apos;interface pour continuer.</p>
        <code className="muted small">{this.state.error.message}</code>
        <button className="btn btn-primary btn-sm" onClick={() => location.reload()}>Recharger</button>
      </div>
    )
  }
}
