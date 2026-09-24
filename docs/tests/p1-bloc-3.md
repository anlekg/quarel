# P1 bloc 3 — Guide de test (vidéo, partage d'écran, modération vocale)

> Test automatique : `make e2e-voice` (18 vérifications avec deux navigateurs réels : audio, caméra, écran, droits, modération).

## Préparation

Comme au jalon 4 : `make run-identity`, `make run-server`, comptes `alice` (propriétaire) et `bob`. Ouvrir la page de test vocal de chacun (`./bin/quarelctl -p alice voice-test`, idem pour bob) dans deux navigateurs (ou une fenêtre normale et une fenêtre privée), accepter l'avertissement de certificat, autoriser micro et caméra.

## 1. Caméra et partage d'écran
1. Les deux rejoignent « Général ». bob clique **Activer la caméra** : alice voit sa vidéo dans « Vidéo », et « 📷 caméra » à côté de son nom (`quarelctl voice` l'affiche aussi).
2. bob clique **Partager l'écran** (le navigateur demande quoi partager) : alice voit une 2e tuile « bob — écran ».
3. Retirer à bob le droit `stream` dans ce salon : `-p alice override Général member:bob deny=stream` → caméra et écran de bob s'arrêtent aussitôt, les boutons se grisent. `override-clear Général member:bob` rétablit.

## 2. Modération vocale
1. `-p alice voice-mute bob larsen` → bob ne peut plus parler ; chez alice : « micro coupé par la modération ». bob quitte et revient : **toujours coupé** (la sanction persiste). `voice-mute bob off` lève.
2. `-p alice voice-deafen bob` → bob n'entend plus rien ; `voice-deafen bob off` → il entend de nouveau, sans se reconnecter.
3. Créer un 2e salon vocal (`channel-create Réunion voice`), puis `-p alice voice-move bob Réunion` → la page de bob change de salon toute seule (« Déplacement par la modération »).
4. `-p alice voice-kick bob` → bob est déconnecté du vocal.
5. Droits : `mute_members`, `deafen_members`, `move_members` (réglables par salon) ; on ne modère que sous son rôle. `-p alice audit` montre tout.
