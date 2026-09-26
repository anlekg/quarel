# Instance officielle quarel.app

| Nom | Service |
|---|---|
| `quarel.app` | site, wiki, MCP de la documentation (`/mcp`), installateurs des serveurs (`/telechargements/`) |
| `app.quarel.app` | client web ; mises à jour de l'application de bureau (`/updates/`) |
| `identity.quarel.app` | service d'identité (inscriptions sur invitation, serveurs approuvés, relais d'appels) |
| `test.quarel.app` | serveur communautaire « Quarel — serveur de test » |

- **HTTPS** : fourni par Cloudflare (certificat reconnu), puis un proxy inverse ; les services tournent en HTTP (`QUAREL_TLS=off`) et ne font confiance qu'aux adresses du proxy pour `X-Forwarded-For` (`QUAREL_TRUSTED_PROXIES`). Le client web peut donc joindre le serveur de test.
- **Ce qui ne passe pas par Cloudflare** : l'UDP. Le vocal (7881/tcp, 7882/udp) et le relais d'appels (3478/udp, 49160-49200/udp) sont joints directement, ports ouverts par UPnP.
- **Confidentialité** : Cloudflare termine le HTTPS et voit le trafic web (messages des salons en clair ; messages privés chiffrés de bout en bout, illisibles pour lui). Accepté pour la phase de test, à rediscuter pour la production.
- **Pages d'administration** : réseau local seulement, jamais exposées.
- **Emails** : envoyés par un relais SMTP (STARTTLS), SPF et DKIM de `quarel.app` en place.
- Les fichiers de déploiement (Compose, configuration du proxy, données) sont gardés hors du dépôt. Publier : `make client-release`, `make windows-release`, `make site-build` (dossiers de destination dans `local.mk`, voir `Makefile`).
