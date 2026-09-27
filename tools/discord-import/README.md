# quarel-discord-import

Copie la **structure** d'un serveur Discord sur un serveur Quarel : rôles (couleur, ordre, permissions, `@everyone` compris), catégories et salons (texte, vocal, annonces, forum, scène), droits de chaque salon pour les rôles, emojis personnalisés, et le nom du serveur si vous le demandez. Les membres et les messages ne sont pas copiés.

Mode d'emploi complet : **[Migrer depuis Discord](https://quarel.app/wiki/heberger/migrer-depuis-discord/)**.

```sh
pip install cryptography        # ou : sudo apt install python3-cryptography
python3 quarel_discord_import.py '<lien d’invitation Quarel>' --dry-run   # le plan, sans rien modifier
python3 quarel_discord_import.py '<lien d’invitation Quarel>'             # l'import
```

Les jetons des deux bots sont lus dans `DISCORD_BOT_TOKEN` et `QUAREL_BOT_TOKEN`, sinon demandés sans affichage : jamais sur la ligne de commande. `--help` pour les autres options (`--guild`, `--rename`, `--replace-defaults`, `--no-emojis`, `--state`).

- **Relancer** la commande met à jour ce qui existe et crée ce qui manque, sans doublon (correspondances gardées dans `discord-import-<serveur Discord>.json`).
- **Certificat** : celui du serveur Quarel est vérifié comme le fait l'application (autorité reconnue, ou certificat auto-signé lié à l'identifiant du lien d'invitation), avant d'envoyer quoi que ce soit.
- Python 3.10 ou plus récent ; seule dépendance : [`cryptography`](https://cryptography.io) (Apache-2.0 / BSD).

Tests (depuis la racine du dépôt) : `make discord-import-test` — un vrai service d'identité et un vrai serveur Quarel, une API Discord simulée.
