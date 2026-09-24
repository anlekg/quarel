# P1 bloc 5 — Guide de test (messages privés avancés)

> Test automatique : `make e2e-dm-groups` (22 vérifications).

## Préparation
`make run-identity`, comptes `alice`, `bob`, `carol`, `dave` ; alice est amie des trois autres (bob et carol ne sont **pas** amis entre eux). Chacun lance `quarelctl -p <nom> e2e` une fois.

## 1. Groupes
1. `-p alice dm-group Tarot bob carol` → groupe de 3. `-p alice dms` le liste.
2. `-p alice dm Tarot "Rendez-vous jeudi"` ; bob et carol le lisent (`dm-history Tarot`), et carol lit aussi les messages de bob bien qu'ils ne soient pas amis.
3. `-p alice dm-add Tarot dave` → dave lit les messages suivants, **pas** les anciens (voulu).
4. `-p alice dm-kick Tarot carol` → carol ne reçoit plus rien ; les autres continuent (nouvelle clé). `dm-leave Tarot` pour partir ; si le créateur part, le groupe passe au plus ancien membre.

## 2. Modifier, supprimer
`dm-history Tarot` affiche des numéros (`#12`). `-p bob dm-edit Tarot 12 texte corrigé` → « (modifié) » chez tout le monde ; `dm-delete Tarot 12` → disparaît partout. Impossible sur le message de quelqu'un d'autre.

## 3. Fichiers chiffrés
`-p alice dm-file Tarot photo.jpg "la photo"` → 📎 chez les autres ; `-p bob dm-download Tarot <n°>` → fichier déchiffré dans le dossier courant. Le serveur ne garde que du chiffré, 30 jours : ensuite le fichier n'existe que sur les appareils qui l'ont téléchargé.

## 4. En train d'écrire, accusés de lecture
1. `-p bob dm-listen` ; `-p alice dm-typing Tarot` → « … alice écrit » ; `-p alice dm-read Tarot` → « 👁 vu par alice ».
2. `-p alice privacy typing=off receipts=off` → bob ne voit plus rien venant d'alice. `privacy` seul affiche les réglages.
