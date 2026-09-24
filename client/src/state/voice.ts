// The voice session: at most one voice channel at a time, on any server.
// Media goes through the server's LiveKit (signalling via /lk, audio and video
// directly over UDP/TCP); the Quarel server decides who may speak or stream.
import { useSyncExternalStore } from 'react'
import {
  ConnectionState, Participant, RemoteParticipant, Room, RoomEvent, Track, type RemoteTrack,
  type LocalTrackPublication, type LocalVideoTrack, type RemoteTrackPublication, type TrackPublication,
} from 'livekit-client'
import { ApiError } from '../api/http'
import type { ServerConn } from './servers'

export interface VideoTile {
  key: string // track SID
  memberId: string // LiveKit identity = member ID ("" for us)
  local: boolean
  source: 'camera' | 'screen'
  track: Track
}

export interface VoiceSnapshot {
  conn: ServerConn
  channelId: number
  status: 'connecting' | 'connected' | 'reconnecting' | 'error'
  error?: string
  muted: boolean // our choice
  deafened: boolean // our choice
  camera: boolean
  screen: boolean
  canSpeak: boolean // permission and not muted by moderation
  canStream: boolean
  canListen: boolean // not deafened by moderation
  speaking: Set<string> // member IDs
  participants: string[] // member IDs connected to the room (us included)
  videos: VideoTile[]
}

let snap: VoiceSnapshot | null = null
let room: Room | null = null
const listeners = new Set<() => void>()
// Our choices survive leaving and rejoining (like Discord).
const prefs = { muted: false, deafened: false }

function emit(patch: Partial<VoiceSnapshot> = {}) {
  if (snap) snap = { ...snap, ...patch }
  for (const l of listeners) l()
}

export function useVoice(): VoiceSnapshot | null {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => snap,
  )
}

export function currentVoice() {
  return snap
}

// --- remote audio: one <audio> element per track, muted while deafened ---

const audioEls = new Map<string, HTMLMediaElement>()

function attachAudio(track: RemoteTrack, pub: RemoteTrackPublication) {
  const el = track.attach()
  el.muted = prefs.deafened
  el.dataset.quarelVoice = pub.trackSid
  document.body.append(el)
  audioEls.set(pub.trackSid, el)
}

function detachAudio(sid: string) {
  const el = audioEls.get(sid)
  if (el) {
    el.remove()
    audioEls.delete(sid)
  }
}

function applyDeafen() {
  for (const el of audioEls.values()) el.muted = prefs.deafened || !snap?.canListen
}

// --- state from the room ---

function sourceOf(pub: TrackPublication): 'camera' | 'screen' | null {
  if (pub.source === Track.Source.Camera) return 'camera'
  if (pub.source === Track.Source.ScreenShare) return 'screen'
  return null
}

function refresh() {
  if (!room || !snap) return
  const lp = room.localParticipant
  const perm = lp.permissions
  const sources = (perm?.canPublishSources ?? []) as unknown[]
  const allows = (n: number, name: string) => !!perm?.canPublish && (sources.length === 0 || sources.includes(n) || sources.includes(name))
  const videos: VideoTile[] = []
  const add = (p: Participant, pub: TrackPublication, local: boolean) => {
    const src = sourceOf(pub)
    if (src && pub.track && pub.kind === Track.Kind.Video && !pub.isMuted) {
      videos.push({ key: pub.trackSid, memberId: local ? '' : p.identity, local, source: src, track: pub.track })
    }
  }
  for (const pub of lp.trackPublications.values()) add(lp, pub, true)
  for (const p of room.remoteParticipants.values()) for (const pub of p.trackPublications.values()) add(p, pub, false)
  emit({
    canSpeak: allows(2, 'MICROPHONE'),
    canStream: allows(1, 'CAMERA') || allows(3, 'SCREEN_SHARE'),
    canListen: perm?.canSubscribe !== false,
    camera: !!lp.getTrackPublication(Track.Source.Camera)?.track && lp.isCameraEnabled,
    screen: lp.isScreenShareEnabled,
    participants: [lp.identity, ...[...room.remoteParticipants.values()].map((p) => p.identity)],
    videos,
  })
  applyDeafen()
}

async function tellServer() {
  if (!snap) return
  const conn = snap.conn
  await conn.api((c) => c.voiceState({ self_mute: prefs.muted || prefs.deafened, self_deaf: prefs.deafened })).catch(() => {})
}

// --- actions ---

export async function joinVoice(conn: ServerConn, channelId: number) {
  if (snap?.conn === conn && snap.channelId === channelId && snap.status !== 'error') return
  await leaveVoice(false)
  snap = {
    conn, channelId, status: 'connecting', muted: prefs.muted, deafened: prefs.deafened, camera: false, screen: false,
    canSpeak: false, canStream: false, canListen: true, speaking: new Set(), participants: [], videos: [],
  }
  emit()
  conn.onVoiceEvent = (event, channel) => {
    if (event === 'move' && channel) joinVoice(conn, channel).catch(() => {})
    if (event === 'removed') leaveVoice(false)
  }
  let j
  try {
    j = await conn.api((c) => c.voiceJoin(channelId))
  } catch (e) {
    emit({ status: 'error', error: e instanceof ApiError ? e.code : 'network' })
    return
  }
  const r = new Room({ adaptiveStream: true, dynacast: true })
  room = r
  r.on(RoomEvent.TrackSubscribed, (track, pub) => {
    if (track.kind === Track.Kind.Audio) attachAudio(track, pub)
    refresh()
  })
    .on(RoomEvent.TrackUnsubscribed, (_t, pub) => {
      detachAudio(pub.trackSid)
      refresh()
    })
    .on(RoomEvent.TrackMuted, refresh)
    .on(RoomEvent.TrackUnmuted, refresh)
    .on(RoomEvent.LocalTrackPublished, refresh)
    .on(RoomEvent.LocalTrackUnpublished, refresh)
    .on(RoomEvent.ParticipantConnected, refresh)
    .on(RoomEvent.ParticipantDisconnected, (p: RemoteParticipant) => {
      for (const pub of p.trackPublications.values()) detachAudio(pub.trackSid)
      refresh()
    })
    .on(RoomEvent.ActiveSpeakersChanged, (speakers) => emit({ speaking: new Set(speakers.map((p) => p.identity)) }))
    .on(RoomEvent.ParticipantPermissionsChanged, (_prev, p) => {
      if (p.isLocal) refresh()
    })
    .on(RoomEvent.ConnectionStateChanged, (st) => {
      if (st === ConnectionState.Reconnecting || st === ConnectionState.SignalReconnecting) emit({ status: 'reconnecting' })
      if (st === ConnectionState.Connected) emit({ status: 'connected' })
    })
    .on(RoomEvent.Disconnected, () => {
      if (room === r) leaveVoice(false) // kicked, moved away, lost connection for good
    })
  try {
    await r.connect(j.url, j.token)
  } catch {
    if (room === r) {
      room = null
      emit({ status: 'error', error: 'voice_unreachable' })
    }
    return
  }
  if (room !== r) return
  emit({ status: 'connected' })
  refresh()
  if (snap && snap.canSpeak && !prefs.muted && !prefs.deafened) {
    await r.localParticipant.setMicrophoneEnabled(true).catch(() => {
      prefs.muted = true
      emit({ muted: true, error: 'mic_unavailable' })
    })
  }
  await tellServer()
  refresh()
}

export async function leaveVoice(tellServerToo = true) {
  const r = room
  const s = snap
  room = null
  snap = null
  for (const sid of [...audioEls.keys()]) detachAudio(sid)
  if (s) s.conn.onVoiceEvent = null
  emit()
  if (r) await r.disconnect().catch(() => {})
  if (s && tellServerToo) await s.conn.api((c) => c.voiceLeave()).catch(() => {})
}

export async function toggleMute() {
  if (!snap) return
  prefs.muted = !(prefs.muted || prefs.deafened)
  if (!prefs.muted) prefs.deafened = false
  emit({ muted: prefs.muted, deafened: prefs.deafened })
  applyDeafen()
  if (room && snap.canSpeak) await room.localParticipant.setMicrophoneEnabled(!prefs.muted).catch(() => {})
  await tellServer()
}

export async function toggleDeafen() {
  if (!snap) return
  prefs.deafened = !prefs.deafened
  if (prefs.deafened) prefs.muted = true
  emit({ muted: prefs.muted, deafened: prefs.deafened })
  applyDeafen()
  if (room && snap.canSpeak) await room.localParticipant.setMicrophoneEnabled(!prefs.muted).catch(() => {})
  await tellServer()
}

export async function toggleCamera() {
  if (!room || !snap) return
  const lp = room.localParticipant
  if (snap.camera) {
    // Unpublish (not just mute) so the server and the others know it is off,
    // and the camera light goes out.
    const pub = lp.getTrackPublication(Track.Source.Camera)
    if (pub?.track) await lp.unpublishTrack(pub.track as LocalVideoTrack, true).catch(() => {})
  } else {
    await lp.setCameraEnabled(true).catch(() => emit({ error: 'camera_unavailable' }))
  }
  refresh()
}

export async function toggleScreen() {
  if (!room || !snap) return
  await room.localParticipant.setScreenShareEnabled(!snap.screen, { audio: true }).catch((e: Error) => {
    if (e?.name !== 'NotAllowedError') emit({ error: 'screen_unavailable' })
  })
  refresh()
}

export type { LocalTrackPublication }
