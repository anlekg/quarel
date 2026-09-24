# Client graphique, étape 6b — Guide de test (administrer un serveur)

> Test automatique : `make e2e-client` (scénario « server administration »).

Sur `test.quarel.app`, dont vous êtes propriétaire, avec un second compte membre (par exemple « Test »).

1. **Menu du serveur** (nom du serveur en haut à gauche) → **Paramètres du serveur**. Les sections affichées dépendent de vos permissions.
2. **Vue d'ensemble** : nom, accès (privé / public), **règles** (les nouveaux membres doivent les accepter), téléphone exigé (grisé tant que le serveur n'a pas de fournisseur de SMS).
3. **Rôles** : **+ Nouveau rôle** → nom, couleur, « Afficher ses membres à part », « mentionnable », permissions (par groupes), **Monter / Descendre**, **Supprimer**. Les permissions que vous n'avez pas sont grisées.
4. **Membres** : **Gérer** → cocher des rôles ; exclure temporairement (durée, lecture seule), expulser, bannir (avec suppression de ses messages). Raison facultative, visible dans le journal. Aussi en cliquant sur un membre dans la liste de droite.
5. **Salons** : **Créer un salon** (textuel, vocal, annonces, catégorie) ; **Modifier** → nom, sujet, catégorie, supprimer ; onglet **Permissions** : pour un rôle ou un membre, chaque droit est ✕ refusé, / hérité ou ✓ autorisé (ex. empêcher @everyone d'écrire dans un salon). Raccourci : roue dentée au survol d'un salon dans la barre de gauche.
6. **Invitations** : créer (nombre d'utilisations, durée ; le lien est copié), copier, révoquer.
7. **Bannissements** : liste, **Lever le bannissement**.
8. **Journal de modération** : qui a fait quoi, avec la raison (90 jours).
9. **Bots** : créer (le jeton n'est affiché qu'une fois), nouveau jeton, supprimer.
10. **Vocal** : dans la fiche d'un membre connecté à un salon vocal : couper son micro, sourdine, déplacer vers un autre salon, déconnecter.

## À savoir
- Vous n'agissez que sur les membres et rôles **sous votre rôle le plus haut** ; le propriétaire est intouchable. Le serveur revérifie tout.
- Échap ferme la fenêtre du dessus seulement.
