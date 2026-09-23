// Command quarelctl is a command-line test client for Quarel services.
//
// State (server URL, session, device key) is kept per profile in the user
// config directory, so several test accounts can coexist (-p alice, -p bob).
package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
	"github.com/mdp/qrterminal/v3"
	"golang.org/x/term"
)

const usage = `quarelctl — client de test Quarel

Usage : quarelctl [-s URL] [-p PROFIL] <commande> [arguments]

Compte
  register <email> <pseudo>        créer un compte (mot de passe demandé)
  verify-email <email> <code>      valider l'email avec le code reçu
  resend-code <email>              renvoyer un code de vérification
  login <email|pseudo> [appareil]  se connecter (code 2FA demandé si besoin)
  logout                           fermer la session courante
  me                               afficher le compte connecté

Appareils
  sessions                         lister les sessions ouvertes
  revoke-session <id>              fermer une session

Double authentification
  2fa-setup                        générer le secret (QR code à scanner)
  2fa-enable <code>                activer la 2FA, affiche les codes de secours
  2fa-disable <code>               désactiver la 2FA (code TOTP ou de secours)

Identité portable
  token                            obtenir un jeton d'identité et l'afficher
  simulate-join [nom-serveur]      simuler la connexion à un serveur communautaire

Serveurs communautaires (connexion Identity requise)
  srv-info <url>                   infos publiques d'un serveur
  join <url|lien> [invitation]     rejoindre un serveur (lien : quarel://hôte:port/CODE?sid=…)
  claim <url> <code>               devenir propriétaire (code affiché au 1er démarrage du serveur)
  servers                          serveurs rejoints (* = courant)
  use <url>                        changer de serveur courant
  srv-set name=… access=public|private   réglages du serveur (propriétaire)
  leave                            quitter le serveur courant

Salons (sur le serveur courant ; un salon se désigne par son nom ou son id)
  channels                         arborescence des salons
  channel-create <nom> [text|voice|category] [catégorie]
  channel-edit <salon> name=… topic=… parent=<catégorie|0> position=N
  channel-delete <salon>

Messages
  send <salon> <texte…>            « @pseudo » est converti en mention, « @everyone » aussi
  history <salon> [nombre] [avant-id]
  edit <salon> <id> <texte…>
  delete <salon> <id>
  listen                           afficher les événements en direct (Ctrl+C pour quitter)

Membres et invitations
  members                          liste des membres
  nick [surnom]                    changer (ou effacer) son surnom sur le serveur
  invite [utilisations] [durée]    créer une invitation (ex. : invite 5 24h ; durée 0 = illimitée)
  invites                          lister les invitations
  invite-revoke <code>

Options
  -s URL      adresse du service Identity (défaut : celle du profil, sinon http://localhost:8080)
  -c URL      serveur communautaire à utiliser au lieu du serveur courant
  -p PROFIL   profil local (défaut : "default") ; un profil = un compte de test
Variable QUAREL_PASSWORD : fournit le mot de passe sans le demander (scripts).
`

type state struct {
	Server       string `json:"server"` // Identity service
	DeviceSeed   string `json:"device_seed"`
	SessionID    string `json:"session_id,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
	Handle       string `json:"handle,omitempty"`

	// Community servers joined, keyed by base URL.
	Communities map[string]*community `json:"communities,omitempty"`
	Current     string                `json:"current_community,omitempty"`
}

type cli struct {
	path      string
	st        state
	stdin     *bufio.Reader
	community string // -c flag: community server URL overriding the current one
}

func main() {
	fs := flag.NewFlagSet("quarelctl", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	server := fs.String("s", "", "")
	profile := fs.String("p", "default", "")
	communityURL := fs.String("c", "", "")
	fs.Parse(os.Args[1:])
	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	c, err := load(*profile)
	if err == nil {
		if *server != "" {
			c.st.Server = strings.TrimRight(*server, "/")
		}
		c.community = strings.TrimRight(*communityURL, "/")
		err = c.run(args[0], args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "erreur :", err)
		os.Exit(1)
	}
}

func load(profile string) (*cli, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	c := &cli{path: filepath.Join(dir, "quarelctl", profile+".json"), stdin: bufio.NewReader(os.Stdin)}
	data, err := os.ReadFile(c.path)
	if err == nil {
		err = json.Unmarshal(data, &c.st)
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if c.st.Server == "" {
		c.st.Server = "http://localhost:8080"
	}
	if c.st.DeviceSeed == "" {
		seed := make([]byte, ed25519.SeedSize)
		rand.Read(seed)
		c.st.DeviceSeed = base64.StdEncoding.EncodeToString(seed)
	}
	return c, err
}

func (c *cli) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(c.st, "", "  ")
	return os.WriteFile(c.path, data, 0o600)
}

func (c *cli) device() ed25519.PrivateKey {
	seed, _ := base64.StdEncoding.DecodeString(c.st.DeviceSeed)
	return ed25519.NewKeyFromSeed(seed)
}

func need(args []string, n int, names string) error {
	if len(args) < n {
		return fmt.Errorf("arguments manquants : %s", names)
	}
	return nil
}

func (c *cli) run(cmd string, args []string) error {
	switch cmd {
	case "register":
		if err := need(args, 2, "<email> <pseudo>"); err != nil {
			return err
		}
		pw, err := c.password("Mot de passe (10 caractères min.) : ", true)
		if err != nil {
			return err
		}
		var out struct{ UserID, Handle string }
		if err := c.do("POST", "/v1/auth/register", map[string]string{"email": args[0], "pseudo": args[1], "password": pw}, &out); err != nil {
			return err
		}
		fmt.Printf("Compte créé : %s\nUn code de vérification a été envoyé à %s.\nEnsuite : quarelctl verify-email %s <code>\n", out.Handle, args[0], args[0])
		return c.save()

	case "verify-email":
		if err := need(args, 2, "<email> <code>"); err != nil {
			return err
		}
		if err := c.do("POST", "/v1/auth/verify-email", map[string]string{"email": args[0], "code": args[1]}, nil); err != nil {
			return err
		}
		fmt.Println("Email vérifié. Vous pouvez vous connecter : quarelctl login", args[0])
		return nil

	case "resend-code":
		if err := need(args, 1, "<email>"); err != nil {
			return err
		}
		if err := c.do("POST", "/v1/auth/resend-verification", map[string]string{"email": args[0]}, nil); err != nil {
			return err
		}
		fmt.Println("Si ce compte existe et n'est pas vérifié, un nouveau code a été envoyé (1 envoi par minute max).")
		return nil

	case "login":
		return c.login(args)

	case "logout":
		if err := c.do("POST", "/v1/auth/logout", nil, nil); err != nil {
			return err
		}
		c.st.SessionID, c.st.SessionToken, c.st.Handle = "", "", ""
		fmt.Println("Déconnecté.")
		return c.save()

	case "me":
		return c.show("GET", "/v1/me", nil)

	case "sessions":
		var list []struct {
			ID         string    `json:"id"`
			DeviceName string    `json:"device_name"`
			CreatedAt  time.Time `json:"created_at"`
			LastSeenAt time.Time `json:"last_seen_at"`
			Current    bool      `json:"current"`
		}
		if err := c.do("GET", "/v1/me/sessions", nil, &list); err != nil {
			return err
		}
		for _, s := range list {
			mark := "  "
			if s.Current {
				mark = "* "
			}
			fmt.Printf("%s%s  %-20s créée %s  vue %s\n", mark, s.ID, s.DeviceName,
				s.CreatedAt.Local().Format("2006-01-02 15:04"), s.LastSeenAt.Local().Format("2006-01-02 15:04"))
		}
		fmt.Println("(* = cette session)")
		return nil

	case "revoke-session":
		if err := need(args, 1, "<id>"); err != nil {
			return err
		}
		if err := c.do("DELETE", "/v1/me/sessions/"+args[0], nil, nil); err != nil {
			return err
		}
		fmt.Println("Session fermée.")
		return nil

	case "2fa-setup":
		pw, err := c.password("Mot de passe : ", false)
		if err != nil {
			return err
		}
		var out struct {
			Secret     string `json:"secret"`
			OtpauthURI string `json:"otpauth_uri"`
		}
		if err := c.do("POST", "/v1/me/2fa/setup", map[string]string{"password": pw}, &out); err != nil {
			return err
		}
		fmt.Println("Scannez ce QR code avec votre application d'authentification :")
		qrterminal.GenerateHalfBlock(out.OtpauthURI, qrterminal.L, os.Stdout)
		fmt.Printf("\nOu saisissez la clé manuellement : %s\n\nPuis : quarelctl 2fa-enable <code à 6 chiffres>\n", out.Secret)
		return nil

	case "2fa-enable":
		if err := need(args, 1, "<code>"); err != nil {
			return err
		}
		var out struct {
			BackupCodes []string `json:"backup_codes"`
		}
		if err := c.do("POST", "/v1/me/2fa/enable", map[string]string{"code": args[0]}, &out); err != nil {
			return err
		}
		fmt.Println("2FA activée. Codes de secours (utilisables une seule fois, à conserver en lieu sûr) :")
		for _, bc := range out.BackupCodes {
			fmt.Println("  " + bc)
		}
		return nil

	case "2fa-disable":
		if err := need(args, 1, "<code>"); err != nil {
			return err
		}
		pw, err := c.password("Mot de passe : ", false)
		if err != nil {
			return err
		}
		if err := c.do("POST", "/v1/me/2fa/disable", map[string]string{"password": pw, "code": args[0]}, nil); err != nil {
			return err
		}
		fmt.Println("2FA désactivée.")
		return nil

	case "token":
		tok, _, err := c.token()
		if err != nil {
			return err
		}
		fmt.Println(tok)
		fmt.Println("\nContenu décodé :")
		parts := strings.Split(tok, ".")
		payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var pretty bytes.Buffer
		json.Indent(&pretty, payload, "  ", "  ")
		fmt.Println("  " + pretty.String())
		return nil

	case "simulate-join":
		audience := "serveur-de-test"
		if len(args) > 0 {
			audience = args[0]
		}
		return c.simulateJoin(audience)

	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	if handled, err := c.runCommunity(cmd, args); handled {
		return err
	}
	return fmt.Errorf("commande inconnue %q (voir quarelctl help)", cmd)
}

func (c *cli) login(args []string) error {
	if err := need(args, 1, "<email|pseudo>"); err != nil {
		return err
	}
	deviceName := "quarelctl"
	if h, err := os.Hostname(); err == nil {
		deviceName = "quarelctl@" + h
	}
	if len(args) > 1 {
		deviceName = args[1]
	}
	pw, err := c.password("Mot de passe : ", false)
	if err != nil {
		return err
	}
	req := map[string]string{
		"login":       args[0],
		"password":    pw,
		"device_name": deviceName,
		"device_key":  idtoken.EncodeKey(c.device().Public().(ed25519.PublicKey)),
	}
	var out struct {
		SessionID    string `json:"session_id"`
		SessionToken string `json:"session_token"`
		User         struct{ Handle string }
	}
	c.st.SessionToken = ""
	err = c.do("POST", "/v1/auth/login", req, &out)
	var ae *apiErr
	if errors.As(err, &ae) && ae.Code == "mfa_required" {
		req["totp_code"], err = c.prompt("Code 2FA (ou code de secours) : ")
		if err != nil {
			return err
		}
		err = c.do("POST", "/v1/auth/login", req, &out)
	}
	if err != nil {
		return err
	}
	c.st.SessionID, c.st.SessionToken, c.st.Handle = out.SessionID, out.SessionToken, out.User.Handle
	fmt.Printf("Connecté en tant que %s (appareil « %s »).\n", out.User.Handle, deviceName)
	return c.save()
}

func (c *cli) token() (string, time.Time, error) {
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	err := c.do("POST", "/v1/identity/token", nil, &out)
	return out.Token, out.ExpiresAt, err
}

// simulateJoin performs, step by step, what a community server will do when a
// user joins it — so the identity flow can be tested before that server exists.
func (c *cli) simulateJoin(audience string) error {
	fmt.Printf("[serveur] récupère les clés publiques de %s%s\n", c.st.Server, idtoken.WellKnownPath)
	var ks idtoken.KeySet
	if err := c.do("GET", idtoken.WellKnownPath, nil, &ks); err != nil {
		return err
	}
	fmt.Printf("          émetteur %q, %d clé(s)\n", ks.Issuer, len(ks.Keys))

	fmt.Println("[client]  demande un jeton d'identité au service Identity")
	tok, exp, err := c.token()
	if err != nil {
		return err
	}
	fmt.Printf("          jeton valable jusqu'au %s\n", exp.Local().Format("2006-01-02 15:04:05"))

	nonceBytes := make([]byte, 16)
	rand.Read(nonceBytes)
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	fmt.Printf("[serveur] envoie un défi (nonce) : %s\n", nonce)

	proof := idtoken.SignProof(c.device(), audience, nonce)
	fmt.Println("[client]  signe le défi avec la clé privée de l'appareil")

	claims, err := idtoken.Verify(tok, ks, time.Now())
	if err != nil {
		return fmt.Errorf("[serveur] jeton refusé : %w", err)
	}
	fmt.Println("[serveur] signature du jeton vérifiée hors ligne ✔ (aucun appel au service Identity)")
	if err := idtoken.VerifyProof(claims, audience, nonce, proof); err != nil {
		return fmt.Errorf("[serveur] preuve refusée : %w", err)
	}
	fmt.Println("[serveur] preuve de l'appareil vérifiée ✔")
	fmt.Printf("\nAccès accordé à %s\n  identifiant stable : %s@%s  (clé utilisée pour les bans)\n",
		claims.Handle, claims.Subject, claims.Issuer)

	if idtoken.VerifyProof(claims, "autre-serveur", nonce, proof) != nil {
		fmt.Println("\nContrôle : la même preuve rejouée sur « autre-serveur » est bien refusée ✔")
	}
	return nil
}

// --- I/O helpers ---

func (c *cli) prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	line, err := c.stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (c *cli) password(label string, confirm bool) (string, error) {
	if pw := os.Getenv("QUAREL_PASSWORD"); pw != "" {
		return pw, nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return c.prompt(label)
	}
	fmt.Fprint(os.Stderr, label)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if confirm {
		fmt.Fprint(os.Stderr, "Confirmez : ")
		again, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if string(again) != string(pw) {
			return "", errors.New("les mots de passe ne correspondent pas")
		}
	}
	return string(pw), nil
}

type apiErr struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiErr) Error() string { return fmt.Sprintf("%s (%d) — %s", e.Code, e.Status, e.Message) }

var client = &http.Client{Timeout: 15 * time.Second}

// do calls the Identity service with the identity session.
func (c *cli) do(method, path string, body, out any) error {
	return c.request(c.st.Server, c.st.SessionToken, method, path, body, out)
}

func (c *cli) request(base, token, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("service injoignable (%s) : %w", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var er struct{ Error apiErr }
		json.NewDecoder(resp.Body).Decode(&er)
		er.Error.Status = resp.StatusCode
		if er.Error.Code == "" {
			er.Error.Code = http.StatusText(resp.StatusCode)
		}
		if er.Error.Code == "account_locked" {
			er.Error.Message += " — compte bloqué après trop d'échecs de connexion"
		}
		if resp.StatusCode == http.StatusUnauthorized && er.Error.Code == "unauthorized" && base == c.st.Server {
			er.Error.Message += " — connectez-vous avec : quarelctl login <email|pseudo>"
		}
		return &er.Error
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *cli) show(method, path string, body any) error {
	var raw json.RawMessage
	if err := c.do(method, path, body, &raw); err != nil {
		return err
	}
	var pretty bytes.Buffer
	json.Indent(&pretty, raw, "", "  ")
	fmt.Println(pretty.String())
	return nil
}
