---
title: Nouveautés
description: "Le journal des versions de Quarel : ce qui change à chaque mise à jour."
sidebar:
  order: 5
---

Quarel est en **version de test** : les versions `0.x` peuvent encore changer en profondeur. Le même journal est dans le dépôt ([CHANGELOG.md](https://github.com/anlekg/quarel/blob/main/CHANGELOG.md)) et sur la [page des versions GitHub](https://github.com/anlekg/quarel/releases).

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
