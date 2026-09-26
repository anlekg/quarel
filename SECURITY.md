# Sécurité

Quarel protège des conversations privées : les failles de sécurité sont traitées en priorité.

## Signaler une faille

**Ne publiez pas une faille dans un ticket public.** Utilisez le signalement privé de GitHub :
[Security › Report a vulnerability](https://github.com/anlekg/quarel/security/advisories/new).

Indiquez si possible :
- le composant touché (service d'identité, serveur communautaire, application de bureau, client web, chiffrement, site) et la version ou le commit ;
- comment reproduire le problème, et ce qu'une personne malveillante pourrait en tirer ;
- une proposition de correction si vous en avez une.

Nous accusons réception sous quelques jours, vous tenons informé·e du correctif, et vous créditons dans l'avis publié si vous le souhaitez. Merci de laisser le temps de corriger et de déployer avant toute publication.

*English is fine too: please use GitHub's private vulnerability reporting.*

## Périmètre

Sont notamment concernés :
- le chiffrement de bout en bout des messages privés, des fichiers et des appels (Olm/Megolm, vodozemac) ;
- l'authentification (comptes, double authentification, jetons d'identité portables, sessions des serveurs) ;
- la vérification des serveurs communautaires (certificat lié à l'identité) ;
- les permissions et la modération des serveurs communautaires ;
- les pages d'administration, le relais d'appels, les aperçus de liens (SSRF) ;
- l'application de bureau (isolation, mises à jour signées).

Hors périmètre : les avertissements dus aux installateurs non signés par un certificat d'éditeur (connu), le contenu des salons lisible par l'hébergeur de chaque serveur (choix de conception, documenté), les attaques qui supposent déjà le contrôle de l'appareil de la personne.

## Versions suivies

Projet en **version de test** : seule la dernière version (branche `main`, dernière version publiée de l'application) reçoit les correctifs.

Documentation de la sécurité : <https://quarel.app/wiki/decouvrir/securite/>.
