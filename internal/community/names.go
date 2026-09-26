package community

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"strings"

	"github.com/anlekg/quarel/internal/tlsconf"
	"github.com/anlekg/quarel/pkg/idtoken"
)

// hostNames lists the names under which clients may reach this server with
// an ordinary certificate (see checkProof): the public names of the
// configuration (QUAREL_TLS_HOSTS, the Let's Encrypt domain), those of a
// certificate given in files, and this machine's loopback names.
func hostNames(c tlsconf.Config) []string {
	names := []string{"localhost", "127.0.0.1", "::1"}
	names = append(names, c.Hosts...)
	switch c.Mode {
	case tlsconf.ACME:
		names = append(names, c.Domain)
	case tlsconf.Files:
		if cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile); err == nil {
			if leaf, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
				names = append(names, leaf.DNSNames...)
				for _, ip := range leaf.IPAddresses {
					names = append(names, ip.String())
				}
			}
		} else {
			slog.Warn("certificate names unreadable", "err", err)
		}
	}
	out := names[:0]
	for _, n := range names {
		if n = idtoken.NormalizeHost(strings.TrimSpace(n)); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// servesHost reports whether host (as signed by a client) is one of this
// server's names; "*.example.org" names cover one label.
func (s *Server) servesHost(host string) bool {
	host = idtoken.NormalizeHost(host)
	for _, n := range s.names {
		if n == host {
			return true
		}
		if rest, ok := strings.CutPrefix(n, "*."); ok {
			if label, parent, ok := strings.Cut(host, "."); ok && label != "" && parent == rest {
				return true
			}
		}
	}
	return false
}
