# Essai à plusieurs (avant l'ouverture publique)

Objectif : 5 à 10 personnes utilisent Quarel « pour de vrai » pendant une à deux semaines, sur leurs propres machines et réseaux, pour trouver ce que les tests automatiques ne voient pas.

## Préparation (CP)

1. Créer une invitation d'inscription par personne : page d'administration du service d'identité (`http://192.168.1.25:18081`) › Invitations.
2. Créer un lien d'invitation vers le serveur de test : dans l'application, menu du serveur › Paramètres du serveur › Invitations (plusieurs utilisations, 14 jours).
3. Envoyer à chacun·e le message ci-dessous.

## Message à envoyer

> Salut ! Je teste **Quarel**, une alternative libre à Discord où chaque serveur tourne chez quelqu'un. Tu veux bien l'essayer avec nous une semaine ou deux ?
>
> 1. Installe l'application : https://quarel.app/#telecharger (Windows : « Informations complémentaires » › « Exécuter quand même », l'installateur n'est pas encore signé) — ou utilise https://app.quarel.app dans ton navigateur.
> 2. « Créer un compte » avec ce code d'invitation : `XXXXXXXX`
> 3. Rejoins notre serveur : `<lien d'invitation>`
> 4. Crée ta **phrase de récupération** (bandeau de Messages privés) et note-la sur papier.
>
> Utilise-le normalement : discute, passe en vocal, partage ton écran, ajoute des amis, écris-leur en privé, appelle-les, installe-le sur un 2e appareil. Si quelque chose coince, dis-le dans #bugs avec ce que tu faisais (et une capture), ou sur https://github.com/anlekg/quarel/issues une fois le dépôt public.

## Ce qu'on veut apprendre

- **Réseaux réels** : vocal et partage d'écran depuis des box différentes, en 4G/5G, derrière un réseau d'entreprise ; appels privés directs ou par le relais (chemin affiché dans la barre d'appel).
- **Windows natif** : installation, notifications, micro/caméra, son du système au partage d'écran, mise à jour automatique (on publiera une 0.2.1 pendant l'essai).
- **Plusieurs appareils** : validation d'un 2e appareil, restauration avec la phrase, serveurs retrouvés partout.
- **Compréhension** : ce qui est déroutant quand on vient de Discord, ce qui manque le plus (pour choisir la suite de la feuille de route).

## Suivi

- Salon `#bugs` sur le serveur de test ; recopier chaque problème dans un ticket GitHub.
- À la fin : un court sondage (3 questions : ce qui a plu, ce qui a bloqué, ce qui manque le plus).
