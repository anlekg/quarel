// Voice: the channel view (tiles, controls), the "connected" bar above the
// user bar, and the participants listed under voice channels.
import { useEffect, useRef, useState } from 'react'
import type { Channel, Member, Ready, VoiceState } from '../api/community'
import { Avatar } from '../components/Avatar'
import { Alert, BackButton } from '../components/ui'
import { Camera, Hangup, Headphones, HeadphonesOff, Mic, MicOff, Monitor, Speaker, Hand, Stage } from '../components/icons'
import { can, memberAvatar } from '../lib/community'
import { errorMessage } from '../lib/errors'
import { ApiError } from '../api/http'
import { isDesktop, chooseScreenSource } from '../platform'
import { ScreenPicker } from '../components/ScreenPicker'
import { SignalBars } from '../components/SignalBars'
import type { ServerConn } from '../state/servers'
import {
  joinVoice, leaveVoice, toggleCamera, toggleDeafen, toggleMute, toggleScreen, useVoice, type VideoTile,
} from '../state/voice'

const voiceErrors: Record<string, string> = {
  voice_disabled: 'Le vocal est désactivé sur ce serveur.',
  voice_unreachable: 'Impossible de joindre le serveur vocal (ports 7881/7882 fermés ?).',
  mic_unavailable: 'Micro indisponible : autorisez-le, ou branchez-en un. Vous écoutez sans parler.',
  camera_unavailable: 'Caméra indisponible.',
  screen_unavailable: 'Partage d’écran impossible.',
}

function voiceError(code?: string) {
  if (!code) return ''
  return voiceErrors[code] ?? errorMessage(new ApiError(0, code, ''))
}

// Participants shown under a voice channel in the channel list.
export function VoiceMembers({ ready, channel, conn }: { ready: Ready; channel: Channel; conn: ServerConn }) {
  const v = useVoice()
  const inside = ready.voice_states.filter((s) => s.channel_id === channel.id)
  if (!inside.length) return null
  const speaking = v && v.conn === conn ? v.speaking : new Set<string>()
  return (
    <div className="voice-users">
      {inside.map((s) => {
        const m = ready.members.find((x) => x.id === s.member_id)
        return (
          <span key={s.member_id} className={'vu' + (speaking.has(s.member_id) ? ' speaking' : '')}>
            <span className="mini avatar-wrap">{m && <Avatar id={m.subject || m.id} name={m.display_name} src={memberAvatar(m)} size={20} />}</span>
            <span className="n">{m?.display_name ?? '…'}</span>
            {s.video && <Camera size={13} aria-label="caméra" />}
            {s.screen && <Monitor size={13} aria-label="partage d'écran" />}
            {s.server_deaf ? <HeadphonesOff size={13} className="mod" aria-label="son coupé par la modération" />
              : s.self_deaf ? <HeadphonesOff size={13} aria-label="sourdine" />
                : s.server_mute ? <MicOff size={13} className="mod" aria-label="micro coupé par la modération" />
                  : s.self_mute ? <MicOff size={13} aria-label="micro coupé" /> : null}
          </span>
        )
      })}
    </div>
  )
}

// "Vocal connecté" panel, above the user bar, visible everywhere.
export function VoiceBar({ onOpen }: { onOpen: (conn: ServerConn, channelId: number) => void }) {
  const v = useVoice()
  if (!v) return null
  const ch = v.conn.state.ready?.channels.find((c) => c.id === v.channelId)
  const server = v.conn.state.ready?.server.name ?? v.conn.saved.name
  const status = v.status === 'connected' ? <b>Vocal connecté</b>
    : v.status === 'error' ? <b className="err">Vocal : échec</b>
      : <b className="warn">{v.status === 'reconnecting' ? 'Reconnexion…' : 'Connexion…'}</b>
  return (
    <div className="voicebar" role="region" aria-label="Vocal">
      <div className="st">
        {v.status === 'connected' && <SignalBars rtt={v.rtt} label="Ping vers le serveur vocal" />}
        <div className="t">
          {status}
          <span onClick={() => onOpen(v.conn, v.channelId)} title="Afficher le salon vocal">{(ch?.name ?? 'Salon vocal') + ' / ' + server}</span>
        </div>
        <button className="icon-btn hang" aria-label="Quitter le vocal" title="Quitter le vocal" onClick={() => leaveVoice()}><Hangup size={18} /></button>
      </div>
      <div className="st">
        <button className={'icon-btn' + (v.muted ? ' on-danger' : '')} aria-pressed={v.muted} aria-label={v.muted ? 'Réactiver le micro' : 'Couper le micro'}
          title={v.canSpeak ? (v.muted ? 'Réactiver le micro' : 'Couper le micro') : 'Vous ne pouvez pas parler ici'} onClick={toggleMute}>
          {v.muted || !v.canSpeak ? <MicOff size={18} /> : <Mic size={18} />}
        </button>
        <button className={'icon-btn' + (v.deafened ? ' on-danger' : '')} aria-pressed={v.deafened} aria-label={v.deafened ? 'Réactiver le son' : 'Sourdine'}
          title={v.deafened ? 'Réactiver le son' : 'Sourdine'} onClick={toggleDeafen}>
          {v.deafened ? <HeadphonesOff size={18} /> : <Headphones size={18} />}
        </button>
      </div>
    </div>
  )
}

function VideoView({ tile }: { tile: VideoTile }) {
  const ref = useRef<HTMLVideoElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    tile.track.attach(el)
    return () => {
      tile.track.detach(el)
    }
  }, [tile.track])
  return <video ref={ref} autoPlay playsInline muted />
}

// The voice channel itself (main area).
export function VoiceView({ conn, ready, channel }: { conn: ServerConn; ready: Ready; channel: Channel }) {
  const v = useVoice()
  const here = v && v.conn === conn && v.channelId === channel.id ? v : null
  const [picking, setPicking] = useState(false)
  const connectOK = can(ready, channel.id, 'connect')
  const inside = ready.voice_states.filter((s) => s.channel_id === channel.id)
  const name = (id: string) => ready.members.find((m) => m.id === id)
  // A stage: speakers (and moderators) on stage, everyone else in the audience.
  const stage = !!channel.stage
  const stateOf = (id: string) => inside.find((s) => s.member_id === id)
  const onStage = (id: string) => !stage || (id === ready.member.id ? !!here?.canSpeak : !!stateOf(id)?.can_speak)
  const mine = stateOf(ready.member.id)
  const moderator = can(ready, channel.id, 'mute_members')
  const [stageError, setStageError] = useState('')
  const act = (p: Promise<unknown>) => {
    setStageError('')
    p.catch((e) => setStageError(errorMessage(e)))
  }
  const audience = here ? here.participants.filter((id) => !onStage(id)) : []

  return (
    <div className="voice-view">
      <header className="channel-head" style={{ background: 'var(--bg-1)' }}>
        <BackButton />
        {stage ? <Stage /> : <Speaker />}
        <span className="title">{channel.name}</span>
        {stage && <span className="e2e-badge">Scène</span>}
        <span className="topic">{inside.length} personne{inside.length > 1 ? 's' : ''}{here?.status === 'reconnecting' ? ' · reconnexion…' : ''}</span>
      </header>
      {!here ? (
        <div className="voice-join">
          <h2>{channel.name}</h2>
          <p>{inside.length ? inside.map((s) => name(s.member_id)?.display_name ?? '…').join(', ') + (inside.length > 1 ? ' sont là.' : ' est là.') : 'Personne pour l’instant.'}</p>
          {v && v.conn === conn && v.status === 'error' && v.channelId === channel.id && <Alert kind="error">{voiceError(v.error)}</Alert>}
          <button className="btn btn-primary" disabled={!connectOK} onClick={() => joinVoice(conn, channel.id)}>
            {connectOK ? 'Rejoindre le vocal' : 'Vous ne pouvez pas rejoindre ce salon'}
          </button>
        </div>
      ) : (
        <>
          {here.error && here.status !== 'error' && <div style={{ padding: '8px 16px 0' }}><Alert kind="warn">{voiceError(here.error)}</Alert></div>}
          {here.status === 'error' && <div style={{ padding: 16 }}><Alert kind="error">{voiceError(here.error)}</Alert></div>}
          {stageError && <div style={{ padding: '8px 16px 0' }}><Alert kind="error">{stageError}</Alert></div>}
          <section className="voice-grid" aria-label={stage ? 'Sur scène' : 'Participants'}>
            {here.videos.map((t) => (
              <div key={t.key} className={'tile' + (here.speaking.has(t.local ? ready.member.id : t.memberId) && t.source === 'camera' ? ' speaking' : '')}>
                <VideoView tile={t} />
                <span className="label">{t.source === 'screen' && <Monitor size={14} />}
                  {t.local ? (t.source === 'screen' ? 'Votre écran' : 'Vous') : (name(t.memberId)?.display_name ?? '…') + (t.source === 'screen' ? ' — écran' : '')}
                </span>
              </div>
            ))}
            {here.participants.filter((id) => onStage(id) && !here.videos.some((t) => t.source === 'camera' && (t.local ? ready.member.id : t.memberId) === id))
              .map((id) => !stage ? <PersonTile key={id} m={name(id)} state={stateOf(id)} me={id === ready.member.id} speaking={here.speaking.has(id)} /> : (
                <div key={id} className="stage-slot">
                  <PersonTile m={name(id)} state={stateOf(id)} me={id === ready.member.id} speaking={here.speaking.has(id)} />
                  {moderator && id !== ready.member.id && stateOf(id)?.speaker && (
                    <button className="btn btn-ghost btn-sm" onClick={() => act(conn.api((c) => c.moderateVoice(id, { speaker: false })))}>Renvoyer dans le public</button>
                  )}
                </div>
              ))}
          </section>
          {stage && (
            <section className="stage-audience" aria-label="Public">
              <h3>Public — {audience.length}</h3>
              {audience.map((id) => {
                const st = stateOf(id)
                return (
                  <div key={id} className="stage-listener" data-testid="listener">
                    <span className="grow">{name(id)?.display_name ?? '…'}{id === ready.member.id ? ' (vous)' : ''}</span>
                    {st?.hand_raised && <Hand size={16} aria-label="main levée" />}
                    {moderator && id !== ready.member.id && (
                      <button className="btn btn-ghost btn-sm" onClick={() => act(conn.api((c) => c.moderateVoice(id, { speaker: true })))}>Inviter à parler</button>
                    )}
                  </div>
                )
              })}
            </section>
          )}
          <footer className="voice-controls">
            <button className={'round' + (here.muted ? ' off' : '')} disabled={!here.canSpeak} onClick={toggleMute}
              aria-pressed={here.muted} aria-label={here.muted ? 'Réactiver le micro' : 'Couper le micro'} title={here.canSpeak ? '' : 'Vous ne pouvez pas parler ici'}>
              {here.muted || !here.canSpeak ? <MicOff size={22} /> : <Mic size={22} />}
            </button>
            <button className={'round' + (here.deafened ? ' off' : '')} onClick={toggleDeafen} aria-pressed={here.deafened} aria-label={here.deafened ? 'Réactiver le son' : 'Sourdine'}>
              {here.deafened ? <HeadphonesOff size={22} /> : <Headphones size={22} />}
            </button>
            <button className={'round' + (here.camera ? ' on' : '')} disabled={!here.canStream} onClick={toggleCamera}
              aria-pressed={here.camera} aria-label={here.camera ? 'Couper la caméra' : 'Activer la caméra'}><Camera size={22} /></button>
            <button className={'round' + (here.screen ? ' on' : '')} disabled={!here.canStream}
              onClick={() => (here.screen || !isDesktop ? toggleScreen() : setPicking(true))}
              aria-pressed={here.screen} aria-label={here.screen ? 'Arrêter le partage d’écran' : 'Partager l’écran'}><Monitor size={22} /></button>
            {stage && !here.canSpeak && (
              <button className={'round' + (mine?.hand_raised ? ' on' : '')} aria-pressed={!!mine?.hand_raised}
                aria-label={mine?.hand_raised ? 'Baisser la main' : 'Lever la main'} title={mine?.hand_raised ? 'Baisser la main' : 'Lever la main pour demander la parole'}
                onClick={() => act(conn.api((c) => c.voiceState({ hand_raised: !mine?.hand_raised })))}><Hand size={22} /></button>
            )}
            {stage && mine?.speaker && (
              <button className="btn btn-ghost btn-sm" onClick={() => act(conn.api((c) => c.voiceState({ speaker: false })))}>Quitter la scène</button>
            )}
            <button className="leave-btn" onClick={() => leaveVoice()}><Hangup size={20} />Quitter</button>
          </footer>
        </>
      )}
      {picking && <ScreenPicker onClose={() => setPicking(false)} onPick={async (id) => {
        setPicking(false)
        await chooseScreenSource(id)
        await toggleScreen()
      }} />}
    </div>
  )
}

function PersonTile({ m, state, me, speaking }: { m?: Member; state?: VoiceState; me: boolean; speaking: boolean }) {
  return (
    <div className="tile">
      <span className={'ring' + (speaking ? ' speaking' : '')}>
        {m ? <Avatar id={m.subject || m.id} name={m.display_name} src={memberAvatar(m)} size={88} /> : <Avatar id="?" name="?" size={88} />}
      </span>
      <span className="who">
        {(m?.display_name ?? '…') + (me ? ' (vous)' : '')}
        {state?.server_mute || state?.self_mute ? <MicOff size={16} className={state?.server_mute ? 'mod' : ''} /> : null}
        {state?.self_deaf || state?.server_deaf ? <HeadphonesOff size={16} /> : null}
      </span>
      {state?.server_mute && <span className="mod-note">Micro coupé par la modération</span>}
      {state?.server_deaf && <span className="mod-note">Son coupé par la modération</span>}
    </div>
  )
}
