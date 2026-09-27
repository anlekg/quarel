// French messages for API error codes. Codes are stable; server messages are not shown.
import { ApiError } from '../api/http'
import { E2EError } from '../e2e/engine'
import { PhraseError } from '../e2e/recovery'

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
  pseudo_change_too_soon: 'Le pseudo ne peut changer qu\u2019une fois par jour (changer seulement les majuscules reste possible).',
  invalid_bio: 'La présentation est limitée à 500 caractères.',
  invalid_image: 'Image non reconnue : PNG, JPEG, GIF ou WebP.',
  invalid_status: 'Statut inconnu.',
  self_block: 'Vous ne pouvez pas vous bloquer vous-même.',
  mfa_required: 'Entrez aussi un code de double authentification.',
  not_found: 'Introuvable.',
  no_passkey: 'Aucune clé d\u2019accès sur ce compte : utilisez un code.',
  invalid_passkey_code: 'Ce n\u2019est pas le code affiché par la page de votre navigateur.',
  ticket_expired: 'La vérification de la clé a expiré ou échoué : recommencez.',
  '2fa_not_enabled': 'Activez d\u2019abord la double authentification : ses codes de secours restent votre porte de sortie.',
  too_many_passkeys: '10 clés d\u2019accès au plus.',
  friend_requests_closed: 'Cette personne n\u2019accepte pas de demande d\u2019ami de votre part.',
  // Community servers.
  invalid_invite: 'Invitation inconnue, expirée, épuisée ou révoquée. Demandez-en une nouvelle.',
  invite_required: 'Une invitation est nécessaire.',
  banned: 'Vous êtes banni de ce serveur.',
  invalid_claim: 'Lien propriétaire expiré ou déjà utilisé : le serveur en affiche un nouveau à chaque démarrage tant qu\u2019il n\u2019a pas de propriétaire.',
  untrusted_issuer: 'Ce serveur n\u2019accepte pas les comptes de votre service d\u2019identité.',
  issuer_unavailable: 'Le serveur n\u2019arrive pas à joindre votre service d\u2019identité. Réessayez plus tard.',
  missing_permissions: 'Vous n\u2019avez pas la permission de faire cela.',
  timed_out: 'Vous êtes exclu temporairement : lecture seule.',
  automod_word: 'Message refusé : il contient un mot interdit sur ce serveur.',
  automod_link: 'Message refusé : les liens ne sont pas autorisés sur ce serveur.',
  automod_mentions: 'Message refusé : trop de mentions dans un même message.',
  automod_duplicate: 'Message refusé : vous avez envoyé le même message trop de fois.',
  invalid_automod: 'Réglage de modération automatique invalide.',
  rules_not_accepted: 'Acceptez d\u2019abord les règles du serveur.',
  phone_not_verified: 'Vérifiez d\u2019abord votre numéro de téléphone.',
  invalid_content: 'Message vide ou trop long (4000 caractères au maximum).',
  too_many_attachments: '10 fichiers au maximum par message.',
  file_too_large: 'Fichier trop volumineux pour ce serveur.',
  invalid_phone: 'Numéro invalide : utilisez le format international (+33…).',
  phone_in_use: 'Ce numéro est déjà utilisé par un autre membre.',
  phone_banned: 'Ce numéro appartient à un compte banni de ce serveur.',
  phone_provider_error: 'Le SMS n\u2019a pas pu partir. Réessayez plus tard.',
  role_hierarchy: 'Impossible : cette personne ou ce rôle est au même niveau que vous ou au-dessus (hiérarchie des rôles).',
  cannot_timeout_admin: 'Un administrateur ne peut pas être exclu temporairement.',
  self_moderation: 'Vous ne pouvez pas faire cela sur vous-même.',
  everyone_role: 'Le rôle @everyone ne peut pas être supprimé ni attribué.',
  invalid_name: 'Nom invalide (1 à 100 caractères, sans @ # < >).',
  invalid_topic: 'Sujet trop long (1024 caractères au maximum).',
  invalid_color: 'Couleur invalide.',
  invalid_permission: 'Permission inconnue.',
  invalid_parent: 'Catégorie invalide.',
  invalid_type: 'Type de salon invalide.',
  invalid_duration: 'Durée invalide (28 jours au maximum).',
  invalid_rules: 'Règles trop longues (4000 caractères au maximum).',
  invalid_access: 'Mode d\u2019accès invalide.',
  invalid_reason: 'Raison trop longue.',
  phone_verification_unavailable: 'Aucun fournisseur de SMS n\u2019est configuré sur ce serveur (page d\u2019administration de l\u2019hébergeur).',
  owner_cannot_leave: 'Le propriétaire ne peut pas quitter son serveur : transmettez d\u2019abord la propriété à un autre membre.',
  not_owner: 'Seul le propriétaire peut transmettre le serveur.',
  invalid_target: 'Le serveur ne peut être transmis qu\u2019à une autre personne (pas un bot).',
  target_cannot_connect: 'Cette personne ne peut pas rejoindre ce salon vocal.',
  not_in_voice: 'Cette personne n\u2019est pas dans un salon vocal.',
  voice_disabled: 'Le vocal est désactivé sur ce serveur.',
  forbidden: 'Action refusée.',
  host_taken: 'Un autre serveur auto-signé utilise déjà cette adresse dans l\u2019application : Quarel n\u2019en suit qu\u2019un par adresse. Quittez l\u2019autre serveur, ou demandez à l\u2019hébergeur une autre adresse (IP, nom de domaine) ou un certificat Let\u2019s Encrypt.',
  server_mismatch: 'L\u2019identité de ce serveur ne correspond pas au lien d\u2019invitation : lien erroné, ou quelqu\u2019un se fait passer pour le serveur.',
  storage_full: 'Le serveur manque d\u2019espace disque : les fichiers sont refusés pour l\u2019instant.',
  file_quota_exceeded: 'Trop de vos fichiers attendent déjà sur le serveur : ils partent dès qu\u2019ils sont reçus (7 jours au plus).',
  inbox_full: 'Un appareil destinataire a trop de messages en attente : il doit d\u2019abord se reconnecter.',
  too_many_pending_uploads: 'Envoyez d\u2019abord les fichiers déjà joints (les fichiers non envoyés sont retirés après une heure).',
  wrong_host: 'Ce serveur ne répond pas sous cette adresse : quelqu\u2019un s\u2019est peut-être fait passer pour lui (connexion refusée). Si l\u2019adresse est la bonne, l\u2019hébergeur doit la déclarer dans « Nom public du serveur ».',
  server_outdated: 'Ce serveur utilise une version de Quarel trop ancienne pour cette application : son hébergeur doit le mettre à jour (0.3.0 ou plus récente).',
  session_ended: 'La session de cet appareil a été fermée sur son service d\u2019identité : reconnectez-vous.',
  tls_conflict: 'Cette adresse a présenté une autre identité depuis le lancement de l\u2019application : redémarrez Quarel puis réessayez. Si le problème persiste, quelqu\u2019un tente peut-être de se faire passer pour le serveur.',
  // Local (client-side) codes.
  bad_invite: 'Lien invalide : il doit ressembler à quarel://hôte:port/CODE?sid=…',
  mfa_needed_for_reset: 'Votre compte est protégé par la double authentification : entrez aussi un code.',
  email_required: 'Entrez votre adresse email.',
  insecure_address: 'Adresse non sécurisée : https est obligatoire (http seulement pour cette machine).',
  bad_address: 'Adresse invalide. Exemple : identity.quarel.app',
  not_identity: 'Aucun service d\u2019identité Quarel ne répond à cette adresse.',
  // End-to-end encryption (client-side).
  device_not_validated: 'Cet appareil n\u2019est pas encore validé : il ne peut pas encore envoyer de messages privés.',
  master_key_changed: 'La clé de sécurité de ce contact a changé depuis votre premier échange : envoi bloqué par précaution (possible usurpation). Comparez votre nouveau code de sécurité avec cette personne avant d\u2019accepter sa nouvelle clé.',
  no_keys_yet: 'Cette personne n\u2019a pas encore de clé de chiffrement (aucun appareil validé).',
  no_one_time_key: 'Un appareil du destinataire n\u2019a plus de clé disponible ; réessayez plus tard.',
  device_not_found: 'Cet appareil n\u2019existe plus (déconnecté entre-temps ?).',
  code_mismatch: 'Ce code ne correspond pas à celui de l\u2019appareil. Ne le validez pas : vérifiez le code affiché sur l\u2019autre appareil. S\u2019il est différent, quelqu\u2019un tente peut-être de s\u2019insérer.',
  bad_device_keys: 'Les clés de cet appareil sont mal signées : validation refusée.',
  no_backup: 'Aucune sauvegarde n\u2019existe pour ce compte : il faut valider cet appareil depuis un autre.',
  wrong_phrase: 'Cette phrase n\u2019ouvre pas la sauvegarde de ce compte (phrase d\u2019un autre compte, ou remplacée depuis).',
  backup_master_mismatch: 'La clé du compte contenue dans la sauvegarde ne correspond pas à celle publiée : restauration refusée.',
  backup_exists: 'Une sauvegarde existe déjà pour ce compte.',
  backup_other_phrase: 'La sauvegarde a été créée avec une autre phrase de récupération.',
  backup_conflict: 'Trop de modifications simultanées de la sauvegarde, réessayez.',
}

// An error whose message is already written for people (French).
export class UserError extends Error {}

export function errorMessage(e: unknown): string {
  if (e instanceof UserError) return e.message
  if (e instanceof PhraseError) return e.message
  if (e instanceof E2EError) return messages[e.code] ?? 'Erreur de chiffrement (' + e.code + ').'
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
