package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCORS(t *testing.T) {
	called := false
	h := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))

	pre := httptest.NewRequest(http.MethodOptions, "/v1/me", nil)
	pre.Header.Set("Origin", "http://localhost:5173")
	pre.Header.Set("Access-Control-Request-Method", "PATCH")
	pre.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, pre)
	if rec.Code != http.StatusNoContent || called {
		t.Fatalf("preflight: status %d, handler called %v", rec.Code, called)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatalf("preflight headers: %v", rec.Header())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/me", nil))
	if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("simple request: status %d, headers %v", rec.Code, rec.Header())
	}
}

// A client sending its body too slowly is cut off; WebSocket-like routes are not.
func TestBodyDeadline(t *testing.T) {
	h := BodyDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}), 200*time.Millisecond, time.Minute, nil, func(r *http.Request) bool { return r.URL.Path == "/stream" })
	srv := httptest.NewServer(h)
	defer srv.Close()
	slow := func(path string) int {
		pr, pw := io.Pipe()
		go func() {
			pw.Write([]byte("a"))
			time.Sleep(500 * time.Millisecond)
			pw.Write([]byte("b"))
			pw.Close()
		}()
		res, err := http.Post(srv.URL+path, "text/plain", pr)
		if err != nil {
			return 0
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := slow("/api"); code == http.StatusOK {
		t.Fatalf("slow body accepted: %d", code)
	}
	if code := slow("/stream"); code != http.StatusOK {
		t.Fatalf("exempt route cut off: %d", code)
	}
}
