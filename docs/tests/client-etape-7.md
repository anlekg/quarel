# Client graphique, étape 7 — Guide de test (version web)

> Tests automatiques : `make e2e-web` (Chromium) et `make e2e-client` (tout).

Sur `https://app.quarel.app`, depuis un ordinateur **et un téléphone**.

1. **Lien d'invitation web** : sur test.quarel.app (application ou web), menu du serveur → **Inviter des personnes** : le lien commence maintenant par `https://app.quarel.app/join#…`. Ouvrez-le dans un navigateur :
   - page « Invitation sur un serveur Quarel » avec **Ouvrir dans l'application Quarel** (si l'application de bureau est installée) ou **Continuer dans le navigateur** ;
   - dans le navigateur : connexion (un rappel indique le serveur visé), puis la fenêtre « Rejoindre » s'ouvre directement.
2. **Application installable** : ordinateur (Chrome/Edge) : icône d'installation dans la barre d'adresse, ou Paramètres › **Installer l'application**. Téléphone : menu du navigateur › « Ajouter à l'écran d'accueil » / « Installer ». Quarel s'ouvre alors dans sa propre fenêtre, avec son icône.
3. **Téléphone** : un écran à la fois — la liste des serveurs, salons et conversations, puis le salon ou la conversation choisis ; la flèche **←** en haut à gauche revient aux listes. L'icône « membres » affiche la liste par-dessus. Les paramètres ont leurs sections en bandeau en haut.
4. **Stockage chiffré** : rien à faire ; la session et l'historique des messages privés gardés par le navigateur sont chiffrés.
5. **Serveur auto-signé** : un serveur hébergé à la maison sans nom de domaine ne peut pas être rejoint depuis le navigateur (message explicatif) ; sa page d'administration indique comment le rendre accessible (Let's Encrypt ou proxy HTTPS). L'application de bureau le rejoint toujours.

## À savoir
- Sur le téléphone, le micro et la caméra marchent (HTTPS). Le partage d'écran dépend du navigateur (souvent absent sur mobile).
- Sur iPhone, l'installation passe par Safari › Partager › « Sur l'écran d'accueil ».
