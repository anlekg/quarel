# Quarel — Fiche technique

Alternative à Discord **auto-hébergeable** : chaque serveur tourne chez son propriétaire (Docker, puis `.exe`), les utilisateurs s'y connectent via un client.

- Processus, recherches et décisions : `PROJECT.md`
- Fonctionnalités à implémenter : `PLAN.md`

## Règles de travail

- Langue de la documentation : français. Code, identifiants et commits : anglais.
- L'utilisateur est chef de projet et testeur ; Claude code. Toute décision d'architecture est validée avec lui puis consignée dans `PROJECT.md` (journal des décisions).
- Backend / fonctionnalités d'abord, front-end en dernier.
- Tenir ce fichier à jour à chaque changement de stack, d'architecture ou de commande.

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

## Architecture

_À définir (phase 3)._

## Commandes

_À définir._

## Environnement de dev

- OS : Linux. Disponibles : Docker, Node.js, Python 3, Go 1.27.1 (installé dans `~/.local/go`, PATH ajouté dans `~/.zshrc`). Non installés : Rust, `gh`.
- Si `go` est introuvable dans le shell courant : `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"`.
- Git : branche `main`, identité locale au dépôt (`anlekg`). Accès SSH à GitHub opérationnel (compte `anlekg`), pas encore de remote.
