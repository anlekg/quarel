package community

import (
	"net"
	"os"
	"strings"
)

// DefaultIssuer is the official Identity service trusted when none is configured.
// The final domain is still to be chosen (see PROJECT.md).
const DefaultIssuer = "identity.quarel.app"

// Config holds the community server settings.
type Config struct {
	Addr    string
	DataDir string
	// Name is the server name set on first start; afterwards it is changed through the API.
	Name string
	// TrustedIssuers lists the Identity services whose users may join.
	TrustedIssuers []string
}

// ConfigFromEnv reads the configuration from QUAREL_* environment variables.
func ConfigFromEnv() Config {
	c := Config{
		Addr:    env("QUAREL_ADDR", ":8090"),
		DataDir: env("QUAREL_DATA_DIR", "./data"),
		Name:    env("QUAREL_SERVER_NAME", "Serveur Quarel"),
	}
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
