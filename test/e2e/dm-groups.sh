#!/usr/bin/env bash
# P1 scenario, advanced private messages: groups (members who are not friends
# with each other, arrivals and departures with key renewal), edits and
# deletions, end-to-end encrypted files, typing indicators and read receipts.
# Run from the repo: make e2e-dm-groups
set -uo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); PIDS=()
trap 'kill "${PIDS[@]}" 2>/dev/null; rm -rf "$D"' EXIT
export XDG_CONFIG_HOME=$D/cfg QUAREL_PASSWORD=motdepasse-solide
QUAREL_RATE_LIMITS=off QUAREL_ADDR=127.0.0.1:28080 QUAREL_ISSUER=localhost:28080 QUAREL_DATA_DIR=$D/id QUAREL_DM_FILE_MAX_MB=1 $B/quarel-identity > $D/id.log 2>&1 & PIDS+=($!)
sleep 0.5
Q() { local p=$1; shift; (cd $D && $B/quarelctl -s http://127.0.0.1:28080 -p "$p" "$@" 2>&1); }
FAIL=0
expect() { # expect "<description>" "<output>" "<text that must appear>"
  if grep -qF -- "$3" <<<"$2"; then echo "✔ $1"; else echo "✘ $1 — attendu : $3"; echo "$2" | sed 's/^/    /'; FAIL=1; fi
}
refuse() { # refuse "<description>" "<output>" "<text that must NOT appear>"
  if grep -qF -- "$3" <<<"$2"; then echo "✘ $1 — ne devrait pas contenir : $3"; FAIL=1; else echo "✔ $1"; fi
}
for u in alice bob carol dave erin; do
  Q $u register $u@example.com $u >/dev/null
  Q $u verify-email $u@example.com "$(grep -o 'Quarel : [0-9]*' $D/id.log | tail -1 | grep -o '[0-9]*$')" >/dev/null
  Q $u login $u "pc-$u" >/dev/null
  Q $u e2e >/dev/null
done
for f in bob carol dave; do Q alice friend-add $f >/dev/null; Q $f friend-accept alice >/dev/null; done

echo "## Groupe"
expect "groupe créé entre amis" "$(Q alice dm-group Tarot bob carol)" "Groupe « Tarot » créé avec 3 membres"
expect "message chiffré pour tout le groupe" "$(Q alice dm Tarot 'Rendez-vous jeudi 20 h')" "envoyé à 2 appareil(s)"
expect "bob le lit" "$(Q bob dm-history Tarot)" "Rendez-vous jeudi 20 h"
expect "carol aussi (sans être amie avec bob)" "$(Q carol dm-history Tarot)" "Rendez-vous jeudi 20 h"
Q bob dm Tarot "J'apporte les cartes" >/dev/null
expect "carol lit bob, qui n'est pas son ami" "$(Q carol dm-history Tarot)" "bob : J'apporte les cartes"
Q alice dm-sync >/dev/null   # the last member to receive it
expect "accusé : distribué à tous les autres membres" "$(Q bob dm-history Tarot)" "J'apporte les cartes  ✓"
Q alice dm-add Tarot dave >/dev/null
Q alice dm Tarot "Bienvenue dave" >/dev/null
H=$(Q dave dm-history Tarot)
expect "le nouveau membre lit les messages suivants" "$H" "Bienvenue dave"
refuse "…mais pas les anciens" "$H" "Rendez-vous jeudi"
Q alice dm-kick Tarot carol >/dev/null
Q alice dm Tarot "Message après le départ de carol" >/dev/null
refuse "le membre retiré ne reçoit plus rien" "$(Q carol dm-history alice; Q carol dms)" "Message après le départ"
expect "les autres oui (nouvelle clé)" "$(Q bob dm-history Tarot)" "Message après le départ de carol"

echo "## Membre glissé dans le groupe par le service"
# A compromised Identity service adds erin behind the members' back: the apps
# only encrypt for members announced by a member (encrypted "members" event).
DB=$D/id/identity.db
CONV=$(sqlite3 $DB "SELECT id FROM conversations WHERE name = 'Tarot'")
ERIN=$(sqlite3 $DB "SELECT id FROM users WHERE pseudo = 'erin'")
sqlite3 $DB "PRAGMA busy_timeout = 5000; INSERT INTO conversation_members (conversation_id, user_id, joined_at) VALUES ('$CONV', '$ERIN', strftime('%s','now'))" >/dev/null
expect "l'intrus figure dans la liste du serveur" "$(Q erin dms)" "Tarot"
expect "alice est prévenue, rien n'est chiffré pour l'intrus" "$(Q alice dm Tarot 'Après l intrusion')" "non chiffré pour erin"
refuse "l'intrus ne lit rien" "$(Q erin dm-history Tarot)" "Après l intrusion"
expect "les vrais membres le lisent" "$(Q bob dm-history Tarot)" "Après l intrusion"
expect "dave, ajouté par alice, a été annoncé aux membres" "$(Q bob dm-history Tarot)" "a ajouté dave au groupe"

echo "## Code de sécurité"
CODE_A=$(Q alice safety bob | sed -n 2p | tr -d " ")
CODE_B=$(Q bob safety alice | sed -n 2p | tr -d " ")
[ -n "$CODE_A" ] && [ "$CODE_A" = "$CODE_B" ] && echo "✔ même code de sécurité des deux côtés ($CODE_A)" || { echo "✘ codes différents : $CODE_A / $CODE_B"; FAIL=1; }
Q alice safety bob ok >/dev/null
expect "contact marqué vérifié" "$(Q alice safety bob)" "(vérifié·e)"

echo "## Modifier, supprimer"
N=$(Q bob dm Tarot "Je serai là à 20 h" | grep -o '#[0-9]*' | head -1 | tr -d '#')
Q bob dm-edit Tarot $N "Je serai là à 21 h" >/dev/null
expect "modification vue par les autres" "$(Q alice dm-history Tarot)" "Je serai là à 21 h (modifié)"
expect "on ne modifie que ses propres messages" "$(Q alice dm-edit Tarot $N 'piraté')" "n'est pas un de vos messages"
Q bob dm-delete Tarot $N >/dev/null
refuse "suppression chez tout le monde" "$(Q dave dm-history Tarot)" "Je serai là"

echo "## Fichiers chiffrés"
head -c 3000 /dev/urandom > $D/photo.bin; printf 'SECRET-DANS-LE-FICHIER' >> $D/photo.bin
M=$(Q alice dm-file Tarot $D/photo.bin "la photo" | grep -o '#[0-9]*' | head -1 | tr -d '#')
expect "fichier annoncé dans la conversation" "$(Q bob dm-history Tarot)" "📎 photo.bin"
Q bob dm-download Tarot $M copie.bin >/dev/null
cmp -s $D/photo.bin $D/copie.bin && echo "✔ fichier déchiffré à l'identique" || { echo "✘ fichier différent"; FAIL=1; }
grep -rqa "SECRET-DANS-LE-FICHIER" $D/id/ && { echo "✘ contenu du fichier lisible sur le serveur"; FAIL=1; } || echo "✔ le serveur ne stocke que du chiffré"
expect "un non-membre ne peut pas le télécharger" "$(Q carol dm-download Tarot $M)" "ni un groupe ni un ami"

echo "## En train d'écrire, accusés de lecture"
Q bob dm-listen > $D/bob.log & PIDS+=($!); sleep 0.8
Q alice dm-typing Tarot >/dev/null
Q alice dm-read Tarot >/dev/null; sleep 0.4
L=$(cat $D/bob.log)
expect "« alice écrit » reçu en direct" "$L" "… alice écrit"
expect "accusé de lecture reçu" "$L" "👁 vu par alice"
Q alice privacy typing=off receipts=off >/dev/null
Q dave dm-typing Tarot >/dev/null; sleep 1
Q alice dm-typing Tarot >/dev/null; Q alice dm-read Tarot >/dev/null; sleep 0.4
L=$(cat $D/bob.log)
expect "dave toujours visible" "$L" "… dave écrit"
[ "$(grep -c 'alice écrit' <<<"$L")" = 1 ] && [ "$(grep -c 'vu par alice' <<<"$L")" = 1 ] && echo "✔ désactivés : plus rien d'alice" || { echo "✘ alice partage encore"; FAIL=1; }
echo "## Fichiers : en direct d'abord, serveur en secours"
# bob is online (dm-listen), dave is not.
head -c 5000 /dev/urandom > $D/plan.bin
N0=$(ls $D/id/dm-files | wc -l)   # the earlier photo still waits for dave
OUT=$(Q alice dm-file Tarot $D/plan.bin "le plan")
expect "envoi direct à l'appareil en ligne, serveur pour l'absent" "$OUT" "1 appareil(s) en direct, 1 via le serveur"
sleep 0.5
expect "bob l'a reçu en direct" "$(cat $D/bob.log)" "fichier reçu en direct"
P=$(grep -o '#[0-9]*' <<<"$OUT" | head -1 | tr -d '#')
expect "bob l'enregistre sans le serveur" "$(Q bob dm-download Tarot $P plan-bob.bin)" "depuis cet appareil"
[ "$(ls $D/id/dm-files | wc -l)" = $((N0 + 1)) ] && echo "✔ une seule copie serveur de plus, pour l'appareil absent" || { echo "✘ copies serveur : $(ls $D/id/dm-files | wc -l)"; FAIL=1; }
Q dave dm-history Tarot >/dev/null   # dave comes online: gets the copy, which is then deleted
[ "$(ls $D/id/dm-files | wc -l)" = 0 ] && echo "✔ copies serveur effacées dès que dave les a" || { echo "✘ copie serveur conservée"; FAIL=1; }
expect "dave l'a bien" "$(Q dave dm-download Tarot $P plan-dave.bin)" "depuis cet appareil"
cmp -s $D/plan.bin $D/plan-dave.bin && echo "✔ fichier identique" || { echo "✘ fichier différent"; FAIL=1; }
head -c 1500000 /dev/urandom > $D/video.bin   # over the server limit (1 MB here)
OUT=$(Q alice dm-file Tarot $D/video.bin "la vidéo")
expect "trop gros pour le serveur : direct seulement" "$OUT" "trop gros pour le serveur"
V=$(grep -o '#[0-9]*' <<<"$OUT" | head -1 | tr -d '#')
sleep 0.5
expect "dave le récupère plus tard, en direct depuis un autre membre en ligne" "$(Q dave dm-download Tarot $V video-dave.bin)" "en direct depuis un autre appareil"
cmp -s $D/video.bin $D/video-dave.bin && echo "✔ gros fichier identique" || { echo "✘ gros fichier différent"; FAIL=1; }
[ "$(ls $D/id/dm-files | wc -l)" = 0 ] && echo "✔ le gros fichier n'est jamais passé par le serveur" || { echo "✘ gros fichier stocké"; FAIL=1; }
for w in "Rendez-vous jeudi" "Bienvenue dave" "21 h"; do
  if cat $D/id/identity.db* | grep -aqF "$w"; then echo "✘ « $w » en clair dans la base"; FAIL=1; fi
done
echo "✔ aucun message en clair dans la base du serveur"
exit $FAIL
