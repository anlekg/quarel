# Import depuis Discord — Guide de test

> Test automatique : `make discord-import-test` (vrais serveurs Quarel, API Discord simulée). Ce guide sert à l'essai avec un **vrai** serveur Discord, le seul point que les tests ne couvrent pas.

Suivez la page du wiki **[Migrer depuis Discord](https://quarel.app/wiki/heberger/migrer-depuis-discord/)** avec un petit serveur Discord de test (quelques rôles de couleurs différentes, une catégorie privée, un salon d'annonces, un forum, un emoji) et un serveur Quarel neuf.

1. **Essai** (`--dry-run`) : le plan liste vos rôles et salons dans le même ordre que Discord ; rien n'apparaît dans Quarel.
2. **Import** (`--rename --replace-defaults`) : les salons d'origine de Quarel disparaissent, le serveur prend le nom Discord ; rôles (couleur, ordre, « affiché à part »), catégories et salons à l'identique ; la catégorie privée n'est visible que par les rôles prévus (vérifiez avec un 2ᵉ compte sans rôle) ; l'emoji s'utilise avec `:nom:`.
3. **Rapport** : il cite ce qui a changé (par exemple les fils, le mode lent, les permissions sans équivalent). Rien d'inattendu ?
4. **Relance** : la même commande répond « 0 création(s), 0 mise(s) à jour » ; renommez un rôle sur Discord et relancez : il est renommé dans Quarel.
5. **Sécurité** : avec un lien d'invitation dont on change une lettre du `sid=`, l'outil refuse (« ce n'est pas le serveur attendu ») sans rien envoyer.
