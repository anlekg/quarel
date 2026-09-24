// French messages for API error codes. Codes are stable; server messages are not shown.
import { ApiError } from '../api/http'

const messages: Record<string, string> = {
  network: 'Service injoignable. Vérifiez votre connexion ou l’adresse du service.',
  invalid_credentials: 'Identifiant ou mot de passe incorrect.',
  invalid_mfa_code: 'Code incorrect.',
  email_not_verified: 'Adresse email pas encore vérifiée.',
  account_disabled: 'Ce compte a été désactivé par le service d’identité.',
  invalid_email: 'Adresse email invalide.',
  invalid_pseudo: 'Le pseudo doit faire de 3 à 32 caractères : lettres, chiffres, « _ », « . » ou « - » (pas au début ni à la fin).',
  weak_password: 'Le mot de passe doit faire au moins 10 caractères.',
  email_taken: 'Cette adresse email est déjà utilisée.',
  pseudo_taken: 'Ce pseudo est déjà pris.',
  already_in_use: 'Déjà utilisé.',
  invalid_code: 'Code invalide ou expiré. Demandez-en un nouveau si besoin.',
  unauthorized: 'Session expirée, reconnectez-vous.',
  // Local (client-side) codes.
  mfa_needed_for_reset: 'Votre compte est protégé par la double authentification : entrez aussi un code.',
  email_required: 'Entrez votre adresse email.',
  insecure_address: 'Adresse non sécurisée : https est obligatoire (http seulement pour cette machine).',
  bad_address: 'Adresse invalide. Exemple : identity.quarel.app',
  not_identity: 'Aucun service d\u2019identité Quarel ne répond à cette adresse.',
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'account_locked' || e.code === 'rate_limited') {
      const wait = e.retryAfter > 0 ? ' Réessayez ' + waitText(e.retryAfter) + '.' : ' Réessayez plus tard.'
      return (e.code === 'account_locked' ? 'Trop d’essais infructueux.' : 'Trop de demandes.') + wait
    }
    return messages[e.code] ?? 'Erreur inattendue (' + e.code + ').'
  }
  return 'Erreur inattendue.'
}

function waitText(seconds: number) {
  if (seconds < 60) return 'dans ' + seconds + ' s'
  const min = Math.ceil(seconds / 60)
  return 'dans ' + min + ' min'
}

export function isCode(e: unknown, code: string) {
  return e instanceof ApiError && e.code === code
}
