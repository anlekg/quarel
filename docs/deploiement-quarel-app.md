# Instance de test quarel.app (2026-09-24)

Hébergée sur la machine du CP, derrière son infrastructure existante :

| Nom | Service | Chemin |
|---|---|---|
| `identity.quarel.app` | service d'identité (sur invitation, pas encore d'emails) | Cloudflare → tunnel « Archnet » → Nginx Proxy Manager → `:18080` |
| `test.quarel.app` | serveur communautaire « Quarel — serveur de test » | même chemin → `:18090` ; vocal en direct (7881/tcp, 7882/udp par UPnP) |
| `app.quarel.app` | client web (fichiers de `client/dist`, servis par nginx) | même chemin → `:18100` |

- **HTTPS** : fourni par Cloudflare (certificat reconnu) ; les deux services tournent en HTTP (`QUAREL_TLS=off`) et font confiance au proxy pour l'adresse des clients (`QUAREL_TRUSTED_PROXIES=172.16.0.0/12`). Le client web peut donc joindre ce serveur de test.
- **Ce qui ne passe pas par le tunnel** : l'UDP. Le vocal va directement à l'adresse publique de la box (visible des participants au vocal) ; le relais d'appels (TURN) est désactivé pour l'instant.
- **Confidentialité** : Cloudflare termine le HTTPS et voit le trafic web (messages des salons en clair ; messages privés chiffrés de bout en bout, illisibles pour lui). Accepté pour la phase de test, à rediscuter pour la production.
- **Pages d'administration** : réseau local seulement (`:18081` identité, `:8091` communautaire).
- **Mettre à jour le client web** : `cd client && npx vite build`, puis copier `dist/` dans `/DATA/AppData/quarel-deploy/web/`.
- **Fichiers** : `/DATA/AppData/quarel-deploy` (Compose, données, README de mise à jour) — hors du dépôt.
- Réglages fixés par Compose (verrouillés dans les pages) : nom public `identity.quarel.app`, `QUAREL_TLS=off`, ports, nom public `test.quarel.app` ; le reste se règle dans les pages.
