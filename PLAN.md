# Quarel — Plan des fonctionnalités

> **Brouillon à valider avec le CP.** Priorités : **P0** = MVP (indispensable pour un premier test bout à bout), **P1** = V1 publique, **P2** = plus tard.
> Le front-end (client) est traité en dernier ; en attendant, tout se teste via l'API (scripts, `curl`, client de test en ligne de commande).

---

## A. Service central « Identity »

### A1. Comptes
- [ ] **P0** Inscription : email, pseudo, mot de passe (hash Argon2id)
- [ ] **P0** Vérification de l'email (lien ou code)
- [ ] **P0** Connexion / déconnexion, sessions par appareil
- [ ] **P0** 2FA TOTP (application d'authentification) + codes de secours
- [ ] **P1** Mot de passe oublié (via email)
- [ ] **P1** Changement d'email, de pseudo, de mot de passe
- [ ] **P1** Suppression de compte par l'utilisateur (effacement réel des données)
- [ ] **P1** Limitation de débit (anti-bruteforce, anti-spam d'inscriptions)
- [ ] **P2** 2FA par clé matérielle / passkey (WebAuthn)

### A2. Identité portable
- [ ] **P0** Identifiant unique au format `pseudo@domaine-du-service`
- [ ] **P0** Émission d'un jeton d'identité signé (ID, pseudo, clé publique, expiration)
- [ ] **P0** Publication de la clé publique de signature du service (vérification hors ligne par les serveurs)
- [ ] **P1** Rotation des clés de signature du service
- [ ] **P1** Liste des comptes désactivés, consultable par les serveurs (réquisition judiciaire)
- [ ] **P1** Profil public minimal : pseudo, avatar, bio

### A3. Appareils et clés E2E
- [ ] **P0** Enregistrement du premier appareil (génération des clés côté client)
- [ ] **P0** Ajout d'un appareil validé depuis un appareil existant (QR code / code court)
- [ ] **P0** Liste et révocation des appareils
- [ ] **P1** Phrase de récupération (restaure les clés si tous les appareils sont perdus)
- [ ] **P1** Transfert de l'historique des MP vers un nouvel appareil

### A4. Amis
- [ ] **P0** Envoyer / accepter / refuser / annuler une demande d'ami
- [ ] **P0** Liste d'amis, retrait d'un ami
- [ ] **P1** Bloquer un utilisateur
- [ ] **P1** Présence entre amis (en ligne / absent / ne pas déranger / invisible)
- [ ] **P2** Réglages de confidentialité (qui peut m'envoyer une demande)

### A5. Messages privés (chiffrés E2E)
- [ ] **P0** MP 1-à-1 chiffrés (bibliothèque éprouvée : MLS ou Olm/Megolm)
- [ ] **P0** Boîte aux lettres : le service stocke uniquement des messages chiffrés, jusqu'à livraison sur tous les appareils
- [ ] **P0** Temps réel (WebSocket) : réception, accusé de livraison
- [ ] **P1** Groupes de MP (plusieurs participants)
- [ ] **P1** Pièces jointes chiffrées
- [ ] **P1** Édition / suppression de ses messages
- [ ] **P1** Indicateur « en train d'écrire », accusés de lecture (désactivables)
- [ ] **P2** Messages éphémères

### A6. Appels entre amis (P2P)
- [ ] **P1** Signalisation WebRTC via le service central (échange d'offres, sans transit du média)
- [ ] **P1** Appels audio 1-à-1 en P2P
- [ ] **P1** Relais TURN de secours, désactivable par l'utilisateur
- [ ] **P2** Vidéo et partage d'écran en P2P
- [ ] **P2** Appels de groupe entre amis

### A7. Administration du service central
- [ ] **P1** Désactivation / réactivation d'un compte (outil admin, journalisé)
- [ ] **P1** Déploiement Docker du service central (auto-hébergeable)

---

## B. Serveur communautaire (Docker)

### B1. Installation et exploitation
- [ ] **P0** Image Docker unique, lancement en une commande, données dans un volume
- [ ] **P0** Base SQLite embarquée, migrations automatiques
- [ ] **P0** Configuration minimale : nom du serveur, services d'identité acceptés
- [ ] **P0** Création du propriétaire au premier démarrage (lien ou code de revendication)
- [ ] **P1** UPnP : ouverture automatique des ports + diagnostic de joignabilité
- [ ] **P1** HTTPS : certificat automatique (Let's Encrypt) ou auto-signé épinglé
- [ ] **P1** Sauvegarde / restauration (export des données)
- [ ] **P1** Mise à jour sans perte de données
- [ ] **P2** Exécutable Windows (`.exe`)

### B2. Accès et membres
- [ ] **P0** Connexion avec un jeton d'identité (vérifié hors ligne)
- [ ] **P0** Mode d'accès : public ou privé (sur invitation)
- [ ] **P0** Liens d'invitation (expiration, nombre d'utilisations)
- [ ] **P0** Liste des membres, pseudo local (surnom sur le serveur)
- [ ] **P1** Vérification supplémentaire configurable (ex. téléphone via le fournisseur choisi par le serveur)
- [ ] **P1** Écran de règles à accepter avant d'entrer

### B3. Salons
- [ ] **P0** Salons texte, salons vocaux
- [ ] **P0** Catégories, ordre des salons
- [ ] **P1** Fils de discussion (threads)
- [ ] **P1** Salons d'annonces (lecture seule)
- [ ] **P2** Salons forum

### B4. Messages (non chiffrés E2E)
- [ ] **P0** Envoi / réception en temps réel (WebSocket)
- [ ] **P0** Historique paginé
- [ ] **P0** Édition / suppression
- [ ] **P0** Mentions (@membre, @rôle, @everyone)
- [ ] **P1** Réponses (citation d'un message)
- [ ] **P1** Réactions emoji
- [ ] **P1** Pièces jointes (stockage disque local, limite de taille configurable)
- [ ] **P1** Aperçus de liens (générés par le serveur)
- [ ] **P1** Messages épinglés
- [ ] **P1** Recherche dans l'historique
- [ ] **P1** Indicateur « en train d'écrire », messages non lus
- [ ] **P2** Emojis personnalisés du serveur
- [ ] **P2** Mise en forme Markdown étendue, blocs de code

### B5. Rôles et permissions
- [ ] **P0** Rôles avec permissions (gérer le serveur, salons, rôles, membres, messages…)
- [ ] **P0** Surcharges de permissions par salon / catégorie
- [ ] **P1** Hiérarchie des rôles, couleur, affichage séparé dans la liste des membres

### B6. Modération
- [ ] **P0** Expulsion (kick) et bannissement (lié à l'identité portable)
- [ ] **P1** Exclusion temporaire (timeout)
- [ ] **P1** Journal d'audit des actions de modération
- [ ] **P1** Suppression en masse des messages d'un membre
- [ ] **P2** Filtres automatiques (mots interdits, anti-spam)

### B7. Voix et vidéo (salons vocaux)
- [ ] **P0** Audio de groupe dans les salons vocaux (SFU LiveKit intégré à l'image)
- [ ] **P0** Muet / sourdine, liste des participants
- [ ] **P1** Vidéo et partage d'écran
- [ ] **P1** Permissions vocales (parler, streamer, déplacer / rendre muet un membre)
- [ ] **P2** Salons « scène » (conférence)

### B8. Bots et intégrations
- [ ] **P1** Comptes bot avec jeton propre au serveur
- [ ] **P1** API publique documentée pour les bots
- [ ] **P2** Webhooks entrants
- [ ] **P2** Commandes slash

### B9. Notifications
- [ ] **P1** Réglages de notification par salon (tout, mentions, rien)
- [ ] **P2** Notifications push mobiles (fonctionnement à définir sans fuite de données vers un tiers)

---

## C. Client (en dernier)

- [ ] **P0** Client de test en ligne de commande (pour valider l'API pendant le dev backend)
- [ ] **P1** Application desktop (multi-serveurs, multi-services d'identité)
- [ ] **P1** Client web
- [ ] **P2** Application mobile

---

## Ordre de développement proposé

1. **Jalon 1 — Identity minimal :** A1 P0, A2 P0 → un compte et un jeton d'identité vérifiable.
2. **Jalon 2 — Serveur communautaire texte :** B1 P0, B2 P0, B3 P0, B4 P0 → rejoindre un serveur et discuter.
3. **Jalon 3 — Rôles et modération :** B5 P0, B6 P0.
4. **Jalon 4 — Voix :** B7 P0.
5. **Jalon 5 — Amis et MP chiffrés :** A3 P0, A4 P0, A5 P0.
6. **Jalon 6 — P1** (par blocs, dans l'ordre choisi par le CP).
7. **Jalon 7 — Client graphique.**
