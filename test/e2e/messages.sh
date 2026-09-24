#!/usr/bin/env bash
# P1 scenario, messages and channels: replies, reactions, pins, files, search,
# threads, announcement channels, unread state, notification settings.
# Run from the repo: make e2e-messages
set -uo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); PIDS=()
trap 'kill "${PIDS[@]}" 2>/dev/null; rm -rf "$D"' EXIT
export XDG_CONFIG_HOME=$D/cfg QUAREL_PASSWORD=motdepasse-solide
QUAREL_ADDR=127.0.0.1:18080 QUAREL_ISSUER=localhost:18080 QUAREL_DATA_DIR=$D/id QUAREL_RATE_LIMITS=off \
  $B/quarel-identity > $D/id.log 2>&1 &
PIDS+=($!); sleep 0.5
QUAREL_ADDR=127.0.0.1:18090 QUAREL_TRUSTED_ISSUERS=localhost:18080 QUAREL_DATA_DIR=$D/srv QUAREL_UPNP=off QUAREL_VOICE=off \
  QUAREL_RATE_LIMITS=off QUAREL_MAX_UPLOAD_MB=1 $B/quarel-server > $D/srv.log 2>&1 &
PIDS+=($!); sleep 0.7
Q() { local p=$1; shift; (cd $D && $B/quarelctl -s http://127.0.0.1:18080 -p "$p" "$@" 2>&1); }
FAIL=0
expect() { # expect "<description>" "<output>" "<text that must appear>"
  if grep -qF -- "$3" <<<"$2"; then echo "✔ $1"; else echo "✘ $1 — attendu : $3"; echo "$2" | sed 's/^/    /'; FAIL=1; fi
}
code() { grep -o 'Quarel : [0-9]*' $D/id.log | tail -1 | grep -o '[0-9]*$'; }
for u in alice bob; do
  Q $u register $u@example.com $u >/dev/null; Q $u verify-email $u@example.com "$(code)" >/dev/null; Q $u login $u "pc-$u" >/dev/null
done
Q alice claim localhost:18090 "$(grep -o 'unique) : [a-z0-9]*' $D/srv.log | awk '{print $NF}')" >/dev/null
Q bob join "$(Q alice invite 5 | grep -o 'quarel://[^ ]*' | head -1)" >/dev/null
Q bob listen > $D/listen.log & PIDS+=($!); sleep 0.5
id() { grep -o '\] [0-9]*' <<<"$1" | head -1 | grep -o '[0-9]*'; }

echo "## Réponses, réactions, épingles"
M1=$(id "$(Q alice send général "Qui vient au tournoi de l'école samedi ?")")
expect "réponse citant le message d'origine" "$(Q bob reply général $M1 Moi !)" "↱ alice : Qui vient au tournoi"
Q bob react général $M1 👍 >/dev/null; Q alice react général $M1 👍 >/dev/null
expect "réactions comptées (et marquées pour soi)" "$(Q bob history général)" "👍 2*"
expect "épingler exige manage_messages" "$(Q bob pin général $M1)" "missing_permissions"
Q alice pin général $M1 >/dev/null
expect "message épinglé listé" "$(Q bob pins général)" "📌 alice"

echo "## Fichiers"
printf 'Liste des inscrits\n' > $D/inscrits.txt
SENT=$(Q bob send-file général $D/inscrits.txt "la liste")
expect "fichier envoyé avec le message" "$SENT" "📎 inscrits.txt (text/plain"
FID=$(grep -o 'download [a-z0-9]*' <<<"$SENT" | awk '{print $2}')
Q alice download $FID copie.txt >/dev/null
cmp -s $D/inscrits.txt $D/copie.txt && echo "✔ fichier téléchargé à l'identique" || { echo "✘ fichier différent"; FAIL=1; }
head -c 2000000 /dev/urandom > $D/gros.bin
expect "limite de taille du serveur (1 Mo ici)" "$(Q bob send-file général $D/gros.bin)" "file_too_large"

echo "## Recherche, fils, annonces"
expect "recherche sans accents ni casse" "$(Q bob search ECOLE)" "1 résultat(s)"
expect "filtre par auteur" "$(Q bob search liste from:bob)" "1 résultat(s)"
expect "fil créé sur un message" "$(Q bob thread général $M1)" "Fil créé : 🧵 Qui vient au tournoi"
TH=$(Q bob channels | grep '🧵' | grep -o 'id [0-9]*' | awk '{print $2}')
Q alice send $TH "Inscriptions ouvertes" >/dev/null
expect "le fil apparaît sous son salon" "$(Q bob channels)" "🧵 Qui vient au tournoi"
Q alice channel-create annonces announcement >/dev/null
expect "salon d'annonces : lecture seule pour les membres" "$(Q bob send annonces coucou)" "missing_permissions"

echo "## Non-lus et notifications"
Q alice send général "@bob tu as vu ?" >/dev/null
expect "non-lus et mention comptés" "$(Q bob unread)" "🔔 1 mention(s)"
Q bob read général >/dev/null
expect "« read » vide bien les non-lus de général" "$(Q bob unread | grep -c général)" "0"
expect "sourdine d'un salon" "$(Q bob notify annonces none muet=toujours)" "#annonces            rien — en sourdine"

echo "## Temps réel"
sleep 0.3
L=$(cat $D/listen.log)
expect "réaction reçue en direct" "$L" "👍 alice réagit au message $M1"
expect "création du fil reçue en direct" "$L" "salon 🧵 Qui vient au tournoi"
expect "fichier annoncé en direct" "$L" "📎 inscrits.txt"
exit $FAIL
