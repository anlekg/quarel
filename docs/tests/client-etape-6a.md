# Client graphique, étape 6a — Guide de test (vos paramètres)

> Test automatique : `make e2e-client` (scénario « settings »).

Roue dentée à côté de votre nom, en bas à gauche.

1. **Statut** : cliquez sur **votre nom** (pas la roue) → En ligne, Absent, Ne pas déranger (pas de sonnerie ni de notification d'appel), Invisible (vous apparaissez hors ligne). Vos amis voient le changement tout de suite.
2. **Profil** : image (réduite automatiquement si elle est grande), **pseudo** (une fois par jour ; vos amis et serveurs vous gardent), **À propos de moi**.
3. **Sécurité** :
   - **Email** : nouvelle adresse + mot de passe → code reçu (pour l'instant dans le journal du service, pas de fournisseur d'email) → Confirmer.
   - **Mot de passe** : vos autres appareils sont déconnectés.
   - **Double authentification** : Activer → mot de passe → QR code à scanner (Aegis, 2FAS, Google Authenticator…) → code à 6 chiffres → **10 codes de secours** à garder. Désactiver : mot de passe + code (ou un code de secours).
   - **Supprimer mon compte** : immédiat et définitif, il faut retaper son pseudo.
4. **Confidentialité** : « Montrer quand j'écris », « Accusés de lecture », **personnes bloquées** (bloquer par pseudo, ou bouton **Bloquer** dans la liste d'amis ; débloquer). Sur les serveurs, les messages d'une personne bloquée sont masqués (« Afficher » pour les voir quand même).
5. **Voix et vidéo** : choix du **micro** (bouton « Tester le micro » : la barre bouge quand vous parlez), des **haut-parleurs** (« Jouer un son de test »), de la **caméra** (aperçu). Le changement s'applique tout de suite, même en vocal ou en appel. En bas : autoriser ou non le relais d'appels.

## À savoir
- Dans le navigateur, les noms des micros et caméras n'apparaissent qu'après avoir autorisé leur accès (un test suffit) ; le choix des haut-parleurs dépend du navigateur (absent sur Firefox et Safari).
