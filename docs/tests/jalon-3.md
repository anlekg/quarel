# Jalon 3 — Guide de test (rôles, permissions, modération)

Objectif : vérifier les rôles et leur hiérarchie, les droits par salon et par catégorie, l'expulsion et le bannissement.

## Préparation

Comme au jalon 2 : `make run-identity`, `make run-server`, puis trois comptes (ex. `alice` propriétaire, `bob`, `carol`) qui ont rejoint le serveur. Voir `docs/tests/jalon-2.md`.

> Un serveur créé au jalon 2 est mis à jour automatiquement au démarrage : il obtient le rôle `@everyone`, et ses membres gardent leurs accès.

`./bin/quarelctl permissions` affiche la liste des permissions et leur signification. Les membres se désignent par leur pseudo, les rôles par leur nom.

## Rappel des règles

- Chaque membre a le rôle `@everyone` ; les autres rôles s'ajoutent. Le propriétaire a toujours tous les droits.
- **Hiérarchie** : les rôles sont classés (`roles` les affiche du plus haut au plus bas). On ne peut gérer, attribuer, expulser ou bannir que ce qui est **strictement en dessous** de son rôle le plus haut, et on ne peut pas accorder une permission qu'on n'a pas.
- **Droits par salon** : une catégorie ou un salon peut autoriser/refuser des permissions à un rôle ou un membre. Ordre : permissions des rôles → catégorie → salon ; à chaque niveau `@everyone`, puis les rôles, puis le membre lui-même (le plus précis gagne).
- Sans `view_channel`, un salon est invisible : ni dans la liste, ni ses messages, ni ses événements en direct.

## Scénarios

### 1. Rôles
1. `./bin/quarelctl -p alice roles` → seul `@everyone` (voir, écrire, inviter, vocal).
2. `./bin/quarelctl -p alice role-create Modo kick_members ban_members manage_messages manage_roles`
3. `./bin/quarelctl -p alice role-add bob Modo` puis `members` → `[Modo]` à côté de bob.
4. `./bin/quarelctl -p alice role-edit Modo color=#3498db mentionable=true`.
5. `./bin/quarelctl -p bob my-perms` → ses permissions sur le serveur et dans chaque salon.

### 2. Hiérarchie
1. Bob crée un rôle `Helper manage_messages` → OK ; `role-create Admin administrator` → `missing_permissions` (il n'a pas cette permission).
2. Bob donne `Helper` à carol → OK ; donne `Modo` à carol → `role_hierarchy` (pas en dessous de son propre rôle).
3. Bob modifie le rôle `Modo` → `role_hierarchy`.
4. Bob essaie `kick alice` → `role_hierarchy` (propriétaire).
5. Donner aussi `Modo` à carol : bob ne peut plus l'expulser (rang égal).
6. `role-edit Helper position=2` par alice → l'ordre change dans `roles`.

### 3. Catégorie privée
1. `channel-create Staff category`
2. `override Staff role:everyone deny=view_channel` puis `override Staff role:Modo allow=view_channel`
3. `channel-create modération text Staff`
4. `./bin/quarelctl -p carol channels` → ni `Staff` ni `modération` ; `send modération …` → salon introuvable.
5. `./bin/quarelctl -p bob channels` → `Staff 🔒` et `modération` visibles.
6. Carol lance `listen` ; bob écrit dans `modération`, alice dans `général` → carol ne reçoit **que** le message de `général`.
7. Donner `Modo` à carol pendant qu'elle écoute → « vos droits ont changé » et le salon apparaît.

### 4. Droits d'un membre dans un salon
1. `override général member:carol deny=send_messages` → carol lit mais ne peut plus écrire (`missing_permissions`).
2. `override-clear général member:carol` → elle peut de nouveau écrire.
3. `override général role:Modo allow=kick_members` → refusé : seules les permissions de salon sont réglables (`permissions` indique lesquelles).

### 5. Mentions
1. Carol écrit `@everyone salut` → le texte est publié mais **personne n'est notifié** (il faut `mention_everyone`).
2. Alice (propriétaire) écrit `@everyone` → notification.
3. Un rôle `mentionable=true` peut être mentionné par tous avec `<@&id_du_rôle>` (l'id est affiché par `roles`).

### 6. Expulsion et bannissement
1. `./bin/quarelctl -p bob kick carol spam` → carol est déconnectée (son `listen` s'arrête) ; elle peut revenir avec une nouvelle invitation, **sans ses anciens rôles**.
2. `./bin/quarelctl -p bob ban carol insultes` → `bans` affiche le bannissement.
3. Carol essaie de revenir avec une invitation neuve → `banned`. Même si le serveur passe en public.
4. `./bin/quarelctl -p alice unban carol` → carol peut revenir avec une invitation.
5. Un membre parti peut être banni à l'avance avec son id (affiché par `members` avant son départ).

## Retour de test

Pour chaque anomalie : la commande lancée, le résultat obtenu, le résultat attendu.
