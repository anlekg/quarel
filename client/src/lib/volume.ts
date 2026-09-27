// How loud each person is for me (0–200 %) and "mute for me", kept on this
// device and keyed by their stable identity (identity service + account),
// so the setting follows them in every server's voice channels and in calls.
// Above 100 %, the sound goes through Web Audio (a gain node).
import { prefs } from '../platform'
import { chosenDevice } from './media'

export const personKey = (issuer: string, subject: string) => issuer + '/' + subject

export function volumeOf(key: string): number {
  const v = Number(prefs.get('vol:' + key, 1))
  return Number.isFinite(v) ? Math.min(2, Math.max(0, v)) : 1
}

export const mutedForMe = (key: string) => prefs.get('mute:' + key, false)

const listeners = new Set<() => void>()

export function onVolumeChange(cb: () => void) {
  listeners.add(cb)
  return () => listeners.delete(cb)
}

export function setVolume(key: string, v: number) {
  prefs.set('vol:' + key, Math.min(2, Math.max(0, v)))
  for (const l of listeners) l()
}

export function setMutedForMe(key: string, on: boolean) {
  prefs.set('mute:' + key, on)
  for (const l of listeners) l()
}

// What to play for this person: 0 when muted for me.
export const effectiveVolume = (key: string) => (mutedForMe(key) ? 0 : volumeOf(key))

interface Boost {
  ctx: AudioContext
  gain: GainNode
}
const boosts = new WeakMap<HTMLMediaElement, Boost>()

// Plays a remote voice at volume v (0–2), or not at all when silent
// (deafened). Once boosted, the element stays muted and Web Audio plays it
// (a WebRTC stream is only heard through Web Audio from the stream itself).
export function applyVolume(el: HTMLMediaElement, v: number, silent = false) {
  let b = boosts.get(el)
  if (!b && v > 1 && !silent && el.srcObject instanceof MediaStream) {
    try {
      const ctx = new AudioContext()
      const gain = ctx.createGain()
      ctx.createMediaStreamSource(el.srcObject).connect(gain).connect(ctx.destination)
      const sink = ctx as AudioContext & { setSinkId?: (id: string) => Promise<void> }
      const out = chosenDevice('audiooutput')
      if (out && sink.setSinkId) sink.setSinkId(out).catch(() => {})
      b = { ctx, gain }
      boosts.set(el, b)
    } catch {
      /* no Web Audio: 100 % at most */
    }
  }
  if (b) {
    el.muted = true
    b.gain.gain.value = silent ? 0 : v
    if (b.ctx.state === 'suspended') b.ctx.resume().catch(() => {})
  } else {
    el.muted = silent || v === 0
    el.volume = Math.min(1, v)
  }
}

// An element goes away: its Web Audio graph too.
export function releaseVolume(el: HTMLMediaElement) {
  const b = boosts.get(el)
  if (b) {
    b.ctx.close().catch(() => {})
    boosts.delete(el)
  }
}
