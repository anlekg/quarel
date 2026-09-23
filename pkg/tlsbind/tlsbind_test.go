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

	var seen string
	if err := get(srv.URL, ClientConfig(sid, func(s string) { seen = s })); err != nil || seen != sid {
		t.Fatalf("expected server: err=%v seen=%q", err, seen)
	}
	if err := get(srv.URL, ClientConfig("", func(s string) { seen = s })); err != nil || seen != sid {
		t.Fatalf("first use: err=%v seen=%q", err, seen)
	}
	if err := get(srv.URL, ClientConfig("someoneelse", nil)); err == nil || !strings.Contains(err.Error(), "possible interception") {
		t.Fatalf("wrong server accepted: %v", err)
	}

	// An interceptor with its own key cannot pretend to be the server.
	_, other, _ := ed25519.GenerateKey(nil)
	mitm, _ := LoadOrCreate(t.TempDir(), other, nil)
	if err := get(serve(t, mitm).URL, ClientConfig(sid, nil)); err == nil {
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
	if err := get(plain.URL, ClientConfig("", nil)); err == nil {
		t.Fatal("unbound self-signed certificate accepted")
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
