# Bloc cosmétique — Guide de test

> Tests automatiques : `make test` (Go : validation des thèmes, profils par serveur, thème du serveur), `make client-test` (filtre du CSS, `lib/themecss.test.ts`), `make e2e-client` (`e2e/themes.spec.ts`).
> Il faut les **serveurs à jour** (service d'identité et serveur communautaire, schéma 14) et l'application à jour.

## Mon CSS
1. Paramètres › **Apparence** › Mon CSS › **Exemple** puis **Appliquer** : l'accent devient orange partout, les messages ont des coins arrondis.
2. Collez un CSS qui cache tout (`* { opacity: 0 }`) et appliquez. **Ctrl + Maj + 0** : tout revient, avec le bandeau « Mode sans échec ». Retirez votre CSS, puis « Réactiver ».
3. Application de bureau : fermez-la et relancez-la avec `--safe-mode` (raccourci Windows : ajouter `--safe-mode` à la cible) ; version web : ajoutez `?safe` à l'adresse.

## Thème d'un serveur (votre serveur)
4. Paramètres du serveur › **Apparence** : un accent, un dégradé, une image de fond, une police, et ce CSS :
   ```css
   .msg { border-radius: 10px; }
   .msg:hover { background: rgba(255, 255, 255, .04); }
   .msg { position: fixed; background: url(https://example.org/x.png); }
   ```
   L'éditeur annonce « Sera ignoré : propriété « position » ; chargement externe… ». **Enregistrer** échoue avec l'`url(` (le serveur la refuse déjà) : retirez la dernière ligne, puis enregistrez.
5. Fermez les paramètres : les salons, les messages et la liste des membres ont le thème ; **la liste des serveurs à gauche et votre barre en bas à gauche gardent vos couleurs**. Les fenêtres (clic droit › Expulser…, confirmations) gardent l'apparence normale.
6. Avec un autre compte sur le serveur : il voit le thème. Paramètres › Apparence → décocher « Le thème d'un serveur passe avant mon CSS » avec votre CSS d'exemple : l'accent orange revient sur le serveur. Décocher « Afficher les thèmes des serveurs et des profils » : plus aucun thème. Menu du serveur › « Ignorer le thème de ce serveur » : pareil pour ce serveur seulement.

## Cartes et profils
7. Clic sur quelqu'un dans la liste des membres (ou sur son nom dans un message) : sa **carte** s'ouvre (bannière, image, rôles, présentation). Échap ou un clic ailleurs la ferme.
8. Paramètres › Profil › **Carte de profil** : une bannière, des couleurs, un peu de CSS (`.pc-name { letter-spacing: 2px }`) → l'aperçu change en direct ; **Enregistrer la carte**. Un autre compte voit votre carte avec ce thème.
9. Menu du serveur › **Mon profil sur ce serveur** : surnom, présentation, image et bannière propres à ce serveur → la liste des membres et vos messages y montrent la nouvelle image ; sur un autre serveur, rien ne change.
10. Avec un rôle qui peut « Exclure temporairement » : clic droit sur quelqu'un qui a un profil sur le serveur › **Réinitialiser son profil ici…** → sa carte reprend son profil habituel ; le journal de modération le note.

## À savoir
- Le CSS des autres ne peut rien charger d'extérieur, ni ajouter de texte, ni sortir de sa zone : c'est voulu (adresse IP, hameçonnage).
- Mon CSS est gardé **par appareil** (non synchronisé entre vos appareils).
