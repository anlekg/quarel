# Client graphique, étape 3 — Guide de test (vocal et vidéo)

> Test automatique : `make e2e-client` (scénario « voice »).

Sur `https://app.quarel.app` (ou l'application), serveur `test.quarel.app`. Le mieux : à deux, sur deux réseaux différents (par exemple un téléphone en 4G), pour vérifier que le vocal passe par Internet.

1. **Rejoindre** : cliquer sur le salon vocal « Général ». Le navigateur demande l'accès au micro (et plus tard à la caméra). La barre **« Vocal connecté »** apparaît en bas à gauche ; vos noms apparaissent sous le salon.
2. **S'entendre** : parler ; le cadre de la personne qui parle et son nom sous le salon passent en vert.
3. **Micro / sourdine** : boutons de la barre ou du bas de la vue. Les autres voient l'icône micro barré ou casque barré sous le salon.
4. **Caméra** et **partage d'écran** : une tuile vidéo apparaît chez tout le monde. Dans l'application de bureau, un sélecteur propose les écrans et les fenêtres.
5. **Naviguer** : aller lire un salon textuel ; la barre « Vocal connecté » reste, un clic sur le nom du salon ramène à la vue vocale.
6. **Modération** (propriétaire ou rôle avec les permissions) : `quarelctl voice-mute <membre>` (bientôt dans l'application, étape « Paramètres ») → « Micro coupé par la modération », bouton micro désactivé ; `voice-move` → la personne est déplacée ; `voice-kick` → elle est déconnectée.
7. **Quitter** : bouton rouge ; plus personne sous le salon.

## À regarder en particulier
Qualité et délai du son entre deux réseaux différents, écho, fiabilité de l'indicateur « en train de parler », reconnexion après une coupure réseau.
