#!/usr/bin/env python3
"""Copy a Discord server's structure to a Quarel community server.

Reads the Discord server with a Discord bot token (REST API only, no gateway)
and recreates on the Quarel server, with a Quarel bot token: roles (name,
colour, order, permissions, @everyone included), categories and channels
(text, voice, announcement, forum, stage), the permissions of each channel
for roles, custom emojis, and optionally the server's name. Messages and
members are not copied: Discord and Quarel accounts are unrelated.

Every Discord ID is mapped to the Quarel ID it became in a state file, so the
import can be run again: what already exists is updated, what is missing is
created, nothing is duplicated. --dry-run shows the plan without writing.

The Quarel server's certificate is verified like the Quarel app does it: a
certificate from an authority, or a self-signed one whose binding proves the
server ID of the invite link (pkg/tlsbind). Nothing is sent before that check.

Tokens are read from the environment (DISCORD_BOT_TOKEN, QUAREL_BOT_TOKEN) or
asked for without echo, never taken from the command line.
"""

from __future__ import annotations

import argparse
import base64
import getpass
import hashlib
import http.client
import json
import os
import re
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

from cryptography import x509
from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

VERSION = "0.1.0"
DISCORD_API = os.environ.get("QUAREL_IMPORT_DISCORD_API", "https://discord.com/api/v10")  # tests use a fake
DISCORD_CDN = os.environ.get("QUAREL_IMPORT_DISCORD_CDN", "https://cdn.discordapp.com")
USER_AGENT = f"DiscordBot (https://github.com/anlekg/quarel, {VERSION})"


class ImportError_(Exception):
    """An error shown to the person as is (in French)."""


# --- permissions ---

# Discord permission bits that have a Quarel equivalent.
PERMS = {
    0: "create_invite",
    1: "kick_members",
    2: "ban_members",
    3: "administrator",
    4: "manage_channels",
    5: "manage_server",
    6: "add_reactions",
    7: "view_audit_log",
    9: "stream",
    10: "view_channel",
    11: "send_messages",
    13: "manage_messages",
    15: "attach_files",
    17: "mention_everyone",
    20: "connect",
    21: "speak",
    22: "mute_members",
    23: "deafen_members",
    24: "move_members",
    28: "manage_roles",
    29: "manage_webhooks",
    40: "moderate_members",
}

# Permissions Quarel lets a channel override (internal/community/permissions.go).
CHANNEL_SCOPED = {
    "view_channel", "send_messages", "manage_messages", "mention_everyone", "manage_channels", "connect", "speak",
    "stream", "add_reactions", "attach_files", "mute_members", "deafen_members", "move_members", "manage_webhooks",
}

# Discord permissions whose absence restricts people, but that Quarel does not
# restrict: (name, what Quarel does instead).
ALWAYS_ALLOWED = {
    16: ("Voir les anciens messages", "voir un salon donne tout son historique"),
    14: ("Intégrer des liens", "les liens ont toujours un aperçu"),
    26: ("Changer de pseudo", "chacun peut changer son surnom"),
    31: ("Utiliser les commandes de l'application", "chacun peut utiliser les commandes des bots"),
    35: ("Créer des fils publics", "créer un fil demande seulement « Envoyer des messages »"),
    38: ("Envoyer des messages dans les fils", "un fil suit « Envoyer des messages » de son salon"),
}

# Discord permissions granting something Quarel has no permission for.
NO_EQUIVALENT = {
    8: "Voix prioritaire",
    27: "Gérer les pseudos",
    30: "Gérer les expressions (dans Quarel, les emojis demandent « Gérer le serveur »)",
    33: "Gérer les événements",
    34: "Gérer les fils (dans Quarel : « Gérer les salons » et « Gérer les messages »)",
}

# Channel overrides: Discord permissions Quarel can only give server-wide.
SERVER_ONLY = {0: "Créer une invitation", 28: "Gérer les permissions du salon"}

ADMIN_BIT = 3


def quarel_perms(bits: int, channel: bool = False) -> list[str]:
    names = [name for bit, name in PERMS.items() if bits >> bit & 1]
    if channel:
        names = [n for n in names if n in CHANNEL_SCOPED]
    return sorted(names)


def has(bits: int, bit: int) -> bool:
    return bool(bits >> bit & 1)


# --- the Quarel server's certificate (pkg/tlsbind) ---

BINDING_CONTEXT = b"quarel-tls-binding-v1\x00"


def _b64url(s: str) -> bytes:
    return base64.urlsafe_b64decode(s + "=" * (-len(s) % 4))


def server_id(identity_key: bytes) -> str:
    h = hashlib.sha256(identity_key).digest()[:16]
    return base64.b32encode(h).decode().rstrip("=").lower()


def binding_server_id(der: bytes) -> str | None:
    """The server ID a self-signed Quarel certificate proves, or None if it has no valid binding."""
    try:
        cert = x509.load_der_x509_certificate(der)
        san = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
    except (ValueError, x509.ExtensionNotFound):
        return None
    spki = cert.public_key().public_bytes(Encoding.DER, PublicFormat.SubjectPublicKeyInfo)
    for uri in san.get_values_for_type(x509.UniformResourceIdentifier):
        u = urllib.parse.urlsplit(uri)
        if u.scheme != "quarel" or u.netloc != "binding":
            continue
        parts = u.path.strip("/").split("/")
        if len(parts) != 2:
            return None
        try:
            pub, sig = _b64url(parts[0]), _b64url(parts[1])
            Ed25519PublicKey.from_public_bytes(pub).verify(sig, BINDING_CONTEXT + spki)
        except (ValueError, InvalidSignature):
            return None
        return server_id(pub)
    return None


class QuarelConnection(http.client.HTTPSConnection):
    """HTTPS to a Quarel server: an authority's certificate, or a self-signed
    one bound to the expected server ID. The check happens during connect(),
    before any byte of the request (and its token) leaves."""

    def __init__(self, host: str, port: int, sid: str | None, mode: dict):
        super().__init__(host, port, timeout=60)
        self.sid, self.mode = sid, mode  # mode is shared: "authority" or "binding" once known

    def _wrap(self, ctx: ssl.SSLContext):
        http.client.HTTPConnection.connect(self)
        self.sock = ctx.wrap_socket(self.sock, server_hostname=self.host)

    def connect(self):
        if self.mode.get("tls") != "binding":
            try:
                self._wrap(ssl.create_default_context())
                self.mode["tls"] = "authority"
                return
            except ssl.SSLCertVerificationError:
                if self.mode.get("tls") == "authority":
                    raise ImportError_("le certificat du serveur Quarel a changé en cours de route : arrêt")
                self.close()
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        ctx.check_hostname, ctx.verify_mode = False, ssl.CERT_NONE  # verified just below, before sending anything
        self._wrap(ctx)
        proven = binding_server_id(self.sock.getpeercert(binary_form=True))
        if proven is None or not self.sid or proven != self.sid:
            self.close()
            if proven is None:
                raise ImportError_("certificat du serveur Quarel non reconnu (ni autorité, ni certificat Quarel) : arrêt")
            if not self.sid:
                raise ImportError_("ce serveur a un certificat auto-signé : donnez son identifiant (--sid, ou un lien d'invitation)")
            raise ImportError_(f"ce n'est pas le serveur attendu (identifiant {proven}, attendu {self.sid}) : arrêt")
        self.mode["tls"] = "binding"


# --- API clients ---

class ApiError(ImportError_):
    def __init__(self, status: int, code: str, message: str):
        super().__init__(f"{code} ({status}) — {message}")
        self.status, self.code = status, code


class Quarel:
    def __init__(self, url: str, sid: str | None, token: str, pause: float = 0.12):
        u = urllib.parse.urlsplit(url)
        if u.scheme not in ("https", "http") or not u.hostname:
            raise ImportError_(f"adresse du serveur Quarel invalide : {url}")
        if u.scheme == "http" and u.hostname not in ("localhost", "127.0.0.1", "::1"):
            raise ImportError_("http:// n'est accepté que sur la machine elle-même : utilisez https://")
        self.scheme, self.host, self.port = u.scheme, u.hostname, u.port or (443 if u.scheme == "https" else 80)
        self.sid, self.token, self.pause = sid, token, pause
        self.tls: dict = {}

    def _conn(self):
        if self.scheme == "http":
            return http.client.HTTPConnection(self.host, self.port, timeout=60)
        return QuarelConnection(self.host, self.port, self.sid, self.tls)

    def request(self, method: str, path: str, body=None, raw: bytes | None = None, ctype: str | None = None):
        headers = {"Authorization": "Bearer " + self.token, "User-Agent": "quarel-discord-import/" + VERSION}
        data = raw
        if body is not None:
            data, ctype = json.dumps(body).encode(), "application/json"
        if ctype:
            headers["Content-Type"] = ctype
        for attempt in range(6):
            conn = self._conn()
            try:
                conn.request(method, path, body=data, headers=headers)
                resp = conn.getresponse()
                payload = resp.read()
            except (OSError, http.client.HTTPException) as e:
                if attempt == 5:
                    raise ImportError_(f"serveur Quarel injoignable ({e})")
                time.sleep(1 + attempt)
                continue
            finally:
                conn.close()
            if resp.status == 429 and attempt < 5:
                time.sleep(float(resp.getheader("Retry-After") or 2))
                continue
            if method != "GET" and self.pause:
                time.sleep(self.pause)  # well under the server's 600 requests per minute
            if resp.status >= 400:
                try:
                    err = json.loads(payload)["error"]
                except (ValueError, KeyError, TypeError):
                    err = {"code": "http_error", "message": payload[:200].decode(errors="replace")}
                raise ApiError(resp.status, err.get("code", ""), err.get("message", ""))
            return json.loads(payload) if payload else None
        raise ImportError_("le serveur Quarel limite trop les requêtes : réessayez plus tard")


class Discord:
    def __init__(self, token: str):
        self.token = token

    def _get(self, url: str, auth: bool = True) -> bytes:
        headers = {"User-Agent": USER_AGENT}
        if auth:
            headers["Authorization"] = "Bot " + self.token
        for attempt in range(6):
            try:
                with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=60) as r:
                    return r.read()
            except urllib.error.HTTPError as e:
                body = e.read()
                if e.code == 429 and attempt < 5:
                    try:
                        wait = float(json.loads(body).get("retry_after", 2))
                    except ValueError:
                        wait = 2
                    time.sleep(wait)
                    continue
                if e.code == 401:
                    raise ImportError_("jeton Discord refusé (401) : vérifiez DISCORD_BOT_TOKEN")
                if e.code in (403, 404):
                    raise ImportError_(f"Discord refuse l'accès ({e.code}) : le bot est-il bien sur ce serveur ?")
                raise ImportError_(f"Discord : erreur {e.code}")
            except (urllib.error.URLError, OSError) as e:
                if attempt == 5:
                    raise ImportError_(f"Discord injoignable ({e})")
                time.sleep(1 + attempt)
        raise ImportError_("Discord limite trop les requêtes : réessayez plus tard")

    def get(self, path: str):
        return json.loads(self._get(DISCORD_API + path))

    def emoji(self, emoji: dict) -> bytes:
        ext = "gif" if emoji.get("animated") else "png"
        return self._get(f"{DISCORD_CDN}/emojis/{emoji['id']}.{ext}", auth=False)


# --- invite links and arguments ---

def parse_target(value: str) -> tuple[str, str | None]:
    """A Quarel server address or invite link → (https base URL, server ID or None)."""
    v = value.strip()
    if "#" in v and "/join" in v:  # web invite: https://app.quarel.app/join#host:port/CODE?sid=…
        v = "quarel://" + v.split("#", 1)[1]
    u = urllib.parse.urlsplit(v if "://" in v else "https://" + v)
    sid = urllib.parse.parse_qs(u.query).get("sid", [None])[0]
    scheme = "http" if u.scheme == "http" else "https"
    if not u.hostname:
        raise ImportError_(f"adresse du serveur Quarel invalide : {value}")
    host = f"[{u.hostname}]" if ":" in u.hostname else u.hostname
    return f"{scheme}://{host}" + (f":{u.port}" if u.port else ""), sid


def secret(env: str, prompt: str) -> str:
    v = os.environ.get(env, "").strip()
    if v:
        return v
    if not sys.stdin.isatty():
        raise ImportError_(f"{env} manquant")
    v = getpass.getpass(prompt).strip()
    if not v:
        raise ImportError_(f"{env} manquant")
    return v


# --- state (Discord ID → Quarel ID) ---

class State:
    def __init__(self, path: str, guild: str, server: str):
        self.path = path
        self.data = {"guild": guild, "server": server, "roles": {}, "channels": {}, "emojis": {}}
        if os.path.exists(path):
            with open(path) as f:
                old = json.load(f)
            if old.get("guild") != guild or (old.get("server") and server and old["server"] != server):
                raise ImportError_(f"{path} concerne un autre serveur : choisissez un autre fichier (--state)")
            self.data.update({k: old.get(k, {}) for k in ("roles", "channels", "emojis")})

    def get(self, kind: str, discord_id: str):
        return self.data[kind].get(discord_id)

    def put(self, kind: str, discord_id: str, quarel_id):
        self.data[kind][discord_id] = quarel_id
        tmp = self.path + ".tmp"
        with open(tmp, "w") as f:
            json.dump(self.data, f, indent=1)
        os.replace(tmp, self.path)


# --- the import ---

TEXT, VOICE, CATEGORY, ANNOUNCEMENT, STAGE, FORUM, MEDIA = 0, 2, 4, 5, 13, 15, 16
KINDS = {TEXT: "text", VOICE: "voice", CATEGORY: "category", ANNOUNCEMENT: "announcement", STAGE: "voice",
         FORUM: "forum", MEDIA: "forum"}
KIND_FR = {TEXT: "salon textuel", VOICE: "salon vocal", CATEGORY: "catégorie", ANNOUNCEMENT: "salon d'annonces",
           STAGE: "salon vocal (scène)", FORUM: "forum", MEDIA: "forum (salon média)"}
DEFAULT_CHANNELS = {("category", "Salons textuels"), ("text", "général"), ("category", "Salons vocaux"), ("voice", "Général")}


def channel_order(channels: list[dict]) -> list[dict]:
    """Discord's display order: channels without a category first, then each
    category followed by its channels; text-like ones before voice-like ones."""
    def key(c):
        return (1 if c["type"] in (VOICE, STAGE) else 0, c.get("position", 0), int(c["id"]))
    known = [c for c in channels if c["type"] in KINDS]
    cats = sorted((c for c in known if c["type"] == CATEGORY), key=lambda c: (c.get("position", 0), int(c["id"])))
    cat_ids = {c["id"] for c in cats}
    out = sorted((c for c in known if c["type"] != CATEGORY and c.get("parent_id") not in cat_ids), key=key)
    for cat in cats:
        out.append(cat)
        out += sorted((c for c in known if c["type"] != CATEGORY and c.get("parent_id") == cat["id"]), key=key)
    return out


def overwrite_set(c: dict) -> set:
    return {(o["id"], int(o["type"]), str(o["allow"]), str(o["deny"])) for o in c.get("permission_overwrites") or []}


def emoji_name(name: str, taken: set[str]) -> str:
    base = re.sub(r"[^a-z0-9_]", "_", name.lower())[:32].strip("_") or "emoji"
    if len(base) < 2:
        base = (base + "_emoji")[:32]
    n, candidate = 2, base
    while candidate in taken:
        suffix = f"_{n}"
        candidate, n = base[: 32 - len(suffix)] + suffix, n + 1
    return candidate


class Importer:
    def __init__(self, discord: Discord, quarel: Quarel, guild_id: str, state: State, dry_run: bool,
                 emojis: bool = True, rename: bool = False, replace_defaults: bool = False, out=print):
        self.d, self.q, self.gid, self.state, self.dry = discord, quarel, guild_id, state, dry_run
        self.opt_emojis, self.rename, self.replace_defaults, self.out = emojis, rename, replace_defaults, out
        self.counts = {"créé": 0, "mis à jour": 0, "supprimé": 0, "inchangé": 0}
        self.notes: dict[str, list[str]] = {}
        self.role_names: dict = {1: "@everyone"}  # Quarel role ID → name, for the messages
        self._fake = 0

    def note(self, section: str, line: str):
        lines = self.notes.setdefault(section, [])
        if line not in lines:
            lines.append(line)

    def fake_id(self):
        self._fake += 1
        return f"(nouveau {self._fake})"

    def write(self, verb: str, what: str, call):
        """Run a write (or only show it with --dry-run)."""
        self.counts[verb] += 1
        self.out(("[essai] " if self.dry else "") + f"{verb[0].upper()}{verb[1:]} : {what}")
        return None if self.dry else call()

    # --- run ---

    def run(self):
        guild = self.d.get(f"/guilds/{self.gid}")
        channels = self.d.get(f"/guilds/{self.gid}/channels")
        server = self.q.request("GET", "/v1/server")
        self.out(f"Discord : « {guild['name']} » ({len(guild.get('roles', []))} rôles, {len(channels)} salons, "
                 f"{len(guild.get('emojis', []))} emojis)")
        self.out(f"Quarel : « {server['name']} » ({server['id']})" + (" — ESSAI, rien ne sera modifié" if self.dry else ""))
        me = self.q.request("GET", "/v1/members/@me/permissions")
        if "administrator" not in me.get("server", []):
            raise ImportError_("le bot Quarel doit avoir un rôle avec « Administrateur », placé tout en haut de la liste des rôles")
        if self.rename and server["name"] != guild["name"][:100]:
            self.write("mis à jour", f"nom du serveur → « {guild['name']} »",
                       lambda: self.q.request("PATCH", "/v1/server", {"name": guild["name"][:100]}))
        self.import_roles(guild)
        self.import_channels(channels)
        if self.opt_emojis:
            self.import_emojis(guild)
        self.summary()

    # --- roles ---

    def import_roles(self, guild: dict):
        existing = {str(r["id"]): r for r in self.q.request("GET", "/v1/roles")}
        everyone = next((r for r in guild["roles"] if r["id"] == self.gid), None)
        roles = sorted((r for r in guild["roles"] if r["id"] != self.gid), key=lambda r: (-r["position"], int(r["id"])))
        for r in roles:  # highest first: each new Quarel role goes to the bottom, so the order is kept
            if r.get("managed"):
                self.note("Rôles non copiés", f"« {r['name']} » : géré par Discord (bot, boost, abonnement)")
                continue
            bits = int(r["permissions"])
            body = {"name": r["name"].lstrip("@").strip()[:100] or "rôle", "color": r.get("color", 0),
                    "permissions": quarel_perms(bits), "mentionable": bool(r.get("mentionable")), "hoist": bool(r.get("hoist"))}
            self.role_notes(r["name"], bits)
            qid = self.state.get("roles", r["id"])
            cur = existing.get(str(qid)) if qid is not None else None
            if cur is None:
                created = self.write("créé", f"rôle « {body['name']} »", lambda b=body: self.q.request("POST", "/v1/roles", b))
                qid = created["id"] if created else self.fake_id()
                if created:
                    self.state.put("roles", r["id"], qid)
                else:
                    self.state.data["roles"][r["id"]] = qid  # --dry-run: kept in memory only
                self.role_names[qid] = body["name"]
                continue
            self.role_names[qid] = body["name"]
            diff = {k: v for k, v in body.items() if (sorted(cur.get(k) or []) if k == "permissions" else cur.get(k)) != v}
            if diff:
                self.write("mis à jour", f"rôle « {body['name']} »", lambda d=diff, i=qid: self.q.request("PATCH", f"/v1/roles/{i}", d))
            else:
                self.counts["inchangé"] += 1
        if everyone:
            bits = int(everyone["permissions"])
            perms = quarel_perms(bits)
            cur = existing.get("1", {})
            for bit, (label, instead) in ALWAYS_ALLOWED.items():
                if not has(bits, bit) and not has(bits, ADMIN_BIT):
                    self.note("Restrictions qui disparaissent", f"@everyone n'a pas « {label} » : dans Quarel, {instead}")
            if sorted(cur.get("permissions") or []) != perms:
                self.write("mis à jour", "permissions de @everyone",
                           lambda: self.q.request("PATCH", "/v1/roles/1", {"permissions": perms}))
            else:
                self.counts["inchangé"] += 1

    def role_notes(self, name: str, bits: int):
        if has(bits, ADMIN_BIT):
            return
        for bit, label in NO_EQUIVALENT.items():
            if has(bits, bit) and not (bit == 30 and has(bits, 5)) and not (bit == 34 and has(bits, 4) and has(bits, 13)):
                self.note("Permissions sans équivalent", f"rôle « {name} » : {label}")

    # --- channels ---

    def import_channels(self, channels: list[dict]):
        for c in channels:
            if c["type"] not in KINDS:
                kind = {10: "fil d'annonces", 11: "fil", 12: "fil privé", 14: "annuaire"}.get(c["type"], f"type {c['type']}")
                self.note("Salons non copiés", f"« {c['name']} » ({kind})")
        existing = {str(c["id"]): c for c in self.q.request("GET", "/v1/channels")}
        if self.replace_defaults:
            self.remove_defaults(existing)
        by_id = {c["id"]: c for c in channels}
        ordered = channel_order(channels)
        for c in ordered:
            self.import_channel(c, existing)
        for c in ordered:
            self.import_overwrites(c, by_id, existing)

    def remove_defaults(self, existing: dict):
        found = {(c["type"], c["name"]) for c in existing.values()}
        if found != DEFAULT_CHANNELS or len(existing) != len(DEFAULT_CHANNELS):
            self.note("Salons par défaut", "gardés : le serveur Quarel contient déjà d'autres salons (--replace-defaults ignoré)")
            return
        text = next(c for c in existing.values() if c["type"] == "text")
        if self.q.request("GET", f"/v1/channels/{text['id']}/messages?limit=1"):
            self.note("Salons par défaut", "gardés : « général » contient déjà des messages (--replace-defaults ignoré)")
            return
        for c in sorted(existing.values(), key=lambda c: c["type"] == "category"):  # channels, then categories
            self.write("supprimé", f"salon par défaut « {c['name']} »",
                       lambda i=c["id"]: self.q.request("DELETE", f"/v1/channels/{i}"))
        if not self.dry:
            existing.clear()

    def import_channel(self, c: dict, existing: dict):
        kind = KINDS[c["type"]]
        parent = self.state.get("channels", c["parent_id"]) if c.get("parent_id") else None
        body = {"name": c["name"][:100], "topic": (c.get("topic") or "")[:1024] if kind in ("text", "announcement", "forum") else ""}
        if kind != "category":
            body["parent_id"] = parent if isinstance(parent, int) else None
        lost = []
        if c.get("rate_limit_per_user"):
            lost.append("mode lent")
        if c.get("nsfw"):
            lost.append("réservé aux adultes")
        if c.get("user_limit"):
            lost.append(f"{c['user_limit']} places au plus")
        if c.get("available_tags"):
            lost.append("étiquettes du forum")
        if len(c.get("topic") or "") > 1024:
            lost.append("fin du sujet (1024 caractères au plus)")
        if lost:
            self.note("Réglages de salon sans équivalent", f"« {c['name']} » : " + ", ".join(lost))
        if c["type"] == ANNOUNCEMENT:
            self.note("À savoir", f"« {c['name']} » (annonces) : écrire y demande aussi « Gérer les messages » dans Quarel")
        qid = self.state.get("channels", c["id"])
        cur = existing.get(str(qid)) if qid is not None else None
        if cur is not None and cur["type"] != kind:
            self.note("Salons non copiés", f"« {c['name']} » : le salon Quarel associé n'est plus du même type, laissé tel quel")
            return
        if cur is None:
            create = dict(body, type=kind, **({"stage": True} if c["type"] == STAGE else {}))
            if kind == "category":
                create.pop("topic")
            created = self.write("créé", f"{KIND_FR[c['type']]} « {c['name']} »",
                                 lambda b=create: self.q.request("POST", "/v1/channels", b))
            if created:
                self.state.put("channels", c["id"], created["id"])
                existing[str(created["id"])] = created
            else:
                self.state.data["channels"].setdefault(c["id"], self.fake_id())
            return
        diff = {k: v for k, v in body.items() if cur.get(k) != v and not (k == "topic" and kind == "category")}
        if diff:
            self.write("mis à jour", f"{KIND_FR[c['type']]} « {c['name']} »",
                       lambda d=diff, i=qid: self.q.request("PATCH", f"/v1/channels/{i}", d))
        else:
            self.counts["inchangé"] += 1

    def import_overwrites(self, c: dict, by_id: dict, existing: dict):
        qid = self.state.get("channels", c["id"])
        if not isinstance(qid, int) and not self.dry:
            return
        parent = by_id.get(c.get("parent_id")) if c["type"] != CATEGORY else None
        mine = overwrite_set(c)
        if parent is not None and mine == overwrite_set(parent):
            return  # synced with its category: Quarel's children inherit the category's overrides
        if parent is not None:
            self.unsynced_notes(c, parent)
        current = {(o["type"], str(o["id"])): o for o in (existing.get(str(qid), {}).get("overrides") or [])}
        members = 0
        for o in c.get("permission_overwrites") or []:
            if int(o["type"]) == 1:
                members += 1
                continue
            if o["id"] == self.gid:
                target = 1
            else:
                target = self.state.get("roles", o["id"])
                if target is None:
                    continue  # a managed role, already noted
            allow_bits, deny_bits = int(o["allow"]), int(o["deny"])
            allow, deny = quarel_perms(allow_bits, True), quarel_perms(deny_bits, True)
            for bit, label in SERVER_ONLY.items():
                if has(allow_bits | deny_bits, bit):
                    self.note("Droits de salon non copiés", f"« {c['name']} » : « {label} » (seulement pour tout le serveur dans Quarel)")
            for bit, (label, instead) in ALWAYS_ALLOWED.items():
                if has(deny_bits, bit):
                    self.note("Restrictions qui disparaissent", f"« {c['name']} » : refus de « {label} » (dans Quarel, {instead})")
            cur = current.get(("role", str(target)))
            if cur is not None and sorted(cur.get("allow") or []) == allow and sorted(cur.get("deny") or []) == deny:
                self.counts["inchangé"] += 1
                continue
            if not allow and not deny and cur is None:
                continue
            path = f"/v1/channels/{qid}/overrides/role/{target}"
            self.write("mis à jour" if cur else "créé", f"droits de « {self.role_names.get(target, target)} » dans « {c['name']} »",
                       lambda p=path, a=allow, d=deny: self.q.request("PUT", p, {"allow": a, "deny": d}))
        if members:
            self.note("Droits de salon non copiés", f"« {c['name']} » : {members} exception(s) pour des membres (les comptes Discord ne sont pas des comptes Quarel)")

    def unsynced_notes(self, c: dict, parent: dict):
        """Discord ignores a category's overwrites in a channel that is not
        synced with it; Quarel applies the category's, then the channel's. The
        result differs for the permissions a category overwrite sets for a role
        the channel has no overwrite for, unless the channel's @everyone
        overwrite sets them again."""
        mine = {o["id"]: o for o in c.get("permission_overwrites") or [] if int(o["type"]) == 0}
        everyone = mine.get(self.gid)
        reset = int(everyone["allow"]) | int(everyone["deny"]) if everyone else 0
        for o in parent.get("permission_overwrites") or []:
            if int(o["type"]) != 0 or o["id"] in mine:
                continue
            touched = int(o["allow"]) | int(o["deny"])
            if o["id"] != self.gid:
                touched &= ~reset
            perms = quarel_perms(touched, True)
            if not perms:
                continue
            who = "@everyone" if o["id"] == self.gid else self.role_names.get(self.state.get("roles", o["id"]), "un rôle")
            self.note("À vérifier", f"« {c['name']} » n'est pas synchronisé avec sa catégorie « {parent['name']} » : dans Quarel, "
                                    f"les droits de « {who} » dans la catégorie s'y appliquent aussi ({', '.join(perms)})")

    # --- emojis ---

    def import_emojis(self, guild: dict):
        existing = {str(e["id"]): e for e in self.q.request("GET", "/v1/emojis")}
        taken = {e["name"] for e in existing.values()}
        count = len(existing)
        for e in guild.get("emojis") or []:
            if e.get("managed") or e.get("available") is False:
                self.note("Emojis non copiés", f":{e['name']}: (géré par une intégration ou indisponible)")
                continue
            qid = self.state.get("emojis", e["id"])
            if qid is not None and str(qid) in existing:
                self.counts["inchangé"] += 1
                continue
            if count >= 100:
                self.note("Emojis non copiés", f":{e['name']}: (100 emojis au plus par serveur Quarel)")
                continue
            name = emoji_name(e["name"], taken)
            data = b"" if self.dry else self.d.emoji(e)
            if len(data) > 256 << 10:
                self.note("Emojis non copiés", f":{e['name']}: (image de plus de 256 Ko)")
                continue
            ctype = "image/gif" if data[:3] == b"GIF" else "image/webp" if data[8:12] == b"WEBP" else "image/png"
            try:
                created = self.write("créé", f"emoji :{name}:", lambda n=name, d=data, t=ctype: self.q.request(
                    "POST", "/v1/emojis?name=" + urllib.parse.quote(n), raw=d, ctype=t))
            except ApiError as err:
                self.counts["créé"] -= 1
                self.note("Emojis non copiés", f":{e['name']}: ({err.code})")
                continue
            taken.add(name)
            count += 1
            if created:
                self.state.put("emojis", e["id"], created["id"])
                existing[str(created["id"])] = created

    # --- report ---

    def summary(self):
        c = self.counts
        self.out("")
        self.out(("Essai terminé : " if self.dry else "Import terminé : ") +
                 f"{c['créé']} création(s), {c['mis à jour']} mise(s) à jour, "
                 + (f"{c['supprimé']} suppression(s), " if c["supprimé"] else "") + f"{c['inchangé']} élément(s) déjà à jour.")
        if self.dry:
            self.out("Relancez sans --dry-run pour appliquer.")
        else:
            self.out(f"Correspondances Discord → Quarel gardées dans {self.state.path} (utile pour relancer l'import).")
        for section, lines in self.notes.items():
            self.out("")
            self.out(section + " :")
            for line in lines:
                self.out("  - " + line)
        self.out("")
        self.out("Pas copiés par conception : les membres et leurs rôles, les messages, les fils, l'icône du serveur.")


# --- entry point ---

def choose_guild(discord: Discord, wanted: str | None) -> str:
    if wanted:
        return wanted
    guilds = discord.get("/users/@me/guilds")
    if len(guilds) == 1:
        return guilds[0]["id"]
    if not guilds:
        raise ImportError_("le bot Discord n'est sur aucun serveur : invitez-le d'abord")
    listing = "\n".join(f"  {g['id']}  {g['name']}" for g in guilds)
    raise ImportError_("le bot Discord est sur plusieurs serveurs : choisissez-en un avec --guild\n" + listing)


def main(argv: list[str] | None = None, out=print) -> int:
    p = argparse.ArgumentParser(
        prog="quarel-discord-import",
        description="Copie la structure d'un serveur Discord (rôles, salons, droits, emojis) sur un serveur Quarel. "
                    "Jetons : variables DISCORD_BOT_TOKEN et QUAREL_BOT_TOKEN (sinon demandés sans affichage).")
    p.add_argument("quarel", nargs="?", default=os.environ.get("QUAREL_URL", ""),
                   help="serveur Quarel : lien d'invitation, ou adresse (https://hôte:port) ; défaut : QUAREL_URL")
    p.add_argument("--sid", default=os.environ.get("QUAREL_SERVER_ID"),
                   help="identifiant du serveur Quarel (inutile si le lien d'invitation le contient) ; défaut : QUAREL_SERVER_ID")
    p.add_argument("--guild", help="identifiant du serveur Discord (inutile si le bot n'est que sur un serveur)")
    p.add_argument("--dry-run", action="store_true", help="montrer ce qui serait fait, sans rien modifier")
    p.add_argument("--state", help="fichier des correspondances (défaut : discord-import-<serveur Discord>.json)")
    p.add_argument("--no-emojis", action="store_true", help="ne pas copier les emojis")
    p.add_argument("--rename", action="store_true", help="donner au serveur Quarel le nom du serveur Discord")
    p.add_argument("--replace-defaults", action="store_true",
                   help="supprimer les salons créés d'office par Quarel (seulement s'il n'y a rien d'autre et aucun message)")
    p.add_argument("--version", action="version", version=VERSION)
    args = p.parse_args(argv)
    try:
        if not args.quarel:
            raise ImportError_("indiquez le serveur Quarel (lien d'invitation ou adresse)")
        base, sid = parse_target(args.quarel)
        sid = (args.sid or sid or "").strip().lower() or None
        discord = Discord(secret("DISCORD_BOT_TOKEN", "Jeton du bot Discord (invisible) : "))
        quarel = Quarel(base, sid, secret("QUAREL_BOT_TOKEN", "Jeton du bot Quarel (invisible) : "))
        server = quarel.request("GET", "/v1/server")  # certificate checked before this first request
        if sid and server.get("id") != sid:
            raise ImportError_(f"ce n'est pas le serveur attendu (identifiant {server.get('id')}, attendu {sid}) : arrêt")
        guild = choose_guild(discord, args.guild)
        state = State(args.state or f"discord-import-{guild}.json", guild, server["id"])
        Importer(discord, quarel, guild, state, args.dry_run, emojis=not args.no_emojis, rename=args.rename,
                 replace_defaults=args.replace_defaults, out=out).run()
        return 0
    except ImportError_ as e:
        print("Erreur : " + str(e), file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("\nInterrompu. Relancez la même commande pour reprendre.", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(main())
