package community

import (
	"net"
	"os"
	"strconv"
	"strings"
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
	VoicePublicIP   string // embedded: "" local addresses, "auto" STUN discovery, or an IP
	LiveKitURL      string // external: URL given to clients (wss://…)
	LiveKitAPIURL   string // external: URL the server uses for the room API (https://…)
	LiveKitKey      string // external: API key
	LiveKitSecret   string // external: API secret
}

// ConfigFromEnv reads the configuration from QUAREL_* environment variables.
func ConfigFromEnv() Config {
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
	c.VoicePublicIP = os.Getenv("QUAREL_VOICE_PUBLIC_IP")
	c.LiveKitURL = os.Getenv("QUAREL_LIVEKIT_URL")
	c.LiveKitAPIURL = os.Getenv("QUAREL_LIVEKIT_API_URL")
	c.LiveKitKey = os.Getenv("QUAREL_LIVEKIT_KEY")
	c.LiveKitSecret = os.Getenv("QUAREL_LIVEKIT_SECRET")
	for _, iss := range strings.Split(env("QUAREL_TRUSTED_ISSUERS", DefaultIssuer), ",") {
		if iss = strings.TrimSpace(iss); iss != "" {
			c.TrustedIssuers = append(c.TrustedIssuers, iss)
		}
	}
	return c
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
