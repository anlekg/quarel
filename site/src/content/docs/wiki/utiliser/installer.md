---
title: Installer l’application
description: Télécharger Quarel pour Windows ou Linux, ou l’utiliser dans le navigateur.
sidebar:
  order: 1
---

Les liens de téléchargement sont sur la [page d’accueil](/#telecharger).

## Windows

1. Téléchargez **`Quarel-Setup-<version>.exe`** (Windows 10 ou 11, 64 bits).
2. Lancez-le. L’installateur ne demande pas de droits administrateur : Quarel s’installe pour votre compte Windows, dans le dossier de votre choix.
   > Les installateurs ne sont pas encore signés : Windows SmartScreen affiche « Windows a protégé votre ordinateur ». Cliquez sur **Informations complémentaires**, puis **Exécuter quand même**.
3. Quarel se lance ; un raccourci est créé sur le bureau et dans le menu Démarrer.

## Linux

- **AppImage** (toutes distributions, recommandé) : téléchargez `Quarel-<version>-x86_64.AppImage`, rendez-le exécutable (`chmod +x Quarel-*.AppImage` ou Propriétés › Permissions) et lancez-le. Certaines distributions demandent le paquet `libfuse2` (`libfuse2t64` sur Ubuntu 24.04 et plus).
- **Paquet .deb** (Debian, Ubuntu) : `sudo apt install ./Quarel-<version>-amd64.deb`. Ce paquet ne se met pas à jour seul : l’application vous signale les nouvelles versions.

## Dans le navigateur

Ouvrez [app.quarel.app](https://app.quarel.app). Sur téléphone ou dans Chrome/Edge, vous pouvez l’**installer** comme une application (menu du navigateur › « Installer » ou « Ajouter à l’écran d’accueil »).

La version web ne peut rejoindre que les serveurs qui ont un nom de domaine et un certificat reconnu (Let’s Encrypt) : un navigateur ne sait pas vérifier le certificat lié à l’identité d’un serveur hébergé à la maison. L’application de bureau les rejoint tous.

## Mises à jour

L’application de bureau cherche une nouvelle version au lancement puis toutes les 6 heures, la télécharge en arrière-plan et affiche **« Mise à jour prête — Redémarrer »**. Sans rien faire, elle s’installe à la prochaine fermeture. Paramètres › **À propos** affiche votre version et permet de chercher tout de suite.

Chaque mise à jour est **signée** par la clé de publication de Quarel : l’application refuse tout fichier qui ne l’est pas.

## Désinstaller

- Windows : Paramètres › Applications › Quarel › Désinstaller.
- Linux : supprimez l’AppImage, ou `sudo apt remove quarel`.

Vos données locales (clés, historique des messages privés) sont dans `%APPDATA%\quarel-client` (Windows) ou `~/.config/quarel-client` (Linux). Sans elles, vous retrouvez tout avec votre [phrase de récupération](/wiki/utiliser/messages-prives/#phrase-de-récupération) ou un autre appareil validé.

## Fermer ou réduire

Sous Windows, la croix **cache Quarel près de l’horloge** (zone de notification) : messages, notifications et appels continuent d’arriver ; un clic sur l’icône rouvre la fenêtre, et **Quitter Quarel** dans son menu la ferme vraiment. Réglage : Paramètres › À propos › « Réduire dans la zone de notification à la fermeture » (désactivé par défaut sous Linux).

Les **mises à jour** se téléchargent seules ; « Redémarrer » (ou la prochaine fermeture) les installe sans fenêtre d’installation, puis Quarel se rouvre.

