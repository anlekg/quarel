// Microphone, camera and speakers chosen in the settings, shared by voice
// channels and calls. An empty id means the system default. Devices that
// disappeared fall back to the default ("ideal" constraints, not "exact").
import { prefs } from '../platform'

export type MediaKind = 'audioinput' | 'videoinput' | 'audiooutput'

const key = (k: MediaKind) => 'media.' + k

export const chosenDevice = (k: MediaKind): string => prefs.get(key(k), '')

const listeners = new Set<(k: MediaKind, id: string) => void>()

// Active voice sessions and calls switch devices live.
export function onDeviceChange(l: (k: MediaKind, id: string) => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export function chooseDevice(k: MediaKind, id: string) {
  prefs.set(key(k), id)
  for (const l of listeners) l(k, id)
}

export function audioConstraints(): MediaTrackConstraints {
  const id = chosenDevice('audioinput')
  return { echoCancellation: true, noiseSuppression: true, autoGainControl: true, ...(id ? { deviceId: { ideal: id } } : {}) }
}

export function videoConstraints(): MediaTrackConstraints {
  const id = chosenDevice('videoinput')
  return { width: { ideal: 1280 }, height: { ideal: 720 }, ...(id ? { deviceId: { ideal: id } } : {}) }
}

// Plays through the chosen speakers (when the platform allows choosing).
export async function applyOutput(el: HTMLMediaElement) {
  const id = chosenDevice('audiooutput')
  const sink = el as HTMLMediaElement & { setSinkId?: (id: string) => Promise<void> }
  if (sink.setSinkId) await sink.setSinkId(id).catch(() => sink.setSinkId!('').catch(() => {}))
}

export const canChooseOutput = () => typeof (HTMLMediaElement.prototype as { setSinkId?: unknown }).setSinkId === 'function'

// Devices with their names (names appear once media permission was granted).
export async function listDevices(): Promise<MediaDeviceInfo[]> {
  try {
    return (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.deviceId && d.deviceId !== 'default' && d.deviceId !== 'communications')
  } catch {
    return []
  }
}
