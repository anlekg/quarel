# Jalon 5 — Guide de test (amis et messages privés chiffrés)

Objectif : vérifier les amis, les messages privés chiffrés de bout en bout, la validation d'un nouvel appareil avec transfert de l'historique, et la révocation d'un appareil.

> Test automatique complet : `make e2e-dm` (16 vérifications, dont l'absence de texte en clair dans la base du serveur).

## Préparation

`make run-identity` (le serveur communautaire n'est pas nécessaire ici). Deux comptes, `alice` et `bob`, connectés (voir `docs/tests/jalon-1.md`). Pour simuler un 2ᵉ appareil d'alice, on utilise un autre profil : `-p alice2`.

## Principes (ce que le test doit confirmer)

- Les messages privés sont **réservés aux amis**.
- Le serveur ne voit que des données chiffrées et **efface chaque message dès que tous les appareils du destinataire l'ont reçu** : l'historique vit sur les appareils.
- Le **premier appareil** d'un compte est validé d'office. Tout **nouvel appareil** doit être validé depuis un appareil déjà validé, après comparaison d'un **code de vérification** (16 caractères). Tant qu'il n'est pas validé, il ne reçoit aucune clé et ne peut pas écrire.
- À la validation, le nouvel appareil reçoit la clé du compte et **tout l'historique**.
- La clé de chaque contact est mémorisée au premier contact : si le serveur en présentait une autre, l'envoi serait refusé avec un avertissement.

## Scénarios

### 1. Amis
1. `./bin/quarelctl -p alice friend-add bob` → « demande envoyée ».
2. `./bin/quarelctl -p bob friends` → la demande apparaît dans « Demandes reçues ».
3. `./bin/quarelctl -p bob friend-accept alice` → « amis ».
4. Autres cas : refuser (`friend-remove` sur une demande reçue), annuler (`friend-remove` sur une demande envoyée), retirer un ami. Si les deux s'envoient une demande, elle est acceptée automatiquement.
5. Écrire à quelqu'un qui n'est pas ami → refusé.

### 2. Messages chiffrés
1. `./bin/quarelctl -p alice e2e` → « ✔ validé » et le code de vérification de l'appareil.
2. Dans un terminal : `./bin/quarelctl -p bob dm-listen`.
3. `./bin/quarelctl -p alice dm bob Bonjour !` → apparaît instantanément chez bob.
4. `./bin/quarelctl -p alice dm-history bob` → la conversation ; `✓` après un message = reçu par tous les appareils de bob.
5. `dm-sync` récupère ce qui est arrivé pendant qu'on n'écoutait pas.

### 3. Nouvel appareil
1. `./bin/quarelctl -p alice2 login alice telephone` puis `./bin/quarelctl -p alice2 e2e` → « en attente de validation » + un code du type `ABCD-EFGH-IJKL-MNOP`.
2. `./bin/quarelctl -p alice2 dm bob test` → refusé (appareil non validé).
3. Bob écrit à alice pendant ce temps.
4. `./bin/quarelctl -p alice devices` → le téléphone apparaît « en attente », avec son code : **comparer les deux codes à l'œil**.
5. Essayer un mauvais code : `device-approve telephone AAAA-BBBB-CCCC-DDDD` → refusé avec un avertissement.
6. `./bin/quarelctl -p alice device-approve telephone <code>` → validé.
7. `./bin/quarelctl -p alice2 dm-sync` → « validé » et « historique reçu » ; `dm-history bob` montre **toute** la conversation, y compris le message de bob envoyé avant la validation.
8. Bob écrit de nouveau → lisible sur les deux appareils d'alice.

### 4. Révocation
1. `./bin/quarelctl -p alice sessions` puis `revoke-session <id du téléphone>`.
2. Bob écrit → « envoyé à 1 appareil » : le téléphone ne reçoit plus rien et sa session est fermée.

## Limites connues
- Si tous les appareils d'un compte sont perdus, l'historique est perdu (phrase de récupération prévue en P1).
- Un message envoyé par un appareil **juste avant** sa révocation, et pas encore récupéré par les autres appareils, est ignoré par précaution (l'appareil révoqué est peut-être volé).
- L'outil de test garde les clés dans un fichier non chiffré du profil ; le vrai client utilisera le trousseau du système.

## Retour de test

Pour chaque anomalie : la commande lancée, le résultat obtenu, le résultat attendu.
