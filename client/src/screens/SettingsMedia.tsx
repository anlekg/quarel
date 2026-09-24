// Settings › Voix et vidéo: microphone, speakers and camera (for voice
// channels and calls), with a level meter, a test sound and a preview; the
// call relay choice.
import { useEffect, useRef, useState } from 'react'
import { applyOutput, audioConstraints, canChooseOutput, chooseDevice, chosenDevice, listDevices, videoConstraints, type MediaKind } from '../lib/media'
import { relayAllowed, setRelayAllowed } from '../state/calls'

function DeviceSelect({ kind, label, devices }: { kind: MediaKind; label: string; devices: MediaDeviceInfo[] }) {
  const [value, setValue] = useState(() => chosenDevice(kind))
  const list = devices.filter((d) => d.kind === kind)
  return (
    <div className="field">
      <label htmlFor={'dev-' + kind}>{label}</label>
      <select id={'dev-' + kind} className="input" value={list.some((d) => d.deviceId === value) ? value : ''}
        onChange={(e) => { setValue(e.target.value); chooseDevice(kind, e.target.value) }}>
        <option value="">Par défaut du système</option>
        {list.map((d, i) => <option key={d.deviceId} value={d.deviceId}>{d.label || 'Périphérique ' + (i + 1)}</option>)}
      </select>
    </div>
  )
}

// Microphone test: live input level.
function MicTest() {
  const [level, setLevel] = useState<number | null>(null)
  const stop = useRef<(() => void) | null>(null)
  useEffect(() => () => stop.current?.(), [])
  const start = async () => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: audioConstraints() })
      const ctx = new AudioContext()
      const analyser = ctx.createAnalyser()
      analyser.fftSize = 512
      ctx.createMediaStreamSource(stream).connect(analyser)
      const data = new Uint8Array(analyser.fftSize)
      let raf = 0
      const tick = () => {
        analyser.getByteTimeDomainData(data)
        let peak = 0
        for (const v of data) peak = Math.max(peak, Math.abs(v - 128))
        setLevel(Math.min(1, peak / 64))
        raf = requestAnimationFrame(tick)
      }
      tick()
      stop.current = () => {
        cancelAnimationFrame(raf)
        stream.getTracks().forEach((t) => t.stop())
        ctx.close().catch(() => {})
        stop.current = null
        setLevel(null)
      }
    } catch {
      setLevel(-1)
    }
  }
  return (
    <div className="media-test">
      <button className="btn btn-ghost btn-sm" onClick={() => (stop.current ? stop.current() : start())}>{stop.current ? 'Arrêter le test' : 'Tester le micro'}</button>
      {level === -1 ? <span className="muted small">Micro indisponible (autorisation refusée ?).</span>
        : level !== null && <div className="meter" role="meter" aria-label="Niveau du micro" aria-valuenow={Math.round(level * 100)}><span style={{ width: level * 100 + '%' }} /></div>}
    </div>
  )
}

// Speakers test: a short chime on the chosen output.
async function playTestSound() {
  const ctx = new AudioContext()
  const dest = ctx.createMediaStreamDestination()
  for (const [i, f] of [523.25, 659.25, 783.99].entries()) {
    const o = ctx.createOscillator()
    const g = ctx.createGain()
    o.frequency.value = f
    const t = ctx.currentTime + i * 0.18
    g.gain.setValueAtTime(0.0001, t)
    g.gain.exponentialRampToValueAtTime(0.2, t + 0.02)
    g.gain.exponentialRampToValueAtTime(0.0001, t + 0.5)
    o.connect(g).connect(dest)
    o.start(t)
    o.stop(t + 0.55)
  }
  const el = new Audio()
  el.srcObject = dest.stream
  await applyOutput(el)
  await el.play().catch(() => {})
  setTimeout(() => {
    el.srcObject = null
    ctx.close().catch(() => {})
  }, 1200)
}

function CameraPreview() {
  const ref = useRef<HTMLVideoElement>(null)
  const [stream, setStream] = useState<MediaStream | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => () => stream?.getTracks().forEach((t) => t.stop()), [stream])
  useEffect(() => {
    if (ref.current) ref.current.srcObject = stream
  }, [stream])
  return (
    <div className="media-test">
      <button className="btn btn-ghost btn-sm" onClick={async () => {
        if (stream) return setStream(null)
        try {
          setFailed(false)
          setStream(await navigator.mediaDevices.getUserMedia({ video: videoConstraints() }))
        } catch {
          setFailed(true)
        }
      }}>{stream ? 'Arrêter l’aperçu' : 'Aperçu de la caméra'}</button>
      {failed && <span className="muted small">Caméra indisponible (autorisation refusée ?).</span>}
      {stream && <video ref={ref} className="cam-preview" autoPlay playsInline muted />}
    </div>
  )
}

export function MediaSection() {
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([])
  const [relay, setRelay] = useState(relayAllowed)
  useEffect(() => {
    const load = () => listDevices().then(setDevices)
    load()
    navigator.mediaDevices?.addEventListener('devicechange', load)
    return () => navigator.mediaDevices?.removeEventListener('devicechange', load)
  }, [])
  const unnamed = devices.length > 0 && devices.every((d) => !d.label)
  return (
    <>
      <h2>Voix et vidéo</h2>
      <p className="muted" style={{ lineHeight: 1.5 }}>Pour les salons vocaux et les appels. Un changement s&apos;applique tout de suite, même en communication.</p>
      {unnamed && <p className="muted small">Les noms des périphériques apparaissent après un premier test du micro ou de la caméra.</p>}
      <div className="settings-form">
        <DeviceSelect kind="audioinput" label="Micro" devices={devices} />
        <MicTest />
        {canChooseOutput() && (
          <>
            <DeviceSelect kind="audiooutput" label="Haut-parleurs" devices={devices} />
            <div className="media-test"><button className="btn btn-ghost btn-sm" onClick={() => playTestSound()}>Jouer un son de test</button></div>
          </>
        )}
        <DeviceSelect kind="videoinput" label="Caméra" devices={devices} />
        <CameraPreview />
      </div>
      <h3 className="settings-sub">Appels entre amis</h3>
      <p className="muted small" style={{ lineHeight: 1.5 }}>
        Les appels passent directement d&apos;un appareil à l&apos;autre, chiffrés de bout en bout. Quand c&apos;est impossible (certaines box ou réseaux
        d&apos;entreprise), votre service d&apos;identité peut relayer le flux, qui reste chiffré : il ne peut ni l&apos;écouter ni le voir, mais il voit votre adresse IP.
      </p>
      <label className="check-line card" style={{ padding: '14px 16px' }}>
        <input type="checkbox" checked={relay} onChange={(e) => { setRelay(e.target.checked); setRelayAllowed(e.target.checked) }} />
        <span>Utiliser le relais si aucune connexion directe n&apos;est possible
          <span className="muted small" style={{ display: 'block' }}>Désactivé : votre appareil n&apos;utilisera jamais le relais (certains appels échoueront). Votre correspondant·e peut toujours utiliser le sien.</span></span>
      </label>
    </>
  )
}
