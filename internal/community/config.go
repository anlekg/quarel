package community

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/anlekg/quarel/internal/ratelimit"
	"github.com/anlekg/quarel/internal/tlsconf"
)

// DefaultIssuer is the official Identity service trusted when none is configured.
// It is part of every official identity: never change it.
const DefaultIssuer = "identity.quarel.app"

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
}

// Limits caps request rates (0 disables a limit).
type Limits struct {
	Global   int // requests per client IP per minute, all endpoints
	Auth     int // login challenges and logins per client IP per minute
	Messages int // messages sent per member per 10 seconds
	Uploads  int // attachments uploaded per member per minute
}

// DefaultLimits are the production limits.
func DefaultLimits() Limits { return Limits{Global: 600, Auth: 30, Messages: 10, Uploads: 10} }

// ConfigFromEnv reads the configuration from QUAREL_* environment variables.
func ConfigFromEnv() (Config, error) {
	c := Config{
		Addr:    env("QUAREL_ADDR", ":8090"),
		DataDir: env("QUAREL_DATA_DIR", "./data"),
		Name:    env("QUAREL_SERVER_NAME", "Serveur Quarel"),
	}
	c.Voice = env("QUAREL_VOICE", "embedded")
	c.LiveKitBin = env("QUAREL_LIVEKIT_BIN", "livekit-server")
	c.VoiceSignalPort = envInt("QUAREL_VOICE_SIGNAL_PORT", 7880)
	c.VoiceTCPPort = envInt("QUAREL_VOICE_TCP_PORT", 7881)
	c.VoiceUDPPort = envInt("QUAREL_VOICE_UDP_PORT", 7882)
	c.VoicePublicIP = env("QUAREL_VOICE_PUBLIC_IP", "auto")
	c.LiveKitURL = os.Getenv("QUAREL_LIVEKIT_URL")
	c.LiveKitAPIURL = os.Getenv("QUAREL_LIVEKIT_API_URL")
	c.LiveKitKey = os.Getenv("QUAREL_LIVEKIT_KEY")
	c.LiveKitSecret = os.Getenv("QUAREL_LIVEKIT_SECRET")
	for _, iss := range strings.Split(env("QUAREL_TRUSTED_ISSUERS", DefaultIssuer), ",") {
		if iss = strings.TrimSpace(iss); iss != "" {
			c.TrustedIssuers = append(c.TrustedIssuers, iss)
		}
	}
	c.Limits = DefaultLimits()
	if os.Getenv("QUAREL_RATE_LIMITS") == "off" {
		c.Limits = Limits{}
	}
	var err error
	if c.TrustedProxies, err = ratelimit.ParseProxies(os.Getenv("QUAREL_TRUSTED_PROXIES")); err != nil {
		return c, fmt.Errorf("QUAREL_TRUSTED_PROXIES: %w", err)
	}
	if c.TLS, err = tlsconf.FromEnv(tlsconf.SelfSigned); err != nil {
		return c, err
	}
	c.UPnP = env("QUAREL_UPNP", "on") != "off"
	c.MaxUploadBytes = int64(envInt("QUAREL_MAX_UPLOAD_MB", 25)) << 20
	c.LinkPreviews = env("QUAREL_LINK_PREVIEWS", "on") != "off"
	c.PublicPort = envInt("QUAREL_PUBLIC_PORT", 0)
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
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
