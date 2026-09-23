# Jalon 1 — Guide de test (service Identity)

Objectif : vérifier l'inscription, la vérification d'email, la connexion, les sessions, la 2FA et le jeton d'identité portable.

## Préparation

Dans un premier terminal, à la racine du projet :

```sh
make run-identity
```

Le serveur écoute sur `http://localhost:8080`. Aucun email n'est réellement envoyé en mode dev : **les codes de vérification s'affichent dans ce terminal** (ligne `email not sent ... code de vérification Quarel : 123456`).

Dans un second terminal, l'outil de test est `./bin/quarelctl` (`./bin/quarelctl help` pour l'aide). L'option `-p <profil>` permet d'avoir plusieurs comptes de test en parallèle (ex. `-p alice`, `-p bob`).

Pour repartir de zéro : arrêter le serveur et supprimer le dossier `data/`.

## Scénarios

### 1. Inscription et vérification d'email
1. `./bin/quarelctl -p alice register alice@example.com Alice` → le mot de passe est demandé deux fois.
2. Relever le code à 6 chiffres dans le terminal du serveur.
3. `./bin/quarelctl -p alice verify-email alice@example.com <code>` → « Email vérifié ».

À vérifier aussi :
- Même email ou même pseudo (même avec une autre casse, ex. `ALICE`) → refusé.
- Mot de passe de moins de 10 caractères → refusé.
- Connexion avant vérification de l'email → `email_not_verified`.
- 5 mauvais codes → le bon code est ensuite refusé ; `resend-code` en envoie un nouveau (1 par minute max).

### 2. Connexion et sessions
1. `./bin/quarelctl -p alice login alice laptop` (par pseudo ou email).
2. `./bin/quarelctl -p alice me` → affiche le compte.
3. Ouvrir une 2ᵉ session : `./bin/quarelctl -p alice2 login alice telephone`.
4. `./bin/quarelctl -p alice sessions` → 2 sessions, `*` sur la courante.
5. `./bin/quarelctl -p alice revoke-session <id de telephone>` puis `./bin/quarelctl -p alice2 me` → refusé.
6. `./bin/quarelctl -p alice logout` puis `me` → refusé.

### 3. Double authentification (2FA)
1. Connecté : `./bin/quarelctl -p alice 2fa-setup` → scanner le QR code avec une application (Google Authenticator, Aegis, 2FAS…).
2. `./bin/quarelctl -p alice 2fa-enable <code>` → noter les 10 codes de secours.
3. `logout` puis `login` → le code 2FA est demandé.
4. Un code à 6 chiffres ne fonctionne **qu'une fois** (réessayer immédiatement avec le même → refusé).
5. Un code de secours fonctionne une fois, avec ou sans tirets.
6. `./bin/quarelctl -p alice 2fa-disable <code>` → la connexion ne demande plus de code.

### 3 bis. Blocage anti-bruteforce
1. Se tromper 15 fois de mot de passe : `QUAREL_PASSWORD=mauvais ./bin/quarelctl -p alice login alice` (répéter 15 fois ; mélanger pseudo et email, le compteur est commun).
2. 16ᵉ tentative, **avec le bon mot de passe** → `account_locked`, avec le délai restant.
3. Avec la 2FA activée : 15 mauvais codes 2FA (bon mot de passe) → même blocage.
4. Un compte inexistant se bloque de la même façon (on ne peut pas deviner quels comptes existent).
5. Le blocage se lève seul 1 h après le premier des 15 échecs. Pour ne pas attendre : arrêter le serveur et supprimer `data/`.
6. Une connexion réussie remet le compteur à zéro.

### 4. Identité portable
1. `./bin/quarelctl -p alice token` → affiche le jeton et son contenu (identifiant stable, pseudo, clé d'appareil, expiration). **Aucun email dedans.**
2. `./bin/quarelctl -p alice simulate-join mon-serveur` → simule ce que fera un serveur communautaire : vérification hors ligne du jeton, défi signé par l'appareil, refus de la même preuve sur un autre serveur.

### 5. Docker (optionnel)
```sh
make docker-identity
docker run --rm -p 8080:8080 -e QUAREL_ISSUER=localhost:8080 -v quarel-identity:/data quarel-identity
```
Les mêmes scénarios fonctionnent ; les codes s'affichent dans la sortie du conteneur.

## Retour de test

Pour chaque anomalie : la commande lancée, le résultat obtenu, le résultat attendu.
