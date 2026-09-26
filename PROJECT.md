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
| 4 | Développement backend (itératif, testé par le CP) | 🟡 Jalons 1 à 6 livrés, en test |
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
| 2026-09-23 | MP chiffrés avec **Olm/Megolm** (choix du CP, contre MLS) | Multi-appareils, validation par un appareil existant et transfert d'historique natifs ; bibliothèques auditée (vodozemac, Rust) et Go (goolm) disponibles. |
| 2026-09-23 | **Historique des MP sur les appareils** (choix du CP) : le serveur efface chaque message dès sa distribution | Privacy : aucun historique ni métadonnée durable sur le serveur. |
| 2026-09-23 | Clé maîtresse par compte certifiant les appareils validés, épinglée par les contacts au premier contact | Un serveur malveillant ne peut pas glisser un faux appareil pour lire les messages. |
| 2026-09-23 | Code de vérification des appareils sur 80 bits (16 caractères) | Trop long pour qu'un serveur fabrique un faux appareil au même code. |
| 2026-09-23 | MP et échanges de clés réservés aux amis | Anti-spam et limitation des métadonnées exposées. |
| 2026-09-23 | Licence **Apache-2.0** (le CP voulait MIT ou Apache) | Permissive comme MIT, avec en plus une protection contre les brevets ; licence de LiveKit. Contrepartie acceptée : un fork hébergé fermé reste possible (l'AGPL l'interdirait). |
| 2026-09-23 | Jalon 6 = bloc « mise en ligne » (HTTPS, UPnP, limites IP, phrase de récupération), dans cet ordre | Proposé et validé par le CP : ce qui empêche une mise en ligne réelle d'abord. |
| 2026-09-23 | Serveurs sans domaine : certificat auto-signé **lié à l'identité du serveur** (URI signée dans le certificat) | Sécurité équivalente à une autorité pour nos clients, sans nom de domaine ni tiers. |
| 2026-09-23 | Let's Encrypt (ACME TLS-ALPN-01) pour qui a un domaine ; propre certificat ou désactivation derrière proxy possibles | Couvrir tous les hébergeurs. |
| 2026-09-23 | UPnP actif par défaut en production, **désactivé en développement et en test** | Ne jamais modifier la box du CP sans son accord. |
| 2026-09-23 | Blocage anti-bruteforce par (compte, IP) + plafond par compte | Corrige le blocage malveillant signalé au jalon 1 sans affaiblir la protection. |
| 2026-09-23 | Phrase de récupération : 12 mots BIP-39 **français**, sauvegarde chiffrée (XChaCha20-Poly1305) stockée sur Identity | Standard éprouvé, lisible en français ; complète le choix « historique sur les appareils ». |
| 2026-09-24 | *(validé par le CP)* Aperçus de liens **actifs par défaut**, désactivables (`QUAREL_LINK_PREVIEWS=off`) | C'est le serveur (pas les membres) qui contacte les sites : l'IP des membres n'est pas exposée. Protection anti-SSRF : adresses privées/locales refusées après résolution DNS, ports 80/443, taille, durée et redirections plafonnées. Les images d'aperçu ne sont pas téléchargées par le serveur. |
| 2026-09-24 | *(validé par le CP)* Pièces jointes des serveurs communautaires : limite **fixée par l'hébergeur**, 25 Mo par défaut (`QUAREL_MAX_UPLOAD_MB`) | Matériel domestique modeste ; réglable par l'hébergeur. Type détecté sur le contenu, seuls images/audio/vidéo/texte s'affichent dans le navigateur, le reste est téléchargé (anti-XSS). |
| 2026-09-24 | Fils et salons d'annonces = salons textuels marqués (colonnes `thread`, `announcement`) | Changer la contrainte de type de la table aurait imposé de la reconstruire, ce qui supprime les messages en cascade. Un fil hérite des droits de son salon. |
| 2026-09-24 | Nouvelles permissions : `add_reactions`, `attach_files`, `stream` (données à `@everyone` par défaut), `moderate_members`, `view_audit_log`, `mute_members`, `deafen_members`, `move_members` | Préparées en une migration pour tout le bloc P1. |
| 2026-09-24 | Réglages de notification stockés par le serveur, appliqués par le client | Le serveur n'envoie pas de notifications push : il synchronise les réglages entre les appareils du membre. |
| 2026-09-24 | Vérification du téléphone : **webhook générique et OVHcloud SMS recommandés**, Twilio Verify en option (validé par le CP) | Twilio, américain, recevait les numéros des membres : contraire à la priorité privacy. Le webhook laisse l'hébergeur utiliser sa propre passerelle (son téléphone, un modem) sans tiers ; OVHcloud garde les données en Europe. Pour ces deux-là, c'est le serveur qui génère et vérifie les codes. |
| 2026-09-24 | Le serveur ne stocke qu'une **empreinte à clé** (HMAC) du numéro, conservée après un ban | Un banni ne peut pas revenir avec le même numéro sans que le numéro soit lisible dans la base. Limite assumée : l'hébergeur, qui détient la clé, pourrait retrouver un numéro par force brute (les numéros sont peu nombreux). |
| 2026-09-24 | *(validé par le CP)* Écran de règles : **seuls les nouveaux membres** doivent accepter (les présents sont dispensés quand les règles apparaissent) ; exigence du téléphone : **tous** les membres sauf propriétaire, administrateurs et bots | Les règles accompagnent l'arrivée ; le téléphone est une mesure de sécurité, qui n'a de sens que pour tous. |
| 2026-09-24 | Membres restreints (exclusion, règles, téléphone) = **lecture seule** (droit `view_channel` uniquement), avec un code d'erreur explicite | Une seule règle simple, appliquée partout où les permissions sont calculées (vocal compris). |
| 2026-09-24 | Bots = membres sans compte Identity, jeton `qb_…` propre au serveur, sans expiration, renouvelable | Même API que les clients ; aucune dépendance au service central. |
| 2026-09-24 | Journal d'audit conservé **90 jours** | Assez pour enquêter, sans historique indéfini (privacy). |
| 2026-09-24 | *(validé par le CP)* Micro et son coupés par la modération **persistants** jusqu'à levée (même après reconnexion) | Sinon il suffirait de se reconnecter pour contourner la sanction. |
| 2026-09-24 | Déplacement vocal = éjection de la salle + consigne `VOICE_MOVE` au client | Le déplacement natif de LiveKit n'est pas garanti hors de son offre cloud ; l'éjection, elle, est garantie par le serveur. |
| 2026-09-24 | Caméra et partage d'écran sous une seule permission `stream` | Simple à comprendre pour les administrateurs ; séparable plus tard sans casser l'API. |
| 2026-09-24 | *(validé par le CP)* Mot de passe oublié : la **2FA reste exigée**, et **tous les appareils sont déconnectés** | Sinon l'accès à la boîte mail suffirait à prendre le compte, et un intrus déjà connecté le resterait. Contrepartie : il faut revalider ses appareils (phrase de récupération). |
| 2026-09-24 | *(validé par le CP)* Suppression de compte **immédiate et définitive**, sans délai de grâce | « Effacement réel des données » ; confirmation par mot de passe, 2FA et saisie du pseudo. |
| 2026-09-24 | *(validé par le CP)* Changement de pseudo limité à **une fois par 24 h** ; l'ancien pseudo redevient libre | Limite l'usurpation en cascade ; les bans reposent sur l'identité, pas sur le pseudo. |
| 2026-09-24 | Profils et avatars **publics** (sans session) | Les clients des serveurs communautaires affichent les avatars des membres qui ne sont pas des amis. Seul ce que la personne choisit de publier y figure. |
| 2026-09-24 | Présence visible **des amis seulement** ; « invisible » = hors ligne | Privacy. Les serveurs communautaires ne reçoivent aucune présence du service central. |
| 2026-09-24 | Désactivation : outil **local** de l'opérateur (pas d'API réseau), raison et nom obligatoires, journalisée ; liste publique des identifiants désactivés lue par les serveurs toutes les 10 min | Aucune surface d'attaque réseau pour une action aussi grave ; traçabilité ; effet rapide sur les serveurs. |
| 2026-09-24 | Rotation de clé : l'ancienne clé privée est détruite, sa clé publique reste publiée le temps de vie des jetons | Pas de coupure pour les membres connectés, et une clé compromise ne peut plus rien signer. |
| 2026-09-24 | *(validé par le CP)* Groupes de MP : **10 membres max** par défaut, réglable par l'opérateur du service central (`QUAREL_DM_GROUP_MAX`), créés entre amis, chaque membre ajoute ses propres amis, seul le créateur retire | Proche de Discord ; les membres d'un groupe peuvent échanger des clés sans être amis entre eux. |
| 2026-09-24 | Un nouveau membre de groupe **ne lit pas les messages d'avant son arrivée** | Propriété de Megolm (clé partagée à l'index courant) : cohérent avec « historique sur les appareils ». |
| 2026-09-24 | Modification et suppression = événements chiffrés, appliqués par les clients (l'auteur seulement) | Le serveur ne distingue pas un message d'une modification : aucune métadonnée de plus. |
| 2026-09-24 | Fichiers des MP : **option D (choix du CP)** — pair à pair vers les appareils en ligne, copie serveur chiffrée seulement pour les autres, **effacée dès réception** (7 jours au plus, 25 Mo réglables) ; au-delà, pair à pair seulement ; un nouvel appareil récupère les anciens fichiers auprès des appareils des membres | Cohérent avec « historique sur les appareils » : le service central ne garde un fichier que le temps nécessaire. Les transferts de fichiers n'utilisent pas le relais TURN (bande passante), la copie serveur sert de secours. |
| 2026-09-24 | « En train d'écrire » et accusés de lecture relayés en direct, **jamais stockés**, désactivables séparément | Privacy ; qui les désactive ne partage plus rien (les siens restent synchronisés entre ses appareils). |
| 2026-09-24 | Signalisation des appels **chiffrée de bout en bout** (messages Olm), sans « trickle ICE » | Le service central ne voit ni les adresses IP ni les paramètres des appels (privacy) ; une seule offre et une seule réponse suffisent. |
| 2026-09-24 | *(validé par le CP)* Relais TURN **intégré au service Identity** (Pion, MIT), identifiants temporaires par compte, jamais vers des adresses privées | **Chaque service central héberge le relais de ses propres utilisateurs** (nous pour `identity.quarel.app`, l'opérateur pour une instance tierce). Installation en une commande, sans service tiers ; pas de relais ouvert ni de rebond vers le réseau de l'hébergeur. Coût : la bande passante des appels relayés est à la charge de l'instance centrale. |
| 2026-09-24 | Refus du relais = **réglage du client** | C'est l'appareil qui choisit ses chemins : le serveur n'a pas besoin de le savoir. |
| 2026-09-24 | Sauvegarde = une archive `.tar.gz` par service, **à chaud** (copie SQLite cohérente), clés comprises | Une seule commande, sans arrêt ; sans la clé, un serveur restauré aurait une autre identité. |
| 2026-09-24 | Restauration : vérification complète avant d'écrire, **jamais d'effacement** (anciennes données mises de côté), `--force` obligatoire par-dessus des données | Impossible de perdre des données par erreur de manipulation. |
| 2026-09-24 | Mises à jour : **copie automatique de la base avant chaque migration**, refus de démarrer une ancienne version sur une base récente | « Mise à jour sans perte » même sans sauvegarde manuelle. |

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

### Jalon 5 — Amis et messages privés chiffrés (livré le 2026-09-23)

**Livré :** amis (demande, acceptation automatique si croisée, refus, annulation, retrait), messages privés chiffrés de bout en bout avec Olm/Megolm, clé maîtresse de compte, premier appareil validé d'office, validation d'un nouvel appareil par code de vérification avec **transfert de la clé et de l'historique** (P1 avancé : cœur de la demande initiale), boîte aux lettres par appareil avec suppression à la distribution, accusés de distribution, temps réel côté Identity (passerelle WebSocket mutualisée avec le serveur communautaire dans `internal/realtime`), révocation d'appareil. Commandes `friends`, `friend-*`, `e2e`, `devices`, `device-approve`, `dm`, `dm-history`, `dm-sync`, `dm-listen`.

**Validation :**
- 3 nouveaux tests serveur (amis, clés et certificats, boîtes aux lettres/accusés/révocation), tous passés avec le détecteur de concurrence.
- **Scénario réel avec la vraie cryptographie** (`make e2e-dm`, 16 vérifications) : échange chiffré, appareil non validé bloqué, mauvais code refusé, historique complet transféré (y compris un message envoyé avant la validation), nouveaux messages lisibles sur les deux appareils, révocation, et **aucun texte des messages dans les fichiers de la base du serveur**.
- Bugs trouvés et corrigés par ces tests : interblocage SQLite (vérification faite pendant une transaction), écrasement de clés entre deux `quarelctl` sur le même profil (verrou ajouté), historique incomplet si l'appareil approbateur n'était pas synchronisé.

**Guide de test du CP :** `docs/tests/jalon-5.md`.

**Limites connues :**
- Perte de tous les appareils = perte de l'historique (phrase de récupération / sauvegarde chiffrée : P1).
- Les secrets envoyés par un appareil juste avant sa révocation sont ignorés par précaution s'ils n'ont pas encore été récupérés.
- Le serveur voit les métadonnées nécessaires à l'acheminement (qui écrit à qui, quand, taille) le temps de la distribution.
- Pas de rétention maximale des boîtes aux lettres d'appareils jamais reconnectés (à ajouter, P1).
- Clés du client de test stockées en clair dans le profil (le vrai client utilisera le trousseau du système).

**Licence (tranchée le 2026-09-23) :** Apache-2.0 (voir journal des décisions).

### Jalon 6 — Mise en ligne : HTTPS, UPnP, limites, récupération (livré le 2026-09-23)

**Livré :**
- **HTTPS** partout : serveur communautaire en HTTPS par défaut avec certificat auto-signé lié à son identité (vérifié par le client pendant la poignée de main TLS : lien d'invitation falsifié ou serveur remplacé → refus) ; Let's Encrypt, certificat fourni ou désactivation. Le vocal passe aussi en HTTPS (utilisable depuis une autre machine).
- **UPnP** : ouverture, renouvellement et fermeture automatiques des ports ; IP publique de la box annoncée pour la voix ; **diagnostic réseau** (`quarelctl network`) détectant l'absence d'UPnP et le double NAT/CGNAT.
- **Limitation de débit** sur les deux serveurs, et blocage anti-bruteforce par (compte, IP) : un attaquant ne bloque plus que lui-même.
- **Phrase de récupération** (12 mots français) et sauvegarde chiffrée automatique : clé du compte + historique restaurés sur un nouvel appareil après perte de tous les appareils.

**Validation :**
- Tests unitaires : limiteurs, IP derrière proxy, lien TLS (interception, lien copié, certificat sans lien), UPnP/diagnostic (box et STUN simulés), BIP-39 (vecteur officiel), chiffrement des sauvegardes, versions de sauvegarde, blocage par IP — tous passés avec le détecteur de concurrence.
- Scénarios de bout en bout : `make e2e-security` (18/18), `make e2e-acme` (certificat réellement obtenu auprès de Pebble, le serveur ACME de test de Let's Encrypt), `make e2e-dm` (16/16), `make e2e-voice` (10/10, désormais en HTTPS). Image Docker vérifiée en HTTPS.
- Bugs trouvés et corrigés : webhooks LiveKit perdus (route interne créée avant l'activation de la voix), requête STUN partie en IPv6 (adresse ignorée) — trouvé en interrogeant les vrais serveurs STUN ; Pebble n'envoie pas un en-tête que Let's Encrypt envoie (contourné pour le test par `acmeshim`, sans toucher au code du serveur).

**Non testé en conditions réelles :** UPnP sur une vraie box (logique testée avec une box simulée ; test réel proposé au CP car il ouvre des ports chez lui) ; ACME contre le vrai Let's Encrypt (nécessite un domaine public).

**Guide de test du CP :** `docs/tests/jalon-6.md`.

### P1 — Bloc 1 : messages et salons (livré le 2026-09-24)

Demande du CP : « on finit la P1 ! ». Découpage annoncé en 7 blocs (messages et salons ; modération et accès ; vocal avancé ; comptes ; MP avancés ; appels P2P ; exploitation du serveur), chacun testé, documenté et poussé.

**Livré :** réponses (citation + notification de l'auteur, désactivable), réactions emoji (20 différentes max par message), messages épinglés (50 max par salon), pièces jointes (envoi puis rattachement au message, nettoyage des envois abandonnés après 1 h, fichiers supprimés avec le message), aperçus de liens générés par le serveur (anti-SSRF), recherche plein texte insensible aux accents et à la casse (SQLite FTS5, limitée aux salons visibles), fils de discussion, salons d'annonces, « en train d'écrire », non-lus et compteur de mentions synchronisés entre appareils, réglages de notification, rôles affichés séparément (`hoist`).

**Validation :** tests d'intégration (réponses, réactions, épingles, pièces jointes y compris droits, taille, types dangereux et nettoyage, aperçus et refus SSRF, recherche et fuite de salons cachés, non-lus, fils, annonces, notifications) ; `make e2e-messages` (18/18) avec les vrais binaires ; détecteur de concurrence. Bugs trouvés et corrigés : mentions dupliquées au rechargement, ordre des réactions instable, sourdine « illimitée » impossible à encoder en JSON (date au-delà de l'an 9999).

**Guide de test du CP :** `docs/tests/p1-bloc-1.md`.

### P1 — Bloc 2 : modération et accès (livré le 2026-09-24)

**Livré :** exclusion temporaire (lecture seule jusqu'à 28 jours, sortie du vocal, fin automatique), journal d'audit (expulsions, bans, exclusions, rôles, salons, droits, réglages, suppressions de messages d'autrui, bots ; filtres et pagination), suppression en masse (par liste, par membre sur une période, ou au bannissement), écran de règles, vérification du téléphone (Twilio Verify ou mode développement, empreinte du numéro, pas de retour d'un banni avec le même numéro), bots (création, jeton, renouvellement, suppression), **documentation publique de l'API** (`site/src/content/docs/wiki/developper/api.md`) et bot d'exemple (`examples/pingbot`).

**Validation :** tests d'intégration (exclusion : droits, hiérarchie, administrateurs, vocal, fin automatique ; journal : contenu, filtres, pagination, expiration ; suppressions en masse et purge ; règles ; téléphone : codes, numéro déjà utilisé, numéro d'un banni, numéro d'un ancien membre ; Twilio contre un faux Twilio ; bots : jeton, hiérarchie, renouvellement, suppression) ; `make e2e-moderation` (24/24, avec le bot d'exemple réellement connecté en HTTPS auto-signé) ; détecteur de concurrence.

**Non testé en conditions réelles :** Twilio (nécessite un compte payant de l'hébergeur ; testé contre une imitation de son API).

**Guide de test du CP :** `docs/tests/p1-bloc-2.md`.

### P1 — Bloc 3 : vocal avancé (livré le 2026-09-24)

**Livré :** caméra et partage d'écran (avec le son de l'écran) dans les salons vocaux, soumis à la permission `stream` ; micro coupé et son coupé par la modération (persistants), déplacement vers un autre salon vocal, déconnexion ; état vocal enrichi (caméra, écran, modération) ; page de test vocal avec vidéo ; commandes `voice-mute`, `voice-deafen`, `voice-move`, `voice-kick`.

**Validation :** tests d'intégration (droits LiveKit selon `speak`/`stream`/modération, persistance, hiérarchie, déplacement et événement `VOICE_MOVE`, déconnexion, journal) ; `make e2e-voice` passe de 10 à **18 vérifications** avec deux navigateurs réels : vidéo de caméra et partage d'écran reçus, retrait de `stream` → LiveKit coupe caméra et écran, micro coupé par la modération, sourdine imposée (plus d'audio reçu) puis levée (audio de retour sans reconnexion), déplacement suivi par la page. Bugs trouvés et corrigés : tuiles vidéo et éléments audio restant affichés après un retrait par LiveKit.

**Guide de test du CP :** `docs/tests/p1-bloc-3.md`.

### P1 — Bloc 4 : comptes (livré le 2026-09-24)

**Livré :** mot de passe oublié (code par email, 2FA exigée, déconnexion de tous les appareils), changement de mot de passe, d'email (code sur la nouvelle adresse, ancienne prévenue) et de pseudo (identité inchangée), suppression définitive du compte, profil public (bio, avatar), blocage, présence entre amis, outil de l'opérateur (désactivation/réactivation journalisées, rotation de la clé de signature), liste publique des comptes désactivés appliquée par les serveurs communautaires, déploiement Docker Compose du service central.

**Validation :** tests d'intégration (réinitialisation avec et sans 2FA, codes à usage unique, fermeture des sessions, changements d'email et de pseudo, suppression et absence de restes en base, avatar : types refusés, taille, en-têtes, blocage, présence en temps réel, désactivation, rotation de clé : ancien jeton encore valide puis clé retirée) ; côté serveur communautaire : liste des comptes désactivés (session fermée, connexion refusée, retour après réactivation) ; `make e2e-accounts` (30/30) avec les vrais binaires et l'outil `quarel-identity admin` ; déploiement Compose testé (santé, clés publiées, rotation de clé puis redémarrage : deux clés publiées).

**Non testé en conditions réelles :** Let's Encrypt avec le vrai domaine `identity.quarel.app` (nécessite le DNS) ; envoi d'emails par un vrai serveur SMTP.

**Guide de test du CP :** `docs/tests/p1-bloc-4.md` ; déploiement : `site/src/content/docs/wiki/heberger/service-identite.md`.

### P1 — Bloc 5 : messages privés avancés (livré le 2026-09-24)

**Livré :** groupes de MP (création entre amis, ajout, retrait, départ, renommage, transmission du groupe), modification et suppression de ses messages (chiffrées, appliquées chez tout le monde), fichiers chiffrés de bout en bout, « en train d'écrire » et accusés de lecture désactivables. Les conversations existantes sont migrées sans perte (accusés de distribution en attente compris).

**Validation :** tests d'intégration (migration d'une base contenant des MP, groupes et règles d'ajout/retrait, échange de clés entre co-membres non amis, frappe et lecture avec et sans partage, fichiers : droits, taille, expiration) ; test du client : une modification/suppression forgée par un autre membre est ignorée ; `make e2e-dm-groups` (22/22 : lecture par un membre non ami, nouveau membre sans l'historique antérieur, membre retiré coupé, nouvelle clé, modification/suppression, fichier déchiffré à l'identique et illisible côté serveur, frappe et accusés en direct puis désactivés, aucun clair en base) ; `make e2e-dm` inchangé (16/16). Bug trouvé et corrigé : membres arrivés dans la même seconde classés au hasard, donc groupe transmis à un membre quelconque au départ du créateur (désormais : ordre d'arrivée).

**Guide de test du CP :** `docs/tests/p1-bloc-5.md`.

### P1 — Bloc 6 : appels pair à pair (livré le 2026-09-24)

**Livré :** appels audio 1-à-1 entre amis en WebRTC pair à pair, signalisation chiffrée de bout en bout par les messages Olm existants, relais TURN de secours intégré au service Identity (identifiants temporaires, refus des adresses privées), refus du relais par l'utilisateur ; ports et variables du relais dans l'image Docker et le Compose.

**Validation :** tests d'intégration du relais (relaie réellement entre deux sockets, refuse des identifiants forgés, **refuse de relayer vers une adresse locale** en configuration réelle, pas de relais proposé quand il est coupé) ; `make e2e-calls` (10/10) avec de vrais appels WebRTC entre deux `quarelctl` : audio reçu des deux côtés en direct, puis **forcé par le relais** (chemin TURN confirmé), relais refusé, appels réservés aux amis, aucune trace de la signalisation en clair dans la base ; image Docker démarrée avec le relais.

**Non testé en conditions réelles :** appel entre deux réseaux domestiques distincts derrière des NAT (nécessite deux accès Internet ; la logique direct/relais est la même que testée en local) ; client graphique (le futur client utilisera la même signalisation).

**Guide de test du CP :** `docs/tests/p1-bloc-6.md`.

### P1 — Bloc 7 : exploitation des serveurs (livré le 2026-09-24)

**Livré :** commandes `backup`, `restore` et `version` pour le serveur communautaire et le service Identity (à chaud, vérifiées, sans écrasement), copie de la base avant chaque mise à jour de schéma, refus des retours arrière dangereux, guide de l'hébergeur (`site/src/content/docs/wiki/heberger/serveur-communautaire.md`) et procédures Docker.

**Validation :** tests unitaires (aller-retour sauvegarde/restauration avec base ouverte, refus sans `--force`, anciennes données conservées, mauvais type, version trop récente, chemins hors dossier et archive invalide refusés, clé absente ; copie avant migration et refus d'une base plus récente) ; `make e2e-ops` (15/15) avec les vrais binaires en marche ; procédure Docker vérifiée (sauvegarde depuis un conteneur en marche, restauration dans un volume neuf, **même identité de serveur**). Bug trouvé et corrigé : l'utilisateur non-root du conteneur ne pouvait pas lire une sauvegarde de l'hôte → restauration depuis l'entrée standard.

**Guide de test du CP :** `docs/tests/p1-bloc-7.md`.

### Bilan : P1 serveur terminée (2026-09-24)

Les sept blocs annoncés sont livrés, testés et poussés : **toute la P1 côté serveurs est faite**. Reste en P1 le **client graphique** (C), prévu pour le dernier jalon comme convenu. Décisions marquées *(à valider par le CP)* ci-dessus : à passer en revue ensemble.

### Retour du CP sur la vérification du téléphone (2026-09-24)

Question du CP : « pourquoi Twilio ? ». Réponse : choix par défaut (service qui gère tout), mais fournisseur américain qui reçoit les numéros. Décision du CP : **Twilio reste une option ; webhook générique et OVHcloud SMS deviennent les recommandations.** Livré : fournisseurs `webhook` (requête signée HMAC vers la passerelle de l'hébergeur) et `ovh` (API OVHcloud signée), codes gérés par le serveur ; tests contre un faux OVH (signature recalculée comme chez OVH, décalage d'horloge, numéro refusé, mauvaise clé) et un faux webhook (signature, panne de passerelle) ; `make e2e-moderation` (24/24) passe désormais par le vrai fournisseur webhook avec une passerelle de test. Non testé contre le vrai OVHcloud (compte SMS nécessaire).

### Revue des décisions par le CP (2026-09-24)

Validées : aperçus de liens actifs par défaut ; règles pour les seuls nouveaux membres ; mot de passe oublié (2FA exigée, tous les appareils déconnectés) ; suppression de compte immédiate ; pseudo une fois par 24 h ; groupes de 10 membres, **rendus réglables** sur le service central (`QUAREL_DM_GROUP_MAX`). Précisé : sur un serveur communautaire, la limite des fichiers est fixée par l'hébergeur (`QUAREL_MAX_UPLOAD_MB`, 25 Mo par défaut). **Ouverts** : partage de fichiers de bout en bout (où ils transitent, combien de temps, quelle limite) et qui héberge le relais d'appels.
| 2026-09-24 | **Chaque service central reste une bulle** (décision du CP) : pas d'amis, de MP ni d'appels entre services centraux pour l'instant | Simplicité et isolement ; à rouvrir plus tard si besoin (petite fédération limitée aux amis). |

### Suite de la revue (2026-09-24)

Le CP choisit l'**option D** pour les fichiers des MP, garde **un relais par service central**, et décide que **chaque service central reste une bulle**. Livré pour D : offre et transfert pair à pair (canal de données WebRTC, signalisation Olm), copie serveur uniquement pour les appareils non servis, effacée dès le dernier accusé (table `conv_file_pending`, migration 7, plafond 7 jours), récupération différée auprès de n'importe quel appareil d'un membre. Tests : copie serveur par appareil (création, accusés, effacement, appareil révoqué, plafond) ; `make e2e-dm-groups` passe à **33/33** (envoi direct à l'appareil en ligne, copie pour l'absent effacée dès réception, fichier au-delà de la limite transmis seulement en direct puis récupéré plus tard auprès d'un autre membre, jamais stocké sur le serveur).
| 2026-09-24 | Client graphique : **Electron + interface web** (choix du CP, contre Tauri recommandé) ; chiffrement avec vodozemac en WebAssembly ; même interface pour le client web | Voix, vidéo et WebRTC identiques sur toutes les plateformes (Chromium embarqué), technologie de Discord. Contrepartie acceptée : application plus lourde (~150 Mo). Maquettes des écrans principaux proposées au CP avant de commencer. |

### Client graphique — étape 1 : comptes (2026-09-24)

Le CP **valide les maquettes sans modification** (thème sombre, accent vert d'eau, Manrope / Space Grotesk, disposition proche de Discord). Ordre des étapes du client : 1. connexion et comptes, 2. serveurs, salons et messages, 3. vocal et vidéo, 4. amis et MP chiffrés, 5. appels, 6. paramètres, 7. version web.

Livré pour l'étape 1 : connexion (avec double authentification et codes de secours), création de compte, vérification de l'email, mot de passe oublié, choix du service d'identité, session gardée entre deux lancements, écran d'accueil, paramètres « Compte » et « Appareils » (déconnexion d'un autre appareil), déconnexion. Test de bout en bout `make e2e-client` : la vraie application Electron contre un vrai service Identity.

| Date | Décision | Raison |
|---|---|---|
| 2026-09-24 | Interface : **React + TypeScript**, construite par **Vite** ; processus Electron construits par esbuild ; polices embarquées (aucun appel à Google Fonts) | Écosystème le plus répandu (contributions), composants réutilisés tels quels par le client web ; pas de dépendance réseau pour afficher l'interface. |
| 2026-09-24 | Sécurité d'Electron : page isolée (`contextIsolation`, `sandbox`, pas de Node), seul pont = secrets et infos de l'appareil ; liens externes ouverts dans le navigateur ; politique de sécurité du contenu stricte | Un message piégé ne doit jamais pouvoir exécuter du code sur la machine. |
| 2026-09-24 | Secrets (session, clé d'appareil) chiffrés par le **trousseau du système** (`safeStorage`) ; sous Linux sans trousseau, simple obscurcissement (signalé à l'interface) | Pas de mot de passe en clair sur le disque ; l'application reste utilisable partout. |
| 2026-09-24 | **Une clé d'appareil par service d'identité** | Deux comptes sur deux services ne peuvent pas être reliés par leur clé d'appareil. |
| 2026-09-24 | **CORS ouvert** (`Access-Control-Allow-Origin: *`) sur les deux services | Nécessaire au client web et à l'interface d'Electron ; sans risque car l'authentification passe par un jeton explicite, jamais par un cookie. Le proxy vocal `/lk/` garde les en-têtes de LiveKit. |

### Client graphique — étape 2 : serveurs, salons, messages (2026-09-24)

Livré : rejoindre un serveur par lien d'invitation (aperçu « Serveur authentifié »), règles et téléphone à l'arrivée, liste des serveurs avec non-lus et mentions, salons par catégorie (fils et vocaux affichés), messages en direct (mentions, réponses, réactions, modification, suppression, fichiers joints, aperçus de liens), « en train d'écrire », lecture, inviter, quitter, expulsion ou bannissement signalés. `make e2e-client` : 2 scénarios (comptes ; serveurs et messages face à une autre personne sur `quarelctl`).

| Date | Décision | Raison |
|---|---|---|
| 2026-09-24 | Certificats auto-signés : **contrôle préalable par le processus principal d'Electron**, puis acceptation par Chromium du seul certificat lié à l'identité du lien | Chromium garde en mémoire tout refus de certificat jusqu'au redémarrage : sans ce contrôle, un lien erroné bloquerait le bon. Aucun serveur n'est contacté par l'interface avant d'avoir prouvé son identité. |
| 2026-09-24 | **Client web : serveurs à certificat reconnu seulement** (nom de domaine, Let's Encrypt) | Un navigateur ne sait pas vérifier la liaison d'un certificat auto-signé. Les serveurs domestiques sans nom de domaine se rejoignent avec l'application. À reprendre à l'étape « version web » (options possibles : nom de domaine automatique, relais). |
| 2026-09-24 | Aperçus de liens affichés **sans leur image** | Le serveur ne télécharge jamais l'image (anti-SSRF) ; la charger depuis le client révélerait l'adresse IP de chaque lecteur au site cité. |

### Installation légère : interfaces d'administration et Windows (2026-09-24)

Le CP rappelle l'objectif de départ : **déploiement et usage les plus légers possible**, sources disponibles pour tout reconstruire soi-même. Constat : Docker existait pour les deux serveurs, mais configuré seulement par variables d'environnement ; le `.exe` n'était prévu qu'en P2 pour le serveur communautaire, sans interface. Le CP fait passer ce chantier **avant la suite du client**, puis la mise en ligne des deux serveurs de test sur `quarel.app`.

| Date | Décision | Raison |
|---|---|---|
| 2026-09-24 | *(choix du CP)* Les **deux serveurs** (identité et communautaire) ont une **version Windows** et une **version Docker**, chacune avec une **interface de configuration** | Installation et administration sans ligne de commande. |
| 2026-09-24 | *(choix du CP)* Windows : **icône près de l'horloge + interface dans le navigateur** ; **installateur** (démarrage avec Windows, désinstallation propre) | Même interface web que Docker : un seul outil à maintenir, `.exe` léger. |
| 2026-09-24 | *(choix du CP)* Docker : interface d'administration **accessible depuis le réseau local, protégée par un mot de passe** choisi au premier lancement ; jamais ouverte vers Internet par l'UPnP | Serveurs souvent sans écran. |
| 2026-09-24 | Réglages dans un **fichier du dossier de données** (`settings.json`) modifiable par l'interface ; **les variables d'environnement restent prioritaires** (affichées comme verrouillées) | Compatible avec les déploiements existants et Docker Compose. |
| 2026-09-24 | Le programme **démarre toujours son interface d'administration**, même avec une configuration invalide, et **redémarre le service** en interne pour appliquer un changement | On peut toujours réparer depuis l'interface, sans ligne de commande. |
| 2026-09-24 | Interface d'administration en **HTML/JS simple intégrés au binaire Go** (pas de Node) | Reconstruire les serveurs ne demande que Go. |

Ordre : **A.** interfaces d'administration (les deux serveurs, Docker) → **B.** Windows (icône, installateur, LiveKit pour Windows) → **C.** mise en ligne sur `quarel.app` avec le CP.

**Bloc A livré (2026-09-24)** : réglages dans `settings.json` (variables d'environnement prioritaires), superviseur qui redémarre le service à chaud et le garde réparable depuis la page, page d'administration commune (premier mot de passe avec code d'installation à distance, tableau de bord, réglages validés avant enregistrement, sauvegarde et restauration, journal, mot de passe) ; communautaire : lien propriétaire reconnu par l'application, renommage ; Identity : comptes (désactivation/réactivation avec raison), journal de l'opérateur, rotation de clé. Messages de configuration traduits en français. Docker : port 8091 (communautaire) / 8081 (Identity, publié sur 127.0.0.1 seulement), `deploy/server/compose.yaml`. Tests : `internal/adminui` (mot de passe, code d'installation, anti-CSRF, réseau local seulement, réglages refusés et retour arrière, secrets, superviseur) ; tous les scénarios de bout en bout existants inchangés.

| Date | Décision | Raison |
|---|---|---|
| 2026-09-24 | Page d'administration **refusée à toute adresse Internet** (boucle locale, réseau privé, lien local seulement), en plus du mot de passe | « Réseau local » choisi par le CP ; protège aussi un serveur loué dont le port serait publié par erreur. Levée possible (`QUAREL_ADMIN_PUBLIC=1`). |
| 2026-09-24 | Premier mot de passe : libre depuis la machine elle-même, **code d'installation** (affiché dans le journal) depuis un autre appareil | Sinon, n'importe qui sur le réseau local pourrait choisir le mot de passe avant l'hébergeur. |
| 2026-09-24 | Page d'administration en **HTTP** sur le réseau local | Pas d'avertissement de certificat pour l'hébergeur. Risque accepté : le mot de passe circule en clair sur le réseau local (Wi-Fi chiffré en pratique). À revoir si le CP le souhaite. |
| 2026-09-24 | Identity en Docker : page publiée sur **127.0.0.1** seulement, accès par tunnel SSH | Un service d'identité tourne en général sur un serveur loué, sans réseau local. |
| 2026-09-24 | **Lien propriétaire** `quarel://…?sid=…&claim=1` affiché dans la page | L'hébergeur devient propriétaire depuis l'application en collant un lien, sans ligne de commande. |

**Bloc B livré (2026-09-24)** : version Windows des deux serveurs. Icône près de l'horloge (menu : ouvrir l'administration, état, lancer au démarrage de Windows, quitter), instance unique, page ouverte au premier lancement, journal dans un fichier, LiveKit pour Windows inclus et lancé sans console. Installateurs NSIS construits depuis Linux (`make windows`) : Program Files, pare-feu, démarrage avec la session, désinstallation qui garde les données. Vérifié sous Wine (installation, démarrage, administration, service HTTPS) ; **à vérifier sur un vrai Windows** : icône et menu, vocal, pare-feu, avertissement SmartScreen. Le lien propriétaire est testé dans l'application (`make e2e-client`, 3 scénarios).

| Date | Décision | Raison |
|---|---|---|
| 2026-09-24 | Windows : installation dans Program Files **avec droits administrateur** (pare-feu), programme lancé ensuite **en tant qu'utilisateur**, données dans `%LOCALAPPDATA%` | Le pare-feu doit laisser entrer les membres ; aucune élévation au quotidien. |
| 2026-09-24 | Windows : page d'administration sur **cette machine seulement** (127.0.0.1) | Un PC Windows a un écran : l'icône ouvre la page ; rien d'exposé sur le réseau. |
| 2026-09-24 | Installateurs **non signés** pour l'instant | Un certificat de signature de code est payant (≈ 100-300 €/an) ; SmartScreen avertit en attendant. **À décider par le CP** avant une diffusion publique. |
| 2026-09-24 | Couleur d'icône par service : **vert d'eau** (communautaire), **violet** (identité) | Distinguer les deux s'ils tournent sur la même machine. |

**Livré (2026-09-24)** : inscriptions ouvertes / sur invitation / fermées, domaines d'email, plafond de comptes, invitations de l'opérateur et des utilisateurs (quota) ; liste noire de serveurs (appliquée par l'application et par le service) ; mode « serveurs approuvés » avec jetons chiffrés HPKE pour le serveur, demande d'approbation signée depuis la page du serveur communautaire, approbation depuis celle du service d'identité. Tests : `internal/identity` (règles d'inscription, invitations et quota, liste noire, approbation, jeton chiffré lisible par le seul serveur approuvé), `internal/community` (jeton chiffré accepté, jeton d'un autre serveur refusé), `pkg/idtoken` ; `make e2e-client` : scénario complet avec les vraies pages d'administration (4 scénarios). `deploy/identity/.env.example` : `QUAREL_REGISTRATION=invite` pour `identity.quarel.app`.

### Mise en ligne de l'instance de test quarel.app (2026-09-24)

Choix du CP : hébergement auto-hébergé par l'équipe, DNS et HTTPS par **Cloudflare**, **pas encore de fournisseur d'emails** (voir `docs/deploiement-quarel-app.md`).

| Date | Décision | Raison |
|---|---|---|
| 2026-09-24 | Web derrière Cloudflare et un proxy inverse → services en HTTP ; HTTPS par Cloudflare | Infrastructure existante ; certificat reconnu partout (le client web fonctionne aussi). |
| 2026-09-24 | Vocal en direct : **seuls 7881/tcp et 7882/udp** ouverts par UPnP (accord du CP) ; derrière un proxy, l'UPnP n'ouvre plus le port web (correctif `5f53055`) | Cloudflare ne transporte pas l'UDP ; ouvrir le 443 par UPnP aurait pris le port du proxy inverse. |
| 2026-09-24 | Relais d'appels (TURN) **désactivé** pour l'instant | UDP hors Cloudflare ; à décider avec l'étape « appels » du client. |
| 2026-09-24 | Emails **plus tard** : codes lisibles dans le journal pendant la phase sur invitation | Choix du CP. `quarel.app` a déjà des MX chez OVH : piste possible pour l'envoi. |
| 2026-09-24 | Cloudflare voit le trafic web (messages des salons) | Accepté pour la phase de test ; les MP restent chiffrés de bout en bout. À rediscuter pour la production. |

### Client graphique — étape 3 : vocal et vidéo (2026-09-24)

Livré : rejoindre un salon vocal d'un clic, participants sous le salon (micro, son, caméra, écran, modération, qui parle), vue du salon (tuiles, caméras, partages d'écran), micro, sourdine, caméra, partage d'écran (sélecteur d'écrans et de fenêtres dans l'application de bureau), barre « Vocal connecté » visible partout, suivi des décisions de la modération (micro/son coupés, déplacement, déconnexion). Fonctionne dans l'application et dans le client web (`app.quarel.app`). Test de bout en bout avec un vrai LiveKit. **Non vérifié automatiquement** : l'indicateur « en train de parler » (le bip du faux micro de Chromium ne déclenche pas la détection de LiveKit) et le vocal à travers Internet (à tester par le CP sur `test.quarel.app`).

### Client graphique — étape 4 : amis et messages privés chiffrés (en cours)

| Date | Décision | Raison |
|---|---|---|
| 2026-09-24 | Chiffrement du client : **vodozemac officiel compilé nous-mêmes en WebAssembly** (`client/crypto/`, fine couche wasm-bindgen, 480 Ko), module généré versionné dans `client/src/crypto/wasm/` et reconstructible (`client/crypto/build.sh`, Rust requis seulement pour le reconstruire) | Aucune version navigateur de vodozemac n'est à la fois officielle et utilisable pour notre protocole : bindings npm tiers d'un seul mainteneur (risque de chaîne d'approvisionnement), libolm abandonné (failles de synchronisation connues), `matrix-sdk-crypto-wasm` lié au protocole Matrix complet. |
| 2026-09-24 | Formats « version 1 » (ceux de libolm) partout | Compatibilité exacte avec le client de test (goolm) : **vérifiée** par `make client-interop` (signatures, sessions Olm et messages Megolm dans les deux sens, clés exportées). |

Découpage : **4a** amis, conversations et groupes chiffrés ; **4b** validation des appareils, transfert d'historique, phrase de récupération ; **4c** fichiers chiffrés.

**4a livré (2026-09-24)** : amis (demandes, acceptation, retrait, présence), conversations directes et groupes chiffrés de bout en bout, envoi / réception en direct, distribué, vu, « … écrit », modification et suppression, historique gardé sur l'appareil (coffre chiffré par le trousseau). **Interopérabilité réelle vérifiée** : l'application et `quarelctl` échangent des messages dans les deux sens, groupes compris (`e2e/dm.spec.ts`). Correctif au passage : l'application de bureau sert son interface par `app://quarel/` au lieu de `file://`.

**4b livré (2026-09-24)** : validation d'un appareil en tapant son code sur un appareil validé (clé du compte, clé de sauvegarde et historique transmis par Olm), phrase de récupération de 12 mots créée et utilisée dans l'application, sauvegarde chiffrée automatique ; vérifié dans les deux sens face à `quarelctl` (`e2e/devices.spec.ts`).

**4c livré (2026-09-24)** : fichiers chiffrés dans les conversations privées, en direct entre appareils en ligne (WebRTC, STUN seulement) et copie serveur pour les autres, effacée dès réception ; vérifié face à `quarelctl` (`e2e/dmfiles.spec.ts`).

### Étape 5 : appels entre amis (2026-09-24)

| Date | Décision | Raison |
|---|---|---|
| 2026-09-24 | **Relais de secours activé sur `identity.quarel.app`** (choix du CP) | Sans relais, les appels entre deux réseaux dont les box sont strictes échouent. Le flux relayé reste chiffré de bout en bout ; chaque utilisateur peut refuser le relais dans ses paramètres. |
| 2026-09-24 | Ports du relais (3478/udp, 49160-49200/udp) ouverts par **UPnP** par le service d'identité lui-même, IP publique **trouvée automatiquement** (`QUAREL_TURN_PUBLIC_IP=auto`) et suivie | Même logique d'installation légère que le serveur communautaire (box domestique, IP qui peut changer) ; accord du CP pour ouvrir ces ports sur sa box. |
| 2026-09-24 | Appels en tête à tête, audio + caméra (entre applications), sans renégociation | La caméra s'allume par `replaceTrack` sur un émetteur vidéo prévu dès l'invitation ; le protocole reste celui de `quarelctl` (invite/answer/reject/hangup), qui ne fait que de l'audio. |

**5 livré (2026-09-24)** : appeler un ami depuis sa conversation, sonnerie sur tous ses appareils, réponse / refus, micro, caméra, chemin affiché (direct, réseau local, relais) ; vérifié face à `quarelctl` dans les deux sens, par le relais seul, et entre deux applications avec vidéo (`e2e/calls.spec.ts`). Au passage : les scripts de test du serveur utilisent les ports 28080/28090 (18080/18090 sont pris par le déploiement de test).

### Étape 7 : version web (2026-09-25)

| Date | Décision | Raison |
|---|---|---|
| 2026-09-25 | **La version web n'accepte que les serveurs à certificat reconnu** (Let's Encrypt intégré, ou derrière un proxy HTTPS) ; les serveurs auto-signés restent accessibles depuis l'application de bureau (choix du CP) | Un navigateur ne peut pas vérifier le certificat lié à l'identité du serveur. Pas de service central à héberger (écarté : noms `*.quarel.direct` à la Plex). L'application web explique le refus, la page d'administration du serveur prévient l'hébergeur. |
| 2026-09-25 | Périmètre de l'étape 7 (choix du CP) : **liens d'invitation web**, **stockage chiffré dans le navigateur**, **application installable (PWA)**, **affichage téléphone** | Rendre la version web utilisable au quotidien, y compris sur mobile. |

| 2026-09-25 | **La liste des serveurs rejoints suit le compte sur tous ses appareils**, chiffrée de bout en bout (messages Olm entre ses appareils, historique transmis à la validation, sauvegarde de la phrase de récupération) | Constat du CP : un nouvel appareil validé retrouvait ses messages privés mais pas ses serveurs. Le service d'identité ne voit toujours pas quels serveurs on fréquente. |

### Mise à jour automatique de l'application de bureau (2026-09-26)

| Date | Décision | Raison |
|---|---|---|
| 2026-09-26 | Mises à jour servies par **`app.quarel.app/updates`** (choix du CP), moteur `electron-updater` (MIT, flux « generic ») | Hébergement déjà en place, pas de dépendance à GitHub ni à un service tiers. |
| 2026-09-26 | **Chaque version est signée par une clé Ed25519 de publication** (clé publique dans l'application, `electron/releasesig.ts`) : l'application ne télécharge que ce que la signature couvre et n'installe qu'un fichier dont l'empreinte SHA-512 est celle signée | Les installateurs ne sont pas encore signés (Authenticode) : sans cela, quiconque prendrait la main sur l'hébergement web pourrait pousser du code sur toutes les machines. La clé privée reste hors du serveur web (`~/.config/quarel-release/`). **La perdre = plus de mise à jour automatique possible** (il faudrait réinstaller à la main une version portant une nouvelle clé) : à sauvegarder hors de la machine. |
| 2026-09-26 | Windows (installateur NSIS) et Linux AppImage se mettent à jour seuls (téléchargement en fond, bandeau « Redémarrer », sinon installation à la fermeture) ; le paquet **.deb** affiche seulement qu'une version existe, avec le lien | Un `.deb` s'installe avec les droits administrateur : l'application ne doit pas le faire elle-même. |

### Site quarel.app, wiki et MCP de la documentation (2026-09-26)

| Date | Décision | Raison |
|---|---|---|
| 2026-09-26 | Site et wiki avec **Astro + Starlight** (choix du CP), sources dans le dépôt (`site/`), servis par le nginx existant | Page de présentation soignée et wiki Markdown avec recherche dans un même site statique ; la documentation suit le code dans les mêmes commits. |
| 2026-09-26 | **Français d'abord, anglais ensuite** ; page d'accueil présentée comme **version de test** (choix du CP) | Cohérent avec l'application et la phase actuelle (inscriptions sur invitation). |
| 2026-09-26 | Le wiki devient **la** documentation publique (guides d'hébergement et API déplacés depuis `docs/`) | Une seule source, publiée. |
| 2026-09-26 | **MCP de la documentation** (choix du CP) sur `quarel.app/mcp` : serveur Go du dépôt, lecture seule, sans compte ; plus `llms.txt` / `llms-full.txt` | Les assistants IA répondent aux questions d'hébergement et d'API à partir de la documentation à jour, en citant les pages. |

### Ouverture publique du dépôt (2026-09-26)

| Date | Décision | Raison |
|---|---|---|
| 2026-09-26 | **Dépôt `anlekg/quarel` public** (choix du CP), après vérification de l'historique (gitleaks, recherche de l'IP publique, de l'email personnel, du mot de passe SMTP : rien), des dépendances (govulncheck, npm audit) et relecture ciblée du code sensible | Projet open source : le code doit pouvoir être lu, audité et amélioré. |
| 2026-09-26 | Documents de travail (`CLAUDE.md`, `PROJECT.md`, `PLAN.md`, guides de test) **gardés publics**, détails de l'infrastructure personnelle retirés ; l'historique n'est pas réécrit (choix du CP) | Transparence des décisions ; pas de secret dans l'historique. |
| 2026-09-26 | Tests GitHub (5 jobs) exigés sur `main`, historique non réécrivable ; signalement privé des failles, alertes et correctifs Dependabot, détection de secrets avec blocage à l'envoi | Garder `main` sain et traiter les failles en privé. |
| 2026-09-26 | Essai à plusieurs **après** l'ouverture (choix du CP) | `docs/tests/essai-a-plusieurs.md`. |

### Audit du dépôt et corrections (2026-09-26)

Audit demandé par le CP (fonctions, sécurité, technologies), puis **correction de tous les points à sa demande**.

| Date | Décision | Raison |
|---|---|---|
| 2026-09-26 | **Preuve de connexion v2** : elle signe aussi **l'adresse contactée** et **comment elle a été vérifiée** (`binding` : certificat lié à l'identité du serveur ; `authority` : certificat ordinaire). Un serveur refuse une preuve `authority` pour un nom qui n'est pas le sien (`QUAREL_TLS_HOSTS`, domaine Let's Encrypt, noms du certificat fourni, boucle locale) et une preuve `binding` s'il n'a pas de certificat lié. Anciennes preuves (v1) encore acceptées pour les anciennes versions de l'application | Faille de l'audit : un serveur malveillant muni d'un certificat Let's Encrypt pouvait relayer la connexion d'une personne vers le serveur dont il prétendait être (lien d'invitation au `sid` d'un autre) et s'y connecter à sa place. |
| 2026-09-26 | Application de bureau : **un nom d'hôte lié à un serveur (certificat lié vérifié) n'accepte plus que ce serveur**, jamais un certificat ordinaire ; une seule identité par nom d'hôte ; libéré quand plus aucun serveur rejoint ne l'utilise | Sans cela, un serveur malveillant pouvait montrer le vrai certificat à la vérification puis le sien à Chromium. Conséquence : deux serveurs auto-signés sur le même nom d'hôte (ports différents) ne sont plus possibles ; utiliser des noms différents. |
| 2026-09-26 | Derrière un proxy HTTPS (`QUAREL_TLS=off`), **`QUAREL_TLS_HOSTS` devient obligatoire** pour les connexions (avertissement sur le tableau de bord) | Le serveur doit connaître ses noms pour reconnaître une connexion relayée. |
| 2026-09-26 | Page d'administration : **noms d'hôte inconnus refusés** (IP, `localhost`, nom de la machine, `QUAREL_ADMIN_HOSTS`) ; **HTTPS obligatoire depuis une autre machine** (même port, certificat auto-signé dont l'empreinte est affichée dans le journal) ; HTTP seulement depuis la machine elle-même ; une requête relayée par un proxy local n'a plus les droits de la machine ; `QUAREL_ADMIN_PUBLIC`, `QUAREL_ADMIN_TLS`, `QUAREL_ADMIN_HOSTS` : variables d'environnement seulement | Faille de l'audit (« DNS rebinding » : un site visité depuis la machine pouvait choisir le mot de passe au premier lancement et télécharger les clés) ; mot de passe en clair sur le réseau local. La page ne peut pas élargir elle-même son exposition. |
| 2026-09-26 | **Défis de connexion sans état** (nonce = aléa + expiration + HMAC d'une clé tirée au démarrage) ; seuls les nonces consommés par une connexion valide sont retenus | L'ancienne réserve de 10 000 défis en mémoire pouvait être remplie depuis quelques centaines d'adresses, bloquant toutes les connexions. |
