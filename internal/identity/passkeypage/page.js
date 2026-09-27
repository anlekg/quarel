// The WebAuthn ceremony of this identity service (see internal/identity/passkeys.go):
// the ticket comes in the fragment, never in a URL sent to a server.
'use strict'
;(() => {
  const ticket = location.hash.slice(1)
  history.replaceState(null, '', location.pathname)
  addEventListener('hashchange', () => location.reload()) // a new ticket in the same tab: start again
  const status = document.getElementById('status')
  const what = document.getElementById('what')
  const warn = document.getElementById('warn')
  const codeLine = document.getElementById('code')
  const go = document.getElementById('go')
  const say = (text, cls) => { status.textContent = text; status.className = cls || '' }

  const toBytes = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4)), (c) => c.charCodeAt(0))
  const toB64 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')

  const post = async (path, body) => {
    const res = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
    const data = res.status === 204 ? null : await res.json().catch(() => null)
    if (!res.ok) throw new Error((data && data.error && data.error.code) || 'http_' + res.status)
    return data
  }

  // Options from the server (base64url strings) → what navigator.credentials wants.
  const creationOptions = (o) => ({
    ...o, challenge: toBytes(o.challenge), user: { ...o.user, id: toBytes(o.user.id) },
    excludeCredentials: (o.excludeCredentials || []).map((c) => ({ ...c, id: toBytes(c.id) })),
  })
  const requestOptions = (o) => ({ ...o, challenge: toBytes(o.challenge), allowCredentials: (o.allowCredentials || []).map((c) => ({ ...c, id: toBytes(c.id) })) })

  const encode = (cred) => {
    const r = cred.response
    const out = { id: cred.id, rawId: toB64(cred.rawId), type: cred.type, clientExtensionResults: cred.getClientExtensionResults(), response: { clientDataJSON: toB64(r.clientDataJSON) } }
    if (r.attestationObject) {
      out.response.attestationObject = toB64(r.attestationObject)
      if (r.getTransports) out.response.transports = r.getTransports()
    } else {
      out.response.authenticatorData = toB64(r.authenticatorData)
      out.response.signature = toB64(r.signature)
      if (r.userHandle) out.response.userHandle = toB64(r.userHandle)
    }
    if (cred.authenticatorAttachment) out.authenticatorAttachment = cred.authenticatorAttachment
    return out
  }

  const errors = {
    ticket_expired: 'Ce lien a expiré. Recommencez depuis l’application Quarel.',
    invalid_passkey: 'La clé n’a pas pu être vérifiée. Recommencez depuis l’application Quarel.',
    no_passkey: 'Aucune clé d’accès sur ce compte.',
    NotAllowedError: 'Opération annulée ou délai dépassé.',
    InvalidStateError: 'Cette clé est déjà enregistrée sur votre compte.',
  }

  let prepared = null
  const run = async () => {
    go.hidden = true
    try {
      const { kind, options } = prepared
      say(kind === 'register' ? 'Suivez les instructions de votre navigateur pour créer la clé…' : 'Suivez les instructions de votre navigateur pour utiliser votre clé…')
      const cred = kind === 'register'
        ? await navigator.credentials.create({ publicKey: creationOptions(options.publicKey) })
        : await navigator.credentials.get({ publicKey: requestOptions(options.publicKey) })
      const done = await post('/v1/passkeys/finish', { ticket, credential: encode(cred) })
      if (kind === 'register') say('Clé ajoutée. Vous pouvez fermer cette page.', 'ok')
      else {
        // The app asks for this code: whoever sent this link would need it too.
        warn.hidden = true
        codeLine.textContent = done.code.slice(0, 3) + ' ' + done.code.slice(3)
        codeLine.hidden = false
        say('Tapez ce code dans l’application Quarel pour terminer la connexion. Ne le donnez à personne : si quelqu’un vous le demande, c’est une tentative de piratage.', 'ok')
      }
      document.body.dataset.done = kind
    } catch (e) {
      const code = e && (e.name in errors ? e.name : e.message)
      say(errors[code] || 'Échec : ' + code, 'err')
      if (e && e.name === 'NotAllowedError') go.hidden = false
    }
  }

  const start = async () => {
    if (!ticket) return say('Lien incomplet : ouvrez-le depuis l’application Quarel.', 'err')
    if (!window.PublicKeyCredential) return say('Ce navigateur ne gère pas les clés d’accès.', 'err')
    try {
      prepared = await post('/v1/passkeys/options', { ticket })
    } catch (e) {
      return say(errors[e.message] || 'Échec : ' + e.message, 'err')
    }
    if (prepared.kind === 'register') what.textContent = 'Ajouter une clé d’accès au compte ' + prepared.name + '.'
    else {
      const at = new Date(prepared.requested_at)
      what.textContent = 'Connexion au compte ' + prepared.name + ' depuis l’appareil « ' + prepared.device + ' » (adresse ' + prepared.ip +
        ', à ' + at.toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' }) + ') : utilisez votre clé d’accès.'
      warn.hidden = false
    }
    say('Prêt.')
    go.hidden = false
    go.onclick = run
    go.focus()
  }
  start()
})()
