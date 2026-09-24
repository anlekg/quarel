// Package tlsconf builds the HTTPS configuration of Quarel services.
//
// Modes (QUAREL_TLS):
//   - off: plain HTTP (development, or behind a reverse proxy doing TLS);
//   - self-signed: certificate generated and bound to the server identity
//     (see pkg/tlsbind) — community servers without a domain name;
//   - acme: free certificate from Let's Encrypt for QUAREL_TLS_DOMAIN, via the
//     TLS-ALPN-01 challenge: the domain must point to this machine and public
//     port 443 must reach the server;
//   - files: certificate and key from QUAREL_TLS_CERT / QUAREL_TLS_KEY.
package tlsconf

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/anlekg/quarel/internal/settings"

	"github.com/anlekg/quarel/pkg/tlsbind"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

const (
	Off        = "off"
	SelfSigned = "self-signed"
	ACME       = "acme"
	Files      = "files"
)

// Config selects how a service serves HTTPS.
type Config struct {
	Mode          string
	Domain        string   // acme
	Email         string   // acme: optional contact for expiry notices
	ACMEDirectory string   // acme: directory URL (default: Let's Encrypt production)
	ACMECAFile    string   // acme: extra root CA for a private ACME server (tests, step-ca…)
	CertFile      string   // files
	KeyFile       string   // files
	Hosts         []string // self-signed: extra names/IPs the certificate covers
}

// FromEnv reads QUAREL_TLS* variables; defMode applies when QUAREL_TLS is unset.
func FromEnv(defMode string) (Config, error) {
	c := Config{
		Mode:          settings.Get("QUAREL_TLS"),
		Domain:        settings.Get("QUAREL_TLS_DOMAIN"),
		Email:         settings.Get("QUAREL_TLS_EMAIL"),
		ACMEDirectory: settings.Get("QUAREL_ACME_DIRECTORY"),
		ACMECAFile:    settings.Get("QUAREL_ACME_CA_FILE"),
		CertFile:      settings.Get("QUAREL_TLS_CERT"),
		KeyFile:       settings.Get("QUAREL_TLS_KEY"),
	}
	if c.Mode == "" {
		c.Mode = defMode
	}
	for _, h := range strings.Split(settings.Get("QUAREL_TLS_HOSTS"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			c.Hosts = append(c.Hosts, h)
		}
	}
	switch c.Mode {
	case Off, SelfSigned:
	case ACME:
		if c.Domain == "" {
			return c, errors.New("Let's Encrypt : indiquez le nom de domaine (QUAREL_TLS_DOMAIN)")
		}
	case Files:
		if c.CertFile == "" || c.KeyFile == "" {
			return c, errors.New("certificat en fichiers : indiquez le certificat et la clé privée (QUAREL_TLS_CERT, QUAREL_TLS_KEY)")
		}
	default:
		return c, fmt.Errorf("certificat (QUAREL_TLS) : %q inconnu (off, self-signed, acme, files)", c.Mode)
	}
	return c, nil
}

// Enabled reports whether the service serves HTTPS.
func (c Config) Enabled() bool { return c.Mode != Off && c.Mode != "" }

// Build returns the server TLS configuration (nil when off). identity is the
// server key used to bind a self-signed certificate; certificates and ACME
// state are kept in dataDir.
func (c Config) Build(dataDir string, identity ed25519.PrivateKey) (*tls.Config, error) {
	switch c.Mode {
	case SelfSigned:
		if identity == nil {
			return nil, errors.New("self-signed TLS needs a server identity key")
		}
		cert, err := tlsbind.LoadOrCreate(dataDir, identity, c.Hosts)
		if err != nil {
			return nil, err
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, nil
	case Files:
		cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, err
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, nil
	case ACME:
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(c.Domain),
			Cache:      autocert.DirCache(filepath.Join(dataDir, "acme")),
			Email:      c.Email,
		}
		if c.ACMEDirectory != "" || c.ACMECAFile != "" {
			m.Client = &acme.Client{DirectoryURL: c.ACMEDirectory}
			if c.ACMECAFile != "" {
				pem, err := os.ReadFile(c.ACMECAFile)
				if err != nil {
					return nil, err
				}
				pool, _ := x509.SystemCertPool()
				if pool == nil {
					pool = x509.NewCertPool()
				}
				if !pool.AppendCertsFromPEM(pem) {
					return nil, fmt.Errorf("%s: no certificate found", c.ACMECAFile)
				}
				m.Client.HTTPClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
			}
		}
		cfg := m.TLSConfig() // answers TLS-ALPN-01 challenges on the same port
		cfg.MinVersion = tls.VersionTLS12
		return cfg, nil
	}
	return nil, nil
}
