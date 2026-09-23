# Quarel — Journal de projet

> Ce fichier documente le **processus** du projet : vision, recherches, décisions et leur justification, avancement.
> La fiche technique (stack, archi, conventions, commandes) vit dans `CLAUDE.md`.
> La liste des fonctionnalités à implémenter vivra dans `PLAN.md`.

## Rôles

- **Chef de projet / testeur** : l'utilisateur — définit les priorités, valide les choix, teste.
- **Développement** : Claude — recherche, architecture, code, documentation.

## Vision

Un équivalent de Discord où **les serveurs ne sont pas hébergés par un fournisseur central** : chacun peut héberger son propre serveur chez lui, via un simple conteneur Docker (puis plus tard un `.exe`). Les utilisateurs se connectent à ces serveurs depuis un client unique.

### Principes de conduite

1. Fonctionnalités d'abord (backend / API / temps réel), **front-end en dernier**.
2. Installation d'un serveur = une commande (`docker run ...`), sans dépendances externes obligatoires.
3. Chaque décision importante est tracée ici avec sa justification.

## Phases

| # | Phase | Statut |
|---|-------|--------|
| 1 | Revue des solutions open source existantes + choix structurants | ✅ Terminé |
| 2 | Liste des fonctionnalités → `PLAN.md` | ✅ Validé le 2026-09-23 |
| 3 | Choix de stack & architecture → `CLAUDE.md` | ✅ Go validé le 2026-09-23 |
| 4 | Développement backend (itératif, testé par le CP) | 🟡 Jalons 1 à 4 livrés, en test |
| 5 | Front-end / client | ⚪ À faire |
| 6 | Packaging `.exe` | ⚪ À faire |

---

## Phase 1 — Revue de l'existant (2026-09-23)

### Solutions « type Discord »

| Projet | Licence | Stack | Voix/Vidéo | Self-host | Notes |
|---|---|---|---|---|---|
| **Stoat** (ex-Revolt, renommé oct. 2025) | AGPL-3.0 | Rust (backend), TS/React (web) | Oui, via **LiveKit** | Docker Compose, ~12 conteneurs, MongoDB (~1 Go) | UX la plus proche de Discord (serveurs, salons texte/voix, rôles, bots). Lourd à héberger ; les clients officiels ne supportent pas bien les instances self-hostées. |
| **Spacebar** (ex-Fosscord) | AGPL-3.0 | Node.js/TS, PostgreSQL | Expérimental (signalisation WebRTC OK, pas d'UDP) | Docker | Réimplémente l'**API Discord** (bots/clients Discord compatibles). Voix pas fiable, client web non recommandé. |
| **Fluxer** (lancé jan. 2026) | AGPL-3.0 | — | Oui (VoIP) | Self-host + fédération « sur la roadmap » | Très jeune, orienté service hébergé freemium. |
| **Matrix + Element** (Synapse) | Apache 2.0 / AGPL | Python (Synapse), TS/React | Oui (Element Call, LiveKit) | Docker/K8s | Fédéré, chiffré E2E. Protocole complexe, modèle « salons » plutôt que « serveurs Discord », déploiement lourd. |

### Solutions « type Slack » (moins pertinentes)

| Projet | Licence | Stack | Voix | Remarque |
|---|---|---|---|---|
| Rocket.Chat | MIT (+EE) | Node, MongoDB (replica set) | Oui | Lourd (2–4 Go RAM). |
| Mattermost | AGPL/MIT | Go, PostgreSQL | Limité | Orienté DevOps, limites dans l'édition gratuite. |
| Zulip | Apache 2.0 | Python/Django, PostgreSQL | Non | Modèle par sujets, pas Discord. |
| Nextcloud Talk | AGPL | PHP | Oui | Nécessite tout Nextcloud. |

### Modèle « TeamSpeak / Mumble » (le plus proche de notre vision)

- **Mumble** (BSD) : serveur `murmur` léger, un binaire, auto-hébergé ; le client se connecte à N serveurs indépendants. Excellente voix basse latence, mais pas de chat moderne/persistant.
- **TeamSpeak** (propriétaire) : même modèle. C'est ce modèle « chacun héberge son serveur, un client pour tous » que Quarel veut marier à l'expérience Discord.

### Briques réutilisables (pas des produits, des composants)

- **LiveKit** (Apache 2.0, Go) : SFU WebRTC pour voix/vidéo/partage d'écran, utilisé par Stoat et Element Call. Un seul binaire, embarquable dans notre conteneur.
- **Pion** (Go, MIT) : stack WebRTC bas niveau si on veut un SFU intégré sur mesure.
- **mediasoup** (Node/C++, ISC) : SFU alternatif.

### Constats

1. **Aucun projet existant n'est « léger + une seule commande + type Discord »**. Stoat est le plus complet mais lourd (Mongo, Redis, RabbitMQ, MinIO, ~12 conteneurs). C'est notre créneau.
2. La **voix** est le point dur partout : il faut s'appuyer sur un SFU éprouvé (LiveKit) plutôt que réinventer.
3. La vraie question d'architecture n'est pas technique mais **identité** : si chaque serveur est indépendant, comment un utilisateur a-t-il un compte / une identité utilisable sur plusieurs serveurs ? (voir questions ouvertes)
4. Pour un hébergement domestique : **SQLite** (zéro config) plutôt que Postgres/Mongo ; stockage de fichiers sur disque local ; traversée NAT / exposition du serveur à Internet à prévoir (UPnP, relais, tunnel…).

### Questions tranchées (2026-09-23)

- **Q1 — Identité** → **identité portable unique**, gérée par un service central. Objectif : un ban est définitif (on bannit l'identité, pas un compte local recréable). Inscription minimale : **email, pseudo, mot de passe, 2FA**. Aucune autre donnée personnelle.
- **Q2 — Fédération** → **aucune**. Les serveurs communautaires sont totalement isolés les uns des autres (privacy).
- **Q3 — Amis / MP** → amis stockés en **SQL sur le service central**. MP **chiffrés de bout en bout**. Le premier appareil est vérifié à la première connexion. On peut ajouter d'autres appareils en les validant depuis le premier, qui leur transmet l'historique.
- **Q4 — API** → **API propre**, aucune compatibilité ni lien avec Discord.
- **Q5 — Réseau** → **UPnP** automatique, avec en secours l'ouverture manuelle des ports. Documentation utilisateur à rédiger plus tard.

### Architecture qui en découle (proposition, à valider)

Deux types de serveurs :

1. **Service central « Identity »** (hébergé par nous) : comptes (email, pseudo, hash du mot de passe, 2FA), clés publiques des appareils, amis, boîtes aux lettres des MP chiffrés, liste des identités révoquées.
2. **Serveur communautaire** (Docker, chez les gens) : salons, rôles, messages, fichiers, voix. Il ne connaît que l'**ID public** et le pseudo des membres, jamais leur email.

**Principe de privacy proposé :** l'Identity délivre au client un **jeton signé** (ID + pseudo + clé publique + expiration). Le serveur communautaire le vérifie **hors ligne** avec la clé publique de l'Identity. Ainsi, le service central **ne sait pas sur quels serveurs** un utilisateur se connecte.

**Chiffrement des MP :** ne pas inventer de protocole. Utiliser une implémentation éprouvée, **MLS** (standard IETF RFC 9420, ex. OpenMLS) ou **Olm/Megolm** (vodozemac, utilisé par Matrix), qui gèrent le multi-appareil et la signature croisée des appareils.

### Limites et points de vigilance (réponses du CP, 2026-09-23)

- **Ban définitif / contournement :** accepté. Chaque serveur choisit son niveau d'exigence : public, privé (sur invitation), ou vérification supplémentaire comme un numéro de téléphone. C'est au serveur de le gérer.
- **Point unique de défaillance du service central :** accepté pour l'instant. De la redondance sera ajoutée si l'application prend.
- **Perte de tous les appareils :** on ajoute une **phrase de récupération**.

### Questions tranchées (round 2, 2026-09-23)

- **Q6 — Salons des serveurs communautaires** → **pas de chiffrement E2E**. Messages en clair côté serveur ; la sécurité est de la responsabilité des admins du serveur.
- **Q7 — Bans** → **serveur par serveur**. L'équipe peut en plus **désactiver un compte** sur le service central, uniquement sur réquisition judiciaire (très rare).
- **Q8 — Service central** → **open source et auto-hébergeable**. Son adresse est configurable dans le client ; notre instance est proposée par défaut.
- **Q9 — Appels entre amis** → **connexion directe (P2P WebRTC)**, privacy avant tout.

### Conséquences techniques à traiter

- **P2P et NAT :** une connexion directe échoue dans environ 10 à 20 % des cas (NAT symétriques, réseaux d'entreprise ou mobiles). Il faudra un relais TURN de secours, désactivable par l'utilisateur ; sans lui, l'appel échoue. Les adresses IP sont exposées entre les correspondants, et l'utilisateur doit en être informé.
- **Plusieurs services centraux :** si quelqu'un héberge son propre service central, ses identités sont distinctes des nôtres. Un serveur communautaire doit donc déclarer **quels services d'identité il accepte** (le nôtre par défaut). Les identifiants doivent inclure le service d'origine (ex. `pseudo@identity.quarel.app`).
- **Vérification par téléphone :** un serveur communautaire qui l'exige doit la réaliser lui-même avec son propre fournisseur SMS. Le service central ne stocke pas de numéro, pour rester fidèle au principe de données minimales.

---

## Journal des décisions

| Date | Décision | Justification |
|---|---|---|
| 2026-09-23 | Front-end développé en dernier | Priorité aux fonctionnalités (demande CP). |
| 2026-09-23 | Serveur distribué en Docker, `.exe` plus tard | Vision produit. |
| 2026-09-23 | Identité portable unique via un service central (email, pseudo, mdp, 2FA) | Bans définitifs, données personnelles minimales. |
| 2026-09-23 | Serveurs communautaires totalement isolés, sans fédération | Privacy. |
| 2026-09-23 | Amis sur le service central (SQL), MP chiffrés E2E, multi-appareils avec transfert d'historique | Privacy + usage multi-appareils. |
| 2026-09-23 | API propre, aucune compatibilité Discord | Indépendance du projet. |
| 2026-09-23 | Joignabilité : UPnP, puis ouverture manuelle des ports en secours | Simplicité pour l'hébergeur. |
| 2026-09-23 | Chaque serveur choisit son mode d'accès : public, privé, vérification supplémentaire | Lutte contre le contournement de ban, laissée aux serveurs. |
| 2026-09-23 | Phrase de récupération pour les clés E2E | Ne pas perdre l'historique des MP si tous les appareils sont perdus. |
| 2026-09-23 | Salons des serveurs non chiffrés E2E | Simplicité (recherche, modération, bots) ; sécurité gérée par les admins. |
| 2026-09-23 | Bans par serveur ; désactivation centrale uniquement sur réquisition judiciaire | Autonomie des serveurs. |
| 2026-09-23 | Service central open source, adresse configurable, notre instance par défaut | Pas de dépendance forcée envers l'équipe. |
| 2026-09-23 | Appels entre amis en P2P WebRTC | Privacy. |
| 2026-09-23 | Go pour les deux serveurs, monorepo, SQLite pure Go | Binaire unique, `.exe` facile, faible RAM, LiveKit en Go. |
| 2026-09-23 | Identifiant stable aléatoire distinct du pseudo ; bans basés sur (service, ID) | Le pseudo pourra changer sans casser les bans. |
| 2026-09-23 | Jeton sans audience + preuve de possession par clé d'appareil (Ed25519) | Privacy (l'Identity ignore les serveurs visités) sans permettre à un serveur de rejouer le jeton ailleurs. |
| 2026-09-23 | Email vérifié obligatoire pour se connecter ; 2FA facultative | Validé par le CP. |
| 2026-09-23 | Blocage du compte après 15 échecs de connexion en 1 h (mot de passe ou 2FA) | Anti-bruteforce, demandé par le CP. |
| 2026-09-23 | Chaque serveur communautaire a sa propre clé ; son identifiant figure dans les invitations et dans la preuve de connexion | Empêche un serveur malveillant de relayer une connexion vers un autre serveur. |
| 2026-09-23 | Propriétaire désigné par un code de revendication affiché au 1er démarrage | Installation en une commande, sans compte admin à préconfigurer. |
| 2026-09-23 | Serveur privé (sur invitation) par défaut | Privacy. |
| 2026-09-23 | Session serveur limitée à la durée du jeton d'identité (12 h) | Un compte désactivé côté Identity perd l'accès aux serveurs en 12 h max, sans que l'Identity sache où il est connecté. |
| 2026-09-23 | Permissions provisoires : propriétaire seul pour salons/réglages, jusqu'aux rôles du jalon 3 | Livrer le texte avant les rôles. |
| 2026-09-23 | Domaine officiel `quarel.app` ; service Identity sur `identity.quarel.app` (permanent) | Le nom du service fait partie de chaque identité : sous-domaine dédié, jamais modifié. |
| 2026-09-23 | Modèle de permissions inspiré de Discord : rôles cumulatifs, `@everyone`, droits par catégorie puis par salon | Familier pour les communautés qui migrent. |
| 2026-09-23 | Hiérarchie des rôles avancée de P1 à P0 | Sans elle, un modérateur pourrait s'attribuer des droits d'administrateur. |
| 2026-09-23 | `@everyone` ne notifie qu'avec la permission `mention_everyone` (absente par défaut) | Anti-spam de notifications. |
| 2026-09-23 | Bannissement lié à l'identité portable, bannissement préventif possible d'un membre parti | Objectif initial : bans sans retour. |
| 2026-09-23 | Vocal : LiveKit lancé et supervisé par le serveur communautaire, dans la même image | Installation en une commande ; LiveKit existe aussi pour Windows (futur `.exe`). |
| 2026-09-23 | Pas de SDK LiveKit Go : petit client maison (jetons, API Twirp, webhooks) | Évite d'embarquer une pile WebRTC complète dans notre binaire. |
| 2026-09-23 | Signalisation vocale via le port HTTP du serveur (proxy `/lk`) ; ouvrir seulement 7882/udp et 7881/tcp en plus | Moins de ports à ouvrir chez l'hébergeur. |
| 2026-09-23 | Page de test vocal minimale servie par le serveur (validée par le CP) | Tester le micro sans avancer le vrai client. |

---

## Phase 3 — Choix de la stack (proposition du 2026-09-23)

### Contraintes

- Binaire unique, image Docker légère, faible RAM (hébergement domestique).
- Compilation facile en `.exe` Windows.
- Voix via LiveKit (écrit en Go), UPnP, SQLite embarquée.
- Chiffrement E2E exécuté **côté client** : le langage serveur n'impacte pas ce choix.

### Options comparées

| Critère | **Go** | Rust | Node.js / TypeScript |
|---|---|---|---|
| Binaire unique / `.exe` | ✅ Natif, compilation croisée triviale | ✅ Natif, compilation croisée plus délicate | ⚠️ Empaquetage lourd |
| Consommation mémoire | ✅ Faible | ✅ Très faible | ⚠️ Plus élevée |
| Vitesse de développement | ✅ Rapide | ⚠️ Plus lente | ✅ Rapide |
| Voix (LiveKit / Pion) | ✅ Même langage, intégration directe | ⚠️ Processus séparé | ⚠️ Processus séparé |
| SQLite sans dépendance C | ✅ `modernc.org/sqlite` | ✅ `rusqlite` (bundled) | ⚠️ Module natif |
| UPnP | ✅ `goupnp` | ⚠️ Bibliothèques moins mûres | ⚠️ Idem |

### Recommandation

**Go pour les deux serveurs** (Identity et communautaire), dans un monorepo partageant le code commun (jetons, modèles, utilitaires).
Le client (phase finale) sera choisi plus tard ; piste : Tauri avec un cœur Rust pour la crypto E2E (OpenMLS ou vodozemac).

**Validé par le CP le 2026-09-23 :** Go pour les serveurs, Go installé localement (`~/.local/go`), dépôt git initialisé.

---

## Phase 4 — Développement

### Jalon 1 — Service Identity minimal (livré le 2026-09-23)

**Livré :** inscription, vérification de l'email par code, connexion par email ou pseudo, sessions par appareil, 2FA TOTP avec codes de secours, jeton d'identité portable vérifiable hors ligne, preuve de possession par l'appareil, client de test `quarelctl`, image Docker (~23 Mo).

**Validation :** 7 tests automatisés (dont vecteurs officiels RFC 6238, rejeu de codes 2FA, rejeu de preuves entre serveurs, jeton falsifié ou `alg=none`) ; parcours complet rejoué à la main avec `quarelctl` ; image Docker démarrée et interrogée.

**Guide de test du CP :** `docs/tests/jalon-1.md`.

**Choix faits pendant le développement (validés par le CP) :**
- 2FA **facultative** (activable par l'utilisateur), pas obligatoire à l'inscription.
- Connexion impossible tant que l'email n'est pas vérifié.
- Bans basés sur l'identifiant stable, pas sur le pseudo.
- Le pseudo est unique sur un service Identity, insensible à la casse ; 3–32 caractères : lettres, chiffres, `_ . -`.

**Ajout demandé par le CP :** blocage du compte après 15 échecs de connexion par heure (mot de passe ou code 2FA), levé automatiquement quand le plus ancien échec a plus d'une heure.
*Limite connue :* n'importe qui connaissant un pseudo peut bloquer ce compte pendant 1 h en tapant de faux mots de passe. Parade prévue avec la limitation par IP (P1).

**Reporté (déjà au plan en P1) :** limitation par IP, mot de passe oublié, outil admin de désactivation, chiffrement du secret TOTP au repos.

### Jalon 2 — Serveur communautaire texte (livré le 2026-09-23)

**Livré :** serveur auto-hébergé (binaire + image Docker ~23 Mo), revendication du propriétaire par code, serveur privé ou public, invitations (nombre d'utilisations, expiration, révocation), membres et surnoms, départ/retour, salons texte/vocaux/catégories avec ordre, messages avec historique paginé, édition, suppression, mentions `@membre` et `@everyone`, temps réel par WebSocket. Le client de test `quarelctl` couvre tout, dont `listen` pour voir les événements en direct.

**Validation :** 6 nouveaux tests d'intégration (revendication, invitations, sécurité de connexion : rejeu de défi, preuve pour un autre serveur, jeton volé sans la clé d'appareil, émetteur non reconnu, jeton falsifié, expiration ; salons ; messages et pagination ; membres ; temps réel), passés aussi avec le détecteur de concurrence. Scénario complet à deux utilisateurs rejoué avec `quarelctl` ; image Docker démarrée.

**Guide de test du CP :** `docs/tests/jalon-2.md`.

**Limites connues (prévues au plan) :**
- Pas de TLS : tout circule en clair (HTTPS prévu en P1 dans B1). À ne pas exposer sur Internet en l'état.
- Pas encore d'UPnP (P1).
- Les salons vocaux existent mais sans audio (jalon 4).
- Pas de bans ni de rôles (jalon 3).

**Domaine officiel (tranché le 2026-09-23) :** le CP commande `quarel.app`. Le service Identity officiel sera `identity.quarel.app` (déjà la valeur par défaut des serveurs communautaires).
- Ce nom est l'identifiant permanent des comptes (`pseudo@identity.quarel.app`, bans basés sur `(identity.quarel.app, sub)`) : **il ne doit jamais changer** et le domaine doit être renouvelé sans faute.
- `.app` impose HTTPS partout (domaine préchargé HSTS) : compatible avec notre besoin, le service Identity devra avoir un certificat valide.

### Jalon 3 — Rôles, permissions, modération (livré le 2026-09-23)

**Livré :** rôles (création, couleur, mentionnable, ordre, suppression), rôle `@everyone`, 13 permissions, hiérarchie stricte, droits par catégorie et par salon (rôle ou membre, autoriser/refuser), salons invisibles sans `view_channel` (API et temps réel), expulsion, bannissement lié à l'identité (y compris préventif), levée de ban, mentions de rôles, permissions visibles par le client (`my-perms`, `CHANNELS_SYNC`). Mise à jour automatique des serveurs du jalon 2 (vérifiée sur des données réelles).

**Validation :** 6 nouveaux tests (hiérarchie, droits par salon, mentions, bans, filtrage temps réel) + tests existants adaptés, tous passés aussi avec le détecteur de concurrence ; scénario complet à 3 comptes rejoué avec `quarelctl`.

**Guide de test du CP :** `docs/tests/jalon-3.md`.

**Limites connues :** exclusion temporaire, journal d'audit et suppression en masse des messages restent en P1.

### Jalon 4 — Salons vocaux (livré le 2026-09-23)

**Livré :** audio de groupe dans les salons vocaux via LiveKit embarqué (démarrage, relance automatique, configuration et clés générées), jetons d'accès selon `connect`/`speak`, suivi des participants par webhooks, un seul salon vocal à la fois, micro coupé et sourdine, application en direct des changements de droits (retrait de la parole, éjection), déconnexion vocale lors d'une expulsion/ban/suppression de salon, page de test vocal, commandes `voice` et `voice-test`, événements vocaux dans `listen`. Image Docker avec LiveKit (139 Mo, contre 23 Mo sans : le binaire LiveKit pèse ~110 Mo).

**Validation :**
- Tests automatisés : client LiveKit (jetons, webhooks signés/falsifiés, API), 4 tests vocaux côté serveur (droits, états, déplacement, réconciliation), tous passés avec le détecteur de concurrence.
- **Test de bout en bout réel** : vrai `livekit-server` + deux Chromium sans interface avec micro simulé (Playwright, `make e2e-voice`, fichiers dans `test/e2e/`). Vérifié : connexion via le proxy `/lk`, audio reçu dans les deux sens (~18 Ko en 2,5 s chacun), participants connus du serveur, micro coupé visible en direct, retrait de `speak` appliqué par LiveKit, retrait de `connect` → éjection, aucune erreur.
- Image Docker démarrée : LiveKit lancé dans le conteneur, page de test et proxy fonctionnels.

**Guide de test du CP :** `docs/tests/jalon-4.md`.

**Limites connues :**
- Micro dans le navigateur seulement sur `localhost` tant que le HTTPS (P1) n'existe pas : tests sur une seule machine.
- Exposition sur Internet : ports 7881/tcp et 7882/udp à ouvrir (UPnP en P1) et `QUAREL_VOICE_PUBLIC_IP=auto`. En Docker, `--network host` recommandé.
- L'état vocal est en mémoire : après un redémarrage du seul serveur communautaire, il est reconstruit depuis LiveKit ; si les deux redémarrent, les clients doivent se reconnecter.
- Vidéo, partage d'écran et modération vocale (déplacer, rendre muet) : P1.
