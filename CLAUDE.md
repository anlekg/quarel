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
- **Chaque service central est une bulle (décision du CP)** : amis, MP et appels seulement entre comptes du même service ; aucune communication entre services centraux pour l'instant. Seuls les serveurs communautaires acceptent des comptes de plusieurs services.
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
cmd/quarel-identity/    binaire du service Identity (+ sous-commandes « admin » de l'opérateur, admin.go)
cmd/quarel-server/      binaire du serveur communautaire
cmd/quarelctl/          client de test en ligne de commande (sorties en français, neutres en genre, pour le CP) :
                        main.go (Identity), account.go (compte, profil, blocage, présence), community.go (serveurs communautaires), messages.go (réponses, réactions,
                        fichiers, recherche, fils, non-lus), access.go (exclusion, purge, journal, règles, téléphone,
                        bots), roles.go (rôles, modération),
                        voice.go (vocal), social.go (amis, MP), dms.go (groupes, fichiers, lecture), calls.go (appels WebRTC), e2e.go (chiffrement Olm/Megolm côté client),
                        recovery.go (phrase de récupération, sauvegarde), network.go (diagnostic réseau)
internal/identity/      service Identity : HTTP (server.go), endpoints (handlers.go), SQLite (store.go),
                        argon2id (crypto.go), TOTP (totp.go), emails (mail.go), anti-bruteforce (lockout.go),
                        amis et conversations (social.go), clés E2E et boîtes aux lettres (e2e.go), temps réel (gateway.go),
                        gestion du compte (account.go), profil/blocage/présence (profile.go), opérateur et rotation de clé (admin.go),
                        conversations et groupes (conversations.go), fichiers chiffrés, frappe, lecture (convextras.go),
                        relais d'appels TURN (turn.go)
internal/community/     serveur communautaire : config, clés des Identity (keys.go), auth (auth.go),
                        membres, invitations, salons + droits par salon (channels.go), messages,
                        permissions (permissions.go), rôles (roles.go), expulsion/bannissement
                        (moderation.go), temps réel (gateway.go), réactions/épingles/fils/non-lus/recherche/
                        notifications (extras.go), pièces jointes (attachments.go), aperçus de liens (previews.go),
                        règles/téléphone/restrictions (access.go), journal d'audit (audit.go), bots (bots.go)
                        vocal (voice.go), page de test vocal (voicetest/, embarquée)
internal/voice/         client LiveKit maison (jetons, API salle, webhooks) + lancement de livekit-server
internal/realtime/      passerelle WebSocket partagée (authentification 1er message, READY, diffusion filtrée)
internal/ratelimit/     limiteurs en mémoire (seaux de jetons), IP client derrière proxys de confiance
internal/tlsconf/       modes HTTPS : off, self-signed, acme (Let's Encrypt), files
internal/netdiag/       UPnP (ouverture/renouvellement des ports), STUN (IP publique), diagnostic de joignabilité
internal/httpapi/       conventions JSON partagées (erreurs, décodage strict)
internal/sqlitedb/      ouverture SQLite + migrations (PRAGMA user_version), copie de la base avant migration
internal/backup/        sauvegarde/restauration d'un dossier de données (archive .tar.gz + manifeste), commandes backup/restore/version
internal/secret/        identifiants aléatoires, jetons porteurs, fichiers de clés Ed25519
pkg/idtoken/            jetons d'identité portables (émission, vérification, preuve d'appareil)
pkg/e2ekeys/            messages signés des clés E2E (partagés serveur/clients), code de vérification
pkg/tlsbind/            certificat auto-signé lié à l'identité du serveur, vérification côté client
pkg/recovery/           phrase de récupération (BIP-39 français) et chiffrement des sauvegardes
docs/tests/             guides de test par jalon, destinés au CP
test/e2e/               tests de bout en bout : vocal (Playwright), MP chiffrés, sécurité (récupération, HTTPS,
                        limites), messages P1 (messages.sh), modération P1 (moderation.sh), ACME contre Pebble (acmeshim : corrige une différence de Pebble avec Let's Encrypt)
examples/pingbot/       bot d'exemple (répond « pong » à « !ping ») : jeton de bot, passerelle, REST
deploy/identity/        déploiement Docker Compose du service Identity (Let's Encrypt ou derrière un proxy)
docs/heberger-un-serveur.md   guide de l'hébergeur d'un serveur communautaire (installation, sauvegarde, restauration, mises à jour)
docs/api.md             documentation publique de l'API des serveurs communautaires (bots, clients)
Dockerfile.identity     image distroless (~25 Mo), volume /data, ports 8080/tcp et 3478/udp (relais d'appels)
Dockerfile.server       image distroless + livekit-server (~141 Mo), volume /data, ports 8090/tcp, 7881/tcp, 7882/udp
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
- **Amis** (`friendships`, une ligne par paire, `user_a < user_b`) : demande par pseudo, demande croisée = acceptation, refus/annulation/retrait = `DELETE`. Événement `FRIENDS_UPDATE`. Conversation directe et création/ajout dans un groupe : **amis uniquement** (`requireFriendOrSelf`). Annuaire de clés, clés à usage unique, messages entre appareils : **contacts** = amis ou membres d'une conversation commune (`requireContact` ; `DEVICES_UPDATE` va aussi aux co-membres).
- **Appareil = session Identity.** Chaque appareil publie ses clés Olm (curve25519, ed25519) signées par lui-même (`e2ekeys.DeviceKeys`). **Clé maîtresse** Ed25519 du compte : créée par le 1er appareil, qui se certifie (`e2ekeys.DeviceCert`) → validé d'office. Un nouvel appareil reste non validé jusqu'à ce qu'un appareil détenant la clé maîtresse le certifie (`POST /v1/keys/certify`) après comparaison du **code de vérification** (`e2ekeys.VerificationCode` : 80 bits du hachage de sa clé ed25519, `XXXX-XXXX-XXXX-XXXX`), puis lui envoie la graine maîtresse et l'historique par Olm.
- **Confiance côté client** : un appareil est de confiance si sa signature propre et son certificat vérifient avec la clé maîtresse **épinglée au premier contact** (changement → envoi refusé, avertissement). Les clés de conversation ne sont partagées qu'avec des appareils de confiance ; les secrets reçus ne sont acceptés que d'appareils de confiance.
- **Olm** (par paire d'appareils) transporte : `room_key` (clé Megolm d'une conversation), `device_approval` (graine maîtresse), `history` (historique + sessions Megolm exportées). Le clair lie expéditeur et destinataire (`olmPlain`), vérifié à la réception. Clés à usage unique signées (`e2ekeys.OneTimeKey`), 20 maintenues sur le serveur ; clé de secours acceptée par l'API (pas encore générée par le client).
- **Megolm** (par conversation et appareil émetteur) chiffre les MP ; renouvelé après 100 messages, 7 jours, ou si un appareil destinataire disparaît (donc aussi quand un membre quitte un groupe). Le clair contient `dm_id`, expéditeur, date, vérifiés contre l'enveloppe ; protection contre le rejeu par (session, index).

### Conversations avancées (P1 bloc 5)

- **Conversations** (migration 6) : `conversations` (`direct` | `group`, `name`, `owner_id`), `conversation_members`, `direct_pairs` (une conversation directe par paire), `conv_events` (ex-`dm_events`, mêmes ids : les accusés en attente survivent à la migration ; les anciennes tables `dms`/`dm_events` sont supprimées). L'API garde le chemin `/v1/dms`. Événements `DM_UPDATE` (conversation vue par chaque membre) et `DM_REMOVED` (au membre retiré).
- **Groupes** : créés entre amis (`POST /v1/dms {user_ids, name}`), **10 membres max** (`QUAREL_DM_GROUP_MAX`), chacun peut renommer et **ajouter ses propres amis** ; seul le créateur retire quelqu'un ; qui part s'en va (`DELETE …/members/@me`), le créateur qui part transmet le groupe au plus ancien membre, le dernier qui part le supprime. Les membres d'un groupe n'ont pas besoin d'être amis entre eux. Un nouveau membre ne lit **que les messages envoyés après son arrivée** (il reçoit la clé Megolm à son index courant).
- **Envoi** : `POST /v1/dms/{id}/messages` distribue à tous les appareils de tous les membres (et aux autres appareils de l'expéditeur) ; conversation directe : encore amis requis. **Accusé de distribution** quand tous les appareils des autres membres ont acquitté.
- **Modifier / supprimer** : événements chiffrés comme les autres (`megolmPlain.type` = `edit` | `delete`, `target` = n° de l'événement). **Le serveur ne sait pas que c'est une modification.** Le client n'applique une modification/suppression que si son expéditeur est l'auteur du message visé (`e2e.apply`).
- **Fichiers chiffrés — option D (choix du CP) : pair à pair d'abord, serveur en secours.** Le client chiffre (XChaCha20-Poly1305, clé aléatoire, données associées = id de la conversation) et garde le chiffré (`<profil>.files/<id>`). Puis :
  1. **Offre** (message Olm `type: file`, `{action: offer, conv_id, file_id, size}`) à tous les appareils validés des membres ; les appareils **en ligne** le demandent (`fetch` avec offre SDP) et le reçoivent par **canal de données WebRTC** (`quarel-file`, morceaux de 16 Ko, accusé `ok` après déchiffrement). Fenêtre de 10 s. **Jamais par le relais TURN** (STUN seulement) : la copie serveur est le secours. Signalisation gardée dans `file_signals` de l'état E2E.
  2. **Copie serveur** seulement pour les appareils non servis : `POST /v1/dms/{id}/files?for=<appareils>` (corps brut, `QUAREL_DM_FILE_MAX_MB` = 25, 60/h/utilisateur) → table `conv_file_pending` (migration 7) ; chaque appareil `POST …/files/{file}/ack` après l'avoir reçu ; **effacée dès que plus aucun appareil ne l'attend**, au plus tard après `QUAREL_DM_FILE_TTL` (**7 jours**) ; appareils révoqués → plus attendus. Au-delà de la limite : pas de copie serveur, pair à pair uniquement.
  3. **Événement Megolm** `type: file` avec `{id, server_id?, name, mime, size, key, nonce}` : la clé ne voyage que dans le message chiffré. À la réception, un appareil sans copie locale récupère la copie serveur puis l'acquitte.
  **Plus tard** (appareil ajouté, gros fichier manqué, copie serveur effacée) : `dm-download` demande le fichier en pair à pair à **tout appareil d'un membre** qui le détient (et `dm-listen` sert ceux qu'il détient, seulement aux membres de la conversation). Téléchargement serveur réservé aux membres ; suppression par l'expéditeur. Dossier serveur `data/dm-files`.
- **« En train d'écrire » et accusés de lecture** : `POST /v1/dms/{id}/typing` (1 par 3 s) → `DM_TYPING {dm_id, user_id}` ; `POST /v1/dms/{id}/read {event_id}` → `DM_READ {dm_id, user_id, event_id}`. **Relayés en direct aux appareils connectés, jamais stockés.** Désactivables : `GET/PATCH /v1/me/privacy {typing, read_receipts}` ; désactivé, le serveur ne relaie rien (la lecture reste synchronisée entre ses propres appareils).
- **Boîte aux lettres** (`inbox`, une ligne par appareil destinataire) : `to_device`, `dm`, `receipt`. `GET /v1/inbox` puis `POST /v1/inbox/ack` (suppression). Accusé de distribution (`receipt`) quand tous les appareils du destinataire ont acquitté. Poussée en direct : événement `INBOX` sur la passerelle Identity (une connexion par appareil). Révocation d'une session → clés, clés à usage unique et boîte supprimées, `DEVICES_UPDATE` aux amis.
- **Client de test** : état E2E dans `<profil>.e2e.json` (0600, non chiffré — le vrai client utilisera le trousseau du système), protégé par un **verrou de fichier** (`withE2E`) car plusieurs `quarelctl` peuvent tourner sur le même profil (`dm-listen` + `dm`).

### Appels entre amis (P1 bloc 6)

- **Pair à pair** : le média ne passe jamais par un serveur Quarel, sauf par le relais TURN de secours (et reste alors chiffré DTLS-SRTP de bout en bout).
- **Signalisation chiffrée** : offre/réponse SDP complètes (sans « trickle ICE » : une fois la collecte des chemins terminée) et raccrochage voyagent en messages Olm entre appareils (`olmPlain.type = "call"`, `{call_id, action: invite|answer|reject|hangup, sdp}`) : le service ne voit ni adresses IP ni paramètres. L'invitation part vers tous les appareils validés de l'ami ; le premier qui répond l'emporte, les autres reçoivent un raccrochage. Côté client, les signaux reçus sont gardés dans l'état E2E (`call_signals`, 2 min) pour qu'un autre processus du même profil (ex. `dm-listen`) ne les perde pas. Appels **réservés aux amis** (vérifié par l'appelé).
- **Relais TURN** intégré au service Identity (`internal/identity/turn.go`, `pion/turn` v5, MIT) : `QUAREL_TURN=on`, `QUAREL_TURN_PUBLIC_IP` (obligatoire), `QUAREL_TURN_LISTEN` (`:3478` UDP), `QUAREL_TURN_PORTS` (`49160-49200`). Identifiants **temporaires par utilisateur** (API REST TURN : `expiration:user_id` + HMAC-SHA1 d'un secret `data/turn.secret`, 12 h) via `GET /v1/calls/ice-servers` (60/h) → `{ice_servers: [stun:…, turn:… + identifiants], relay}`. **Refuse de relayer vers des adresses privées, locales, CGNAT** (`PermissionHandler`) : pas de rebond vers le réseau de l'hébergeur ; `QUAREL_TURN_ALLOW_PRIVATE=1` seulement pour les tests. Le STUN du même port sert à découvrir son adresse publique (pas de STUN tiers).
- **Qui héberge le relais (décision du CP)** : **chaque service Identity héberge le relais de ses propres utilisateurs** (`identity.quarel.app` : nous ; une instance tierce : son opérateur, qui peut le couper).
- **Refus du relais par l'utilisateur** : réglage du client (`quarelctl calls relay=off`, `state.call_relay_off`) → les serveurs `turn:` sont ignorés ; l'appel ne réussit que si un chemin direct existe.
- **Client de test** (`cmd/quarelctl/calls.go`, `pion/webrtc` v4) : `call <ami> [--seconds N] [--relay-only]`, `call-listen [--once]` (répond automatiquement), envoie une tonalité PCMU 440 Hz, mesure l'audio reçu et affiche le chemin (réseau local, pair à pair, relais).

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
| GET/POST | `/v1/dms` | Conversations / ouvrir `{user_id}` (ami) ou créer un groupe `{user_ids, name}` |
| PATCH | `/v1/dms/{id}` | Renommer un groupe `{name}` |
| PUT/DELETE | `/v1/dms/{id}/members/{user\|@me}` | Ajouter un ami / retirer (créateur) ou partir |
| POST | `/v1/dms/{id}/messages` | `{payload}` chiffré → `{event_id, recipient_devices}` |
| POST | `/v1/dms/{id}/typing`, `/v1/dms/{id}/read` | Éphémères : `DM_TYPING`, `DM_READ {event_id}` |
| POST/GET/DELETE | `/v1/dms/{id}/files[/{file}][?for=appareils]` | Copie serveur chiffrée (corps brut) pour les appareils non servis en direct → `{id, size, pending_devices, expires_at}` |
| POST | `/v1/dms/{id}/files/{file}/ack` | Cet appareil a le fichier ; copie effacée quand plus personne ne l'attend |
| GET/PATCH | `/v1/me/privacy` | `{typing, read_receipts}` |
| GET | `/v1/calls/ice-servers` | Serveurs STUN/TURN + identifiants temporaires du relais |
| POST | `/v1/to-device` | `{messages: [{device_id, payload}]}` (8 Mo max) |
| GET | `/v1/inbox` | Éléments en attente pour cet appareil |
| POST | `/v1/inbox/ack` | `{ids}` |
| GET | `/v1/gateway` | WebSocket : READY, INBOX, FRIENDS_UPDATE, DEVICES_UPDATE, PRESENCE_UPDATE, PRESENCE_SETTING, USER_UPDATE, DM_UPDATE, DM_REMOVED, DM_TYPING, DM_READ |

## Mise en ligne (jalon 6) : HTTPS, UPnP, limites, récupération

### HTTPS (`internal/tlsconf`, `pkg/tlsbind`)
- `QUAREL_TLS` : `self-signed` (**défaut du serveur communautaire**), `acme`, `files`, `off` (**défaut d'Identity**, qui refuse `self-signed` : les serveurs communautaires vérifient son certificat avec les autorités publiques).
- **Auto-signé lié à l'identité** : certificat ECDSA P-256 (`data/tls-selfsigned.*`, 5 ans, régénéré si l'identité ou `QUAREL_TLS_HOSTS` changent) portant l'URI `quarel://binding/<clé Ed25519 du serveur>/<signature de la clé du certificat>`. `server_id` = hachage de cette clé : un client qui connaît le `sid` (lien d'invitation ou épinglé) vérifie le serveur **pendant la poignée de main TLS**, sans autorité (`tlsbind.ClientConfig`). Le client vérifie aussi que l'identité prouvée par TLS = `server_id` annoncé au défi de connexion (anti-relais). Certificat d'autorité (acme/files) : vérification classique.
- **acme** : `QUAREL_TLS_DOMAIN`, `QUAREL_TLS_EMAIL`, `QUAREL_ACME_DIRECTORY` (défaut Let's Encrypt), `QUAREL_ACME_CA_FILE` (autorité ACME privée) ; défi TLS-ALPN-01 sur le port du service → le port public 443 doit y mener. Cache `data/acme`. **files** : `QUAREL_TLS_CERT`, `QUAREL_TLS_KEY`.
- Les webhooks LiveKit arrivent sur un **écouteur interne HTTP en boucle locale** (port aléatoire, `InternalHandler`) : LiveKit ne peut pas vérifier un certificat auto-signé.
- Navigateurs : avertissement à accepter pour l'auto-signé (page de test vocal) ; Playwright : `ignoreHTTPSErrors`.

### UPnP et diagnostic (`internal/netdiag`)
- `QUAREL_UPNP` (`on` par défaut, **`off` dans `make run-server` et les tests : ne jamais ouvrir de ports sur la box du CP sans son accord**). Découverte IGD (WANIPConnection2/1, WANPPPConnection1), ouverture de 8090/tcp (ou `QUAREL_PUBLIC_PORT`), 7882/udp, 7881/tcp, bail 1 h renouvelé toutes les 30 min, suppression à l'arrêt.
- `QUAREL_VOICE_PUBLIC_IP=auto` (défaut) : adresse annoncée pour la voix = IP publique de la box (UPnP) si elle est publique, sinon découverte STUN par LiveKit ; `local` = adresses locales seulement.
- STUN maison (RFC 5389, **IPv4 forcé**, `stun.l.google.com`, `stun.cloudflare.com`) ; verdicts : `ok`, `manual_ports`, `partial`, `double_nat` (adresse de la box privée/CGNAT ou ≠ STUN), `unknown`. `GET /v1/server/network` (`manage_server`), commande `quarelctl network`.
- En Docker, l'UPnP (multicast) exige `--network host`.

### Limitation de débit (`internal/ratelimit`)
- Seaux de jetons en mémoire (remis à zéro au redémarrage), clés par IP (IPv6 groupé en /64) ; `X-Forwarded-For` pris en compte seulement depuis `QUAREL_TRUSTED_PROXIES` (CIDR). `QUAREL_RATE_LIMITS=off` désactive (tests de bout en bout).
- **Identity** (`DefaultLimits`) : 300 requêtes/min/IP ; 5 inscriptions/h/IP ; 20 connexions/10 min/IP ; 20 demandes d'email/h/IP ; 30 demandes d'ami/h/utilisateur.
- **Blocage anti-bruteforce** revu : 15 échecs/h par **(compte, IP)** → bloqué pour cette IP seulement ; 100 échecs/h par compte toutes IP → bloqué pour tous. Succès = remise à zéro du compteur de cette IP uniquement.
- **Serveur communautaire** : 600 requêtes/min/IP ; 30 connexions/min/IP ; 10 messages/10 s/membre.

### Phrase de récupération (`pkg/recovery`, `cmd/quarelctl/recovery.go`)
- 12 mots de la liste **BIP-39 française** (encodage BIP-39 standard : 128 bits + somme de contrôle ; saisie insensible aux accents et à la casse). Clé de sauvegarde = HKDF-SHA256(secret, identifiant utilisateur) ; chiffrement **XChaCha20-Poly1305** lié à l'identifiant.
- Contenu chiffré : graine de la clé maîtresse, historique des MP, sessions Megolm exportées, contacts épinglés. Stocké opaque sur Identity (`GET/PUT/DELETE /v1/backup`, 16 Mo max) avec **versions** (`PUT {version attendue, data}` → `409 version_conflict` si un autre appareil a sauvegardé : le client télécharge, fusionne, renvoie).
- Mise à jour automatique après chaque envoi/synchronisation qui change le contenu. La clé de sauvegarde est transmise aux appareils approuvés. Restauration : vérifie que la graine correspond à la clé maîtresse publiée, puis l'appareil se certifie lui-même.

## Service Identity — détails

- **Mots de passe** : argon2id (t=3, m=64 Mo, p=2), format PHC ; 4 hachages simultanés max ; hachage factice si le compte n'existe pas (pas d'énumération par le temps de réponse).
- **Sessions** : jeton porteur aléatoire 256 bits, seul le SHA-256 est stocké ; une session par appareil (`device_name`, `device_key`).
- **Email** : code à 6 chiffres, 15 min, 5 essais, renvoi limité à 1/min ; `resend-verification` répond toujours 202. La connexion exige un email vérifié.
- **2FA** : TOTP RFC 6238 (SHA1, 30 s, 6 chiffres, ±1 pas), chaque code utilisable une seule fois (`totp_last_step`) ; 10 codes de secours de 80 bits (SHA-256 stocké), usage unique.
- **Anti-bruteforce** (`lockout.go`) : 15 échecs (mauvais mot de passe ou mauvais code 2FA) en 1 h glissante → `429 account_locked` + `Retry-After`, même avec le bon mot de passe. Compté par compte (email et pseudo partagent le compteur), ou par identifiant tapé si le compte n'existe pas (pas d'énumération). Les re-vérifications de mot de passe/2FA (`2fa/setup`, `2fa/disable`) passent par le même compteur (`s.guarded`). Remise à zéro après une connexion réussie complète. Table `auth_failures`.
- **Compte désactivé** (`users.disabled_at`, réquisition judiciaire) : login, sessions, jetons et profil public refusés ; appareils et données **conservés** (réactivation = retour à l'identique). Posé uniquement par l'outil de l'opérateur (voir « Comptes »).

### Comptes (P1 bloc 4)

- **Codes par email** (`account_codes`, un par utilisateur et usage `reset` | `email`) : 6 chiffres, 15 min, 5 essais, renvoi 1/min ; mêmes règles que la vérification d'email.
- **Mot de passe oublié** : `POST /v1/auth/forgot-password {email}` (répond toujours 202 : pas d'énumération) puis `POST /v1/auth/reset-password {email, code, password, totp_code?}` — **la 2FA reste exigée** (une boîte mail seule ne suffit pas) ; **toutes les sessions sont fermées** (les appareils sont à revalider, avec la phrase de récupération). Email de confirmation.
- **Changer de mot de passe** : `POST /v1/me/password {current_password, new_password}` → les autres sessions sont fermées (`closeSessions`), email d'alerte.
- **Changer d'email** : `POST /v1/me/email {password, new_email}` → code à la nouvelle adresse → `POST /v1/me/email/confirm {code}` ; l'ancienne adresse est prévenue.
- **Changer de pseudo** : `PATCH /v1/me {pseudo}`, **une fois par 24 h** (changer seulement la casse est libre) ; l'ancien pseudo redevient libre. L'identité `(iss, sub)` ne change pas : les serveurs communautaires mettent le handle à jour à la connexion suivante. `USER_UPDATE` aux amis et à soi.
- **Supprimer son compte** : `DELETE /v1/me {password, totp_code?}` → suppression **immédiate et définitive** de la ligne `users`, les clés étrangères effacent en cascade sessions, clés, amitiés, conversations, boîtes, sauvegarde, avatar, blocages. Restent : les messages chiffrés déjà dans les boîtes d'autres personnes (leurs copies), et `admin_log` (sans FK). Les amis reçoivent `FRIENDS_UPDATE none`.
- **Profil public** : `GET /v1/users/{id}/profile` et `/avatar` **sans session** (les clients des serveurs communautaires affichent les avatars de membres non amis) ; `PATCH /v1/me/profile {bio}` (500 caractères), `PUT /v1/me/avatar` (corps = image PNG/JPEG/GIF/WebP détectée sur le contenu, 1 Mo, stockée en base), `DELETE /v1/me/avatar`. `avatar_url` contient une version (`?v=`) → cache long. Comptes désactivés ou non vérifiés : 404.
- **Blocage** (`blocks`) : `POST /v1/blocks {pseudo}` ou `PUT /v1/blocks/{id}`, `DELETE`, `GET`. Supprime amitié/demande ; le bloqué reçoit `404 not_found` en demandant en ami (il ne sait pas qu'il est bloqué), le bloqueur `403 blocked`. Masquer les messages sur les serveurs communautaires : au client (liste `GET /v1/blocks`).
- **Présence** : réglage `users.presence` (`online|idle|dnd|invisible`, `PUT /v1/me/presence`) ; présence **vue par les amis** = `offline` si aucun appareil connecté à la passerelle ou `invisible`, sinon le réglage. Le hub signale le 1er appareil connecté / le dernier déconnecté (`realtime.Hub.OnGroup`, `GroupOnline`) → `PRESENCE_UPDATE {user_id, status}` aux amis ; `presence` dans `GET /v1/friends` (amis seulement) et READY (réglage propre) ; `PRESENCE_SETTING` à ses autres appareils.
- **Liste publique des comptes désactivés** : `GET /v1/disabled-accounts` → `{issuer, accounts: [{sub, since}]}`. Les serveurs communautaires l'interrogent (`QUAREL_DISABLED_POLL`, 10 min) : sessions des membres concernés supprimées, connexions fermées, vocal coupé, connexion refusée (`account_disabled`) ; ils restent membres (retour possible après réactivation).
- **Outil de l'opérateur** (`quarel-identity admin …`, sur la machine : jamais par le réseau ; ouvre la base SQLite en parallèle du service grâce au WAL) : `disable|enable <pseudo|email|id> --reason … [--by …]` (raison et opérateur obligatoires), `log`, `rotate-signing-key`. Tout est consigné dans `admin_log`. Le service ferme les connexions des comptes désactivés sous 15 s (`WatchDisabled`).
- **Rotation de la clé de signature** : `rotate-signing-key` ajoute la clé publique actuelle à `data/retired-keys.json`, **détruit la clé privée**, en crée une nouvelle ; au redémarrage, les clés retirées depuis moins de `QUAREL_TOKEN_TTL` + 1 h restent publiées (les jetons qu'elles ont signés restent valides), puis disparaissent. Les serveurs communautaires rechargent les clés dès qu'ils voient un `kid` inconnu.
- **Déploiement** : `deploy/identity/compose.yaml` (Let's Encrypt direct, port 443) et `compose.proxy.yaml` (derrière un proxy HTTPS), `.env.example` ; guide `docs/deploiement-identity.md`. `QUAREL_SMTP_FROM` peut contenir un nom (`Quarel <quarel@…>`).
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
| POST | `/v1/auth/forgot-password` | — | `{email}` → 202 toujours |
| POST | `/v1/auth/reset-password` | — | `{email, code, password, totp_code?}` |
| PATCH/DELETE | `/v1/me` | session | `{pseudo}` / `{password, totp_code?}` : suppression définitive |
| POST | `/v1/me/password` | session | `{current_password, new_password}` |
| POST | `/v1/me/email`, `/v1/me/email/confirm` | session | `{password, new_email}` puis `{code}` |
| PATCH | `/v1/me/profile` | session | `{bio}` |
| PUT/DELETE | `/v1/me/avatar` | session | Image brute (1 Mo) |
| PUT | `/v1/me/presence` | session | `{status}` |
| GET | `/v1/users/{id}/profile`, `/v1/users/{id}/avatar` | — | Profil public |
| GET/POST | `/v1/blocks` | session | Liste / `{pseudo}` |
| PUT/DELETE | `/v1/blocks/{id}` | session | Bloquer / débloquer |
| GET | `/v1/disabled-accounts` | — | `{issuer, accounts: [{sub, since}]}` |

### Configuration (variables d'environnement)

`QUAREL_ADDR` (`:8080`), `QUAREL_DATA_DIR` (`./data`), `QUAREL_ISSUER` (`localhost:8080` — **doit être le domaine public en production**), `QUAREL_TOKEN_TTL` (`12h`), `QUAREL_DM_FILE_MAX_MB` (25), `QUAREL_DM_FILE_TTL` (`168h`), `QUAREL_DM_GROUP_MAX` (10), `QUAREL_TURN` (`off`), `QUAREL_TURN_PUBLIC_IP`, `QUAREL_TURN_LISTEN` (`:3478`), `QUAREL_TURN_PORTS` (`49160-49200`), `QUAREL_SMTP_HOST/PORT/USER/PASSWORD/FROM` (sans `QUAREL_SMTP_HOST`, les emails sont écrits dans le log : mode dev).

## Serveur communautaire (jalon 2)

- **Identité du serveur** : clé Ed25519 `$QUAREL_DATA_DIR/server.key` (1er démarrage). `server_id` = base32(SHA-256(clé publique)[:16]). Les liens d'invitation portent le `sid` ; le client (quarelctl) épingle le `server_id` au 1er contact et refuse s'il change. Prévu (P1) : certificat TLS lié à cette clé, pour empêcher un intermédiaire.
- **Connexion** : `POST /v1/auth/challenge` → `{server_id, nonce}` (usage unique, 2 min, en mémoire) ; `POST /v1/auth/login {identity_token, nonce, proof, invite?, claim?}`. Le serveur vérifie le jeton hors ligne avec les clés de l'émetteur (`QUAREL_TRUSTED_ISSUERS`), puis la preuve `SignProof(device, server_id, nonce)`. Session = jeton porteur (SHA-256 stocké) qui **expire avec le jeton d'identité** ; le client se reconnecte alors avec un nouveau jeton.
- **Clés des services Identity** (`keys.go`) : récupérées sur `https://<issuer>/.well-known/quarel-identity` (`http://` pour localhost/127.x), cache 1 h, re-téléchargement forcé si `kid` inconnu (1/min max), anciennes clés conservées si l'Identity est injoignable.
- **Membres** : identifiés par `(issuer, subject)`. Départ, expulsion ou bannissement = `left_at` + suppression des sessions et des rôles (`removeMember`) ; la ligne reste : les messages gardent leur auteur, les bans leur cible, même `id` au retour. Le propriétaire (`is_owner`) ne peut pas partir.
- **Propriétaire** : tant qu'il n'y en a pas, un code de revendication est généré (et affiché) à chaque démarrage ; `login` avec `claim` le consomme.
- **Accès** : `private` (défaut, invitation requise pour rejoindre) ou `public`. Invitations : code 10 caractères, `max_uses` (0 = illimité), `expires_in` en secondes (absent = 7 jours, 0 = jamais) ; `create_invite` pour inviter ; chacun voit/révoque les siennes, `manage_server` voit/révoque tout.

### Permissions et rôles (jalon 3)

- **Permissions** (`permissions.go`) : bits stockés en base (**ne jamais renuméroter**), échangés par l'API sous forme de **noms** : `view_channel`, `send_messages`, `manage_messages`, `mention_everyone`, `create_invite`, `manage_channels`, `manage_roles`, `kick_members`, `ban_members`, `manage_server`, `connect`, `speak`, `administrator`, puis (P1) `stream`, `add_reactions`, `attach_files`, `moderate_members`, `view_audit_log`, `mute_members`, `deafen_members`, `move_members`. Réglables par salon : `view_channel`, `send_messages`, `manage_messages`, `mention_everyone`, `manage_channels`, `connect`, `speak`, `stream`, `add_reactions`, `attach_files`, `mute_members`, `deafen_members`, `move_members`.
- **Rôles** (`roles.go`) : le rôle `@everyone` (id 1, position 0, défaut : voir, écrire, inviter, connect, speak, stream, réagir, joindre des fichiers ; `hoist` = affiché à part dans la liste des membres) s'applique à tous ; les autres ont les positions 1..n (plus haut = plus puissant), nouveaux rôles en bas. Permissions serveur = union des rôles ; `administrator` ou propriétaire = tout.
- **Hiérarchie** : on ne gère/attribue que les rôles **strictement sous** son rôle le plus haut (`checkRoleRank`), on n'accorde ou ne retire que des permissions qu'on possède (`checkGrant`), on n'expulse/bannit/surcharge un membre que s'il est strictement en dessous (`outranks`) ; le propriétaire est au-dessus de tout et intouchable.
- **Droits par salon** (`channel_overrides`) : par rôle ou par membre, `allow`/`deny`. Calcul (`inChannel`) : permissions serveur → surcharges de la catégorie parente → surcharges du salon ; à chaque niveau `@everyone`, puis l'union des rôles du membre, puis le membre. Sans `view_channel` : aucune permission, salon invisible (404 `not_found`, pas de fuite d'existence). Modifier une surcharge exige `manage_roles` et de posséder dans ce salon les permissions changées.
- **Instantané** : chaque vérification charge tout l'état des permissions (`loadPerms` : propriétaires, rôles, rôles des membres, salons + surcharges). Simple et cohérent pour des serveurs domestiques ; à mettre en cache si un serveur devient gros.
- **Modération** (`moderation.go`) : `kick_members` (retour possible avec invitation, rôles perdus) ; `ban_members` → table `bans` liée au membre, donc à l'identité `(issuer, subject)` : refus `banned` au login quelle que soit l'invitation ou le mode d'accès ; on peut bannir un membre déjà parti.
- **Messages** : écrire = `send_messages` ; supprimer ceux des autres = `manage_messages` ; `@everyone` et rôles non mentionnables ne notifient qu'avec `mention_everyone` (le texte reste). Rôle mentionnable : `<@&role_id>` notifie pour tous.
- **Salons** : `text`, `voice` (média au jalon 4), `category` (non imbriquables). Liste plate triée `(position, id)`, le client construit l'arbre via `parent_id`. Supprimer une catégorie remonte ses salons à la racine. Création initiale : « Salons textuels / général », « Salons vocaux / Général ».
- **Messages** : 1–4000 caractères ; ids entiers croissants. Historique : `?limit=` (≤100, défaut 50), `?before=ID` ou `?after=ID`, toujours renvoyé en ordre chronologique. Mentions : `<@member_id>` et `@everyone` (mot isolé) ; `<@&role_id>` viendra avec les rôles. Mentions recalculées à l'édition.
- **Horodatages** en millisecondes Unix en base, RFC 3339 en JSON.
- **SQLite à une seule connexion** (les deux services) : dans une transaction, **toujours** requêter via `tx`, jamais `s.db`, et faire les vérifications (droits, amis) **avant** d'ouvrir la transaction — sinon interblocage (vécu au jalon 5). Les cibles `make test` ont un délai maximal pour qu'un blocage échoue au lieu de geler.

### Messages et salons (P1 bloc 1)

- **Types de salon côté API** : `text`, `voice`, `category`, `announcement`, `thread`. En base, annonces et fils sont des salons `text` marqués (`announcement`, `thread`, `thread_starter`) : la contrainte CHECK du type ne peut pas changer sans reconstruire la table (ce qui supprimerait les messages en cascade). `channel.messaging()` = text/announcement/thread.
- **Fils** : créés depuis un message (`thread_starter`, unique), `parent_id` = salon textuel, pas de fil dans un fil, pas de changement de parent. **Droits = ceux du salon parent** (`inChannel` redirige). Supprimer un salon supprime ses fils. Le message de départ porte `thread_id`.
- **Annonces** : écrire exige `send_messages` **et** `manage_messages` (`requirePost`).
- **Message** enrichi par `enrich()` (qui remet d'abord à zéro les champs calculés) : `reply_to` + `referenced {id, author_id, content (200 caractères)}` (réponse dans le même salon ; l'auteur cité est mentionné sauf `mention_reply: false`), `attachments`, `embeds`, `reactions [{emoji, count, me}]` (ordre de première réaction), `pinned_at`, `thread_id`.
- **Réactions** : `add_reactions`, 20 emojis différents max par message, emoji = 32 octets max, au moins un symbole Unicode (≥ U+2000), ni lettre latine, ni espace ; retirer celle d'un autre exige `manage_messages`. Événements `REACTION_ADD|REMOVE {channel_id, message_id, emoji, member_id}`.
- **Épingles** : `manage_messages`, 50 max par salon, événement `MESSAGE_UPDATE`.
- **Pièces jointes** (`attachments.go`) : `POST /v1/channels/{id}/attachments` (multipart, champ `file`, `attach_files`, limite `QUAREL_MAX_UPLOAD_MB`, 10 envois/min) → `{id, filename, content_type, size, url}`, puis `attachments: [ids]` dans le message (10 max ; un message peut n'avoir qu'un fichier). Fichiers dans `data/attachments/<id>`. Type **détecté sur le contenu** ; téléchargement réservé à qui voit le salon (un fichier pas encore envoyé : son auteur seulement), `nosniff`, CSP `sandbox`, affichage direct seulement pour images/audio/vidéo/texte, sinon `application/octet-stream` en téléchargement. `CleanupAttachments` (toutes les heures) : envois non rattachés après 1 h, fichiers orphelins. Supprimer le message supprime ses fichiers.
- **Aperçus de liens** (`previews.go`) : 3 liens max par message, récupérés en tâche de fond puis `MESSAGE_UPDATE`. **Anti-SSRF** : l'adresse **résolue** est vérifiée dans `net.Dialer.Control` (privées, locales, lien-local, multicast, CGNAT refusées ; ports 80/443 seulement), 8 s, 1 Mo, 3 redirections, pas de proxy. Titre/description Open Graph ; l'image n'est jamais téléchargée par le serveur. `QUAREL_LINK_PREVIEWS=off` pour désactiver.
- **Recherche** : table FTS5 `messages_fts` (`unicode61 remove_diacritics 2`) tenue à jour par déclencheurs ; chaque mot devient un préfixe entre guillemets (10 mots max, tous requis) ; seulement dans les salons visibles ; filtres `channel_id`, `author_id`, `before` ; `limit` ≤ 50 ; du plus récent au plus ancien.
- **Non-lus** (`read_states`) : `POST /v1/channels/{id}/ack {message_id?}` (absent = dernier message ; ne recule jamais) → `{channel_id, last_read, last_message_id, unread (plafonné à 100), mentions}` + `READ_STATE_UPDATE` aux autres appareils du membre. Envoyer un message marque le salon lu. Messages d'avant l'arrivée du membre et les siens ne comptent pas. `read_states` dans READY.
- **« En train d'écrire »** : `POST /v1/channels/{id}/typing` (1 par 3 s) → `TYPING_START {channel_id, member_id, at}` ; le client l'affiche ~8 s.
- **Réglages de notification** : `channel_id` 0 = tout le serveur ; `level` `default|all|mentions|none` ; `mute_for` en secondes (−1 = indéfiniment → `muted_until` = 9999-12-31). `default` sans sourdine supprime la ligne. Stockés et synchronisés (`NOTIFICATION_SETTINGS_UPDATE`, `notification_settings` dans READY) ; **appliqués par le client**.

### Modération et accès (P1 bloc 2)

- **Restriction** (`ps.restricted`, chargée par `loadRestrictions` dans `loadPerms`) : exclusion temporaire (`timeout_until` futur), règles non acceptées, téléphone non vérifié (dans cet ordre de priorité). Effet : permissions réduites à `permRestricted` (= `view_channel`) partout (`base`, `inChannel`), donc vocal coupé par `reconcileVoice`. **Exemptés** : propriétaire, administrateurs (l'exemption vient de `base`/`inChannel` qui renvoient `permAll` avant la restriction), bots (règles et téléphone seulement). Les refus passent par `ps.deny(member, p)` → code `timed_out` / `rules_not_accepted` / `phone_not_verified` au lieu de `missing_permissions`. `restriction` dans READY et CHANNELS_SYNC. Modifier ses anciens messages est aussi refusé pendant une restriction.
- **Exclusion temporaire** : `PUT /v1/members/{id}/timeout {duration (s, ≤ 28 j), reason}` / `DELETE`, `moderate_members` + hiérarchie, administrateurs immunisés (`cannot_timeout_admin`). `MEMBER_UPDATE` + `syncPermissions` ; un `time.AfterFunc` resynchronise à l'échéance (perdu au redémarrage : sans effet sur les droits, calculés à chaque requête).
- **Suppressions en masse** : `deleteMessages` (fichiers compris, un `MESSAGE_DELETE_BULK {channel_id, ids}` par salon) ; `POST …/messages/bulk-delete {ids ≤ 100}` ; `POST /v1/members/{id}/purge {window (s, -1 = tout), channel_id?}` dans les salons où l'on a `manage_messages`, hiérarchie requise ; `PUT /v1/bans/{id} {delete_messages}` purge tous salons.
- **Journal d'audit** (`audit.go`, table `audit_log`) : `s.audit(ctx, q, actor, action, target, reason, details)` — passer `tx` dans une transaction. Actions : `member_kick|ban|unban|timeout|timeout_remove|role_add|role_remove`, `messages_delete` (d'autrui seulement), `role_create|update|delete`, `channel_create|update|delete`, `override_update|delete`, `server_update`, `invite_delete` (d'autrui), `bot_create|delete|token_reset`, et `voice_*` (bloc 3). `GET /v1/audit-log` (`view_audit_log`, filtres `action`, `actor_id`, `target_id`, `before`, `limit` ≤ 100, du plus récent). **Conservation 90 jours** : `Housekeeping` (toutes les heures : journal, sessions expirées, pièces jointes).
- **Règles** : réglage `rules` (≤ 4000 caractères, public dans `GET /v1/server`). Quand des règles apparaissent, les membres présents sont marqués comme les ayant acceptées ; les nouveaux doivent `POST /v1/members/@me/accept-rules`.
- **Téléphone** : réglage `require_phone` (refusé sans fournisseur). `QUAREL_PHONE_VERIFY` (choix du CP : webhook et OVH recommandés, Twilio en option) :
  - `webhook` (**recommandé**, `sms.go`) : `POST` JSON `{phone, code, text}` vers `QUAREL_PHONE_WEBHOOK_URL` (https, ou http seulement en boucle locale), signé si `QUAREL_PHONE_WEBHOOK_SECRET` : en-tête `X-Quarel-Signature: sha256=<hex HMAC-SHA256(secret, corps)>` ; toute réponse 2xx = envoyé. L'hébergeur branche la passerelle de son choix (son téléphone, un modem GSM, un fournisseur).
  - `ovh` (**recommandé**, fournisseur européen) : API OVHcloud `POST /sms/{service}/jobs`, signature `$1$` + SHA1(secret+clé consommateur+méthode+URL+corps+horodatage), horloge prise sur `/auth/time` ; `QUAREL_OVH_APP_KEY`, `QUAREL_OVH_APP_SECRET`, `QUAREL_OVH_CONSUMER_KEY`, `QUAREL_OVH_SMS_SERVICE` (`sms-xx00000-1`), `QUAREL_OVH_SMS_SENDER` (facultatif ; sinon numéro court), `QUAREL_OVH_ENDPOINT` (`https://eu.api.ovh.com/1.0`).
  - `twilio` (option) : `QUAREL_TWILIO_ACCOUNT_SID`, `QUAREL_TWILIO_AUTH_TOKEN`, `QUAREL_TWILIO_VERIFY_SID` ; Twilio Verify gère lui-même les codes (`Verifications`, `VerificationCheck`).
  - `log` (développement : code dans le journal du serveur).
  Pour `webhook`, `ovh` et `log`, **le serveur gère les codes** (`codeVerifier` : 6 chiffres, 10 min, 5 essais, en mémoire ; un code qui n'a pas pu partir n'est pas retenu) ; le fournisseur ne fait qu'envoyer le SMS. Numéros au format international (`+33…`, `0033…` accepté). `POST /v1/members/@me/phone {phone}` (5/h/membre) puis `…/phone/verify {phone, code}`. Stocké : `phone_hash` = HMAC-SHA256 (clé dérivée de la clé du serveur), unique ; numéro d'un banni → `phone_banned`, d'un membre présent → `phone_in_use`, d'un ancien membre → transféré. Les conflits ne sont vérifiés qu'après le code (sinon on pourrait sonder quels numéros sont inscrits).
- **Bots** (`bots.go`) : membres `bot = 1`, `issuer = "#bot"` (jamais un domaine ; `QUAREL_TRUSTED_ISSUERS` refuse `#…`), `handle` = nom. Jeton `qb_` + 256 bits, SHA-256 dans `bot_tokens`, sans expiration ; `memberForToken` reconnaît le préfixe. `POST/GET /v1/bots`, `POST /v1/bots/{id}/token`, `DELETE /v1/bots/{id}` (`manage_server`, et hiérarchie pour un bot existant : son jeton porte ses droits). Renouvellement → connexions temps réel du bot fermées.

### Vocal (jalon 4)

- **Architecture** : le média ne passe jamais par notre serveur. LiveKit (SFU, `livekit-server` v1.13.7) tourne à côté, lancé et relancé par `quarel-server` (`internal/voice/embedded.go`) avec une config générée (`$DATA/livekit.yaml`) et des clés propres (`$DATA/livekit.keys`). Signalisation LiveKit en boucle locale (7880), exposée aux clients via le proxy **`/lk/`** du serveur ; média : **7882/udp** (+ **7881/tcp** de secours).
- **Pas de SDK LiveKit Go** (il embarque une pile WebRTC) : `internal/voice/livekit.go` signe les jetons (JWT HS256, claim `video`), appelle l'API Twirp JSON (`RemoveParticipant`, `UpdateParticipant`, `ListRooms`, `ListParticipants`) et vérifie les webhooks (JWT + SHA-256 du corps).
- **Salles** : salon vocal `id` = salle LiveKit `channel-<id>`, identité LiveKit = `member_id`, nom = nom affiché.
- **Rejoindre** : `POST /v1/channels/{id}/voice/join` (salon `voice`, `view_channel` + `connect`) → `{url, token, room, can_speak, can_stream, server_mute, server_deaf}`. Jeton valable 1 h (connexion seulement).
- **Droits LiveKit** (`voice.Grant{Microphone, Camera, Screen, Listen}`, calculé par `voiceGrant`) : micro = `speak` et pas coupé par la modération ; caméra et écran (+ son de l'écran) = `stream` ; écoute = pas en sourdine imposée. Traduit en `canPublishSources` (jeton : `microphone`, `camera`, `screen_share`, `screen_share_audio` ; API : en majuscules) et `canSubscribe`. **Une liste de sources vide veut dire « toutes » pour LiveKit** : sans aucune source, `canPublish` est mis à faux. Vérifié en navigateur : LiveKit dépublie les pistes retirées, désabonne quand `canSubscribe` passe à faux et réabonne quand il revient.
- **État vocal** en mémoire (`voiceRegistry`), alimenté par les webhooks LiveKit (`participant_joined|left`, `room_finished`) sur `POST /internal/livekit/webhook` ; reconstruit depuis LiveKit au démarrage (`SyncVoice`). Un membre n'est que dans un salon à la fois (l'arrivée ailleurs le retire du précédent). À l'arrivée, les droits sont revérifiés (jeton périmé → éjection).
- **Respect des permissions** (`reconcileVoice`, appelé après tout changement de droits, restriction, expulsion/ban/départ, suppression de salon, modération vocale) : perte de `connect` → éjection ; tout changement du `Grant` → `UpdateParticipant` (`SetPermissions`).
- **Caméra / écran** : connus par les webhooks `track_published|unpublished` (source `CAMERA` / `SCREEN_SHARE`) → `video`, `screen` dans l'état vocal (ignorés sans le droit `stream`).
- **Modération vocale** : `PATCH /v1/voice/states/{member} {mute?, deaf?, channel_id?, reason?}` — `mute_members` / `deafen_members` / `move_members` vérifiés dans le salon vocal actuel du membre (ou au niveau serveur s'il n'est pas en vocal), puis hiérarchie (`checkVoiceRank` ; soi-même permis). **Micro/son coupés par la modération persistants** (`members.voice_mute|voice_deaf`, migration 5) jusqu'à levée, même après reconnexion. **Déplacement** : LiveKit auto-hébergé ne sait pas déplacer un participant entre salles → le serveur l'éjecte de la salle actuelle puis envoie `VOICE_MOVE {channel_id, from_channel_id}` à ses connexions ; le client rejoint le salon indiqué (la cible doit avoir `connect` : sinon `target_cannot_connect`). `DELETE /v1/voice/states/{member}` = déconnexion (`move_members`). Tout est journalisé (`voice_mute|deafen|move|disconnect`).
- **Micro/sourdine** : décidés par le client (`PATCH /v1/voice/state {self_mute, self_deaf}`, sourdine ⇒ micro coupé), informatifs pour les autres. Événement `VOICE_STATE_UPDATE` (`channel_id: null` = départ), filtré par visibilité du salon ; `voice_states` dans READY et CHANNELS_SYNC.
- **Page de test** `/voice-test/` (HTML/JS embarqués, `livekit-client` 2.22.3 vendu dans le dépôt, licence Apache-2.0) : jeton de session dans le fragment d'URL (`quarelctl voice-test`). Micro, sourdine, caméra, partage d'écran, tuiles vidéo, états de modération, suit `VOICE_MOVE`. Outil de test, pas le futur client. Les éléments audio/vidéo sont retirés par identifiant de piste (`dropTrack`) : `track.detach()` ne suffit pas quand LiveKit retire la piste lui-même.
- **Micro dans un navigateur** : uniquement sur `localhost` ou en HTTPS.

### Temps réel (`GET /v1/gateway`, WebSocket)

1. Client → `{"op":"auth","token":"<session>"}` (premier message, ≤ 10 s). Jamais de cookie : toutes origines acceptées.
2. Serveur → `{"t":"READY","d":{member, server, channels, members}}`.
3. Serveur → `{"t":"<EVENT>","d":…}` : `MESSAGE_CREATE|UPDATE|DELETE`, `MESSAGE_DELETE_BULK`, `REACTION_ADD|REMOVE`, `TYPING_START`, `READ_STATE_UPDATE`, `NOTIFICATION_SETTINGS_UPDATE`, `CHANNEL_CREATE|UPDATE|DELETE`, `MEMBER_JOIN|UPDATE`, `MEMBER_LEAVE {id, reason: left|kicked|banned}`, `VOICE_STATE_UPDATE`, `VOICE_MOVE` (au membre déplacé seulement), `ROLES_UPDATE` (liste complète), `ROLE_DELETE`, `CHANNELS_SYNC {channels, permissions}` (après tout changement de droits : remplace la liste des salons du client), `SERVER_UPDATE`. Les événements peuvent répéter un état déjà dans READY : les appliquer de façon idempotente.
- READY : `{member, server, roles, members, channels, voice_states, read_states, notification_settings, restriction, permissions: {server: [...], channels: {id: [...]}}}` — seulement les salons visibles.
- Les événements de messages et `CHANNEL_CREATE|UPDATE` ne sont envoyés qu'aux membres qui voient le salon (`broadcastChannel`).
- Fermetures : `4001` session invalide/expirée ; `1008` membre parti, client trop lent (file de 256 événements pleine) ou arrêt du serveur. Ping toutes les 30 s.
- Les écritures passent par l'API REST ; le gateway ne fait que diffuser.

### Endpoints

| Méthode | Chemin | Auth | Rôle |
|---|---|---|---|
| GET | `/v1/server` | — | Infos publiques `{id, name, access, member_count, rules, require_phone, phone_verification}` |
| PATCH | `/v1/server` | `manage_server` | `{name?, access?, rules?, require_phone?}` |
| POST | `/v1/auth/challenge` | — | Défi de connexion |
| POST | `/v1/auth/login` | — | `{identity_token, nonce, proof, invite?, claim?}` |
| POST | `/v1/auth/logout` | session | |
| GET | `/v1/members` | session | Membres actifs (avec `roles`) |
| GET/PATCH/DELETE | `/v1/members/@me` | session | Profil / `{nickname}` / quitter |
| GET | `/v1/members/@me/permissions` | session | `{server: [...], channels: {id: [...]}}` |
| PUT/DELETE | `/v1/members/{id}/roles/{role}` | `manage_roles` + hiérarchie | Donner / retirer un rôle |
| POST | `/v1/members/{id}/kick` | `kick_members` + hiérarchie | `{reason?}` |
| PUT/DELETE | `/v1/members/{id}/timeout` | `moderate_members` + hiérarchie | `{duration, reason?}` / lever |
| POST | `/v1/members/{id}/purge` | `manage_messages` + hiérarchie | `{window, channel_id?, reason?}` → `{deleted}` |
| POST | `/v1/members/@me/accept-rules` | session | Accepter les règles |
| POST | `/v1/members/@me/phone` | session | `{phone}` → code envoyé |
| POST | `/v1/members/@me/phone/verify` | session | `{phone, code}` |
| GET | `/v1/audit-log` | `view_audit_log` | Journal de modération |
| GET/POST | `/v1/bots` | `manage_server` | Bots / `{name}` → `{member, token}` |
| POST | `/v1/bots/{id}/token` | `manage_server` + hiérarchie | Nouveau jeton |
| DELETE | `/v1/bots/{id}` | `manage_server` + hiérarchie | |
| GET | `/v1/bans` | `ban_members` | |
| PUT/DELETE | `/v1/bans/{member_id}` | `ban_members` (+ hiérarchie pour PUT) | `{reason?, delete_messages?}` / lever |
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
| POST | `/v1/channels/{id}/messages` | `send_messages` | `{content, reply_to?, mention_reply?, attachments?}` |
| PATCH | `/v1/channels/{id}/messages/{mid}` | auteur | `{content}` |
| DELETE | `/v1/channels/{id}/messages/{mid}` | auteur ou `manage_messages` | |
| POST | `/v1/channels/{id}/messages/bulk-delete` | `manage_messages` | `{ids (≤ 100), reason?}` → `{deleted}` |
| POST | `/v1/channels/{id}/attachments` | `attach_files` (+ `send_messages`) | Envoi d'un fichier (multipart `file`) |
| GET | `/v1/attachments/{id}/{nom}` | `view_channel` | Téléchargement |
| PUT/DELETE | `/v1/channels/{id}/messages/{mid}/reactions/{emoji}` | `add_reactions` / soi | Réagir / retirer sa réaction |
| DELETE | `/v1/channels/{id}/messages/{mid}/reactions/{emoji}/{member}` | `manage_messages` | Retirer la réaction d'un autre |
| GET | `/v1/channels/{id}/pins` | `view_channel` | Messages épinglés |
| PUT/DELETE | `/v1/channels/{id}/pins/{mid}` | `manage_messages` | Épingler / désépingler |
| POST | `/v1/channels/{id}/messages/{mid}/threads` | `send_messages` | `{name?}` → fil |
| POST | `/v1/channels/{id}/typing` | `send_messages` | « En train d'écrire » |
| POST | `/v1/channels/{id}/ack` | `view_channel` | `{message_id?}` → état de lecture |
| GET | `/v1/read-states` | session | Non-lus et mentions par salon visible |
| GET | `/v1/notification-settings` | session | Réglages du membre |
| PUT | `/v1/notification-settings/{id\|0}` | session | `{level, mute_for?}` |
| GET | `/v1/search` | session | `?q=&channel_id=&author_id=&before=&limit=` |
| POST | `/v1/channels/{id}/voice/join` | `connect` | Jeton LiveKit `{url, token, room, can_speak, can_stream, server_mute, server_deaf}` |
| GET | `/v1/voice/states` | session | Qui est dans quel salon vocal (salons visibles) |
| PATCH | `/v1/voice/state` | session (en vocal) | `{self_mute?, self_deaf?}` |
| POST | `/v1/voice/leave` | session | Quitter le vocal |
| PATCH | `/v1/voice/states/{member}` | `mute_members` / `deafen_members` / `move_members` + hiérarchie | `{mute?, deaf?, channel_id?, reason?}` |
| DELETE | `/v1/voice/states/{member}` | `move_members` + hiérarchie | Déconnecter du vocal |
| POST | `/internal/livekit/webhook` | signature LiveKit | Événements de salle |
| * | `/lk/…` | — | Proxy vers la signalisation LiveKit embarquée |
| GET | `/voice-test/` | jeton dans le fragment | Page de test vocal |
| GET | `/v1/gateway` | premier message | WebSocket |

### Configuration

Vocal : `QUAREL_VOICE` (`embedded` par défaut, `external`, `off`) ; embarqué : `QUAREL_LIVEKIT_BIN` (`livekit-server`, `/livekit-server` dans l'image), `QUAREL_VOICE_SIGNAL_PORT` (7880, boucle locale), `QUAREL_VOICE_TCP_PORT` (7881), `QUAREL_VOICE_UDP_PORT` (7882), `QUAREL_VOICE_PUBLIC_IP` (vide = adresses locales, `auto` = découverte STUN, ou une IP) ; externe : `QUAREL_LIVEKIT_URL`, `QUAREL_LIVEKIT_API_URL`, `QUAREL_LIVEKIT_KEY`, `QUAREL_LIVEKIT_SECRET`. Sans binaire LiveKit, le serveur démarre avec le vocal désactivé.

Messages : `QUAREL_MAX_UPLOAD_MB` (25), `QUAREL_LINK_PREVIEWS` (`on`). Limites serveur : 10 envois de fichiers/min/membre, 5 codes SMS/h/membre en plus des précédentes. Téléphone : `QUAREL_PHONE_VERIFY` (`off`, `webhook`, `ovh`, `twilio`, `log`) et ses variables `QUAREL_PHONE_WEBHOOK_*`, `QUAREL_OVH_*`, `QUAREL_TWILIO_*` (voir « Modération et accès »).

`QUAREL_DISABLED_POLL` (`10m`) : fréquence de lecture des comptes désactivés des services Identity.

`QUAREL_ADDR` (`:8090`), `QUAREL_DATA_DIR` (`./data`), `QUAREL_SERVER_NAME` (nom au 1er démarrage seulement), `QUAREL_TRUSTED_ISSUERS` (liste séparée par des virgules ; défaut `identity.quarel.app`, instance officielle — domaine `quarel.app` choisi par le CP ; **ce nom ne doit jamais changer**, il fait partie de chaque identité).

## Exploitation : sauvegarde, restauration, mises à jour (P1 bloc 7)

- **`quarel-server backup <fichier|->`** et **`quarel-identity backup <fichier|->`** (service en marche) : archive `.tar.gz` = manifeste `quarel-backup.json` (`{format, kind, created_at, schema_version, identity, files}`) + **copie cohérente de la base** (`VACUUM INTO`, sans arrêter le service) + clés (`server.key` ; `signing.key`, `retired-keys.json`, `turn.secret`) + dossiers (`attachments`, `acme` ; `dm-files`, `acme`). Jamais d'écrasement d'un fichier existant. `-` = sortie standard (Docker : `docker exec … backup - > f.tar.gz`).
- **`restore <fichier|-> [--force]`** (service arrêté) : tout est vérifié **avant** d'écrire (type de service, format, version de schéma ≤ celle du programme, base et clés présentes, aucun chemin hors du dossier) ; extraction dans un dossier temporaire du dossier de données ; données existantes refusées sans `--force`, sinon **déplacées** dans `before-restore-<date>/` (fonctionne aussi quand le dossier de données est un point de montage Docker). `-` = entrée standard (Docker : l'utilisateur du conteneur ne peut pas lire un fichier 0600 de l'hôte). L'identité restaurée (ID du serveur) est comparée au manifeste.
- **`version`** : version (révision VCS) et version du schéma de base.
- **Mises à jour sans perte** (`sqlitedb.Open`) : avant d'appliquer des migrations à une base existante, copie `…db.pre-v<ancienne version>-<date>` (3 dernières gardées) ; une base plus récente que le programme est refusée (pas de retour arrière destructeur).

## Commandes

```sh
make build            # binaires dans bin/ (dont le bot d'exemple pingbot)
make test             # tests (go test ./...)
make test-race        # tests avec détecteur de concurrence (gcc requis, installé)
make vet
make run-identity     # service Identity local sur :8080, données dans ./data/identity
make run-server       # serveur communautaire local sur https://localhost:8090 (auto-signé, UPnP off), données dans ./data/server
make docker-identity  # image quarel-identity
make docker-server    # image quarel-server
make e2e-voice        # vocal de bout en bout avec 2 navigateurs : audio, caméra, écran, droits, modération (18 vérifications)
make e2e-dm           # scénario MP chiffrés de bout en bout (16 vérifications)
make e2e-security     # récupération, HTTPS lié à l'identité, limites (18 vérifications)
make e2e-acme         # HTTPS via ACME contre Pebble (Docker)
make e2e-messages     # réponses, réactions, fichiers, recherche, fils, non-lus (18 vérifications)
make e2e-moderation   # exclusion, purge, journal, règles, téléphone, bots avec examples/pingbot (24 vérifications)
make e2e-ops          # sauvegardes à chaud des deux services, restauration, retour à l'identique (15 vérifications)
make e2e-calls        # appels pair à pair réels (WebRTC) : direct, par le relais TURN, relais refusé (10 vérifications)
make e2e-dm-groups    # groupes, modification/suppression, fichiers chiffrés, frappe, lecture (22 vérifications)
make e2e-accounts     # comptes : mots de passe, email, pseudo, profil, blocage, présence, suppression, outil opérateur (30 vérifications)
./bin/quarel-server backup f.tar.gz   # sauvegarde à chaud (idem quarel-identity), restore f.tar.gz [--force], version
./bin/quarelctl help  # client de test
```

Tests : les tests d'intégration (`internal/identity/identity_test.go`, `internal/community/community_test.go`) démarrent un vrai serveur HTTP sur une base SQLite temporaire, avec une horloge contrôlable (`srv.now`). Côté Identity, argon2 est allégé ; côté communautaire, un `idtoken.Signer` en mémoire remplace le service Identity (`staticKeys`) et `fakeVoice` remplace LiveKit.

Test vocal de bout en bout (hors `go test`) : `make e2e-voice` — vrai Identity + serveur + LiveKit, 2 Chromium sans interface avec micro simulé (Playwright, `test/e2e/`). Installe Playwright au premier lancement ; Chromium et ses dépendances système sont déjà présents sur la machine.

## Environnement de dev

- OS : Linux. Disponibles : Docker, Node.js, Python 3, make, gcc, livekit-server 1.13.7 (`~/.local/bin`, somme de contrôle vérifiée), Go 1.27.1 (installé dans `~/.local/go`, PATH ajouté dans `~/.zshrc`). Non installés : Rust, `gh`. `sudo` non interactif disponible (le CP autorise l'installation d'outils si besoin).
- Si `go` est introuvable dans le shell courant : `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"`.
- Git : branche `main`, remote `origin` = `git@github.com:anlekg/quarel.git` (SSH). Identité locale au dépôt : `anlekg` / adresse masquée GitHub `106981899+anlekg@users.noreply.github.com` (ne jamais utiliser l'email personnel).
