package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
