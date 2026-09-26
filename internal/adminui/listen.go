package adminui

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The administration page answers plain HTTP and HTTPS on the same port.
// Plain HTTP is only for this machine itself (loopback): anyone else is sent
// to HTTPS, so the admin password never crosses the local network in clear.
// The certificate is self-signed (a browser warning to accept once); its
// fingerprint, shown in the log, lets the host check it.

const (
	certFile = "admin-tls.crt"
	keyFile  = "admin-tls.key"
)

// adminEnv reads a setting of the administration page itself. These are
// environment variables only, never the settings file: the page must not be
// able to widen its own exposure (QUAREL_ADMIN_PUBLIC, QUAREL_ADMIN_TLS).
func adminEnv(key string) string { return os.Getenv(key) }

// plainHTTPAllowed reports whether plain HTTP is accepted from other
// machines: QUAREL_ADMIN_TLS=off, for a page only reachable through a local
// port mapping (Docker on 127.0.0.1) or an SSH tunnel.
func plainHTTPAllowed() bool { return adminEnv("QUAREL_ADMIN_TLS") == "off" }

// loadCertificate returns the page's certificate, created on first use.
func loadCertificate(dir string) (tls.Certificate, string, error) {
	cp, kp := filepath.Join(dir, certFile), filepath.Join(dir, keyFile)
	cert, err := tls.LoadX509KeyPair(cp, kp)
	if err != nil || certExpiring(cert) {
		certPEM, keyPEM, gerr := newCertificate()
		if gerr != nil {
			return tls.Certificate{}, "", gerr
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return tls.Certificate{}, "", err
		}
		if err := os.WriteFile(kp, keyPEM, 0o600); err != nil {
			return tls.Certificate{}, "", err
		}
		if err := os.WriteFile(cp, certPEM, 0o644); err != nil {
			return tls.Certificate{}, "", err
		}
		if cert, err = tls.X509KeyPair(certPEM, keyPEM); err != nil {
			return tls.Certificate{}, "", err
		}
	}
	return cert, Fingerprint(cert.Certificate[0]), nil
}

func certExpiring(c tls.Certificate) bool {
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	return err != nil || time.Until(leaf.NotAfter) < 30*24*time.Hour
}

// Fingerprint formats the SHA-256 of a certificate as browsers show it.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

func newCertificate() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Quarel administration"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(5 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		tmpl.DNSNames = append(tmpl.DNSNames, h)
	}
	for _, a := range LocalAddresses() {
		tmpl.IPAddresses = append(tmpl.IPAddresses, net.ParseIP(a))
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	kd, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), nil
}

// sniffListener hands the HTTP server TLS connections (first byte 0x16, a
// TLS handshake) or plain ones, from the same port. The first byte is read in
// a goroutine per connection, so a silent client cannot hold up the others.
type sniffListener struct {
	net.Listener
	cfg   *tls.Config
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newSniffListener(ln net.Listener, cfg *tls.Config) *sniffListener {
	l := &sniffListener{Listener: ln, cfg: cfg, conns: make(chan net.Conn), done: make(chan struct{})}
	go l.loop()
	return l
}

func (l *sniffListener) loop() {
	for {
		c, err := l.Listener.Accept()
		if errors.Is(err, net.ErrClosed) {
			l.once.Do(func() { close(l.done) })
			return
		}
		if err != nil { // e.g. too many open files: retry shortly, as net/http does
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go l.sniff(c)
	}
}

func (l *sniffListener) sniff(c net.Conn) {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	c.SetReadDeadline(time.Time{})
	if err != nil {
		c.Close()
		return
	}
	var out net.Conn = &peekedConn{Conn: c, r: br}
	if first[0] == 0x16 {
		out = tls.Server(out, l.cfg)
	}
	select {
	case l.conns <- out:
	case <-l.done:
		c.Close()
	}
}

func (l *sniffListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *sniffListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.done) })
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// peekedConn replays the bytes read while sniffing.
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// KeepFiles are the page's own files in the data directory: never archived,
// left in place by a restore (the admin password and the page's certificate).
var KeepFiles = []string{adminFile, certFile, keyFile}
