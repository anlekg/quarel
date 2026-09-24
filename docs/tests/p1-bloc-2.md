# P1 bloc 2 — Guide de test (modération et accès)

> Test automatique : `make e2e-moderation` (24 vérifications, dont le bot d'exemple réellement connecté).

## Préparation

Comme au bloc 1 (`make build`, `make run-identity`, `make run-server`, comptes `alice` propriétaire et `bob` membre, `-p alice listen` dans un terminal). Pour tester le téléphone sans compte Twilio, lancer le serveur avec le fournisseur de développement :

```sh
QUAREL_PHONE_VERIFY=log make run-server
```

Les codes « SMS » apparaissent alors dans le journal du serveur (`code=123456`).

## 1. Exclusion temporaire
1. `-p alice timeout bob 10m flood` → bob ne peut plus écrire, réagir, inviter ni rejoindre le vocal (`timed_out`), mais lit toujours. S'il était en vocal, il en est sorti.
2. `-p alice listen` affiche « ⏸ exclusion temporaire de bob ».
3. `-p alice untimeout bob`, ou attendre la fin : bob peut de nouveau écrire.
4. Un administrateur ne peut pas être exclu (`cannot_timeout_admin`) ; un modérateur ne peut exclure que sous son rôle.

## 2. Suppression en masse
1. bob écrit plusieurs messages, puis `-p alice purge bob 1h` → « N message(s) de bob supprimé(s) ». `purge bob tout général` limite à un salon.
2. `-p alice ban carol --purge=7d raison…` bannit en effaçant ses messages des 7 derniers jours (`--purge=tout` : tous).

## 3. Journal de modération
`-p alice audit` : qui a fait quoi, à qui, avec la raison. `audit 10 member_ban` filtre. Donner le droit `view_audit_log` à un rôle pour le rendre lisible par d'autres. Les entrées de plus de 90 jours sont effacées.

## 4. Règles
1. `-p alice rules "1. Pas de spam. 2. Restez courtois."` (ou `rules -f regles.txt`). Les membres déjà présents ne sont pas bloqués.
2. Un nouveau membre voit les règles en rejoignant (`join`) et ne peut qu'observer jusqu'à `accept-rules`.
3. `srv-info` affiche les règles **avant** de rejoindre. `rules --clear` les retire.

## 5. Téléphone (avec `QUAREL_PHONE_VERIFY=log`)
1. `-p alice srv-set require_phone=true` (refusé si aucun fournisseur n'est configuré).
2. bob ne peut plus écrire (`phone_not_verified`). `-p bob phone +33 6 12 34 56 78` → code dans le journal du serveur ; `-p bob phone-verify +33612345678 <code>` → vérifié, il peut écrire.
3. Le même numéro ne peut pas vérifier un 2e membre présent (`phone_in_use`), ni jamais resservir après un bannissement (`phone_banned`).
4. La base ne contient pas le numéro, seulement une empreinte.
5. En production : compte Twilio de l'hébergeur, `QUAREL_PHONE_VERIFY=twilio` et les trois variables `QUAREL_TWILIO_*` (voir `CLAUDE.md`). **Non testé avec le vrai Twilio.**

## 6. Bots
1. `-p alice bot-create Pingbot` → jeton affiché une seule fois, avec la commande pour lancer le bot d'exemple.
2. Lancer cette commande (ou `./bin/pingbot` avec les mêmes variables) → « connected to … ».
3. `-p bob send général !ping` → le bot répond « pong » (en réponse au message).
4. `-p alice bot-token Pingbot` → nouveau jeton ; le bot en cours est déconnecté (l'ancien jeton ne marche plus). `bot-delete Pingbot` le supprime.
5. La documentation pour écrire un bot est dans `docs/api.md`.
