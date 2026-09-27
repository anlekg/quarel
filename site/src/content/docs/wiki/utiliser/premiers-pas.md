---
title: Premiers pas
description: Créer un compte, rejoindre un serveur, ajouter des amis.
sidebar:
  order: 2
---

## Créer un compte

1. Ouvrez l’application (ou [app.quarel.app](https://app.quarel.app)) et choisissez **Créer un compte**.
2. Service d’identité : laissez `identity.quarel.app` (le service officiel), ou indiquez celui de votre association si elle a le sien.
3. Email, pseudo, mot de passe — et, pendant la phase de test, le **code d’invitation** qu’on vous a donné.
4. Saisissez le code à 6 chiffres reçu par email. Vous êtes connecté·e.

Votre identifiant complet est `pseudo@identity.quarel.app`. Le pseudo peut changer (une fois par jour) : vos serveurs vous reconnaissent quand même.

:::tip[Protégez votre compte]
Dans **Paramètres › Sécurité**, activez la **double authentification** et gardez les codes de secours. Puis créez votre **phrase de récupération** (bandeau de Messages privés ou Paramètres › Récupération) : c’est elle qui vous rendra vos messages privés si vous perdez vos appareils.
:::

### Clé d’accès (facultatif)

Une fois la double authentification activée, Paramètres › Sécurité › **Clés d’accès** › **Ajouter une clé** : une page de votre service d’identité s’ouvre dans le navigateur pour enregistrer une clé de sécurité, Windows Hello ou votre téléphone. À la connexion, **Utiliser une clé d’accès** remplace alors le code à 6 chiffres. Les codes de secours restent valables.

## Rejoindre un serveur

1. On vous envoie un **lien d’invitation** (`https://app.quarel.app/join#…` ou `quarel://…`).
2. Cliquez dessus : il ouvre l’application de bureau, ou la version web. Sinon, dans l’application, bouton **+** (« Ajouter un serveur ») à gauche, puis collez le lien.
3. L’application vérifie le serveur et affiche son nom : **Rejoindre**.

Certains serveurs demandent d’accepter leurs **règles**, ou de vérifier un **numéro de téléphone** par SMS (le serveur ne garde qu’une empreinte du numéro).

Vos serveurs vous suivent sur tous vos appareils validés.

## Inviter quelqu’un

Menu du serveur (son nom, en haut) › **Inviter des personnes** : vos amis sont listés, **Inviter** leur envoie en message privé une invitation valable pour une personne. Ou copiez le lien affiché (7 jours) pour quelqu’un d’autre. Un lien d’invitation reçu dans un message s’affiche en carte avec **Rejoindre** (ou **Ouvrir** si vous êtes déjà membre).

## Se repérer

- **Colonne de gauche** : vos serveurs ; en haut, l’icône **Messages privés** (amis et conversations).
- **Salons** d’un serveur : textuels (`#`), vocaux (haut-parleur), regroupés en catégories. Un clic sur un salon vocal vous y connecte.
- **Barre du bas** : votre nom (clic : statut en ligne, absent, ne pas déranger, invisible), et **Paramètres**.
- Sur un message : répondre, réagir, créer un fil, épingler (selon vos droits), modifier ou supprimer les vôtres.
- **Fils** : sans message pendant une semaine, un fil est **archivé** (il disparaît de la liste des salons) ; on le retrouve par le lien sous son message de départ, et y écrire le rouvre. Les gestionnaires des salons le renomment ou le suppriment (roue dentée ou clic droit).
- En tête de salon : **recherche**, messages épinglés, **cloche** des notifications (tous les messages, @mentions seulement, rien, sourdine).

## Forums

Un salon **forum** rassemble des posts : chacun a un titre et sa propre discussion (**Nouveau post**). Les posts les plus actifs remontent en tête. Les gestionnaires des salons en créent avec « Créer un salon » › type **Forum**.

## Mettre en forme

Dans les salons comme dans les messages privés : `**gras**`, `*italique*`, `__souligné__`, `~~barré~~`, `` `code` ``, `||divulgâcheur||` (caché jusqu’au clic) ; en début de ligne, `# Titre` (aussi `##`, `###`), `> citation`, `- liste` ou `1. liste numérotée` ; les blocs de code entre trois accents graves, avec le langage après les premiers (` ```js `). Les liens s’affichent toujours avec leur vraie adresse.

**Emojis du serveur** : tapez `:` puis le début de leur nom (Tab ou clic pour choisir) ; ils sont aussi proposés pour réagir. Les gestionnaires du serveur les ajoutent dans Paramètres du serveur › **Emojis**.

## Clic droit

Un **clic droit** ouvre les actions de ce qui est sous le pointeur :

- sur une personne (liste des membres, salon vocal, appel) : son **volume pour vous** (jusqu’à 200 %) ou « Rendre muet pour moi » — gardés sur cet appareil, pour cette personne partout —, message privé, demande d’ami, et la modération que vos droits permettent (micro, son, exclusion, expulsion…) ;
- sur un message : réagir, répondre, modifier, épingler, créer un fil, copier, supprimer ;
- sur un salon ou un fil : marquer comme lu, rejoindre le vocal, modifier, renommer, supprimer.

Les suppressions et autres actions importantes demandent toujours une confirmation dans une fenêtre de l’application.

## Ajouter des amis

Messages privés › **Amis** › **Ajouter un ami** : tapez son pseudo. Dès qu’elle accepte, vous pouvez lui écrire et l’appeler. Les amis doivent avoir un compte sur le **même service d’identité** que vous.

Paramètres › Confidentialité › **Qui peut vous demander en ami** : tout le monde, les amis de vos amis et les membres de vos groupes, ou personne. Une demande que vous avez envoyée peut toujours être acceptée.

Vous pouvez **bloquer** quelqu’un depuis la liste d’amis ou Paramètres › Confidentialité : ses messages sont aussi masqués sur les serveurs communautaires.
