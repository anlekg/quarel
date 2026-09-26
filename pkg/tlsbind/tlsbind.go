// Package tlsbind ties a community server's TLS certificate to its identity.
//
// Home-hosted servers rarely have a domain name, so they use a self-signed
// certificate. To make it trustworthy anyway, the certificate carries a
// "binding": the server's Ed25519 identity key and its signature over the
// certificate's public key, in a subject alternative name URI:
//
//	quarel://binding/<ed25519 public key>/<signature>   (base64url)
//
// The server ID (in invite links) is derived from that identity key, so a
// client that knows the ID verifies, during the TLS handshake, that it talks
// to the right server — without any certificate authority. A certificate
// issued by a public authority (Let's Encrypt…) is accepted the usual way.
package tlsbind

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	context  = "quarel-tls-binding-v1\x00"
	validity = 5 * 365 * 24 * time.Hour
	renewal  = 30 * 24 * time.Hour
)

var b64 = base64.RawURLEncoding

// ServerID derives the public server identifier from its identity key.
func ServerID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:16]))
}

func bindingURI(identity ed25519.PrivateKey, spki []byte) *url.URL {
	sig := ed25519.Sign(identity, append([]byte(context), spki...))
	pub := identity.Public().(ed25519.PublicKey)
	return &url.URL{Scheme: "quarel", Host: "binding", Path: "/" + b64.EncodeToString(pub) + "/" + b64.EncodeToString(sig)}
}

// Generate creates a self-signed certificate (ECDSA P-256) bound to identity,
// valid for hosts (names or IPs) plus localhost.
func Generate(identity ed25519.PrivateKey, hosts []string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	spki, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	sid := ServerID(identity.Public().(ed25519.PublicKey))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Quarel " + sid},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		URIs:         []*url.URL{bindingURI(identity, spki)},
	}
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

// LoadOrCreate returns the self-signed certificate stored in dir, creating
// or renewing it when missing, close to expiry, bound to another identity,
// or missing one of hosts.
func LoadOrCreate(dir string, identity ed25519.PrivateKey, hosts []string) (tls.Certificate, error) {
	certPath, keyPath := filepath.Join(dir, "tls-selfsigned.crt"), filepath.Join(dir, "tls-selfsigned.key")
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil && usable(cert, identity, hosts) {
		return cert, nil
	}
	certPEM, keyPEM, err := Generate(identity, hosts)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

func usable(cert tls.Certificate, identity ed25519.PrivateKey, hosts []string) bool {
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || time.Until(leaf.NotAfter) < renewal {
		return false
	}
	pub, err := verifyLeaf(leaf)
	if err != nil || !pub.Equal(identity.Public()) {
		return false
	}
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h != "" && leaf.VerifyHostname(h) != nil {
			return false
		}
	}
	return true
}

// verifyLeaf checks the binding of a certificate and returns the identity key it names.
func verifyLeaf(leaf *x509.Certificate) (ed25519.PublicKey, error) {
	for _, u := range leaf.URIs {
		if u.Scheme != "quarel" || u.Host != "binding" {
			continue
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 2 {
			return nil, errors.New("tlsbind: malformed binding")
		}
		pub, err1 := b64.DecodeString(parts[0])
		sig, err2 := b64.DecodeString(parts[1])
		if err1 != nil || err2 != nil || len(pub) != ed25519.PublicKeySize {
			return nil, errors.New("tlsbind: malformed binding")
		}
		if !ed25519.Verify(pub, append([]byte(context), leaf.RawSubjectPublicKeyInfo...), sig) {
			return nil, errors.New("tlsbind: binding signature does not match the certificate")
		}
		return ed25519.PublicKey(pub), nil
	}
	return nil, errors.New("tlsbind: certificate has no Quarel binding")
}

// Verify returns the server ID proven by the peer certificate of a TLS connection.
func Verify(cs tls.ConnectionState) (string, error) {
	if len(cs.PeerCertificates) == 0 {
		return "", errors.New("tlsbind: no certificate")
	}
	pub, err := verifyLeaf(cs.PeerCertificates[0])
	if err != nil {
		return "", err
	}
	return ServerID(pub), nil
}

// Verifier checks the certificates of one community server for a client:
// either a certificate valid for host under the system's authorities, or a
// self-signed one whose binding proves the expected server ID (any ID when
// none is expected yet: trust on first use).
//
// Once a connection proved a binding, certificates from authorities are
// refused: a server known to hold its identity key must keep proving it, so
// nobody with an ordinary certificate for the same name can step in. Mode
// tells the application what every connection so far has proven, for the
// login proof (see idtoken.SignProofV2).
type Verifier struct {
	host   string
	mu     sync.Mutex
	expect string
	bound  string         // server ID proven by bindings ("" if none yet)
	ca     bool           // a certificate from an authority was accepted
	roots  *x509.CertPool // nil: the system's authorities (tests set their own)
}

// NewVerifier returns a verifier for connections to host (a name or an IP,
// without port). expectSID may be empty.
func NewVerifier(host, expectSID string) *Verifier {
	return &Verifier{host: strings.Trim(host, "[]"), expect: expectSID}
}

// Config returns the TLS configuration to dial the server with.
func (v *Verifier) Config() *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // verification is done below
		VerifyConnection:   v.verify,
	}
}

func (v *Verifier) verify(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("tlsbind: no certificate")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	inter := x509.NewCertPool()
	for _, c := range cs.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	// The name checked is the host dialled: cs.ServerName is empty for IP addresses.
	if _, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: v.host, Intermediates: inter, Roots: v.roots}); err == nil {
		if v.bound != "" {
			return fmt.Errorf("ce serveur a jusqu'ici prouvé son identité %s par son certificat lié, il présente maintenant un certificat ordinaire : connexion refusée (possible interception)", v.bound)
		}
		v.ca = true
		return nil
	}
	sid, err := Verify(cs)
	if err != nil {
		return fmt.Errorf("certificat non reconnu : ni autorité de confiance, ni lien Quarel valide (%w)", err)
	}
	if want := v.expected(); want != "" && sid != want {
		return fmt.Errorf("ce serveur prouve l'identité %s au lieu de %s attendue : connexion refusée (possible interception)", sid, want)
	}
	v.bound = sid
	return nil
}

func (v *Verifier) expected() string {
	if v.bound != "" {
		return v.bound
	}
	return v.expect
}

// Mode reports how the connections so far were checked: idtoken.TLSBinding
// ("binding") with the proven server ID when every connection presented a
// certificate bound to that ID; "authority" when only authority certificates
// were seen; "" when nothing was seen yet or both kinds were.
func (v *Verifier) Mode() (mode, sid string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case v.bound != "" && !v.ca:
		return "binding", v.bound
	case v.ca && v.bound == "":
		return "authority", ""
	}
	return "", ""
}
