---
title: Déployer un service d’identité
description: "Héberger votre propre service de comptes Quarel : installation, inscriptions, relais d’appels, sauvegardes, outils de l’opérateur."
sidebar:
  order: 3
---

Le service Identity gère les comptes (email, pseudo, mot de passe, 2FA), les amis et les messages privés chiffrés, et délivre les identités portables acceptées par les serveurs communautaires. L'instance officielle est `identity.quarel.app` ; n'importe qui peut héberger la sienne.

## Prérequis

- Une machine avec Docker et Docker Compose.
- Un **nom de domaine** pointant vers elle (ex. `identity.mon-asso.fr`). Il fait partie de chaque identité (`pseudo@identity.mon-asso.fr`) et de la configuration des serveurs qui l'acceptent : **choisissez-le une fois pour toutes**.
- Le port **443** ouvert vers la machine (Let's Encrypt et les utilisateurs y passent).
- Pour le **relais d'appels** (TURN, utilisé quand deux amis ne peuvent pas se joindre directement) : les ports **UDP 3478 et 49160-49200** ouverts vers la machine. L'adresse IP publique est trouvée automatiquement (`QUAREL_TURN_PUBLIC_IP=auto` : box par UPnP, sinon STUN ; suivie si elle change) ou indiquée à la main. À la maison, avec le réseau de l'hôte (`network_mode: host`, ou l'exécutable Windows), le service ouvre lui-même ces ports sur la box par **UPnP** (`QUAREL_UPNP=off` pour l'empêcher). Mettre `QUAREL_TURN=off` pour ne pas proposer de relais.
- Un compte SMTP pour envoyer les codes (vérification d'email, mot de passe oublié). Sans SMTP, les codes n'apparaissent que dans le journal du service : suffisant pour un essai, pas pour de vrais utilisateurs.

## Installation

```sh
git clone https://github.com/anlekg/quarel.git
cd quarel/deploy/identity
cp .env.example .env      # renseigner le domaine, l'email Let's Encrypt, le SMTP
docker compose up -d --build
curl https://identity.mon-asso.fr/v1/health    # {"status":"ok"}
```

Le certificat HTTPS est obtenu automatiquement au premier accès.

**Derrière un proxy HTTPS existant** (Caddy, nginx, Traefik…) : utiliser `docker compose -f compose.proxy.yaml up -d --build`. Le service écoute alors en HTTP sur `127.0.0.1:8080` ; le proxy doit transmettre `https://<domaine>` vers cette adresse, WebSockets compris (`/v1/gateway`), et `X-Forwarded-For`.

### Page d'administration

Le service a une page d'administration sur le port **8081**, publiée seulement sur la machine elle-même (`127.0.0.1:8081`, et elle refuse toute adresse Internet). Sur un serveur loué, passez par un tunnel SSH :

```sh
ssh -L 8081:127.0.0.1:8081 utilisateur@mon-serveur    # puis ouvrir http://localhost:8081
```

Au premier passage, choisissez le mot de passe administrateur. On y trouve :
- **Tableau de bord** : nom public, nombre de comptes, certificat, emails, relais d'appels, avec des alertes (nom public encore « localhost », pas de SMTP…) ;
- **Réglages** : tout ce que contient `.env`, modifiable sans ligne de commande. Une variable présente dans `.env` reste prioritaire (le réglage est alors verrouillé dans la page) : vous pouvez vider `.env` et tout régler dans la page ;
- **Comptes** : rechercher, désactiver ou réactiver un compte (raison obligatoire, consignée), journal de l'opérateur, changement de la clé de signature ;
- **Sauvegardes** et **Journal**.

### Qui peut s'inscrire, quels serveurs sont autorisés

Dans **Réglages** :
- **Inscriptions** : ouvertes, **sur invitation** (codes créés dans la page « Invitations », et par vos utilisateurs si vous leur en accordez), ou fermées ; éventuellement limitées à certains domaines d'email et à un nombre de comptes.
- **Serveurs communautaires** :
  - **tous, sauf ceux que vous bloquez** (recommandé) : la page « Serveurs » tient une liste noire que l'application respecte ; vous ne savez pas où vont vos utilisateurs ;
  - **seulement les serveurs approuvés** : un serveur demande l'accès depuis sa propre page d'administration, vous l'approuvez dans « Serveurs ». Les jetons d'identité sont alors chiffrés pour ce seul serveur : aucun autre ne peut accepter vos comptes. En contrepartie, votre service voit à quels serveurs chacun se connecte (il ne le conserve pas).

Pour que des serveurs communautaires acceptent vos comptes, leurs hébergeurs ajoutent votre domaine : `QUAREL_TRUSTED_ISSUERS=identity.quarel.app,identity.mon-asso.fr`.

### Sous Windows

`Quarel-Identite-Setup-<version>.exe` installe le service avec une icône (violette) près de l'horloge et sa page d'administration sur `http://127.0.0.1:8081` (données dans `%LOCALAPPDATA%\Quarel\Identite`). Les prérequis restent les mêmes : nom de domaine, port 443 vers la machine (Let's Encrypt, à choisir dans les réglages), SMTP. Convient à une petite instance (association, famille) sur un PC allumé en permanence.

## Relais d'appels

Les appels entre amis sont pair à pair : le son ne passe par aucun serveur Quarel. Quand aucun chemin direct n'existe (NAT stricts), le relais TURN intégré au service transmet le flux, **toujours chiffré de bout en bout** (il ne peut pas l'écouter). Les identifiants du relais sont temporaires et propres à chaque compte ; il refuse de relayer vers des adresses privées ou locales (il ne peut pas servir à atteindre votre réseau). Chaque utilisateur peut refuser le relais dans son client.

Avec Docker, la plage de ports du relais doit être publiée telle quelle (voir `compose.yaml`) ; ne l'élargissez pas trop (chaque port ouvert est une ligne de règle NAT pour Docker).

## Sauvegardes

Le volume `identity-data` contient **la base** (`identity.db`), **la clé de signature** (`signing.key`) et les fichiers chiffrés des conversations. Perdre la clé invalide les jetons en cours ; la voir volée permettrait d'usurper n'importe quel compte de votre service. La sauvegarde intégrée fonctionne service en marche :

```sh
docker compose exec -T identity /quarel-identity backup - > identity-$(date +%F).tar.gz
```

Restauration (service arrêté ; les données présentes sont mises de côté, jamais effacées) :

```sh
docker compose stop identity
docker compose run --rm -T identity restore - --force < identity-2026-09-24.tar.gz
docker compose start identity
```

Gardez les sauvegardes chiffrées, hors de la machine.

## Outils de l'opérateur

Ils s'exécutent sur la machine, jamais à distance, et chaque action est consignée :

```sh
docker compose exec identity /quarel-identity admin help
docker compose exec identity /quarel-identity admin disable <pseudo|email|id> --reason "réquisition n°…" --by "Nom"
docker compose exec identity /quarel-identity admin enable  <pseudo|email|id> --reason "…" --by "Nom"
docker compose exec identity /quarel-identity admin log
```

**Désactiver un compte** ne se fait que sur ordre juridique : la personne ne peut plus se connecter ni obtenir de jeton, ses connexions ouvertes sont fermées, et les serveurs communautaires l'apprennent sous 10 minutes (liste publique `GET /v1/disabled-accounts`). Ses données sont conservées : une réactivation la rétablit à l'identique. Le service central ne peut ni bannir d'un serveur ni lire les messages : chaque serveur modère lui-même, et les messages privés sont chiffrés de bout en bout.

**Changer la clé de signature** (par précaution, ou en cas de doute sur sa confidentialité) :

```sh
docker compose exec identity /quarel-identity admin rotate-signing-key --reason "rotation annuelle" --by "Nom"
docker compose restart identity
```

L'ancienne clé privée est détruite ; sa clé publique reste publiée le temps que les jetons qu'elle a signés expirent (`QUAREL_TOKEN_TTL`, 12 h), si bien que personne n'est déconnecté.

## Mises à jour

```sh
git pull && docker compose up -d --build
```

La base est migrée automatiquement au démarrage, après en avoir gardé une copie (`identity.db.pre-v<version>-<date>`). Une ancienne version refuse de démarrer sur une base plus récente. Faites tout de même une sauvegarde avant.
