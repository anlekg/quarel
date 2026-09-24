# Administration web des serveurs — Guide de test

> Tests automatiques : `go test ./internal/adminui/` ; scénarios existants inchangés (`make e2e-…`).

## Serveur communautaire (Docker)
1. `cd deploy/server && docker compose up -d --build` puis `docker compose logs server` : l'adresse de la page (`http://192.168.x.x:8091`) et un **code d'installation** s'affichent.
   > Attention : ce fichier active l'UPnP (ouverture des ports de votre box). Pour un essai sans toucher à la box, ajoutez `environment: { QUAREL_UPNP: "off" }` au service.
2. Depuis un autre appareil du réseau : ouvrir l'adresse, entrer le code et choisir un mot de passe. Un mauvais code est refusé.
3. **Tableau de bord** : « En marche », nom, identifiant, adresse, vocal ; un **lien propriétaire** tant que personne n'a revendiqué le serveur. Dans l'application : « Rejoindre un serveur », coller ce lien → vous êtes propriétaire (le lien disparaît du tableau de bord).
4. **Renommer le serveur** : le nouveau nom apparaît dans l'application.
5. **Réglages** : les champs dépendants apparaissent selon les choix (Let's Encrypt, LiveKit externe, SMS). Essayer « OVHcloud SMS » sans clés → refus expliqué, rien n'est changé. Changer la taille maximale des fichiers → « le service redémarre », il revient « En marche » en quelques secondes.
6. **Sauvegardes** : télécharger une archive, puis la restaurer : message avec la date de la sauvegarde ; le mot de passe de la page ne change pas.
7. **Journal** : messages du serveur, filtre.
8. **Mot de passe** : le changer ; « Se déconnecter » puis se reconnecter.
9. Une variable d'environnement (ex. `QUAREL_MAX_UPLOAD_MB` dans le fichier Compose) → le réglage est verrouillé dans la page.

## Service d'identité
1. `make run-identity` : page sur `http://localhost:8081` (sur un serveur loué : tunnel SSH, voir `docs/deploiement-identity.md`).
2. Tableau de bord : alertes « nom public localhost » et « aucun serveur d'emails ».
3. **Comptes** : rechercher un compte, le désactiver (raison obligatoire) → il ne peut plus se connecter ; le réactiver. Le journal de l'opérateur garde les deux actions.
4. **Changer la clé de signature** : le service redémarre ; les connexions existantes continuent.

## À regarder en particulier
Clarté des textes, facilité de la première installation, comportement sur téléphone (page adaptée aux petits écrans).
