---
title: MCP de la documentation
description: Brancher un assistant IA (Claude, etc.) sur la documentation de Quarel grâce au Model Context Protocol.
sidebar:
  order: 3
---

Le **serveur MCP de la documentation** permet à un assistant IA compatible avec le [Model Context Protocol](https://modelcontextprotocol.io) de **chercher et lire ce wiki** : il répond à vos questions sur l’hébergement, l’API ou les bots en s’appuyant sur la documentation à jour, et cite les pages utilisées.

Adresse : **`https://quarel.app/mcp`** (transport HTTP « streamable », sans compte, en lecture seule).

## Outils proposés

| Outil | Rôle |
|---|---|
| `search_docs` | recherche plein texte (accents et majuscules ignorés), renvoie les meilleures sections avec leur adresse |
| `read_page` | une page entière en Markdown |
| `list_pages` | toutes les pages, avec leur résumé |

Chaque page est aussi disponible comme **ressource** MCP (adresse de la page sur le site).

## Le brancher

**Claude Code** :

```sh
claude mcp add --transport http quarel-docs https://quarel.app/mcp
```

**Claude (claude.ai ou application)** : Paramètres › Connecteurs › **Ajouter un connecteur personnalisé**, adresse `https://quarel.app/mcp`.

**Autres clients** (fichier de configuration JSON) :

```json
{
  "mcpServers": {
    "quarel-docs": { "type": "http", "url": "https://quarel.app/mcp" }
  }
}
```

Puis demandez par exemple : « Comment sauvegarder mon serveur Quarel sous Docker ? » ou « Écris un bot Quarel en Python qui souhaite la bienvenue ».

## En local

Le serveur fait partie du dépôt (`cmd/quarel-docs-mcp`, Go) et peut tourner chez vous, sur la documentation de votre copie :

```sh
go run ./cmd/quarel-docs-mcp -docs site/src/content/docs -stdio        # pour un assistant local (stdio)
go run ./cmd/quarel-docs-mcp -docs site/src/content/docs -addr 127.0.0.1:8095   # HTTP, sur /mcp
```

## Sans MCP

Le site publie aussi [`/llms.txt`](/llms.txt) (sommaire) et [`/llms-full.txt`](/llms-full.txt) (toute la documentation en un fichier), à donner directement à un assistant.
