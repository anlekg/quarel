# Contribuer à Quarel

Merci de votre intérêt ! Quarel est une alternative libre et auto-hébergeable à Discord, en **version de test**.

## Avant de commencer

- **Bug** : ouvrez un [ticket](https://github.com/anlekg/quarel/issues/new/choose) avec les étapes pour le reproduire.
- **Idée ou gros changement** : ouvrez d'abord un ticket pour en discuter ; les choix d'architecture sont consignés dans `PROJECT.md`.
- **Faille de sécurité** : jamais en public, voir [SECURITY.md](SECURITY.md).

## Construire et tester

Prérequis : Go 1.27+, Node.js 24 (npm 11). Pour certains tests : `xvfb-run`, `sqlite3`, [livekit-server](https://github.com/livekit/livekit) 1.13.7 dans `~/.local/bin`, Docker.

```sh
make build            # binaires dans bin/
make test             # tests Go
make run-identity     # service d'identité local (:8080)
make run-server       # serveur communautaire local (https://localhost:8090)
make client-dev       # application en développement
make client-test      # types et tests unitaires du client
make e2e-client       # application réelle contre de vrais services
make site-dev         # site et wiki (http://localhost:4321)
```

Toutes les commandes et l'architecture détaillée : `CLAUDE.md` (fiche technique). Les tests automatiques de GitHub (`.github/workflows/ci.yml`) doivent passer.

## Règles du projet

- **Langues** : documentation en français, rédigée de façon **neutre en genre** ; code, identifiants et messages de commit en anglais.
- **Licence** : en contribuant, vous acceptez que votre contribution soit publiée sous licence Apache-2.0.
- **Dépendances** : licences compatibles avec Apache-2.0 seulement (MIT, BSD, ISC, Apache-2.0, MPL-2.0…), **jamais de GPL/AGPL** ; tout composant embarqué est ajouté à `NOTICE`.
- **Chiffrement** : bibliothèques éprouvées uniquement, jamais de cryptographie maison.
- **Serveurs** : un binaire Go unique, SQLite embarqué, aucun service externe obligatoire ; ne jamais modifier une migration existante, seulement en ajouter.
- **Tests** : toute fonction nouvelle vient avec ses tests (unitaires, et de bout en bout quand elle touche au réseau ou à l'interface). Les tests n'ouvrent jamais de ports sur la box (`QUAREL_UPNP=off`).
- **Documentation publique** : le wiki (`site/src/content/docs/wiki/`) suit le code dans le même commit.

## Proposer un changement

1. Créez une branche depuis `main`.
2. Des commits courts et explicites en anglais (`feat(client): …`, `fix(identity): …`, `docs: …`).
3. Ouvrez une *pull request* qui explique le pourquoi, et comment vous avez testé.
