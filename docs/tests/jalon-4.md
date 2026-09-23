# Jalon 4 — Guide de test (salons vocaux)

Objectif : vérifier l'audio de groupe dans les salons vocaux, le micro coupé et la sourdine, la liste des participants, et le respect des permissions `connect` et `speak`.

## Préparation

```sh
make run-identity   # terminal 1
make run-server     # terminal 2 : démarre aussi LiveKit (« voice ready » dans le journal)
```

LiveKit est déjà installé sur la machine de développement (`~/.local/bin/livekit-server`). Sans lui, le serveur démarre quand même mais indique « voice disabled ».

Deux comptes membres du serveur (ex. `alice` propriétaire et `bob`), voir les guides des jalons 2 et 3.

> **Micro et navigateurs :** un navigateur n'autorise le micro que sur `localhost` ou en HTTPS. Tant que le HTTPS n'est pas en place (prévu en P1), tester **sur la même machine**, en rejoignant le serveur avec l'adresse `localhost:8090` (pas `127.0.0.1` ni l'IP du réseau local). Pour deux personnes sur la même machine : deux navigateurs différents, ou une fenêtre normale + une fenêtre de navigation privée. Un casque évite l'effet larsen.

## Scénarios

### 1. Rejoindre un salon vocal
1. `./bin/quarelctl -p alice voice-test` → affiche une adresse `http://localhost:8090/voice-test/#token=…`. L'ouvrir dans le navigateur.
2. Même chose pour bob dans un autre navigateur (ou une fenêtre privée).
3. Chacun clique **Rejoindre** sur « Général » ; autoriser le micro.
4. Parler : on s'entend mutuellement ; le nom de celui qui parle passe en vert.
5. Dans un terminal : `./bin/quarelctl -p alice voice` → les deux participants sont listés.
6. `./bin/quarelctl -p alice listen` affiche en direct les arrivées et départs du vocal.

### 2. Micro et sourdine
1. Bob clique **Couper le micro** → alice ne l'entend plus ; « micro coupé » s'affiche à côté de son nom chez alice.
2. Bob clique **Sourdine** → il n'entend plus rien, et son micro est coupé aussi.
3. **Quitter** → bob disparaît de la liste chez alice.

### 3. Changer de salon
1. `./bin/quarelctl -p alice channel-create Jeux voice`
2. Bob, dans « Général », clique **Rejoindre** sur « Jeux » → il quitte automatiquement « Général » (un seul salon vocal à la fois).

### 4. Permissions
1. `./bin/quarelctl -p alice override Général member:bob deny=speak` → pendant l'appel, bob n'est plus entendu ; sa page indique « Vous n'avez plus le droit de parler » et « (ne peut pas parler) » s'affiche.
2. `./bin/quarelctl -p alice override-clear Général member:bob` → bob peut de nouveau parler (réactiver son micro si besoin).
3. `./bin/quarelctl -p alice override Général member:bob deny=connect` → bob est éjecté du salon vocal et ne peut plus le rejoindre.
4. Expulser ou bannir quelqu'un qui est en vocal le déconnecte aussi.
5. Supprimer le salon vocal pendant un appel déconnecte ses participants.

### 5. Docker (optionnel)
```sh
make docker-server
docker run --rm --network host -e QUAREL_TRUSTED_ISSUERS=localhost:8080 -v quarel-server:/data quarel-server
```
Ports utilisés : 8090/tcp (API + signalisation vocale), 7881/tcp et 7882/udp (audio). `--network host` est le plus simple sous Linux.

## Pas encore couvert (prévu)
- Vocal depuis une autre machine : nécessite le HTTPS (P1).
- Serveur accessible depuis Internet : ouverture des ports 7881/tcp et 7882/udp (UPnP prévu en P1) et `QUAREL_VOICE_PUBLIC_IP=auto`.
- Vidéo et partage d'écran (P1).

## Retour de test

Pour chaque anomalie : la commande ou l'action, le résultat obtenu, le résultat attendu. Pour l'audio : navigateur utilisé et contenu du journal en bas de la page de test.
