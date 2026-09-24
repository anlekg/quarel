# Client graphique, étape 2 — Guide de test (serveurs, salons, messages)

> Test automatique : `make e2e-client` (scénario « servers » : l'application face à une autre personne sur `quarelctl`).

## Préparation
1. `make run-identity` (terminal 1) et `make run-server` (terminal 2 : serveur sur `https://localhost:8090`, certificat auto-signé ; le **code de revendication** du propriétaire s'affiche au démarrage).
2. Une autre personne avec `quarelctl` : `quarelctl -p alice register …`, `verify-email`, `login`, puis `quarelctl -p alice claim localhost:8090 <code>` (alice devient propriétaire) et `quarelctl -p alice invite` (affiche un lien `quarel://localhost:8090/…?sid=…`).
3. `make client-dev`, connexion avec un autre compte (service `localhost:8080`).

> Le client **web** (navigateur) ne peut pas rejoindre un serveur à certificat auto-signé : utilisez l'application.

## 1. Rejoindre
1. Bouton **+** (ou « Rejoindre un serveur » sur l'accueil) → coller le lien → **Continuer**.
2. Écran « Serveur authentifié » : nom, nombre de membres, accès. **Rejoindre**.
3. Essayer avec un lien dont on a modifié le `sid=` : message « L'identité de ce serveur ne correspond pas au lien d'invitation ».
4. Si alice a défini des règles (`quarelctl -p alice rules "Soyez courtois."`) avant votre arrivée : écran « Avant de participer », case à cocher, **Entrer dans le serveur**.

## 2. Salons et messages
- Salons regroupés par catégorie ; les salons vocaux sont affichés mais pas encore utilisables (étape 3).
- Écrire, Entrée pour envoyer, Maj + Entrée pour aller à la ligne. `**gras**`, `*italique*`, `` `code` ``, blocs ```` ``` ````, liens cliquables (ouverts dans le navigateur).
- `@alice` dans un message la mentionne ; un message d'alice qui vous mentionne est surligné.
- Au survol d'un message : réagir, répondre, modifier (le vôtre), supprimer (le vôtre, ou tous avec la permission ; Maj + clic sans confirmation).
- Trombone, glisser-déposer ou coller : fichiers joints (images affichées, clic pour agrandir ; autres fichiers téléchargeables).
- `quarelctl -p alice typing général` → « alice écrit… ».
- Faire défiler vers le haut charge les messages plus anciens.

## 3. Non-lus
Pendant que vous êtes dans un salon, alice écrit dans un autre : il passe en gras ; avec une mention, une pastille rouge apparaît sur le salon et sur l'icône du serveur. Ouvrir le salon remet à zéro.

## 4. Menu du serveur (nom du serveur en haut à gauche)
- **Inviter des personnes** (si vous en avez le droit) : lien à copier, valable 7 jours.
- **Quitter le serveur** (sauf propriétaire).

## 5. Robustesse
- Fermer et relancer l'application : on retrouve le serveur et le dernier salon ouvert.
- Arrêter `make run-server` : bandeau « Connexion perdue, nouvelle tentative… » ; le relancer : tout revient seul.
- `quarelctl -p alice kick <vous>` : « Vous avez été expulsé de ce serveur », avec **Retirer de ma liste**.

## À regarder en particulier
Fluidité (arrivée des messages, défilement), clarté des messages d'erreur, fidélité aux maquettes.
