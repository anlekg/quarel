#!/usr/bin/env bash
# P1 scenario, moderation and access: timeout, purge, audit log, rules screen,
# phone verification (generic webhook provider), bots with the example bot.
# Run from the repo: make e2e-moderation
set -uo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); PIDS=()
trap 'kill "${PIDS[@]}" 2>/dev/null; rm -rf "$D"' EXIT
export XDG_CONFIG_HOME=$D/cfg QUAREL_PASSWORD=motdepasse-solide
QUAREL_ADDR=127.0.0.1:18080 QUAREL_ISSUER=localhost:18080 QUAREL_DATA_DIR=$D/id QUAREL_RATE_LIMITS=off \
  $B/quarel-identity > $D/id.log 2>&1 &
PIDS+=($!); sleep 0.5
# Fake SMS gateway behind the generic webhook provider: it records what it receives.
python3 -c '
import http.server, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        open(sys.argv[1], "ab").write(self.rfile.read(int(self.headers["Content-Length"])) + b"\n")
        self.send_response(200); self.end_headers()
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", 18095), H).serve_forever()' $D/sms.log & PIDS+=($!)
QUAREL_ADDR=127.0.0.1:18090 QUAREL_TRUSTED_ISSUERS=localhost:18080 QUAREL_DATA_DIR=$D/srv QUAREL_UPNP=off QUAREL_VOICE=off \
  QUAREL_RATE_LIMITS=off QUAREL_PHONE_VERIFY=webhook QUAREL_PHONE_WEBHOOK_URL=http://127.0.0.1:18095/sms \
  $B/quarel-server > $D/srv.log 2>&1 &
PIDS+=($!); sleep 0.7
Q() { local p=$1; shift; $B/quarelctl -s http://127.0.0.1:18080 -p "$p" "$@" 2>&1; }
FAIL=0
expect() { # expect "<description>" "<output>" "<text that must appear>"
  if grep -qF -- "$3" <<<"$2"; then echo "✔ $1"; else echo "✘ $1 — attendu : $3"; echo "$2" | sed 's/^/    /'; FAIL=1; fi
}
code() { grep -o 'Quarel : [0-9]*' $D/id.log | tail -1 | grep -o '[0-9]*$'; }
for u in alice bob carol dave; do
  Q $u register $u@example.com $u >/dev/null; Q $u verify-email $u@example.com "$(code)" >/dev/null; Q $u login $u "pc-$u" >/dev/null
done
Q alice claim localhost:18090 "$(grep -o 'unique) : [a-z0-9]*' $D/srv.log | awk '{print $NF}')" >/dev/null
LINK=$(Q alice invite 10 | grep -o 'quarel://[^ ]*' | head -1)
Q bob join "$LINK" >/dev/null
Q alice listen > $D/listen.log & PIDS+=($!); sleep 0.5

echo "## Exclusion temporaire, purge, journal"
Q bob send général "spam 1" >/dev/null; Q bob send général "spam 2" >/dev/null
expect "exclusion temporaire" "$(Q alice timeout bob 10m flood)" "lecture seule"
expect "exclusion : écriture refusée" "$(Q bob send général encore)" "timed_out"
expect "…mais peut lire" "$(Q bob history général)" "spam 2"
expect "fin de l'exclusion" "$(Q alice untimeout bob)" "Fin de l'exclusion"
expect "purge des messages récents" "$(Q alice purge bob 1h)" "2 message(s) de bob supprimé(s)"
AUDIT=$(Q alice audit)
expect "journal : exclusion avec raison" "$AUDIT" "exclusion temporaire → bob « flood »"
expect "journal : purge" "$AUDIT" "messages supprimés → bob (2)"

echo "## Règles et téléphone"
Q alice rules "1. Pas de spam. 2. Restez courtois." >/dev/null
expect "les membres déjà présents ne sont pas bloqués" "$(Q bob send général toujours là)" "toujours là"
JOIN=$(Q carol join "$LINK")
expect "le nouveau membre voit les règles en arrivant" "$JOIN" "Pas de spam"
expect "sans accepter : lecture seule" "$(Q carol send général bonjour)" "rules_not_accepted"
Q carol accept-rules >/dev/null
expect "après acceptation : peut écrire" "$(Q carol send général bonjour)" "carol : bonjour"
expect "téléphone exigé" "$(Q alice srv-set require_phone=true)" "téléphone vérifié exigé"
expect "sans numéro vérifié : lecture seule" "$(Q carol send général re)" "phone_not_verified"
Q carol phone +33 6 12 34 56 78 >/dev/null; sleep 0.2
SMS=$(grep -o '"code":"[0-9]*"' $D/sms.log | tail -1 | grep -o '[0-9]*')
expect "mauvais code refusé" "$(Q carol phone-verify +33612345678 000000)" "invalid_code"
expect "bon code accepté" "$(Q carol phone-verify +33612345678 $SMS)" "Numéro vérifié"
expect "numéro vérifié : peut écrire" "$(Q carol send général re)" "carol : re"
grep -q '612345678' $D/srv/server.db && { echo "✘ numéro en clair dans la base"; FAIL=1; } || echo "✔ le numéro n'est pas stocké en clair"
Q alice ban carol >/dev/null
Q dave join "$LINK" >/dev/null; Q dave accept-rules >/dev/null
Q dave phone +33612345678 >/dev/null; sleep 0.2
SMS=$(grep -o '"code":"[0-9]*"' $D/sms.log | tail -1 | grep -o '[0-9]*')
expect "le numéro d'un banni ne peut pas resservir" "$(Q dave phone-verify +33612345678 $SMS)" "phone_banned"
Q alice srv-set require_phone=false >/dev/null

echo "## Bots"
BOT=$(Q alice bot-create Pingbot)
expect "bot créé avec son jeton" "$BOT" "Jeton du bot"
TOKEN=$(grep -o 'qb_[A-Za-z0-9_-]*' <<<"$BOT" | head -1)
SID=$(grep -o 'QUAREL_SERVER_ID=[a-z2-7]*' <<<"$BOT" | head -1 | cut -d= -f2)
QUAREL_URL=https://localhost:18090 QUAREL_SERVER_ID=$SID QUAREL_BOT_TOKEN=$TOKEN $B/pingbot > $D/bot.log 2>&1 & PIDS+=($!)
sleep 0.7
expect "le bot se connecte (certificat auto-signé vérifié)" "$(cat $D/bot.log)" "connected to"
Q bob send général '!ping' >/dev/null; sleep 0.5
expect "le bot répond" "$(Q bob history général)" "Pingbot : pong"
Q alice bot-token Pingbot >/dev/null; sleep 0.5
expect "ancien jeton révoqué : le bot est déconnecté" "$(cat $D/bot.log)" "disconnected"

sleep 0.3
L=$(cat $D/listen.log)
expect "exclusion vue en direct" "$L" "exclusion temporaire de bob"
expect "suppression en masse vue en direct" "$L" "2 message(s) supprimé(s) par la modération"
exit $FAIL
