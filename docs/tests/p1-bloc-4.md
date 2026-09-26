# P1 bloc 4 — Guide de test (comptes)

> Test automatique : `make e2e-accounts` (30 vérifications avec les vrais binaires et l'outil de l'opérateur).

## Préparation
`make run-identity` (sans SMTP, les codes s'affichent dans son journal), `make run-server`, comptes `alice` et `bob` amis, `alice` propriétaire du serveur et `bob` membre.

## 1. Mot de passe
1. `-p bob passwd` → ancien puis nouveau mot de passe ; ses autres appareils sont déconnectés, un email d'alerte part (journal).
2. `-p bob forgot-password bob@example.com` → code dans le journal ; `-p bob reset-password bob@example.com <code>` → nouveau mot de passe, **tous** les appareils déconnectés. Si bob a la 2FA, le code 2FA est aussi demandé.
3. Une adresse inconnue donne la même réponse (on ne peut pas savoir quels emails sont inscrits).

## 2. Email et pseudo
1. `-p alice email-change alice@exemple.org` → mot de passe, code envoyé à la **nouvelle** adresse ; `email-confirm <code>` ; l'ancienne adresse reçoit un avertissement.
2. `-p alice pseudo alicia` → nouveau handle `alicia@…`. Sur le serveur communautaire, après reconnexion (`join localhost:8090`), la liste des membres affiche `alicia` : c'est le même membre (rôles, bans, messages conservés). Un 2e changement le même jour est refusé.

## 3. Profil
`-p alice bio …`, `-p alice avatar image.png` (PNG, JPEG, GIF, WebP, 1 Mo), puis `-p bob profile alice` → bio et adresse de l'avatar, ouvrable dans un navigateur sans être connecté.

## 4. Présence et blocage
1. `-p alice dm-listen` (garde alice connectée) ; `-p bob friends` → alice « 🟢 en ligne ». `-p alice status dnd|idle|invisible|online` → bob voit le changement (en direct dans son `dm-listen`) ; invisible = hors ligne.
2. `-p alice block bob` → plus d'amitié ; bob qui redemande reçoit « not_found » (il ne sait pas qu'il est bloqué). `unblock`, `blocks`.

## 5. Suppression de compte
`-p carol delete-account` → saisir son pseudo, mot de passe (et 2FA) → tout est effacé ; le pseudo et l'email redeviennent libres.

## 6. Outil de l'opérateur
Avec les mêmes variables que `make run-identity` :
1. `QUAREL_DATA_DIR=./data/identity ./bin/quarel-identity admin disable bob --reason "test" --by CP` → bob ne peut plus rien faire ; sur le serveur communautaire sa session est fermée (au plus tard 10 min ; `QUAREL_DISABLED_POLL=10s make run-server` pour aller plus vite).
2. `… admin enable bob --reason …` → tout revient. `… admin log` → le journal.
3. `… admin rotate-signing-key --reason …` puis redémarrer le service : `curl localhost:8080/.well-known/quarel-identity` montre 2 clés ; les membres connectés aux serveurs ne sont pas déconnectés.

## 7. Déploiement Docker du service central
Suivre `site/src/content/docs/wiki/heberger/service-identite.md` (nécessite un domaine pour Let's Encrypt ; sinon la variante `compose.proxy.yaml` en local).
