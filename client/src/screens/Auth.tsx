// Signed-out screens: sign in (with 2FA), create an account, verify the email
// address, reset a forgotten password, choose the identity service.
import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { ApiError } from '../api/http'
import { IdentityClient, type Policy } from '../api/identity'
import { Alert, Dialog, Field, PasswordField, Submit, useCooldown } from '../components/ui'
import { Home, Lock, Phone } from '../components/icons'
import { deviceKey } from '../lib/device'
import { errorMessage, isCode } from '../lib/errors'
import { DEFAULT_IDENTITY, identityBaseURL, identityLabel } from '../lib/identityURL'
import { appInfo, prefs } from '../platform'
import { signIn } from '../state/account'
import { usePendingInvite } from '../state/invite'
import { parseInvite } from '../lib/invite'

type View = 'login' | 'mfa' | 'register' | 'verify' | 'forgot' | 'reset'

const pseudoRe = /^[A-Za-z0-9_][A-Za-z0-9_.-]{1,30}[A-Za-z0-9_]$/
const MIN_PASSWORD = 10

export function Auth({ notice }: { notice?: string }) {
  const [identity, setIdentity] = useState(() => prefs.get('identity', 'https://' + DEFAULT_IDENTITY))
  const [view, setView] = useState<View>('login')
  const [info, setInfo] = useState(notice ?? '')
  const [picking, setPicking] = useState(false)
  // Kept in memory between steps (2FA, email verification) to finish signing in.
  const [creds, setCreds] = useState({ login: prefs.get('last-login', ''), password: '' })
  const [email, setEmail] = useState('')

  const invite = usePendingInvite()
  const inviteHost = invite ? parseInvite(invite).host : ''
  const client = new IdentityClient(identity)
  const [policy, setPolicy] = useState<Policy | null>(null)
  useEffect(() => {
    setPolicy(null)
    new IdentityClient(identity).policy().then(setPolicy, () => {})
  }, [identity])

  async function finishLogin(login: string, password: string, totp?: string) {
    const [dk, app, keys] = await Promise.all([deviceKey(identityLabel(identity)), appInfo(), client.keySet()])
    const res = await client.login({
      login,
      password,
      totp_code: totp,
      device_name: app.platform === 'web' ? 'Web (' + app.deviceName + ')' : app.deviceName,
      device_key: dk.publicKeyB64,
    })
    prefs.set('last-login', login)
    await signIn({ identity, issuer: keys.issuer, sessionId: res.session_id, token: res.session_token, user: res.user })
  }

  function go(v: View, message = '') {
    setInfo(message)
    setView(v)
  }

  let body: ReactNode
  switch (view) {
    case 'login':
      body = (
        <LoginForm
          initial={creds.login}
          info={info}
          onSubmit={async (login, password) => {
            setCreds({ login, password })
            try {
              await finishLogin(login, password)
            } catch (e) {
              if (isCode(e, 'mfa_required')) return go('mfa')
              if (isCode(e, 'email_not_verified')) {
                setEmail(login.includes('@') ? login : '')
                return go('verify', 'Votre adresse email n’est pas encore vérifiée. Entrez le code reçu à l’inscription, ou demandez-en un nouveau.')
              }
              throw e
            }
          }}
          onForgot={() => go('forgot')}
          onRegister={() => go('register')}
        />
      )
      break
    case 'mfa':
      body = (
        <MfaForm
          onSubmit={(code) => finishLogin(creds.login, creds.password, code)}
          onBack={() => go('login')}
        />
      )
      break
    case 'register':
      body = (
        <RegisterForm
          policy={policy}
          onSubmit={async (em, pseudo, password, invite) => {
            await client.register(em, pseudo, password, invite)
            setEmail(em)
            setCreds({ login: em, password })
            go('verify', 'Compte créé. Un code à 6 chiffres vient d’être envoyé à ' + em + '.')
          }}
          onLogin={() => go('login')}
        />
      )
      break
    case 'verify':
      body = (
        <VerifyForm
          email={email}
          info={info}
          client={client}
          onVerified={async (em) => {
            if (creds.password) {
              try {
                await finishLogin(creds.login || em, creds.password)
                return
              } catch (e) {
                if (isCode(e, 'mfa_required')) return go('mfa')
                return go('login', 'Adresse vérifiée, mais la connexion a échoué : ' + errorMessage(e))
              }
            }
            go('login', 'Adresse vérifiée. Vous pouvez vous connecter.')
          }}
          onBack={() => go('login')}
        />
      )
      break
    case 'forgot':
      body = (
        <ForgotForm
          onSubmit={async (em) => {
            await client.forgotPassword(em)
            setEmail(em)
            go('reset', 'Si un compte utilise ' + em + ', un code à 6 chiffres vient de lui être envoyé.')
          }}
          onBack={() => go('login')}
        />
      )
      break
    case 'reset':
      body = (
        <ResetForm
          email={email}
          info={info}
          client={client}
          onDone={() => go('login', 'Mot de passe changé. Par sécurité, toutes vos sessions ont été fermées : reconnectez-vous sur chaque appareil.')}
          onBack={() => go('forgot')}
        />
      )
      break
  }

  return (
    <div className="welcome">
      <WelcomeSide />
      <main className="welcome-main">
        <div className="auth-form">
          {invite && <Alert kind="info">Connectez-vous (ou créez un compte) pour rejoindre le serveur <b>{inviteHost}</b>.</Alert>}
          {body}
          {(view === 'login' || view === 'register') && (
            <div className="identity-box">
              <div>
                <div className="label">Service d&apos;identité</div>
                <div className="value" data-testid="identity-label">{identityLabel(identity)}</div>
              </div>
              <button type="button" className="btn btn-ghost btn-sm" onClick={() => setPicking(true)}>Changer</button>
            </div>
          )}
        </div>
      </main>
      {picking && (
        <IdentityPicker
          current={identity}
          onClose={() => setPicking(false)}
          onPick={(base) => {
            setIdentity(base)
            prefs.set('identity', base)
            setPicking(false)
          }}
        />
      )}
    </div>
  )
}

function WelcomeSide() {
  return (
    <aside className="welcome-side">
      <div className="brand">
        <span className="brand-mark">Q</span>
        <span className="brand-name">Quarel</span>
      </div>
      <div className="welcome-pitch">
        <h1>Vos serveurs, chez vous.</h1>
        <p>Un seul compte pour rejoindre les serveurs que vos amis hébergent. Vos messages privés sont chiffrés de bout en bout.</p>
        <div className="welcome-points">
          <div><Lock />Messages privés illisibles par le serveur</div>
          <div><Home />Chaque communauté est hébergée par ses membres</div>
          <div><Phone />Appels entre amis en direct, sans intermédiaire</div>
        </div>
      </div>
      <span className="small" style={{ color: 'var(--text-4)' }}>Logiciel libre · Apache-2.0</span>
    </aside>
  )
}

// Runs an async submit handler, showing its error in French.
function useSubmit() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function run(fn: () => Promise<unknown>) {
    setBusy(true)
    setError('')
    try {
      await fn()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return { busy, error, setError, run }
}

function Head({ title, sub }: { title: string; sub?: ReactNode }) {
  return (
    <div className="auth-head">
      <h2>{title}</h2>
      {sub && <p className="muted">{sub}</p>}
    </div>
  )
}

function LoginForm({ initial, info, onSubmit, onForgot, onRegister }: {
  initial: string
  info: string
  onSubmit: (login: string, password: string) => Promise<void>
  onForgot: () => void
  onRegister: () => void
}) {
  const [login, setLogin] = useState(initial)
  const [password, setPassword] = useState('')
  const s = useSubmit()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!login.trim() || !password) return s.setError('Remplissez les deux champs.')
    s.run(() => onSubmit(login.trim(), password))
  }
  return (
    <form className="auth-form" onSubmit={submit} noValidate>
      <Head title="Connexion" sub="Bon retour parmi nous." />
      <Alert kind="info">{info}</Alert>
      <Field label="Email ou pseudo" autoComplete="username" value={login} onChange={(e) => setLogin(e.target.value)} autoFocus={!initial} />
      <PasswordField label="Mot de passe" autoComplete="current-password" value={password}
        onChange={(e) => setPassword(e.target.value)} autoFocus={!!initial} />
      <button type="button" className="link small" style={{ alignSelf: 'flex-start' }} onClick={onForgot}>Mot de passe oublié ?</button>
      <Alert kind="error">{s.error}</Alert>
      <Submit busy={s.busy}>Se connecter</Submit>
      <p className="muted small">Pas encore de compte ? <button type="button" className="link" onClick={onRegister}>Créer un compte</button></p>
    </form>
  )
}

function MfaForm({ onSubmit, onBack }: { onSubmit: (code: string) => Promise<void>; onBack: () => void }) {
  const [code, setCode] = useState('')
  const [backup, setBackup] = useState(false)
  const s = useSubmit()
  return (
    <form className="auth-form" noValidate onSubmit={(e) => {
      e.preventDefault()
      if (code.trim()) s.run(() => onSubmit(code.trim()))
    }}>
      <Head title="Double authentification"
        sub={backup ? 'Entrez un de vos codes de secours. Chacun ne sert qu’une fois.' : 'Entrez le code à 6 chiffres affiché par votre application d’authentification.'} />
      {backup
        ? <Field key="b" label="Code de secours" autoComplete="off" value={code} onChange={(e) => setCode(e.target.value)} autoFocus />
        : <Field key="t" label="Code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} inputClass="code"
            value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} autoFocus />}
      <button type="button" className="link small" style={{ alignSelf: 'flex-start' }}
        onClick={() => { setBackup(!backup); setCode('') }}>
        {backup ? 'Utiliser l’application d’authentification' : 'Utiliser un code de secours'}
      </button>
      <Alert kind="error">{s.error}</Alert>
      <Submit busy={s.busy}>Valider</Submit>
      <button type="button" className="link small" style={{ alignSelf: 'flex-start' }} onClick={onBack}>Retour</button>
    </form>
  )
}

function RegisterForm({ policy, onSubmit, onLogin }: {
  policy: Policy | null
  onSubmit: (email: string, pseudo: string, password: string, invite?: string) => Promise<void>
  onLogin: () => void
}) {
  const [invite, setInvite] = useState('')
  const [email, setEmail] = useState('')
  const [pseudo, setPseudo] = useState('')
  const [password, setPassword] = useState('')
  const [touched, setTouched] = useState(false)
  const s = useSubmit()
  const domains = policy?.email_domains ?? []
  const needInvite = policy?.registration === 'invite'
  const domainOK = domains.length === 0 || domains.includes(email.trim().toLowerCase().split('@')[1] ?? '')
  const errs = {
    email: !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim()) ? 'Adresse email invalide.'
      : !domainOK ? 'Adresses acceptées : ' + domains.map((d) => '@' + d).join(', ') + '.' : '',
    invite: needInvite && !invite.trim() ? 'Entrez le code d\u2019invitation reçu.' : '',
    pseudo: pseudoRe.test(pseudo.trim()) ? '' : 'De 3 à 32 caractères : lettres, chiffres, « _ », « . » ou « - » (pas au début ni à la fin).',
    password: [...password].length >= MIN_PASSWORD ? '' : 'Au moins ' + MIN_PASSWORD + ' caractères.',
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (errs.email || errs.pseudo || errs.password || errs.invite) return
    s.run(() => onSubmit(email.trim(), pseudo.trim(), password, needInvite ? invite.trim() : undefined))
  }
  if (policy?.registration === 'closed') {
    return (
      <div className="auth-form">
        <Head title="Créer un compte" />
        <Alert kind="info">Ce service d&apos;identité n&apos;accepte pas de nouveaux comptes. Vous pouvez en choisir un autre ci-dessous.</Alert>
        <p className="muted small">Déjà un compte ? <button type="button" className="link" onClick={onLogin}>Se connecter</button></p>
      </div>
    )
  }
  return (
    <form className="auth-form" onSubmit={submit} noValidate>
      <Head title="Créer un compte" sub="Un compte suffit pour tous les serveurs Quarel." />
      {needInvite && (
        <Field label="Code d'invitation" autoComplete="off" value={invite} onChange={(e) => setInvite(e.target.value)}
          error={touched ? errs.invite : ''} hint="Ce service est sur invitation : le code vous a été donné par son équipe ou par un de ses membres." autoFocus />
      )}
      <Field label="Email" type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)}
        error={touched ? errs.email : ''} autoFocus={!needInvite}
        hint={domains.length ? 'Adresses acceptées : ' + domains.map((d) => '@' + d).join(', ') + '. Jamais montrée aux autres.' : 'Jamais montrée aux autres. Sert à vérifier le compte et à le récupérer.'} />
      <Field label="Pseudo" autoComplete="username" value={pseudo} onChange={(e) => setPseudo(e.target.value)}
        error={touched ? errs.pseudo : ''} hint="Visible par tous. Modifiable ensuite, une fois par jour." />
      <PasswordField label="Mot de passe" autoComplete="new-password" value={password}
        onChange={(e) => setPassword(e.target.value)} error={touched ? errs.password : ''} hint={'Au moins ' + MIN_PASSWORD + ' caractères.'} />
      <Alert kind="error">{s.error}</Alert>
      <Submit busy={s.busy}>Créer mon compte</Submit>
      <p className="muted small">Déjà un compte ? <button type="button" className="link" onClick={onLogin}>Se connecter</button></p>
    </form>
  )
}

function VerifyForm({ email: initialEmail, info, client, onVerified, onBack }: {
  email: string
  info: string
  client: IdentityClient
  onVerified: (email: string) => Promise<void>
  onBack: () => void
}) {
  const [email, setEmail] = useState(initialEmail)
  const [code, setCode] = useState('')
  const [notice, setNotice] = useState(info)
  const [cooldown, startCooldown] = useCooldown()
  const s = useSubmit()
  const resend = () => s.run(async () => {
    if (!email.trim()) throw new ApiError(0, 'email_required', '')
    await client.resendVerification(email.trim())
    setNotice('Nouveau code envoyé à ' + email.trim() + '.')
    startCooldown(60)
  })
  return (
    <form className="auth-form" noValidate onSubmit={(e) => {
      e.preventDefault()
      if (!email.trim() || code.length !== 6) return s.setError('Entrez votre email et le code à 6 chiffres.')
      s.run(async () => {
        await client.verifyEmail(email.trim(), code)
        await onVerified(email.trim())
      })
    }}>
      <Head title="Vérifier votre email" sub="Le code est valable 15 minutes." />
      <Alert kind="info">{notice}</Alert>
      {!initialEmail && <Field label="Email" type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} />}
      <Field label="Code reçu par email" inputMode="numeric" autoComplete="one-time-code" maxLength={6} inputClass="code"
        value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} autoFocus />
      <Alert kind="error">{s.error}</Alert>
      <Submit busy={s.busy}>Vérifier</Submit>
      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <button type="button" className="link small" onClick={onBack}>Retour</button>
        <button type="button" className="link small" onClick={resend} disabled={cooldown > 0}>
          {cooldown > 0 ? 'Renvoyer le code (' + cooldown + ' s)' : 'Renvoyer le code'}
        </button>
      </div>
    </form>
  )
}

function ForgotForm({ onSubmit, onBack }: { onSubmit: (email: string) => Promise<void>; onBack: () => void }) {
  const [email, setEmail] = useState('')
  const s = useSubmit()
  return (
    <form className="auth-form" noValidate onSubmit={(e) => {
      e.preventDefault()
      if (!email.trim()) return s.setError('Entrez votre adresse email.')
      s.run(() => onSubmit(email.trim()))
    }}>
      <Head title="Mot de passe oublié" sub="Nous vous envoyons un code pour choisir un nouveau mot de passe." />
      <Field label="Email du compte" type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus />
      <Alert kind="error">{s.error}</Alert>
      <Submit busy={s.busy}>Recevoir un code</Submit>
      <button type="button" className="link small" style={{ alignSelf: 'flex-start' }} onClick={onBack}>Retour à la connexion</button>
    </form>
  )
}

function ResetForm({ email, info, client, onDone, onBack }: {
  email: string
  info: string
  client: IdentityClient
  onDone: () => void
  onBack: () => void
}) {
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [totp, setTotp] = useState('')
  const [needTotp, setNeedTotp] = useState(false)
  const s = useSubmit()
  return (
    <form className="auth-form" noValidate onSubmit={(e) => {
      e.preventDefault()
      if (code.length !== 6) return s.setError('Entrez le code à 6 chiffres reçu par email.')
      if ([...password].length < MIN_PASSWORD) return s.setError('Le mot de passe doit faire au moins ' + MIN_PASSWORD + ' caractères.')
      s.run(async () => {
        try {
          await client.resetPassword({ email, code, password, totp_code: needTotp ? totp.trim() : undefined })
        } catch (err) {
          if (isCode(err, 'mfa_required')) {
            setNeedTotp(true)
            throw new ApiError(0, 'mfa_needed_for_reset', '')
          }
          throw err
        }
        onDone()
      })
    }}>
      <Head title="Nouveau mot de passe" />
      <Alert kind="info">{info}</Alert>
      <Field label="Code reçu par email" inputMode="numeric" autoComplete="one-time-code" maxLength={6} inputClass="code"
        value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} autoFocus />
      <PasswordField label="Nouveau mot de passe" autoComplete="new-password" value={password}
        onChange={(e) => setPassword(e.target.value)} hint={'Au moins ' + MIN_PASSWORD + ' caractères.'} />
      {needTotp && (
        <Field label="Code de double authentification (ou code de secours)" autoComplete="one-time-code"
          value={totp} onChange={(e) => setTotp(e.target.value)}
          hint="Votre compte est protégé par la double authentification : l'email seul ne suffit pas." />
      )}
      <Alert kind="error">{s.error}</Alert>
      <Submit busy={s.busy}>Changer le mot de passe</Submit>
      <button type="button" className="link small" style={{ alignSelf: 'flex-start' }} onClick={onBack}>Retour</button>
    </form>
  )
}

function IdentityPicker({ current, onPick, onClose }: {
  current: string
  onPick: (base: string) => void
  onClose: () => void
}) {
  const [value, setValue] = useState(identityLabel(current))
  const s = useSubmit()
  return (
    <Dialog title="Service d'identité" onClose={onClose}>
      <p className="muted small" style={{ lineHeight: 1.5 }}>
        Votre compte est créé sur un service d&apos;identité. Celui de Quarel est proposé par défaut ; une association
        ou une personne peut héberger le sien. Les amis et messages privés restent entre comptes du même service.
      </p>
      <form style={{ display: 'flex', flexDirection: 'column', gap: 16 }} noValidate onSubmit={(e) => {
        e.preventDefault()
        s.run(async () => {
          let base: string
          try {
            base = identityBaseURL(value, import.meta.env.DEV)
          } catch (err) {
            const m = (err as Error).message
            throw new ApiError(0, m === 'insecure' ? 'insecure_address' : 'bad_address', '')
          }
          const keys = await new IdentityClient(base).keySet()
          if (!keys?.issuer) throw new ApiError(0, 'not_identity', '')
          onPick(base)
        })
      }}>
        <Field label="Adresse du service" value={value} onChange={(e) => setValue(e.target.value)}
          placeholder={DEFAULT_IDENTITY} error={s.error ? pickerError(s.error) : ''} autoFocus />
        <div className="dialog-actions">
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => onPick('https://' + DEFAULT_IDENTITY)}>Service par défaut</button>
          <Submit busy={s.busy} className="btn btn-primary btn-sm">Utiliser ce service</Submit>
        </div>
      </form>
    </Dialog>
  )
}

function pickerError(msg: string) {
  if (msg.includes('injoignable') || msg.startsWith('Erreur inattendue (http_')) return 'Aucun service d’identité Quarel ne répond à cette adresse.'
  return msg
}
