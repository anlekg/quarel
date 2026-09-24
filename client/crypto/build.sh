#!/usr/bin/env bash
# Rebuilds the WebAssembly crypto module (needs Rust with the
# wasm32-unknown-unknown target, and wasm-bindgen-cli 0.2.128).
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/.cargo/bin:$PATH"
cargo build --release --target wasm32-unknown-unknown --locked 2>/dev/null || cargo build --release --target wasm32-unknown-unknown
wasm-bindgen --target web --out-dir ../src/crypto/wasm --out-name quarel_crypto target/wasm32-unknown-unknown/release/quarel_crypto.wasm
ls -la ../src/crypto/wasm
