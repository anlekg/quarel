# Déployer un service Identity Quarel

Le service Identity gère les comptes (email, pseudo, mot de passe, 2FA), les amis et les messages privés chiffrés, et délivre les identités portables acceptées par les serveurs communautaires. L'instance officielle est `identity.quarel.app` ; n'importe qui peut héberger la sienne.

## Prérequis

- Une machine avec Docker et Docker Compose.
- Un **nom de domaine** pointant vers elle (ex. `identity.mon-asso.fr`). Il fait partie de chaque identité (`pseudo@identity.mon-asso.fr`) et de la configuration des serveurs qui l'acceptent : **choisissez-le une fois pour toutes**.
- Le port **443** ouvert vers la machine (Let's Encrypt et les utilisateurs y passent).
- Pour le **relais d'appels** (TURN, utilisé quand deux amis ne peuvent pas se joindre directement) : l'adresse IP publique de la machine (`QUAREL_TURN_PUBLIC_IP`) et les ports **UDP 3478 et 49160-49200** ouverts. Mettre `QUAREL_TURN=off` pour ne pas proposer de relais.
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

Pour que des serveurs communautaires acceptent vos comptes, leurs hébergeurs ajoutent votre domaine : `QUAREL_TRUSTED_ISSUERS=identity.quarel.app,identity.mon-asso.fr`.

## Relais d'appels

Les appels entre amis sont pair à pair : le son ne passe par aucun serveur Quarel. Quand aucun chemin direct n'existe (NAT stricts), le relais TURN intégré au service transmet le flux, **toujours chiffré de bout en bout** (il ne peut pas l'écouter). Les identifiants du relais sont temporaires et propres à chaque compte ; il refuse de relayer vers des adresses privées ou locales (il ne peut pas servir à atteindre votre réseau). Chaque utilisateur peut refuser le relais dans son client.

Avec Docker, la plage de ports du relais doit être publiée telle quelle (voir `compose.yaml`) ; ne l'élargissez pas trop (chaque port ouvert est une ligne de règle NAT pour Docker).

## Sauvegardes

Le volume `identity-data` contient **la base** (`identity.db`) et **la clé de signature** (`signing.key`). Perdre la clé oblige tous les serveurs à recharger vos clés (automatique) mais invalide les jetons en cours ; la voir volée permettrait d'usurper n'importe quel compte de votre service. Sauvegardez le volume chiffré, par exemple :

```sh
docker run --rm -v identity_identity-data:/data -v "$PWD":/out alpine \
  tar czf /out/identity-$(date +%F).tar.gz -C /data .
```

(Le nom exact du volume est donné par `docker volume ls`.)

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

La base est migrée automatiquement au démarrage ; faites une sauvegarde avant.
