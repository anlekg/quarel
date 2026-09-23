#!/usr/bin/env bash
# Milestone 6 scenario: recovery phrase after losing every device, TLS
# identity pinning of community servers, and rate limiting.
# Run from the repo: make e2e-security
set -uo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); PIDS=()
trap 'kill "${PIDS[@]}" 2>/dev/null; rm -rf "$D"' EXIT
export XDG_CONFIG_HOME=$D/cfg QUAREL_PASSWORD=motdepasse-solide
start_identity() { # port, extra env...
  local port=$1; shift
  env "$@" QUAREL_ADDR=127.0.0.1:$port QUAREL_ISSUER=localhost:$port QUAREL_DATA_DIR=$D/id$port $B/quarel-identity >> $D/id$port.log 2>&1 &
  PIDS+=($!); sleep 0.5
}
start_server() { # data dir
  QUAREL_ADDR=127.0.0.1:18090 QUAREL_TRUSTED_ISSUERS=localhost:18080 QUAREL_DATA_DIR=$1 QUAREL_UPNP=off QUAREL_VOICE=off \
    QUAREL_RATE_LIMITS=off $B/quarel-server > $1.log 2>&1 &
  SRV=$!; PIDS+=($SRV); sleep 0.7
}
Q() { local p=$1; shift; $B/quarelctl -s http://127.0.0.1:18080 -p "$p" "$@" 2>&1; }
FAIL=0
expect() { # expect "<description>" "<output>" "<text that must appear>"
  if grep -qF -- "$3" <<<"$2"; then echo "✔ $1"; else echo "✘ $1 — attendu : $3"; echo "$2" | sed 's/^/    /'; FAIL=1; fi
}
code() { grep -o 'Quarel : [0-9]*' $D/id18080.log | tail -1 | grep -o '[0-9]*$'; }

start_identity 18080 QUAREL_RATE_LIMITS=off
for u in alice bob; do
  Q $u register $u@example.com $u >/dev/null; Q $u verify-email $u@example.com "$(code)" >/dev/null; Q $u login $u "pc-$u" >/dev/null
done
Q alice friend-add bob >/dev/null; Q bob friend-accept alice >/dev/null
Q alice dm bob "Message un" >/dev/null; Q bob dm alice "Message deux" >/dev/null

echo "## Phrase de récupération"
expect "sans sauvegarde, l'état le signale" "$(Q alice recovery-status)" "Aucune sauvegarde"
SETUP=$(Q alice recovery-setup)
expect "phrase générée et sauvegarde envoyée" "$SETUP" "Sauvegarde chiffrée envoyée"
PHRASE=$(grep -o '[0-9]\+\. [^ ]\+' <<<"$SETUP" | sort -n | awk '{print $2}' | tr '\n' ' ')
[ "$(wc -w <<<"$PHRASE")" = 12 ] && echo "✔ 12 mots affichés" || { echo "✘ phrase illisible : $PHRASE"; FAIL=1; }
Q alice dm bob "Message trois, après la création de la phrase" >/dev/null
expect "sauvegarde active" "$(Q alice recovery-status)" "Sauvegarde active"
expect "une seconde phrase exige --replace" "$(Q alice recovery-setup)" "--replace"

rm -f "$D"/cfg/quarelctl/alice.*   # alice loses her only device
Q alice login alice "nouveau-pc" >/dev/null
expect "le nouvel appareil n'est pas validé" "$(Q alice e2e)" "en attente de validation"
expect "une faute de frappe est détectée" "$(Q alice recovery-restore ${PHRASE/ /x })" "phrase invalide"
BOBPHRASE=$(Q bob recovery-setup | grep -o '[0-9]\+\. [^ ]\+' | sort -n | awk '{print $2}' | tr '\n' ' ')
expect "la phrase d'un autre compte est refusée" "$(Q alice recovery-restore $BOBPHRASE)" "n'ouvre pas la sauvegarde"
expect "la bonne phrase restaure le compte" "$(Q alice recovery-restore $PHRASE)" "cet appareil est validé, 3 message(s) retrouvé(s)"
expect "l'historique est de retour" "$(Q alice dm-history bob)" "Message trois, après la création de la phrase"
Q alice dm bob "Écrit depuis l'appareil restauré" >/dev/null
expect "bob lit le message de l'appareil restauré" "$(Q bob dm-sync)" "Écrit depuis l'appareil restauré"
for w in "Message un" "Message trois" "restauré"; do
  if cat "$D"/id18080/identity.db* | grep -aqF "$w"; then echo "✘ « $w » en clair dans la base du serveur"; FAIL=1; fi
done
echo "✔ la sauvegarde est opaque pour le serveur (aucun texte en clair dans sa base)"

echo "## HTTPS et identité du serveur"
start_server $D/srv
CLAIM=$(grep -o 'unique) : [a-z0-9]*' $D/srv.log | awk '{print $NF}')
expect "le serveur démarre en HTTPS" "$(cat $D/srv.log)" "url=https://127.0.0.1:18090"
expect "revendication en HTTPS (certificat auto-signé vérifié)" "$(Q alice claim localhost:18090 "$CLAIM")" "propriétaire"
LINK=$(Q alice invite 5 | grep -o 'quarel://[^ ]*' | head -1)
BAD=$(sed 's/sid=[a-z2-7]*/sid=aaaaaaaaaaaaaaaaaaaaaaaaaa/' <<<"$LINK")
expect "lien au sid falsifié refusé dès la poignée de main TLS" "$(Q bob join "$BAD")" "possible interception"
expect "lien authentique accepté" "$(Q bob join "$LINK")" "Bienvenue"
kill $SRV; sleep 0.3
start_server $D/srv-imposteur   # another server (other key) at the same address
expect "un autre serveur à la même adresse est refusé" "$(Q bob channels)" "possible interception"

echo "## Limitation de débit"
start_identity 18081
for i in 1 2 3 4 5; do
  $B/quarelctl -s http://127.0.0.1:18081 -p "rl$i" register rl$i@example.com user$i >/dev/null 2>&1
done
expect "6e inscription depuis la même adresse refusée" "$($B/quarelctl -s http://127.0.0.1:18081 -p rl6 register rl6@example.com user6 2>&1)" "rate_limited"
exit $FAIL
