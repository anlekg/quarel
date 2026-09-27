---
title: Messages privés et appareils
description: Conversations chiffrées, validation de vos appareils, phrase de récupération.
sidebar:
  order: 3
---

Les messages privés (en tête à tête ou en groupe jusqu’à 10 personnes) sont **chiffrés de bout en bout** : seuls les appareils des participant·es peuvent les lire. La mention « Chiffré de bout en bout » l’indique en haut de chaque conversation.

## Écrire

- Messages privés › choisissez un·e ami·e, ou **Nouveau groupe**.
- Vous voyez **Envoyé**, **Distribué** puis **Vu** (si la personne a laissé les accusés de lecture), et « … écrit ».
- Fichiers : trombone ou glisser-déposer (100 Mo au plus). Ils sont chiffrés sur votre appareil et envoyés en direct aux appareils en ligne ; les autres les récupèrent chiffrés plus tard.
- Paramètres › Confidentialité : désactiver « … écrit » et les accusés de lecture.
- **Messages éphémères** : l’horloge en haut de la conversation règle la durée de vie des **prochains** messages (5 minutes, 1 heure, 1 jour, 7 jours ou désactivés), pour tout le monde. N’importe quel membre peut la changer ; le changement s’affiche dans la conversation. Passé ce délai, chaque appareil efface le message et son fichier. Cela protège si un appareil est perdu ou saisi, pas contre la personne qui les reçoit : elle peut toujours faire une capture d’écran.

## Notifications sur téléphone

L’application web installée (app.quarel.app, « Installer l’application » dans les paramètres) peut vous prévenir même fermée : Paramètres › Notifications › **Même quand l’application est fermée**. Votre service d’identité envoie alors un simple réveil, **sans contenu ni expéditeur**, par le service de notifications de votre navigateur (Google, Mozilla, Apple…), qui ne peut donc rien lire ; la notification dit seulement que du nouveau vous attend. Cela concerne les messages privés et les appels, pas encore les mentions sur les serveurs.

## Ajouter un appareil

Votre historique n’est **pas** stocké en clair sur un serveur : un nouvel appareil (autre ordinateur, navigateur) doit être **validé**.

1. Connectez-vous sur le nouvel appareil : il affiche un **code** (`XXXX-XXXX-XXXX-XXXX`).
2. Sur un appareil déjà validé, un bandeau signale l’appareil en attente (ou Paramètres › Appareils). Vérifiez que le code est **le même** sur les deux écrans, puis validez.
3. Le nouvel appareil reçoit vos clés, votre historique et la liste de vos serveurs.

Ne validez jamais un appareil que vous ne reconnaissez pas : il pourrait lire vos messages.

## Phrase de récupération

Douze mots qui chiffrent une sauvegarde de vos clés et de votre historique. Créez-la dans le bandeau de Messages privés ou Paramètres › **Récupération** ; elle ne s’affiche **qu’une fois** : notez-la sur papier, hors ligne.

Si vous n’avez plus aucun appareil validé, connectez-vous sur un nouvel appareil, choisissez de restaurer avec la phrase, saisissez les 12 mots puis **Restaurer** : tout revient (historique, contacts, serveurs). Sans phrase ni appareil validé, vos anciens messages privés sont **perdus** — personne, même pas nous, ne peut les déchiffrer.

**Tout perdu** (appareils validés et phrase) ? Dans le bandeau de l’appareil non validé, « J’ai tout perdu » repart avec de nouvelles clés (mot de passe et double authentification demandés). Vos anciens messages restent illisibles, vos contacts sont prévenus que votre clé a changé.

## Code de sécurité

Dans une conversation en tête à tête, le bouton **bouclier** affiche un code de sécurité (`XXXX-XXXX-XXXX-XXXX-XXXX-XXXX`). Votre contact voit **le même code** si vos deux applications détiennent les vraies clés l’une de l’autre — et non des clés substituées par un service compromis. Comparez-le de vive voix ou par téléphone, puis « Les codes correspondent » : la conversation affiche **Vérifié**.

## Avertissement de sécurité

Si les clés d’un contact changent, l’application **refuse d’envoyer** et vous prévient. C’est normal si la personne a réinitialisé ses clés ; sinon, quelqu’un tente peut-être de se faire passer pour elle. Comparez le **nouveau** code de sécurité avec elle avant d’accepter sa nouvelle clé.

Dans un groupe, l’application ne chiffre que pour les membres qu’elle connaissait déjà et ceux qu’un membre a **annoncés** en les ajoutant (« alice a ajouté carol au groupe »). Une personne apparue dans la liste sans annonce ne reçoit rien, et l’application vous le signale.
