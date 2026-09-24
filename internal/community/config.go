package community

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/settings"

	"github.com/anlekg/quarel/internal/ratelimit"
	"github.com/anlekg/quarel/internal/tlsconf"
)

// DefaultIssuer is the official Identity service trusted when none is configured.
// It is part of every official identity: never change it.
const DefaultIssuer = "identity.quarel.app"

// DefaultLiveKitBin is the voice server program when QUAREL_LIVEKIT_BIN is
// not set (the Windows build points to the copy installed next to it).
var DefaultLiveKitBin = "livekit-server"

// Config holds the community server settings.
type Config struct {
	Addr    string
	DataDir string
	// Name is the server name set on first start; afterwards it is changed through the API.
	Name string
	// TrustedIssuers lists the Identity services whose users may join.
	TrustedIssuers []string

	// Voice: "embedded" (default, runs livekit-server), "external" (existing LiveKit) or "off".
	Voice           string
	LiveKitBin      string // embedded: livekit-server binary
	VoiceSignalPort int    // embedded: loopback signalling port
	VoiceTCPPort    int    // embedded: media over TCP (fallback), to open on the router
	VoiceUDPPort    int    // embedded: media over UDP, to open on the router
	VoicePublicIP   string // embedded: "auto" (router's address via UPnP, else STUN), "local" (LAN only), or an IP
	LiveKitURL      string // external: URL given to clients (wss://…)
	LiveKitAPIURL   string // external: URL the server uses for the room API (https://…)
	LiveKitKey      string // external: API key
	LiveKitSecret   string // external: API secret

	Limits         Limits
	TrustedProxies ratelimit.Proxies // reverse proxies whose X-Forwarded-For is believed
	TLS            tlsconf.Config
	UPnP           bool // open ports on the router automatically
	PublicPort     int  // external port of the HTTP(S) service (UPnP mapping); 0 = same as the listening port

	MaxUploadBytes int64 // attachment size limit
	LinkPreviews   bool  // fetch previews of posted links (the server contacts the linked sites)

	// Phone verification provider: "off" (default), "webhook", "ovh", "twilio",
	// or "log" (development: codes in the log).
	PhoneVerify        string
	PhoneWebhookURL    string
	PhoneWebhookSecret string
	OVHEndpoint        string
	OVHAppKey          string
	OVHAppSecret       string
	OVHConsumerKey     string
	OVHSMSService      string
	OVHSMSSender       string
	TwilioAccountSID   string
	TwilioAuthToken    string
	TwilioVerifySID    string // Verify service ("VA…")

	// DisabledPoll is how often the lists of disabled accounts of the trusted
	// Identity services are fetched.
	DisabledPoll time.Duration
}

// Limits caps request rates (0 disables a limit).
type Limits struct {
	Global   int // requests per client IP per minute, all endpoints
	Auth     int // login challenges and logins per client IP per minute
	Messages int // messages sent per member per 10 seconds
	Uploads  int // attachments uploaded per member per minute
	Phone    int // phone verification codes sent per member per hour
}

// DefaultLimits are the production limits.
func DefaultLimits() Limits {
	return Limits{Global: 600, Auth: 30, Messages: 10, Uploads: 10, Phone: 5}
}

// ConfigFromEnv reads the configuration from QUAREL_* environment variables.
func ConfigFromEnv() (Config, error) {
	c := Config{
		Addr:    env("QUAREL_ADDR", ":8090"),
		DataDir: settings.DataDir(),
		Name:    env("QUAREL_SERVER_NAME", "Serveur Quarel"),
	}
	c.Voice = env("QUAREL_VOICE", "embedded")
	c.LiveKitBin = env("QUAREL_LIVEKIT_BIN", DefaultLiveKitBin)
	c.VoiceSignalPort = envInt("QUAREL_VOICE_SIGNAL_PORT", 7880)
	c.VoiceTCPPort = envInt("QUAREL_VOICE_TCP_PORT", 7881)
	c.VoiceUDPPort = envInt("QUAREL_VOICE_UDP_PORT", 7882)
	c.VoicePublicIP = env("QUAREL_VOICE_PUBLIC_IP", "auto")
	c.LiveKitURL = settings.Get("QUAREL_LIVEKIT_URL")
	c.LiveKitAPIURL = settings.Get("QUAREL_LIVEKIT_API_URL")
	c.LiveKitKey = settings.Get("QUAREL_LIVEKIT_KEY")
	c.LiveKitSecret = settings.Get("QUAREL_LIVEKIT_SECRET")
	for _, iss := range strings.Split(env("QUAREL_TRUSTED_ISSUERS", DefaultIssuer), ",") {
		if iss = strings.TrimSpace(iss); iss != "" {
			c.TrustedIssuers = append(c.TrustedIssuers, iss)
		}
	}
	c.Limits = DefaultLimits()
	if settings.Get("QUAREL_RATE_LIMITS") == "off" {
		c.Limits = Limits{}
	}
	var err error
	if c.TrustedProxies, err = ratelimit.ParseProxies(settings.Get("QUAREL_TRUSTED_PROXIES")); err != nil {
		return c, fmt.Errorf("proxys de confiance (QUAREL_TRUSTED_PROXIES) : %w", err)
	}
	if c.TLS, err = tlsconf.FromEnv(tlsconf.SelfSigned); err != nil {
		return c, err
	}
	c.UPnP = env("QUAREL_UPNP", "on") != "off"
	c.MaxUploadBytes = int64(envInt("QUAREL_MAX_UPLOAD_MB", 25)) << 20
	c.LinkPreviews = env("QUAREL_LINK_PREVIEWS", "on") != "off"
	c.PublicPort = envInt("QUAREL_PUBLIC_PORT", 0)
	if c.DisabledPoll, err = time.ParseDuration(env("QUAREL_DISABLED_POLL", "10m")); err != nil || c.DisabledPoll < time.Second {
		return c, fmt.Errorf("QUAREL_DISABLED_POLL : durée invalide (ex. 10m)")
	}
	c.PhoneVerify = env("QUAREL_PHONE_VERIFY", "off")
	c.TwilioAccountSID = settings.Get("QUAREL_TWILIO_ACCOUNT_SID")
	c.TwilioAuthToken = settings.Get("QUAREL_TWILIO_AUTH_TOKEN")
	c.TwilioVerifySID = settings.Get("QUAREL_TWILIO_VERIFY_SID")
	c.PhoneWebhookURL = settings.Get("QUAREL_PHONE_WEBHOOK_URL")
	c.PhoneWebhookSecret = settings.Get("QUAREL_PHONE_WEBHOOK_SECRET")
	c.OVHEndpoint = strings.TrimRight(env("QUAREL_OVH_ENDPOINT", "https://eu.api.ovh.com/1.0"), "/")
	c.OVHAppKey = settings.Get("QUAREL_OVH_APP_KEY")
	c.OVHAppSecret = settings.Get("QUAREL_OVH_APP_SECRET")
	c.OVHConsumerKey = settings.Get("QUAREL_OVH_CONSUMER_KEY")
	c.OVHSMSService = settings.Get("QUAREL_OVH_SMS_SERVICE")
	c.OVHSMSSender = settings.Get("QUAREL_OVH_SMS_SENDER")
	switch c.PhoneVerify {
	case "off", "log":
	case "webhook":
		if !strings.HasPrefix(c.PhoneWebhookURL, "https://") && !strings.HasPrefix(c.PhoneWebhookURL, "http://127.") && !strings.HasPrefix(c.PhoneWebhookURL, "http://localhost") {
			return c, fmt.Errorf("SMS par webhook : indiquez l'adresse du webhook en https:// (http:// seulement sur cette machine) (QUAREL_PHONE_WEBHOOK_URL)")
		}
	case "ovh":
		if c.OVHAppKey == "" || c.OVHAppSecret == "" || c.OVHConsumerKey == "" || c.OVHSMSService == "" {
			return c, fmt.Errorf("SMS OVHcloud : application key, application secret, consumer key et service SMS sont obligatoires (QUAREL_OVH_APP_KEY, QUAREL_OVH_APP_SECRET, QUAREL_OVH_CONSUMER_KEY, QUAREL_OVH_SMS_SERVICE)")
		}
	case "twilio":
		if c.TwilioAccountSID == "" || c.TwilioAuthToken == "" || c.TwilioVerifySID == "" {
			return c, fmt.Errorf("SMS Twilio : account SID, auth token et service Verify sont obligatoires (QUAREL_TWILIO_ACCOUNT_SID, QUAREL_TWILIO_AUTH_TOKEN, QUAREL_TWILIO_VERIFY_SID)")
		}
	default:
		return c, fmt.Errorf("envoi des SMS (QUAREL_PHONE_VERIFY) : %q inconnu (off, webhook, ovh, twilio, log)", c.PhoneVerify)
	}
	switch c.Voice {
	case "embedded", "off":
	case "external":
		if c.LiveKitURL == "" || c.LiveKitAPIURL == "" || c.LiveKitKey == "" || c.LiveKitSecret == "" {
			return c, fmt.Errorf("LiveKit externe : les deux adresses, la clé et le secret d'API sont obligatoires (QUAREL_LIVEKIT_URL, QUAREL_LIVEKIT_API_URL, QUAREL_LIVEKIT_KEY, QUAREL_LIVEKIT_SECRET)")
		}
	default:
		return c, fmt.Errorf("salons vocaux (QUAREL_VOICE) : %q inconnu (embedded, external, off)", c.Voice)
	}
	if c.MaxUploadBytes <= 0 {
		return c, fmt.Errorf("taille maximale des fichiers (QUAREL_MAX_UPLOAD_MB) : nombre de Mo positif attendu")
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return c, fmt.Errorf("adresse d'écoute (QUAREL_ADDR) : format « :port » attendu, par exemple :8090")
	}
	for _, iss := range c.TrustedIssuers {
		if strings.HasPrefix(iss, "#") {
			return c, fmt.Errorf("services d'identité acceptés (QUAREL_TRUSTED_ISSUERS) : %q n'est pas un nom de domaine", iss)
		}
	}
	return c, nil
}

func env(key, def string) string {
	if v := settings.Get(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(settings.Get(key)); err == nil {
		return v
	}
	return def
}

func (c Config) trusts(issuer string) bool {
	for _, iss := range c.TrustedIssuers {
		if iss == issuer {
			return true
		}
	}
	return false
}

// issuerBaseURL maps an issuer name to its base URL: plain HTTP for loopback
// hosts (local development), HTTPS otherwise.
func issuerBaseURL(issuer string) string {
	host := issuer
	if h, _, err := net.SplitHostPort(issuer); err == nil {
		host = h
	}
	if host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.") {
		return "http://" + issuer
	}
	return "https://" + issuer
}
