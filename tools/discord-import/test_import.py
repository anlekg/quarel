"""Tests of quarel_discord_import.

Unit tests need nothing. The integration test runs the real Quarel services
(bin/ of the repository: make build) with a self-signed server, prepares the
owner and the bot with quarelctl, and serves a Discord server from a fake
Discord API. Run: make discord-import-test
"""

import http.server
import io
import json
import os
import re
import shutil
import struct
import subprocess
import tempfile
import threading
import time
import unittest
import zlib
from contextlib import redirect_stderr

import quarel_discord_import as qdi

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
BIN = os.path.join(REPO, "bin")
ID_PORT, SRV_PORT, DISCORD_PORT = 28380, 28390, 28395


def png(w=4, h=4) -> bytes:
    def chunk(t, d):
        return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d))
    raw = b"".join(b"\x00" + b"\x5f\xb8\xa5" * w for _ in range(h))
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))


def bits(*n):
    return str(sum(1 << b for b in n))


G = "900000000000000001"  # the guild; its @everyone role has the same ID
GUILD = {
    "id": G, "name": "Le Repaire",
    "roles": [
        {"id": G, "name": "@everyone", "position": 0, "color": 0, "hoist": False, "mentionable": False, "managed": False,
         # view, send, reactions, invite, connect, speak — no history, no public threads
         "permissions": bits(10, 11, 6, 0, 20, 21, 14, 26, 31, 38)},
        {"id": "900000000000000010", "name": "Admin", "position": 5, "color": 0xE0555D, "hoist": True, "mentionable": False,
         "managed": False, "permissions": bits(3)},
        {"id": "900000000000000011", "name": "Modo", "position": 4, "color": 0x5FB8A5, "hoist": True, "mentionable": True,
         "managed": False, "permissions": bits(1, 2, 13, 40, 22, 34, 27)},
        {"id": "900000000000000012", "name": "MEE6", "position": 3, "color": 0, "hoist": False, "mentionable": False,
         "managed": True, "permissions": bits(3)},
        {"id": "900000000000000013", "name": "Membre", "position": 1, "color": 0x7289DA, "hoist": False, "mentionable": True,
         "managed": False, "permissions": bits(15, 9)},
    ],
    "emojis": [
        {"id": "900000000000000100", "name": "Parrot-Party", "animated": False, "managed": False, "available": True},
        {"id": "900000000000000101", "name": "big", "animated": False, "managed": False, "available": True},
        {"id": "900000000000000102", "name": "twitch", "animated": False, "managed": True, "available": True},
    ],
}
STAFF_OW = [
    {"id": G, "type": 0, "allow": "0", "deny": bits(10)},
    {"id": "900000000000000011", "type": 0, "allow": bits(10), "deny": "0"},
]
CHANNELS = [
    {"id": "900000000000000200", "type": 5, "name": "annonces", "position": 0, "parent_id": None, "topic": "Les nouvelles"},
    {"id": "900000000000000201", "type": 13, "name": "Conférence", "position": 1, "parent_id": None},
    {"id": "900000000000000210", "type": 4, "name": "Général", "position": 0},
    {"id": "900000000000000211", "type": 0, "name": "discussion", "position": 0, "parent_id": "900000000000000210",
     "topic": "On parle de tout", "rate_limit_per_user": 10},
    {"id": "900000000000000212", "type": 2, "name": "Salon vocal", "position": 1, "parent_id": "900000000000000210", "user_limit": 5},
    {"id": "900000000000000213", "type": 15, "name": "idées", "position": 2, "parent_id": "900000000000000210",
     "available_tags": [{"id": "1", "name": "bug"}]},
    {"id": "900000000000000220", "type": 4, "name": "Staff", "position": 1, "permission_overwrites": STAFF_OW},
    {"id": "900000000000000221", "type": 0, "name": "staff-chat", "position": 0, "parent_id": "900000000000000220",
     "permission_overwrites": STAFF_OW},  # synced with its category
    {"id": "900000000000000222", "type": 0, "name": "logs", "position": 1, "parent_id": "900000000000000220",
     "permission_overwrites": [
         {"id": G, "type": 0, "allow": "0", "deny": bits(10, 16)},
         {"id": "900000000000000013", "type": 0, "allow": bits(10), "deny": bits(11, 0)},
         {"id": "900000000000000999", "type": 1, "allow": bits(10), "deny": "0"},
     ]},
    {"id": "900000000000000230", "type": 11, "name": "un fil", "parent_id": "900000000000000211"},
]
IMAGES = {"900000000000000100": png(), "900000000000000101": png() + b"\x00" * (300 << 10)}


class FakeDiscord(http.server.BaseHTTPRequestHandler):
    calls: list = []

    def do_GET(self):
        FakeDiscord.calls.append(self.path)
        if self.path.startswith("/cdn/emojis/"):
            data = IMAGES.get(self.path.split("/")[-1].split(".")[0])
            return self.reply(200 if data else 404, data or b"", "image/png")
        if self.headers.get("Authorization") != "Bot discord-test-token":
            return self.reply(401, b'{"message": "401: Unauthorized"}')
        body = {"/api/users/@me/guilds": [{"id": G, "name": GUILD["name"]}], f"/api/guilds/{G}": GUILD,
                f"/api/guilds/{G}/channels": CHANNELS}.get(self.path)
        self.reply(200 if body is not None else 404, json.dumps(body if body is not None else {"message": "Unknown"}).encode())

    def reply(self, code, data, ctype="application/json"):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *a):
        pass


class UnitTests(unittest.TestCase):
    def test_permissions(self):
        self.assertEqual(qdi.quarel_perms(int(bits(10, 11, 16, 14))), ["send_messages", "view_channel"])
        self.assertEqual(qdi.quarel_perms(int(bits(0, 28, 10)), channel=True), ["view_channel"])
        self.assertEqual(qdi.quarel_perms(int(bits(3))), ["administrator"])

    def test_targets(self):
        self.assertEqual(qdi.parse_target("quarel://example.org:8090/ABCDEFGHIJ?sid=abc"), ("https://example.org:8090", "abc"))
        self.assertEqual(qdi.parse_target("https://app.quarel.app/join#10.0.0.2:8090/ABCDEFGHIJ?sid=xyz"), ("https://10.0.0.2:8090", "xyz"))
        self.assertEqual(qdi.parse_target("example.org"), ("https://example.org", None))
        self.assertEqual(qdi.parse_target("http://localhost:8090"), ("http://localhost:8090", None))

    def test_emoji_names(self):
        self.assertEqual(qdi.emoji_name("Parrot-Party", set()), "parrot_party")
        self.assertEqual(qdi.emoji_name("ok", {"ok"}), "ok_2")
        self.assertEqual(qdi.emoji_name("é", set()), "emoji")
        self.assertEqual(len(qdi.emoji_name("x" * 40, {"x" * 32})), 32)

    def test_order(self):
        names = [c["name"] for c in qdi.channel_order(CHANNELS)]
        self.assertEqual(names, ["annonces", "Conférence", "Général", "discussion", "idées", "Salon vocal", "Staff", "staff-chat", "logs"])

    def test_unsynced_channel(self):
        with tempfile.TemporaryDirectory() as d:
            imp = qdi.Importer(None, None, G, qdi.State(os.path.join(d, "s.json"), G, "sid"), dry_run=True, out=lambda _: None)
            imp.state.data["roles"]["900000000000000011"] = 7
            imp.role_names[7] = "Modo"
            cat = {"name": "Staff", "permission_overwrites": STAFF_OW}
            # No @everyone overwrite in the channel: Discord shows it to everyone, Quarel hides it (category).
            imp.unsynced_notes({"name": "ouvert", "permission_overwrites": []}, cat)
            notes = imp.notes["À vérifier"]
            self.assertEqual(len(notes), 2)
            self.assertIn("droits de « @everyone » dans la catégorie s'y appliquent aussi (view_channel)", notes[0])
            self.assertIn("« Modo »", notes[1])

    def test_http_only_on_loopback(self):
        with self.assertRaises(qdi.ImportError_):
            qdi.Quarel("http://example.org", None, "t")


@unittest.skipUnless(all(os.path.exists(os.path.join(BIN, b)) for b in ("quarel-identity", "quarel-server", "quarelctl")),
                     "make build first")
class IntegrationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.dir = tempfile.mkdtemp(prefix="quarel-discord-import-")
        env = dict(os.environ, QUAREL_RATE_LIMITS="off", QUAREL_UPNP="off", QUAREL_ADMIN_ADDR="off")
        cls.procs, cls.logs = [], []
        cls.id_log = os.path.join(cls.dir, "id.log")
        cls.srv_log = os.path.join(cls.dir, "srv.log")
        cls.procs.append(subprocess.Popen([os.path.join(BIN, "quarel-identity")], stdout=cls.log(cls.id_log), stderr=subprocess.STDOUT,
                                          env=dict(env, QUAREL_ADDR=f"127.0.0.1:{ID_PORT}", QUAREL_ISSUER=f"localhost:{ID_PORT}",
                                                   QUAREL_DATA_DIR=os.path.join(cls.dir, "id"))))
        time.sleep(0.5)
        cls.procs.append(subprocess.Popen([os.path.join(BIN, "quarel-server")], stdout=cls.log(cls.srv_log), stderr=subprocess.STDOUT,
                                          env=dict(env, QUAREL_ADDR=f"127.0.0.1:{SRV_PORT}", QUAREL_TRUSTED_ISSUERS=f"localhost:{ID_PORT}",
                                                   QUAREL_DATA_DIR=os.path.join(cls.dir, "srv"), QUAREL_VOICE="off")))
        cls.discord = http.server.ThreadingHTTPServer(("127.0.0.1", DISCORD_PORT), FakeDiscord)
        threading.Thread(target=cls.discord.serve_forever, daemon=True).start()
        qdi.DISCORD_API = f"http://127.0.0.1:{DISCORD_PORT}/api"
        qdi.DISCORD_CDN = f"http://127.0.0.1:{DISCORD_PORT}/cdn"
        time.sleep(0.8)
        cls.ctl_env = dict(os.environ, XDG_CONFIG_HOME=os.path.join(cls.dir, "cfg"), QUAREL_PASSWORD="motdepasse-solide")
        cls.ctl("register", "alice@example.com", "alice")
        cls.ctl("verify-email", "alice@example.com", re.findall(r"Quarel : (\d{6})", cls.read(cls.id_log))[-1])
        cls.ctl("login", "alice", "pc-alice")
        claim = re.findall(r"unique\) : ([a-z0-9]+)", cls.read(cls.srv_log))[-1]
        cls.ctl("claim", f"localhost:{SRV_PORT}", claim)
        out = cls.ctl("bot-create", "Importeur")
        cls.token = re.search(r"qb_[A-Za-z0-9_-]+", out).group(0)
        cls.sid = re.search(r"QUAREL_SERVER_ID=([a-z2-7]+)", out).group(1)
        cls.ctl("role-create", "Importation", "administrator")
        cls.ctl("role-add", "Importeur", "Importation")
        cls.state = os.path.join(cls.dir, "state.json")
        cls.q = qdi.Quarel(f"https://localhost:{SRV_PORT}", cls.sid, cls.token, pause=0)

    @classmethod
    def tearDownClass(cls):
        cls.discord.shutdown()
        cls.discord.server_close()
        for p in cls.procs:
            p.kill()
            p.wait()
        for f in cls.logs:
            f.close()
        shutil.rmtree(cls.dir, ignore_errors=True)

    @classmethod
    def log(cls, path):
        f = open(path, "w")
        cls.logs.append(f)
        return f

    @staticmethod
    def read(path):
        with open(path) as f:
            return f.read()

    @classmethod
    def ctl(cls, *args):
        r = subprocess.run([os.path.join(BIN, "quarelctl"), "-s", f"http://127.0.0.1:{ID_PORT}", "-p", "alice", *args],
                           env=cls.ctl_env, capture_output=True, text=True, timeout=60)
        if r.returncode != 0:
            raise AssertionError(f"quarelctl {args}: {r.stdout}{r.stderr}")
        return r.stdout

    def run_import(self, *extra, sid=None):
        lines, err = [], io.StringIO()
        os.environ.update(DISCORD_BOT_TOKEN="discord-test-token", QUAREL_BOT_TOKEN=self.token)
        with redirect_stderr(err):
            code = qdi.main([f"https://localhost:{SRV_PORT}", "--sid", sid or self.sid, "--state", self.state, *extra], out=lines.append)
        return code, "\n".join(lines), err.getvalue()

    def test_import(self):
        # A wrong server ID: refused before anything is sent.
        code, _, err = self.run_import(sid="a" * 26)
        self.assertEqual(code, 1)
        self.assertIn("ce n'est pas le serveur attendu", err)

        # Dry run: the plan, nothing written.
        roles_before = self.q.request("GET", "/v1/roles")
        code, out, err = self.run_import("--dry-run", "--replace-defaults")
        self.assertEqual(code, 0, err)
        self.assertIn("[essai] Créé : rôle « Modo »", out)
        self.assertIn("[essai] Créé : forum « idées »", out)
        self.assertEqual(self.q.request("GET", "/v1/roles"), roles_before)
        self.assertFalse(os.path.exists(self.state))

        # The import.
        code, out, err = self.run_import("--rename", "--replace-defaults")
        self.assertEqual(code, 0, err)
        server = self.q.request("GET", "/v1/server")
        self.assertEqual(server["name"], "Le Repaire")

        roles = self.q.request("GET", "/v1/roles")  # highest first
        names = [r["name"] for r in roles]
        self.assertEqual(names, ["Importation", "Admin", "Modo", "Membre", "@everyone"])
        by_name = {r["name"]: r for r in roles}
        self.assertEqual(sorted(by_name["Modo"]["permissions"]), ["ban_members", "kick_members", "manage_messages", "moderate_members", "mute_members"])
        self.assertTrue(by_name["Modo"]["hoist"] and by_name["Modo"]["mentionable"])
        self.assertEqual(by_name["Modo"]["color"], 0x5FB8A5)
        self.assertEqual(by_name["Admin"]["permissions"], ["administrator"])
        self.assertEqual(sorted(by_name["@everyone"]["permissions"]), ["add_reactions", "connect", "create_invite", "send_messages", "speak", "view_channel"])

        chans = self.q.request("GET", "/v1/channels")
        by = {c["name"]: c for c in chans}
        self.assertNotIn("général", by)  # Quarel's default channels replaced
        self.assertEqual(by["annonces"]["type"], "announcement")
        self.assertEqual(by["annonces"]["topic"], "Les nouvelles")
        self.assertEqual(by["Conférence"]["type"], "voice")
        self.assertTrue(by["Conférence"].get("stage"))
        self.assertEqual(by["idées"]["type"], "forum")
        self.assertEqual(by["discussion"]["parent_id"], by["Général"]["id"])
        self.assertEqual(by["Salon vocal"]["parent_id"], by["Général"]["id"])
        self.assertNotIn("un fil", by)
        # Discord's order within a parent: text-like, then voice.
        general = sorted((c for c in chans if c.get("parent_id") == by["Général"]["id"]), key=lambda c: (c["position"], c["id"]))
        self.assertEqual([c["name"] for c in general], ["discussion", "idées", "Salon vocal"])

        def ov(channel):
            return {(o["type"], o["id"]): (sorted(o["allow"]), sorted(o["deny"])) for o in by[channel]["overrides"]}
        modo, membre = str(by_name["Modo"]["id"]), str(by_name["Membre"]["id"])
        self.assertEqual(ov("Staff"), {("role", "1"): ([], ["view_channel"]), ("role", modo): (["view_channel"], [])})
        self.assertEqual(ov("staff-chat"), {})  # synced: inherits the category
        self.assertEqual(ov("logs"), {("role", "1"): ([], ["view_channel"]), ("role", membre): (["view_channel"], ["send_messages"])})

        emojis = self.q.request("GET", "/v1/emojis")
        self.assertEqual([e["name"] for e in emojis], ["parrot_party"])

        # The report.
        for text in ["« MEE6 » : géré par Discord", "« un fil » (fil)", "« discussion » : mode lent",
                     "« Salon vocal » : 5 places au plus", "« idées » : étiquettes du forum",
                     "« logs » : 1 exception(s) pour des membres", "« logs » : « Créer une invitation »",
                     "Voir les anciens messages", "rôle « Modo » : Gérer les pseudos", "rôle « Modo » : Gérer les fils",
                     ":big: (image de plus de 256 Ko)",
                     ":twitch: (géré par une intégration", "« annonces » (annonces) : écrire y demande aussi"]:
            self.assertIn(text, out)
        self.assertNotIn("Admin", "\n".join(l for l in out.splitlines() if "sans équivalent" in l))
        # "logs" is not synced with "Staff", but its @everyone overwrite sets
        # view_channel again: same result in Quarel, nothing to check.
        self.assertNotIn("n'est pas synchronisé", out)
        self.assertIn("Supprimé : salon par défaut « général »", out)

        # Again: nothing to create, nothing to change.
        code, out, err = self.run_import("--rename")
        self.assertEqual(code, 0, err)
        self.assertIn("Import terminé : 0 création(s), 0 mise(s) à jour", out)
        self.assertEqual(len(self.q.request("GET", "/v1/channels")), len(chans))

        # A role renamed on Discord and a channel deleted on Quarel: fixed by the next run.
        GUILD["roles"][2]["name"] = "Modération"
        self.q.request("DELETE", f"/v1/channels/{by['idées']['id']}")
        try:
            code, out, err = self.run_import()
        finally:
            GUILD["roles"][2]["name"] = "Modo"
        self.assertEqual(code, 0, err)
        self.assertIn("Mis à jour : rôle « Modération »", out)
        self.assertIn("Créé : forum « idées »", out)
        self.assertIn("Modération", [r["name"] for r in self.q.request("GET", "/v1/roles")])


if __name__ == "__main__":
    unittest.main()
