# Client graphique, étape 1 — Guide de test (comptes)

> Test automatique : `make e2e-client` (l'application Electron réelle, pilotée contre un vrai service d'identité).

## Préparation
1. Terminal 1 : `make run-identity` (service d'identité local ; les codes envoyés « par email » s'affichent dans ce terminal).
2. Terminal 2 : `make client-dev` (installe les dépendances au premier lancement, puis ouvre l'application).
3. Sur l'écran de connexion, sous « Service d'identité », cliquer **Changer** et saisir `localhost:8080`.

> Sous Linux, si Electron refuse de démarrer à cause de `chrome-sandbox` : `sudo chown root:root client/node_modules/electron/dist/chrome-sandbox && sudo chmod 4755 client/node_modules/electron/dist/chrome-sandbox`.

## 1. Choisir le service d'identité
- Une adresse qui ne répond pas (`exemple.invalid`) : message « Aucun service d'identité Quarel ne répond à cette adresse ».
- `http://` est refusé sauf pour cette machine (`localhost`, `127.0.0.1`).
- Le choix est retenu au prochain lancement.

## 2. Créer un compte
1. **Créer un compte** : email, pseudo, mot de passe. Les erreurs (pseudo trop court, mot de passe de moins de 10 caractères, email ou pseudo déjà pris) s'affichent sous les champs.
2. Écran « Vérifier votre email » : copier le code à 6 chiffres depuis le terminal 1. Un mauvais code affiche une erreur ; **Renvoyer le code** est limité à une fois par minute.
3. Le bon code connecte directement : écran « Bienvenue ». En bas à gauche : pseudo et identifiant complet (`pseudo@localhost:8080`).

## 3. Rester connecté
Fermer l'application et la relancer : on arrive directement sur l'accueil, sans ressaisir le mot de passe.

## 4. Paramètres (roue dentée en bas à gauche)
- **Compte** : pseudo, identifiant complet, email, double authentification, service, date de création.
- **Appareils** : chaque session ouverte (« Cet appareil » pour celle-ci). Se connecter ailleurs (autre profil `quarelctl`, par exemple) puis **Déconnecter** cette autre session.
- **Se déconnecter** (avec confirmation) : retour à l'écran de connexion, identifiant pré-rempli.
- Échap ou la croix ferme les paramètres.

## 5. Double authentification
L'activer avec `quarelctl` (`2fa-setup`, `2fa-enable`) en attendant l'étape « Paramètres ». À la connexion suivante : écran « Double authentification », code de l'application d'authentification, ou **Utiliser un code de secours**.

## 6. Session fermée ailleurs
Fermer la session de l'application depuis un autre appareil, puis relancer l'application : message « Votre session a expiré ou a été fermée depuis un autre appareil ».

## 7. Mot de passe oublié
**Mot de passe oublié ?** → email du compte → code (terminal 1) + nouveau mot de passe. Avec la double authentification, un code est aussi demandé : l'email seul ne suffit pas. Message final : toutes les sessions ont été fermées.

## À regarder en particulier
Fidélité aux maquettes (couleurs, polices, espacements), clarté des messages, navigation au clavier (Tab, Entrée, Échap).
