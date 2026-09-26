---
title: Contribuer
description: Construire Quarel depuis les sources, lancer les tests, proposer des changements.
sidebar:
  order: 4
---

Le code est sur [GitHub](https://github.com/anlekg/quarel), sous licence **Apache-2.0**.

## Organisation du dépôt

| Dossier | Contenu |
|---|---|
| `cmd/quarel-identity`, `internal/identity` | service d’identité (Go) |
| `cmd/quarel-server`, `internal/community` | serveur communautaire (Go, LiveKit embarqué pour le vocal) |
| `cmd/quarelctl` | client de test en ligne de commande |
| `client/` | application (Electron, React, TypeScript, Vite ; chiffrement vodozemac en WebAssembly) |
| `site/` | ce site (Astro, Starlight) ; le wiki est dans `site/src/content/docs/wiki/` |
| `cmd/quarel-docs-mcp`, `internal/docsmcp` | serveur MCP de la documentation |
| `deploy/` | fichiers Docker Compose |

## Construire et tester

Il faut Go 1.27+, Node.js 24, et pour certains tests Docker, `xvfb-run` et livekit-server.

```sh
make build            # binaires dans bin/
make test             # tests Go
make run-identity     # service d’identité local (:8080)
make run-server       # serveur communautaire local (https://localhost:8090)
make client-dev       # application en développement
make client-test      # types et tests unitaires du client
make e2e-client       # application réelle contre de vrais services
```

Site : `cd site && npm ci && npm run dev`.

## Règles du projet

- Documentation en **français**, rédigée de façon **neutre en genre** ; code, identifiants et messages de commit en anglais.
- Dépendances : licences compatibles avec Apache-2.0 seulement (MIT, BSD, Apache-2.0, MPL-2.0…), **jamais de GPL/AGPL** ; les composants embarqués sont listés dans `NOTICE`.
- Chiffrement : uniquement des bibliothèques éprouvées, jamais de cryptographie maison.

## Signaler un problème

- **Bug ou idée** : [tickets GitHub](https://github.com/anlekg/quarel/issues).
- **Faille de sécurité** : ne l’exposez pas dans un ticket public ; utilisez le [signalement privé de GitHub](https://github.com/anlekg/quarel/security/advisories/new).
