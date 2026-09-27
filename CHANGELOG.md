# Journal des versions

Quarel est en **version de test** : les versions `0.x` peuvent encore changer en profondeur.

## Prochaine version (non publiée)

> **À mettre à jour ensemble.** Serveurs communautaires (schéma 14) et services d'identité (schéma 14) d'abord, application ensuite.

### Personnalisation
- **Mon CSS** (Paramètres › Apparence) : votre propre CSS dans l'application, sur cet appareil, avec un **mode sans échec** (Ctrl + Maj + 0) si un CSS la rend inutilisable.
- **Thème d'un serveur** (Paramètres du serveur › Apparence) : couleurs, dégradé, image de fond, police et CSS, sur les salons, les messages, les membres et le vocal du serveur.
- Le thème d'un serveur **passe avant votre CSS** par défaut ; on peut inverser, ignorer le thème d'un serveur, ou ceux de tout le monde.
- **Cartes de profil** : un clic sur une personne montre sa bannière, son image, ses rôles et sa présentation, dans son thème.
- **Profil par serveur** : surnom, présentation, image, bannière et thème propres à un serveur (menu du serveur › Mon profil sur ce serveur) ; la modération peut le réinitialiser.
- **Sécurité** : le CSS des serveurs et des cartes est filtré par votre application (rien n'est chargé depuis un autre site : votre adresse IP reste privée ; pas de faux texte ni de faux boutons ; rien hors de sa zone).

### Outils
- **Migrer depuis Discord** : `tools/discord-import` recrée sur un serveur Quarel les rôles (permissions comprises), catégories, salons, droits des salons et emojis d'un serveur Discord, avec un rapport de ce qui n'a pas d'équivalent. Essai sans rien modifier, relançable sans doublon.

## 0.4.5 — 2026-09-27

Corrections du deuxième audit (sécurité et vie privée) et demandes de test. Première mise à jour **invisible** de l'application de bureau : depuis la 0.4.0, « Redémarrer » installe sans fenêtre et rouvre Quarel.

> **À mettre à jour ensemble.** Serveurs communautaires (schéma 13) et services d'identité (schéma 13) d'abord, application ensuite : la connexion par clé d'accès et l'export des données demandent les deux côtés à jour.

### Corrections demandées
- Toutes les confirmations (supprimer un salon, expulser, quitter un groupe…) s'ouvrent **dans l'application** : sous Windows, après une boîte du système, l'application de bureau ne prenait plus le clavier (impossible d'écrire après avoir supprimé un salon).
- Les **fils** et les **posts de forum** se renomment et se suppriment (roue dentée, clic droit, en-tête du fil).
- **Inviter ses amis** depuis « Inviter sur… » : une invitation personnelle envoyée en message privé ; un lien d'invitation dans un message s'affiche en carte **Rejoindre** / **Ouvrir**.
- Rejoindre un second serveur auto-signé à la même adresse coupait le premier : c'est maintenant refusé avec une explication.

### Sécurité
- **Webhooks** : nouvelle permission « Gérer les webhooks », et il faut pouvoir écrire dans le salon pour en créer un (un webhook permettait d'écrire dans un salon d'annonces sans en avoir le droit). Les rôles qui géraient les salons la reçoivent.
- **Clés d'accès** : la page de connexion indique l'appareil, l'adresse et l'heure, puis un **code à taper dans l'application** ; email à l'ajout et au retrait d'une clé (le retrait demande le mot de passe).
- **Fils et posts** : 5 créations par 10 minutes et par membre ; **archivés** après une semaine sans message (hors de la liste des salons, toujours lisibles ; un message les rouvre).
- **Notifications web** envoyées seulement aux services des navigateurs.
- **Modération automatique** : plus trompée par les caractères invisibles ni les lettres imitées (cyrillique, grec, pleine chasse) ; s'applique aussi aux noms de fils et aux surnoms.

### Vie privée
- **Qui voit votre adresse IP** (Paramètres › Voix et vidéo) : par défaut, seulement vos amis ; avec les autres membres d'un groupe, appels par le relais et fichiers par la copie chiffrée.
- **Messages éphémères** jamais sauvegardés ni transmis à un nouvel appareil ; notifications sans leur texte ni celui des divulgâcheurs ; pas d'aperçu pour un lien caché.
- **Compte supprimé** : son nom devient « Ancien compte » sur les serveurs rejoints.
- **Télécharger mes données** (Paramètres › Confidentialité, et « Mes données sur ce serveur » dans le menu d'un serveur).

## 0.4.0 — 2026-09-27

La plus grosse mise à jour depuis le début : toute la liste « plus tard » (P2) du plan, et du confort pour l'application de bureau.

> **À mettre à jour ensemble.** Les nouveautés marquées ★ ne marchent qu'entre applications 0.4.0 (l'application se met à jour seule ; la version web est toujours à jour). Serveurs communautaires et services d'identité auto-hébergés : mettez-les à jour aussi (installateur Windows plus récent, ou nouvelle image Docker) — leurs bases sont migrées automatiquement, avec une copie de sécurité.

### Appels et vocal
- ★ **Partage d'écran pendant un appel** entre amis (un écran ou une fenêtre ; sous Windows avec le son du système), les deux personnes en même temps si elles veulent.
- ★ **Appels de groupe** dans les groupes de messages privés, jusqu'à 6 personnes, en direct entre vous (aucun serveur ne voit le son ni la vidéo) : sonnerie, « Rejoindre l'appel », caméra et partage d'écran.
- **Salons « scène »** : un salon vocal où le public écoute et **lève la main** ; la modération invite à parler.
- **Ping** : barres de signal à gauche de « Vocal connecté », dans la barre d'appel et sur chaque participant·e (valeur au survol).
- **Volume par personne** (jusqu'à 200 %) et « Rendre muet pour moi », au clic droit.

### Messages privés et amis
- **Messages éphémères** : durée de vie réglable par conversation (5 min à 7 jours), par n'importe quel membre ; chaque appareil efface lui-même messages et fichiers.
- **Qui peut vous demander en ami** : tout le monde, les amis de vos amis et membres de vos groupes, ou personne.

### Serveurs communautaires
- **Modération automatique** : mots interdits, liens, trop de mentions, messages répétés, exclusion automatique ; chaque refus est journalisé (sans le texte).
- **Emojis personnalisés** (100 par serveur), dans les messages (`:nom:`) et les réactions.
- **Salons forum** : des posts avec un titre, chacun sa discussion, les plus actifs en tête.
- **Webhooks entrants** : une adresse secrète pour publier dans un salon (supervision, sauvegardes, formulaires…).
- **Commandes slash** pour les bots (`/ping`, `/echo` dans le bot d'exemple), avec réponses publiques ou visibles par vous seul·e.
- **Mise en forme** : titres, listes, citations, barré, souligné, divulgâcheurs `||…||`, langage des blocs de code.

### Application
- **Clic droit** partout : sur une personne (volume, message, ami, modération selon vos droits), un message (réagir, répondre, épingler, copier…), un salon (marquer comme lu, modifier).
- **Zone de notification** (Windows) : la croix garde Quarel près de l'horloge ; messages et appels continuent d'arriver.
- **Mises à jour invisibles** : « Redémarrer » installe sans fenêtre et rouvre Quarel (à partir de la prochaine mise à jour).
- **Vérification d'intégrité** (Windows) : l'application refuse de démarrer si ses fichiers ont été modifiés après l'installation.
- **Version web installée sur téléphone** : notifications même application fermée, par un simple réveil **sans contenu ni expéditeur** (messages privés et appels).

### Compte et sécurité
- **Clés d'accès** (Windows Hello, clé de sécurité, téléphone) en double authentification, à côté du code à 6 chiffres.
- Les connexions des applications antérieures à 0.3.0 sont refusées (ancienne preuve de connexion).

### Corrections
- Un salon qui venait d'être créé n'avait aucun droit dans l'application avant une reconnexion (pas de zone de saisie).

## 0.3.0 — 2026-09-26

Audit de sécurité appliqué : preuve de connexion liée au serveur réellement contacté, pages d'administration en HTTPS, blocage anti-force-brute revu, sessions inactives fermées après 90 jours, quotas des boîtes aux lettres et des fichiers, codes de sécurité entre contacts, membres de groupe confirmés, transfert de propriété d'un serveur.

## 0.2.0 — 2026-09-26

Première version publique de test : application de bureau (Windows, Linux) et version web, serveurs communautaires et service d'identité (Docker, installateurs Windows), messages privés et appels chiffrés de bout en bout.
