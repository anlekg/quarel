#!/usr/bin/env bash
# End-to-end voice test: real Identity + community server + embedded LiveKit,
# two headless Chromium with fake microphones. Run from the repo: make e2e-voice
set -uo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); trap 'pkill -P $$ 2>/dev/null; kill $IDP $SRVP 2>/dev/null; rm -rf "$D"' EXIT
export XDG_CONFIG_HOME=$D/cfg QUAREL_PASSWORD=motdepasse-solide PATH="$HOME/.local/bin:$PATH"
QUAREL_RATE_LIMITS=off QUAREL_ADDR=127.0.0.1:28080 QUAREL_ISSUER=localhost:28080 QUAREL_DATA_DIR=$D/id $B/quarel-identity > $D/id.log 2>&1 & IDP=$!
QUAREL_ADDR=127.0.0.1:28090 QUAREL_TRUSTED_ISSUERS=localhost:28080 QUAREL_DATA_DIR=$D/srv QUAREL_UPNP=off QUAREL_VOICE_PUBLIC_IP=local QUAREL_RATE_LIMITS=off $B/quarel-server > $D/srv.log 2>&1 & SRVP=$!
for _ in $(seq 1 50); do grep -q "voice ready" $D/srv.log && break; sleep 0.2; done
grep -q "voice ready" $D/srv.log || { echo "voice did not start"; cat $D/srv.log; exit 1; }
Q() { local p=$1; shift; $B/quarelctl -s http://127.0.0.1:28080 -p "$p" "$@"; }
for u in alice bob; do
  Q $u register $u@example.com $u >/dev/null
  Q $u verify-email $u@example.com "$(grep -o 'Quarel : [0-9]*' $D/id.log | tail -1 | grep -o '[0-9]*$')" >/dev/null
  Q $u login $u >/dev/null
done
Q alice claim localhost:28090 "$(grep -o 'unique) : [a-z0-9]*' $D/srv.log | awk '{print $NF}')" >/dev/null
Q bob join "$(Q alice invite 1 | grep -o 'quarel://[^ ]*' | head -1 | sed 's#127.0.0.1#localhost#')" >/dev/null
AURL=$(Q alice voice-test | grep -o 'https://[^ ]*voice-test[^ ]*'); BURL=$(Q bob voice-test | grep -o 'https://[^ ]*voice-test[^ ]*')
cd "$REPO/test/e2e" && node voice-e2e.mjs "$AURL" "$BURL"
