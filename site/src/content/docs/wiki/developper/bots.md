---
title: Écrire un bot
description: Créer un bot pour un serveur communautaire, en quelques minutes, avec l’exemple pingbot.
sidebar:
  order: 2
---

Un bot est un **membre du serveur** qui se connecte avec un jeton au lieu d’un compte. Il reçoit ses droits par des rôles, comme tout le monde. Référence complète : [API des serveurs communautaires](/wiki/developper/api/).

## 1. Créer le bot

Dans l’application : menu du serveur › **Paramètres du serveur** › **Bots** › nom du bot › Créer (il faut la permission « Gérer le serveur »). Le **jeton** `qb_…` s’affiche **une seule fois** : gardez-le comme un mot de passe. « Nouveau jeton » le remplace (l’ancien cesse aussitôt de marcher).

Puis donnez-lui un rôle avec les permissions nécessaires (voir les salons, écrire…).

## 2. Le faire tourner

L’exemple [`examples/pingbot`](https://github.com/anlekg/quarel/tree/main/examples/pingbot) (Go, ~150 lignes) répond « pong » à « !ping » dans tous les salons où il peut écrire :

```sh
git clone https://github.com/anlekg/quarel.git && cd quarel
QUAREL_URL=https://mon-serveur.fr:8090 \
QUAREL_BOT_TOKEN=qb_… \
QUAREL_SERVER_ID=<identifiant du serveur> \
go run ./examples/pingbot
```

`QUAREL_SERVER_ID` (26 caractères, affiché avec le jeton et dans les liens d’invitation après `sid=`) sert à vérifier un serveur au **certificat auto-signé lié à son identité** ; inutile avec Let’s Encrypt.

## 3. Comment ça marche

1. **Temps réel** : WebSocket sur `/v1/gateway`, premier message `{"op":"auth","token":"qb_…"}`, puis `READY` (le bot, le serveur, les salons visibles…) et les événements (`MESSAGE_CREATE`…).
2. **Actions** : API REST avec `Authorization: Bearer qb_…`, par exemple `POST /v1/channels/{id}/messages {"content": "pong"}`.
3. **Reconnexion** : en cas de coupure, se reconnecter avec un délai croissant ; `READY` redonne l’état complet.

## Commandes slash

Un bot peut proposer des commandes : l’application les suggère dès qu’on tape `/` dans un salon.

1. **Déclarer** (à chaque connexion, par exemple sur `READY`) : `PUT /v1/bots/@me/commands` avec la liste complète, qui remplace la précédente :
   ```json
   [{"name": "lancer", "description": "Lance un dé",
     "options": [{"name": "faces", "description": "Nombre de faces", "type": "integer", "required": true}]}]
   ```
   Noms : 1 à 32 caractères `a-z 0-9 - _` ; 50 commandes et 10 options au plus ; types `string`, `integer`, `boolean`, `member` (identifiant de membre), `channel` (identifiant de salon) ; les options obligatoires d’abord.
2. **Recevoir** : quand un membre lance la commande dans un salon où il peut écrire, le serveur vérifie les options puis envoie **au bot seul** `INTERACTION_CREATE {id, name, options, channel_id, member_id}`. Le bot doit être connecté (sinon `409 bot_offline` pour le membre).
3. **Répondre** dans les 15 minutes : `POST /v1/interactions/{id}/reply {"content": "…", "ephemeral": false}`. Réponse publique : un message du bot dans le salon, marqué « X a utilisé /nom » (le bot doit pouvoir écrire dans ce salon). `"ephemeral": true` : seul l’auteur de la commande la voit, rien n’est enregistré.

Dans l’application, les arguments suivent la commande dans l’ordre (`/lancer 20`), `"entre guillemets"` pour garder des mots ensemble ; un texte en dernière option prend toute la fin de la ligne ; membres en `@pseudo`, salons en `#nom`. L’exemple `pingbot` déclare `/ping` et `/echo`.

N’importe quel langage fait l’affaire (HTTP + WebSocket). Pour un certificat auto-signé hors Go, la vérification du lien entre le certificat et l’identité du serveur est décrite dans l’[API](/wiki/developper/api/#1-principes) ; ne désactivez jamais simplement la vérification du certificat.

## Bonnes pratiques

- Respectez les limites de débit (`429` + `Retry-After`).
- Ignorez les champs et événements inconnus : l’API v1 peut s’enrichir.
- Ne répondez pas à vos propres messages ni à ceux des autres bots (membres `bot: true`) pour éviter les boucles.

## Plus simple : un webhook entrant

Pour **publier** des messages sans rien écouter (alerte de supervision, fin d’une sauvegarde, intégration continue, formulaire), un webhook suffit : pas de programme qui tourne, une simple requête HTTP.

1. Roue dentée du salon › **Webhooks** › nom › **Créer un webhook** (permission « gérer les salons »). Copiez l’adresse : elle n’est affichée qu’une fois.
2. Publiez :

```sh
curl -X POST -H 'Content-Type: application/json' \
  -d '{"content": "Sauvegarde terminée ✅"}' \
  https://mon-serveur.exemple:8090/v1/webhooks/<id>/<jeton>
```

Le message apparaît sous le nom du webhook, marqué **WEBHOOK**. Qui connaît l’adresse peut écrire dans le salon : gardez-la secrète, et supprimez le webhook s’il a fuité (l’adresse cesse aussitôt de fonctionner, ses anciens messages restent).

