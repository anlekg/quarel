#!/usr/bin/env bash
# P1 scenario, accounts: password reset and change, email and pseudo change,
# profile and avatar, blocking, presence, account deletion, operator tool
# (disable on legal order) and its effect on community servers.
# Run from the repo: make e2e-accounts
set -uo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); PIDS=()
trap 'kill "${PIDS[@]}" 2>/dev/null; rm -rf "$D"' EXIT
export XDG_CONFIG_HOME=$D/cfg QUAREL_PASSWORD=motdepasse-solide
IDENV="QUAREL_ADDR=127.0.0.1:28080 QUAREL_ISSUER=localhost:28080 QUAREL_DATA_DIR=$D/id QUAREL_RATE_LIMITS=off"
env $IDENV $B/quarel-identity > $D/id.log 2>&1 & PIDS+=($!); sleep 0.5
QUAREL_ADDR=127.0.0.1:28090 QUAREL_TRUSTED_ISSUERS=localhost:28080 QUAREL_DATA_DIR=$D/srv QUAREL_UPNP=off QUAREL_VOICE=off \
  QUAREL_RATE_LIMITS=off QUAREL_DISABLED_POLL=1s $B/quarel-server > $D/srv.log 2>&1 & PIDS+=($!); sleep 0.7
Q() { local p=$1; shift; $B/quarelctl -s http://127.0.0.1:28080 -p "$p" "$@" 2>&1; }
FAIL=0
expect() { # expect "<description>" "<output>" "<text that must appear>"
  if grep -qF -- "$3" <<<"$2"; then echo "✔ $1"; else echo "✘ $1 — attendu : $3"; echo "$2" | sed 's/^/    /'; FAIL=1; fi
}
code() { grep -o 'Quarel : [0-9]*' $D/id.log | tail -1 | grep -o '[0-9]*$'; }
for u in alice bob carol; do
  Q $u register $u@example.com $u >/dev/null; Q $u verify-email $u@example.com "$(code)" >/dev/null; Q $u login $u "pc-$u" >/dev/null
done
Q alice friend-add bob >/dev/null; Q bob friend-accept alice >/dev/null
Q alice claim localhost:28090 "$(grep -o 'unique) : [a-z0-9]*' $D/srv.log | awk '{print $NF}')" >/dev/null
LINK=$(Q alice invite 10 | grep -o 'quarel://[^ ]*' | head -1)
Q bob join "$LINK" >/dev/null

echo "## Présence et blocage"
Q alice dm-listen > $D/alice.log & PIDS+=($!)
Q bob dm-listen > $D/bob.log & PIDS+=($!); sleep 0.8
expect "connexion d'alice : en ligne pour bob" "$(Q bob friends)" "🟢 en ligne"
Q alice status dnd >/dev/null; sleep 0.3
expect "statut « ne pas déranger » vu par bob" "$(Q bob friends)" "⛔ ne pas déranger"
expect "…et reçu en direct" "$(cat $D/bob.log)" "alice : ⛔ ne pas déranger"
Q alice status invisible >/dev/null; sleep 0.3
expect "invisible = hors ligne pour les amis" "$(Q bob friends)" "⚫ hors ligne"
Q alice status online >/dev/null
Q alice block bob >/dev/null
expect "blocage : l'amitié disparaît" "$(Q bob friends)" "Amis (0)"
expect "le bloqué ne peut plus envoyer de demande (alice « n'existe pas »)" "$(Q bob friend-add alice)" "not_found"
Q alice unblock bob >/dev/null
expect "après déblocage, l'amitié peut reprendre" "$(Q bob friend-add alice)" "demande envoyée"
Q alice friend-accept bob >/dev/null

echo "## Profil"
printf '\x89PNG\r\n\x1a\n%064d' 0 > $D/avatar.png
Q alice bio "Organise le tournoi de tarot." >/dev/null
Q alice avatar $D/avatar.png >/dev/null
P=$(Q bob profile alice)
expect "profil public : bio" "$P" "Organise le tournoi de tarot."
AV=$(grep -o 'http://[^ ]*/avatar[^ ]*' <<<"$P")
[ "$(curl -s -o /dev/null -w '%{content_type}' "$AV")" = "image/png" ] && echo "✔ avatar servi (sans session)" || { echo "✘ avatar"; FAIL=1; }

echo "## Pseudo, mot de passe, email"
expect "changement de pseudo" "$(Q alice pseudo alicia)" "alicia@localhost:28080"
Q alice join localhost:28090 >/dev/null   # reconnection: the server learns the new handle
expect "le serveur communautaire affiche le nouveau pseudo, même membre" "$(Q bob members)" "alicia"
expect "pseudo : une fois par jour" "$(Q alice pseudo alice2)" "pseudo_change_too_soon"
expect "changement de mot de passe" "$(QUAREL_NEW_PASSWORD=nouveau-mdp-alice Q alice passwd)" "Mot de passe changé"
expect "l'ancien mot de passe ne marche plus" "$(Q alice login alicia)" "invalid_credentials"
export QUAREL_PASSWORD=nouveau-mdp-alice
expect "le nouveau oui" "$(Q alice login alicia pc-alice)" "Connexion réussie"
Q alice email-change alicia@example.org >/dev/null
expect "nouvelle adresse confirmée par code" "$(Q alice email-confirm "$(code)")" "alicia@example.org"
export QUAREL_PASSWORD=motdepasse-solide

echo "## Mot de passe oublié"
Q bob forgot-password bob@example.com >/dev/null
expect "réinitialisation par code" "$(QUAREL_NEW_PASSWORD=nouveau-mdp-bob Q bob reset-password bob@example.com "$(code)")" "Mot de passe réinitialisé"
expect "tous les appareils de bob sont déconnectés" "$(QUAREL_PASSWORD=nouveau-mdp-bob Q bob me)" "unauthorized"
expect "bob se reconnecte avec le nouveau mot de passe" "$(QUAREL_PASSWORD=nouveau-mdp-bob Q bob login bob pc-bob)" "Connexion réussie"

echo "## Désactivation par l'opérateur (réquisition)"
expect "l'outil exige une raison" "$(env $IDENV $B/quarel-identity admin disable bob 2>&1)" "obligatoires"
expect "désactivation consignée" "$(env $IDENV $B/quarel-identity admin disable bob --reason 'réquisition 2026-42' --by e2e 2>&1)" "désactivé (consigné)"
expect "bob ne peut plus utiliser son compte" "$(QUAREL_PASSWORD=nouveau-mdp-bob Q bob me)" "account_disabled"
sleep 2.5
expect "le serveur communautaire a fermé sa session (liste publiée)" "$(grep 'disabled by its identity service' $D/srv.log)" "member's account disabled"
expect "bob ne peut plus se connecter au serveur communautaire" "$(QUAREL_PASSWORD=nouveau-mdp-bob Q bob members)" "account_disabled"
env $IDENV $B/quarel-identity admin enable bob --reason 'levée 2026-43' --by e2e >/dev/null 2>&1; sleep 2.5
expect "réactivé : bob revient sur le serveur" "$(QUAREL_PASSWORD=nouveau-mdp-bob Q bob members)" "bob"
expect "journal de l'opérateur" "$(env $IDENV $B/quarel-identity admin log 2>&1)" "account_disable"

echo "## Suppression de compte"
expect "une confirmation erronée ne supprime rien" "$(printf 'mauvais\n' | Q carol delete-account)" "rien n'a été supprimé"
expect "suppression définitive" "$(printf 'carol\n' | Q carol delete-account)" "Compte supprimé"
expect "le compte n'existe plus" "$(Q carol login carol)" "invalid_credentials"
[ "$(sqlite3 $D/id/identity.db "SELECT COUNT(*) FROM users WHERE pseudo='carol'" 2>/dev/null || echo 0)" = "0" ] && echo "✔ plus aucune trace de carol dans la base" || { echo "✘ carol encore en base"; FAIL=1; }
exit $FAIL
