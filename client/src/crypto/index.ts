// Loads the WebAssembly Olm/Megolm module (vodozemac) once.
import init, * as wasm from './wasm/quarel_crypto.js'
import wasmURL from './wasm/quarel_crypto_bg.wasm?url'

let ready: Promise<typeof wasm> | null = null

export function loadCrypto(): Promise<typeof wasm> {
  ready ??= init({ module_or_path: wasmURL }).then(() => wasm)
  return ready
}

export type Crypto = typeof wasm
