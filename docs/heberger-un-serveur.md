# Héberger un serveur communautaire Quarel

Un serveur communautaire, c'est votre « serveur Discord » à vous : salons textuels et vocaux, rôles, modération. Il tourne chez vous (un PC, un mini-serveur, un NAS avec Docker) ; les membres s'y connectent avec leur compte Quarel.

## Installation

Il faut Docker. Depuis une copie du dépôt :

```sh
git clone https://github.com/anlekg/quarel.git && cd quarel
docker build -f Dockerfile.server -t quarel-server .
docker run -d --name quarel --restart unless-stopped --network host \
  -v quarel-data:/data -e QUAREL_SERVER_NAME="Mon serveur" quarel-server
docker logs quarel
```

Le journal affiche un **code de revendication** : la première personne qui l'utilise (`quarelctl claim <adresse> <code>`, puis avec le vrai client) devient propriétaire du serveur.

- `--network host` permet l'**ouverture automatique des ports** de la box (UPnP). Sans UPnP, ouvrez à la main vers cette machine : **8090/tcp** (application et connexions), **7882/udp** et **7881/tcp** (voix). `quarelctl network` diagnostique la joignabilité.
- **HTTPS** est actif d'office avec un certificat lié à l'identité du serveur : les clients le vérifient sans autorité ni nom de domaine. Avec un nom de domaine : `-e QUAREL_TLS=acme -e QUAREL_TLS_DOMAIN=… -e QUAREL_TLS_EMAIL=…` (le port public 443 doit mener au 8090).
- Comptes acceptés : ceux de `identity.quarel.app` par défaut ; ajoutez d'autres services avec `-e QUAREL_TRUSTED_ISSUERS=identity.quarel.app,identity.mon-asso.fr`.
- Toutes les options sont décrites dans `CLAUDE.md` (variables `QUAREL_*`).

## Sauvegarder

La sauvegarde se fait **serveur en marche** ; elle contient la base (messages, membres, rôles…), la **clé du serveur** (son identité : sans elle, les membres verraient un autre serveur) et les fichiers envoyés :

```sh
docker exec quarel /quarel-server backup - > quarel-$(date +%F).tar.gz
```

Gardez ces fichiers en lieu sûr, idéalement chiffrés : ils permettent de se faire passer pour votre serveur.

## Restaurer

Serveur arrêté, sur la même machine ou une nouvelle :

```sh
docker stop quarel
docker run --rm -i -v quarel-data:/data quarel-server restore - --force < quarel-2026-09-24.tar.gz
docker start quarel
```

La restauration vérifie l'archive avant de toucher à quoi que ce soit (bon type de sauvegarde, version compatible, clé présente) ; sans `--force` elle refuse d'écrire par-dessus des données existantes, et avec `--force` les anciennes données sont **mises de côté** dans `before-restore-<date>` (à supprimer à la main une fois rassuré). Le serveur garde la même identité : les membres se reconnectent sans rien faire.

## Mettre à jour

```sh
cd quarel && git pull && docker build -f Dockerfile.server -t quarel-server .
docker stop quarel && docker rm quarel
docker run -d --name quarel --restart unless-stopped --network host -v quarel-data:/data quarel-server
```

Les données restent dans le volume `quarel-data`. Au premier démarrage d'une nouvelle version qui change la base, le serveur en **garde d'abord une copie** (`server.db.pre-v<version>-<date>`, les 3 dernières sont conservées), puis la met à jour. Une ancienne version refuse de démarrer sur une base plus récente plutôt que de l'abîmer : réinstallez la version récente, ou restaurez une sauvegarde faite avec l'ancienne. `docker run --rm quarel-server version` affiche la version et le schéma de base.

Par prudence, faites une sauvegarde avant chaque mise à jour.
