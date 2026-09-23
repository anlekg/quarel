#!/usr/bin/env bash
# End-to-end encrypted DM scenario with the real Identity service and quarelctl:
# friends, first devices, exchange, second device approval + history transfer,
# revocation, and a check that no plaintext reaches the server's database.
# Run from the repo: make e2e-dm
set -uo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); IDP=
trap 'kill $IDP 2>/dev/null; rm -rf "$D"' EXIT
export XDG_CONFIG_HOME=$D/cfg QUAREL_PASSWORD=motdepasse-solide
QUAREL_RATE_LIMITS=off QUAREL_ADDR=127.0.0.1:18080 QUAREL_ISSUER=localhost:18080 QUAREL_DATA_DIR=$D/id $B/quarel-identity > $D/id.log 2>&1 & IDP=$!
sleep 0.5
Q() { local p=$1; shift; $B/quarelctl -s http://127.0.0.1:18080 -p "$p" "$@" 2>&1; }
FAIL=0
expect() { # expect "<description>" "<output>" "<text that must appear>"
  if grep -qF -- "$3" <<<"$2"; then echo "✔ $1"; else echo "✘ $1 — attendu : $3"; echo "$2" | sed 's/^/    /'; FAIL=1; fi
}
for u in alice bob; do
  Q $u register $u@example.com $u >/dev/null
  Q $u verify-email $u@example.com "$(grep -o 'Quarel : [0-9]*' $D/id.log | tail -1 | grep -o '[0-9]*$')" >/dev/null
  Q $u login $u "pc-$u" >/dev/null
done
Q alice friend-add bob >/dev/null
expect "demande d'ami acceptée" "$(Q bob friend-accept alice)" "amis avec alice"
expect "premier appareil validé d'office" "$(Q alice e2e)" "✔ validé"
Q bob e2e >/dev/null
expect "message chiffré envoyé" "$(Q alice dm bob 'Bonjour Bob, message numéro un')" "chiffré de bout en bout, envoyé à 1 appareil"
expect "message déchiffré par bob" "$(Q bob dm-history alice)" "Bonjour Bob, message numéro un"
Q bob dm alice "Réponse de Bob" >/dev/null
expect "accusé de distribution" "$(Q alice dm-history bob)" "message numéro un  ✓"

Q alice2 login alice telephone >/dev/null
expect "second appareil en attente" "$(Q alice2 e2e)" "en attente de validation"
expect "appareil non validé ne peut pas écrire" "$(Q alice2 dm bob 'test')" "pas encore validé"
Q bob dm alice "Envoyé avant la validation du téléphone" >/dev/null
CODE=$(Q alice2 e2e | grep -o '[A-Z2-7]\{4\}-[A-Z2-7]\{4\}-[A-Z2-7]\{4\}-[A-Z2-7]\{4\}')
expect "mauvais code refusé" "$(Q alice device-approve telephone AAAA-BBBB-CCCC-DDDD)" "NE PAS l'approuver"
expect "bon code accepté" "$(Q alice device-approve telephone "$CODE")" "validé : il a reçu la clé du compte"
expect "le téléphone reçoit sa validation" "$(Q alice2 dm-sync)" "cet appareil a été validé"
expect "historique transféré, message d'avant validation compris" "$(Q alice2 dm-history bob)" "Envoyé avant la validation du téléphone"
Q bob dm alice "Lisible sur les deux appareils" >/dev/null
expect "nouveau message lu par le téléphone" "$(Q alice2 dm-sync)" "Lisible sur les deux appareils"
expect "nouveau message lu par le PC" "$(Q alice dm-sync)" "Lisible sur les deux appareils"

PHONE=$(Q alice sessions | grep telephone | awk '{print $1}')
Q alice revoke-session "$PHONE" >/dev/null
expect "après révocation, un seul appareil destinataire" "$(Q bob dm alice 'Après révocation')" "envoyé à 1 appareil"
expect "le téléphone révoqué est coupé" "$(Q alice2 dm-sync)" "unauthorized"

for w in "Bonjour Bob" "Réponse de Bob" "avant la validation" "Lisible" "révocation"; do
  if cat "$D"/id/identity.db* | grep -aqF "$w"; then echo "✘ texte en clair « $w » trouvé dans la base du serveur"; FAIL=1; fi
done
[ $FAIL = 0 ] && echo "✔ aucun texte de message en clair dans la base du serveur"
exit $FAIL
