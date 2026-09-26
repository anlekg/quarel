---
title: Statut du projet
description: Où en est Quarel, ce qui marche, ce qui arrive.
sidebar:
  order: 4
---

:::caution[Version de test]
Quarel est en **phase de test**. Les fonctions ci-dessous marchent de bout en bout et sont couvertes par des tests automatiques, mais le projet n’a pas encore été éprouvé à grande échelle. Gardez des sauvegardes de vos serveurs.
:::

## Disponible

- **Serveurs communautaires** : salons textuels, vocaux, catégories, annonces, fils ; rôles et droits par salon ; modération (exclusions temporaires, expulsions, bannissements, suppressions en masse, journal) ; règles à accepter ; vérification par SMS ; bots ; recherche ; épingles ; réactions ; fichiers ; aperçus de liens ; notifications réglables.
- **Vocal et vidéo** dans les salons : micro, caméra, partage d’écran, modération du micro et du son, déplacement entre salons.
- **Amis, messages privés et groupes chiffrés** de bout en bout, fichiers chiffrés, validation des appareils, phrase de récupération.
- **Appels** entre amis en pair à pair, avec vidéo et relais de secours.
- **Application de bureau** Windows et Linux (mises à jour automatiques signées) et **version web** installable, utilisable sur téléphone.
- **Hébergement** : Docker, installateurs Windows des serveurs, page d’administration, sauvegardes et restauration, UPnP.

## Service officiel

- Comptes : `identity.quarel.app`. Pendant la phase de test, **l’inscription se fait sur invitation** (un code d’invitation est demandé) et seuls les **serveurs communautaires approuvés** acceptent ces comptes : un nouveau serveur demande l’accès depuis sa page d’administration.
- Application web : [app.quarel.app](https://app.quarel.app).
- Un serveur de test tourne sur `test.quarel.app`.

## À venir

- Traduction du site et de l’application en anglais.
- Signature des installateurs par un certificat d’éditeur.
- Application macOS, applications mobiles natives.
- Vous avez une idée ou un bug ? [Ouvrez un ticket sur GitHub](https://github.com/anlekg/quarel/issues).
