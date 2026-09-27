// Calls between friends and in groups: the bar above the user bar (visible
// everywhere), the incoming call card, and the call panel of a private
// conversation.
import { useEffect, useRef, useState } from 'react'
import type { Account } from '../state/account'
import { Avatar } from '../components/Avatar'
import { Camera, Hangup, Mic, MicOff, Monitor, Phone } from '../components/icons'
import { ScreenPicker } from '../components/ScreenPicker'
import { SignalBars } from '../components/SignalBars'
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

const connected = (c: CallSnapshot) => c.peers.filter((p) => p.connected).length

function statusText(c: CallSnapshot, elapsed: string) {
  const group = c.kind === 'group'
  switch (c.status) {
    case 'ringing': return group ? 'En attente des autres membres…' : 'Appel en cours…'
    case 'incoming': return group ? 'Appel de groupe' : 'Appel entrant'
    case 'connecting': return 'Connexion…'
    case 'active': return 'En communication · ' + (group ? connected(c) + 1 + ' personnes · ' : '') + elapsed
    case 'ended': return c.ended ?? 'Appel terminé.'
  }
}

// Where the call is: the friend (and the path), or the group.
function where(c: CallSnapshot) {
  return c.title + (c.kind === 'direct' && c.status === 'active' && c.path ? ' · ' + pathLabel[c.path] : '')
}

export function CallBar({ onOpen }: { onOpen: (c: CallSnapshot) => void }) {
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
  const live = c.status === 'active' || c.status === 'connecting' || (c.kind === 'group' && c.status === 'ringing')
  const received = c.kind === 'direct' ? c.received : c.peers.reduce((n, p) => n + p.received, 0)
  return (
    <div className="voicebar" role="region" aria-label="Appel" data-testid="callbar" data-status={c.status} data-path={c.path ?? ''} data-received={received}
      data-peers={connected(c)}>
      <div className="st">
        {c.status === 'active' && <SignalBars rtt={c.rtt} label={c.kind === 'group' ? 'Ping (la liaison la plus lente)' : 'Ping avec ' + c.peer.pseudo} />}
        <div className="t">
          <b className={cls}>{statusText(c, elapsed)}</b>
          <span onClick={() => onOpen(c)} title="Afficher la conversation">{where(c)}</span>
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
          {c.canVideo && live && (
            <button className={'icon-btn' + (c.camera ? ' on' : '')} aria-pressed={c.camera} aria-label={c.camera ? 'Couper la caméra' : 'Activer la caméra'}
              title={c.camera ? 'Couper la caméra' : 'Activer la caméra'} onClick={() => toggleCallCamera().catch(() => {})}><Camera size={18} /></button>
          )}
          {c.canScreen && (c.status === 'active' || c.kind === 'group') && live && canShareScreen() && (
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
  const group = c.kind === 'group'
  return (
    <div className="incoming-call" role="alertdialog" aria-label={group ? 'Appel de groupe ' + c.title : 'Appel de ' + c.peer.pseudo}>
      <Avatar id={c.peer.id} name={c.peer.pseudo} src={account.identity + '/v1/users/' + c.peer.id + '/avatar'} size={64} />
      <div className="who">
        <b>{group ? c.title : c.peer.pseudo}</b>
        <span className="muted small">{group ? c.peer.pseudo + ' lance un appel de groupe' : 'vous appelle'}</span>
      </div>
      <div className="acts">
        <button className="btn btn-danger btn-sm" onClick={declineCall}><Hangup size={16} />{group ? 'Ignorer' : 'Refuser'}</button>
        <button className="btn btn-primary btn-sm" onClick={() => acceptCall().catch(() => hangUp('Impossible de répondre.'))}>
          <Phone size={16} />{group ? 'Rejoindre' : 'Répondre'}
        </button>
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

const hasVideo = (s: MediaStream | null): s is MediaStream => !!s && s.getVideoTracks().length > 0

// The call in this conversation: with this person (userId), or in this group (convId).
export function CallPanel({ account, userId, convId }: { account: Account; userId?: string; convId?: string }) {
  const c = useCall()
  const elapsed = useElapsed(c?.startedAt)
  if (!c || c.status === 'incoming') return null
  if (c.kind === 'direct' ? !userId || c.peer.id !== userId : !convId || c.convId !== convId) return null
  const me = account.user
  const avatar = (id: string, name: string) => <Avatar id={id} name={name} src={account.identity + '/v1/users/' + id + '/avatar'} size={72} />
  // Before the friend answers, their tile is still shown.
  const others = c.kind === 'direct' && !c.peers.length
    ? [{ device: '', user: c.peer, connected: false, muted: false, camera: false, screen: false, stream: null, screenStream: null, rtt: undefined as number | undefined }]
    : c.peers
  const screens = others.filter((p) => p.screen && hasVideo(p.screenStream))
  return (
    <div className="call-panel" data-testid="call-panel">
      {(screens.length > 0 || (c.screen && c.screenStream)) && (
        <div className="call-screens">
          {screens.map((p) => (
            <div key={p.device} className="call-screen" data-testid="call-remote-screen">
              <Video stream={p.screenStream!} muted />
              <span className="call-name">Écran de {p.user.pseudo}</span>
            </div>
          ))}
          {c.screen && c.screenStream && (
            <div className="call-screen mine" data-testid="call-my-screen">
              <Video stream={c.screenStream} muted />
              <span className="call-name">Votre écran</span>
            </div>
          )}
        </div>
      )}
      <div className={'call-stage' + (others.length > 1 ? ' grid' : '')}>
        {others.map((p) => (
          <div key={p.device || p.user.id} className={'call-tile' + (p.connected || c.kind === 'direct' ? '' : ' waiting')} data-testid="call-peer" data-connected={p.connected}>
            {p.camera && hasVideo(p.stream) ? <Video stream={p.stream} muted /> : avatar(p.user.id, p.user.pseudo)}
            <span className="call-name">{p.user.pseudo}{p.muted && <MicOff size={14} />}{p.connected && <SignalBars rtt={p.rtt} label={'Ping avec ' + p.user.pseudo} size={12} />}</span>
          </div>
        ))}
        <div className="call-tile">
          {c.camera && c.localStream ? <Video stream={c.localStream} muted mirror /> : avatar(me.id, me.pseudo)}
          <span className="call-name">{me.pseudo}{c.muted && <MicOff size={14} />}</span>
        </div>
      </div>
      <p className="call-status">{statusText(c, elapsed)}{c.kind === 'direct' && c.status === 'active' && c.path ? ' · ' + pathLabel[c.path] : ''}</p>
    </div>
  )
}
