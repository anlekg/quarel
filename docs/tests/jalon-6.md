# Jalon 6 — Guide de test (HTTPS, UPnP, limites, phrase de récupération)

> Tests automatiques : `make e2e-security` (18 vérifications : récupération, HTTPS, limites), `make e2e-acme` (certificat Let's Encrypt via le serveur de test Pebble, Docker requis), et les anciens `make e2e-dm`, `make e2e-voice` (désormais en HTTPS).

## Préparation

`make run-identity` et `make run-server`, comme avant. **Nouveau :** le serveur communautaire est maintenant en **HTTPS** (`https://localhost:8090`) avec un certificat auto-signé. En développement, l'UPnP est désactivé (`make run-server` ne touche jamais à la box).

> Si un ancien serveur a été rejoint en `http://`, rejoignez-le à nouveau (`quarelctl join localhost:8090 …`) : les adresses sont désormais en `https://`.

## 1. HTTPS et identité du serveur

1. `./bin/quarelctl -p alice srv-info localhost:8090` → fonctionne en HTTPS sans avertissement : quarelctl vérifie que le certificat appartient bien au serveur (lien entre certificat et identifiant du serveur).
2. Créer une invitation, puis modifier le `sid=` du lien et tenter `join` → refusé dès la connexion (« possible interception »).
3. Arrêter le serveur, supprimer `data/server/`, le relancer (nouveau serveur à la même adresse) → pour un membre existant, toute commande est refusée (« possible interception ») : un autre serveur ne peut pas se faire passer pour le premier.
4. Page de test vocal (`voice-test`) : le navigateur affiche un avertissement de certificat, à accepter (le certificat est auto-signé). Le **vocal fonctionne maintenant depuis une autre machine du réseau** : rejoindre le serveur avec l'adresse IP locale du serveur (ex. `192.168.1.20:8090`) — ajouter cette IP au certificat avec `QUAREL_TLS_HOSTS=192.168.1.20` pour limiter les avertissements.

Autres modes (documentés dans `CLAUDE.md`) : `QUAREL_TLS=acme` (Let's Encrypt, avec un nom de domaine et le port 443 redirigé), `files` (son propre certificat), `off` (derrière un proxy).

## 2. Limitation de débit
1. Créer plus de 5 comptes en moins d'une heure depuis la même machine → `rate_limited`. (Les compteurs sont en mémoire : redémarrer le service Identity les remet à zéro.)
2. **Blocage anti-bruteforce par adresse** : 15 mauvais mots de passe depuis une adresse ne bloquent le compte **que pour cette adresse** ; le propriétaire se connecte normalement ailleurs. Au-delà de 100 échecs par heure toutes adresses confondues, le compte est bloqué pour tous. (Difficile à tester à la main sur une seule machine : couvert par les tests automatiques.)
3. Anti-flood : plus de 10 messages en 10 secondes dans un serveur → `rate_limited`.

## 3. Phrase de récupération
1. Sur un appareil validé : `./bin/quarelctl -p alice recovery-setup` → 12 mots à noter.
2. `recovery-status` → « Sauvegarde active ». Elle se met à jour toute seule à chaque message.
3. Simuler la perte de tous les appareils : supprimer le profil (`~/.config/quarelctl/alice.*`), puis `login` → le nouvel appareil est « en attente de validation » et sans historique.
4. `recovery-restore` avec un mot modifié → « phrase invalide » (faute détectée). Avec la phrase d'un autre compte → refusée.
5. `./bin/quarelctl -p alice recovery-restore <les 12 mots>` → « cet appareil est validé, N message(s) retrouvé(s) » ; `dm-history` montre tout, et l'appareil peut de nouveau écrire.
6. Les accents et majuscules sont ignorés à la saisie (`eleve` = `élève`).
7. Un appareil validé ensuite par `device-approve` reçoit aussi la clé de sauvegarde.

## 4. UPnP et diagnostic réseau (optionnel, sur votre vraie box)

⚠ Ce test **ouvre réellement des ports sur votre box** (ils sont refermés à l'arrêt du serveur).
1. `QUAREL_UPNP=on make run-server` → le journal indique « ports opened on the router » (ou pourquoi c'est impossible).
2. `./bin/quarelctl -p alice network` → diagnostic : ports ouverts, adresse publique, et verdict (joignable / ports à ouvrir à la main / double NAT-CGNAT).
3. Arrêter le serveur (Ctrl+C) → les ports sont refermés (vérifiable dans l'interface de la box).

## Retour de test

Pour chaque anomalie : la commande lancée, le résultat obtenu, le résultat attendu.
