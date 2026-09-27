---
title: Héberger un serveur communautaire
description: Installer votre propre serveur Quarel avec Docker ou sous Windows, le régler, le sauvegarder et le mettre à jour.
sidebar:
  order: 1
---

Un serveur communautaire, c'est votre « serveur Discord » à vous : salons textuels et vocaux, rôles, modération. Il tourne chez vous (un PC, un mini-serveur, un NAS avec Docker) ; les membres s'y connectent avec leur compte Quarel.

## Installation

Il faut Docker. Depuis une copie du dépôt :

```sh
git clone https://github.com/anlekg/quarel.git && cd quarel/deploy/server
docker compose up -d --build
docker compose logs server     # affiche l'adresse de l'administration et le code d'installation
```

(Sans Compose : `docker build -f Dockerfile.server -t quarel-server .` puis `docker run -d --name quarel --restart unless-stopped --network host -v quarel-data:/data quarel-server`.)

### Sous Windows

1. Téléchargez **`Quarel-Serveur-Setup-<version>.exe`** depuis la [page d’accueil](/#heberger) (empreintes SHA-256 : [SHA256SUMS](https://quarel.app/telechargements/SHA256SUMS)) et lancez-le (Windows 10 ou 11, 64 bits). Windows demande l'autorisation d'administrateur : l'installateur place le programme dans `Program Files`, autorise le serveur et le vocal dans le pare-feu, et le fait démarrer avec votre session.
   > Les installateurs ne sont pas encore signés : Windows SmartScreen affiche « Windows a protégé votre ordinateur » ; cliquez sur « Informations complémentaires » puis « Exécuter quand même ».
2. À la fin, la **page d'administration** s'ouvre dans votre navigateur (`http://127.0.0.1:8091`) : choisissez son mot de passe, puis suivez la page comme ci-dessous.
3. Une **icône Quarel** (verte) reste près de l'horloge : clic → page d'administration ; clic droit → état du serveur, « Lancer au démarrage de Windows », « Quitter ».

Sous Windows, la page d'administration n'est ouverte que sur l'ordinateur lui-même. Les données (base, clés, réglages, journal `quarel-server.log`) sont dans `%LOCALAPPDATA%\Quarel\Serveur` ; la désinstallation (Paramètres → Applications) les conserve.

Pour reconstruire l'installateur depuis les sources (Linux ou macOS, avec Go et NSIS) : `make windows`.

### Page d'administration

Ouvrez **`https://<adresse de la machine>:8091`** depuis n'importe quel appareil du réseau local (l'adresse est affichée dans le journal). Le certificat de la page est auto-signé : le navigateur affiche un avertissement à accepter une fois ; son **empreinte** (SHA-256) est écrite dans le journal pour vérifier qu'il s'agit bien du vôtre. Depuis la machine elle-même, `http://localhost:8091` suffit. La page ne répond qu'à l'adresse IP de la machine, à `localhost` et au nom de la machine (autres noms : `QUAREL_ADMIN_HOSTS`) : un site web ne peut pas la piloter depuis votre navigateur.

1. **Premier passage** : choisissez le mot de passe administrateur. Depuis un autre appareil que la machine elle-même, le **code d'installation** affiché dans le journal est demandé (personne d'autre sur le réseau ne peut prendre la main avant vous).
2. **Tableau de bord** : état du service, adresse, identifiant, membres, vocal, ports de la box. Tant que le serveur n'a pas de propriétaire, il affiche un **lien propriétaire** : dans l'application Quarel, « Rejoindre un serveur », collez-le : vous devenez propriétaire. Bouton « Renommer le serveur ». Si le propriétaire ne peut plus se connecter (compte perdu ou supprimé), **« Réinitialiser le propriétaire »** le retire et affiche un nouveau lien propriétaire. Le propriétaire peut aussi transmettre le serveur à un autre membre depuis l'application (clic sur le membre › « Transférer la propriété »).
3. **Réglages** : services d'identité acceptés, réseau et UPnP, certificat HTTPS, vocal, taille des fichiers, espace disque réservé (les fichiers sont refusés quand il reste moins de 1 Go libre), aperçus de liens, SMS. **Enregistrer** vérifie les réglages (refusés avec explication s'ils sont incohérents, rien n'est changé) puis redémarre le service en quelques secondes.
4. **Sauvegardes** : télécharger une archive complète, ou en restaurer une (les données actuelles sont mises de côté, jamais effacées).
5. **Journal** : les derniers messages du serveur.
6. **Services d'identité acceptés** (bas du tableau de bord) : pour chacun, si votre serveur est accepté. Certains services n'autorisent que les serveurs qu'ils ont approuvés : bouton **Demander l'accès** (on vous demande un moyen de vous contacter), puis attendre l'approbation de leur opérateur. Un service peut aussi avoir **bloqué** votre serveur : ses utilisateurs ne peuvent plus le rejoindre.

La page n'est **jamais ouverte vers Internet** : l'UPnP ne l'ouvre pas et elle refuse toute adresse qui ne vient pas du réseau local. Mot de passe oublié : arrêtez le serveur, supprimez `admin.json` dans le volume de données, relancez. Les réglages de la page elle-même (`QUAREL_ADMIN_PUBLIC=1` pour l'ouvrir au-delà du réseau local, `QUAREL_ADMIN_TLS=off` pour accepter HTTP depuis les autres machines, `QUAREL_ADMIN_HOSTS`) ne se règlent que par variables d'environnement : la page ne peut pas élargir elle-même son exposition.

Les réglages sont enregistrés dans `settings.json` (volume de données). Une variable d'environnement `QUAREL_*` (option `-e` de Docker, fichier Compose) reste prioritaire : le réglage correspondant apparaît alors verrouillé dans la page. `QUAREL_ADMIN_ADDR` change l'adresse de la page (`off` pour la couper).

### Bon à savoir

- `network_mode: host` (`--network host`) permet l'**ouverture automatique des ports** de la box (UPnP). Sans UPnP, ouvrez à la main vers cette machine : **8090/tcp** (application et connexions), **7882/udp** et **7881/tcp** (voix) ; le tableau de bord les rappelle.
- **HTTPS** est actif d'office avec un certificat lié à l'identité du serveur : l'application le vérifie sans autorité ni nom de domaine. Avec un nom de domaine, choisissez Let's Encrypt dans les réglages (le port public 443 doit mener au 8090). Le **client web** (navigateur) ne peut joindre que des serveurs avec un nom de domaine et Let's Encrypt.
- Comptes acceptés : ceux de `identity.quarel.app` par défaut ; ajoutez d'autres services dans les réglages.

## Venir de Discord

Votre communauté est déjà sur Discord ? L’outil d’import recrée ses rôles, salons, droits et emojis sur votre serveur Quarel : voir [Migrer depuis Discord](/wiki/heberger/migrer-depuis-discord/).

## Modération automatique

Dans l’application : menu du serveur › Paramètres du serveur › **Modération automatique** (permission « gérer le serveur »).

- **Mots interdits**, un par ligne : majuscules et accents ignorés, mots entiers ; `arnaq*` couvre tous les mots qui commencent par « arnaq » ; plusieurs mots : cette suite exacte.
- **Refuser les liens**, **limiter les mentions** par message, **refuser les messages répétés** (le même message une troisième fois en 30 secondes).
- **Exclusion automatique** après 3 messages refusés en 10 minutes (durée au choix).

Le message refusé n’est pas publié ; son auteur voit pourquoi. Chaque refus est noté dans le journal de modération, sans le texte du message. Les personnes qui peuvent gérer les messages d’un salon (modération, administration, propriétaire) n’y sont pas soumises.

## Exiger un numéro de téléphone (facultatif)

Pour limiter les faux comptes et les retours de bannis, un serveur peut exiger un numéro vérifié par SMS (`quarelctl srv-set require_phone=true`). Le serveur ne garde jamais le numéro, seulement une empreinte. Choisissez comment les SMS partent (page d'administration, « Réglages », « Vérification du téléphone », ou variables ci-dessous) :

- **Webhook (recommandé)** : le serveur envoie `{phone, code, text}` à une adresse de votre choix, qui fait partir le SMS. Vous pouvez y brancher votre propre téléphone Android (application « passerelle SMS »), un modem GSM ou n'importe quel fournisseur : aucun tiers imposé.
  `-e QUAREL_PHONE_VERIFY=webhook -e QUAREL_PHONE_WEBHOOK_URL=https://… -e QUAREL_PHONE_WEBHOOK_SECRET=…`
  Avec un secret, chaque requête porte `X-Quarel-Signature: sha256=<HMAC-SHA256 du corps>` : vérifiez-la côté passerelle.
- **OVHcloud SMS (recommandé)** : fournisseur européen. Créez un compte SMS et des clés d'API (droit `POST /sms/*/jobs`), puis
  `-e QUAREL_PHONE_VERIFY=ovh -e QUAREL_OVH_APP_KEY=… -e QUAREL_OVH_APP_SECRET=… -e QUAREL_OVH_CONSUMER_KEY=… -e QUAREL_OVH_SMS_SERVICE=sms-xx00000-1` (et `QUAREL_OVH_SMS_SENDER` si vous avez un nom d'expéditeur validé).
- **Twilio (option)** : fournisseur américain ; les numéros de vos membres transitent chez lui. `QUAREL_PHONE_VERIFY=twilio` et les variables `QUAREL_TWILIO_*`.

Les SMS sont à vos frais selon le fournisseur.

## Sauvegarder

Le plus simple : page d'administration, **Sauvegardes**, « Télécharger une sauvegarde ». En ligne de commande :

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
