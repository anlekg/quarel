package adminui

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anlekg/quarel/internal/settings"
)

type fixture struct {
	ui       *UI
	sup      *Supervisor
	srv      *httptest.Server
	c        *http.Client
	restarts atomic.Int32
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("QUAREL_DATA_DIR", dir)
	f := &fixture{sup: NewSupervisor()}
	var err error
	f.ui, err = New(Options{
		Product: "Test", Kind: "community",
		Fields: []Field{{Key: "QUAREL_T_NAME", Label: "Nom", Kind: "text"}, {Key: "QUAREL_T_SECRET", Label: "Secret", Kind: "secret"},
			{Key: "QUAREL_T_LOCKED", Label: "Verrouillé", Kind: "text"}},
		Validate: func() error {
			if settings.Get("QUAREL_T_NAME") == "invalide" {
				return errors.New("nom invalide")
			}
			return nil
		},
		Restart: func() { f.restarts.Add(1); f.sup.Restart() },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.srv = httptest.NewServer(f.ui.Handler())
	t.Cleanup(f.srv.Close)
	jar, _ := cookiejar.New(nil)
	f.c = &http.Client{Jar: jar}
	return f
}

func (f *fixture) do(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
	req.Header.Set("X-Quarel-Admin", "1")
	req.Header.Set("Content-Type", "application/json")
	res, err := f.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestSetupLoginAndProtection(t *testing.T) {
	f := newFixture(t)
	if code, _ := f.do(t, "GET", "/api/status", ""); code != http.StatusUnauthorized {
		t.Fatalf("status before sign-in: %d", code)
	}
	if code, _ := f.do(t, "POST", "/api/setup", `{"password":"court"}`); code != http.StatusBadRequest {
		t.Fatalf("weak password: %d", code)
	}
	// httptest connects over loopback: no setup code needed.
	if code, _ := f.do(t, "POST", "/api/setup", `{"password":"motdepasse-admin"}`); code != http.StatusNoContent {
		t.Fatalf("setup: %d", code)
	}
	if code, _ := f.do(t, "POST", "/api/setup", `{"password":"autre-motdepasse"}`); code != http.StatusConflict {
		t.Fatalf("second setup: %d", code)
	}
	if code, _ := f.do(t, "GET", "/api/status", ""); code != http.StatusOK {
		t.Fatalf("status after setup: %d", code)
	}
	// Writes need the custom header and a same-origin request.
	req, _ := http.NewRequest("POST", f.srv.URL+"/api/restart", nil)
	if res, _ := f.c.Do(req); res.StatusCode != http.StatusForbidden {
		t.Fatalf("write without header: %d", res.StatusCode)
	}
	req.Header.Set("X-Quarel-Admin", "1")
	req.Header.Set("Origin", "http://evil.example")
	if res, _ := f.c.Do(req); res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin write: %d", res.StatusCode)
	}
	// Logout, wrong passwords are throttled.
	f.do(t, "POST", "/api/logout", "")
	for i := 0; i < maxFailures; i++ {
		if code, _ := f.do(t, "POST", "/api/login", `{"password":"mauvais"}`); code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d: %d", i, code)
		}
	}
	if code, _ := f.do(t, "POST", "/api/login", `{"password":"motdepasse-admin"}`); code != http.StatusTooManyRequests {
		t.Fatalf("after %d failures: %d", maxFailures, code)
	}
}

func TestSetupCodeFromAnotherMachine(t *testing.T) {
	f := newFixture(t)
	code := f.ui.SetupCode()
	if len(code) != 10 {
		t.Fatalf("setup code %q", code)
	}
	remote := func(body string) int {
		req := httptest.NewRequest("POST", "https://192.168.1.10:8091/api/setup", strings.NewReader(body))
		req.RemoteAddr, req.TLS = "192.168.1.30:50000", &tls.ConnectionState{}
		req.Header.Set("X-Quarel-Admin", "1")
		rec := httptest.NewRecorder()
		f.ui.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if c := remote(`{"password":"motdepasse-admin"}`); c != http.StatusForbidden {
		t.Fatalf("remote setup without code: %d", c)
	}
	if c := remote(`{"password":"motdepasse-admin","code":"` + strings.ToUpper(code) + `"}`); c != http.StatusNoContent {
		t.Fatalf("remote setup with code: %d", c)
	}
	if f.ui.SetupCode() != "" {
		t.Fatal("setup code must be spent")
	}
}

func TestLocalNetworkOnly(t *testing.T) {
	f := newFixture(t)
	for addr, want := range map[string]int{"192.168.1.30:4000": 200, "10.0.0.2:4000": 200, "[fd00::5]:4000": 200, "203.0.113.9:4000": 403, "[2001:db8::1]:4000": 403} {
		req := httptest.NewRequest("GET", "https://192.168.1.10:8091/api/session", nil)
		req.RemoteAddr, req.TLS = addr, &tls.ConnectionState{}
		rec := httptest.NewRecorder()
		f.ui.Handler().ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", addr, rec.Code, want)
		}
	}
}

func TestSettings(t *testing.T) {
	f := newFixture(t)
	t.Setenv("QUAREL_T_LOCKED", "par-env")
	f.do(t, "POST", "/api/setup", `{"password":"motdepasse-admin"}`)

	if code, _ := f.do(t, "PUT", "/api/settings", `{"values":{"QUAREL_T_NAME":"bon","QUAREL_T_SECRET":"s3cret","QUAREL_T_LOCKED":"ignoré"}}`); code != http.StatusNoContent {
		t.Fatalf("save: %d", code)
	}
	if settings.FromFile("QUAREL_T_NAME") != "bon" || settings.Get("QUAREL_T_LOCKED") != "par-env" || settings.FromFile("QUAREL_T_LOCKED") != "" {
		t.Fatalf("saved values: %v", settings.Snapshot())
	}
	if f.restarts.Load() != 1 {
		t.Fatalf("restarts: %d", f.restarts.Load())
	}
	// Invalid: rolled back, no restart.
	code, body := f.do(t, "PUT", "/api/settings", `{"values":{"QUAREL_T_NAME":"invalide"}}`)
	if code != http.StatusBadRequest || settings.FromFile("QUAREL_T_NAME") != "bon" || f.restarts.Load() != 1 {
		t.Fatalf("invalid: %d %v %v", code, body, settings.Snapshot())
	}
	// Secrets are never sent back; an empty secret keeps the value.
	_, got := f.do(t, "GET", "/api/settings", "")
	for _, raw := range got["fields"].([]any) {
		fv := raw.(map[string]any)
		switch fv["key"] {
		case "QUAREL_T_SECRET":
			if fv["value"] != "" || fv["set"] != true {
				t.Fatalf("secret field: %v", fv)
			}
		case "QUAREL_T_LOCKED":
			if fv["locked"] != true {
				t.Fatalf("locked field: %v", fv)
			}
		}
	}
	f.do(t, "PUT", "/api/settings", `{"values":{"QUAREL_T_SECRET":""}}`)
	if settings.FromFile("QUAREL_T_SECRET") != "s3cret" {
		t.Fatal("empty secret must keep the value")
	}
	if code, _ := f.do(t, "PUT", "/api/settings", `{"values":{"QUAREL_INCONNU":"x"}}`); code != http.StatusBadRequest {
		t.Fatalf("unknown setting: %d", code)
	}
}

func TestSupervisorRestartsAndWaitsForFix(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var runs atomic.Int32
	failing := atomic.Bool{}
	failing.Store(true)
	done := make(chan struct{})
	go func() {
		f.sup.Run(ctx, f.ui, func(ctx context.Context, ready func()) error {
			runs.Add(1)
			if failing.Load() {
				return errors.New("port déjà utilisé")
			}
			ready()
			<-ctx.Done()
			return nil
		})
		close(done)
	}()
	waitState := func(want State) {
		t.Helper()
		for i := 0; i < 200; i++ {
			if s, _, _ := f.ui.current(); s == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		s, e, _ := f.ui.current()
		t.Fatalf("state %s (%s), want %s", s, e, want)
	}
	waitState(Failed)
	if _, e, _ := f.ui.current(); e != "port déjà utilisé" {
		t.Fatalf("error %q", e)
	}
	failing.Store(false)
	f.sup.Restart()
	waitState(Running)
	n := runs.Load()
	f.sup.Restart()
	for i := 0; i < 200 && runs.Load() == n; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	waitState(Running)
	resume := f.sup.Pause()
	waitState(Stopped)
	resume()
	waitState(Running)
	cancel()
	<-done
}

// A web page pointing its own domain at this machine (DNS rebinding) must
// not reach the page; other machines are sent to HTTPS; a request relayed by
// a local proxy does not get the privileges of this machine.
func TestHostAndTransport(t *testing.T) {
	f := newFixture(t)
	h := f.ui.Handler()
	serve := func(method, host, remote string, tlsOn bool, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://"+host+"/api/session", strings.NewReader(`{"password":"motdepasse-admin"}`))
		req.RemoteAddr = remote
		req.Header.Set("X-Quarel-Admin", "1")
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		if tlsOn {
			req.TLS = &tls.ConnectionState{}
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := serve("GET", "attacker.example:8091", "127.0.0.1:5000", false, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("rebinding host: %d", rec.Code)
	}
	for _, host := range []string{"127.0.0.1:8091", "localhost:8091", "[::1]:8091", "192.168.1.10:8091"} {
		if rec := serve("GET", host, "127.0.0.1:5000", false, nil); rec.Code != http.StatusOK {
			t.Fatalf("host %s: %d", host, rec.Code)
		}
	}
	rec := serve("GET", "192.168.1.10:8091", "192.168.1.20:5000", false, nil)
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "https://192.168.1.10:8091/api/session" {
		t.Fatalf("plain HTTP from the network: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := serve("POST", "192.168.1.10:8091", "192.168.1.20:5000", false, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("plain POST from the network: %d", rec.Code)
	}
	if rec := serve("GET", "192.168.1.10:8091", "192.168.1.20:5000", true, nil); rec.Code != http.StatusOK {
		t.Fatalf("HTTPS from the network: %d", rec.Code)
	}
	// Setup through a local proxy needs the code, like any other machine.
	req := httptest.NewRequest("POST", "http://127.0.0.1:8091/api/setup", strings.NewReader(`{"password":"motdepasse-admin"}`))
	req.RemoteAddr, req.TLS = "127.0.0.1:5000", &tls.ConnectionState{}
	req.Header.Set("X-Quarel-Admin", "1")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "invalid_setup_code") {
		t.Fatalf("setup through a proxy without code: %d %s", rec.Code, rec.Body.String())
	}
}

// One port serves the page over plain HTTP and HTTPS.
func TestSniffListener(t *testing.T) {
	cert, fp, err := loadCertificate(t.TempDir())
	if err != nil || len(fp) != 95 {
		t.Fatalf("certificate: %v %q", err, fp)
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := newSniffListener(raw, &tls.Config{Certificates: []tls.Certificate{cert}})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			w.Write([]byte("tls"))
		} else {
			w.Write([]byte("plain"))
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()
	// A silent client does not hold up the others.
	silent, _ := net.Dial("tcp", raw.Addr().String())
	defer silent.Close()
	get := func(url string, c *http.Client) string {
		res, err := c.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	if got := get("http://"+raw.Addr().String(), http.DefaultClient); got != "plain" {
		t.Fatalf("plain: %q", got)
	}
	insecure := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	if got := get("https://"+raw.Addr().String(), insecure); got != "tls" {
		t.Fatalf("tls: %q", got)
	}
}
