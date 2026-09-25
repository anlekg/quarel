# Application — Guide de test (installateurs, recherche, épingles, fils, notifications)

> Tests automatiques : `make e2e-client` (scénario « channels » et le reste). Paquets : `make client-dist` (Linux) et `make client-dist-win` (Windows) → `client/release/`.

## Installer l'application de bureau
- **Windows** : `Quarel-Setup-0.1.0.exe`. L'installateur n'est pas signé : Windows affiche « Windows a protégé votre ordinateur » → **Informations complémentaires** → **Exécuter quand même**. Installation pour votre compte seulement (pas besoin d'être administrateur), raccourcis bureau et menu Démarrer. Pour mettre à jour : relancer un installateur plus récent (Quarel est fermé automatiquement). Désinstallation : Paramètres Windows › Applications.
- **Linux** : `Quarel-0.1.0-x86_64.AppImage` (rendre exécutable, puis lancer) ou `sudo apt install ./Quarel-0.1.0-amd64.deb`.
- Au premier lancement, les liens `quarel://` s'ouvrent dans l'application (depuis la page d'invitation web : « Ouvrir dans l'application Quarel »).

## Dans un salon
1. **Rechercher** : loupe en haut à droite → mots (accents et majuscules ignorés) ; cocher « Seulement dans #salon » pour limiter. Cliquer un résultat : le message s'affiche en surbrillance, même s'il est ancien.
2. **Épingler** (modération) : survol d'un message → épingle ; la mention « Épinglé » apparaît ; icône épingle en haut → liste des messages épinglés.
3. **Fil** : survol d'un message → « Créer un fil » → nom : le fil s'ouvre, il apparaît sous le salon dans la liste de gauche, et un lien « Ouvrir le fil » sous le message de départ.
4. **Notifications** :
   - par défaut, une notification seulement quand on vous mentionne (et pour chaque message privé), si le salon ou la conversation n'est pas sous vos yeux ;
   - **cloche** en haut d'un salon : comme le serveur / tous les messages / @mentions / rien, et **sourdine** (15 min à « jusqu'à réactivation ») ; un salon en sourdine apparaît grisé ;
   - **menu du serveur › Notifications** : le même réglage pour tout le serveur ;
   - Paramètres › **Notifications** : tout couper, ou (navigateur) autoriser les notifications ;
   - rien en mode « Ne pas déranger ». Un clic sur la notification ouvre le salon ou la conversation.
