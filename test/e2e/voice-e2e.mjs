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
  const ctx = await browser.newContext({ permissions: ["microphone"], ignoreHTTPSErrors: true });
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
  let r = await api(alice, aliceURL, "PUT", `/v1/channels/${voiceCh}/overrides/member/${bobID}`, { allow: [], deny: ["speak"] });
  await bob.waitForFunction(() => window.quarelTest.room && window.quarelTest.room.localParticipant.permissions && !window.quarelTest.room.localParticipant.permissions.canPublish, null, { timeout: 5000 });
  check("retrait de la permission speak appliqué en direct par LiveKit", r.status === 200);

  r = await api(alice, aliceURL, "PUT", `/v1/channels/${voiceCh}/overrides/member/${bobID}`, { allow: [], deny: ["connect"] });
  await bob.waitForFunction(() => window.quarelTest.room === null, null, { timeout: 5000 });
  check("retrait de la permission connect : bob est déconnecté du vocal", r.status === 200);
  await alice.waitForFunction(() => window.quarelTest.room.remoteParticipants.size === 0, null, { timeout: 5000 });
  check("alice ne voit plus bob dans le salon", true);

  const errs = [...(await alice.evaluate(() => window.quarelTest.errors)), ...(await bob.evaluate(() => window.quarelTest.errors))];
  check("aucune erreur dans les pages", errs.length === 0, errs.join(" | "));
} catch (e) {
  check("scénario complet", false, e.message);
} finally {
  await browser.close();
}
process.exit(results.every((r) => r.ok) ? 0 : 1);
