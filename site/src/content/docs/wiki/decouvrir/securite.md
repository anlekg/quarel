---
title: Sécurité et chiffrement
description: Comment Quarel protège vos comptes, vos messages privés, vos appels et vos connexions aux serveurs.
sidebar:
  order: 3
---

Quarel n’invente pas sa cryptographie : il s’appuie sur des bibliothèques éprouvées (Olm/Megolm via **vodozemac**, la bibliothèque de Matrix ; Ed25519, X25519, XChaCha20-Poly1305, HPKE).

## Votre compte

- Mots de passe hachés avec **argon2id** ; double authentification **TOTP** (application d’authentification) avec 10 codes de secours.
- Blocage automatique après des échecs répétés (par compte et par adresse IP), sans révéler si un compte existe.
- Mot de passe oublié : un code par email, **et la double authentification reste exigée**. Toutes les sessions sont alors fermées.

## Messages privés et fichiers

- **Chiffrés de bout en bout** avec Olm (entre appareils) et Megolm (par conversation), le protocole de Matrix. Le service d’identité ne voit que des données illisibles et les efface dès que tous les appareils destinataires les ont reçues.
- **Chaque appareil a ses clés.** Un nouvel appareil doit être **validé** par un appareil déjà validé, après comparaison d’un code affiché sur les deux écrans. Sans cela, il ne reçoit rien.
- **Phrase de récupération** : 12 mots (liste BIP-39 française) qui chiffrent une sauvegarde de vos clés et de votre historique, stockée sur le service sous forme illisible. Si vous perdez tous vos appareils, elle seule permet de tout retrouver : notez-la sur papier.
- Les **fichiers** envoyés en privé sont chiffrés sur votre appareil, transmis de préférence directement aux appareils en ligne (pair à pair), sinon déposés chiffrés sur le service, puis effacés dès réception (7 jours au plus).
- Le contact avec chaque personne est **épinglé** au premier échange : si ses clés changent de façon suspecte, l’application refuse d’envoyer et vous prévient.

## Appels

Les appels entre amis sont **pair à pair** et chiffrés (DTLS-SRTP). La mise en relation elle-même voyage dans des messages chiffrés : le service ne voit ni vos adresses IP ni les paramètres de l’appel. Si aucun chemin direct n’existe, le relais de votre service d’identité transmet le flux sans pouvoir le lire ; vous pouvez le refuser (Paramètres › Voix et vidéo).

## Connexion aux serveurs communautaires

- **Identité portable** : un jeton signé par votre service, lié à la clé de votre appareil. Chaque connexion prouve la possession de cette clé en signant un défi à usage unique : un jeton volé seul ne suffit pas.
- **Certificat lié à l’identité du serveur** : les liens d’invitation contiennent l’identifiant du serveur (`sid`). L’application vérifie pendant la connexion HTTPS que le serveur détient la clé correspondante, sans autorité de certification. Un intermédiaire ne peut pas se faire passer pour lui.
- Les serveurs communautaires ne reçoivent jamais votre email.

## L’application

- Secrets chiffrés par le trousseau du système (bureau) ou par une clé non exportable du navigateur (version web).
- Interface isolée (Electron en bac à sable, politique de sécurité du contenu stricte), liens externes ouverts dans votre navigateur.
- **Mises à jour signées** : chaque version est signée par la clé de publication de Quarel ; l’application refuse toute mise à jour qui ne l’est pas, même venant de notre propre site.

## Limites à connaître

- Les **salons des serveurs** ne sont pas chiffrés de bout en bout : l’hébergeur du serveur peut les lire.
- Pour le vocal des salons, votre adresse IP est visible du serveur vocal de ce serveur.
- Les installateurs ne sont pas encore signés par un certificat d’éditeur (avertissement de Windows).

Une faille ? Merci de la signaler en privé plutôt que dans un ticket public : voir [Contribuer](/wiki/developper/contribuer/).
