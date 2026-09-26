// Quarel server administration. Plain JavaScript, no build step: rebuilding
// the servers only needs Go.
'use strict'

const app = document.getElementById('app')
let session = null
let page = 'dashboard'
let timer = null

// --- helpers ---

function h(tag, attrs, ...children) {
  const el = document.createElement(tag)
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue
    if (k.startsWith('on')) el.addEventListener(k.slice(2), v)
    else if (k === 'class') el.className = v
    else if (k === 'value') el.value = v
    else if (k === 'checked') el.checked = !!v
    else el.setAttribute(k, v === true ? '' : v)
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue
    el.append(c instanceof Node ? c : document.createTextNode(String(c)))
  }
  return el
}

class ApiError extends Error {
  constructor(status, code, message, retryAfter) {
    super(message)
    this.status = status
    this.code = code
    this.retryAfter = retryAfter
  }
}

async function api(method, path, body, raw) {
  const opts = { method, headers: { 'X-Quarel-Admin': '1' } }
  if (body !== undefined) {
    if (raw) opts.body = body
    else {
      opts.headers['Content-Type'] = 'application/json'
      opts.body = JSON.stringify(body)
    }
  }
  let res
  try {
    res = await fetch('/api' + path, opts)
  } catch {
    throw new ApiError(0, 'network', 'Le serveur ne répond pas.')
  }
  const text = await res.text()
  let data
  try { data = text ? JSON.parse(text) : undefined } catch { data = undefined }
  if (!res.ok) {
    const e = (data && data.error) || {}
    if (res.status === 401 && e.code === 'unauthorized') {
      session.signed_in = false
      render()
    }
    throw new ApiError(res.status, e.code || 'http_' + res.status, e.message || res.statusText, e.retry_after)
  }
  return data
}

const messages = {
  network: 'Le serveur ne répond pas.',
  invalid_credentials: 'Mot de passe incorrect.',
  invalid_setup_code: 'Code incorrect. Il est affiché dans le journal du serveur.',
  weak_password: 'Le mot de passe doit faire au moins 10 caractères.',
  rate_limited: 'Trop d’essais. Réessayez dans un quart d’heure.',
  already_set: 'Le mot de passe a déjà été choisi : connectez-vous.',
}

function errText(e) {
  if (e.code === 'invalid_settings') return 'Réglages refusés, rien n’a été changé :\n' + e.message
  if (e.code === 'restore_failed') return 'Restauration impossible : ' + e.message
  return messages[e.code] || e.message || 'Erreur inattendue.'
}

function alertBox(kind, text) {
  return text ? h('div', { class: 'alert alert-' + kind, role: kind === 'error' ? 'alert' : 'status' }, text) : null
}

function brand(small) {
  return h('div', { class: 'brand' }, h('span', { class: 'mark' }, 'Q'),
    h('div', {}, h('b', {}, session.product), small ? h('div', { class: 'muted small' }, 'Administration') : null))
}

function field(label, input, help) {
  const id = 'f' + Math.random().toString(36).slice(2)
  input.id = id
  return h('div', { class: 'field' }, h('label', { for: id }, label), input, help ? h('span', { class: 'help' }, help) : null)
}

function copyRow(text) {
  const input = h('input', { class: 'input mono', readonly: true, value: text, onfocus: (e) => e.target.select() })
  const btn = h('button', { class: 'btn btn-primary btn-sm', type: 'button', onclick: async () => {
    try {
      await navigator.clipboard.writeText(text)
      btn.textContent = 'Copié'
    } catch {
      input.select() // clipboard API needs HTTPS or localhost
      btn.textContent = 'Ctrl+C pour copier'
    }
  } }, 'Copier')
  return h('div', { class: 'toolbar' }, input, btn)
}

function fmtDate(iso) {
  return iso ? new Date(iso).toLocaleString('fr-FR', { dateStyle: 'medium', timeStyle: 'short' }) : '—'
}

// --- screens ---

async function start() {
  try {
    session = await api('GET', '/session')
  } catch (e) {
    app.replaceChildren(h('div', { class: 'center' }, alertBox('error', errText(e))))
    return
  }
  document.title = 'Administration · ' + session.product
  render()
}

function render() {
  clearInterval(timer)
  if (session.setup) return renderSetup()
  if (!session.signed_in) return renderLogin()
  renderApp()
}

function renderSetup() {
  const pw = h('input', { class: 'input', type: 'password', autocomplete: 'new-password' })
  const pw2 = h('input', { class: 'input', type: 'password', autocomplete: 'new-password' })
  const code = h('input', { class: 'input mono', autocomplete: 'off' })
  const err = h('div')
  const btn = h('button', { class: 'btn btn-primary', type: 'submit' }, 'Enregistrer')
  const form = h('form', { class: 'card', onsubmit: async (ev) => {
    ev.preventDefault()
    err.replaceChildren()
    if (pw.value !== pw2.value) return err.append(alertBox('error', 'Les deux mots de passe sont différents.'))
    btn.disabled = true
    try {
      await api('POST', '/setup', { password: pw.value, code: code.value })
      session.setup = false
      session.signed_in = true
      render()
    } catch (e) {
      err.append(alertBox('error', errText(e)))
      btn.disabled = false
    }
  } },
    brand(true),
    h('h2', {}, 'Bienvenue'),
    h('p', { class: 'muted' }, 'Choisissez le mot de passe qui protégera cette page d’administration.'),
    session.code_needed ? field('Code d’installation', code,
      'Par sécurité, depuis un autre appareil il faut le code affiché dans le journal du serveur (avec Docker : « docker logs <conteneur> »).') : null,
    field('Mot de passe', pw, 'Au moins 10 caractères.'),
    field('Confirmer le mot de passe', pw2),
    err, btn)
  app.replaceChildren(h('div', { class: 'center' }, form))
  ;(session.code_needed ? code : pw).focus()
}

function renderLogin() {
  const pw = h('input', { class: 'input', type: 'password', autocomplete: 'current-password' })
  const err = h('div')
  const btn = h('button', { class: 'btn btn-primary', type: 'submit' }, 'Se connecter')
  const form = h('form', { class: 'card', onsubmit: async (ev) => {
    ev.preventDefault()
    btn.disabled = true
    err.replaceChildren()
    try {
      await api('POST', '/login', { password: pw.value })
      session.signed_in = true
      render()
    } catch (e) {
      err.append(alertBox('error', errText(e)))
      btn.disabled = false
      pw.select()
    }
  } }, brand(true), field('Mot de passe administrateur', pw), err, btn,
    h('p', { class: 'muted small' }, 'Mot de passe oublié : arrêtez le serveur, supprimez le fichier admin.json de son dossier de données, puis relancez-le.'))
  app.replaceChildren(h('div', { class: 'center' }, form))
  pw.focus()
}

const pages = () => [
  ['dashboard', 'Tableau de bord'],
  ['settings', 'Réglages'],
  ...(session.kind === 'identity' ? [['accounts', 'Comptes'], ['invites', 'Invitations'], ['servers', 'Serveurs']] : []),
  ['backups', 'Sauvegardes'],
  ['logs', 'Journal'],
  ['password', 'Mot de passe'],
]

function renderApp() {
  const main = h('main')
  const nav = h('nav', { class: 'side', 'aria-label': 'Sections' }, brand(false),
    pages().map(([id, label]) => h('button', { class: page === id ? 'active' : '', 'aria-current': page === id ? 'page' : null,
      onclick: () => { page = id; render() } }, label)),
    h('span', { class: 'spacer' }),
    h('button', { onclick: async () => { await api('POST', '/logout').catch(() => {}); session.signed_in = false; render() } }, 'Se déconnecter'))
  app.replaceChildren(h('div', { class: 'layout' }, nav, main))
  ;({ dashboard: pageDashboard, settings: pageSettings, accounts: pageAccounts, invites: pageInvites, servers: pageServers,
    backups: pageBackups, logs: pageLogs, password: pagePassword })[page](main)
}

const stateLabels = { running: 'En marche', starting: 'Démarrage…', error: 'Arrêté : erreur de configuration', stopped: 'Arrêté' }

async function pageDashboard(main) {
  const body = h('div', { class: 'stack' })
  main.append(h('h2', {}, 'Tableau de bord'), body)
  const load = async () => {
    let st
    try {
      st = await api('GET', '/status')
    } catch (e) {
      body.replaceChildren(alertBox('error', errText(e)))
      return
    }
    const svc = st.service || {}
    const rows = [
      ['État', h('span', { class: 'badge ' + st.state }, stateLabels[st.state] || st.state)],
      ['Depuis', fmtDate(st.since)],
      ...(svc.items || []).map((it) => [it.label, it.mono ? h('span', { class: 'mono' }, it.value) : it.value]),
      ['Version', h('span', { class: 'mono' }, st.version)],
      ['Dossier de données', h('span', { class: 'mono' }, st.data_dir)],
    ]
    body.replaceChildren(...[
      st.error ? h('div', { class: 'alert alert-error' }, 'Le service ne peut pas démarrer :\n' + st.error + '\n\nCorrigez les réglages : il redémarrera tout seul.') : null,
      svc.claim_code ? h('div', { class: 'panel' }, h('h3', {}, 'Devenir propriétaire'),
        h('div', { class: 'form-grid' },
          h('p', { class: 'muted' }, 'Ce serveur n\u2019a pas encore de propriétaire. Dans l\u2019application Quarel, choisissez « Rejoindre un serveur » et collez ce lien : vous en deviendrez propriétaire. Il ne sert qu\u2019une fois ; un nouveau est créé à chaque démarrage tant que personne ne l\u2019a utilisé.'),
          copyRow(svc.owner_link),
          svc.owner_web_link ? h('p', { class: 'muted small' }, 'Même lien pour la version web (navigateur) :') : null,
          svc.owner_web_link ? copyRow(svc.owner_web_link) : null,
          h('p', { class: 'muted small' }, 'Si l\u2019application se connecte depuis Internet, remplacez l\u2019adresse du lien par l\u2019adresse publique ou le nom de domaine du serveur.'))) : null,
      ...(svc.notices || []).map((n) => alertBox(n.level, n.text)),
      h('div', { class: 'panel' }, h('h3', {}, 'Service'), rows.map(([k, v]) => h('div', { class: 'row' }, h('div', { class: 'k' }, k), h('div', { class: 'v' }, v)))),
      h('div', { class: 'actions' },
        session.kind === 'community' && st.state === 'running' ? h('button', { class: 'btn btn-ghost btn-sm', onclick: async () => {
          const name = prompt('Nouveau nom du serveur :', (svc.items || []).find((i) => i.label === 'Nom')?.value || '')
          if (!name || !name.trim()) return
          try {
            await api('POST', '/x/rename', { name: name.trim() })
            load()
          } catch (e) {
            alert(errText(e))
          }
        } }, 'Renommer le serveur') : null,
        session.kind === 'community' && st.state === 'running' && svc.has_owner ? h('button', { class: 'btn btn-ghost btn-sm', onclick: async () => {
          if (!confirm('Retirer le propriétaire actuel du serveur ?\n\nÀ faire seulement s\u2019il ne peut plus se connecter (compte perdu ou supprimé). Il reste membre ; un nouveau lien propriétaire s\u2019affichera ici.')) return
          try {
            await api('POST', '/x/reset-owner')
            load()
          } catch (e) {
            alert(errText(e))
          }
        } }, 'Réinitialiser le propriétaire') : null,
        h('button', { class: 'btn btn-ghost btn-sm', onclick: async (ev) => {
          ev.target.disabled = true
          await api('POST', '/restart').catch(() => {})
          setTimeout(load, 1500)
        } }, 'Redémarrer le service')),
    ].filter(Boolean))
  }
  await load()
  timer = setInterval(load, 5000)
  if (session.kind === 'community') main.append(await identityPanel())
}

// Community server: where it stands with each accepted identity service.
async function identityPanel() {
  const box = h('div', { class: 'form-grid' }, h('p', { class: 'muted' }, 'Vérification…'))
  const panel = h('div', { class: 'panel' }, h('h3', {}, 'Services d\u2019identité acceptés'), box)
  const draw = async () => {
    let list
    try {
      list = await api('GET', '/x/identity')
    } catch (e) {
      box.replaceChildren(alertBox('error', errText(e)))
      return
    }
    box.replaceChildren(...list.map((it) => {
      let state
      if (!it.reachable) state = h('span', { class: 'tag red' }, 'injoignable')
      else if (it.blocked) state = h('span', { class: 'tag red' }, 'a bloqué ce serveur')
      else if (it.server_policy !== 'approved') state = h('span', { class: 'tag' }, 'accepte tous les serveurs')
      else state = h('span', { class: 'tag' + (it.status === 'approved' ? '' : ' red') },
        { approved: 'serveur approuvé', pending: 'demande en attente', rejected: 'demande refusée', none: 'approbation nécessaire' }[it.status] || it.status)
      const ask = it.reachable && !it.blocked && it.server_policy === 'approved' && it.status !== 'approved' && it.status !== 'pending'
        ? h('button', { class: 'btn btn-primary btn-sm', onclick: async () => {
          const contact = prompt('Comment l\u2019opérateur de ' + it.issuer + ' peut-il vous joindre ? (email, pseudo…)')
          if (contact === null) return
          try {
            await api('POST', '/x/identity/request', { issuer: it.issuer, contact })
            draw()
          } catch (e) {
            alert(errText(e))
          }
        } }, 'Demander l\u2019accès') : null
      const help = it.blocked ? 'Ses utilisateurs ne peuvent plus rejoindre ce serveur.'
        : it.server_policy === 'approved' && it.status !== 'approved' ? 'Ses utilisateurs ne pourront se connecter qu\u2019une fois le serveur approuvé par son opérateur.' : ''
      return h('div', { class: 'row' }, h('div', { class: 'k mono' }, it.issuer), h('div', { class: 'v' }, state, ' ', ask, help ? h('div', { class: 'muted small' }, help) : null))
    }))
  }
  draw()
  return panel
}

// --- identity service: invitations ---

async function pageInvites(main) {
  const err = h('div')
  const list = h('div')
  const note = h('input', { class: 'input', placeholder: 'Pour qui ? (facultatif)' })
  const uses = h('input', { class: 'input', type: 'number', min: '0', value: '1' })
  const days = h('input', { class: 'input', type: 'number', min: '0', value: '30' })
  const created = h('div')
  async function load() {
    try {
      const invites = await api('GET', '/x/invites')
      const now = Date.now()
      list.replaceChildren(invites.length === 0 ? h('p', { class: 'muted empty' }, 'Aucune invitation.') :
        h('table', {}, h('thead', {}, h('tr', {}, ['Code', 'Créée par', 'Utilisations', 'Expire', ''].map((t) => h('th', {}, t)))),
          h('tbody', {}, invites.map((i) => {
            const expired = i.expires_at && Date.parse(i.expires_at) <= now
            const full = i.max_uses > 0 && i.uses >= i.max_uses
            return h('tr', {},
              h('td', {}, h('span', { class: 'mono' }, i.code), i.note ? h('div', { class: 'muted small' }, i.note) : null),
              h('td', {}, i.created_by || 'opérateur'),
              h('td', {}, i.uses + ' / ' + (i.max_uses || '∞')),
              h('td', {}, expired ? h('span', { class: 'tag red' }, 'expirée') : full ? h('span', { class: 'tag red' }, 'épuisée') : i.expires_at ? fmtDate(i.expires_at) : 'jamais'),
              h('td', {}, !expired && !full ? h('button', { class: 'btn btn-ghost btn-sm', onclick: async () => {
                await api('POST', '/x/invites/' + encodeURIComponent(i.code) + '/revoke').catch((e) => err.replaceChildren(alertBox('error', errText(e))))
                load()
              } }, 'Révoquer') : null))
          }))))
    } catch (e) {
      list.replaceChildren(alertBox('error', errText(e)))
    }
  }
  main.append(h('h2', {}, 'Invitations'),
    h('p', { class: 'muted' }, 'Avec les inscriptions « sur invitation » (Réglages), un code est demandé pour créer un compte. Donnez-le avec l\u2019adresse de ce service.'),
    h('form', { class: 'panel', onsubmit: async (ev) => {
      ev.preventDefault()
      err.replaceChildren()
      try {
        const inv = await api('POST', '/x/invites', { note: note.value, max_uses: Number(uses.value), valid_days: Number(days.value) })
        created.replaceChildren(h('div', { class: 'form-grid' }, h('p', {}, 'Nouvelle invitation :'), copyRow(inv.code)))
        note.value = ''
        load()
      } catch (e) {
        err.append(alertBox('error', errText(e)))
      }
    } }, h('h3', {}, 'Nouvelle invitation'), h('div', { class: 'form-grid' },
      field('Note', note), field('Nombre d\u2019utilisations', uses, '0 : illimité.'), field('Valable (jours)', days, '0 : sans limite de durée.'),
      err, h('div', { class: 'actions' }, h('button', { class: 'btn btn-primary', type: 'submit' }, 'Créer')))),
    created,
    h('div', { class: 'panel' }, h('h3', {}, 'Toutes les invitations'), list))
  await load()
}

// --- identity service: community servers (block list, approvals) ---

async function pageServers(main) {
  const err = h('div')
  const list = h('div')
  const policy = h('p', { class: 'muted' })
  const id = h('input', { class: 'input mono', placeholder: 'identifiant (26 caractères, après « sid= » dans ses liens)' })
  const reason = h('input', { class: 'input', placeholder: 'Raison (affichée aux utilisateurs)' })
  const act = (path, body) => async () => {
    err.replaceChildren()
    try {
      await api('POST', path, body)
      load()
    } catch (e) {
      err.append(alertBox('error', errText(e)))
    }
  }
  async function load() {
    try {
      const data = await api('GET', '/x/servers')
      policy.textContent = data.policy === 'approved'
        ? 'Mode actuel : seuls les serveurs approuvés peuvent utiliser les comptes de ce service (jetons chiffrés pour eux).'
        : 'Mode actuel : tous les serveurs sauf ceux bloqués ci-dessous (l\u2019application refuse de s\u2019y connecter). Le mode « approuvés seulement » se choisit dans les Réglages.'
      list.replaceChildren(data.servers.length === 0 ? h('p', { class: 'muted empty' }, 'Aucun serveur bloqué ni demande d\u2019approbation.') :
        h('table', {}, h('thead', {}, h('tr', {}, ['Serveur', 'Contact', 'État', ''].map((t) => h('th', {}, t)))),
          h('tbody', {}, data.servers.map((sv) => h('tr', {},
            h('td', {}, h('b', {}, sv.name || '—'), h('div', { class: 'muted small mono' }, sv.id), sv.url ? h('div', { class: 'muted small mono' }, sv.url) : null),
            h('td', {}, sv.contact || '—', sv.requested_at ? h('div', { class: 'muted small' }, 'demande du ' + fmtDate(sv.requested_at)) : null),
            h('td', {}, sv.blocked ? h('span', { class: 'tag red', title: sv.reason }, 'bloqué') : null, ' ',
              sv.status ? h('span', { class: 'tag' + (sv.status === 'approved' ? '' : ' red') }, { approved: 'approuvé', pending: 'en attente', rejected: 'refusé' }[sv.status]) : null),
            h('td', {}, h('div', { class: 'toolbar' },
              sv.status && sv.status !== 'approved' ? h('button', { class: 'btn btn-primary btn-sm', onclick: act('/x/servers/' + sv.id + '/approve') }, 'Approuver') : null,
              sv.status === 'pending' || sv.status === 'approved' ? h('button', { class: 'btn btn-ghost btn-sm', onclick: act('/x/servers/' + sv.id + '/reject') }, sv.status === 'approved' ? 'Retirer l\u2019approbation' : 'Refuser') : null,
              sv.blocked ? h('button', { class: 'btn btn-ghost btn-sm', onclick: act('/x/servers/' + sv.id + '/unblock') }, 'Débloquer')
                : h('button', { class: 'btn btn-ghost btn-sm', onclick: () => {
                  const why = prompt('Bloquer ' + (sv.name || sv.id) + ' : raison (affichée aux utilisateurs)')
                  if (why !== null) act('/x/servers/block', { id: sv.id, reason: why.trim() })()
                } }, 'Bloquer'))))))))
    } catch (e) {
      list.replaceChildren(alertBox('error', errText(e)))
    }
  }
  main.append(h('h2', {}, 'Serveurs communautaires'), policy,
    h('form', { class: 'panel', onsubmit: (ev) => { ev.preventDefault(); act('/x/servers/block', { id: id.value.trim(), reason: reason.value.trim() })().then(() => { id.value = ''; reason.value = '' }) } },
      h('h3', {}, 'Bloquer un serveur'), h('div', { class: 'form-grid' },
        h('p', { class: 'muted' }, 'Ses utilisateurs ne pourront plus le rejoindre avec un compte de ce service.'), field('Identifiant du serveur', id), field('Raison', reason),
        h('div', { class: 'actions' }, h('button', { class: 'btn btn-danger btn-sm', type: 'submit' }, 'Bloquer')))),
    err, h('div', { class: 'panel' }, h('h3', {}, 'Serveurs connus'), list))
  await load()
}

function shown(f, values) {
  if (!f.show_if) return true
  const [key, want] = f.show_if.split('=')
  return want.split('|').includes(values[key] ?? '')
}

async function pageSettings(main) {
  main.append(h('h2', {}, 'Réglages'))
  let data
  try {
    data = await api('GET', '/settings')
  } catch (e) {
    return main.append(alertBox('error', errText(e)))
  }
  const values = {} // current form values
  const changed = {} // key -> string | null
  const inputs = {}
  for (const f of data.fields) values[f.key] = f.value || f.default || ''

  const wrap = h('div', { class: 'stack' })
  const err = h('div')
  const drawn = []
  const groups = [...new Set(data.fields.map((f) => f.group))]
  for (const g of groups) {
    const grid = h('div', { class: 'form-grid' })
    for (const f of data.fields.filter((x) => x.group === g)) {
      let input
      const set = (v) => { values[f.key] = v; changed[f.key] = v; refresh() }
      if (f.kind === 'bool') {
        input = h('input', { type: 'checkbox', checked: values[f.key] === 'on', disabled: f.locked, onchange: (e) => set(e.target.checked ? 'on' : 'off') })
      } else if (f.kind === 'select') {
        input = h('select', { class: 'input', disabled: f.locked, onchange: (e) => set(e.target.value) },
          f.options.map((o) => h('option', { value: o.value, selected: values[f.key] === o.value ? true : null }, o.label)))
        input.value = values[f.key]
      } else {
        input = h('input', { class: 'input' + (f.kind === 'list' || f.kind === 'secret' ? ' mono' : ''), type: f.kind === 'secret' ? 'password' : f.kind === 'number' ? 'number' : 'text',
          value: f.kind === 'secret' ? '' : values[f.key], disabled: f.locked, autocomplete: 'off',
          placeholder: f.kind === 'secret' && f.set ? '•••••••• (inchangé)' : (f.placeholder || f.default || ''),
          oninput: (e) => { values[f.key] = e.target.value; changed[f.key] = e.target.value; refresh() } })
      }
      inputs[f.key] = input
      const head = h('div', { class: 'field-head' }, h('label', { for: 'k-' + f.key }, f.label),
        f.locked ? h('span', { class: 'locked' }, 'Fixé par la variable ' + f.key)
          : h('button', { type: 'button', class: 'link small', title: 'Revenir à la valeur par défaut', onclick: () => {
            changed[f.key] = null
            values[f.key] = f.default || ''
            if (f.kind === 'bool') input.checked = values[f.key] === 'on'
            else input.value = f.kind === 'secret' ? '' : values[f.key]
            refresh()
          } }, 'Par défaut'))
      input.id = 'k-' + f.key
      const box = f.kind === 'bool'
        ? h('div', { class: 'field' }, h('label', { class: 'check', for: 'k-' + f.key }, input, f.label),
          f.locked ? h('span', { class: 'locked' }, 'Fixé par la variable ' + f.key) : null, f.help ? h('span', { class: 'help' }, f.help) : null)
        : h('div', { class: 'field' }, head, input, f.help ? h('span', { class: 'help' }, f.help) : null)
      drawn.push([f, box])
      grid.append(box)
    }
    wrap.append(h('div', { class: 'panel' }, h('h3', {}, g), grid))
  }
  function refresh() {
    for (const [f, box] of drawn) box.hidden = !shown(f, values)
  }
  refresh()
  const save = h('button', { class: 'btn btn-primary' }, 'Enregistrer et redémarrer')
  save.addEventListener('click', async () => {
    err.replaceChildren()
    if (!Object.keys(changed).length) return err.append(alertBox('info', 'Aucun changement.'))
    save.disabled = true
    try {
      await api('PUT', '/settings', { values: changed })
      err.append(alertBox('info', 'Réglages enregistrés : le service redémarre.'))
      for (const k of Object.keys(changed)) delete changed[k]
    } catch (e) {
      err.append(alertBox('error', errText(e)))
    } finally {
      save.disabled = false
    }
  })
  main.append(h('p', { class: 'muted small' }, 'Les réglages sont enregistrés dans le fichier settings.json du dossier de données. Une variable d’environnement (Docker Compose, par exemple) a toujours la priorité : le réglage correspondant est alors verrouillé ici.'),
    wrap, err, h('div', { class: 'actions' }, save))
}

async function pageBackups(main) {
  const err = h('div')
  const file = h('input', { type: 'file', accept: '.tar.gz,.gz,application/gzip' })
  const restore = h('button', { class: 'btn btn-danger btn-sm' }, 'Restaurer cette sauvegarde')
  restore.addEventListener('click', async () => {
    err.replaceChildren()
    const f = file.files[0]
    if (!f) return err.append(alertBox('error', 'Choisissez d’abord un fichier de sauvegarde.'))
    if (!confirm('Remplacer les données actuelles par cette sauvegarde ? Le service sera arrêté pendant l’opération. Les données actuelles seront mises de côté (dossier before-restore-…), pas effacées.')) return
    restore.disabled = true
    try {
      const r = await api('POST', '/restore', f, true)
      err.append(alertBox('info', 'Sauvegarde du ' + fmtDate(r.created_at) + ' restaurée' + (r.identity ? ' (' + r.identity + ')' : '') + '. Anciennes données : ' + (r.previous_data || 'aucune') + '.'))
    } catch (e) {
      err.append(alertBox('error', errText(e)))
    } finally {
      restore.disabled = false
    }
  })
  main.append(h('h2', {}, 'Sauvegardes'),
    h('div', { class: 'panel' }, h('h3', {}, 'Sauvegarder'), h('div', { class: 'form-grid' },
      h('p', { class: 'muted' }, 'Une archive de tout ce qu’il faut pour remettre le serveur en état : base de données (copie cohérente, sans arrêter le service), clés, fichiers, réglages. Gardez-la en lieu sûr : elle contient les clés du serveur.'),
      h('div', {}, h('a', { class: 'btn btn-primary', href: '/api/backup', download: '' }, 'Télécharger une sauvegarde')))),
    h('div', { class: 'panel' }, h('h3', {}, 'Restaurer'), h('div', { class: 'form-grid' },
      h('p', { class: 'muted' }, 'Remplace les données par celles d’une sauvegarde. Le mot de passe de cette page ne change pas.'),
      file, h('div', {}, restore), err)))
}

async function pageLogs(main) {
  const pre = h('pre', { class: 'log', tabindex: '0', 'aria-label': 'Journal' })
  const filter = h('input', { class: 'input', placeholder: 'Filtrer…', oninput: () => draw() })
  let lines = []
  let follow = true
  pre.addEventListener('scroll', () => { follow = pre.scrollHeight - pre.scrollTop - pre.clientHeight < 30 })
  function draw() {
    const q = filter.value.toLowerCase()
    pre.replaceChildren(...lines.filter((l) => !q || l.toLowerCase().includes(q)).map((l) =>
      h('div', { class: /level=(WARN)/.test(l) ? 'warn' : /level=ERROR/.test(l) ? 'err' : '' }, l)))
    if (follow) pre.scrollTop = pre.scrollHeight
  }
  const load = async () => {
    try {
      lines = (await api('GET', '/logs')).lines
      draw()
    } catch { /* shown on next attempt */ }
  }
  main.append(h('h2', {}, 'Journal'), h('div', { class: 'toolbar' }, filter, h('span', { class: 'muted small' }, 'Mis à jour toutes les 3 secondes.')), pre)
  await load()
  timer = setInterval(load, 3000)
}

function pagePassword(main) {
  const cur = h('input', { class: 'input', type: 'password', autocomplete: 'current-password' })
  const pw = h('input', { class: 'input', type: 'password', autocomplete: 'new-password' })
  const pw2 = h('input', { class: 'input', type: 'password', autocomplete: 'new-password' })
  const err = h('div')
  main.append(h('h2', {}, 'Mot de passe'), h('form', { class: 'panel', onsubmit: async (ev) => {
    ev.preventDefault()
    err.replaceChildren()
    if (pw.value !== pw2.value) return err.append(alertBox('error', 'Les deux mots de passe sont différents.'))
    try {
      await api('POST', '/password', { current: cur.value, new: pw.value })
      err.append(alertBox('info', 'Mot de passe changé. Les autres sessions ont été fermées.'))
      cur.value = pw.value = pw2.value = ''
    } catch (e) {
      err.append(alertBox('error', errText(e)))
    }
  } }, h('h3', {}, 'Changer le mot de passe administrateur'), h('div', { class: 'form-grid' },
    field('Mot de passe actuel', cur), field('Nouveau mot de passe', pw, 'Au moins 10 caractères.'), field('Confirmer', pw2), err,
    h('div', { class: 'actions' }, h('button', { class: 'btn btn-primary', type: 'submit' }, 'Changer')))))
}

// --- identity service: accounts (legal requests), operator log, signing key ---

async function pageAccounts(main) {
  const q = h('input', { class: 'input', placeholder: 'Pseudo, email ou identifiant', 'aria-label': 'Rechercher un compte' })
  const results = h('div')
  const log = h('div')
  const err = h('div')
  async function search() {
    err.replaceChildren()
    try {
      const list = await api('GET', '/x/accounts?q=' + encodeURIComponent(q.value.trim()))
      results.replaceChildren(list.length === 0 ? h('p', { class: 'muted empty' }, 'Aucun compte trouvé.') :
        h('table', {}, h('thead', {}, h('tr', {}, ['Compte', 'Email', 'Créé le', 'État', ''].map((t) => h('th', {}, t)))),
          h('tbody', {}, list.map((a) => h('tr', {},
            h('td', {}, h('b', {}, a.pseudo), h('div', { class: 'muted small mono' }, a.id)),
            h('td', {}, a.email),
            h('td', {}, fmtDate(a.created_at)),
            h('td', {}, a.disabled_at ? h('span', { class: 'tag red', title: a.disabled_reason }, 'Désactivé') : h('span', { class: 'tag' }, 'Actif')),
            h('td', {}, h('button', { class: 'btn btn-ghost btn-sm', onclick: () => toggle(a) }, a.disabled_at ? 'Réactiver' : 'Désactiver')))))))
    } catch (e) {
      err.append(alertBox('error', errText(e)))
    }
  }
  async function toggle(a) {
    const action = a.disabled_at ? 'enable' : 'disable'
    const reason = prompt((a.disabled_at ? 'Réactiver ' : 'Désactiver ') + a.pseudo + ' : raison (obligatoire, consignée au journal), par exemple la référence de la réquisition.')
    if (!reason || !reason.trim()) return
    try {
      await api('POST', '/x/accounts/' + encodeURIComponent(a.id) + '/' + action, { reason: reason.trim() })
      await search()
      await loadLog()
    } catch (e) {
      err.append(alertBox('error', errText(e)))
    }
  }
  async function loadLog() {
    try {
      const entries = await api('GET', '/x/log')
      log.replaceChildren(entries.length === 0 ? h('p', { class: 'muted empty' }, 'Aucune action.') :
        h('table', {}, h('thead', {}, h('tr', {}, ['Date', 'Action', 'Compte', 'Par', 'Raison'].map((t) => h('th', {}, t)))),
          h('tbody', {}, entries.map((e) => h('tr', {}, h('td', {}, fmtDate(e.at)), h('td', {}, e.action), h('td', { class: 'mono' }, e.target || '—'), h('td', {}, e.operator), h('td', {}, e.reason))))))
    } catch (e) {
      log.replaceChildren(alertBox('error', errText(e)))
    }
  }
  const rotate = h('button', { class: 'btn btn-ghost btn-sm', onclick: async () => {
    if (!confirm('Changer la clé de signature ? L’ancienne clé privée est détruite ; les jetons déjà émis restent valables jusqu’à leur expiration. Le service redémarre.')) return
    try {
      await api('POST', '/x/rotate-key')
      err.append(alertBox('info', 'Nouvelle clé de signature créée ; le service redémarre.'))
      await loadLog()
    } catch (e) {
      err.append(alertBox('error', errText(e)))
    }
  } }, 'Changer la clé de signature')
  main.append(h('h2', {}, 'Comptes'),
    h('p', { class: 'muted' }, 'Désactiver un compte (réquisition judiciaire) le bloque partout, sans rien effacer : la réactivation le remet à l’identique. Chaque action est consignée avec sa raison.'),
    h('form', { class: 'toolbar', onsubmit: (ev) => { ev.preventDefault(); search() } }, q, h('button', { class: 'btn btn-primary btn-sm', type: 'submit' }, 'Rechercher')),
    err, h('div', { class: 'panel' }, h('h3', {}, 'Résultats'), results),
    h('div', { class: 'panel' }, h('h3', {}, 'Journal de l’opérateur'), log),
    h('div', { class: 'panel' }, h('h3', {}, 'Clé de signature'), h('div', { class: 'form-grid' },
      h('p', { class: 'muted' }, 'À changer si vous pensez que la clé privée du service a pu être copiée.'), h('div', {}, rotate))))
  await Promise.all([search(), loadLog()])
}

start()
