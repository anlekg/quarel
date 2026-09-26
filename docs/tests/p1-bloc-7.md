# P1 bloc 7 — Guide de test (sauvegarde, restauration, mises à jour)

> Test automatique : `make e2e-ops` (15 vérifications). Procédure Docker : `site/src/content/docs/wiki/heberger/serveur-communautaire.md`.

## 1. Sauvegarde à chaud
Avec `make run-server` en marche :
```sh
QUAREL_DATA_DIR=./data/server ./bin/quarel-server backup serveur.tar.gz
QUAREL_DATA_DIR=./data/identity QUAREL_ISSUER=localhost:8080 ./bin/quarel-identity backup identity.tar.gz
```
Relancer la même commande : refus (« existe déjà »). `tar tzf serveur.tar.gz` montre la base, la clé, les fichiers joints.

## 2. Restauration
1. Écrire un message après la sauvegarde, puis arrêter les deux services.
2. `QUAREL_DATA_DIR=./data/server ./bin/quarel-server restore serveur.tar.gz` → refusé (données présentes) ; avec `--force` → restauré, anciennes données dans `data/server/before-restore-…`.
3. Essayer de restaurer `identity.tar.gz` dans le serveur → refusé (mauvais type).
4. Relancer : le message écrit après la sauvegarde a disparu (retour à l'état sauvegardé), tout le reste est là, et `quarelctl` se reconnecte sans alerte : c'est bien le même serveur.

## 3. Mises à jour
`./bin/quarel-server version` affiche la version et le schéma de base. Lors d'une future mise à jour qui change la base, un fichier `server.db.pre-v<n>-<date>` apparaîtra dans le dossier de données avant la migration.
