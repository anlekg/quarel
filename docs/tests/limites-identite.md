# Limites fixées par un service d'identité — Guide de test

> Test automatique : `make e2e-client` (scénario « identity limits », avec les vraies pages d'administration) ; `go test ./internal/identity/ ./internal/community/ ./pkg/idtoken/`.

## Préparation
- Service d'identité : `make run-identity`, page d'administration `http://localhost:8081`.
- Serveur communautaire qui l'accepte : `QUAREL_TRUSTED_ISSUERS=localhost:8080 make run-server`, page `http://localhost:8091`.

## 1. Inscriptions
1. Réglages du service d'identité → **Création de comptes : sur invitation**. Dans l'application (écran « Créer un compte »), un champ **Code d'invitation** apparaît.
2. Page **Invitations** : créer un code (nombre d'utilisations, durée), l'utiliser dans l'application. Un code faux, expiré ou épuisé est refusé.
3. **Invitations par utilisateur : 1** → dans l'application, Paramètres → **Invitations** : « 1 invitation restante », créer un code ; le bouton se désactive ensuite.
4. **Domaines autorisés** `exemple.fr` : une autre adresse est refusée dès le formulaire. **Nombre maximum de comptes** atteint → refus. **Fermée** → l'écran d'inscription l'annonce.

## 2. Liste noire (mode « tous, sauf… »)
1. Page **Serveurs** → Bloquer : coller l'identifiant du serveur (dans son lien d'invitation, après `sid=`) et une raison.
2. Dans l'application : « Rejoindre un serveur » avec son lien → « Ce serveur est bloqué par votre service d'identité. Raison : … ». Un serveur déjà rejoint apparaît comme bloqué au lancement suivant.
3. **Débloquer** : tout redevient normal.

## 3. Serveurs approuvés seulement
1. Réglages du service d'identité → **Serveurs autorisés : seulement les serveurs approuvés**.
2. Dans l'application, rejoindre le serveur → « Ce serveur n'est pas approuvé par votre service d'identité ».
3. Page d'administration du **serveur communautaire**, bas du tableau de bord : « approbation nécessaire » → **Demander l'accès** (contact) → « demande en attente ».
4. Page **Serveurs** du service d'identité : la demande apparaît (nom, adresse, contact) → **Approuver**. Le serveur affiche « serveur approuvé » ; dans l'application, **Rejoindre** fonctionne.
5. **Retirer l'approbation** ou **Bloquer** : les connexions suivantes sont refusées.

## À regarder
Clarté des messages dans l'application et dans les deux pages d'administration.
