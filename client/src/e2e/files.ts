// Files of private conversations, as cmd/quarelctl (dms.go, filexfer.go),
// option D chosen by the PM: encrypted on the device (XChaCha20-Poly1305,
// fresh key, associated data = conversation id), offered peer to peer to the
// devices that are online (WebRTC data channel "quarel-file", 16 KiB chunks,
// "ok" once received; never through the TURN relay), and a server copy only
// for the devices that did not get it, deleted once they all have it. The key
// travels in the encrypted conversation event.
//
// Every device keeps the ciphertexts it has and serves them to the members of
// the conversation who ask (a new device, someone who missed a large file).
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import type { DeviceInfo, IdentityClient } from '../api/identity'
import { ApiError } from '../api/http'
import { files as store } from '../platform'
import type { E2E, FileRef, FileSignal } from './engine'
import { b64, unb64 } from './keys'

const CHUNK = 16 << 10
const OFFER_WINDOW = 10_000 // how long a sender serves the devices that are online
const FETCH_TIMEOUT = 30_000 // how long a requester waits for a holder
export const MAX_FILE = 100 << 20 // kept in memory while sending

const enc = new TextEncoder()
const padded = (b: Uint8Array) => {
  const s = b64(b)
  return s + '='.repeat((4 - (s.length % 4)) % 4)
}

function newId() {
  const a = 'abcdefghijklmnopqrstuvwxyz234567'
  return Array.from(crypto.getRandomValues(new Uint8Array(26)), (x) => a[x & 31]).join('')
}

export function decryptFile(convId: string, ref: FileRef, ct: Uint8Array): Uint8Array {
  const key = unb64(ref.key)
  const nonce = unb64(ref.nonce)
  if (key.length !== 32 || nonce.length !== 24) throw new FileError('bad_key')
  try {
    return xchacha20poly1305(key, nonce, enc.encode(convId)).decrypt(ct)
  } catch {
    throw new FileError('tampered')
  }
}

export class FileError extends Error {
  constructor(public code: 'bad_key' | 'tampered' | 'unavailable' | 'too_large') {
    super(code)
  }
}

export interface SendResult {
  direct: number // devices served peer to peer
  server: number // devices that will use the server copy
  missed: number // offline devices that will have to ask a holder later (too large for the server)
}

export class Files {
  private answers = new Map<string, (sdp: string) => void>() // transfer id → waiting requester
  private served = new Map<string, (device: string) => void>() // file id → sender waiting for deliveries
  private fetching = new Map<string, Promise<Uint8Array>>() // file id → transfer in progress
  private ice: { at: number; servers: RTCIceServer[] } | null = null
  private off: () => void

  constructor(
    private e2e: E2E,
    private api: IdentityClient,
    private members: (convId: string) => string[] | undefined, // user ids of a conversation, if we are in it
    private direct: (userId: string) => boolean = () => true, // may connect directly (see state/netprivacy.ts)
  ) {
    this.off = e2e.on((ev) => {
      if (ev.kind === 'file_signal') this.onSignal(ev.signal)
      if (ev.kind === 'file_received') this.pullServerCopy(ev.dmId, ev.ref).catch(() => {})
      if (ev.kind === 'file_gone') store.delete(ev.ref.id).catch(() => {})
    })
  }

  close() {
    this.off()
  }

  // --- sending ---

  async send(convId: string, others: string[], file: File, text: string): Promise<SendResult> {
    if (file.size > MAX_FILE) throw new FileError('too_large')
    const data = new Uint8Array(await file.arrayBuffer())
    const key = crypto.getRandomValues(new Uint8Array(32))
    const nonce = crypto.getRandomValues(new Uint8Array(24))
    const ct = xchacha20poly1305(key, nonce, enc.encode(convId)).encrypt(data)
    const ref: FileRef = { id: newId(), name: file.name || 'fichier', mime: file.type || 'application/octet-stream', size: data.length, key: padded(key), nonce: padded(nonce) }
    await store.put(ref.id, ct)

    // 1. Offer it to every trusted device of the conversation that may connect
    // directly (see state/netprivacy.ts); serve those that ask.
    const targets = await this.e2e.devicesOf([...others, this.e2e.userId])
    const direct = await this.e2e.devicesOf([...others, this.e2e.userId].filter((u) => this.direct(u)))
    const delivered = new Set<string>()
    let active = 0
    const done = new Promise<void>((resolve) => {
      const start = Date.now()
      const check = () => {
        const quiet = Date.now() - start > 4000 && active === 0 // nobody online asked
        if (delivered.size >= direct.length || quiet || Date.now() - start > OFFER_WINDOW) {
          clearInterval(timer)
          this.served.delete(ref.id)
          resolve()
        }
      }
      const timer = setInterval(check, 250)
      this.served.set(ref.id, (dev) => {
        if (dev === '+') active++
        else if (dev === '-') active--
        else delivered.add(dev)
        check()
      })
    })
    if (direct.length) await this.e2e.sendFileSignal(direct, { action: 'offer', conv_id: convId, file_id: ref.id, size: ct.length })
    await done

    // 2. A server copy for the others, deleted once they all have it.
    const missing = targets.filter((d) => !delivered.has(d.device_id)).map((d) => d.device_id)
    const res: SendResult = { direct: delivered.size, server: 0, missed: 0 }
    if (missing.length) {
      try {
        ref.server_id = (await this.api.uploadDMFile(convId, ct, missing)).id
        res.server = missing.length
      } catch (e) {
        if (!(e instanceof ApiError && e.code === 'file_too_large')) throw e
        res.missed = missing.length
      }
    }

    // 3. The key, in the encrypted conversation event.
    await this.e2e.send(convId, others, { type: 'file', file: ref, text })
    return res
  }

  // --- receiving ---

  // The decrypted content of a file: this device's copy, else the server
  // copy, else peer to peer from any device of the conversation holding it.
  async open(convId: string, ref: FileRef): Promise<Uint8Array> {
    return decryptFile(convId, ref, await this.ciphertext(convId, ref))
  }

  private async ciphertext(convId: string, ref: FileRef): Promise<Uint8Array> {
    const local = await store.get(ref.id)
    if (local) return local
    const running = this.fetching.get(ref.id)
    if (running) return running
    const p = (async () => {
      if (ref.server_id) {
        try {
          return await this.pullServerCopy(convId, ref)
        } catch {
          /* deleted or expired: ask a holder */
        }
      }
      const holders = await this.e2e.devicesOf((this.members(convId) ?? []).filter((u) => this.direct(u)))
      const ct = await this.fetchP2P(convId, ref.id, holders, FETCH_TIMEOUT)
      decryptFile(convId, ref, ct) // never keep something that does not decrypt
      await store.put(ref.id, ct)
      return ct
    })()
    this.fetching.set(ref.id, p)
    try {
      return await p
    } finally {
      this.fetching.delete(ref.id)
    }
  }

  // Fetches the server copy if this device lacks the file, then acknowledges
  // it so the server can delete it.
  private async pullServerCopy(convId: string, ref: FileRef): Promise<Uint8Array> {
    let ct = await store.get(ref.id)
    if (!ref.server_id) {
      if (ct) return ct
      throw new FileError('unavailable')
    }
    if (!ct) {
      ct = await this.api.downloadDMFile(convId, ref.server_id)
      decryptFile(convId, ref, ct)
      await store.put(ref.id, ct)
    }
    await this.api.ackDMFile(convId, ref.server_id).catch(() => {})
    return ct
  }

  // Deletes the server copy of a file this account sent (message deleted).
  async forget(convId: string, ref: FileRef) {
    await store.delete(ref.id).catch(() => {})
    if (ref.server_id) await this.api.deleteDMFile(convId, ref.server_id).catch(() => {})
  }

  // --- peer to peer ---

  private async rtcConfig(): Promise<RTCConfiguration> {
    if (!this.ice || Date.now() - this.ice.at > 10 * 60_000) {
      const r = await this.api.iceServers().catch(() => ({ ice_servers: [] }))
      // STUN only: file transfers never use the relay (the server copy is their fallback).
      const servers = r.ice_servers.filter((s) => s.urls.every((u) => u.startsWith('stun:'))).map((s) => ({ urls: s.urls }))
      this.ice = { at: Date.now(), servers }
    }
    return { iceServers: this.ice.servers }
  }

  private onSignal(sig: FileSignal) {
    if (sig.action === 'answer' && sig.transfer_id && sig.sdp) {
      this.answers.get(sig.transfer_id)?.(sig.sdp)
      this.answers.delete(sig.transfer_id)
    } else if (sig.action === 'fetch') {
      this.serve(sig).catch(() => {})
    } else if (sig.action === 'offer') {
      this.autoFetch(sig).catch(() => {})
    }
  }

  // Takes a file just offered by the device that sends it.
  private async autoFetch(offer: FileSignal) {
    if (!this.members(offer.conv_id) || !this.direct(offer.from_user) || (await store.get(offer.file_id)) || this.fetching.has(offer.file_id)) return
    const holder = await this.e2e.trustedDevice(offer.from_user, offer.from_device)
    if (!holder) return
    const p = this.fetchP2P(offer.conv_id, offer.file_id, [holder], OFFER_WINDOW).then(async (ct) => {
      await store.put(offer.file_id, ct)
      return ct
    })
    this.fetching.set(offer.file_id, p)
    try {
      await p
    } finally {
      this.fetching.delete(offer.file_id)
    }
  }

  // Asks holders for a file's ciphertext; the first one that answers sends it.
  private async fetchP2P(convId: string, fileId: string, holders: DeviceInfo[], timeout: number): Promise<Uint8Array> {
    if (!holders.length) throw new FileError('unavailable')
    const pc = new RTCPeerConnection(await this.rtcConfig())
    const transfer = newId()
    try {
      const dc = pc.createDataChannel('quarel-file')
      dc.binaryType = 'arraybuffer'
      const received = new Promise<Uint8Array>((resolve, reject) => {
        let buf: Uint8Array | null = null
        let off = 0
        dc.onmessage = (m) => {
          if (typeof m.data === 'string') {
            const h = JSON.parse(m.data) as { size?: number; error?: string }
            if (h.error || typeof h.size !== 'number' || h.size < 0 || h.size > 2 * MAX_FILE) return reject(new FileError('unavailable'))
            buf = new Uint8Array(h.size)
          } else if (buf) {
            const chunk = new Uint8Array(m.data as ArrayBuffer)
            if (off + chunk.length > buf.length) return reject(new FileError('unavailable'))
            buf.set(chunk, off)
            off += chunk.length
          }
          if (buf && off >= buf.length) resolve(buf)
        }
        dc.onclose = () => reject(new FileError('unavailable'))
      })
      const sdp = await gather(pc, await pc.createOffer())
      const answer = new Promise<string>((resolve, reject) => {
        this.answers.set(transfer, resolve)
        setTimeout(() => reject(new FileError('unavailable')), timeout)
      })
      await this.e2e.sendFileSignal(holders, { action: 'fetch', transfer_id: transfer, conv_id: convId, file_id: fileId, sdp })
      await pc.setRemoteDescription({ type: 'answer', sdp: await answer })
      const ct = await withTimeout(received, 5 * 60_000)
      dc.send('ok')
      await new Promise((r) => setTimeout(r, 200)) // let the acknowledgement leave
      return ct
    } finally {
      this.answers.delete(transfer)
      pc.close()
    }
  }

  // Sends a file this device holds to a member of its conversation.
  private async serve(req: FileSignal) {
    if (!req.sdp || !req.transfer_id) return
    if (!this.members(req.conv_id)?.includes(req.from_user)) return // never serve outside the conversation
    if (!this.direct(req.from_user)) return // would give our address: the server copy is there for them
    const ct = await store.get(req.file_id)
    if (!ct) return // another holder may answer
    const requester = await this.e2e.trustedDevice(req.from_user, req.from_device)
    if (!requester) return
    const notify = this.served.get(req.file_id)
    notify?.('+')
    const pc = new RTCPeerConnection(await this.rtcConfig())
    try {
      const confirmed = new Promise<void>((resolve, reject) => {
        pc.ondatachannel = ({ channel: dc }) => {
          dc.onopen = async () => {
            try {
              dc.send(JSON.stringify({ size: ct.length }))
              dc.bufferedAmountLowThreshold = 1 << 20
              for (let off = 0; off < ct.length; off += CHUNK) {
                if (dc.bufferedAmount > 4 << 20) await new Promise((r) => { dc.onbufferedamountlow = r })
                dc.send(ct.slice(off, Math.min(off + CHUNK, ct.length)))
              }
            } catch (e) {
              reject(e)
            }
          }
          dc.onmessage = (m) => m.data === 'ok' && resolve()
          dc.onclose = () => reject(new FileError('unavailable'))
        }
      })
      await pc.setRemoteDescription({ type: 'offer', sdp: req.sdp })
      const sdp = await gather(pc, await pc.createAnswer())
      await this.e2e.sendFileSignal([requester], { action: 'answer', transfer_id: req.transfer_id, conv_id: req.conv_id, file_id: req.file_id, sdp })
      await withTimeout(confirmed, 3 * 60_000)
      this.served.get(req.file_id)?.(req.from_device)
    } finally {
      pc.close()
      notify?.('-')
    }
  }
}

// Sets the local description and waits for all network paths (no trickle ICE:
// the complete description goes in one encrypted message).
async function gather(pc: RTCPeerConnection, desc: RTCSessionDescriptionInit): Promise<string> {
  const complete = new Promise<void>((resolve) => {
    if (pc.iceGatheringState === 'complete') return resolve()
    pc.addEventListener('icegatheringstatechange', () => pc.iceGatheringState === 'complete' && resolve())
  })
  await pc.setLocalDescription(desc)
  await withTimeout(complete, 15_000).catch(() => {}) // use what was found so far
  return pc.localDescription!.sdp
}

function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  return Promise.race([p, new Promise<T>((_, reject) => setTimeout(() => reject(new FileError('unavailable')), ms))])
}
