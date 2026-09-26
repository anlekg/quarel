package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/adminui"
	"github.com/anlekg/quarel/internal/community"
	"github.com/anlekg/quarel/internal/netdiag"
)

// live is what the running service exposes to the admin interface.
type live struct {
	mu     sync.Mutex
	srv    *community.Server
	cfg    community.Config
	claim  string
	voice  string // why voice is on or off
	mapper *netdiag.PortMapper
	ports  []netdiag.Mapping
}

func (l *live) set(srv *community.Server, cfg community.Config, claim, voice string, mapper *netdiag.PortMapper, ports []netdiag.Mapping) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.srv, l.cfg, l.claim, l.voice, l.mapper, l.ports = srv, cfg, claim, voice, mapper, ports
}

func (l *live) setClaim(claim string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.claim = claim
}

func (l *live) get() (*community.Server, community.Config, string, string, *netdiag.PortMapper, []netdiag.Mapping) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.srv, l.cfg, l.claim, l.voice, l.mapper, l.ports
}

func opt(v, label string) adminui.Option { return adminui.Option{Value: v, Label: label} }

var fields = []adminui.Field{
	{Group: "Accès", Key: "QUAREL_TRUSTED_ISSUERS", Label: "Services d'identité acceptés", Kind: "list", Default: community.DefaultIssuer,
		Help: "Les comptes de ces services peuvent rejoindre le serveur (séparés par des virgules). Le service officiel est identity.quarel.app."},

	{Group: "Réseau", Key: "QUAREL_ADDR", Label: "Adresse d'écoute", Kind: "text", Default: ":8090",
		Help: "Port sur lequel les applications se connectent, au format « :port »."},
	{Group: "Réseau", Key: "QUAREL_UPNP", Label: "Ouvrir automatiquement les ports sur la box (UPnP)", Kind: "bool", Default: "on",
		Help: "Sinon, ouvrez vous-même les ports indiqués dans le tableau de bord. Derrière un proxy HTTPS (certificat « aucun »), seuls les ports du vocal sont ouverts. La page d'administration n'est jamais ouverte vers Internet."},
	{Group: "Réseau", Key: "QUAREL_PUBLIC_PORT", Label: "Port public", Kind: "number", Placeholder: "identique",
		Help: "Port vu depuis Internet s'il diffère du port d'écoute (redirection de port)."},
	{Group: "Réseau", Key: "QUAREL_TRUSTED_PROXIES", Label: "Proxys de confiance", Kind: "list", Placeholder: "aucun",
		Help: "Plages d'adresses (CIDR) de vos proxys HTTPS, dont l'en-tête X-Forwarded-For est cru."},

	{Group: "HTTPS", Key: "QUAREL_TLS", Label: "Certificat", Kind: "select", Default: "self-signed",
		Options: []adminui.Option{opt("self-signed", "Automatique, lié à l'identité du serveur (recommandé sans nom de domaine)"),
			opt("acme", "Let's Encrypt (nom de domaine requis, port 443 ouvert)"), opt("files", "Mes fichiers de certificat"), opt("off", "Aucun (derrière un proxy HTTPS)")}},
	{Group: "HTTPS", Key: "QUAREL_TLS_HOSTS", Label: "Nom public du serveur", Kind: "list", ShowIf: "QUAREL_TLS=self-signed|off",
		Help: "Derrière un proxy HTTPS : son nom de domaine, obligatoire (utilisé dans les liens ; les applications refusent de se connecter sous un nom que le serveur ne déclare pas, pour qu'aucun autre serveur ne puisse se faire passer pour lui). Avec le certificat automatique : facultatif, noms ajoutés au certificat."},
	{Group: "HTTPS", Key: "QUAREL_TLS_DOMAIN", Label: "Nom de domaine", Kind: "text", ShowIf: "QUAREL_TLS=acme", Placeholder: "chat.exemple.fr"},
	{Group: "HTTPS", Key: "QUAREL_TLS_EMAIL", Label: "Email pour Let's Encrypt", Kind: "text", ShowIf: "QUAREL_TLS=acme", Help: "Facultatif : avertissements d'expiration."},
	{Group: "HTTPS", Key: "QUAREL_TLS_CERT", Label: "Fichier du certificat", Kind: "text", ShowIf: "QUAREL_TLS=files"},
	{Group: "HTTPS", Key: "QUAREL_TLS_KEY", Label: "Fichier de la clé privée", Kind: "text", ShowIf: "QUAREL_TLS=files"},

	{Group: "Vocal et vidéo", Key: "QUAREL_VOICE", Label: "Salons vocaux", Kind: "select", Default: "embedded",
		Options: []adminui.Option{opt("embedded", "Intégrés (recommandé)"), opt("external", "Serveur LiveKit existant"), opt("off", "Désactivés")}},
	{Group: "Vocal et vidéo", Key: "QUAREL_VOICE_PUBLIC_IP", Label: "Adresse annoncée", Kind: "text", Default: "auto", ShowIf: "QUAREL_VOICE=embedded",
		Help: "« auto » : adresse publique de la box ; « local » : réseau local seulement ; ou une adresse IP."},
	{Group: "Vocal et vidéo", Key: "QUAREL_VOICE_UDP_PORT", Label: "Port UDP", Kind: "number", Default: "7882", ShowIf: "QUAREL_VOICE=embedded"},
	{Group: "Vocal et vidéo", Key: "QUAREL_VOICE_TCP_PORT", Label: "Port TCP (secours)", Kind: "number", Default: "7881", ShowIf: "QUAREL_VOICE=embedded"},
	{Group: "Vocal et vidéo", Key: "QUAREL_LIVEKIT_URL", Label: "Adresse LiveKit pour les applications", Kind: "text", ShowIf: "QUAREL_VOICE=external", Placeholder: "wss://…"},
	{Group: "Vocal et vidéo", Key: "QUAREL_LIVEKIT_API_URL", Label: "Adresse de l'API LiveKit", Kind: "text", ShowIf: "QUAREL_VOICE=external", Placeholder: "https://…"},
	{Group: "Vocal et vidéo", Key: "QUAREL_LIVEKIT_KEY", Label: "Clé d'API LiveKit", Kind: "text", ShowIf: "QUAREL_VOICE=external"},
	{Group: "Vocal et vidéo", Key: "QUAREL_LIVEKIT_SECRET", Label: "Secret d'API LiveKit", Kind: "secret", ShowIf: "QUAREL_VOICE=external"},

	{Group: "Messages", Key: "QUAREL_MAX_UPLOAD_MB", Label: "Taille maximale des fichiers (Mo)", Kind: "number", Default: "25"},
	{Group: "Messages", Key: "QUAREL_MIN_FREE_MB", Label: "Espace disque réservé (Mo)", Kind: "number", Default: "1024",
		Help: "Les fichiers sont refusés quand il reste moins d'espace libre sur la machine."},
	{Group: "Messages", Key: "QUAREL_LINK_PREVIEWS", Label: "Aperçus des liens", Kind: "bool", Default: "on",
		Help: "Le serveur visite les liens publiés pour en afficher le titre (jamais d'adresse privée)."},

	{Group: "Vérification du téléphone", Key: "QUAREL_PHONE_VERIFY", Label: "Envoi des SMS", Kind: "select", Default: "off",
		Options: []adminui.Option{opt("off", "Désactivé"), opt("webhook", "Webhook (votre passerelle SMS)"), opt("ovh", "OVHcloud SMS"), opt("twilio", "Twilio Verify"), opt("log", "Test : code dans le journal")}},
	{Group: "Vérification du téléphone", Key: "QUAREL_PHONE_WEBHOOK_URL", Label: "Adresse du webhook", Kind: "text", ShowIf: "QUAREL_PHONE_VERIFY=webhook", Placeholder: "https://…"},
	{Group: "Vérification du téléphone", Key: "QUAREL_PHONE_WEBHOOK_SECRET", Label: "Secret de signature", Kind: "secret", ShowIf: "QUAREL_PHONE_VERIFY=webhook"},
	{Group: "Vérification du téléphone", Key: "QUAREL_OVH_APP_KEY", Label: "Application key", Kind: "text", ShowIf: "QUAREL_PHONE_VERIFY=ovh"},
	{Group: "Vérification du téléphone", Key: "QUAREL_OVH_APP_SECRET", Label: "Application secret", Kind: "secret", ShowIf: "QUAREL_PHONE_VERIFY=ovh"},
	{Group: "Vérification du téléphone", Key: "QUAREL_OVH_CONSUMER_KEY", Label: "Consumer key", Kind: "secret", ShowIf: "QUAREL_PHONE_VERIFY=ovh"},
	{Group: "Vérification du téléphone", Key: "QUAREL_OVH_SMS_SERVICE", Label: "Service SMS", Kind: "text", ShowIf: "QUAREL_PHONE_VERIFY=ovh", Placeholder: "sms-xx00000-1"},
	{Group: "Vérification du téléphone", Key: "QUAREL_OVH_SMS_SENDER", Label: "Expéditeur", Kind: "text", ShowIf: "QUAREL_PHONE_VERIFY=ovh", Placeholder: "numéro court par défaut"},
	{Group: "Vérification du téléphone", Key: "QUAREL_TWILIO_ACCOUNT_SID", Label: "Account SID", Kind: "text", ShowIf: "QUAREL_PHONE_VERIFY=twilio"},
	{Group: "Vérification du téléphone", Key: "QUAREL_TWILIO_AUTH_TOKEN", Label: "Auth token", Kind: "secret", ShowIf: "QUAREL_PHONE_VERIFY=twilio"},
	{Group: "Vérification du téléphone", Key: "QUAREL_TWILIO_VERIFY_SID", Label: "Service Verify (VA…)", Kind: "text", ShowIf: "QUAREL_PHONE_VERIFY=twilio"},
}

// The official web app (invite links open it).
const webApp = "https://app.quarel.app"

var tlsLabels = map[string]string{"self-signed": "automatique (lié à l'identité du serveur)", "acme": "Let's Encrypt", "files": "fichiers fournis", "off": "aucun (proxy HTTPS)"}

// status fills the dashboard.
func (l *live) status(r *http.Request) map[string]any {
	srv, cfg, claim, voice, mapper, ports := l.get()
	if srv == nil {
		return nil
	}
	ctx := r.Context()
	ov, err := srv.Overview(ctx)
	if err != nil {
		return map[string]any{"notices": []map[string]string{{"level": "error", "text": err.Error()}}}
	}
	host := hostOf(r, cfg)
	port := publicPort(cfg)
	if cfg.TLS.Mode == "off" { // behind an HTTPS proxy: the apps use its address
		port = 443
		if len(cfg.TLS.Hosts) > 0 {
			host = cfg.TLS.Hosts[0]
		}
	}
	items := []map[string]any{
		{"label": "Nom", "value": ov.Name},
		{"label": "Identifiant", "value": srv.ID(), "mono": true},
		{"label": "Adresse", "value": httpsURL(host, port), "mono": true},
		{"label": "Membres", "value": ov.Members},
		{"label": "Services d'identité acceptés", "value": strings.Join(cfg.TrustedIssuers, ", ")},
		{"label": "Certificat", "value": tlsLabels[cfg.TLS.Mode]},
		{"label": "Vocal", "value": voice},
	}
	var notices []map[string]string
	switch {
	case !cfg.UPnP:
		notices = append(notices, map[string]string{"level": "info", "text": "UPnP désactivé : pour être joignable depuis Internet, redirigez sur votre box " + netdiag.Describe(ports) + " vers cette machine."})
	case mapper == nil:
		notices = append(notices, map[string]string{"level": "warn", "text": "La box n'a pas répondu à l'UPnP : redirigez vous-même " + netdiag.Describe(ports) + " vers cette machine."})
	default:
		st := mapper.Status()
		items = append(items, map[string]any{"label": "Box (UPnP)", "value": fmt.Sprintf("%d/%d port(s) ouverts", len(st.Mapped), len(ports))})
		if netdiag.IsPrivate(st.ExternalIP) {
			notices = append(notices, map[string]string{"level": "warn", "text": "L'adresse de votre box est elle-même privée (NAT de l'opérateur) : le serveur n'est probablement pas joignable depuis Internet. Demandez une adresse IPv4 publique à votre fournisseur."})
		} else if len(st.Errors) > 0 {
			notices = append(notices, map[string]string{"level": "warn", "text": "Certains ports n'ont pas pu être ouverts : " + strings.Join(st.Errors, " ; ")})
		}
	}
	if cfg.TLS.Mode == "off" && len(cfg.TLS.Hosts) == 0 {
		notices = append(notices, map[string]string{"level": "warn", "text": "Derrière un proxy HTTPS, indiquez son nom de domaine dans « Nom public du serveur » (réglages › HTTPS) : " +
			"les applications vérifient que le serveur répond bien sous l'adresse utilisée, et refusent la connexion sinon."})
	}
	if cfg.TLS.Mode == "self-signed" {
		notices = append(notices, map[string]string{"level": "info", "text": "Certificat automatique : l'application de bureau vérifie ce serveur grâce à son identité, mais les navigateurs le refusent. " +
			"Pour que la version web (" + webApp + ") puisse aussi le rejoindre, donnez-lui un nom de domaine et choisissez Let's Encrypt (réglages › HTTPS), ou placez-le derrière un proxy HTTPS."})
	}
	resp := map[string]any{"items": items, "notices": notices, "has_owner": ov.HasOwner}
	if !ov.HasOwner && claim != "" {
		addr := strings.TrimPrefix(httpsURL(host, port), "https://")
		resp["claim_code"] = claim
		resp["owner_link"] = fmt.Sprintf("quarel://%s/%s?sid=%s&claim=1", addr, claim, srv.ID())
		if cfg.TLS.Mode != "self-signed" { // browsers accept this certificate
			resp["owner_web_link"] = fmt.Sprintf("%s/join#%s/%s?sid=%s&claim=1", webApp, addr, claim, srv.ID())
		}
	}
	return resp
}

func httpsURL(host string, port int) string {
	if port == 443 {
		return "https://" + host
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// hostOf guesses the address the apps will use: the domain if any, else the
// address under which the administrator reached this machine.
func hostOf(r *http.Request, cfg community.Config) string {
	if cfg.TLS.Mode == "acme" && cfg.TLS.Domain != "" {
		return cfg.TLS.Domain
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

func publicPort(cfg community.Config) int {
	if cfg.PublicPort != 0 {
		return cfg.PublicPort
	}
	_, p, _ := net.SplitHostPort(cfg.Addr)
	n, _ := strconv.Atoi(p)
	return n
}

// api serves the community-specific admin endpoints.
func (l *live) api() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /rename", func(w http.ResponseWriter, r *http.Request) {
		srv, _, _, _, _, _ := l.get()
		var req struct{ Name string }
		if err := adminui.DecodeJSON(r, &req); err != nil || srv == nil {
			http.Error(w, "service not running", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10e9)
		defer cancel()
		if err := srv.SetName(ctx, req.Name); err != nil {
			adminui.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// The owner can no longer sign in (account lost or deleted): the host
	// removes them and gets a new owner link.
	mux.HandleFunc("POST /reset-owner", func(w http.ResponseWriter, r *http.Request) {
		srv, _, _, _, _, _ := l.get()
		if srv == nil {
			http.Error(w, "service not running", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		claim, err := srv.ResetOwnership(ctx)
		if err != nil {
			adminui.WriteError(w, r, err)
			return
		}
		l.setClaim(claim)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /identity", func(w http.ResponseWriter, r *http.Request) {
		srv, _, _, _, _, _ := l.get()
		if srv == nil {
			adminui.WriteJSON(w, http.StatusOK, []any{})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		adminui.WriteJSON(w, http.StatusOK, srv.IdentityServices(ctx))
	})
	mux.HandleFunc("POST /identity/request", func(w http.ResponseWriter, r *http.Request) {
		srv, cfg, _, _, _, _ := l.get()
		var req struct{ Issuer, Contact string }
		if err := adminui.DecodeJSON(r, &req); err != nil || srv == nil {
			http.Error(w, "service not running", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		host, port := hostOf(r, cfg), publicPort(cfg)
		if cfg.TLS.Mode == "off" {
			port = 443
			if len(cfg.TLS.Hosts) > 0 {
				host = cfg.TLS.Hosts[0]
			}
		}
		url := httpsURL(host, port)
		status, err := srv.RequestApproval(ctx, req.Issuer, url, strings.TrimSpace(req.Contact))
		if err != nil {
			adminui.WriteJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]string{"code": "request_failed", "message": err.Error()}})
			return
		}
		adminui.WriteJSON(w, http.StatusOK, map[string]string{"status": status})
	})
	return mux
}
