---
title: Personnaliser Quarel
description: "Votre propre CSS, le thème d’un serveur, votre carte de profil et votre profil sur chaque serveur."
sidebar:
  order: 5
---

Quarel se personnalise à quatre niveaux :

| Quoi | Qui le choisit | Qui le voit | Où |
|---|---|---|---|
| **Mon CSS** | vous | vous seul·e, sur cet appareil | Paramètres › Apparence |
| **Thème d’un serveur** | les personnes qui peuvent « Gérer le serveur » | ses membres, sur ce serveur | Paramètres du serveur › Apparence |
| **Carte de profil** | vous | les personnes qui cliquent sur vous | Paramètres › Profil › Carte de profil |
| **Profil sur un serveur** | vous | les membres de ce serveur | menu du serveur › Mon profil sur ce serveur |

## Mon CSS

Paramètres › **Apparence** › **Mon CSS** : collez du CSS, puis **Appliquer**. Il s’applique à toute l’application, **sur cet appareil seulement** (l’application de bureau et la version web ont chacune le leur), **sans aucun filtre**. N’y collez que du CSS que vous comprenez.

Les couleurs de l’application sont des variables : changez-les sur `:root`.

```css
:root {
  --bg: #101418;        /* fond principal */
  --bg-1: #151a20;      /* colonnes */
  --bg-2: #1b2129;      /* cartes, fenêtres */
  --bg-3: #242c36;      /* champs, boutons */
  --text: #eef1f4;
  --accent: #f08a4b;    /* couleur d’accent */
  --accent-text: #f6b58c;
  --font: Georgia, serif;
}
.msg { border-radius: 10px; }
```

### Mode sans échec

L’application devient illisible à cause d’un CSS ? Appuyez sur **Ctrl + Maj + 0** : plus aucun CSS personnalisé (ni le vôtre, ni ceux des serveurs) jusqu’au prochain lancement. Un bandeau le signale, avec « Réactiver ». Vous pouvez aussi lancer l’application de bureau avec `--safe-mode`, ou ajouter `?safe` à l’adresse de la version web.

## Le thème d’un serveur

Dans Paramètres du serveur › **Apparence** (il faut « Gérer le serveur ») :

- **couleurs** de l’application (fonds, texte, accent…) ;
- **dégradé** et **image de fond** (envoyée au serveur, jamais chargée depuis un autre site) ;
- **police**, parmi celles de l’application ;
- **CSS**, filtré (voir plus bas).

Le thème s’applique à la **zone du serveur** : liste des salons, messages, liste des membres, vocal. Il ne touche jamais la liste des serveurs, votre barre en bas à gauche, les paramètres ni les fenêtres (confirmations, modération…).

Chaque membre choisit ce qu’il voit :

- **Le thème d’un serveur passe avant mon CSS** (coché par défaut). Décoché, c’est votre CSS qui l’emporte : les couleurs que vous définissez restent les vôtres, même sur un serveur à thème.
- **Afficher les thèmes des serveurs et des profils** : décoché, vous ne voyez plus aucun thème des autres.
- **Animations des thèmes** : décoché, aucun thème ne peut rien animer.
- Pour **un seul serveur** : menu du serveur › **Ignorer le thème de ce serveur**.

## Cartes de profil

Un clic sur une personne (liste des membres, nom ou image d’un message) ouvre sa **carte** : bannière, image, nom, rôles, présentation, dans le thème qu’elle a choisi. Clic droit › **Voir le profil** fait pareil.

- **Votre carte** : Paramètres › Profil › **Carte de profil** (bannière, couleurs, dégradé, police, CSS), avec un aperçu. C’est celle que voient vos amis, et les membres des serveurs où vous n’avez pas de profil propre.
- **Votre profil sur un serveur** : menu du serveur › **Mon profil sur ce serveur**. Surnom, présentation, image, bannière et thème de carte **propres à ce serveur**. Ce que vous laissez vide reprend votre profil habituel.

La modération d’un serveur (« Exclure temporairement ») peut **réinitialiser** le profil de quelqu’un sur ce serveur : clic droit › Réinitialiser son profil ici. Son profil habituel reste affiché.

## Ce que le CSS des autres ne peut pas faire

Le CSS d’un serveur ou d’une carte est **filtré par votre application** avant d’être appliqué. Un serveur malveillant ne peut donc pas s’en servir contre vous :

- **rien n’est chargé depuis un autre site** : ni `url()`, ni `@import`, ni police externe. Une simple image extérieure suffirait à révéler votre adresse IP ;
- **pas de faux texte ni de faux boutons** : ni `content`, ni `position`, ni `z-index`, ni `transform`, ni `pointer-events` ;
- **rien hors de sa zone** : la zone du serveur ou la carte, jamais le reste de l’application ;
- pas de `!important` (vos réglages gardent le dernier mot), ni d’échappements (`\`), et 16 Ko au plus.

L’éditeur du thème liste ce qui sera ignoré avant même d’enregistrer.
