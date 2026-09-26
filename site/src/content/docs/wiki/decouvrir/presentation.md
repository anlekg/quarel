---
title: Qu’est-ce que Quarel ?
description: Quarel en bref — une alternative libre et auto-hébergeable à Discord.
sidebar:
  order: 1
---

Quarel est une **alternative libre à Discord** : salons textuels et vocaux, rôles et modération, amis, messages privés et appels. La différence : **aucune plateforme centrale ne détient les communautés**. Chaque serveur tourne chez la personne qui l’héberge (un PC, un mini-serveur, un NAS) et ses données y restent.

:::caution[Version de test]
Quarel est en phase de test. Tout fonctionne de bout en bout, mais des changements sont encore possibles et des bugs attendus. Voir [Statut du projet](/wiki/decouvrir/statut/).
:::

## Ce que vous pouvez faire

- **Rejoindre des serveurs communautaires** avec un seul compte, par un lien d’invitation.
- **Discuter** : messages avec mise en forme, réponses, réactions, fichiers, fils, épingles, recherche, notifications réglables.
- **Parler et vous montrer** dans les salons vocaux : micro, caméra, partage d’écran avec le son.
- **Écrire en privé** à vos amis, seul·e à seul·e ou en groupe (jusqu’à 10), avec un **chiffrement de bout en bout** : même le service qui transporte les messages ne peut pas les lire.
- **Appeler vos amis** directement, en pair à pair, avec la vidéo.
- **Héberger votre propre serveur** en une commande, et même votre propre service de comptes.

## Les principes

- **Auto-hébergement d’abord.** Un serveur tient dans une image Docker, avec une base de données embarquée : aucun service externe à installer. Il ouvre lui-même les ports de la box (UPnP) et se règle depuis une page web.
- **Pas de fédération.** Chaque serveur est une île : il ne parle pas aux autres serveurs. Seuls les comptes circulent, grâce à une **identité portable** signée par votre service d’identité.
- **Vie privée.** Les messages privés et les appels sont chiffrés de bout en bout. Un service d’identité réglé en mode ouvert ne sait pas quels serveurs vous fréquentez. Pas de publicité, pas de pistage.
- **Logiciel libre.** Licence Apache-2.0, code sur [GitHub](https://github.com/anlekg/quarel).

## Par où commencer ?

- Pour utiliser Quarel : [Installer l’application](/wiki/utiliser/installer/), puis [Premiers pas](/wiki/utiliser/premiers-pas/).
- Pour héberger une communauté : [Héberger un serveur communautaire](/wiki/heberger/serveur-communautaire/).
- Pour comprendre la technique : [Architecture](/wiki/decouvrir/architecture/) et [Sécurité et chiffrement](/wiki/decouvrir/securite/).
