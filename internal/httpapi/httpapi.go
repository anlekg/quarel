// Package httpapi holds the JSON conventions shared by Quarel HTTP services.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Error is an API error, sent as {"error": {...}}.
type Error struct {
	Status     int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int64  `json:"retry_after,omitempty"` // seconds, for 429 responses
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Errf builds an Error.
func Errf(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// IsCode reports whether err is an *Error with the given code.
func IsCode(err error, code string) bool {
	var ae *Error
	return errors.As(err, &ae) && ae.Code == code
}

// WriteJSON sends v as JSON with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// redactPath hides secrets carried in paths (incoming webhook tokens, "qw_…").
func redactPath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if strings.HasPrefix(seg, "qw_") {
			parts[i] = "…"
		}
	}
	return strings.Join(parts, "/")
}

// WriteErr sends err as a JSON error; unexpected errors are logged and hidden.
func WriteErr(w http.ResponseWriter, r *http.Request, err error) {
	var ae *Error
	if !errors.As(err, &ae) {
		slog.Error("internal error", "method", r.Method, "path", redactPath(r.URL.Path), "err", err)
		ae = Errf(http.StatusInternalServerError, "internal", "internal server error")
	}
	if ae.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(ae.RetryAfter, 10))
	}
	WriteJSON(w, ae.Status, map[string]*Error{"error": ae})
}

// MaxBody is the largest accepted JSON request body.
const MaxBody = 64 << 10

// Decode reads a JSON body (at most MaxBody bytes) into v, rejecting unknown fields.
func Decode(r *http.Request, v any) error { return DecodeLimit(r, v, MaxBody) }

// DecodeLimit is Decode with a custom size limit.
func DecodeLimit(r *http.Request, v any, limit int64) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Errf(http.StatusBadRequest, "bad_request", "invalid JSON body: %v", err)
	}
	return nil
}

// CORS lets browser clients (the desktop app's web UI, the web client) call the
// API from any origin. Safe because authentication is a bearer token sent
// explicitly, never a cookie: another site gains nothing it could not already
// do with its own requests. Preflight requests are answered directly.
func CORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Access-Control-Allow-Origin", "*")
		hd.Set("Access-Control-Expose-Headers", "Retry-After")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			hd.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			hd.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			hd.Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// BodyDeadline bounds the time a client has to send its request body, so
// that one sending it a byte at a time cannot hold a connection (and a
// goroutine) forever: normal for most requests, slow for those matching
// isSlow (file uploads). Requests matching skip (WebSocket upgrades,
// proxied streams) keep no deadline.
func BodyDeadline(h http.Handler, normal, slow time.Duration, isSlow, skip func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skip == nil || !skip(r) {
			d := normal
			if isSlow != nil && isSlow(r) {
				d = slow
			}
			http.NewResponseController(w).SetReadDeadline(time.Now().Add(d))
		}
		h.ServeHTTP(w, r)
	})
}
