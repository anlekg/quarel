#!/usr/bin/env bash
# P1 scenario, peer-to-peer calls between friends: Olm-encrypted signalling,
# direct audio, audio through the TURN relay, relay refused by the user.
# Run from the repo: make e2e-calls
set -uo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); PIDS=()
trap 'kill "${PIDS[@]}" 2>/dev/null; rm -rf "$D"' EXIT
export XDG_CONFIG_HOME=$D/cfg QUAREL_PASSWORD=motdepasse-solide
# Local TURN relay on the loopback address (private peers allowed for the test only).
QUAREL_RATE_LIMITS=off QUAREL_ADDR=127.0.0.1:28080 QUAREL_ISSUER=localhost:28080 QUAREL_DATA_DIR=$D/id \
  QUAREL_TURN=on QUAREL_UPNP=off QUAREL_TURN_LISTEN=127.0.0.1:13478 QUAREL_TURN_PUBLIC_IP=127.0.0.1 QUAREL_TURN_PORTS=50000-50050 QUAREL_TURN_ALLOW_PRIVATE=1 \
  $B/quarel-identity > $D/id.log 2>&1 & PIDS+=($!)
sleep 0.5
Q() { local p=$1; shift; $B/quarelctl -s http://127.0.0.1:28080 -p "$p" "$@" 2>&1; }
FAIL=0
expect() { # expect "<description>" "<output>" "<text that must appear>"
  if grep -qF -- "$3" <<<"$2"; then echo "✔ $1"; else echo "✘ $1 — attendu : $3"; echo "$2" | sed 's/^/    /'; FAIL=1; fi
}
for u in alice bob carol; do
  Q $u register $u@example.com $u >/dev/null
  Q $u verify-email $u@example.com "$(grep -o 'Quarel : [0-9]*' $D/id.log | tail -1 | grep -o '[0-9]*$')" >/dev/null
  Q $u login $u "pc-$u" >/dev/null
  Q $u e2e >/dev/null
done
Q alice friend-add bob >/dev/null; Q bob friend-accept alice >/dev/null
expect "relais TURN démarré" "$(cat $D/id.log)" "TURN relay ready"

echo "## Appel direct"
Q bob call-listen --once --seconds 3 > $D/bob1.log & L=$!; PIDS+=($L); sleep 0.5
A=$(Q alice call bob --seconds 3)
wait $L
expect "alice reçoit l'audio de bob" "$A" "audio reçu ✔"
expect "chemin direct" "$A" "en direct"
expect "bob a répondu et reçoit l'audio d'alice" "$(cat $D/bob1.log)" "audio reçu ✔"

echo "## Appel par le relais"
Q bob call-listen --once --seconds 3 --relay-only > $D/bob2.log & L=$!; PIDS+=($L); sleep 0.5
A=$(Q alice call bob --seconds 3 --relay-only)
wait $L
expect "audio relayé par TURN" "$A" "audio reçu ✔"
expect "chemin : relais" "$A" "par le relais TURN"
expect "des deux côtés" "$(cat $D/bob2.log)" "audio reçu ✔"

echo "## Refus du relais, amis seulement"
Q alice calls relay=off >/dev/null
expect "relais refusé dans les réglages : jamais utilisé" "$(Q alice call bob --relay-only)" "aucun relais disponible"
expect "appels réservés aux amis" "$(Q carol call bob)" "ni un ami"
for w in "a=fingerprint" "candidate" "127.0.0.1 "; do
  if cat $D/id/identity.db* | grep -aqF "$w"; then echo "✘ « $w » (signalisation) en clair dans la base"; FAIL=1; fi
done
echo "✔ la signalisation n'apparaît pas en clair sur le serveur"
exit $FAIL
