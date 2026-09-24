# Client graphique, étape 4a — Guide de test (amis et messages privés chiffrés)

> Tests automatiques : `make e2e-client` (scénario « private messages », face à `quarelctl`) et `make client-interop`.

Sur `https://app.quarel.app`, avec deux comptes (par exemple le vôtre et « Test », dans deux navigateurs ou une fenêtre privée).

1. **Amis** : icône bulle en haut à gauche → **Amis** → **Ajouter un ami** → pseudo de l'autre compte. De l'autre côté, un badge apparaît sur l'icône et sur « Amis » ; onglet **En attente** → **Accepter**.
2. **Présence** : l'onglet **En ligne** montre qui est connecté (pastille verte).
3. **Conversation** : bouton bulle à côté de l'ami. La mention **« Chiffré de bout en bout »** s'affiche. Écrire : le message apparaît chez l'autre instantanément.
4. **Suivi** : sous votre dernier message, « Envoyé » puis **« Distribué »** (reçu par tous les appareils de l'autre) puis **« Vu »** quand l'autre a ouvert la conversation. « … écrit » pendant qu'il tape.
5. **Modifier / supprimer** : survol de votre message → crayon / corbeille : appliqué chez l'autre.
6. **Groupe** : **+** à côté de « Conversations » → nom, amis → **Créer le groupe**. Tout le groupe reçoit les messages ; **Quitter le groupe** en haut à droite.
7. **Persistance** : recharger la page : l'historique est là (gardé sur l'appareil, jamais sur le serveur).

## À savoir
- Chaque connexion est un **appareil**. Le premier appareil d'un compte est validé d'office ; un deuxième (autre navigateur, application de bureau) affiche « Cet appareil n'est pas encore validé » avec un code : le valider : voir `client-etape-4b.md`.
- Dans un navigateur, l'historique est gardé dans le stockage du navigateur (non chiffré) ; dans l'application de bureau, chiffré par le trousseau du système.
