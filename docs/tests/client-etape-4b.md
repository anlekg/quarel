# Client graphique, étape 4b — Guide de test (validation des appareils, phrase de récupération)

> Test automatique : `make e2e-client` (scénario « devices » : l'application valide un appareil `quarelctl` et inversement, restauration par la phrase dans l'application et dans `quarelctl`).

Il vous faut **deux appareils connectés au même compte** : par exemple l'application de bureau et `https://app.quarel.app`, ou deux navigateurs (un normal, un en fenêtre privée).

## 1. Phrase de récupération
1. Sur votre appareil déjà validé, en haut de **Messages privés** : « Créez une phrase de récupération » → **Créer ma phrase** → **Créer la phrase**.
2. Les 12 mots s'affichent **une seule fois** : notez-les sur papier, cochez « J'ai noté ma phrase de récupération » → **Terminer**.
3. **Paramètres › Récupération** indique « Sauvegarde active » avec sa date. Elle se met à jour toute seule après chaque message.

## 2. Valider un nouvel appareil
1. Connectez-vous sur le second appareil. En haut de Messages privés : « Cet appareil n'est pas encore validé », avec un code `XXXX-XXXX-XXXX-XXXX`. L'envoi y est impossible.
2. Sur l'appareil validé, un bandeau apparaît : « Un appareil attend d'être validé » → **Valider un appareil** (ou **Paramètres › Appareils** → **Valider** sur la ligne « Non validé »).
3. Tapez le code affiché par le nouvel appareil → **Valider**. Un mauvais code est refusé (« ne correspond pas »).
4. Sur le nouvel appareil, le bandeau disparaît en quelques secondes et **les anciennes conversations apparaissent** (historique transféré, chiffré de bout en bout). Il peut écrire.
5. **Paramètres › Appareils** : les deux appareils sont marqués « Validé ».

## 3. Tout retrouver avec la phrase
1. Connectez-vous sur un troisième appareil (autre navigateur ou fenêtre privée). Il n'est pas validé.
2. Dans le bandeau : **Utiliser ma phrase de récupération** → tapez les 12 mots (accents et majuscules sans importance) → **Restaurer**.
3. Une faute de frappe est signalée ; avec la bonne phrase : « Compte restauré », l'appareil est validé et l'historique est là.

## À savoir
- Le code se compare **de vos propres yeux**, entre deux appareils que vous avez devant vous : ne validez jamais un code reçu par message.
- Le service d'identité ne voit que du chiffré : il ne peut ni lire la sauvegarde ni retrouver la phrase. Phrase perdue mais un appareil encore validé : **Paramètres › Récupération › Nouvelle phrase** (l'ancienne ne sert plus).
- Un appareil perdu : **Paramètres › Appareils › Déconnecter** (ses clés sont supprimées).
