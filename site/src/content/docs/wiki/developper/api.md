---
title: API des serveurs communautaires
description: Référence de l’API v1 des serveurs communautaires, pour les bots et les clients.
sidebar:
  order: 1
---

Cette page décrit l'API d'un **serveur communautaire** Quarel, telle que l'utilisent les clients et les **bots**. Elle est stable dans sa version `v1` : des champs et des routes peuvent s'ajouter, rien d'existant n'est retiré ni renommé sans passer à `v2`. Un client doit donc ignorer les champs et les événements qu'il ne connaît pas.

Un exemple complet de bot (~150 lignes de Go) se trouve dans [`examples/pingbot`](https://github.com/anlekg/quarel/blob/main/examples/pingbot/main.go).

## 1. Principes

- **Adresse** : chaque serveur est auto-hébergé, par exemple `https://mon-serveur.fr:8090`. Toutes les routes sont sous `/v1/`.
- **HTTPS** : un serveur a soit un certificat d'une autorité (Let's Encrypt…), soit un **certificat auto-signé lié à son identité**. Dans ce second cas, le certificat contient une preuve signée par la clé du serveur ; le client connaît l'identifiant du serveur (`server_id`, 26 caractères, donné par le lien d'invitation ou `quarelctl bot-create`) et vérifie cette preuve pendant la poignée de main TLS. En Go : `tlsbind.NewVerifier(hôte, serverID).Config()` (paquet `github.com/anlekg/quarel/pkg/tlsbind`) : il accepte un certificat d'autorité valable pour l'hôte, ou le certificat lié à ce serveur — et plus jamais un certificat ordinaire une fois le lien vérifié. Ne désactivez jamais simplement la vérification du certificat.
- **Format** : JSON en UTF-8 ; corps de requête de 64 Ko au plus (sauf envoi de fichiers) ; champs inconnus refusés (`400 bad_request`).
- **Dates** : RFC 3339 en UTC (`2026-09-24T09:30:00Z`).
- **Identifiants** : membres, fichiers : chaînes de 26 caractères ; salons, messages, rôles, entrées du journal : entiers croissants.
- **Permissions** : toujours échangées par **nom** (voir §6).
- **Erreurs** : statut HTTP + `{"error": {"code": "missing_permissions", "message": "…"}}`. Le **code est stable** et fait foi ; le message est indicatif (en anglais).
- **Limites de débit** : dépassement → `429 rate_limited` avec l'en-tête `Retry-After` (secondes). Par défaut : 600 requêtes/min par adresse IP, 10 messages par 10 s et 10 fichiers par minute par membre, 1 « en train d'écrire » par 3 s et par salon. L'hébergeur peut les modifier.

## 2. Authentification

Chaque requête porte `Authorization: Bearer <jeton>`.

### Bots
Un gestionnaire du serveur (permission `manage_server`) crée le bot : `POST /v1/bots {"name": "Mon bot"}` (ou `quarelctl bot-create`). La réponse contient le **jeton** (`qb_…`), affiché une seule fois : conservez-le comme un mot de passe. Il n'expire pas ; `POST /v1/bots/{id}/token` le remplace (l'ancien cesse immédiatement de fonctionner et ses connexions temps réel sont fermées).

Un bot est un membre comme les autres (`"bot": true`) : il reçoit ses droits par des **rôles** (`PUT /v1/members/{id}/roles/{role}`), sans compte sur un service d'identité. Il n'est pas concerné par l'écran de règles ni par la vérification du téléphone.

### Membres (clients)
Les personnes se connectent avec leur identité portable : `POST /v1/auth/challenge` → `{server_id, nonce}`, puis `POST /v1/auth/login {identity_token, nonce, proof, host, tls, invite?}` → `{session_token, expires_at, member, server, joined}`. La preuve (`idtoken.SignProofV2`) est la signature, par la clé de l'appareil, de `quarel-auth-v2␀<server_id>␀<nonce>␀<host>␀<tls>` : `host` est le nom contacté (minuscules, sans port ni crochets), `tls` vaut `binding` (certificat lié à l'identité du serveur, vérifié à chaque connexion) ou `authority` (certificat ordinaire : le serveur refuse alors, par `403 wrong_host`, un nom qui n'est pas le sien). Un serveur malveillant ne peut donc pas relayer la connexion d'une personne vers celui qu'il prétend être. La session expire avec le jeton d'identité (12 h par défaut) ; le client se reconnecte alors. Ce processus est décrit dans [Sécurité et chiffrement](/wiki/decouvrir/securite/) ; un bot n'en a pas besoin. Une connexion sans `host` ni `tls` (application antérieure à 0.3.0) est refusée : `426 client_outdated`.

## 3. Temps réel (`GET /v1/gateway`, WebSocket)

1. Ouvrir une connexion WebSocket (`wss://…/v1/gateway`).
2. Envoyer en premier message, dans les 10 s : `{"op": "auth", "token": "<jeton>"}`.
3. Recevoir `{"t": "READY", "d": {…}}` : l'état initial.
4. Recevoir ensuite des événements `{"t": "<TYPE>", "d": {…}}`.

La connexion est en lecture seule : **toutes les actions passent par l'API REST**. Le serveur envoie un ping WebSocket toutes les 30 s. Un événement peut répéter un état déjà connu : appliquez-les de façon idempotente.

**READY** : `{member, server, roles, members, channels, voice_states, permissions: {server: [...], channels: {id: [...]}}, restriction, read_states, notification_settings}`. Seuls les salons visibles par vous sont inclus. `restriction` vaut `""`, ou la raison pour laquelle vous êtes en lecture seule (`timed_out`, `rules_not_accepted`, `phone_not_verified`).

| Événement | Contenu | Quand |
|---|---|---|
| `MESSAGE_CREATE`, `MESSAGE_UPDATE` | message | Message écrit, modifié, épinglé, aperçu de lien ajouté, fil créé |
| `MESSAGE_DELETE` | `{id, channel_id}` | Message supprimé |
| `MESSAGE_DELETE_BULK` | `{channel_id, ids}` | Suppression en masse (modération) |
| `REACTION_ADD`, `REACTION_REMOVE` | `{channel_id, message_id, emoji, member_id}` | |
| `TYPING_START` | `{channel_id, member_id, at}` | À afficher ~8 s |
| `CHANNEL_CREATE`, `CHANNEL_UPDATE` | salon | |
| `CHANNEL_DELETE` | `{id}` | |
| `CHANNELS_SYNC` | `{channels, permissions, voice_states, restriction}` | Vos droits ont changé : remplace votre liste de salons |
| `MEMBER_JOIN`, `MEMBER_UPDATE` | membre | Arrivée, surnom, rôles, exclusion temporaire… |
| `MEMBER_LEAVE` | `{id, reason: left\|kicked\|banned}` | |
| `ROLES_UPDATE` | liste complète des rôles | |
| `ROLE_DELETE` | `{id}` | |
| `SERVER_UPDATE` | infos du serveur | |
| `VOICE_STATE_UPDATE` | état vocal (`channel_id: null` = départ) | |
| `VOICE_MOVE` | `{channel_id, from_channel_id}` | La modération vous déplace : rejoignez ce salon |
| `READ_STATE_UPDATE` | état de lecture | Vos autres connexions ont lu un salon |
| `NOTIFICATION_SETTINGS_UPDATE` | liste des réglages | |

Les événements d'un salon ne sont envoyés qu'à ceux qui le voient. **Fermetures** : `4001` jeton invalide ou expiré (ne pas se reconnecter avec le même jeton) ; `1008` membre parti, client trop lent (plus de 256 événements en attente) ou arrêt du serveur. En cas de coupure, reconnectez-vous avec un délai croissant : le nouveau READY redonne l'état complet.

## 4. Objets

**Membre** : `{id, handle, issuer, subject, nickname?, display_name, owner, roles: [ids], joined_at, bot, timeout_until, rules_accepted, phone_verified}`. L'identité stable d'une personne est `(issuer, subject)` ; `handle` (`pseudo@service`) peut changer. Profil public et avatar : `GET https://<issuer>/v1/users/<subject>/profile` sur son service Identity (sans session) → `{handle, pseudo, bio, avatar_url}` (`avatar_url` relative à ce service). Les bots n'ont pas de profil Identity. `timeout_until` : lecture seule jusqu'à cette date si elle est future.

**Salon** : `{id, type, name, topic, parent_id, position, thread_starter?, overrides}`. Types : `text`, `voice`, `category`, `announcement` (écrire exige aussi `manage_messages`), `thread` (fil : `parent_id` = salon textuel, mêmes droits que lui). La liste est plate, triée par `(position, id)` ; l'arbre se reconstruit avec `parent_id`.

**Message** : `{id, channel_id, author_id, content, mentions: [member ids], mention_roles, mention_everyone, reply_to, referenced?: {id, author_id, content}, attachments, embeds, reactions: [{emoji, count, me}], pinned_at, thread_id, created_at, edited_at}`. Contenu : 0 à 4000 caractères (vide seulement avec un fichier). Mentions dans le texte : `<@member_id>`, `<@&role_id>`, `@everyone`.

**Fichier** : `{id, filename, content_type, size, url}` ; `url` est relative au serveur et exige le jeton.

**Rôle** : `{id, name, color, position, permissions, mentionable, hoist}`. `@everyone` a l'id 1 et la position 0.

## 5. Routes REST

Légende : 🔑 = permission requise.

### Serveur
| | | |
|---|---|---|
| `GET /v1/server` | public | `{id, name, access, member_count, rules, require_phone, phone_verification}` |
| `PATCH /v1/server` | 🔑 `manage_server` | `{name?, access?: private\|public, rules?, require_phone?}` |

### Membres
| | | |
|---|---|---|
| `GET /v1/members` | | Membres présents |
| `GET/PATCH/DELETE /v1/members/@me` | | Soi / `{nickname}` / quitter |
| `GET /v1/members/@me/permissions` | | `{server, channels}` |
| `POST /v1/members/@me/accept-rules` | | Accepter les règles |
| `POST /v1/members/@me/phone` | | `{phone}` (format international) → code par SMS |
| `POST /v1/members/@me/phone/verify` | | `{phone, code}` |
| `PUT/DELETE /v1/members/{id}/roles/{role}` | 🔑 `manage_roles` | Rôle strictement sous le vôtre |

### Rôles
`GET /v1/roles` (du plus haut au plus bas) ; `POST /v1/roles {name, color?, permissions?, mentionable?, hoist?}` ; `PATCH /v1/roles/{id} {…, position?}` ; `DELETE /v1/roles/{id}` — 🔑 `manage_roles`, sur des rôles sous le vôtre, en n'accordant que des permissions que vous avez.

### Salons
| | | |
|---|---|---|
| `GET /v1/channels` | | Salons visibles |
| `POST /v1/channels` | 🔑 `manage_channels` | `{type, name, topic?, parent_id?, position?}` |
| `PATCH/DELETE /v1/channels/{id}` | 🔑 `manage_channels` | |
| `PUT/DELETE /v1/channels/{id}/overrides/{role\|member}/{id}` | 🔑 `manage_roles` | `{allow: [...], deny: [...]}` |

### Messages
| | | |
|---|---|---|
| `GET /v1/channels/{id}/messages` | 🔑 `view_channel` | `?limit=` (≤ 100) `&before=` ou `&after=` ; ordre chronologique |
| `POST /v1/channels/{id}/messages` | 🔑 `send_messages` | `{content, reply_to?, mention_reply?, attachments?: [ids]}` |
| `PATCH /v1/channels/{id}/messages/{mid}` | auteur | `{content}` |
| `DELETE /v1/channels/{id}/messages/{mid}` | auteur ou 🔑 `manage_messages` | |
| `POST /v1/channels/{id}/messages/bulk-delete` | 🔑 `manage_messages` | `{ids (≤ 100), reason?}` → `{deleted}` |
| `POST /v1/channels/{id}/attachments` | 🔑 `attach_files` | multipart, champ `file` → fichier, à citer ensuite dans `attachments` |
| `GET /v1/attachments/{id}/{nom}` | 🔑 `view_channel` | Téléchargement |
| `PUT/DELETE /v1/channels/{id}/messages/{mid}/reactions/{emoji}` | 🔑 `add_reactions` | Emoji encodé dans l'URL |
| `DELETE …/reactions/{emoji}/{member}` | 🔑 `manage_messages` | |
| `GET /v1/channels/{id}/pins` ; `PUT/DELETE /v1/channels/{id}/pins/{mid}` | 🔑 `manage_messages` pour modifier | |
| `POST /v1/channels/{id}/messages/{mid}/threads` | 🔑 `send_messages` | `{name?}` → salon `thread` |
| `POST /v1/channels/{id}/typing` | 🔑 `send_messages` | |
| `POST /v1/channels/{id}/ack` | | `{message_id?}` : marquer lu |
| `GET /v1/read-states` | | Non-lus et mentions par salon |
| `GET /v1/notification-settings` ; `PUT /v1/notification-settings/{salon\|0}` | | `{level: default\|all\|mentions\|none, mute_for?: secondes, -1 = toujours}` |
| `GET /v1/search` | | `?q=&channel_id=&author_id=&before=&limit=` (≤ 50) |

### Modération
| | | |
|---|---|---|
| `POST /v1/members/{id}/kick` | 🔑 `kick_members` | `{reason?}` |
| `GET /v1/bans` ; `PUT /v1/bans/{id}` ; `DELETE /v1/bans/{id}` | 🔑 `ban_members` | PUT : `{reason?, delete_messages?: secondes, -1 = tout}` |
| `PUT /v1/members/{id}/timeout` ; `DELETE …` | 🔑 `moderate_members` | `{duration: secondes (≤ 28 j), reason?}` |
| `POST /v1/members/{id}/purge` | 🔑 `manage_messages` | `{window: secondes ou -1, channel_id?, reason?}` → `{deleted}` |
| `POST /v1/members/{id}/transfer-ownership` | propriétaire | Transmet le serveur à ce membre (une personne, pas un bot) ; l'ancien propriétaire reste membre |
| `GET /v1/server/automod` ; `PUT …` | 🔑 `manage_server` | `{words: [...], block_links, max_mentions, duplicates, timeout: secondes}` : modération automatique. Message refusé : `403 automod_word`, `automod_link`, `automod_mentions`, `automod_duplicate` (sauf pour qui a `manage_messages` dans le salon) |
| `GET /v1/audit-log` | 🔑 `view_audit_log` | `?limit=&before=&action=&actor_id=&target_id=` → `[{id, actor_id, action, target_id, reason, details, created_at, actor_name?, target_name?}]` (noms des membres concernés, même partis) ; conservé 90 jours |

On n'agit que sur un membre dont le rôle le plus haut est **strictement sous** le vôtre ; le propriétaire est intouchable, les administrateurs ne peuvent pas être exclus temporairement.

### Invitations, bots, vocal
- `GET /v1/invites`, `POST /v1/invites {max_uses?, expires_in?}` (🔑 `create_invite`), `DELETE /v1/invites/{code}`.
- **Scènes** : un salon vocal avec `stage: true` (création ou `PATCH /v1/channels/{id} {stage}`) ; seuls les orateurs et les membres qui ont `mute_members` y parlent et partagent. `PATCH /v1/voice/state {hand_raised}` lève la main, `{speaker: false}` quitte la scène ; `PATCH /v1/voice/states/{member} {speaker}` (🔑 `mute_members`) invite à parler ou renvoie dans le public. L'état vocal porte `speaker`, `hand_raised`, `can_speak`.
- **Forums** (type de salon `forum`) : pas de messages propres ; `POST /v1/channels/{id}/posts {title, content}` crée un post (un fil `thread` du forum, avec son premier message) ; `GET /v1/channels/{id}/posts?limit=&before=` → `[{channel, author_id, excerpt, message_count, last_message_at}]`, dernière activité d'abord. On répond dans un post comme dans n'importe quel fil.
- **Emojis personnalisés** : `GET /v1/emojis` → `[{id, name}]` ; `POST /v1/emojis?name=nom` (corps = image PNG, GIF ou WebP, 256 Ko) et `DELETE /v1/emojis/{id}` — 🔑 `manage_server` ; image : `GET /v1/emojis/{id}` (session). Dans les messages et les réactions : `<:nom:id>`. Événement `EMOJIS_UPDATE` (liste complète), `emojis` dans `READY`.
- **Commandes slash** : `PUT /v1/bots/@me/commands [{name, description, options: [{name, description, type, required}]}]` (bots, remplace la liste), `GET /v1/commands` → `[{bot_id, name, description, options}]` (événement `COMMANDS_UPDATE` avec la liste complète), `POST /v1/channels/{id}/commands {bot_id, name, options: {nom: valeur}}` → `202 {id}` (droit d'écrire dans le salon ; erreurs `unknown_command`, `missing_option`, `invalid_option`, `unknown_option`, `bot_offline`) ; le bot reçoit `INTERACTION_CREATE {id, name, options, channel_id, member_id}` et répond sous 15 min par `POST /v1/interactions/{id}/reply {content, ephemeral?}` → message (`interaction: {name, member_id}`) ou, éphémère, `INTERACTION_REPLY {interaction_id, channel_id, bot_id, name, content}` au seul membre (`204`).
- **Webhooks entrants** : `GET /v1/channels/{id}/webhooks`, `POST /v1/channels/{id}/webhooks {name}` → `{id, channel_id, name, created_by, created_at, token}` (jeton `qw_…` affiché une seule fois), `DELETE /v1/webhooks/{id}` — 🔑 `manage_webhooks` dans le salon (textuel ou d'annonces) ; créer un webhook exige aussi d'y avoir le droit d'écrire (`send_messages`, et `manage_messages` dans un salon d'annonces). Publier : `POST /v1/webhooks/{id}/{token} {"content": "…"}` **sans session** → le message (`201`) ; 10 messages par 10 s ; pas de notification `@everyone` ni des rôles non mentionnables. Les messages d'un webhook portent `webhook: {id, name}` ; leur `author_id` est un membre caché, jamais listé.
- `GET /v1/bots` → `[{member}]`, `POST /v1/bots {name}` → `{member, token}` (jeton affiché une seule fois), `POST /v1/bots/{id}/token` → `{member, token}`, `DELETE /v1/bots/{id}` — 🔑 `manage_server`.
- **Vocal et vidéo** : `POST /v1/channels/{id}/voice/join` (🔑 `connect`) → `{url, token, room, can_speak, can_stream, server_mute, server_deaf}` pour se connecter au serveur média LiveKit (SDK `livekit-client` ou équivalent) ; micro = 🔑 `speak`, caméra et partage d'écran = 🔑 `stream`. `GET /v1/voice/states` → `[{member_id, channel_id, self_mute, self_deaf, server_mute, server_deaf, can_speak, can_stream, video, screen, joined_at}]`, `PATCH /v1/voice/state {self_mute?, self_deaf?}`, `POST /v1/voice/leave`.
- **Modération vocale** : `PATCH /v1/voice/states/{member} {mute?, deaf?, channel_id?, reason?}` (🔑 `mute_members`, `deafen_members`, `move_members`) ; micro et son coupés par la modération persistent jusqu'à levée. Un client déplacé par la modération reçoit `VOICE_MOVE {channel_id, from_channel_id}` et doit rejoindre ce salon (il a déjà été retiré de l'ancien). `DELETE /v1/voice/states/{member}` (🔑 `move_members`) le déconnecte.

## 6. Permissions

`view_channel`, `send_messages`, `manage_messages`, `mention_everyone`, `create_invite`, `manage_channels`, `manage_roles`, `kick_members`, `ban_members`, `manage_server`, `connect`, `speak`, `stream`, `add_reactions`, `attach_files`, `moderate_members`, `view_audit_log`, `mute_members`, `deafen_members`, `move_members`, `manage_webhooks`, `administrator`.

Calcul dans un salon : permissions de `@everyone` + union de vos rôles → surcharges de la catégorie → surcharges du salon (à chaque niveau : `@everyone`, puis vos rôles, puis vous). Sans `view_channel`, le salon n'existe pas pour vous (`404`). `administrator` et le propriétaire ont tout. Un membre **restreint** (exclusion temporaire, règles non acceptées, téléphone non vérifié) garde seulement `view_channel` ; ses refus portent le code de la restriction plutôt que `missing_permissions`.

## 7. Bonnes pratiques pour les bots

- Ne répondez jamais à vos propres messages ni, sauf besoin, à ceux des autres bots (`author_id` = votre id, ou membre `bot: true`).
- Respectez `Retry-After` sur les `429`.
- Reconnectez-vous avec un délai croissant (1 s, 2 s, 4 s… jusqu'à 1 min).
- Donnez à votre bot un rôle avec **le strict nécessaire** : son jeton porte toutes ses permissions.
