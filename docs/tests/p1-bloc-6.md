# P1 bloc 6 — Guide de test (appels entre amis)

> Test automatique : `make e2e-calls` (10 vérifications, avec de vrais appels WebRTC entre deux `quarelctl`).

## Préparation
Lancer le service Identity avec le relais, sur une seule machine :

```sh
QUAREL_TURN=on QUAREL_TURN_PUBLIC_IP=127.0.0.1 QUAREL_TURN_ALLOW_PRIVATE=1 make run-identity
```

(`QUAREL_TURN_ALLOW_PRIVATE=1` n'est là que pour ce test local : en production, le relais refuse les adresses privées.) Comptes `alice` et `bob` amis, `quarelctl -p … e2e` lancé une fois pour chacun.

## 1. Appel direct
1. Terminal 1 : `./bin/quarelctl -p bob call-listen` (répond automatiquement : client de test).
2. Terminal 2 : `./bin/quarelctl -p alice call bob --seconds 5` → « En communication », puis « audio reçu ✔ … chemin en direct ». Chacun envoie une tonalité à l'autre ; le résultat s'affiche des deux côtés.

## 2. Par le relais
`-p bob call-listen --relay-only` et `-p alice call bob --relay-only` → « chemin par le relais TURN ». Le son reste chiffré de bout en bout : le relais ne peut pas l'écouter.

## 3. Refuser le relais
`-p alice calls relay=off` → alice n'utilisera jamais le relais (`call … --relay-only` est refusé). `calls relay=on` pour revenir.

## 4. Confidentialité
Un non-ami ne peut pas appeler. La signalisation (adresses IP, paramètres) est chiffrée : elle n'apparaît pas dans la base du service.

Un vrai test entre deux maisons (deux box différentes) demandera le service déployé avec une IP publique ; il sera fait avec le client graphique.
