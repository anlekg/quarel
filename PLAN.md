# Quarel — Plan des fonctionnalités

> **Validé par le CP le 2026-09-23.** Priorités : **P0** = MVP (indispensable pour un premier test bout à bout), **P1** = V1 publique, **P2** = plus tard.
> Le front-end (client) est traité en dernier ; en attendant, tout se teste via l'API (scripts, `curl`, client de test en ligne de commande).

---

## A. Service central « Identity »

### A1. Comptes
- [x] **P0** Inscription : email, pseudo, mot de passe (hash Argon2id)
- [x] **P0** Vérification de l'email (lien ou code)
- [x] **P0** Connexion / déconnexion, sessions par appareil
- [x] **P0** 2FA TOTP (application d'authentification) + codes de secours
- [x] **P1** Mot de passe oublié (via email)
- [x] **P1** Changement d'email, de pseudo, de mot de passe
- [x] **P1** Suppression de compte par l'utilisateur (effacement réel des données)
- [x] **P1** Anti-bruteforce : blocage du compte après 15 échecs de connexion par heure *(avancé au jalon 1 à la demande du CP)*
- [x] **P1** Limitation de débit par IP (anti-spam d'inscriptions, anti-blocage malveillant de comptes)
- [ ] **P2** 2FA par clé matérielle / passkey (WebAuthn)

### A2. Identité portable
- [x] **P0** Identifiant unique au format `pseudo@domaine-du-service`
- [x] **P0** Émission d'un jeton d'identité signé (ID, pseudo, clé publique, expiration)
- [x] **P0** Publication de la clé publique de signature du service (vérification hors ligne par les serveurs)
- [x] **P1** Rotation des clés de signature du service
- [x] **P1** Liste des comptes désactivés, consultable par les serveurs (réquisition judiciaire)
- [x] **P1** Profil public minimal : pseudo, avatar, bio

### A3. Appareils et clés E2E
- [x] **P0** Enregistrement du premier appareil (génération des clés côté client)
- [x] **P0** Ajout d'un appareil validé depuis un appareil existant (code de vérification à comparer ; QR code avec le vrai client)
- [x] **P0** Liste et révocation des appareils
- [x] **P1** Phrase de récupération (restaure les clés si tous les appareils sont perdus) *+ sauvegarde chiffrée de l'historique*
- [x] **P1** Transfert de l'historique des MP vers un nouvel appareil *(avancé au jalon 5 : au cœur de la demande du CP)*

### A4. Amis
- [x] **P0** Envoyer / accepter / refuser / annuler une demande d'ami
- [x] **P0** Liste d'amis, retrait d'un ami
- [x] **P1** Bloquer un utilisateur
- [x] **P1** Présence entre amis (en ligne / absent / ne pas déranger / invisible)
- [ ] **P2** Réglages de confidentialité (qui peut m'envoyer une demande)

### A5. Messages privés (chiffrés E2E)
- [x] **P0** MP 1-à-1 chiffrés (Olm/Megolm, choisi par le CP)
- [x] **P0** Boîte aux lettres : le service stocke uniquement des messages chiffrés, jusqu'à livraison sur tous les appareils
- [x] **P0** Temps réel (WebSocket) : réception, accusé de livraison
- [x] **P1** Groupes de MP (plusieurs participants)
- [x] **P1** Pièces jointes chiffrées
- [x] **P1** Édition / suppression de ses messages
- [x] **P1** Indicateur « en train d'écrire », accusés de lecture (désactivables)
- [ ] **P2** Messages éphémères

### A6. Appels entre amis (P2P)
- [x] **P1** Signalisation WebRTC via le service central (échange d'offres, sans transit du média)
- [x] **P1** Appels audio 1-à-1 en P2P
- [x] **P1** Relais TURN de secours, désactivable par l'utilisateur
- [ ] **P2** Vidéo et partage d'écran en P2P
- [ ] **P2** Appels de groupe entre amis

### A7. Administration du service central
- [x] **P1** Désactivation / réactivation d'un compte (outil admin, journalisé)
- [x] **P1** Déploiement Docker du service central (auto-hébergeable)

---

## B. Serveur communautaire (Docker)

### B1. Installation et exploitation
- [x] **P0** Image Docker unique, lancement en une commande, données dans un volume
- [x] **P0** Base SQLite embarquée, migrations automatiques
- [x] **P0** Configuration minimale : nom du serveur, services d'identité acceptés
- [x] **P0** Création du propriétaire au premier démarrage (lien ou code de revendication)
- [x] **P1** UPnP : ouverture automatique des ports + diagnostic de joignabilité
- [x] **P1** HTTPS : certificat automatique (Let's Encrypt) ou auto-signé épinglé
- [x] **P1** Sauvegarde / restauration (export des données)
- [x] **P1** Mise à jour sans perte de données
- [x] **P1** Interface d'administration web des deux serveurs (Docker : réseau local + mot de passe) *(priorité du CP, livrée le 2026-09-24)*
- [x] **P1** Version Windows des deux serveurs : installateur, icône près de l'horloge, démarrage avec Windows *(priorité du CP, livrée le 2026-09-24 ; à valider sur un vrai Windows)*

### B2. Accès et membres
- [x] **P0** Connexion avec un jeton d'identité (vérifié hors ligne)
- [x] **P0** Mode d'accès : public ou privé (sur invitation)
- [x] **P0** Liens d'invitation (expiration, nombre d'utilisations)
- [x] **P0** Liste des membres, pseudo local (surnom sur le serveur)
- [x] **P1** Vérification supplémentaire configurable (ex. téléphone via le fournisseur choisi par le serveur) *(webhook générique ou OVHcloud SMS recommandés, Twilio en option)*
- [x] **P1** Écran de règles à accepter avant d'entrer

### B3. Salons
- [x] **P0** Salons texte, salons vocaux *(le vocal lui-même arrive au jalon 4)*
- [x] **P0** Catégories, ordre des salons
- [x] **P1** Fils de discussion (threads)
- [x] **P1** Salons d'annonces (lecture seule)
- [ ] **P2** Salons forum

### B4. Messages (non chiffrés E2E)
- [x] **P0** Envoi / réception en temps réel (WebSocket)
- [x] **P0** Historique paginé
- [x] **P0** Édition / suppression
- [x] **P0** Mentions (@membre, @rôle, @everyone)
- [x] **P1** Réponses (citation d'un message)
- [x] **P1** Réactions emoji
- [x] **P1** Pièces jointes (stockage disque local, limite de taille configurable)
- [x] **P1** Aperçus de liens (générés par le serveur)
- [x] **P1** Messages épinglés
- [x] **P1** Recherche dans l'historique
- [x] **P1** Indicateur « en train d'écrire », messages non lus *+ réglages de notification par salon (tout / mentions / rien, sourdine)*
- [ ] **P2** Emojis personnalisés du serveur
- [ ] **P2** Mise en forme Markdown étendue, blocs de code

### B5. Rôles et permissions
- [x] **P0** Rôles avec permissions (gérer le serveur, salons, rôles, membres, messages…)
- [x] **P0** Surcharges de permissions par salon / catégorie
- [x] **P1** Hiérarchie des rôles, couleur *(avancé au jalon 3 : indispensable à la sécurité des rôles)*
- [x] **P1** Affichage séparé des rôles dans la liste des membres

### B6. Modération
- [x] **P0** Expulsion (kick) et bannissement (lié à l'identité portable)
- [x] **P1** Exclusion temporaire (timeout)
- [x] **P1** Journal d'audit des actions de modération
- [x] **P1** Suppression en masse des messages d'un membre
- [ ] **P2** Filtres automatiques (mots interdits, anti-spam)

### B7. Voix et vidéo (salons vocaux)
- [x] **P0** Audio de groupe dans les salons vocaux (SFU LiveKit intégré à l'image)
- [x] **P0** Muet / sourdine, liste des participants
- [x] **P1** Vidéo et partage d'écran
- [x] **P1** Permissions vocales `connect` / `speak`, appliquées en direct *(avancé au jalon 4)*
- [x] **P1** Modération vocale : déplacer / rendre muet un membre, permission de streamer *(+ sourdine imposée, déconnexion)*
- [ ] **P2** Salons « scène » (conférence)

### B8. Bots et intégrations
- [x] **P1** Comptes bot avec jeton propre au serveur
- [x] **P1** API publique documentée pour les bots *(`docs/api.md`, bot d'exemple `examples/pingbot`)*
- [ ] **P2** Webhooks entrants
- [ ] **P2** Commandes slash

### B9. Notifications
- [x] **P1** Réglages de notification par salon (tout, mentions, rien) *(livré au bloc 1 : réglages stockés et synchronisés, appliqués par le client)*
- [ ] **P2** Notifications push mobiles (fonctionnement à définir sans fuite de données vers un tiers)

---

## C. Client (en dernier)

- [x] **P0** Client de test en ligne de commande (pour valider l'API pendant le dev backend)
- [ ] **P1** Application desktop (multi-serveurs, multi-services d'identité) — *en cours : étapes 1 (comptes), 2 (serveurs, salons, messages), 3 (vocal, vidéo), 4a (amis, MP chiffrés) et 4b (validation des appareils, phrase de récupération) livrées le 2026-09-24*
- [ ] **P1** Client web
- [ ] **P2** Application mobile

---

## Ordre de développement proposé

1. **Jalon 1 — Identity minimal :** A1 P0, A2 P0 → un compte et un jeton d'identité vérifiable. ✅ *Livré le 2026-09-23, en test par le CP (`docs/tests/jalon-1.md`).*
2. **Jalon 2 — Serveur communautaire texte :** B1 P0, B2 P0, B3 P0, B4 P0 → rejoindre un serveur et discuter. ✅ *Livré le 2026-09-23, en test par le CP (`docs/tests/jalon-2.md`).*
3. **Jalon 3 — Rôles et modération :** B5 P0, B6 P0. ✅ *Livré le 2026-09-23, en test par le CP (`docs/tests/jalon-3.md`).*
4. **Jalon 4 — Voix :** B7 P0. ✅ *Livré le 2026-09-23, en test par le CP (`docs/tests/jalon-4.md`).*
5. **Jalon 5 — Amis et MP chiffrés :** A3 P0, A4 P0, A5 P0. ✅ *Livré le 2026-09-23, en test par le CP (`docs/tests/jalon-5.md`).*
6. **Jalon 6 — P1, premier bloc (mise en ligne) :** HTTPS, UPnP, limitation par IP, phrase de récupération. ✅ *Livré le 2026-09-23, en test par le CP (`docs/tests/jalon-6.md`).*
6 bis. **Jalons suivants — autres blocs P1**, dans l'ordre choisi par le CP.
7. **Jalon 7 — Client graphique.** Maquettes validées le 2026-09-24. Étapes : 1. comptes ✅ (`docs/tests/client-etape-1.md`), 2. serveurs, salons, messages ✅ (`docs/tests/client-etape-2.md`), 3. vocal et vidéo ✅ (`docs/tests/client-etape-3.md`), 4. amis et MP chiffrés : 4a ✅ (`docs/tests/client-etape-4a.md`), 4b ✅ (`docs/tests/client-etape-4b.md`), 4c fichiers, 5. appels, 6. paramètres, 7. version web.
