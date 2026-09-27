// Calls: one to one between friends, as cmd/quarelctl/calls.go, and between
// the members of a group conversation (apps from 0.4.0). WebRTC peer to peer
// (DTLS-SRTP), signalling (complete SDP offers and answers, no trickle ICE) in
// Olm-encrypted messages between devices, so the Identity service sees neither
// addresses nor media parameters. When no direct path exists, the Identity
// service's TURN relay carries the (still encrypted) media — unless the user
// refused the relay.
//
// One to one: the invite rings every trusted device of the friend; the first
// that answers wins, the others get a hang-up. Between two apps, the caller
// adds a video transceiver (camera switched on and off without
// renegotiation) and a "quarel-call" data channel carrying {muted, camera,
// screen, caps}. Screen sharing (apps from 0.4.0, announced by caps:
// "screen") adds its transceivers once the call runs: the offer and the
// answer then travel over that data channel (DTLS between the two devices the
// Olm-signalled SDP authenticated), in "perfect negotiation" (the callee
// yields on collision). The first video and audio transceivers carry the
// camera and the microphone, the later ones the screen and its sound.
//
// Groups: a mesh, one connection per pair of participants, GROUP_CALL_MAX
// people at most. Joining sends "join" to the devices of the members this
// device confirmed (see E2E.observe); "start" makes them ring. Every device
// already in the call answers a join with an offer (or "full"), carrying all
// four transceivers from the start: sharing a screen needs no renegotiation.
// Signals are accepted only from confirmed members of the conversation.
import { useSyncExternalStore } from 'react'
import type { Conversation, DeviceInfo, PublicUser } from '../api/identity'
import type { CallSignal } from '../e2e/engine'
import { UserError } from '../lib/errors'
import { applyOutput, audioConstraints, onDeviceChange, videoConstraints } from '../lib/media'
import { prefs, showWindow } from '../platform'
import { applyVolume, effectiveVolume, onVolumeChange, personKey, releaseVolume } from '../lib/volume'
import { currentAccount } from './account'
import { engine, identityAPI, onEngineEvent, socialState } from './social'
import { leaveVoice } from './voice'

const RING_TIMEOUT = 30_000
const CONNECT_TIMEOUT = 20_000
export const GROUP_CALL_MAX = 6

export type CallStatus = 'ringing' | 'incoming' | 'connecting' | 'active' | 'ended'
export type CallPath = 'local' | 'direct' | 'relay'

// Someone on the other end of a connection.
export interface PeerSnapshot {
  device: string
  user: PublicUser
  connected: boolean
  muted: boolean
  camera: boolean
  screen: boolean
  canScreen: boolean // their app can receive a shared screen (0.4.0+)
  path?: CallPath
  rtt?: number // round trip with them (ms)
  received: number // audio bytes received (diagnostics, tests)
  stream: MediaStream | null // their microphone and camera
  screenStream: MediaStream | null // their screen
}

export interface CallSnapshot {
  id: string
  kind: 'direct' | 'group'
  convId?: string // group
  title: string // the friend's pseudo, or the group's name
  peer: PublicUser // the friend; in a group, who rang
  direction: 'out' | 'in'
  status: CallStatus // group: "ringing" while nobody else is connected
  ended?: string // why, for people
  startedAt?: number // connected at (ms)
  muted: boolean
  camera: boolean
  canVideo: boolean // one to one: the other side negotiated video (another app)
  screen: boolean // sharing my screen
  canScreen: boolean
  localStream: MediaStream | null
  screenStream: MediaStream | null // my shared screen (preview)
  peers: PeerSnapshot[]
  // One to one: the friend's side (peers[0]).
  remoteMuted: boolean
  remoteCamera: boolean
  remoteScreen: boolean
  path?: CallPath
  rtt?: number // the friend (one to one), the slowest connected person (group)
  received: number
  remoteStream: MediaStream | null
  remoteScreenStream: MediaStream | null
}

interface Link {
  key: string // device id (one to one: "peer")
  device: DeviceInfo | null // one to one, outgoing: known once answered
  pc: RTCPeerConnection
  dc: RTCDataChannel | null
  polite: boolean // yields on an offer collision (the side that answered first)
  makingOffer: boolean
  needOffer: boolean
  audio: HTMLAudioElement
  screenAudio: HTMLAudioElement
  stats?: ReturnType<typeof setInterval>
  timer?: ReturnType<typeof setTimeout>
  snap: PeerSnapshot
}

interface Call {
  snap: Omit<CallSnapshot, 'peers' | 'remoteMuted' | 'remoteCamera' | 'remoteScreen' | 'path' | 'rtt' | 'received' | 'remoteStream' | 'remoteScreenStream'>
  links: Map<string, Link>
  config: Promise<RTCConfiguration>
  devices?: DeviceInfo[] // one to one, outgoing: the friend's devices that ring
  invite?: CallSignal // one to one, incoming: the offer
  inviter?: DeviceInfo // one to one, incoming: the device that rang
  connected: boolean // someone was connected at least once
  mic: MediaStreamTrack | null
  cam: MediaStreamTrack | null
  screen: MediaStream | null // shared screen (video, maybe sound)
  timers: ReturnType<typeof setTimeout>[]
}

let call: Call | null = null
let snapshot: CallSnapshot | null = null
const listeners = new Set<() => void>()

function publish() {
  if (!call) {
    snapshot = null
  } else {
    const peers = [...call.links.values()].map((l) => ({ ...l.snap }))
    const p = call.snap.kind === 'direct' ? peers[0] : undefined
    snapshot = {
      ...call.snap, peers,
      remoteMuted: !!p?.muted, remoteCamera: !!p?.camera, remoteScreen: !!p?.screen, path: p?.path, received: p?.received ?? 0,
      rtt: p ? p.rtt : peers.reduce<number | undefined>((m, x) => (x.connected && x.rtt !== undefined ? Math.max(m ?? 0, x.rtt) : m), undefined),
      remoteStream: p?.stream ?? null, remoteScreenStream: p?.screenStream ?? null,
    }
  }
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

// --- group calls going on (from the join and leave signals) ---

interface GroupCallInfo {
  id: string
  devices: Record<string, { user: string; at: number }>
}

let groupCalls: Record<string, GroupCallInfo> = {}
const groupListeners = new Set<() => void>()
const GROUP_INFO_TTL = 6 * 3600_000

function noteGroupCall(convId: string, callId: string, device: string, user: string, present: boolean) {
  const cur = groupCalls[convId]
  if (!present && cur?.id !== callId) return
  const info: GroupCallInfo = cur?.id === callId ? { id: callId, devices: { ...cur.devices } } : { id: callId, devices: {} }
  if (present) info.devices[device] = { user, at: Date.now() }
  else delete info.devices[device]
  groupCalls = { ...groupCalls }
  if (Object.keys(info.devices).length) groupCalls[convId] = info
  else delete groupCalls[convId]
  for (const l of groupListeners) l()
}

// The call going on in a group, as far as this device heard: its id and how
// many devices are in it.
export function useGroupCall(convId: string): { id: string; count: number } | null {
  const all = useSyncExternalStore(
    (l) => {
      groupListeners.add(l)
      return () => groupListeners.delete(l)
    },
    () => groupCalls,
  )
  const info = all[convId]
  if (!info) return null
  const live = Object.values(info.devices).filter((d) => Date.now() - d.at < GROUP_INFO_TTL).length
  return live ? { id: info.id, count: live } : null
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
const me = () => engine()?.userId ?? ''
const noPath = () => 'aucun chemin réseau' + (relayAllowed() ? '' : ' (relais refusé dans vos paramètres)')

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

function notify(title: string) {
  if (document.hasFocus()) return
  try {
    const n = new Notification(title, { body: 'Ouvrez Quarel pour répondre.', silent: true })
    n.onclick = () => {
      showWindow()
      n.close()
    }
  } catch {
    /* notifications unavailable */
  }
}

// --- lifecycle ---

function newCall(id: string, kind: 'direct' | 'group', title: string, peer: PublicUser, direction: 'out' | 'in', status: CallStatus, convId?: string): Call {
  return {
    snap: {
      id, kind, convId, title, peer, direction, status, muted: prefs.get('call-muted', false), camera: false, canVideo: kind === 'group',
      screen: false, canScreen: kind === 'group', localStream: null, screenStream: null,
    },
    links: new Map(), config: rtcConfig(), connected: false, mic: null, cam: null, screen: null, timers: [],
  }
}

const busy = () => !!call && call.snap.status !== 'ended'

function end(reason: string) {
  const c = call
  if (!c || c.snap.status === 'ended') return
  stopRing()
  for (const t of c.timers) clearTimeout(t)
  for (const l of c.links.values()) {
    closeLink(l)
    l.snap = { ...l.snap, stream: null, screenStream: null }
  }
  c.mic?.stop()
  c.cam?.stop()
  c.screen?.getTracks().forEach((t) => t.stop())
  c.snap = { ...c.snap, status: 'ended', ended: reason, screen: false, localStream: null, screenStream: null }
  publish()
  setTimeout(() => {
    if (call === c) {
      call = null
      publish()
    }
  }, 4000)
}

// Forgets a call that only rang here (answered elsewhere, missed): no message.
function drop(c: Call) {
  if (call !== c) return
  stopRing()
  for (const t of c.timers) clearTimeout(t)
  call = null
  publish()
}

// Each person's own volume (lib/volume.ts): their voice and their screen's sound.
function applyLinkVolume(l: Link) {
  const issuer = currentAccount()?.issuer ?? ''
  const v = effectiveVolume(personKey(issuer, l.snap.user.id))
  for (const el of [l.audio, l.screenAudio]) if (el.srcObject) applyVolume(el, v)
}
onVolumeChange(() => {
  for (const l of call?.links.values() ?? []) applyLinkVolume(l)
})

function closeLink(l: Link) {
  releaseVolume(l.audio)
  releaseVolume(l.screenAudio)
  clearInterval(l.stats)
  clearTimeout(l.timer)
  l.pc.close()
  l.audio.srcObject = null
  l.screenAudio.srcObject = null
}

function removeLink(c: Call, key: string) {
  const l = c.links.get(key)
  if (!l) return
  closeLink(l)
  c.links.delete(key)
  if (c.snap.kind === 'group' && c.snap.status === 'active' && ![...c.links.values()].some((x) => x.snap.connected)) c.snap.status = 'ringing' // alone again
  publish()
}

function output() {
  const a = new Audio()
  a.autoplay = true
  applyOutput(a)
  return a
}

// A connection with one device. The caller of a one to one call and, in a
// group, the device already in the call make the offer.
async function newLink(c: Call, key: string, user: PublicUser, device: DeviceInfo | null, polite: boolean): Promise<Link> {
  const pc = new RTCPeerConnection(await c.config)
  const l: Link = {
    key, device, pc, dc: null, polite, makingOffer: false, needOffer: false, audio: output(), screenAudio: output(),
    snap: { device: device?.device_id ?? '', user, connected: false, muted: false, camera: false, screen: false, canScreen: false, received: 0, stream: null, screenStream: null },
  }
  const old = c.links.get(key)
  if (old) closeLink(old)
  c.links.set(key, l)
  const remote = new MediaStream()
  const remoteScreen = new MediaStream()
  pc.ontrack = (ev) => {
    const screen = isScreen(pc, ev.transceiver)
    const into = screen ? remoteScreen : remote
    for (const t of into.getTracks()) if (t.kind === ev.track.kind) into.removeTrack(t)
    into.addTrack(ev.track)
    if (ev.track.kind === 'audio') {
      const el = screen ? l.screenAudio : l.audio
      releaseVolume(el)
      el.srcObject = new MediaStream([ev.track])
      el.play().catch(() => {})
      applyLinkVolume(l)
    }
    // New objects: React sees the change.
    l.snap.stream = new MediaStream(remote.getTracks())
    l.snap.screenStream = new MediaStream(remoteScreen.getTracks())
    publish()
  }
  pc.onconnectionstatechange = () => {
    if (call !== c || c.links.get(key) !== l) return
    if (pc.connectionState === 'connected' && !l.snap.connected) {
      l.snap.connected = true
      clearTimeout(l.timer)
      c.connected = true
      if (c.snap.status !== 'active') {
        c.snap.status = 'active'
        c.snap.startedAt ??= Date.now()
        stopRing()
      }
      publish()
      watchStats(c, l)
    } else if (pc.connectionState === 'failed') {
      if (c.snap.kind === 'group') return removeLink(c, key)
      hangUp(c.snap.status === 'active' ? 'Connexion perdue.' : 'Connexion impossible : ' + noPath() + '.')
    }
  }
  pc.ondatachannel = (ev) => useChannel(c, l, ev.channel)
  return l
}

// Gives up on a connection that does not come up in time.
function connectTimeout(c: Call, l: Link) {
  clearTimeout(l.timer)
  l.timer = setTimeout(() => {
    if (call !== c || c.links.get(l.key) !== l || l.snap.connected) return
    if (c.snap.kind === 'group') return removeLink(c, l.key)
    hangUp('Connexion impossible dans le temps imparti : ' + noPath() + '.')
  }, CONNECT_TIMEOUT)
}

// Transceivers after the first of their kind carry the shared screen.
function isScreen(pc: RTCPeerConnection, t: RTCRtpTransceiver) {
  return pc.getTransceivers().find((x) => x.receiver.track.kind === t.receiver.track.kind) !== t
}

const first = (l: Link, kind: 'audio' | 'video') => l.pc.getTransceivers().find((t) => t.receiver.track.kind === kind)
const screenSlot = (l: Link, kind: 'audio' | 'video') =>
  l.pc.getTransceivers().find((t) => t.receiver.track.kind === kind && isScreen(l.pc, t) && t.currentDirection !== 'stopped')

// The local tracks on a connection just negotiated from an offer.
async function attachLocal(c: Call, l: Link) {
  const local = new MediaStream(c.mic ? [c.mic] : [])
  for (const t of l.pc.getTransceivers()) {
    const kind = t.receiver.track.kind as 'audio' | 'video'
    if (!isScreen(l.pc, t)) {
      if (kind === 'audio') {
        if (c.mic) {
          await t.sender.replaceTrack(c.mic)
          t.sender.setStreams(local)
          t.direction = 'sendrecv'
        }
      } else {
        await t.sender.replaceTrack(c.cam)
        t.direction = 'sendrecv'
        c.snap.canVideo = true
      }
    } else {
      const track = (kind === 'audio' ? c.screen?.getAudioTracks() : c.screen?.getVideoTracks())?.[0] ?? null
      await t.sender.replaceTrack(track)
      if (c.screen) t.sender.setStreams(c.screen)
      t.direction = 'sendrecv'
    }
  }
}

interface ChannelMessage {
  muted?: boolean
  camera?: boolean
  screen?: boolean
  caps?: string[]
  sdp?: RTCSessionDescriptionInit // renegotiation (both sides announced "screen")
}

function useChannel(c: Call, l: Link, dc: RTCDataChannel) {
  l.dc = dc
  dc.onopen = () => sendState(c, l)
  dc.onmessage = (m) => {
    let msg: ChannelMessage
    try {
      msg = JSON.parse(m.data as string) as ChannelMessage
    } catch {
      return
    }
    if (msg.sdp) {
      if (l.snap.canScreen) onRemoteDescription(c, l, msg.sdp).catch(() => {})
      return
    }
    l.snap.muted = !!msg.muted
    l.snap.camera = !!msg.camera
    l.snap.screen = !!msg.screen
    l.snap.canScreen = !!msg.caps?.includes('screen')
    if (c.snap.kind === 'direct') c.snap.canScreen = l.snap.canScreen
    publish()
  }
}

function send(l: Link, msg: ChannelMessage) {
  if (l.dc?.readyState === 'open') l.dc.send(JSON.stringify(msg))
}

function sendState(c: Call, l?: Link) {
  const msg = { muted: c.snap.muted, camera: c.snap.camera, screen: c.snap.screen, caps: ['screen'] }
  for (const x of l ? [l] : c.links.values()) send(x, msg)
}

// Path in use and audio received, from the connection statistics.
function watchStats(c: Call, l: Link) {
  const tick = async () => {
    if (call !== c || c.links.get(l.key) !== l) return
    const stats = await l.pc.getStats().catch(() => null)
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
    if (pair && typeof pair.currentRoundTripTime === 'number') l.snap.rtt = Math.round(pair.currentRoundTripTime * 1000)
    if (pair) {
      const local = stats.get(pair.localCandidateId)
      const remote = stats.get(pair.remoteCandidateId)
      // A relay candidate learnt from a check (prflx) keeps its low priority (type preference 0).
      const relayed = (x?: { candidateType?: string; priority?: number }) => x?.candidateType === 'relay' || (x?.priority !== undefined && x.priority < 2 ** 24)
      l.snap.path = relayed(local) || relayed(remote) ? 'relay' : local?.candidateType === 'host' && remote?.candidateType === 'host' ? 'local' : 'direct'
    }
    l.snap.received = received
    publish()
  }
  tick()
  l.stats = setInterval(tick, 2000)
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

// --- renegotiation over the data channel (one to one screen sharing) ---

async function offer(c: Call, l: Link) {
  const pc = l.pc
  if (call !== c || c.links.get(l.key) !== l) return
  if (l.makingOffer || pc.signalingState !== 'stable') {
    l.needOffer = true // once the current exchange is over
    return
  }
  l.needOffer = false
  l.makingOffer = true
  try {
    await pc.setLocalDescription()
    send(l, { sdp: pc.localDescription!.toJSON() })
  } finally {
    l.makingOffer = false
  }
}

async function onRemoteDescription(c: Call, l: Link, desc: RTCSessionDescriptionInit) {
  const pc = l.pc
  if (desc.type !== 'offer' && desc.type !== 'answer') return
  const collision = desc.type === 'offer' && (l.makingOffer || pc.signalingState !== 'stable')
  if (collision && !l.polite) return // our offer wins; they answer it, then offer again
  if (collision) l.needOffer = true // our offer is rolled back
  if (desc.type === 'answer' && pc.signalingState !== 'have-local-offer') return
  await pc.setRemoteDescription(desc)
  if (desc.type === 'offer') {
    await pc.setLocalDescription()
    send(l, { sdp: pc.localDescription!.toJSON() })
  }
  if (l.needOffer && pc.signalingState === 'stable') await offer(c, l)
}

// --- one to one: placing a call ---

export async function startCall(peer: PublicUser) {
  if (busy()) return
  if (!isFriend(peer.id)) throw new UserError('Les appels sont réservés aux amis.')
  const e = engine()
  if (!e?.validated) throw new UserError('Cet appareil n’est pas encore validé.')
  const devices = await e.devicesOf([peer.id])
  if (!devices.length) throw new UserError(peer.pseudo + ' n’a aucun appareil capable de recevoir un appel.')
  leaveVoice()
  const c = newCall(newId(), 'direct', peer.pseudo, peer, 'out', 'ringing')
  call = c
  publish()
  ring('out')
  await microphone(c)
  if (call !== c) return c.mic?.stop()
  const l = await newLink(c, 'peer', peer, null, false)
  const local = new MediaStream(c.mic ? [c.mic] : [])
  if (c.mic) l.pc.addTransceiver(c.mic, { direction: 'sendrecv', streams: [local] })
  else l.pc.addTransceiver('audio', { direction: 'recvonly' })
  l.pc.addTransceiver('video', { direction: 'sendrecv', streams: [local] })
  useChannel(c, l, l.pc.createDataChannel('quarel-call'))
  c.snap.canVideo = true // known for sure once answered
  const sdp = await gather(l.pc, await l.pc.createOffer())
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
  const l = c.links.get('peer')
  if (!dev || !s.sdp || !l) return
  l.device = dev
  l.snap.device = dev.device_id
  stopRing()
  // Other devices of the friend stop ringing.
  signal(devices.filter((d) => d.device_id !== dev.device_id), { call_id: c.snap.id, action: 'hangup' })
  c.snap.status = 'connecting'
  publish()
  await l.pc.setRemoteDescription({ type: 'answer', sdp: s.sdp })
  c.snap.canVideo = l.pc.getTransceivers().some((t) => t.receiver.track.kind === 'video' && t.currentDirection !== 'inactive' && t.currentDirection !== 'recvonly')
  publish()
  connectTimeout(c, l)
}

// --- one to one: receiving a call ---

async function onInvite(s: CallSignal) {
  const friend = isFriend(s.from_user)
  if (!friend || !s.sdp) return // calls are for friends only
  const dev = await engine()?.trustedDevice(s.from_user, s.from_device)
  if (!dev) return
  if (busy()) {
    if (call!.snap.id !== s.call_id) signal([dev], { call_id: s.call_id, action: 'reject' }) // busy
    return
  }
  const c = newCall(s.call_id, 'direct', friend.pseudo, friend, 'in', 'incoming')
  c.invite = s
  c.inviter = dev
  call = c
  publish()
  const dnd = socialState().presence_setting === 'dnd' // do not disturb: no sound, no notification
  if (!dnd) {
    ring('in')
    notify('Appel de ' + friend.pseudo)
  }
  c.timers.push(setTimeout(() => call === c && c.snap.status === 'incoming' && end('Appel manqué de ' + friend.pseudo + '.'), RING_TIMEOUT))
}

export async function acceptCall() {
  const c = call
  if (!c || c.snap.status !== 'incoming') return
  if (c.snap.kind === 'group') return joinGroupCall(c, false)
  if (!c.invite?.sdp || !c.inviter) return
  stopRing()
  leaveVoice()
  c.snap.status = 'connecting'
  publish()
  await microphone(c)
  const l = await newLink(c, 'peer', c.snap.peer, c.inviter, true)
  await l.pc.setRemoteDescription({ type: 'offer', sdp: c.invite.sdp })
  await attachLocal(c, l)
  const sdp = await gather(l.pc, await l.pc.createAnswer())
  if (call !== c || (c.snap.status as CallStatus) === 'ended') return
  await signal([c.inviter], { call_id: c.snap.id, action: 'answer', sdp })
  publish()
  connectTimeout(c, l)
}

export function declineCall() {
  const c = call
  if (!c || c.snap.status !== 'incoming') return
  if (c.snap.kind === 'group') return drop(c) // the others go on without us
  if (c.inviter) signal([c.inviter], { call_id: c.snap.id, action: 'reject' })
  end('Appel refusé.')
}

// Ends the call (or cancels it while it rings).
export function hangUp(reason = 'Appel terminé.') {
  const c = call
  if (!c || c.snap.status === 'ended') return
  if (c.snap.status === 'incoming') return declineCall()
  if (c.snap.kind === 'group') return leaveGroupCall(c, reason)
  const l = c.links.get('peer')
  signal(l?.device ? [l.device] : (c.devices ?? []), { call_id: c.snap.id, action: 'hangup' })
  end(reason)
}

function onDirectSignal(s: CallSignal) {
  if (s.action === 'invite') return void onInvite(s)
  const c = call
  if (!c || c.snap.kind !== 'direct' || c.snap.id !== s.call_id || s.from_user !== c.snap.peer.id || c.snap.status === 'ended') return
  const peerDevice = c.links.get('peer')?.device ?? c.inviter
  if (peerDevice && s.from_device !== peerDevice.device_id && c.snap.status !== 'ringing') return // another device of theirs
  if (s.action === 'answer' && c.snap.direction === 'out' && c.snap.status === 'ringing') onAnswer(c, s).catch(() => hangUp('Réponse illisible.'))
  else if (s.action === 'reject' && c.snap.status === 'ringing') end(c.snap.peer.pseudo + ' a refusé l’appel.')
  else if (s.action === 'hangup') end(c.snap.status === 'incoming' ? 'Appel manqué de ' + c.snap.peer.pseudo + '.' : 'Appel terminé.')
}

// --- groups ---

const groupOf = (convId?: string) => socialState().conversations.find((x) => x.id === convId && x.kind === 'group')
const groupName = (conv: Conversation) => conv.name || conv.members.filter((m) => m.id !== me()).map((m) => m.pseudo).join(', ')

// Trusted devices of the confirmed members (mine included, except this one).
async function groupDevices(convId: string) {
  const e = engine()
  const conv = groupOf(convId)
  if (!e || !conv) return []
  const users = conv.members.map((m) => m.id).filter((id) => id === me() || e.isConfirmed(convId, id))
  return e.devicesOf(users)
}

// Starts a call in a group, or joins the one going on.
export async function startGroupCall(conv: Conversation) {
  if (busy()) return
  const e = engine()
  if (!e?.validated) throw new UserError('Cet appareil n’est pas encore validé.')
  const going = groupCalls[conv.id]
  if (going && Object.keys(going.devices).length >= GROUP_CALL_MAX) throw new UserError('L’appel est complet (' + GROUP_CALL_MAX + ' personnes au plus).')
  const self = conv.members.find((m) => m.id === me()) ?? { id: me(), pseudo: '' }
  const c = newCall(going?.id ?? newId(), 'group', groupName(conv), self as PublicUser, 'out', 'ringing', conv.id)
  call = c
  await joinGroupCall(c, !going)
}

async function joinGroupCall(c: Call, start: boolean) {
  const e = engine()
  if (!e) return
  stopRing()
  leaveVoice()
  c.snap.status = 'ringing'
  publish()
  await microphone(c)
  if (call !== c) return c.mic?.stop()
  const devs = await groupDevices(c.snap.convId!)
  noteGroupCall(c.snap.convId!, c.snap.id, e.deviceId, me(), true)
  await signal(devs, { call_id: c.snap.id, conv_id: c.snap.convId, action: 'join', start })
  c.timers.push(setTimeout(() => {
    if (call === c && !c.connected && c.links.size === 0 && c.snap.status === 'ringing') {
      leaveGroupCall(c, start ? 'Personne n’a rejoint l’appel.' : 'Plus personne dans l’appel.')
    }
  }, start ? RING_TIMEOUT : 10_000))
}

function leaveGroupCall(c: Call, reason: string) {
  const convId = c.snap.convId!
  groupDevices(convId).then((devs) => signal(devs, { call_id: c.snap.id, conv_id: convId, action: 'leave' }))
  noteGroupCall(convId, c.snap.id, engine()?.deviceId ?? '', me(), false)
  end(reason)
}

async function onGroupSignal(s: CallSignal) {
  const e = engine()
  const conv = groupOf(s.conv_id)
  if (!e || !conv || s.from_device === e.deviceId) return
  if (s.from_user !== me() && !e.isConfirmed(conv.id, s.from_user)) return // only members confirmed here
  const user = conv.members.find((m) => m.id === s.from_user)
  if (!user) return
  if (s.action === 'join') noteGroupCall(conv.id, s.call_id, s.from_device, s.from_user, true)
  if (s.action === 'leave') noteGroupCall(conv.id, s.call_id, s.from_device, s.from_user, false)
  const c = call
  const inCall = !!c && c.snap.kind === 'group' && c.snap.id === s.call_id && c.snap.status !== 'ended' && c.snap.status !== 'incoming'
  switch (s.action) {
    case 'join': {
      if (s.from_user === me()) {
        if (c?.snap.kind === 'group' && c.snap.id === s.call_id && c.snap.status === 'incoming') drop(c) // answered on another of my devices
        return
      }
      if (inCall) return void (await offerTo(c!, conv, user, s))
      if (!s.start || busy()) return
      // Someone starts a call in the group: it rings here.
      const rc = newCall(s.call_id, 'group', groupName(conv), user, 'in', 'incoming', conv.id)
      call = rc
      publish()
      if (socialState().presence_setting !== 'dnd') {
        ring('in')
        notify('Appel de groupe · ' + groupName(conv))
      }
      rc.timers.push(setTimeout(() => call === rc && rc.snap.status === 'incoming' && drop(rc), RING_TIMEOUT))
      return
    }
    case 'offer': {
      if (!inCall || !s.sdp) return
      const cur = c!.links.get(s.from_device)
      // Both joined at once and offered each other: the offer of the lower device id wins.
      if (cur && (cur.snap.connected || e.deviceId < s.from_device)) return
      const dev = await e.trustedDevice(s.from_user, s.from_device)
      if (!dev) return
      const l = await newLink(c!, dev.device_id, user, dev, true)
      await l.pc.setRemoteDescription({ type: 'offer', sdp: s.sdp })
      await attachLocal(c!, l)
      const sdp = await gather(l.pc, await l.pc.createAnswer())
      if (call !== c || c!.links.get(dev.device_id) !== l) return
      await signal([dev], { call_id: s.call_id, conv_id: conv.id, action: 'answer', sdp })
      connectTimeout(c!, l)
      return
    }
    case 'answer': {
      const l = inCall ? c!.links.get(s.from_device) : undefined
      if (!l || !s.sdp || l.pc.signalingState !== 'have-local-offer') return
      await l.pc.setRemoteDescription({ type: 'answer', sdp: s.sdp })
      return
    }
    case 'leave': {
      if (inCall) removeLink(c!, s.from_device)
      else if (c?.snap.kind === 'group' && c.snap.id === s.call_id && c.snap.status === 'incoming' && !groupCalls[conv.id]) drop(c) // nobody left in it
      return
    }
    case 'full': {
      if (inCall && c!.links.size === 0) leaveGroupCall(c!, 'L’appel est complet (' + GROUP_CALL_MAX + ' personnes au plus).')
      return
    }
  }
}

// A member joined the call this device is in: offer them a connection with
// all four transceivers (microphone, camera, screen, screen sound).
async function offerTo(c: Call, conv: Conversation, user: PublicUser, s: CallSignal) {
  const e = engine()!
  if (c.links.has(s.from_device)) return
  const dev = await e.trustedDevice(s.from_user, s.from_device)
  if (!dev) return
  if (c.links.size + 2 > GROUP_CALL_MAX) return signal([dev], { call_id: s.call_id, conv_id: conv.id, action: 'full' })
  const l = await newLink(c, dev.device_id, user, dev, false)
  const local = new MediaStream(c.mic ? [c.mic] : [])
  const screen = c.screen ?? new MediaStream()
  l.pc.addTransceiver(c.mic ?? 'audio', { direction: c.mic ? 'sendrecv' : 'recvonly', streams: [local] })
  l.pc.addTransceiver(c.cam ?? 'video', { direction: 'sendrecv', streams: [local] })
  l.pc.addTransceiver(c.screen?.getVideoTracks()[0] ?? 'video', { direction: 'sendrecv', streams: [screen] })
  l.pc.addTransceiver(c.screen?.getAudioTracks()[0] ?? 'audio', { direction: 'sendrecv', streams: [screen] })
  useChannel(c, l, l.pc.createDataChannel('quarel-call'))
  const sdp = await gather(l.pc, await l.pc.createOffer())
  if (call !== c || c.links.get(dev.device_id) !== l) return
  await signal([dev], { call_id: s.call_id, conv_id: conv.id, action: 'offer', sdp })
  connectTimeout(c, l)
}

onEngineEvent((ev) => {
  if (ev.kind !== 'call_signal') return
  if (ev.signal.conv_id) onGroupSignal(ev.signal).catch(() => {})
  else onDirectSignal(ev.signal)
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
  if (!c || !c.snap.canVideo || c.snap.status === 'ended' || c.snap.status === 'incoming') return
  if (c.cam) {
    const cam = c.cam
    c.cam = null
    for (const l of c.links.values()) await first(l, 'video')?.sender.replaceTrack(null)
    cam.stop()
    c.snap.camera = false
    c.snap.localStream = null
  } else {
    const s = await navigator.mediaDevices.getUserMedia({ video: videoConstraints() })
    c.cam = s.getVideoTracks()[0]
    for (const l of c.links.values()) await first(l, 'video')?.sender.replaceTrack(c.cam)
    c.snap.camera = true
    c.snap.localStream = new MediaStream([c.cam])
  }
  sendState(c)
  publish()
}

export const canShareScreen = () => typeof navigator.mediaDevices?.getDisplayMedia === 'function'

// Shares the screen (the desktop app asks which one first, see ScreenPicker)
// with its sound when the system gives it (Windows), or stops sharing.
export async function toggleCallScreen() {
  const c = call
  if (!c || c.snap.status === 'ended' || c.snap.status === 'incoming' || !c.snap.canScreen) return
  if (c.screen) {
    const s = c.screen
    c.screen = null
    for (const l of c.links.values()) for (const kind of ['video', 'audio'] as const) await screenSlot(l, kind)?.sender.replaceTrack(null)
    s.getTracks().forEach((t) => t.stop())
    c.snap.screen = false
    c.snap.screenStream = null
    sendState(c)
    publish()
    return
  }
  let s: MediaStream
  try {
    s = await navigator.mediaDevices.getDisplayMedia({
      video: { frameRate: { ideal: 30 } },
      audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false },
      // Not the call's own sound (the others would hear their voices again), where supported.
      restrictOwnAudio: true,
      systemAudio: 'include',
    } as DisplayMediaStreamOptions)
  } catch (e) {
    if ((e as Error)?.name === 'NotAllowedError') return // cancelled
    throw new UserError('Partage d’écran impossible sur cet appareil.')
  }
  if (call !== c || (c.snap.status as CallStatus) === 'ended') return s.getTracks().forEach((t) => t.stop())
  c.screen = s
  for (const l of c.links.values()) await shareOn(c, l, s)
  s.getVideoTracks()[0]?.addEventListener('ended', () => call === c && c.screen === s && toggleCallScreen().catch(() => {})) // stopped from the system
  c.snap.screen = true
  c.snap.screenStream = new MediaStream(s.getVideoTracks())
  sendState(c)
  publish()
}

// Puts the screen on a connection: its screen transceivers when it has them
// (groups: always; one to one: after a first share), else new ones and a
// renegotiation (the other app announced it can).
async function shareOn(c: Call, l: Link, s: MediaStream) {
  if (c.snap.kind === 'direct' && !l.snap.canScreen) return
  let renegotiate = false
  for (const track of s.getTracks()) {
    const t = screenSlot(l, track.kind as 'audio' | 'video')
    if (!t) {
      l.pc.addTransceiver(track, { direction: 'sendrecv', streams: [s] })
      renegotiate = true
      continue
    }
    await t.sender.replaceTrack(track)
    t.sender.setStreams(s)
    if (t.direction !== 'sendrecv') {
      t.direction = 'sendrecv'
      renegotiate = true
    }
  }
  if (renegotiate) await offer(c, l)
}

// Signed out: the call ends.
export function closeCalls() {
  if (call) hangUp()
}

// Devices chosen in the settings apply to the call in progress.
onDeviceChange(async (kind) => {
  const c = call
  if (!c || c.snap.status === 'ended') return
  if (kind === 'audiooutput') {
    for (const l of c.links.values()) {
      applyOutput(l.audio)
      applyOutput(l.screenAudio)
    }
    return
  }
  try {
    if (kind === 'audioinput' && c.mic) {
      const mic = (await navigator.mediaDevices.getUserMedia({ audio: audioConstraints() })).getAudioTracks()[0]
      mic.enabled = !c.snap.muted
      for (const l of c.links.values()) await first(l, 'audio')?.sender.replaceTrack(mic)
      c.mic.stop()
      c.mic = mic
    } else if (kind === 'videoinput' && c.cam) {
      const cam = (await navigator.mediaDevices.getUserMedia({ video: videoConstraints() })).getVideoTracks()[0]
      for (const l of c.links.values()) await first(l, 'video')?.sender.replaceTrack(cam)
      c.cam.stop()
      c.cam = cam
      c.snap.localStream = new MediaStream([cam])
      publish()
    }
  } catch {
    /* device unavailable: keep the current one */
  }
})
