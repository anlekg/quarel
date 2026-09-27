// Calls between friends: the bar above the user bar (visible everywhere), the
// incoming call card, and the call panel of a private conversation.
import { useEffect, useRef, useState } from 'react'
import type { Account } from '../state/account'
import { Avatar } from '../components/Avatar'
import { Camera, Hangup, Mic, MicOff, Monitor, Phone } from '../components/icons'
import { ScreenPicker } from '../components/ScreenPicker'
import { errorMessage } from '../lib/errors'
import { chooseScreenSource, isDesktop } from '../platform'
import {
  acceptCall, canShareScreen, declineCall, hangUp, toggleCallCamera, toggleCallMute, toggleCallScreen, useCall, type CallPath, type CallSnapshot,
} from '../state/calls'

const pathLabel: Record<CallPath, string> = { local: 'en direct (réseau local)', direct: 'en direct', relay: 'via le relais (chiffré)' }

function useElapsed(since?: number) {
  const [, tick] = useState(0)
  useEffect(() => {
    if (!since) return
    const t = setInterval(() => tick((n) => n + 1), 1000)
    return () => clearInterval(t)
  }, [since])
  if (!since) return ''
  const s = Math.floor((Date.now() - since) / 1000)
  return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0')
}

function statusText(c: CallSnapshot, elapsed: string) {
  switch (c.status) {
    case 'ringing': return 'Appel en cours…'
    case 'incoming': return 'Appel entrant'
    case 'connecting': return 'Connexion…'
    case 'active': return 'En communication · ' + elapsed
    case 'ended': return c.ended ?? 'Appel terminé.'
  }
}

export function CallBar({ onOpen }: { onOpen: (userId: string) => void }) {
  const c = useCall()
  const elapsed = useElapsed(c?.startedAt)
  const [picking, setPicking] = useState(false)
  const [error, setError] = useState('')
  if (!c || c.status === 'incoming') return null
  const share = (fn: () => Promise<void>) => {
    setError('')
    fn().catch((e) => setError(errorMessage(e)))
  }
  const cls = c.status === 'active' ? '' : c.status === 'ended' ? 'err' : 'warn'
  return (
    <div className="voicebar" role="region" aria-label="Appel" data-testid="callbar" data-status={c.status} data-path={c.path ?? ''} data-received={c.received}>
      <div className="st">
        <div className="t">
          <b className={cls}>{statusText(c, elapsed)}</b>
          <span onClick={() => onOpen(c.peer.id)} title="Afficher la conversation">
            {c.peer.pseudo}{c.status === 'active' && c.path ? ' · ' + pathLabel[c.path] : ''}
          </span>
        </div>
        {c.status !== 'ended' && (
          <button className="icon-btn hang" aria-label="Raccrocher" title="Raccrocher" onClick={() => hangUp()}><Hangup size={18} /></button>
        )}
      </div>
      {c.status !== 'ended' && (
        <div className="st">
          <button className={'icon-btn' + (c.muted ? ' on-danger' : '')} aria-pressed={c.muted} aria-label={c.muted ? 'Réactiver le micro' : 'Couper le micro'}
            title={c.muted ? 'Réactiver le micro' : 'Couper le micro'} onClick={toggleCallMute}>
            {c.muted ? <MicOff size={18} /> : <Mic size={18} />}
          </button>
          {c.canVideo && (c.status === 'active' || c.status === 'connecting') && (
            <button className={'icon-btn' + (c.camera ? ' on' : '')} aria-pressed={c.camera} aria-label={c.camera ? 'Couper la caméra' : 'Activer la caméra'}
              title={c.camera ? 'Couper la caméra' : 'Activer la caméra'} onClick={() => toggleCallCamera().catch(() => {})}><Camera size={18} /></button>
          )}
          {c.canScreen && c.status === 'active' && canShareScreen() && (
            <button className={'icon-btn' + (c.screen ? ' on' : '')} aria-pressed={c.screen} aria-label={c.screen ? 'Arrêter le partage d’écran' : 'Partager l’écran'}
              title={c.screen ? 'Arrêter le partage d’écran' : 'Partager l’écran'}
              onClick={() => (c.screen || !isDesktop ? share(toggleCallScreen) : setPicking(true))}><Monitor size={18} /></button>
          )}
        </div>
      )}
      {error && <p className="err small">{error}</p>}
      {picking && <ScreenPicker onClose={() => setPicking(false)} onPick={(id) => {
        setPicking(false)
        share(async () => {
          await chooseScreenSource(id)
          await toggleCallScreen()
        })
      }} />}
    </div>
  )
}

export function IncomingCall({ account }: { account: Account }) {
  const c = useCall()
  if (!c || c.status !== 'incoming') return null
  return (
    <div className="incoming-call" role="alertdialog" aria-label={'Appel de ' + c.peer.pseudo}>
      <Avatar id={c.peer.id} name={c.peer.pseudo} src={account.identity + '/v1/users/' + c.peer.id + '/avatar'} size={64} />
      <div className="who"><b>{c.peer.pseudo}</b><span className="muted small">vous appelle</span></div>
      <div className="acts">
        <button className="btn btn-danger btn-sm" onClick={declineCall}><Hangup size={16} />Refuser</button>
        <button className="btn btn-primary btn-sm" onClick={() => acceptCall().catch(() => hangUp('Impossible de répondre.'))}><Phone size={16} />Répondre</button>
      </div>
    </div>
  )
}

function Video({ stream, muted, mirror }: { stream: MediaStream; muted?: boolean; mirror?: boolean }) {
  const ref = useRef<HTMLVideoElement>(null)
  useEffect(() => {
    if (ref.current) ref.current.srcObject = stream
  }, [stream])
  return <video ref={ref} autoPlay playsInline muted={muted} className={mirror ? 'mirror' : ''} />
}

// The call with this person, in their conversation.
export function CallPanel({ account, userId }: { account: Account; userId: string }) {
  const c = useCall()
  const elapsed = useElapsed(c?.startedAt)
  if (!c || c.peer.id !== userId || c.status === 'incoming') return null
  const remoteVideo = c.remoteCamera && c.remoteStream && c.remoteStream.getVideoTracks().length > 0
  const remoteScreen = c.remoteScreen && c.remoteScreenStream && c.remoteScreenStream.getVideoTracks().length > 0
  const me = account.user
  return (
    <div className="call-panel" data-testid="call-panel">
      {(remoteScreen || (c.screen && c.screenStream)) && (
        <div className="call-screens">
          {remoteScreen && (
            <div className="call-screen" data-testid="call-remote-screen">
              <Video stream={c.remoteScreenStream!} muted />
              <span className="call-name">Écran de {c.peer.pseudo}</span>
            </div>
          )}
          {c.screen && c.screenStream && (
            <div className="call-screen mine" data-testid="call-my-screen">
              <Video stream={c.screenStream} muted />
              <span className="call-name">Votre écran</span>
            </div>
          )}
        </div>
      )}
      <div className="call-stage">
        <div className="call-tile">
          {remoteVideo ? <Video stream={c.remoteStream!} muted /> : <Avatar id={c.peer.id} name={c.peer.pseudo} src={account.identity + '/v1/users/' + c.peer.id + '/avatar'} size={72} />}
          <span className="call-name">{c.peer.pseudo}{c.remoteMuted && <MicOff size={14} />}</span>
        </div>
        <div className="call-tile">
          {c.camera && c.localStream ? <Video stream={c.localStream} muted mirror /> : <Avatar id={me.id} name={me.pseudo} src={account.identity + '/v1/users/' + me.id + '/avatar'} size={72} />}
          <span className="call-name">{me.pseudo}{c.muted && <MicOff size={14} />}</span>
        </div>
      </div>
      <p className="call-status">{statusText(c, elapsed)}{c.status === 'active' && c.path ? ' · ' + pathLabel[c.path] : ''}</p>
    </div>
  )
}
