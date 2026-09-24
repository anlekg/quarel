package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/adminui"
	"github.com/anlekg/quarel/internal/identity"
)

// live is what the running service exposes to the admin interface.
type live struct {
	mu      sync.Mutex
	srv     *identity.Server
	cfg     identity.Config
	restart func()
}

func (l *live) set(srv *identity.Server, cfg identity.Config) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.srv, l.cfg = srv, cfg
}

func (l *live) get() (*identity.Server, identity.Config) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.srv, l.cfg
}

func opt(v, label string) adminui.Option { return adminui.Option{Value: v, Label: label} }

var fields = []adminui.Field{
	{Group: "Service", Key: "QUAREL_ISSUER", Label: "Nom public du service", Kind: "text", Default: "localhost:8080", Placeholder: "identity.exemple.fr",
		Help: "Le nom de domaine sous lequel ce service est joignable. Il fait partie de chaque identifiant (pseudo@nom) : ne le changez plus une fois des comptes créés."},
	{Group: "Service", Key: "QUAREL_ADDR", Label: "Adresse d'écoute", Kind: "text", Default: ":8080", Help: "Port du service, au format « :port »."},
	{Group: "Service", Key: "QUAREL_TRUSTED_PROXIES", Label: "Proxys de confiance", Kind: "list", Placeholder: "aucun",
		Help: "Plages d'adresses (CIDR) de vos proxys HTTPS, dont l'en-tête X-Forwarded-For est cru."},

	{Group: "Inscriptions", Key: "QUAREL_REGISTRATION", Label: "Création de comptes", Kind: "select", Default: "open",
		Options: []adminui.Option{opt("open", "Ouverte à tous"), opt("invite", "Sur invitation (code à saisir)"), opt("closed", "Fermée")},
		Help:    "Les invitations se créent dans la page « Invitations »."},
	{Group: "Inscriptions", Key: "QUAREL_REGISTRATION_DOMAINS", Label: "Domaines d'email autorisés", Kind: "list", Placeholder: "tous",
		Help: "Par exemple « asso.fr, ecole.fr » : seules ces adresses peuvent créer un compte."},
	{Group: "Inscriptions", Key: "QUAREL_MAX_ACCOUNTS", Label: "Nombre maximum de comptes", Kind: "number", Default: "0", Help: "0 : pas de limite."},
	{Group: "Inscriptions", Key: "QUAREL_USER_INVITES", Label: "Invitations par utilisateur", Kind: "number", Default: "0",
		Help: "Nombre d'invitations que chaque utilisateur peut créer (usage unique, 30 jours). 0 : seul l'opérateur invite."},

	{Group: "Serveurs communautaires", Key: "QUAREL_SERVER_POLICY", Label: "Serveurs autorisés", Kind: "select", Default: "open",
		Options: []adminui.Option{opt("open", "Tous, sauf ceux de la liste noire (recommandé)"), opt("approved", "Seulement les serveurs approuvés")},
		Help:    "« Tous » : l'application refuse les serveurs bloqués, et ce service ne sait pas où vont ses utilisateurs. « Approuvés » : les jetons d'identité sont chiffrés pour les seuls serveurs approuvés (contrôle réel), mais ce service voit à quels serveurs chacun se connecte (sans le conserver)."},

	{Group: "HTTPS", Key: "QUAREL_TLS", Label: "Certificat", Kind: "select", Default: "off",
		Options: []adminui.Option{opt("off", "Aucun (derrière un proxy HTTPS, ou tests)"), opt("acme", "Let's Encrypt (port 443 ouvert vers ce service)"), opt("files", "Mes fichiers de certificat")},
		Help:    "Les serveurs communautaires vérifient ce service avec les autorités publiques : un certificat reconnu est obligatoire en production."},
	{Group: "HTTPS", Key: "QUAREL_TLS_DOMAIN", Label: "Nom de domaine", Kind: "text", ShowIf: "QUAREL_TLS=acme", Placeholder: "identity.exemple.fr"},
	{Group: "HTTPS", Key: "QUAREL_TLS_EMAIL", Label: "Email pour Let's Encrypt", Kind: "text", ShowIf: "QUAREL_TLS=acme"},
	{Group: "HTTPS", Key: "QUAREL_TLS_CERT", Label: "Fichier du certificat", Kind: "text", ShowIf: "QUAREL_TLS=files"},
	{Group: "HTTPS", Key: "QUAREL_TLS_KEY", Label: "Fichier de la clé privée", Kind: "text", ShowIf: "QUAREL_TLS=files"},

	{Group: "Emails", Key: "QUAREL_SMTP_HOST", Label: "Serveur SMTP", Kind: "text", Placeholder: "smtp.exemple.fr",
		Help: "Sans serveur SMTP, les codes de vérification sont seulement écrits dans le journal (tests)."},
	{Group: "Emails", Key: "QUAREL_SMTP_PORT", Label: "Port SMTP", Kind: "number", Default: "587"},
	{Group: "Emails", Key: "QUAREL_SMTP_USER", Label: "Utilisateur SMTP", Kind: "text"},
	{Group: "Emails", Key: "QUAREL_SMTP_PASSWORD", Label: "Mot de passe SMTP", Kind: "secret"},
	{Group: "Emails", Key: "QUAREL_SMTP_FROM", Label: "Expéditeur", Kind: "text", Placeholder: "Quarel <quarel@exemple.fr>"},

	{Group: "Relais d'appels", Key: "QUAREL_TURN", Label: "Relais pour les appels qui ne passent pas en direct", Kind: "select", Default: "off",
		Options: []adminui.Option{opt("off", "Désactivé"), opt("on", "Activé")},
		Help:    "La bande passante des appels relayés passe par cette machine. Le relais refuse toute adresse privée."},
	{Group: "Relais d'appels", Key: "QUAREL_TURN_PUBLIC_IP", Label: "Adresse IP publique de cette machine", Kind: "text", ShowIf: "QUAREL_TURN=on"},
	{Group: "Relais d'appels", Key: "QUAREL_TURN_LISTEN", Label: "Port du relais", Kind: "text", Default: ":3478", ShowIf: "QUAREL_TURN=on", Help: "UDP, à ouvrir sur le pare-feu."},
	{Group: "Relais d'appels", Key: "QUAREL_TURN_PORTS", Label: "Plage de ports relayés", Kind: "text", Default: "49160-49200", ShowIf: "QUAREL_TURN=on", Help: "UDP, à ouvrir sur le pare-feu."},

	{Group: "Messages privés", Key: "QUAREL_DM_FILE_MAX_MB", Label: "Taille maximale d'une copie de fichier sur le serveur (Mo)", Kind: "number", Default: "25",
		Help: "Au-delà, les fichiers passent uniquement en direct entre appareils."},
	{Group: "Messages privés", Key: "QUAREL_DM_FILE_TTL", Label: "Conservation maximale d'une copie de fichier", Kind: "text", Default: "168h",
		Help: "Effacée dès que tous les appareils l'ont reçue, et au plus tard après cette durée (168h = 7 jours)."},
	{Group: "Messages privés", Key: "QUAREL_DM_GROUP_MAX", Label: "Membres maximum d'un groupe", Kind: "number", Default: "10"},
	{Group: "Messages privés", Key: "QUAREL_TOKEN_TTL", Label: "Durée des jetons d'identité", Kind: "text", Default: "12h"},
}

var tlsLabels = map[string]string{"acme": "Let's Encrypt", "files": "fichiers fournis", "off": "aucun (proxy HTTPS ou tests)"}

func (l *live) status(r *http.Request) map[string]any {
	srv, cfg := l.get()
	if srv == nil {
		return nil
	}
	total, disabled, err := srv.Admin().Counts(r.Context())
	if err != nil {
		return map[string]any{"notices": []map[string]string{{"level": "error", "text": err.Error()}}}
	}
	mail := "non configurés : les codes sont écrits dans le journal"
	if cfg.SMTP.Host != "" {
		mail = "via " + cfg.SMTP.Host
	}
	relay := "désactivé"
	if cfg.TURN.Enabled {
		relay = fmt.Sprintf("actif sur %s (UDP %s, ports %d-%d)", cfg.TURN.PublicIP, strings.TrimPrefix(cfg.TURN.Listen, ":"), cfg.TURN.MinPort, cfg.TURN.MaxPort)
	}
	regLabels := map[string]string{"open": "ouverte", "invite": "sur invitation", "closed": "fermée"}
	polLabels := map[string]string{"open": "tous, sauf liste noire", "approved": "approuvés seulement"}
	items := []map[string]any{
		{"label": "Nom public", "value": cfg.Issuer, "mono": true},
		{"label": "Inscriptions", "value": regLabels[cfg.Registration]},
		{"label": "Serveurs communautaires", "value": polLabels[cfg.ServerPolicy]},
		{"label": "Comptes", "value": fmt.Sprintf("%d (dont %d désactivé(s))", total, disabled)},
		{"label": "Certificat", "value": tlsLabels[cfg.TLS.Mode]},
		{"label": "Emails", "value": mail},
		{"label": "Relais d'appels", "value": relay},
	}
	var notices []map[string]string
	host := strings.Split(cfg.Issuer, ":")[0]
	if host == "localhost" || strings.HasPrefix(host, "127.") {
		notices = append(notices, map[string]string{"level": "warn", "text": "Le nom public est « " + cfg.Issuer + " » : seuls des tests sur cette machine sont possibles. Indiquez le nom de domaine du service dans les réglages avant de créer des comptes."})
	}
	if cfg.SMTP.Host == "" {
		notices = append(notices, map[string]string{"level": "warn", "text": "Aucun serveur d'emails : les utilisateurs ne recevront pas leurs codes de vérification."})
	}
	if servers, err := srv.Admin().Servers(r.Context()); err == nil {
		pending := 0
		for _, sv := range servers {
			if sv.Status == "pending" && !sv.Blocked {
				pending++
			}
		}
		if pending > 0 {
			notices = append(notices, map[string]string{"level": "info", "text": fmt.Sprintf("%d serveur(s) communautaire(s) attendent votre approbation (page « Serveurs »).", pending)})
		}
	}
	return map[string]any{"items": items, "notices": notices}
}

func fail(w http.ResponseWriter, code, msg string) {
	adminui.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func (l *live) api() http.Handler {
	mux := http.NewServeMux()
	admin := func(w http.ResponseWriter, r *http.Request) *identity.Admin {
		srv, _ := l.get()
		if srv == nil {
			adminui.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]string{"code": "stopped", "message": "le service est arrêté"}})
			return nil
		}
		return srv.Admin()
	}
	mux.HandleFunc("GET /accounts", func(w http.ResponseWriter, r *http.Request) {
		if a := admin(w, r); a != nil {
			list, err := a.Search(r.Context(), r.URL.Query().Get("q"), 50)
			if err != nil {
				adminui.WriteError(w, r, err)
				return
			}
			adminui.WriteJSON(w, http.StatusOK, list)
		}
	})
	for _, action := range []string{"disable", "enable"} {
		mux.HandleFunc("POST /accounts/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			a := admin(w, r)
			if a == nil {
				return
			}
			var req struct{ Reason string }
			if err := adminui.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
				adminui.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "reason_required", "message": "la raison est obligatoire"}})
				return
			}
			if _, err := a.SetDisabled(r.Context(), r.PathValue("id"), action == "disable", req.Reason, "administration web"); err != nil {
				adminui.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "failed", "message": err.Error()}})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	mux.HandleFunc("GET /log", func(w http.ResponseWriter, r *http.Request) {
		if a := admin(w, r); a != nil {
			entries, err := a.Log(r.Context(), 100)
			if err != nil {
				adminui.WriteError(w, r, err)
				return
			}
			out := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				out = append(out, map[string]any{"at": e.At.UTC(), "action": e.Action, "target": e.UserID, "operator": e.Operator, "reason": e.Reason})
			}
			adminui.WriteJSON(w, http.StatusOK, out)
		}
	})
	mux.HandleFunc("GET /invites", func(w http.ResponseWriter, r *http.Request) {
		if a := admin(w, r); a != nil {
			list, err := a.Invites(r.Context())
			if err != nil {
				adminui.WriteError(w, r, err)
				return
			}
			adminui.WriteJSON(w, http.StatusOK, list)
		}
	})
	mux.HandleFunc("POST /invites", func(w http.ResponseWriter, r *http.Request) {
		a := admin(w, r)
		if a == nil {
			return
		}
		var req struct {
			Note      string `json:"note"`
			MaxUses   int    `json:"max_uses"`
			ValidDays int    `json:"valid_days"`
		}
		if err := adminui.DecodeJSON(r, &req); err != nil || req.MaxUses < 0 || req.ValidDays < 0 {
			fail(w, "invalid_request", "valeurs invalides")
			return
		}
		inv, err := a.CreateInvite(r.Context(), req.Note, req.MaxUses, time.Duration(req.ValidDays)*24*time.Hour)
		if err != nil {
			fail(w, "failed", err.Error())
			return
		}
		adminui.WriteJSON(w, http.StatusOK, inv)
	})
	mux.HandleFunc("POST /invites/{code}/revoke", func(w http.ResponseWriter, r *http.Request) {
		if a := admin(w, r); a != nil {
			if err := a.RevokeInvite(r.Context(), r.PathValue("code")); err != nil {
				adminui.WriteError(w, r, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("GET /servers", func(w http.ResponseWriter, r *http.Request) {
		if a := admin(w, r); a != nil {
			list, err := a.Servers(r.Context())
			if err != nil {
				adminui.WriteError(w, r, err)
				return
			}
			_, cfg := l.get()
			adminui.WriteJSON(w, http.StatusOK, map[string]any{"policy": cfg.ServerPolicy, "servers": list})
		}
	})
	mux.HandleFunc("POST /servers/block", func(w http.ResponseWriter, r *http.Request) {
		a := admin(w, r)
		if a == nil {
			return
		}
		var req struct{ ID, Reason string }
		if err := adminui.DecodeJSON(r, &req); err != nil {
			fail(w, "invalid_request", "requête invalide")
			return
		}
		if err := a.BlockServer(r.Context(), req.ID, req.Reason, "administration web"); err != nil {
			fail(w, "failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, action := range []string{"unblock", "approve", "reject"} {
		mux.HandleFunc("POST /servers/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			a := admin(w, r)
			if a == nil {
				return
			}
			var err error
			switch action {
			case "unblock":
				err = a.UnblockServer(r.Context(), r.PathValue("id"), "administration web")
			default:
				err = a.DecideServer(r.Context(), r.PathValue("id"), action == "approve", "administration web")
			}
			if err != nil {
				fail(w, "failed", err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	mux.HandleFunc("POST /rotate-key", func(w http.ResponseWriter, r *http.Request) {
		a := admin(w, r)
		if a == nil {
			return
		}
		_, cfg := l.get()
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if _, _, err := a.RotateSigningKey(ctx, cfg.DataDir, "rotation depuis l'administration web", "administration web"); err != nil {
			adminui.WriteError(w, r, err)
			return
		}
		l.restart() // the new key is loaded at start
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
