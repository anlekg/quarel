# Test — mise à jour automatique de l'application de bureau

Les versions sont publiées sur `https://app.quarel.app/updates`, signées par la clé de publication de Quarel.

## Une seule fois : installer la 0.2.0
Les versions 0.1.x ne savent pas se mettre à jour. Installez **une fois** la 0.2.0 :
- Windows : `https://app.quarel.app/updates/Quarel-Setup-0.2.0.exe` (avertissement SmartScreen : « Informations complémentaires » › « Exécuter quand même ») ;
- Linux : `https://app.quarel.app/updates/Quarel-0.2.0-x86_64.AppImage` (rendre exécutable) ; le `.deb` fonctionne aussi mais ne se met pas à jour seul.

Vos données (compte, messages, serveurs) sont conservées.

## À vérifier
1. Paramètres › **À propos** : version 0.2.0, « Quarel est à jour » (après « Rechercher » ou 15 s après le lancement).
2. Quand la version suivante sera publiée (on vous préviendra) : dans les 6 h, ou tout de suite avec « Rechercher », l'état passe à « Téléchargement… » puis un bandeau **« Mise à jour … prête — Redémarrer »** apparaît en bas de la colonne de gauche.
3. « Redémarrer » : Quarel se ferme, s'installe (Windows : fenêtre d'installation brève) et se relance dans la nouvelle version ; vous êtes toujours connecté·e.
4. Sans cliquer : fermer Quarel installe la mise à jour ; le lancement suivant est dans la nouvelle version.
5. Avec le `.deb` : « À propos » annonce la nouvelle version avec un bouton « Télécharger ».

Si « Mise à jour refusée : elle n'est pas signée par Quarel » s'affiche, prévenez-nous : c'est ce qui se passerait si quelqu'un modifiait les fichiers du site.
