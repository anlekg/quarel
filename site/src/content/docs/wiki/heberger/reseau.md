---
title: Réseau, ports et HTTPS
description: Rendre un serveur joignable depuis Internet — UPnP, ports à ouvrir, certificats, diagnostic.
sidebar:
  order: 2
---

## Ports d’un serveur communautaire

| Port | Rôle |
|---|---|
| **8090/tcp** | application, connexions, temps réel (HTTPS) |
| **7882/udp** | vocal et vidéo |
| **7881/tcp** | vocal, secours quand l’UDP est bloqué |
| 8091/tcp | page d’administration — **réseau local seulement, ne jamais l’ouvrir** |

Par défaut, le serveur ouvre lui-même ces ports sur votre box par **UPnP** (bail d’une heure renouvelé, retiré à l’arrêt). Avec Docker, il faut pour cela le réseau de l’hôte (`network_mode: host`, déjà dans le `compose.yaml` fourni). Pour l’empêcher : `QUAREL_UPNP=off`, puis redirigez ces ports à la main vers la machine dans l’interface de votre box.

## Diagnostic

Le tableau de bord de la page d’administration indique si le serveur est joignable :

- **ok** : ports ouverts, adresse publique trouvée ;
- **ports à ouvrir à la main** : pas d’UPnP sur la box (désactivé, ou box qui ne le propose pas) ;
- **double NAT** : votre box est elle-même derrière un autre routeur (box opérateur + routeur perso, réseau mobile 4G/5G, CGNAT). Il faut ouvrir les ports sur les deux équipements, ou demander une IP publique à votre opérateur ;
- **inconnu** : le test n’a pas pu aboutir.

L’adresse publique annoncée pour le vocal est celle de la box (trouvée par UPnP), sinon découverte par STUN.

## HTTPS

Réglage « HTTPS › Certificat » de la page d’administration (`QUAREL_TLS`) :

- **Auto-signé lié à l’identité** (par défaut) : rien à faire. Le certificat porte une preuve signée par la clé du serveur ; l’application le vérifie grâce à l’identifiant contenu dans les liens d’invitation. Pas besoin de nom de domaine. **Mais la version web (navigateur) ne peut pas rejoindre un tel serveur.**
- **Let’s Encrypt** (`acme`) : avec un nom de domaine qui pointe vers votre box. Le port public **443** doit mener au port 8090 de la machine (défi TLS-ALPN). La version web peut alors rejoindre le serveur.
- **Fichiers** : votre propre certificat (`QUAREL_TLS_CERT`, `QUAREL_TLS_KEY`).
- **Désactivé** : derrière un proxy HTTPS (Caddy, nginx, Traefik…) qui transmet les WebSockets. Déclarez le proxy dans `QUAREL_TRUSTED_PROXIES` pour que les limites s’appliquent à la vraie adresse des clients. Le vocal (UDP) ne passe pas par le proxy : ses ports restent à ouvrir.

## Adresse publique et nom

`QUAREL_TLS_HOSTS` (réglage « Nom public du serveur ») : le nom ou l’adresse que les membres utilisent, repris dans les liens d’invitation. Si votre IP change souvent, utilisez un nom de domaine dynamique (DynDNS).

## Service d’identité

Il écoute sur 8080/tcp (derrière le port public 443) et, si le relais d’appels est actif, sur **3478/udp** et **49160-49200/udp**, ouverts aussi par UPnP à la maison. Détails : [Déployer un service d’identité](/wiki/heberger/service-identite/).
