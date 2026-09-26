package tlsbind

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, cert tls.Certificate) *httptest.Server {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func get(url string, cfg *tls.Config) error {
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
	resp, err := c.Get(url)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

func TestBinding(t *testing.T) {
	_, identity, _ := ed25519.GenerateKey(nil)
	sid := ServerID(identity.Public().(ed25519.PublicKey))
	cert, err := LoadOrCreate(t.TempDir(), identity, []string{"192.168.1.20", "chez-moi.example"})
	if err != nil {
		t.Fatal(err)
	}
	srv := serve(t, cert)
	host := "127.0.0.1"

	v := NewVerifier(host, sid)
	if err := get(srv.URL, v.Config()); err != nil {
		t.Fatalf("expected server: %v", err)
	}
	if mode, seen := v.Mode(); mode != "binding" || seen != sid {
		t.Fatalf("mode after a bound connection: %q %q", mode, seen)
	}
	v = NewVerifier(host, "")
	if err := get(srv.URL, v.Config()); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if _, seen := v.Mode(); seen != sid {
		t.Fatalf("first use: seen %q", seen)
	}
	if err := get(srv.URL, NewVerifier(host, "someoneelse").Config()); err == nil || !strings.Contains(err.Error(), "possible interception") {
		t.Fatalf("wrong server accepted: %v", err)
	}

	// An interceptor with its own key cannot pretend to be the server.
	_, other, _ := ed25519.GenerateKey(nil)
	mitm, _ := LoadOrCreate(t.TempDir(), other, nil)
	if err := get(serve(t, mitm).URL, NewVerifier(host, sid).Config()); err == nil {
		t.Fatal("interceptor accepted")
	}

	// Copying the binding into another certificate breaks its signature.
	certPEM, _, _ := Generate(identity, nil)
	block, _ := pem.Decode(certPEM)
	orig, _ := x509.ParseCertificate(block.Bytes)
	forged, _ := x509.ParseCertificate(mitm.Certificate[0])
	forged.URIs = orig.URIs
	if _, err := verifyLeaf(forged); err == nil {
		t.Fatal("binding copied to another key accepted")
	}

	// A plain self-signed certificate without binding is refused.
	plain := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer plain.Close()
	if err := get(plain.URL, NewVerifier(host, "").Config()); err == nil {
		t.Fatal("unbound self-signed certificate accepted")
	}
}

// A host that proved its binding keeps having to: an ordinary certificate
// from an authority (someone else holding a valid certificate for the same
// name) is refused afterwards. An authority certificate is checked against
// the host dialled, IP addresses included.
func TestVerifierAuthority(t *testing.T) {
	ca := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer ca.Close()
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate())

	v := NewVerifier("127.0.0.1", "")
	v.roots = roots
	if err := get(ca.URL, v.Config()); err != nil {
		t.Fatalf("authority certificate refused: %v", err)
	}
	if mode, _ := v.Mode(); mode != "authority" {
		t.Fatalf("mode %q", mode)
	}
	// httptest's certificate covers 127.0.0.1, not 127.0.0.2.
	other := NewVerifier("127.0.0.2", "")
	other.roots = roots
	if err := get(ca.URL, other.Config()); err == nil {
		t.Fatal("authority certificate accepted for another IP")
	}

	_, identity, _ := ed25519.GenerateKey(nil)
	cert, _ := LoadOrCreate(t.TempDir(), identity, nil)
	bound := serve(t, cert)
	v = NewVerifier("127.0.0.1", "")
	v.roots = roots
	if err := get(bound.URL, v.Config()); err != nil {
		t.Fatal(err)
	}
	if err := get(ca.URL, v.Config()); err == nil || !strings.Contains(err.Error(), "certificat ordinaire") {
		t.Fatalf("authority certificate accepted after a binding: %v", err)
	}
	if mode, sid := v.Mode(); mode != "binding" || sid != ServerID(identity.Public().(ed25519.PublicKey)) {
		t.Fatalf("mode %q %q", mode, sid)
	}

	// Authority first, then a binding: mixed, no mode.
	v = NewVerifier("127.0.0.1", "")
	v.roots = roots
	get(ca.URL, v.Config())
	get(bound.URL, v.Config())
	if mode, _ := v.Mode(); mode != "" {
		t.Fatalf("mixed connections gave mode %q", mode)
	}
}

func TestLoadOrCreateReuses(t *testing.T) {
	_, identity, _ := ed25519.GenerateKey(nil)
	dir := t.TempDir()
	a, _ := LoadOrCreate(dir, identity, []string{"10.0.0.5"})
	b, _ := LoadOrCreate(dir, identity, []string{"10.0.0.5"})
	if string(a.Certificate[0]) != string(b.Certificate[0]) {
		t.Fatal("certificate regenerated needlessly")
	}
	c, _ := LoadOrCreate(dir, identity, []string{"10.0.0.6"})
	if string(a.Certificate[0]) == string(c.Certificate[0]) {
		t.Fatal("certificate not regenerated for a new host")
	}
	_, other, _ := ed25519.GenerateKey(nil)
	d, _ := LoadOrCreate(dir, other, []string{"10.0.0.6"})
	if string(c.Certificate[0]) == string(d.Certificate[0]) {
		t.Fatal("certificate not regenerated for a new identity")
	}
}
