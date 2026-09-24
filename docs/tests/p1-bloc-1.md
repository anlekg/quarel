# P1 bloc 1 — Guide de test (messages et salons)

> Test automatique : `make e2e-messages` (18 vérifications avec les vrais serveurs et `quarelctl`).

## Préparation

`make build`, `make run-identity`, `make run-server`, deux comptes (`alice` propriétaire, `bob` membre) comme aux jalons précédents. Dans un 3e terminal : `./bin/quarelctl -p bob listen` pour voir les événements en direct.

> Le serveur applique une migration de sa base au démarrage : un serveur existant garde ses données.

Dans la suite, `Q` = `./bin/quarelctl -p <profil>` ; les ids des messages s'affichent dans `history`.

## 1. Réponses, réactions, épingles
1. `-p alice send général Qui vient samedi ?`, puis `-p bob reply général <id> Moi !` → la réponse affiche `↱ alice : Qui vient samedi ?` ; alice est notifiée (🔔 dans son `listen`).
2. `-p bob react général <id> 👍`, puis la même chose avec alice → `history` affiche `👍 2*` (`*` = j'ai réagi). `unreact` retire. Un texte (`react … lol`) est refusé.
3. `-p bob pin général <id>` → refusé (`manage_messages`). `-p alice pin général <id>` → 📌 dans l'historique ; `pins général` les liste.

## 2. Fichiers
1. `-p bob send-file général ./photo.png Regardez` → message avec 📎 et la commande `download <id>` à utiliser.
2. `-p alice download <id>` → le fichier est enregistré dans le dossier courant, identique.
3. Fichier trop gros (25 Mo par défaut, `QUAREL_MAX_UPLOAD_MB`) → `file_too_large`.
4. Retirer le droit `attach_files` à bob sur un salon (`override … deny=attach_files`) → envoi refusé. Retirer `view_channel` → il ne peut plus télécharger les fichiers de ce salon.

## 3. Aperçus de liens
1. `-p alice send général https://fr.wikipedia.org/wiki/Discord` → une seconde plus tard, `listen` montre une mise à jour avec 🔗 et le titre de la page ; `history` affiche l'aperçu.
2. Un lien vers une adresse locale (`http://192.168.1.1`, `http://localhost:8080`) ne donne **aucun** aperçu : le serveur refuse de contacter son propre réseau.
3. `QUAREL_LINK_PREVIEWS=off` désactive la fonction.

## 4. Recherche
1. `-p bob search ecole` trouve « l'École » (accents et majuscules ignorés, mots commencés : `eco` suffit).
2. Filtres : `search liste in:général from:bob`.
3. Un message d'un salon que bob ne voit pas n'apparaît jamais dans ses résultats.

## 5. Fils et salons d'annonces
1. `-p bob thread général <id>` → fil nommé d'après le message ; `channels` l'affiche (🧵) sous son salon. On y écrit avec `send <id-du-fil> …`.
2. Un fil suit les droits de son salon : si bob ne voit plus le salon, il ne voit plus le fil. Supprimer le salon supprime ses fils.
3. `-p alice channel-create annonces announcement` → 📢 ; bob peut lire mais pas écrire (`missing_permissions`) ; il faut `manage_messages`.

## 6. Non-lus, « en train d'écrire », notifications
1. alice écrit 2 messages dont un `@bob …` → `-p bob unread` : « 2 non lu(s) 🔔 1 mention(s) ».
2. `-p bob read général` → plus rien. Si bob a deux appareils, l'autre voit « ✓ lu » dans son `listen`.
3. `-p alice typing général` → le `listen` de bob affiche « alice écrit dans #général ».
4. `-p bob notify annonces none muet=toujours`, `notify serveur mentions`, `notify` seul pour la liste. Ces réglages sont stockés sur le serveur et appliqués par le futur client (aucune notification n'est envoyée par le serveur lui-même).

## 7. Rôles séparés
`-p alice role-edit Modo hoist=true` → le rôle est marqué pour être affiché à part dans la liste des membres (rendu par le futur client).
