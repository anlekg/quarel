# Jalon 2 — Guide de test (serveur communautaire, texte)

Objectif : vérifier qu'un serveur communautaire auto-hébergé fonctionne de bout en bout : revendication par le propriétaire, invitations, salons, messages en temps réel, mentions, surnoms, départ.

## Préparation (3 terminaux + 1 par utilisateur « à l'écoute »)

```sh
make run-identity   # terminal 1 : service Identity sur :8080 (codes email affichés ici)
make run-server     # terminal 2 : serveur communautaire sur :8090
```

Au premier démarrage, le terminal 2 affiche un encadré avec un **code de revendication** : il fait de son utilisateur le propriétaire.

Les données sont dans `data/identity/` et `data/server/`. Pour repartir de zéro : arrêter les deux services et supprimer `data/`.

Créer deux comptes Identity (voir `docs/tests/jalon-1.md`), par exemple :
```sh
./bin/quarelctl -p alice register alice@example.com alice   # puis verify-email et login
./bin/quarelctl -p bob   register bob@example.com bob       # puis verify-email et login
```

`./bin/quarelctl help` liste toutes les commandes. Un salon se désigne par son nom (`général`) ou son id.

## Scénarios

### 1. Revendication du serveur
1. `./bin/quarelctl -p alice srv-info localhost:8090` → nom, id, accès « privé », 0 membre.
2. `./bin/quarelctl -p alice claim localhost:8090 <code>` → « Vous êtes maintenant propriétaire ».
3. Réutiliser le même code avec bob → `invalid_claim`.
4. Redémarrer le serveur → plus aucun code affiché (il a un propriétaire).

### 2. Invitations et serveur privé
1. `./bin/quarelctl -p bob join localhost:8090` → `invite_required` (serveur privé par défaut).
2. `./bin/quarelctl -p alice invite 1 24h` → affiche un lien `quarel://…?sid=…` (1 utilisation, 24 h).
3. `./bin/quarelctl -p bob join '<lien>'` → « Bienvenue ».
4. Un 3ᵉ compte essaie le même lien → `invalid_invite` (déjà utilisé).
5. `./bin/quarelctl -p alice invites` → liste avec compteur d'utilisations ; `invite-revoke <code>`.
6. Bob peut aussi créer des invitations, mais ne voit et ne révoque que les siennes.
7. `./bin/quarelctl -p alice srv-set access=public` → n'importe quel compte peut rejoindre sans invitation.

### 3. Salons (propriétaire)
1. `./bin/quarelctl -p alice channels` → arborescence par défaut (Salons textuels / # général, Salons vocaux / 🔊 Général).
2. `channel-create Projets category`, puis `channel-create dev text Projets`.
3. `channel-edit dev topic="Discussions techniques" name=développement`.
4. `channel-delete Projets` → la catégorie disparaît, `développement` remonte à la racine.
5. Bob essaie `channel-create pirate` → `forbidden` (réservé au propriétaire jusqu'aux rôles du jalon 3).

### 4. Messages en temps réel
1. Dans un terminal dédié : `./bin/quarelctl -p bob listen` → « Connecté … En écoute ».
2. `./bin/quarelctl -p alice send général Salut @bob !` → le message apparaît **instantanément** chez bob, avec « 🔔 vous êtes mentionné ».
3. `@everyone` dans un message → notification pour tous.
4. `./bin/quarelctl -p bob history général` → historique ; avec beaucoup de messages : `history général 10` puis la commande suggérée pour les plus anciens.
5. `edit général <id> nouveau texte` → « (modifié) » ; bob ne peut pas modifier un message d'alice (`forbidden`).
6. `delete général <id>` → l'auteur peut supprimer le sien ; le propriétaire peut supprimer n'importe lequel.
7. Envoyer un message dans le salon vocal → refusé (`not_text_channel` ou salon introuvable).
8. Chaque action (salon créé, surnom, suppression…) s'affiche en direct dans le terminal `listen`.

### 5. Membres
1. `./bin/quarelctl -p bob nick Bobby` → le nom affiché change partout ; `nick` sans argument l'efface.
2. `./bin/quarelctl -p alice members` → liste avec 👑 pour le propriétaire.
3. `./bin/quarelctl -p bob leave` → son terminal `listen` est déconnecté ; ses anciens messages restent visibles.
4. Pour revenir sur un serveur privé, bob a besoin d'une nouvelle invitation.
5. Le propriétaire ne peut pas quitter son serveur (`owner_cannot_leave`).

### 6. Sécurité de la connexion (optionnel)
1. Donner à bob un lien d'invitation dont on modifie le `sid=` → connexion refusée (« le lien désigne un autre serveur »).
2. Un serveur qui ne fait pas confiance au service Identity : relancer le serveur avec `QUAREL_TRUSTED_ISSUERS=autre.exemple` → `untrusted_issuer`.
3. Arrêter le service Identity après la connexion : le serveur communautaire continue de fonctionner (il vérifie les identités hors ligne).

### 7. Docker (optionnel)
```sh
make docker-server
docker run --rm --network host -e QUAREL_TRUSTED_ISSUERS=localhost:8080 -v quarel-server:/data quarel-server
```
(`--network host` pour que le conteneur joigne le service Identity lancé sur la machine.) Le code de revendication s'affiche dans la sortie du conteneur.

## Retour de test

Pour chaque anomalie : la commande lancée, le résultat obtenu, le résultat attendu.
