# Quarel — Fiche technique

Alternative à Discord **auto-hébergeable** : chaque serveur tourne chez son propriétaire (Docker, puis `.exe`), les utilisateurs s'y connectent via un client.

- Processus, recherches et décisions : `PROJECT.md`
- Fonctionnalités à implémenter : `PLAN.md`

## Règles de travail

- Langue de la documentation : français. Code, identifiants et commits : anglais.
- L'utilisateur est chef de projet et testeur ; Claude code. Toute décision d'architecture est validée avec lui puis consignée dans `PROJECT.md` (journal des décisions).
- Backend / fonctionnalités d'abord, front-end en dernier.
- Tenir ce fichier à jour à chaque changement de stack, d'architecture ou de commande.
- Licence **Apache-2.0** (`LICENSE`). Toute dépendance ajoutée doit être compatible (MIT, BSD, Apache-2.0, MPL-2.0… ; pas de GPL/AGPL) ; composants embarqués ou vendus listés dans `NOTICE`.

## Contraintes techniques (connues à ce stade)

- Serveur installable en **une commande**, image Docker unique, sans service externe obligatoire.
- Doit tourner sur du matériel domestique modeste (faible RAM).
- Distribution future en exécutable Windows (`.exe`) → privilégier un langage compilé en binaire unique.

## Composants (décidés)

- **Identity** (service central, hébergé par l'équipe) : comptes (email, pseudo, mot de passe, 2FA), clés d'appareils, amis, MP chiffrés E2E, révocations.
- **Serveur communautaire** (Docker chez l'hébergeur) : totalement isolé des autres serveurs, pas de fédération. Authentifie les membres via l'identité portable émise par Identity. Joignable via UPnP, ou ouverture manuelle des ports en secours.
- Service central **open source et auto-hébergeable** ; adresse configurable dans le client (notre instance par défaut). Les identifiants incluent le service d'origine (`pseudo@domaine`) ; chaque serveur communautaire déclare les services d'identité qu'il accepte.
- Salons des serveurs communautaires **non chiffrés E2E** (en clair côté serveur).
- Appels entre amis : **P2P WebRTC**, avec relais TURN de secours désactivable.
- Bans gérés **par serveur communautaire** ; le service central peut seulement désactiver un compte.
- API propre, aucune compatibilité Discord.
- Chiffrement E2E : utiliser une bibliothèque éprouvée (MLS/OpenMLS ou Olm/vodozemac), jamais de crypto maison.

## Stack

- **Langage serveurs :** Go (1.27+), monorepo unique pour Identity et le serveur communautaire.
- **Base de données :** SQLite embarquée via `modernc.org/sqlite` (pur Go, sans cgo → compilation croisée `.exe` triviale).
- **Voix / vidéo :** LiveKit (SFU) intégré à l'image du serveur communautaire.
- **UPnP :** `github.com/huin/goupnp`.
- **Client :** décidé en phase finale (piste : Tauri + cœur Rust pour la crypto E2E).

## Arborescence

```
cmd/quarel-identity/    binaire du service Identity
cmd/quarel-server/      binaire du serveur communautaire
cmd/quarelctl/          client de test en ligne de commande (sorties en français, neutres en genre, pour le CP) :
                        main.go (Identity), community.go (serveurs communautaires), roles.go (rôles, modération),
                        voice.go (vocal), social.go (amis, MP), e2e.go (chiffrement Olm/Megolm côté client)
internal/identity/      service Identity : HTTP (server.go), endpoints (handlers.go), SQLite (store.go),
                        argon2id (crypto.go), TOTP (totp.go), emails (mail.go), anti-bruteforce (lockout.go),
                        amis et conversations (social.go), clés E2E et boîtes aux lettres (e2e.go), temps réel (gateway.go)
internal/community/     serveur communautaire : config, clés des Identity (keys.go), auth (auth.go),
                        membres, invitations, salons + droits par salon (channels.go), messages,
                        permissions (permissions.go), rôles (roles.go), expulsion/bannissement
                        (moderation.go), temps réel (gateway.go)
                        vocal (voice.go), page de test vocal (voicetest/, embarquée)
internal/voice/         client LiveKit maison (jetons, API salle, webhooks) + lancement de livekit-server
internal/realtime/      passerelle WebSocket partagée (authentification 1er message, READY, diffusion filtrée)
internal/httpapi/       conventions JSON partagées (erreurs, décodage strict)
internal/sqlitedb/      ouverture SQLite + migrations (PRAGMA user_version)
internal/secret/        identifiants aléatoires, jetons porteurs, fichiers de clés Ed25519
pkg/idtoken/            jetons d'identité portables (émission, vérification, preuve d'appareil)
pkg/e2ekeys/            messages signés des clés E2E (partagés serveur/clients), code de vérification
docs/tests/             guides de test par jalon, destinés au CP
test/e2e/               tests de bout en bout : vocal avec navigateurs (Playwright), MP chiffrés (quarelctl)
Dockerfile.identity     image distroless (~23 Mo), volume /data, port 8080
Dockerfile.server       image distroless + livekit-server (~139 Mo), volume /data, ports 8090/tcp, 7881/tcp, 7882/udp
Makefile                commandes de dev (build, test, run-identity, run-server…)
```

## Identité portable (implémentée, jalon 1)

- **Identifiant stable** : `(iss, sub)` = domaine du service Identity + ID aléatoire 128 bits (base32). **Les bans et appartenances se basent dessus**, jamais sur le handle `pseudo@domaine` (le pseudo pourra changer).
- **Jeton** : JWT EdDSA (Ed25519) avec `iss`, `sub`, `handle`, `dkey` (clé publique de l'appareil), `iat`, `exp`, `jti`, en-tête `kid`. Durée : `QUAREL_TOKEN_TTL` (12 h par défaut). **Pas d'audience** : l'Identity ne sait pas à quel serveur le jeton est destiné. Aucun email dans le jeton.
- **Clés publiques** du service : `GET /.well-known/quarel-identity` → `{issuer, keys:[{kid, alg, crv, x}]}`. Clé privée : `$QUAREL_DATA_DIR/signing.key` (générée au 1er démarrage, à sauvegarder).
- **Anti-rejeu** : le client génère une paire Ed25519 par appareil, envoie la clé publique au login. Un serveur communautaire envoie un nonce ; le client renvoie `idtoken.SignProof(device, audience, nonce)` ; le serveur appelle `idtoken.Verify` puis `idtoken.VerifyProof`. Le serveur doit garantir l'usage unique de ses nonces.

## Amis et messages privés chiffrés (jalon 5)

- **Choix validés par le CP** : protocole **Olm/Megolm** (celui de Matrix) ; **historique sur les appareils** (le serveur efface chaque message dès que tous les appareils du destinataire l'ont acquitté).
- **Le serveur ne déchiffre rien** : il stocke des clés publiques, vérifie leurs signatures (les clients les revérifient) et relaie des blobs opaques. Seul `quarelctl` importe la crypto (`maunium.net/go/mautrix/crypto/goolm`, Go pur, MPL-2.0, appel explicite à `goolm.Register()`) ; le futur client utilisera vodozemac (Rust), même protocole.
- **Amis** (`friendships`, une ligne par paire, `user_a < user_b`) : demande par pseudo, demande croisée = acceptation, refus/annulation/retrait = `DELETE`. Événement `FRIENDS_UPDATE`. MP, annuaire de clés, clés à usage unique et messages entre appareils : **amis ou soi-même uniquement** (`requireFriendOrSelf`).
- **Appareil = session Identity.** Chaque appareil publie ses clés Olm (curve25519, ed25519) signées par lui-même (`e2ekeys.DeviceKeys`). **Clé maîtresse** Ed25519 du compte : créée par le 1er appareil, qui se certifie (`e2ekeys.DeviceCert`) → validé d'office. Un nouvel appareil reste non validé jusqu'à ce qu'un appareil détenant la clé maîtresse le certifie (`POST /v1/keys/certify`) après comparaison du **code de vérification** (`e2ekeys.VerificationCode` : 80 bits du hachage de sa clé ed25519, `XXXX-XXXX-XXXX-XXXX`), puis lui envoie la graine maîtresse et l'historique par Olm.
- **Confiance côté client** : un appareil est de confiance si sa signature propre et son certificat vérifient avec la clé maîtresse **épinglée au premier contact** (changement → envoi refusé, avertissement). Les clés de conversation ne sont partagées qu'avec des appareils de confiance ; les secrets reçus ne sont acceptés que d'appareils de confiance.
- **Olm** (par paire d'appareils) transporte : `room_key` (clé Megolm d'une conversation), `device_approval` (graine maîtresse), `history` (historique + sessions Megolm exportées). Le clair lie expéditeur et destinataire (`olmPlain`), vérifié à la réception. Clés à usage unique signées (`e2ekeys.OneTimeKey`), 20 maintenues sur le serveur ; clé de secours acceptée par l'API (pas encore générée par le client).
- **Megolm** (par conversation et appareil émetteur) chiffre les MP ; renouvelé après 100 messages, 7 jours, ou si un appareil destinataire disparaît. Le clair contient `dm_id`, expéditeur, date, vérifiés contre l'enveloppe ; protection contre le rejeu par (session, index).
- **Boîte aux lettres** (`inbox`, une ligne par appareil destinataire) : `to_device`, `dm`, `receipt`. `GET /v1/inbox` puis `POST /v1/inbox/ack` (suppression). Accusé de distribution (`receipt`) quand tous les appareils du destinataire ont acquitté. Poussée en direct : événement `INBOX` sur la passerelle Identity (une connexion par appareil). Révocation d'une session → clés, clés à usage unique et boîte supprimées, `DEVICES_UPDATE` aux amis.
- **Client de test** : état E2E dans `<profil>.e2e.json` (0600, non chiffré — le vrai client utilisera le trousseau du système), protégé par un **verrou de fichier** (`withE2E`) car plusieurs `quarelctl` peuvent tourner sur le même profil (`dm-listen` + `dm`).

### Endpoints (Identity)

| Méthode | Chemin | Rôle |
|---|---|---|
| GET/POST | `/v1/friends` | Listes `{friends, incoming, outgoing}` / demande `{pseudo}` |
| POST | `/v1/friends/{id}/accept` | Accepter |
| DELETE | `/v1/friends/{id}` | Retirer, refuser, annuler |
| POST/GET | `/v1/keys/device` | Publier `{curve25519, ed25519, signature, master_key?, master_signature?}` / mes clés |
| POST | `/v1/keys/certify` | `{device_id, master_signature}` |
| POST | `/v1/keys/one-time` | `{keys: [{id, key, signature}], fallback?}` → `{one_time_keys}` |
| POST | `/v1/keys/claim` | `{device_ids}` → une clé par appareil |
| GET | `/v1/users/{id}/keys` | `{user, master_key, devices}` (amis/soi) |
| GET/POST | `/v1/dms` | Conversations / ouvrir `{user_id}` (amis) |
| POST | `/v1/dms/{id}/messages` | `{payload}` chiffré → `{event_id, recipient_devices}` |
| POST | `/v1/to-device` | `{messages: [{device_id, payload}]}` (8 Mo max) |
| GET | `/v1/inbox` | Éléments en attente pour cet appareil |
| POST | `/v1/inbox/ack` | `{ids}` |
| GET | `/v1/gateway` | WebSocket : READY, INBOX, FRIENDS_UPDATE, DEVICES_UPDATE |

## Service Identity — détails

- **Mots de passe** : argon2id (t=3, m=64 Mo, p=2), format PHC ; 4 hachages simultanés max ; hachage factice si le compte n'existe pas (pas d'énumération par le temps de réponse).
- **Sessions** : jeton porteur aléatoire 256 bits, seul le SHA-256 est stocké ; une session par appareil (`device_name`, `device_key`).
- **Email** : code à 6 chiffres, 15 min, 5 essais, renvoi limité à 1/min ; `resend-verification` répond toujours 202. La connexion exige un email vérifié.
- **2FA** : TOTP RFC 6238 (SHA1, 30 s, 6 chiffres, ±1 pas), chaque code utilisable une seule fois (`totp_last_step`) ; 10 codes de secours de 80 bits (SHA-256 stocké), usage unique.
- **Anti-bruteforce** (`lockout.go`) : 15 échecs (mauvais mot de passe ou mauvais code 2FA) en 1 h glissante → `429 account_locked` + `Retry-After`, même avec le bon mot de passe. Compté par compte (email et pseudo partagent le compteur), ou par identifiant tapé si le compte n'existe pas (pas d'énumération). Les re-vérifications de mot de passe/2FA (`2fa/setup`, `2fa/disable`) passent par le même compteur (`s.guarded`). Remise à zéro après une connexion réussie complète. Table `auth_failures`.
- **Compte désactivé** (`users.disabled_at`, réquisition judiciaire) : login, sessions et émission de jetons refusés. Pas encore d'outil admin (P1).
- **Erreurs API** : `{"error":{"code":"...","message":"..."}}` ; les codes (`invalid_credentials`, `mfa_required`, `email_not_verified`…) sont stables, les messages sont indicatifs.
- **Migrations SQLite** : liste `migrations` dans `store.go`, version dans `PRAGMA user_version`. Ne jamais modifier une migration existante, seulement en ajouter.

### Endpoints

| Méthode | Chemin | Auth | Rôle |
|---|---|---|---|
| GET | `/.well-known/quarel-identity` | — | Clés publiques de signature |
| GET | `/v1/health` | — | Santé |
| POST | `/v1/auth/register` | — | `{email, pseudo, password}` |
| POST | `/v1/auth/verify-email` | — | `{email, code}` |
| POST | `/v1/auth/resend-verification` | — | `{email}` |
| POST | `/v1/auth/login` | — | `{login, password, totp_code?, device_name, device_key}` |
| POST | `/v1/auth/logout` | session | Ferme la session courante |
| GET | `/v1/me` | session | Compte courant |
| GET | `/v1/me/sessions` | session | Sessions ouvertes |
| DELETE | `/v1/me/sessions/{id}` | session | Ferme une session |
| POST | `/v1/me/2fa/setup` | session | `{password}` → secret + URI otpauth |
| POST | `/v1/me/2fa/enable` | session | `{code}` → codes de secours |
| POST | `/v1/me/2fa/disable` | session | `{password, code}` |
| POST | `/v1/identity/token` | session | Jeton d'identité portable |

### Configuration (variables d'environnement)

`QUAREL_ADDR` (`:8080`), `QUAREL_DATA_DIR` (`./data`), `QUAREL_ISSUER` (`localhost:8080` — **doit être le domaine public en production**), `QUAREL_TOKEN_TTL` (`12h`), `QUAREL_SMTP_HOST/PORT/USER/PASSWORD/FROM` (sans `QUAREL_SMTP_HOST`, les emails sont écrits dans le log : mode dev).

## Serveur communautaire (jalon 2)

- **Identité du serveur** : clé Ed25519 `$QUAREL_DATA_DIR/server.key` (1er démarrage). `server_id` = base32(SHA-256(clé publique)[:16]). Les liens d'invitation portent le `sid` ; le client (quarelctl) épingle le `server_id` au 1er contact et refuse s'il change. Prévu (P1) : certificat TLS lié à cette clé, pour empêcher un intermédiaire.
- **Connexion** : `POST /v1/auth/challenge` → `{server_id, nonce}` (usage unique, 2 min, en mémoire) ; `POST /v1/auth/login {identity_token, nonce, proof, invite?, claim?}`. Le serveur vérifie le jeton hors ligne avec les clés de l'émetteur (`QUAREL_TRUSTED_ISSUERS`), puis la preuve `SignProof(device, server_id, nonce)`. Session = jeton porteur (SHA-256 stocké) qui **expire avec le jeton d'identité** ; le client se reconnecte alors avec un nouveau jeton.
- **Clés des services Identity** (`keys.go`) : récupérées sur `https://<issuer>/.well-known/quarel-identity` (`http://` pour localhost/127.x), cache 1 h, re-téléchargement forcé si `kid` inconnu (1/min max), anciennes clés conservées si l'Identity est injoignable.
- **Membres** : identifiés par `(issuer, subject)`. Départ, expulsion ou bannissement = `left_at` + suppression des sessions et des rôles (`removeMember`) ; la ligne reste : les messages gardent leur auteur, les bans leur cible, même `id` au retour. Le propriétaire (`is_owner`) ne peut pas partir.
- **Propriétaire** : tant qu'il n'y en a pas, un code de revendication est généré (et affiché) à chaque démarrage ; `login` avec `claim` le consomme.
- **Accès** : `private` (défaut, invitation requise pour rejoindre) ou `public`. Invitations : code 10 caractères, `max_uses` (0 = illimité), `expires_in` en secondes (absent = 7 jours, 0 = jamais) ; `create_invite` pour inviter ; chacun voit/révoque les siennes, `manage_server` voit/révoque tout.

### Permissions et rôles (jalon 3)

- **Permissions** (`permissions.go`) : bits stockés en base (**ne jamais renuméroter**), échangés par l'API sous forme de **noms** : `view_channel`, `send_messages`, `manage_messages`, `mention_everyone`, `create_invite`, `manage_channels`, `manage_roles`, `kick_members`, `ban_members`, `manage_server`, `connect`, `speak`, `administrator`. Réglables par salon : `view_channel`, `send_messages`, `manage_messages`, `mention_everyone`, `manage_channels`, `connect`, `speak`.
- **Rôles** (`roles.go`) : le rôle `@everyone` (id 1, position 0, défaut : voir, écrire, inviter, connect, speak) s'applique à tous ; les autres ont les positions 1..n (plus haut = plus puissant), nouveaux rôles en bas. Permissions serveur = union des rôles ; `administrator` ou propriétaire = tout.
- **Hiérarchie** : on ne gère/attribue que les rôles **strictement sous** son rôle le plus haut (`checkRoleRank`), on n'accorde ou ne retire que des permissions qu'on possède (`checkGrant`), on n'expulse/bannit/surcharge un membre que s'il est strictement en dessous (`outranks`) ; le propriétaire est au-dessus de tout et intouchable.
- **Droits par salon** (`channel_overrides`) : par rôle ou par membre, `allow`/`deny`. Calcul (`inChannel`) : permissions serveur → surcharges de la catégorie parente → surcharges du salon ; à chaque niveau `@everyone`, puis l'union des rôles du membre, puis le membre. Sans `view_channel` : aucune permission, salon invisible (404 `not_found`, pas de fuite d'existence). Modifier une surcharge exige `manage_roles` et de posséder dans ce salon les permissions changées.
- **Instantané** : chaque vérification charge tout l'état des permissions (`loadPerms` : propriétaires, rôles, rôles des membres, salons + surcharges). Simple et cohérent pour des serveurs domestiques ; à mettre en cache si un serveur devient gros.
- **Modération** (`moderation.go`) : `kick_members` (retour possible avec invitation, rôles perdus) ; `ban_members` → table `bans` liée au membre, donc à l'identité `(issuer, subject)` : refus `banned` au login quelle que soit l'invitation ou le mode d'accès ; on peut bannir un membre déjà parti.
- **Messages** : écrire = `send_messages` ; supprimer ceux des autres = `manage_messages` ; `@everyone` et rôles non mentionnables ne notifient qu'avec `mention_everyone` (le texte reste). Rôle mentionnable : `<@&role_id>` notifie pour tous.
- **Salons** : `text`, `voice` (média au jalon 4), `category` (non imbriquables). Liste plate triée `(position, id)`, le client construit l'arbre via `parent_id`. Supprimer une catégorie remonte ses salons à la racine. Création initiale : « Salons textuels / général », « Salons vocaux / Général ».
- **Messages** : 1–4000 caractères ; ids entiers croissants. Historique : `?limit=` (≤100, défaut 50), `?before=ID` ou `?after=ID`, toujours renvoyé en ordre chronologique. Mentions : `<@member_id>` et `@everyone` (mot isolé) ; `<@&role_id>` viendra avec les rôles. Mentions recalculées à l'édition.
- **Horodatages** en millisecondes Unix en base, RFC 3339 en JSON.
- **SQLite à une seule connexion** (les deux services) : dans une transaction, **toujours** requêter via `tx`, jamais `s.db`, et faire les vérifications (droits, amis) **avant** d'ouvrir la transaction — sinon interblocage (vécu au jalon 5). Les cibles `make test` ont un délai maximal pour qu'un blocage échoue au lieu de geler.

### Vocal (jalon 4)

- **Architecture** : le média ne passe jamais par notre serveur. LiveKit (SFU, `livekit-server` v1.13.7) tourne à côté, lancé et relancé par `quarel-server` (`internal/voice/embedded.go`) avec une config générée (`$DATA/livekit.yaml`) et des clés propres (`$DATA/livekit.keys`). Signalisation LiveKit en boucle locale (7880), exposée aux clients via le proxy **`/lk/`** du serveur ; média : **7882/udp** (+ **7881/tcp** de secours).
- **Pas de SDK LiveKit Go** (il embarque une pile WebRTC) : `internal/voice/livekit.go` signe les jetons (JWT HS256, claim `video`), appelle l'API Twirp JSON (`RemoveParticipant`, `UpdateParticipant`, `ListRooms`, `ListParticipants`) et vérifie les webhooks (JWT + SHA-256 du corps).
- **Salles** : salon vocal `id` = salle LiveKit `channel-<id>`, identité LiveKit = `member_id`, nom = nom affiché.
- **Rejoindre** : `POST /v1/channels/{id}/voice/join` (salon `voice`, `view_channel` + `connect`) → `{url, token, room, can_speak}` ; `canPublish` = `speak`. Jeton valable 1 h (connexion seulement).
- **État vocal** en mémoire (`voiceRegistry`), alimenté par les webhooks LiveKit (`participant_joined|left`, `room_finished`) sur `POST /internal/livekit/webhook` ; reconstruit depuis LiveKit au démarrage (`SyncVoice`). Un membre n'est que dans un salon à la fois (l'arrivée ailleurs le retire du précédent). À l'arrivée, les droits sont revérifiés (jeton périmé → éjection).
- **Respect des permissions** (`reconcileVoice`, appelé après tout changement de droits, expulsion/ban/départ, suppression de salon) : perte de `connect` → éjection ; changement de `speak` → `UpdateParticipant` (micro coupé côté serveur).
- **Micro/sourdine** : décidés par le client (`PATCH /v1/voice/state {self_mute, self_deaf}`, sourdine ⇒ micro coupé), informatifs pour les autres. Événement `VOICE_STATE_UPDATE` (`channel_id: null` = départ), filtré par visibilité du salon ; `voice_states` dans READY et CHANNELS_SYNC.
- **Page de test** `/voice-test/` (HTML/JS embarqués, `livekit-client` 2.22.3 vendu dans le dépôt, licence Apache-2.0) : jeton de session dans le fragment d'URL (`quarelctl voice-test`). Outil de test, pas le futur client.
- **Micro dans un navigateur** : uniquement sur `localhost` ou en HTTPS.

### Temps réel (`GET /v1/gateway`, WebSocket)

1. Client → `{"op":"auth","token":"<session>"}` (premier message, ≤ 10 s). Jamais de cookie : toutes origines acceptées.
2. Serveur → `{"t":"READY","d":{member, server, channels, members}}`.
3. Serveur → `{"t":"<EVENT>","d":…}` : `MESSAGE_CREATE|UPDATE|DELETE`, `CHANNEL_CREATE|UPDATE|DELETE`, `MEMBER_JOIN|UPDATE`, `MEMBER_LEAVE {id, reason: left|kicked|banned}`, `VOICE_STATE_UPDATE`, `ROLES_UPDATE` (liste complète), `ROLE_DELETE`, `CHANNELS_SYNC {channels, permissions}` (après tout changement de droits : remplace la liste des salons du client), `SERVER_UPDATE`. Les événements peuvent répéter un état déjà dans READY : les appliquer de façon idempotente.
- READY : `{member, server, roles, members, channels, voice_states, permissions: {server: [...], channels: {id: [...]}}}` — seulement les salons visibles.
- Les événements de messages et `CHANNEL_CREATE|UPDATE` ne sont envoyés qu'aux membres qui voient le salon (`broadcastChannel`).
- Fermetures : `4001` session invalide/expirée ; `1008` membre parti, client trop lent (file de 256 événements pleine) ou arrêt du serveur. Ping toutes les 30 s.
- Les écritures passent par l'API REST ; le gateway ne fait que diffuser.

### Endpoints

| Méthode | Chemin | Auth | Rôle |
|---|---|---|---|
| GET | `/v1/server` | — | Infos publiques `{id, name, access, member_count}` |
| PATCH | `/v1/server` | `manage_server` | `{name?, access?}` |
| POST | `/v1/auth/challenge` | — | Défi de connexion |
| POST | `/v1/auth/login` | — | `{identity_token, nonce, proof, invite?, claim?}` |
| POST | `/v1/auth/logout` | session | |
| GET | `/v1/members` | session | Membres actifs (avec `roles`) |
| GET/PATCH/DELETE | `/v1/members/@me` | session | Profil / `{nickname}` / quitter |
| GET | `/v1/members/@me/permissions` | session | `{server: [...], channels: {id: [...]}}` |
| PUT/DELETE | `/v1/members/{id}/roles/{role}` | `manage_roles` + hiérarchie | Donner / retirer un rôle |
| POST | `/v1/members/{id}/kick` | `kick_members` + hiérarchie | `{reason?}` |
| GET | `/v1/bans` | `ban_members` | |
| PUT/DELETE | `/v1/bans/{member_id}` | `ban_members` (+ hiérarchie pour PUT) | `{reason?}` / lever |
| GET | `/v1/roles` | session | Du plus haut au plus bas |
| POST | `/v1/roles` | `manage_roles` | `{name, color?, permissions?, mentionable?}` |
| PATCH/DELETE | `/v1/roles/{id}` | `manage_roles` + hiérarchie | `{name?, color?, permissions?, mentionable?, position?}` |
| GET/POST | `/v1/invites` | session / `create_invite` | `{max_uses?, expires_in?}` |
| DELETE | `/v1/invites/{code}` | créateur ou `manage_server` | |
| GET | `/v1/channels` | session | Salons visibles, avec `overrides` |
| POST | `/v1/channels` | `manage_channels` | `{type, name, topic, parent_id, position}` |
| PATCH/DELETE | `/v1/channels/{id}` | `manage_channels` dans le salon | |
| PUT/DELETE | `/v1/channels/{id}/overrides/{role\|member}/{id}` | `manage_roles` + hiérarchie | `{allow: [...], deny: [...]}` |
| GET | `/v1/channels/{id}/messages` | `view_channel` | Historique |
| POST | `/v1/channels/{id}/messages` | `send_messages` | `{content}` |
| PATCH | `/v1/channels/{id}/messages/{mid}` | auteur | `{content}` |
| DELETE | `/v1/channels/{id}/messages/{mid}` | auteur ou `manage_messages` | |
| POST | `/v1/channels/{id}/voice/join` | `connect` | Jeton LiveKit `{url, token, room, can_speak}` |
| GET | `/v1/voice/states` | session | Qui est dans quel salon vocal (salons visibles) |
| PATCH | `/v1/voice/state` | session (en vocal) | `{self_mute?, self_deaf?}` |
| POST | `/v1/voice/leave` | session | Quitter le vocal |
| POST | `/internal/livekit/webhook` | signature LiveKit | Événements de salle |
| * | `/lk/…` | — | Proxy vers la signalisation LiveKit embarquée |
| GET | `/voice-test/` | jeton dans le fragment | Page de test vocal |
| GET | `/v1/gateway` | premier message | WebSocket |

### Configuration

Vocal : `QUAREL_VOICE` (`embedded` par défaut, `external`, `off`) ; embarqué : `QUAREL_LIVEKIT_BIN` (`livekit-server`, `/livekit-server` dans l'image), `QUAREL_VOICE_SIGNAL_PORT` (7880, boucle locale), `QUAREL_VOICE_TCP_PORT` (7881), `QUAREL_VOICE_UDP_PORT` (7882), `QUAREL_VOICE_PUBLIC_IP` (vide = adresses locales, `auto` = découverte STUN, ou une IP) ; externe : `QUAREL_LIVEKIT_URL`, `QUAREL_LIVEKIT_API_URL`, `QUAREL_LIVEKIT_KEY`, `QUAREL_LIVEKIT_SECRET`. Sans binaire LiveKit, le serveur démarre avec le vocal désactivé.

`QUAREL_ADDR` (`:8090`), `QUAREL_DATA_DIR` (`./data`), `QUAREL_SERVER_NAME` (nom au 1er démarrage seulement), `QUAREL_TRUSTED_ISSUERS` (liste séparée par des virgules ; défaut `identity.quarel.app`, instance officielle — domaine `quarel.app` choisi par le CP ; **ce nom ne doit jamais changer**, il fait partie de chaque identité).

## Commandes

```sh
make build            # binaires dans bin/
make test             # tests (go test ./...)
make test-race        # tests avec détecteur de concurrence (gcc requis, installé)
make vet
make run-identity     # service Identity local sur :8080, données dans ./data/identity
make run-server       # serveur communautaire local sur :8090, données dans ./data/server
make docker-identity  # image quarel-identity
make docker-server    # image quarel-server
make e2e-voice        # test vocal de bout en bout avec navigateurs
make e2e-dm           # scénario MP chiffrés de bout en bout (16 vérifications)
./bin/quarelctl help  # client de test
```

Tests : les tests d'intégration (`internal/identity/identity_test.go`, `internal/community/community_test.go`) démarrent un vrai serveur HTTP sur une base SQLite temporaire, avec une horloge contrôlable (`srv.now`). Côté Identity, argon2 est allégé ; côté communautaire, un `idtoken.Signer` en mémoire remplace le service Identity (`staticKeys`) et `fakeVoice` remplace LiveKit.

Test vocal de bout en bout (hors `go test`) : `make e2e-voice` — vrai Identity + serveur + LiveKit, 2 Chromium sans interface avec micro simulé (Playwright, `test/e2e/`). Installe Playwright au premier lancement ; Chromium et ses dépendances système sont déjà présents sur la machine.

## Environnement de dev

- OS : Linux. Disponibles : Docker, Node.js, Python 3, make, gcc, livekit-server 1.13.7 (`~/.local/bin`, somme de contrôle vérifiée), Go 1.27.1 (installé dans `~/.local/go`, PATH ajouté dans `~/.zshrc`). Non installés : Rust, `gh`. `sudo` non interactif disponible (le CP autorise l'installation d'outils si besoin).
- Si `go` est introuvable dans le shell courant : `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"`.
- Git : branche `main`, remote `origin` = `git@github.com:anlekg/quarel.git` (SSH). Identité locale au dépôt : `anlekg` / adresse masquée GitHub `106981899+anlekg@users.noreply.github.com` (ne jamais utiliser l'email personnel).
