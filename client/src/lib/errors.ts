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
  registration_closed: 'Ce service d\u2019identité n\u2019accepte pas de nouveaux comptes.',
  email_domain_not_allowed: 'Ce service n\u2019accepte que certaines adresses email.',
  account_limit_reached: 'Ce service a atteint son nombre maximum de comptes.',
  invite_quota_reached: 'Vous avez utilisé toutes vos invitations.',
  server_blocked: 'Ce serveur est bloqué par votre service d\u2019identité.',
  server_not_approved: 'Ce serveur n\u2019est pas approuvé par votre service d\u2019identité : ses comptes ne peuvent pas s\u2019y connecter.',
  wrong_audience: 'Jeton d\u2019identité destiné à un autre serveur.',
  // Community servers.
  invalid_invite: 'Invitation inconnue, expirée, épuisée ou révoquée. Demandez-en une nouvelle.',
  invite_required: 'Une invitation est nécessaire.',
  banned: 'Vous êtes banni de ce serveur.',
  invalid_claim: 'Lien propriétaire expiré ou déjà utilisé : le serveur en affiche un nouveau à chaque démarrage tant qu\u2019il n\u2019a pas de propriétaire.',
  untrusted_issuer: 'Ce serveur n\u2019accepte pas les comptes de votre service d\u2019identité.',
  issuer_unavailable: 'Le serveur n\u2019arrive pas à joindre votre service d\u2019identité. Réessayez plus tard.',
  missing_permissions: 'Vous n\u2019avez pas la permission de faire cela.',
  timed_out: 'Vous êtes exclu temporairement : lecture seule.',
  rules_not_accepted: 'Acceptez d\u2019abord les règles du serveur.',
  phone_not_verified: 'Vérifiez d\u2019abord votre numéro de téléphone.',
  invalid_content: 'Message vide ou trop long (4000 caractères au maximum).',
  too_many_attachments: '10 fichiers au maximum par message.',
  file_too_large: 'Fichier trop volumineux pour ce serveur.',
  invalid_phone: 'Numéro invalide : utilisez le format international (+33…).',
  phone_in_use: 'Ce numéro est déjà utilisé par un autre membre.',
  phone_banned: 'Ce numéro appartient à un compte banni de ce serveur.',
  phone_provider_error: 'Le SMS n\u2019a pas pu partir. Réessayez plus tard.',
  server_mismatch: 'L\u2019identité de ce serveur ne correspond pas au lien d\u2019invitation : lien erroné, ou quelqu\u2019un se fait passer pour le serveur.',
  // Local (client-side) codes.
  bad_invite: 'Lien invalide : il doit ressembler à quarel://hôte:port/CODE?sid=…',
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
