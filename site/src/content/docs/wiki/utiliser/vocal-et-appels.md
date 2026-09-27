---
title: Vocal et appels
description: Salons vocaux, caméra et partage d’écran ; appels entre amis.
sidebar:
  order: 4
---

## Salons vocaux

- **Cliquez sur un salon vocal** pour le rejoindre. La barre « Vocal connecté » reste visible en bas, même quand vous changez de salon ou de serveur.
- Commandes : **micro**, **sourdine** (vous n’entendez plus et votre micro se coupe), **caméra**, **partage d’écran** (un écran ou une fenêtre ; sous Windows, avec le son du système), **quitter**.
- Qui parle s’entoure de vert ; un micro coupé par la modération apparaît en rouge.
- Vous n’êtes que dans un salon vocal à la fois. Les modérateurs peuvent couper votre micro ou votre son, vous déplacer ou vous déconnecter.

Le son et la vidéo vont directement au serveur vocal du serveur communautaire, jamais par un service central.

## Scènes

Un salon vocal peut être une **scène** (conférence, soirée quiz…) : case « Scène » dans les réglages du salon. En arrivant, vous êtes dans le **public** : vous écoutez. **Lever la main** demande la parole ; les personnes qui peuvent couper le micro des autres dans ce salon parlent librement et **invitent à parler** (ou renvoient dans le public). « Quitter la scène » vous ramène dans le public ; en partant du salon, vous y revenez aussi.

## Appels entre amis

- Dans une conversation en tête à tête avec un·e ami·e : bouton **Appeler**. La sonnerie retentit sur tous ses appareils ; le premier qui répond prend l’appel.
- Pendant l’appel : micro, caméra, **partage d’écran** (un écran ou une fenêtre, les deux personnes peuvent partager en même temps ; sous Windows, avec le son du système ; il faut que l’autre personne ait Quarel 0.4.0 ou plus récent), raccrocher. La barre d’appel indique la durée et le **chemin** : réseau local, pair à pair ou relais.
- En statut « Ne pas déranger », vous ne recevez ni sonnerie ni notification.

### Appels de groupe

- Dans un groupe de messages privés : bouton **Appeler le groupe**. Les autres membres reçoivent une sonnerie (« Rejoindre » ou « Ignorer »).
- Tant qu’un appel est en cours, le groupe affiche **Rejoindre l’appel** avec le nombre de participant·es : on peut partir et revenir.
- **6 personnes au plus** : chaque participant·e envoie son son et sa vidéo directement à chacun·e des autres (aucun serveur ne voit le flux), ce qui demande plus de débit montant qu’un appel à deux.
- Micro, caméra et partage d’écran comme pour un appel à deux. Il faut Quarel 0.4.0 ou plus récent.

Les appels sont **pair à pair et chiffrés**. Quand aucun chemin direct n’existe, le relais de votre service d’identité transmet le flux chiffré. Paramètres › Voix et vidéo › décochez « Utiliser le relais si aucune connexion directe n’est possible » pour le refuser (l’appel échouera alors sans chemin direct).

## Micro, haut-parleurs, caméra

Paramètres › **Voix et vidéo** : choisir les périphériques (appliqué en direct, même en plein appel), tester le micro (vu-mètre), jouer un son de test, prévisualiser la caméra.

Dans le navigateur, le micro et la caméra demandent votre autorisation la première fois.
