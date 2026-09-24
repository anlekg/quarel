// Interoperability test: the client's vodozemac (WebAssembly) against the Go
// test client's goolm. Run: node client/crypto/interop.mjs <olminterop binary>
import { spawn } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { createInterface } from 'node:readline'
import * as ed from '@noble/ed25519'
import init, { OlmAccount, MegolmOutbound, MegolmInbound } from '../src/crypto/wasm/quarel_crypto.js'

await init({ module_or_path: readFileSync(new URL('../src/crypto/wasm/quarel_crypto_bg.wasm', import.meta.url)) })
const go = spawn(process.argv[2], [], { stdio: ['pipe', 'pipe', 'inherit'] })
const lines = createInterface({ input: go.stdout })[Symbol.asyncIterator]()
async function call(op, args = {}) {
  go.stdin.write(JSON.stringify({ Op: op, ...args }) + '\n')
  const r = JSON.parse((await lines.next()).value)
  if (r.error) throw new Error(op + ': ' + r.error)
  return r
}
let failed = 0
function check(what, ok) {
  console.log((ok ? '✔ ' : '✘ ') + what)
  if (!ok) failed++
}
const b64d = (s) => Uint8Array.from(Buffer.from(s, 'base64'))

const g = await call('init')
const me = new OlmAccount()

// Signatures both ways (device keys, one-time keys, certificates).
check('signature Go vérifiée par le client', await ed.verifyAsync(b64d(g.signature), new TextEncoder().encode('quarel-interop'), b64d(g.ed25519)))
check('signature du client vérifiée par Go', (await call('verify', { Key: me.ed25519, Msg: 'bonjour', Sig: me.sign(new TextEncoder().encode('bonjour')) })).ok)

// Olm: the client opens a session with Go's one-time key.
const out = me.outboundSession(g.curve25519, g.otk)
const m1 = JSON.parse(out.encrypt('{"type":"room_key","x":"é"}'))
check('premier message Olm de type pré-clé', m1.type === 0)
const r1 = await call('olm_inbound', { SenderCurve: me.curve25519, Body: m1.body })
check('Go ouvre la session et déchiffre', r1.plaintext === '{"type":"room_key","x":"é"}')
const r2 = await call('olm_encrypt', { Text: 'réponse de Go' })
check('le client déchiffre la réponse de Go', out.decrypt(r2.type, r2.body) === 'réponse de Go')
const m2 = JSON.parse(out.encrypt('suite'))
check('message suivant déchiffré par Go', (await call('olm_decrypt', { Type: m2.type, Body: m2.body })).plaintext === 'suite')

// Olm the other way: Go opens a session with the client's one-time key.
me.generateOneTimeKeys(1)
const [[, otk]] = Object.entries(JSON.parse(me.oneTimeKeys()))
me.markKeysAsPublished()
// (goolm's outbound session is exercised by the end-to-end tests with quarelctl.)
check('clé à usage unique produite', typeof otk === 'string' && otk.length === 43)

// Megolm: Go → client.
const gs = await call('megolm_out')
const inbound = new MegolmInbound(gs.session_key)
check('même identifiant de session Megolm', inbound.sessionId === gs.session_id)
for (const t of ['premier', 'deuxième']) {
  const c = await call('megolm_encrypt', { Text: t })
  const d = inbound.decrypt(c.body)
  check(`Megolm Go → client (${t}, index ${d.index})`, d.plaintext === t)
}

// Megolm: client → Go, then via an exported key (history transfer, backup).
const og = new MegolmOutbound()
check('Go reconnaît la session du client', (await call('megolm_in', { SessionKey: og.sessionKey })).session_id === og.sessionId)
const c1 = og.encrypt('{"text":"salut"}')
check('Megolm client → Go', (await call('megolm_decrypt', { Body: c1 })).plaintext === '{"text":"salut"}')
const exp = (await call('megolm_export', { Index: 0 })).exported
const imported = MegolmInbound.import(exp)
check('clé exportée par Go importée par le client', imported.decrypt(c1).plaintext === '{"text":"salut"}')
const exp2 = imported.exportAt(0)
check('clé exportée par le client importée par Go', (await call('megolm_import', { Exported: exp2 })).session_id === og.sessionId)
check('et déchiffre', (await call('megolm_decrypt', { Body: og.encrypt('encore') })).plaintext === 'encore')

// Pickles survive a round trip.
const key = new Uint8Array(32).fill(7)
const again = OlmAccount.fromPickle(me.pickle(key), key)
check('compte Olm sauvegardé et relu', again.curve25519 === me.curve25519)

go.stdin.end()
console.log(failed ? `\n${failed} échec(s)` : '\nInteropérabilité vérifiée.')
process.exit(failed ? 1 : 0)
