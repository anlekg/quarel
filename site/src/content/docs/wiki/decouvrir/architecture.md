---
title: Architecture
description: Les trois pièces de Quarel — service d’identité, serveurs communautaires, application — et ce que chacune voit.
sidebar:
  order: 2
---

Quarel repose sur trois pièces indépendantes.

## Le service d’identité

Il gère les **comptes** (email, pseudo, mot de passe, double authentification), les **amis**, et transporte les **messages privés chiffrés**. Il délivre à l’application une **identité portable** : un jeton signé qui prouve « je suis `pseudo@identity.quarel.app` » auprès des serveurs communautaires.

- L’instance officielle est `identity.quarel.app`. Le logiciel est libre : une association, une école ou une famille peut [héberger le sien](/wiki/heberger/service-identite/).
- **Chaque service d’identité est une bulle** : amis, messages privés et appels ne relient que des comptes du même service.
- Il héberge aussi le **relais d’appels** de ses utilisateurs, utilisé seulement quand deux personnes ne peuvent pas se joindre directement.

## Les serveurs communautaires

Chaque serveur (l’équivalent d’un « serveur Discord ») tourne **chez son hébergeur**, isolé de tous les autres : ses salons, ses rôles, ses règles, sa modération, ses fichiers. Il intègre un serveur vocal (LiveKit) pour les salons vocaux.

- Il **accepte les comptes** des services d’identité qu’il choisit (par défaut `identity.quarel.app`) et vérifie les identités portables sans contacter le service.
- Un membre est reconnu par son identifiant stable chez son service (pas par son pseudo, qui peut changer) : les bannissements tiennent même après un changement de pseudo.
- Les **bannissements sont propres à chaque serveur**. Le service d’identité ne peut que désactiver un compte (sur décision de justice), jamais bannir d’un serveur.
- Les salons des serveurs **ne sont pas chiffrés de bout en bout** : l’hébergeur peut les lire, comme sur tout forum ou chat de groupe. Choisissez des serveurs dont vous connaissez l’hébergeur.

## L’application

Application de bureau (Windows, Linux) et version web, avec la même interface. Elle :

- garde vos **clés de chiffrement** sur votre appareil ;
- chiffre et déchiffre les messages privés et les fichiers qu’ils contiennent ;
- **vérifie chaque serveur** : un serveur a une clé d’identité, et son certificat HTTPS est lié à cette clé. L’application le vérifie sans autorité de certification, même sans nom de domaine ;
- se connecte directement à chaque serveur communautaire.

## Qui voit quoi ?

| | Service d’identité | Serveur communautaire | Relais d’appels |
|---|---|---|---|
| Votre email | oui | non | non |
| Vos messages privés | **non** (chiffrés) | — | — |
| Messages des salons | — | oui (son hébergeur) | — |
| Les serveurs que vous fréquentez | non ¹ | le sien | — |
| Vos appels | non | — | flux chiffré, illisible |
| Vos amis | oui | non | non |

¹ Sauf si le service est en mode « serveurs approuvés » : il voit alors les connexions, sans les conserver.
