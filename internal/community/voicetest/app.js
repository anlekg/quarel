// Quarel voice test page: joins voice channels through the community server
// API and LiveKit. The session token comes from the URL fragment (#token=…),
// which browsers never send to the server.
"use strict";

const LK = LivekitClient;
const token = new URLSearchParams(location.hash.slice(1)).get("token");
const $ = (id) => document.getElementById(id);

// Exposed for automated tests.
const qt = (window.quarelTest = { room: null, joinedChannel: null, errors: [], ready: false });

const state = {
  me: null,
  members: {},      // id → member
  channels: [],     // visible voice channels
  voice: {},        // member id → voice state (server truth)
  speaking: new Set(),
  muted: false,
  deaf: false,
};

function log(msg, isErr) {
  const line = document.createElement("div");
  line.textContent = new Date().toLocaleTimeString() + "  " + msg;
  if (isErr) { line.className = "err"; qt.errors.push(msg); }
  $("log").prepend(line);
}

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return null;
  const data = await res.json();
  if (!res.ok) throw new Error((data.error && data.error.code) + " — " + (data.error && data.error.message));
  return data;
}

function name(id) {
  const m = state.members[id];
  return m ? m.display_name : "?";
}

function render() {
  const box = $("channels");
  box.replaceChildren();
  if (state.channels.length === 0) box.textContent = "Aucun salon vocal visible.";
  for (const ch of state.channels) {
    const row = document.createElement("div");
    row.className = "channel";
    const left = document.createElement("div");
    left.innerHTML = "🔊 <strong></strong>";
    left.querySelector("strong").textContent = ch.name;
    const people = document.createElement("ul");
    people.className = "people";
    for (const v of Object.values(state.voice)) {
      if (v.channel_id !== ch.id) continue;
      const li = document.createElement("li");
      let flags = "";
      if (v.self_deaf) flags += " 🔇 sourdine";
      else if (v.self_mute) flags += " 🎙️ micro coupé";
      if (!v.can_speak) flags += " (ne peut pas parler)";
      li.textContent = name(v.member_id) + flags;
      if (state.speaking.has(v.member_id)) li.classList.add("speaking");
      people.append(li);
    }
    left.append(people);
    const btn = document.createElement("button");
    btn.className = "primary";
    btn.textContent = qt.joinedChannel === ch.id ? "Connecté" : "Rejoindre";
    btn.disabled = qt.joinedChannel === ch.id;
    btn.onclick = () => join(ch).catch((e) => log("Échec : " + e.message, true));
    row.append(left, btn);
    box.append(row);
  }
  const inVoice = qt.room !== null;
  $("mute").disabled = $("deaf").disabled = $("leave").disabled = !inVoice;
  $("mute").textContent = state.muted ? "Réactiver le micro" : "Couper le micro";
  $("mute").classList.toggle("on", state.muted);
  $("deaf").textContent = state.deaf ? "Réactiver le son" : "Sourdine";
  $("deaf").classList.toggle("on", state.deaf);
  const ch = state.channels.find((c) => c.id === qt.joinedChannel);
  $("status").textContent = inVoice && ch ? "Dans 🔊 " + ch.name : "Hors d'un salon vocal";
}

function applyAudioMute() {
  document.querySelectorAll("audio[data-quarel]").forEach((el) => (el.muted = state.deaf));
}

async function join(ch) {
  if (qt.room) await leave();
  const j = await api("POST", "/v1/channels/" + ch.id + "/voice/join");
  const room = new LK.Room({ adaptiveStream: true, dynacast: true });
  room
    .on(LK.RoomEvent.TrackSubscribed, (track, pub, participant) => {
      if (track.kind !== "audio") return;
      const el = track.attach();
      el.dataset.quarel = participant.identity;
      el.muted = state.deaf;
      document.body.append(el);
      log("Audio reçu de " + name(participant.identity));
    })
    .on(LK.RoomEvent.TrackUnsubscribed, (track) => track.detach().forEach((el) => el.remove()))
    .on(LK.RoomEvent.ActiveSpeakersChanged, (speakers) => {
      state.speaking = new Set(speakers.map((p) => p.identity));
      render();
    })
    .on(LK.RoomEvent.ParticipantPermissionsChanged, (_prev, participant) => {
      if (participant && participant.isLocal) {
        const can = participant.permissions && participant.permissions.canPublish;
        log(can ? "Vous pouvez de nouveau parler." : "Vous n'avez plus le droit de parler.");
      }
    })
    .on(LK.RoomEvent.Disconnected, (reason) => {
      log("Déconnexion du salon vocal (" + (LK.DisconnectReason[reason] || reason) + ")");
      cleanup();
    });
  await room.connect(j.url, j.token);
  qt.room = room;
  qt.joinedChannel = ch.id;
  log("Connexion à 🔊 " + ch.name + (j.can_speak ? "" : " — écoute seule (pas la permission de parler)"));
  if (j.can_speak && !state.muted) {
    try {
      await room.localParticipant.setMicrophoneEnabled(true);
      log("Micro activé");
    } catch (e) {
      log("Micro indisponible : " + e.message, true);
    }
  }
  render();
}

function cleanup() {
  document.querySelectorAll("audio[data-quarel]").forEach((el) => el.remove());
  qt.room = null;
  qt.joinedChannel = null;
  state.speaking.clear();
  render();
}

async function leave() {
  if (!qt.room) return;
  const room = qt.room;
  cleanup();
  await room.disconnect();
}

$("leave").onclick = () => leave();
$("mute").onclick = async () => {
  state.muted = !state.muted;
  if (!state.muted && state.deaf) state.deaf = false;
  if (qt.room) await qt.room.localParticipant.setMicrophoneEnabled(!state.muted).catch((e) => log(e.message, true));
  applyAudioMute();
  await api("PATCH", "/v1/voice/state", { self_mute: state.muted, self_deaf: state.deaf }).catch((e) => log(e.message, true));
  render();
};
$("deaf").onclick = async () => {
  state.deaf = !state.deaf;
  state.muted = state.deaf || state.muted;
  if (qt.room) await qt.room.localParticipant.setMicrophoneEnabled(!state.muted).catch((e) => log(e.message, true));
  applyAudioMute();
  await api("PATCH", "/v1/voice/state", { self_mute: state.muted, self_deaf: state.deaf }).catch((e) => log(e.message, true));
  render();
};

function applyChannels(channels, voiceStates) {
  state.channels = channels.filter((c) => c.type === "voice");
  state.voice = {};
  for (const v of voiceStates || []) state.voice[v.member_id] = v;
}

// Live updates from the community server gateway.
function connectGateway() {
  const ws = new WebSocket(location.origin.replace(/^http/, "ws") + "/v1/gateway");
  ws.onopen = () => ws.send(JSON.stringify({ op: "auth", token }));
  ws.onmessage = (msg) => {
    const ev = JSON.parse(msg.data);
    const d = ev.d;
    switch (ev.t) {
      case "READY":
        state.me = d.member;
        for (const m of d.members) state.members[m.id] = m;
        applyChannels(d.channels, d.voice_states);
        $("who").textContent = "Connexion à « " + d.server.name + " » en tant que " + d.member.display_name;
        qt.ready = true;
        break;
      case "CHANNELS_SYNC":
        applyChannels(d.channels, d.voice_states);
        break;
      case "VOICE_STATE_UPDATE":
        if (d.channel_id === null) delete state.voice[d.member_id];
        else state.voice[d.member_id] = d;
        break;
      case "MEMBER_JOIN":
      case "MEMBER_UPDATE":
        state.members[d.id] = d;
        break;
      case "CHANNEL_CREATE":
      case "CHANNEL_UPDATE":
        state.channels = state.channels.filter((c) => c.id !== d.id);
        if (d.type === "voice") state.channels.push(d);
        break;
      case "CHANNEL_DELETE":
        state.channels = state.channels.filter((c) => c.id !== d.id);
        break;
    }
    render();
  };
  ws.onclose = (e) => {
    log("Connexion temps réel fermée (" + e.code + " " + e.reason + ")", e.code !== 1000);
    leave();
  };
}

if (!token) {
  $("missing").hidden = false;
  $("who").textContent = "Jeton manquant";
} else {
  connectGateway();
  render();
}
