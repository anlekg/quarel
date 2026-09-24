// End-to-end voice test: two headless browsers with fake microphones join the
// same voice channel through the real community server + embedded LiveKit.
// Usage: node voice-e2e.mjs <aliceURL> <bobURL>
import { chromium } from "playwright";

const [aliceURL, bobURL] = process.argv.slice(2);
const results = [];
const check = (name, ok, detail = "") => {
  results.push({ name, ok, detail });
  console.log(`${ok ? "✔" : "✘"} ${name}${detail ? " — " + detail : ""}`);
};

const browser = await chromium.launch({
  args: ["--use-fake-ui-for-media-stream", "--use-fake-device-for-media-stream", "--autoplay-policy=no-user-gesture-required"],
});
async function open(url, label) {
  // The community server uses a self-signed certificate (verified by quarelctl through its binding).
  const ctx = await browser.newContext({ permissions: ["microphone", "camera"], ignoreHTTPSErrors: true });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => console.log(`[${label}] page error: ${e.message}`));
  await page.goto(url);
  await page.waitForFunction(() => window.quarelTest && window.quarelTest.ready, null, { timeout: 10000 });
  return page;
}
const token = (url) => new URL(url).hash.slice("#token=".length);
async function api(page, url, method, path, body) {
  return page.evaluate(
    async ([t, method, path, body]) => {
      const r = await fetch(path, { method, headers: { Authorization: "Bearer " + t, "Content-Type": "application/json" }, body: body && JSON.stringify(body) });
      return { status: r.status, body: r.status === 204 ? null : await r.json() };
    },
    [token(url), method, path, body],
  );
}
async function bytesReceived(page) {
  return page.evaluate(async () => {
    let total = 0;
    for (const p of window.quarelTest.room.remoteParticipants.values()) {
      for (const pub of p.audioTrackPublications.values()) {
        if (!pub.track) continue;
        const stats = await pub.track.receiver.getStats();
        stats.forEach((s) => { if (s.type === "inbound-rtp") total += s.bytesReceived || 0; });
      }
    }
    return total;
  });
}

try {
  const alice = await open(aliceURL, "alice");
  const bob = await open(bobURL, "bob");
  check("pages chargées, gateway connecté", true);

  for (const page of [alice, bob]) {
    await page.locator(".channel", { hasText: "Général" }).getByRole("button").click();
  }
  for (const [label, page] of [["alice", alice], ["bob", bob]]) {
    await page.waitForFunction(() => window.quarelTest.room && window.quarelTest.room.remoteParticipants.size === 1, null, { timeout: 15000 });
    await page.waitForSelector("audio[data-quarel]", { state: "attached", timeout: 15000 });
    check(`${label} voit l'autre participant et reçoit sa piste audio`, true);
  }

  await alice.waitForTimeout(2500);
  const [a1, b1] = [await bytesReceived(alice), await bytesReceived(bob)];
  check("l'audio circule dans les deux sens", a1 > 1000 && b1 > 1000, `alice a reçu ${a1} octets, bob ${b1} octets`);

  const states = (await api(alice, aliceURL, "GET", "/v1/voice/states")).body;
  check("le serveur connaît les 2 participants (webhooks LiveKit)", states.length === 2, JSON.stringify(states.map((s) => s.channel_id)));

  await bob.getByRole("button", { name: "Couper le micro" }).click();
  await alice.waitForFunction(() => document.body.innerText.includes("micro coupé"), null, { timeout: 5000 });
  check("micro coupé de bob visible en direct chez alice", true);

  const members = (await api(alice, aliceURL, "GET", "/v1/members")).body;
  const bobID = members.find((m) => m.handle.startsWith("bob@")).id;
  const voiceCh = states[0].channel_id;
  const bobState = async () => (await api(alice, aliceURL, "GET", "/v1/voice/states")).body.find((s) => s.member_id === bobID);
  const waitState = async (pred, what) => {
    for (let i = 0; i < 50; i++) {
      const st = await bobState();
      if (st && pred(st)) return st;
      await alice.waitForTimeout(100);
    }
    throw new Error("état vocal attendu non atteint : " + what);
  };
  const bobSources = () => bob.evaluate(() => {
    const p = window.quarelTest.room.localParticipant.permissions || {};
    return { canPublish: p.canPublish, sources: [...(p.canPublishSources || [])], canSubscribe: p.canSubscribe };
  });

  // Camera (fake device): alice receives bob's video, the server learns it from LiveKit.
  await bob.getByRole("button", { name: "Activer la caméra" }).click();
  await alice.waitForSelector(`.tile[data-identity="${bobID}"] video`, { timeout: 10000 });
  await alice.waitForTimeout(1500);
  const videoBytes = await alice.evaluate(async () => {
    let total = 0;
    for (const p of window.quarelTest.room.remoteParticipants.values())
      for (const pub of p.videoTrackPublications.values()) {
        if (!pub.track) continue;
        (await pub.track.receiver.getStats()).forEach((s) => { if (s.type === "inbound-rtp") total += s.bytesReceived || 0; });
      }
    return total;
  });
  check("caméra de bob reçue par alice", videoBytes > 1000, `${videoBytes} octets de vidéo`);
  await waitState((s) => s.video, "video");
  check("le serveur sait que bob a sa caméra allumée (webhook track_published)", true);

  await bob.getByRole("button", { name: "Partager l'écran" }).click();
  await alice.waitForFunction((id) => [...document.querySelectorAll(`.tile[data-identity="${id}"] span`)].some((e) => e.textContent.includes("écran")), bobID, { timeout: 8000 })
    .then(() => check("partage d'écran de bob reçu par alice", true))
    .catch(async () => check("partage d'écran de bob reçu par alice", false, (await bob.evaluate(() => window.quarelTest.errors)).join(" | ")));
  await waitState((s) => s.screen, "partage d'écran").then(() => check("le serveur sait que bob partage son écran", true)).catch((e) => check("le serveur sait que bob partage son écran", false, e.message));

  let r = await api(alice, aliceURL, "PUT", `/v1/channels/${voiceCh}/overrides/member/${bobID}`, { allow: [], deny: ["stream"] });
  await bob.waitForFunction(() => { const p = window.quarelTest.room.localParticipant.permissions; return p && !(p.canPublishSources || []).includes(1); }, null, { timeout: 5000 })
    .catch(async (e) => { throw new Error("permissions de bob : " + JSON.stringify(await bobSources())); });
  await alice.waitForFunction((id) => !document.querySelector(`.tile[data-identity="${id}"]`), bobID, { timeout: 8000 });
  await waitState((s) => !s.video && !s.screen && !s.can_stream, "caméra et écran coupés");
  check("retrait de stream : caméra et partage d'écran de bob coupés par LiveKit", r.status === 200);

  // Server mute / deafen by a moderator (here the owner).
  r = await api(alice, aliceURL, "PATCH", `/v1/voice/states/${bobID}`, { mute: true });
  await bob.waitForFunction(() => { const p = window.quarelTest.room.localParticipant.permissions; return p && !(p.canPublishSources || []).includes(2); }, null, { timeout: 5000 });
  await alice.waitForFunction(() => document.body.innerText.includes("micro coupé par la modération"), null, { timeout: 5000 });
  check("micro de bob coupé par la modération (LiveKit + affichage)", r.status === 200, JSON.stringify(await bobSources()));
  r = await api(alice, aliceURL, "PATCH", `/v1/voice/states/${bobID}`, { mute: false, deaf: true });
  await bob.waitForFunction(() => { const p = window.quarelTest.room.localParticipant.permissions; return p && p.canSubscribe === false; }, null, { timeout: 5000 })
    .catch(async () => { throw new Error("sourdine : permissions de bob " + JSON.stringify(await bobSources())); });
  await bob.waitForFunction(() => document.querySelectorAll("audio[data-quarel]").length === 0, null, { timeout: 5000 });
  check("son coupé par la modération : bob ne reçoit plus l'audio", r.status === 200);
  await api(alice, aliceURL, "PATCH", `/v1/voice/states/${bobID}`, { deaf: false });
  await bob.waitForSelector("audio[data-quarel]", { state: "attached", timeout: 8000 })
    .then(() => check("fin de la sourdine imposée : bob entend de nouveau, sans se reconnecter", true))
    .catch(() => check("fin de la sourdine imposée : bob entend de nouveau, sans se reconnecter", false));

  r = await api(alice, aliceURL, "PUT", `/v1/channels/${voiceCh}/overrides/member/${bobID}`, { allow: [], deny: ["speak", "stream"] });
  await bob.waitForFunction(() => { const p = window.quarelTest.room.localParticipant.permissions; return p && p.canSubscribe && !p.canPublish; }, null, { timeout: 5000 });
  check("retrait de la permission speak appliqué en direct par LiveKit", r.status === 200);

  // Move: bob's page follows VOICE_MOVE into the other channel.
  const other = (await api(alice, aliceURL, "POST", "/v1/channels", { type: "voice", name: "Réunion" })).body;
  await bob.waitForFunction((id) => document.body.innerText.includes("Réunion"), other.id, { timeout: 5000 });
  r = await api(alice, aliceURL, "PATCH", `/v1/voice/states/${bobID}`, { channel_id: other.id });
  await bob.waitForFunction((id) => window.quarelTest.joinedChannel === id && window.quarelTest.room, other.id, { timeout: 10000 });
  await waitState((s) => s.channel_id === other.id, "bob dans Réunion");
  await alice.waitForFunction(() => window.quarelTest.room.remoteParticipants.size === 0, null, { timeout: 5000 });
  check("bob déplacé dans « Réunion » par la modération", r.status === 200);

  r = await api(alice, aliceURL, "PUT", `/v1/channels/${other.id}/overrides/member/${bobID}`, { allow: [], deny: ["connect"] });
  await bob.waitForFunction(() => window.quarelTest.room === null, null, { timeout: 5000 });
  check("retrait de la permission connect : bob est déconnecté du vocal", r.status === 200);

  const errs = [...(await alice.evaluate(() => window.quarelTest.errors)), ...(await bob.evaluate(() => window.quarelTest.errors))];
  check("aucune erreur dans les pages", errs.length === 0, errs.join(" | "));
} catch (e) {
  check("scénario complet", false, e.message);
} finally {
  await browser.close();
}
process.exit(results.every((r) => r.ok) ? 0 : 1);
