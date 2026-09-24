#!/usr/bin/env bash
# P1 scenario, operations: live backups of both services, restore (refused
# over existing data without --force), everything back afterwards, including
# the community server's identity.
# Run from the repo: make e2e-ops
set -uo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); PIDS=()
trap 'kill "${PIDS[@]}" 2>/dev/null; rm -rf "$D"' EXIT
export XDG_CONFIG_HOME=$D/cfg QUAREL_PASSWORD=motdepasse-solide
IDENV="QUAREL_ADDR=127.0.0.1:18080 QUAREL_ISSUER=localhost:18080 QUAREL_DATA_DIR=$D/id QUAREL_RATE_LIMITS=off"
SRVENV="QUAREL_ADDR=127.0.0.1:18090 QUAREL_TRUSTED_ISSUERS=localhost:18080 QUAREL_DATA_DIR=$D/srv QUAREL_UPNP=off QUAREL_VOICE=off QUAREL_RATE_LIMITS=off"
start() { env $IDENV $B/quarel-identity >> $D/id.log 2>&1 & ID=$!; env $SRVENV $B/quarel-server >> $D/srv.log 2>&1 & SRV=$!; PIDS+=($ID $SRV); sleep 0.8; }
stop() { kill $ID $SRV; wait $ID $SRV 2>/dev/null; }
Q() { local p=$1; shift; (cd $D && $B/quarelctl -s http://127.0.0.1:18080 -p "$p" "$@" 2>&1); }
FAIL=0
expect() { # expect "<description>" "<output>" "<text that must appear>"
  if grep -qF -- "$3" <<<"$2"; then echo "✔ $1"; else echo "✘ $1 — attendu : $3"; echo "$2" | sed 's/^/    /'; FAIL=1; fi
}
refuse() {
  if grep -qF -- "$3" <<<"$2"; then echo "✘ $1 — ne devrait pas contenir : $3"; FAIL=1; else echo "✔ $1"; fi
}
code() { grep -o 'Quarel : [0-9]*' $D/id.log | tail -1 | grep -o '[0-9]*$'; }
start
for u in alice bob; do
  Q $u register $u@example.com $u >/dev/null; Q $u verify-email $u@example.com "$(code)" >/dev/null; Q $u login $u "pc-$u" >/dev/null
done
Q alice claim localhost:18090 "$(grep -o 'unique) : [a-z0-9]*' $D/srv.log | awk '{print $NF}')" >/dev/null
Q bob join "$(Q alice invite 5 | grep -o 'quarel://[^ ]*' | head -1)" >/dev/null
Q alice role-create Modo kick_members >/dev/null; Q alice role-add bob Modo >/dev/null
Q alice send général "Message d'avant la sauvegarde" >/dev/null
printf 'Règlement du tournoi\n' > $D/reglement.txt
FID=$(Q bob send-file général $D/reglement.txt | grep -o 'download [a-z0-9]*' | awk '{print $2}')
SID=$(Q alice srv-info | grep -o 'id      : [a-z2-7]*' | awk '{print $NF}')

echo "## Sauvegardes à chaud"
expect "sauvegarde du serveur communautaire (en marche)" "$(env $SRVENV $B/quarel-server backup $D/srv.tar.gz 2>&1)" "Sauvegarde écrite"
expect "sauvegarde du service Identity (en marche)" "$(env $IDENV $B/quarel-identity backup $D/id.tar.gz 2>&1)" "Sauvegarde écrite"
expect "une sauvegarde n'écrase pas un fichier existant" "$(env $SRVENV $B/quarel-server backup $D/srv.tar.gz 2>&1)" "existe déjà"
tar tzf $D/srv.tar.gz | grep -q "attachments/$FID" && echo "✔ les fichiers joints sont dans la sauvegarde" || { echo "✘ fichier joint absent"; FAIL=1; }
Q alice send général "Message d'après la sauvegarde" >/dev/null
stop

echo "## Restauration"
expect "refusée par-dessus des données existantes" "$(env $SRVENV $B/quarel-server restore $D/srv.tar.gz 2>&1)" "--force"
expect "une sauvegarde Identity ne se restaure pas dans un serveur" "$(env $SRVENV $B/quarel-server restore $D/id.tar.gz --force 2>&1)" "pas d'un quarel-server"
expect "restauration du serveur" "$(env $SRVENV $B/quarel-server restore $D/srv.tar.gz --force 2>&1)" "Identité : serveur $SID"
expect "restauration d'Identity" "$(env $IDENV $B/quarel-identity restore $D/id.tar.gz --force 2>&1)" "restaurée"
ls -d $D/srv/before-restore-* >/dev/null 2>&1 && echo "✔ anciennes données mises de côté, pas effacées" || { echo "✘ anciennes données perdues"; FAIL=1; }
start
H=$(Q bob history général)
expect "même serveur (identité et certificat vérifiés par le client)" "$H" "général"
expect "les messages sont revenus" "$H" "Message d'avant la sauvegarde"
refuse "l'état est celui de la sauvegarde" "$H" "Message d'après la sauvegarde"
expect "les rôles aussi" "$(Q alice members)" "Modo"
Q alice download $FID copie.txt >/dev/null
cmp -s $D/reglement.txt $D/copie.txt && echo "✔ fichier joint restauré à l'identique" || { echo "✘ fichier joint"; FAIL=1; }
expect "les comptes et sessions Identity sont revenus" "$(Q bob me)" "bob@localhost:18080"
exit $FAIL
