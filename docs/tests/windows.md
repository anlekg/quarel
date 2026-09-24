# Version Windows des serveurs — Guide de test

Fichiers : `dist/windows/Quarel-Serveur-Setup-<version>.exe` et `Quarel-Identite-Setup-<version>.exe` (construits par `make windows`). Windows 10 ou 11, 64 bits.

## Serveur communautaire
1. Lancer l'installateur. SmartScreen (installateur non signé) : « Informations complémentaires » → « Exécuter quand même ». Accepter la demande d'administrateur.
2. Dernière page : laisser cochée « Lancer… et ouvrir sa page d'administration » → **Terminer**. Le navigateur s'ouvre sur `http://127.0.0.1:8091` : choisir le mot de passe (pas de code demandé sur la machine elle-même).
3. **Icône verte** près de l'horloge (éventuellement dans la flèche « icônes cachées ») :
   - clic : ouvre la page d'administration ;
   - clic droit : « État : en marche », « Lancer au démarrage de Windows » (coché), « Quitter ».
4. Relancer le programme depuis le menu Démarrer alors qu'il tourne déjà : pas de second serveur, la page s'ouvre.
5. Redémarrer Windows : l'icône revient seule. Décocher « Lancer au démarrage de Windows » puis redémarrer : elle ne revient pas.
6. Tableau de bord : état, ports de la box (l'UPnP est actif par défaut sous Windows aussi) ; **lien propriétaire** à coller dans l'application.
7. **Vocal** : dans l'application, rejoindre le salon vocal (à tester quand l'étape 3 du client sera prête ; en attendant, `quarelctl voice-test`). Le vocal n'a pas pu être vérifié sous Wine : c'est le point le plus important à regarder.
8. **Pare-feu** : Windows ne doit **pas** demander d'autorisation au premier lancement (règles créées par l'installateur).
9. Journal : page « Journal », ou le fichier `%LOCALAPPDATA%\Quarel\Serveur\quarel-server.log`.
10. **Désinstaller** (Paramètres → Applications → « Quarel — serveur communautaire ») : message indiquant que les données sont conservées ; l'icône disparaît, plus de démarrage automatique.
11. **Mise à jour** : relancer un installateur plus récent pendant que le serveur tourne : il est arrêté, remplacé, relancé ; données et réglages intacts.

## Service d'identité
Même déroulé avec `Quarel-Identite-Setup-<version>.exe` : icône **violette**, page sur `http://127.0.0.1:8081`, données dans `%LOCALAPPDATA%\Quarel\Identite`. Le tableau de bord signale un nom public « localhost » et l'absence d'emails tant que ce n'est pas réglé.
