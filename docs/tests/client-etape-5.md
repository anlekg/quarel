# Client graphique, étape 5 — Guide de test (appels entre amis)

> Test automatique : `make e2e-client` (scénario « calls » : application ↔ `quarelctl` dans les deux sens, relais seul, appel refusé, deux applications avec caméra).

Avec deux comptes **amis**, chacun sur un appareil validé (par exemple l'application de bureau et `https://app.quarel.app` sur un téléphone ou un autre ordinateur). Idéalement, essayez aussi **depuis deux réseaux différents** (par exemple un téléphone en 4G/5G, Wi-Fi coupé) pour tester le relais.

1. **Appeler** : dans la conversation privée avec l'ami, bouton **téléphone** en haut à droite. Une tonalité d'attente, la barre « Appel en cours… » au-dessus de votre nom.
2. **Recevoir** : chez l'ami, une carte « … vous appelle » apparaît en haut de l'écran avec une sonnerie (notification si la fenêtre est en arrière-plan) → **Répondre** ou **Refuser**. Si l'ami a plusieurs appareils, tous sonnent ; dès qu'un répond, les autres s'arrêtent.
3. **En communication** : la barre indique la durée et le chemin : « en direct (réseau local) », « en direct » ou « via le relais (chiffré) ». La conversation affiche les deux participants.
4. **Micro** et **caméra** : boutons de la barre. La vidéo apparaît chez l'autre ; l'icône micro barré s'affiche à côté du nom de qui a coupé son micro.
5. **Raccrocher** : bouton rouge. Sans réponse au bout de 30 s : « Pas de réponse ».
6. **Refus du relais** : Paramètres › **Appels** → décocher « Utiliser le relais… ». Entre deux réseaux difficiles, l'appel peut alors échouer (« Connexion impossible… »).

## À savoir
- Le son et l'image ne passent jamais par un serveur Quarel, sauf par le relais quand aucune connexion directe n'est possible ; ils restent alors chiffrés de bout en bout (le relais ne peut ni écouter ni voir).
- Le relais de `identity.quarel.app` tourne sur votre machine : ports UDP 3478 et 49160-49200 ouverts sur la Livebox par UPnP, adresse IP publique suivie automatiquement.
- Rejoindre un appel quitte le salon vocal où vous étiez.
- Pas encore : choix du micro, de la caméra et des haut-parleurs (étape « Paramètres »), partage d'écran en appel, appels de groupe.
