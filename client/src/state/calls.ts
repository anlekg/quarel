// One-to-one calls between friends, as cmd/quarelctl/calls.go: WebRTC peer to
// peer (DTLS-SRTP), signalling (complete SDP offer/answer, reject, hang-up) in
// Olm-encrypted messages between devices, so the Identity service sees neither
// addresses nor media parameters. The invite rings every trusted device of the
// friend; the first that answers wins, the others get a hang-up. When no
// direct path exists, the Identity service's TURN relay carries the (still
// encrypted) media — unless the user refused the relay.
//
// Between two apps, the caller adds a video transceiver (camera switched on
// and off without renegotiation) and a "quarel-call" data channel carrying
// {muted, camera}.
import { useSyncExternalStore } from 'react'
import type { DeviceInfo, PublicUser } from '../api/identity'
import type { CallSignal } from '../e2e/engine'
import { UserError } from '../lib/errors'
import { applyOutput, audioConstraints, onDeviceChange, videoConstraints } from '../lib/media'
import { prefs } from '../platform'
import { engine, identityAPI, onEngineEvent, socialState } from './social'
import { leaveVoice } from './voice'

const RING_TIMEOUT = 30_000
const CONNECT_TIMEOUT = 20_000

export type CallStatus = 'ringing' | 'incoming' | 'connecting' | 'active' | 'ended'
export type CallPath = 'local' | 'direct' | 'relay'

export interface CallSnapshot {
  id: string
  peer: PublicUser
  direction: 'out' | 'in'
  status: CallStatus
  ended?: string // why, for people
  startedAt?: number // connected at (ms)
  muted: boolean
  camera: boolean
  canVideo: boolean // the other side negotiated video (another app)
  remoteMuted: boolean
  remoteCamera: boolean
  path?: CallPath
  received: number // audio bytes received (diagnostics, tests)
  localStream: MediaStream | null
  remoteStream: MediaStream | null
}

interface Call {
  snap: CallSnapshot
  pc: RTCPeerConnection | null
  peerDevice?: DeviceInfo // the device we talk with
  devices?: DeviceInfo[] // outgoing: the friend's devices that ring
  invite?: CallSignal // incoming: the offer
  mic: MediaStreamTrack | null
  cam: MediaStreamTrack | null
  dc: RTCDataChannel | null
  timers: ReturnType<typeof setTimeout>[]
  stats?: ReturnType<typeof setInterval>
  audio: HTMLAudioElement
}

let call: Call | null = null
let snapshot: CallSnapshot | null = null
const listeners = new Set<() => void>()

function publish() {
  snapshot = call ? { ...call.snap } : null
  for (const l of listeners) l()
}

export function useCall(): CallSnapshot | null {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => snapshot,
  )
}

// --- settings ---

export const relayAllowed = () => prefs.get('call-relay', true)
export const setRelayAllowed = (on: boolean) => prefs.set('call-relay', on)

// --- helpers ---

function newId() {
  return Array.from(crypto.getRandomValues(new Uint8Array(12)), (b) => b.toString(16).padStart(2, '0')).join('')
}

async function rtcConfig(): Promise<RTCConfiguration> {
  const r = await identityAPI()!.iceServers().catch(() => ({ ice_servers: [], relay: false }))
  const relay = relayAllowed()
  return {
    iceServers: r.ice_servers.filter((s) => relay || !s.urls.some((u) => u.startsWith('turn'))).map((s) => ({ urls: s.urls, username: s.username, credential: s.credential })),
  }
}

async function gather(pc: RTCPeerConnection, desc: RTCSessionDescriptionInit): Promise<string> {
  const complete = new Promise<void>((resolve) => {
    if (pc.iceGatheringState === 'complete') return resolve()
    pc.addEventListener('icegatheringstatechange', () => pc.iceGatheringState === 'complete' && resolve())
  })
  await pc.setLocalDescription(desc)
  await Promise.race([complete, new Promise((r) => setTimeout(r, 15_000))]) // use what was found so far
  return pc.localDescription!.sdp
}

const isFriend = (userId: string) => socialState().friends.friends.find((f) => f.id === userId)

async function signal(devs: DeviceInfo[], s: Omit<CallSignal, 'from_user' | 'from_device' | 'at'>) {
  if (devs.length) await engine()?.sendCallSignal(devs, s).catch(() => {})
}

// --- ringing sounds (Web Audio, no files) ---

let ringer: { ctx: AudioContext; timer: ReturnType<typeof setInterval> } | null = null

function ring(kind: 'in' | 'out') {
  stopRing()
  try {
    const ctx = new AudioContext()
    const beep = (at: number, freq: number, len: number) => {
      const o = ctx.createOscillator()
      const g = ctx.createGain()
      o.frequency.value = freq
      g.gain.setValueAtTime(0.0001, ctx.currentTime + at)
      g.gain.exponentialRampToValueAtTime(kind === 'in' ? 0.15 : 0.06, ctx.currentTime + at + 0.02)
      g.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + at + len)
      o.connect(g).connect(ctx.destination)
      o.start(ctx.currentTime + at)
      o.stop(ctx.currentTime + at + len + 0.05)
    }
    const pattern = () => (kind === 'in' ? (beep(0, 880, 0.25), beep(0.35, 660, 0.35)) : beep(0, 440, 1))
    pattern()
    ringer = { ctx, timer: setInterval(pattern, kind === 'in' ? 2000 : 3000) }
  } catch {
    /* no audio output */
  }
}

function stopRing() {
  if (!ringer) return
  clearInterval(ringer.timer)
  ringer.ctx.close().catch(() => {})
  ringer = null
}

// --- lifecycle ---

function newCall(id: string, peer: PublicUser, direction: 'out' | 'in', status: CallStatus): Call {
  const audio = new Audio()
  audio.autoplay = true
  applyOutput(audio)
  return {
    snap: {
      id, peer, direction, status, muted: prefs.get('call-muted', false), camera: false, canVideo: false, remoteMuted: false, remoteCamera: false,
      received: 0, localStream: null, remoteStream: null,
    },
    pc: null, mic: null, cam: null, dc: null, timers: [], audio,
  }
}

function end(reason: string) {
  const c = call
  if (!c || c.snap.status === 'ended') return
  stopRing()
  for (const t of c.timers) clearTimeout(t)
  clearInterval(c.stats)
  c.pc?.close()
  c.mic?.stop()
  c.cam?.stop()
  c.audio.srcObject = null
  c.snap = { ...c.snap, status: 'ended', ended: reason, localStream: null, remoteStream: null }
  publish()
  setTimeout(() => {
    if (call === c) {
      call = null
      publish()
    }
  }, 4000)
}

async function newPeer(c: Call) {
  const pc = new RTCPeerConnection(await rtcConfig())
  c.pc = pc
  const remote = new MediaStream()
  c.snap.remoteStream = remote
  pc.ontrack = (ev) => {
    remote.addTrack(ev.track)
    if (ev.track.kind === 'audio') {
      c.audio.srcObject = new MediaStream([ev.track])
      c.audio.play().catch(() => {})
    }
    c.snap.remoteStream = new MediaStream(remote.getTracks()) // new object: React sees the change
    publish()
  }
  pc.onconnectionstatechange = () => {
    if (call !== c) return
    if (pc.connectionState === 'connected' && c.snap.status !== 'active') {
      c.snap.status = 'active'
      c.snap.startedAt = Date.now()
      stopRing()
      publish()
      watchStats(c)
    } else if (pc.connectionState === 'failed') {
      hangUp(c.snap.status === 'active' ? 'Connexion perdue.' : 'Connexion impossible : aucun chemin réseau entre vos appareils' + (relayAllowed() ? '.' : ' (relais refusé dans vos paramètres).'))
    }
  }
  pc.ondatachannel = (ev) => useChannel(c, ev.channel)
  return pc
}

function useChannel(c: Call, dc: RTCDataChannel) {
  c.dc = dc
  dc.onopen = () => sendState(c)
  dc.onmessage = (m) => {
    try {
      const st = JSON.parse(m.data as string) as { muted?: boolean; camera?: boolean }
      c.snap.remoteMuted = !!st.muted
      c.snap.remoteCamera = !!st.camera
      publish()
    } catch {
      /* ignore */
    }
  }
}

function sendState(c: Call) {
  if (c.dc?.readyState === 'open') c.dc.send(JSON.stringify({ muted: c.snap.muted, camera: c.snap.camera }))
}

// Path in use and audio received, from the connection statistics.
function watchStats(c: Call) {
  const tick = async () => {
    if (!c.pc || call !== c) return
    const stats = await c.pc.getStats().catch(() => null)
    if (!stats) return
    let pairId = ''
    stats.forEach((r) => {
      if (r.type === 'transport' && r.selectedCandidatePairId) pairId = r.selectedCandidatePairId
    })
    let received = 0
    stats.forEach((r) => {
      if (r.type === 'candidate-pair' && !pairId && r.nominated && r.state === 'succeeded') pairId = r.id
      if (r.type === 'inbound-rtp' && r.kind === 'audio') received += r.bytesReceived ?? 0
    })
    const pair = pairId ? stats.get(pairId) : null
    if (pair) {
      const local = stats.get(pair.localCandidateId)
      const remote = stats.get(pair.remoteCandidateId)
      // A relay candidate learnt from a check (prflx) keeps its low priority (type preference 0).
      const relayed = (x?: { candidateType?: string; priority?: number }) => x?.candidateType === 'relay' || (x?.priority !== undefined && x.priority < 2 ** 24)
      c.snap.path = relayed(local) || relayed(remote) ? 'relay' : local?.candidateType === 'host' && remote?.candidateType === 'host' ? 'local' : 'direct'
    }
    c.snap.received = received
    publish()
  }
  tick()
  c.stats = setInterval(tick, 2000)
}

async function microphone(c: Call) {
  try {
    const s = await navigator.mediaDevices.getUserMedia({ audio: audioConstraints() })
    c.mic = s.getAudioTracks()[0]
    c.mic.enabled = !c.snap.muted
  } catch {
    c.mic = null // no microphone: listen only
  }
}

// --- placing a call ---

export async function startCall(peer: PublicUser) {
  if (call && call.snap.status !== 'ended') return
  if (!isFriend(peer.id)) throw new UserError('Les appels sont réservés aux amis.')
  const e = engine()
  if (!e?.validated) throw new UserError('Cet appareil n’est pas encore validé.')
  const devices = await e.devicesOf([peer.id])
  if (!devices.length) throw new UserError(peer.pseudo + ' n’a aucun appareil capable de recevoir un appel.')
  leaveVoice()
  const c = newCall(newId(), peer, 'out', 'ringing')
  call = c
  publish()
  ring('out')
  await microphone(c)
  if (call !== c) return c.mic?.stop()
  const pc = await newPeer(c)
  const local = new MediaStream(c.mic ? [c.mic] : [])
  if (c.mic) pc.addTransceiver(c.mic, { direction: 'sendrecv', streams: [local] })
  else pc.addTransceiver('audio', { direction: 'recvonly' })
  pc.addTransceiver('video', { direction: 'sendrecv', streams: [local] })
  useChannel(c, pc.createDataChannel('quarel-call'))
  c.snap.canVideo = true // known for sure once answered
  const sdp = await gather(pc, await pc.createOffer())
  if (call !== c || c.snap.status === 'ended') return
  c.devices = devices
  await signal(devices, { call_id: c.snap.id, action: 'invite', sdp })
  c.timers.push(setTimeout(() => {
    if (call === c && c.snap.status === 'ringing') {
      signal(devices, { call_id: c.snap.id, action: 'hangup' })
      end('Pas de réponse de ' + peer.pseudo + '.')
    }
  }, RING_TIMEOUT))
}

async function onAnswer(c: Call, s: CallSignal) {
  const devices = c.devices ?? []
  const dev = devices.find((d) => d.device_id === s.from_device)
  if (!dev || !s.sdp || !c.pc) return
  c.peerDevice = dev
  stopRing()
  // Other devices of the friend stop ringing.
  signal(devices.filter((d) => d.device_id !== dev.device_id), { call_id: c.snap.id, action: 'hangup' })
  c.snap.status = 'connecting'
  publish()
  await c.pc.setRemoteDescription({ type: 'answer', sdp: s.sdp })
  c.snap.canVideo = c.pc.getTransceivers().some((t) => t.receiver.track.kind === 'video' && t.currentDirection !== 'inactive' && t.currentDirection !== 'recvonly')
  publish()
  connectTimeout(c)
}

function connectTimeout(c: Call) {
  c.timers.push(setTimeout(() => {
    if (call === c && c.snap.status === 'connecting') {
      hangUp('Connexion impossible dans le temps imparti : aucun chemin réseau' + (relayAllowed() ? '.' : ' (relais refusé dans vos paramètres).'))
    }
  }, CONNECT_TIMEOUT))
}

// --- receiving a call ---

async function onInvite(s: CallSignal) {
  const friend = isFriend(s.from_user)
  if (!friend || !s.sdp) return // calls are for friends only
  const e = engine()
  const dev = await e?.trustedDevice(s.from_user, s.from_device)
  if (!dev) return
  if (call && call.snap.status !== 'ended') {
    if (call.snap.id !== s.call_id) signal([dev], { call_id: s.call_id, action: 'reject' }) // busy
    return
  }
  const c = newCall(s.call_id, friend, 'in', 'incoming')
  c.invite = s
  c.peerDevice = dev
  call = c
  publish()
  const dnd = socialState().presence_setting === 'dnd' // do not disturb: no sound, no notification
  if (!dnd) ring('in')
  if (!dnd && !document.hasFocus()) {
    try {
      new Notification('Appel de ' + friend.pseudo, { body: 'Ouvrez Quarel pour répondre.', silent: true })
    } catch {
      /* notifications unavailable */
    }
  }
  c.timers.push(setTimeout(() => call === c && c.snap.status === 'incoming' && end('Appel manqué de ' + friend.pseudo + '.'), RING_TIMEOUT))
}

export async function acceptCall() {
  const c = call
  if (!c || c.snap.status !== 'incoming' || !c.invite?.sdp || !c.peerDevice) return
  stopRing()
  leaveVoice()
  c.snap.status = 'connecting'
  publish()
  await microphone(c)
  const pc = await newPeer(c)
  await pc.setRemoteDescription({ type: 'offer', sdp: c.invite.sdp })
  const local = new MediaStream(c.mic ? [c.mic] : [])
  for (const t of pc.getTransceivers()) {
    const kind = t.receiver.track.kind
    if (kind === 'audio') {
      if (c.mic) {
        await t.sender.replaceTrack(c.mic)
        t.sender.setStreams(local)
        t.direction = 'sendrecv'
      }
    } else if (kind === 'video') {
      t.direction = 'sendrecv'
      c.snap.canVideo = true
    }
  }
  const sdp = await gather(pc, await pc.createAnswer())
  if (call !== c || (c.snap.status as CallStatus) === 'ended') return
  await signal([c.peerDevice], { call_id: c.snap.id, action: 'answer', sdp })
  publish()
  connectTimeout(c)
}

export function declineCall() {
  const c = call
  if (!c || c.snap.status !== 'incoming') return
  if (c.peerDevice) signal([c.peerDevice], { call_id: c.snap.id, action: 'reject' })
  end('Appel refusé.')
}

// Ends the call (or cancels it while it rings).
export function hangUp(reason = 'Appel terminé.') {
  const c = call
  if (!c || c.snap.status === 'ended') return
  if (c.snap.status === 'incoming') return declineCall()
  const devices = c.peerDevice ? [c.peerDevice] : (c.devices ?? [])
  signal(devices, { call_id: c.snap.id, action: 'hangup' })
  end(reason)
}

function onSignal(s: CallSignal) {
  if (s.action === 'invite') return void onInvite(s)
  const c = call
  if (!c || c.snap.id !== s.call_id || s.from_user !== c.snap.peer.id || c.snap.status === 'ended') return
  if (c.peerDevice && s.from_device !== c.peerDevice.device_id && c.snap.status !== 'ringing') return // another device of theirs
  if (s.action === 'answer' && c.snap.direction === 'out' && c.snap.status === 'ringing') onAnswer(c, s).catch(() => hangUp('Réponse illisible.'))
  else if (s.action === 'reject' && c.snap.status === 'ringing') end(c.snap.peer.pseudo + ' a refusé l’appel.')
  else if (s.action === 'hangup') end(c.snap.status === 'incoming' ? 'Appel manqué de ' + c.snap.peer.pseudo + '.' : 'Appel terminé.')
}

onEngineEvent((ev) => {
  if (ev.kind === 'call_signal') onSignal(ev.signal)
})

// --- controls ---

export function toggleCallMute() {
  const c = call
  if (!c) return
  c.snap.muted = !c.snap.muted
  prefs.set('call-muted', c.snap.muted)
  if (c.mic) c.mic.enabled = !c.snap.muted
  sendState(c)
  publish()
}

export async function toggleCallCamera() {
  const c = call
  if (!c?.pc || !c.snap.canVideo) return
  const t = c.pc.getTransceivers().find((x) => x.receiver.track.kind === 'video')
  if (!t) return
  if (c.cam) {
    await t.sender.replaceTrack(null)
    c.cam.stop()
    c.cam = null
    c.snap.camera = false
    c.snap.localStream = null
  } else {
    const s = await navigator.mediaDevices.getUserMedia({ video: videoConstraints() })
    c.cam = s.getVideoTracks()[0]
    await t.sender.replaceTrack(c.cam)
    c.snap.camera = true
    c.snap.localStream = new MediaStream([c.cam])
  }
  sendState(c)
  publish()
}

// Signed out: the call ends.
export function closeCalls() {
  if (call) hangUp()
}

// Devices chosen in the settings apply to the call in progress.
onDeviceChange(async (kind) => {
  const c = call
  if (!c || c.snap.status === 'ended') return
  if (kind === 'audiooutput') return void applyOutput(c.audio)
  const t = c.pc?.getTransceivers().find((x) => x.receiver.track.kind === (kind === 'audioinput' ? 'audio' : 'video'))
  if (!t) return
  try {
    if (kind === 'audioinput' && c.mic) {
      const mic = (await navigator.mediaDevices.getUserMedia({ audio: audioConstraints() })).getAudioTracks()[0]
      mic.enabled = !c.snap.muted
      await t.sender.replaceTrack(mic)
      c.mic.stop()
      c.mic = mic
    } else if (kind === 'videoinput' && c.cam) {
      const cam = (await navigator.mediaDevices.getUserMedia({ video: videoConstraints() })).getVideoTracks()[0]
      await t.sender.replaceTrack(cam)
      c.cam.stop()
      c.cam = cam
      c.snap.localStream = new MediaStream([cam])
      publish()
    }
  } catch {
    /* device unavailable: keep the current one */
  }
})
