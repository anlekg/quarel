---
title: Migrer depuis Discord
description: "Recréer les rôles, salons, droits et emojis d’un serveur Discord sur votre serveur Quarel, avec l’outil d’import."
sidebar:
  order: 4
---

L’outil **quarel-discord-import** lit votre serveur Discord et recrée sa **structure** sur votre serveur Quarel. Il faut **deux bots** : un bot Discord qui lit, et un bot Quarel qui écrit. Comptez une quinzaine de minutes.

## Ce qui est copié

| Discord | Quarel |
|---|---|
| Rôles : nom, couleur, ordre, « afficher séparément », « mentionnable », permissions | Pareil |
| Permissions de `@everyone` | Permissions de `@everyone` |
| Catégories, salons textuels, vocaux, d’annonces, forums | Pareil |
| Salons « scène » | Salons vocaux avec la case **Scène** |
| Salons « média » | Forums |
| Sujet des salons | Sujet (1 024 caractères au plus) |
| Permissions d’un salon ou d’une catégorie pour un rôle | Droits du salon ou de la catégorie pour ce rôle |
| Emojis personnalisés | Emojis du serveur (100 au plus, 256 Ko chacun) |
| Nom du serveur (option `--rename`) | Nom du serveur |

## Ce qui n’est pas copié

- **Les membres et leurs rôles** : un compte Discord et un compte Quarel n’ont aucun lien. Invitez vos membres (menu du serveur › **Inviter sur…**), puis redonnez les rôles.
- **Les messages**, les **fils**, l’**icône** du serveur et des rôles.
- Les rôles **gérés par Discord** (bots, boosts, abonnements).
- Les exceptions de permissions **pour une personne** dans un salon.
- Les réglages sans équivalent : mode lent, salon réservé aux adultes, nombre de places d’un salon vocal, étiquettes des forums.

À la fin, l’outil liste tout ce qui n’a pas pu être copié à l’identique : lisez ce rapport avant d’inviter vos membres.

## Avant de commencer

- **Python 3.10** ou plus récent, et la bibliothèque `cryptography` :
  - Windows : installez Python depuis [python.org](https://www.python.org/downloads/), puis `py -m pip install cryptography` ;
  - Linux : `sudo apt install python3-cryptography` (ou `pip install cryptography`).
- L’outil : téléchargez [`quarel_discord_import.py`](https://raw.githubusercontent.com/anlekg/quarel/main/tools/discord-import/quarel_discord_import.py) (clic droit › Enregistrer sous), ou prenez le dossier [`tools/discord-import`](https://github.com/anlekg/quarel/tree/main/tools/discord-import) du dépôt.
- Sur Discord, la permission **Gérer le serveur** (pour ajouter un bot). Sur Quarel, être **propriétaire** du serveur, ou avoir « Gérer le serveur » et « Gérer les rôles ».

## 1. Le bot Discord (lecture seule)

1. Ouvrez le [portail des développeurs Discord](https://discord.com/developers/applications) › **New Application** › un nom (par exemple « Import Quarel ») › **Create**.
2. Onglet **Bot** › **Reset Token** › copiez le jeton et gardez-le pour l’étape 3. **Ne le partagez pas** : il donne accès à votre bot.
3. Onglet **OAuth2** › **URL Generator** : cochez **bot**, **aucune permission**, puis ouvrez le lien généré et choisissez votre serveur.

Le bot n’a besoin d’aucune permission : il lit seulement la liste des rôles, des salons et des emojis. Si des salons privés manquent dans le plan, donnez-lui temporairement « Voir les salons ».

## 2. Le bot Quarel (écriture)

Dans l’application Quarel, sur votre serveur :

1. Menu du serveur › **Paramètres du serveur** › **Bots** › nom « Importation » › **Créer**. Copiez le **jeton** `qb_…` : il ne s’affiche qu’une fois.
2. **Rôles** › créez un rôle « Importation » avec **Administrateur**, puis **montez-le tout en haut** de la liste. Un bot ne peut gérer que les rôles placés sous son propre rôle.
3. **Membres** › le bot « Importation » › donnez-lui ce rôle.
4. **Invitations** › créez une invitation et copiez le **lien** : il désigne votre serveur et permet à l’outil de vérifier qu’il parle bien à lui (même avec un certificat auto-signé).

## 3. Lancer l’import

Ouvrez un terminal dans le dossier de l’outil. **D’abord un essai**, qui montre tout ce qui sera fait sans rien modifier :

```sh
python3 quarel_discord_import.py 'https://app.quarel.app/join#…' --dry-run
```

Sous Windows, remplacez `python3` par `py`. L’outil demande les deux jetons (rien ne s’affiche pendant la saisie, c’est normal). Vous pouvez aussi les mettre dans les variables `DISCORD_BOT_TOKEN` et `QUAREL_BOT_TOKEN` : ne les écrivez jamais dans la commande elle-même, elle resterait dans l’historique du terminal.

Si le plan vous convient, lancez l’import :

```sh
python3 quarel_discord_import.py 'https://app.quarel.app/join#…' --rename --replace-defaults
```

- `--rename` : le serveur Quarel prend le nom du serveur Discord ;
- `--replace-defaults` : supprime les salons créés d’office avec le serveur Quarel (« général », « Général » et leurs catégories), **seulement** s’il n’y a encore rien d’autre et aucun message ;
- `--guild <identifiant>` : si votre bot Discord est sur plusieurs serveurs (l’outil les liste) ;
- `--no-emojis` : sans les emojis.

**Relancer** la même commande est sans risque : ce qui existe est mis à jour, ce qui manque est créé, rien n’est dupliqué. Les correspondances entre Discord et Quarel sont gardées dans `discord-import-<identifiant>.json`, à côté de l’outil : gardez ce fichier si vous comptez relancer l’import.

## 4. Après l’import

1. Lisez le **rapport** de fin et ajustez à la main ce qui doit l’être (voir ci-dessous).
2. Retirez le rôle « Importation » au bot Quarel, puis supprimez le bot (**Bots** › Supprimer) et le rôle.
3. Sur Discord, retirez le bot du serveur, ou supprimez l’application.
4. Invitez vos membres et redonnez les rôles.

## Les différences à connaître

### Permissions

| Discord | Quarel |
|---|---|
| Administrateur, Gérer le serveur, Gérer les salons, Gérer les webhooks | Mêmes permissions |
| Gérer les rôles | Gérer les rôles et les droits des salons |
| Voir les logs du serveur | Voir le journal de modération |
| Expulser, Bannir, Exclure temporairement | Mêmes permissions |
| Voir les salons, Envoyer des messages, Gérer les messages, Ajouter des réactions, Joindre des fichiers | Mêmes permissions |
| Mentionner @everyone, @here et tous les rôles | Mentionner @everyone et tous les rôles |
| Créer une invitation | Créer des invitations |
| Se connecter, Parler | Mêmes permissions |
| Vidéo | Caméra et partage d’écran |
| Rendre les membres muets, Mettre en sourdine, Déplacer des membres | Couper le micro des autres, Mettre les autres en sourdine, Déplacer et déconnecter |
| Voir les anciens messages | Pas de permission : voir un salon donne **tout son historique** |
| Créer des fils, Envoyer des messages dans les fils | « Envoyer des messages » du salon |
| Gérer les fils | « Gérer les salons » et « Gérer les messages » |
| Gérer les expressions | Les emojis demandent « Gérer le serveur » |
| Gérer les pseudos, Voix prioritaire, Événements, TTS, sondages, soundboard, activités, autocollants | Pas d’équivalent |

Le rapport signale chaque rôle qui perd une permission, et chaque **restriction qui disparaît**. Par exemple, un salon où `@everyone` ne peut pas voir les anciens messages montrera tout l’historique sur Quarel.

### Salons d’annonces

Dans Quarel, écrire dans un salon d’annonces demande **aussi** « Gérer les messages ». Vérifiez que les bonnes personnes l’ont dans ces salons.

### Catégories non synchronisées

Sur Discord, un salon **non synchronisé** avec sa catégorie ignore les permissions de la catégorie. Sur Quarel, un salon applique **toujours** les droits de sa catégorie, puis les siens. L’outil copie les droits de chaque salon. Quand le résultat peut différer, il l’écrit dans la partie « À vérifier » du rapport : ouvrez alors les droits de ce salon (roue dentée › **Permissions**) et corrigez-les si besoin.

Un salon **synchronisé** reçoit seulement les droits de sa catégorie, comme sur Discord.

## Questions fréquentes

**« le bot Quarel doit avoir un rôle avec Administrateur »** : refaites l’étape 2 (rôle Administrateur, tout en haut, donné au bot).

**« ce n’est pas le serveur attendu »** : l’adresse mène à un autre serveur que celui du lien d’invitation. L’outil s’arrête avant d’envoyer le jeton : vérifiez le lien.

**« ce serveur a un certificat auto-signé : donnez son identifiant »** : utilisez un lien d’invitation plutôt que l’adresse seule (il contient l’identifiant du serveur), ou ajoutez `--sid <identifiant>`.

**« role_hierarchy »** : le rôle du bot Quarel n’est pas tout en haut de la liste des rôles.

**Rien ne s’affiche quand je colle le jeton** : c’est voulu, le jeton reste invisible. Collez-le puis appuyez sur Entrée.
