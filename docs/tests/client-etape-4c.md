# Client graphique, étape 4c — Guide de test (fichiers chiffrés dans les messages privés)

> Test automatique : `make e2e-client` (scénario « private conversation files », face à `quarelctl`).

Avec deux comptes amis (par exemple le vôtre et « Test »), dans une conversation privée.

1. **Envoyer** : trombone à gauche de la zone de saisie (ou glisser-déposer un fichier sur la conversation). Le texte déjà tapé part comme légende. Pendant l'envoi : « Chiffrement et envoi… ».
2. **Images** (PNG, JPEG, GIF, WebP) : affichées directement ; un clic les agrandit. **Autres fichiers** : carte avec nom, taille, « chiffré de bout en bout » et bouton **Télécharger** (déchiffré à ce moment-là).
3. **Destinataire en ligne** : le fichier passe **directement d'appareil à appareil**, sans copie sur le serveur.
4. **Destinataire hors ligne** : une copie chiffrée est gardée par le service d'identité (25 Mo au maximum), **effacée dès que tous ses appareils l'ont récupérée**, au plus tard après 7 jours.
5. **Supprimer** un message avec fichier (corbeille) : il disparaît aussi chez l'autre, et la copie du serveur est effacée.
6. **Nouvel appareil** : après validation (étape 4b), il récupère les anciens fichiers en les demandant à un autre appareil de la conversation en ligne.

## À savoir
- Le serveur ne voit jamais le contenu ni le nom des fichiers : ils sont chiffrés sur l'appareil, la clé voyage dans le message chiffré.
- **Transfert direct entre deux réseaux différents** : `identity.quarel.app` n'a pas encore de serveur STUN/TURN (décision de l'étape 5, appels). Pour l'instant, le transfert direct ne marche donc qu'entre appareils du **même réseau local** ; sinon, la copie du serveur prend le relais automatiquement (l'envoi attend quelques secondes avant de basculer).
- Limites : 100 Mo par fichier dans l'application, 25 Mo pour la copie du serveur. Un fichier plus gros ne passe qu'en direct : un destinataire hors ligne le recevra quand vous serez connecté·e en même temps.
