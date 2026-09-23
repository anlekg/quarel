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

## Arborescence

```
cmd/quarel-identity/    binaire du service Identity
cmd/quarelctl/          client de test en ligne de commande (sorties en français, pour le CP)
internal/identity/      service Identity : HTTP (server.go), endpoints (handlers.go), SQLite (store.go),
                        argon2id / clés (crypto.go), TOTP (totp.go), emails (mail.go),
                        anti-bruteforce (lockout.go)
pkg/idtoken/            jetons d'identité portables — partagé avec le futur serveur communautaire
docs/tests/             guides de test par jalon, destinés au CP
Dockerfile.identity     image distroless (~23 Mo), volume /data
dev.sh                  commandes de dev (build, test, run-identity…) — pas de make sur la machine
```

## Identité portable (implémentée, jalon 1)

- **Identifiant stable** : `(iss, sub)` = domaine du service Identity + ID aléatoire 128 bits (base32). **Les bans et appartenances se basent dessus**, jamais sur le handle `pseudo@domaine` (le pseudo pourra changer).
- **Jeton** : JWT EdDSA (Ed25519) avec `iss`, `sub`, `handle`, `dkey` (clé publique de l'appareil), `iat`, `exp`, `jti`, en-tête `kid`. Durée : `QUAREL_TOKEN_TTL` (12 h par défaut). **Pas d'audience** : l'Identity ne sait pas à quel serveur le jeton est destiné. Aucun email dans le jeton.
- **Clés publiques** du service : `GET /.well-known/quarel-identity` → `{issuer, keys:[{kid, alg, crv, x}]}`. Clé privée : `$QUAREL_DATA_DIR/signing.key` (générée au 1er démarrage, à sauvegarder).
- **Anti-rejeu** : le client génère une paire Ed25519 par appareil, envoie la clé publique au login. Un serveur communautaire envoie un nonce ; le client renvoie `idtoken.SignProof(device, audience, nonce)` ; le serveur appelle `idtoken.Verify` puis `idtoken.VerifyProof`. Le serveur doit garantir l'usage unique de ses nonces.

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

## Commandes

```sh
./dev.sh build          # binaires dans bin/
./dev.sh test           # tests (go test ./...)
./dev.sh vet
./dev.sh run-identity  # service Identity local sur :8080, données dans ./data
./dev.sh docker-identity # image quarel-identity
./bin/quarelctl help  # client de test
```

Tests : les tests d'intégration (`internal/identity/identity_test.go`) démarrent un vrai serveur HTTP sur une base SQLite temporaire, avec une horloge contrôlable (`srv.now`) et un argon2 allégé.

## Environnement de dev

- OS : Linux. Disponibles : Docker, Node.js, Python 3, Go 1.27.1 (installé dans `~/.local/go`, PATH ajouté dans `~/.zshrc`). Non installés : Rust, `gh`.
- Si `go` est introuvable dans le shell courant : `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"`.
- Git : branche `main`, remote `origin` = `git@github.com:anlekg/quarel.git` (SSH). Identité locale au dépôt : `anlekg` / adresse masquée GitHub `106981899+anlekg@users.noreply.github.com` (ne jamais utiliser l'email personnel).
