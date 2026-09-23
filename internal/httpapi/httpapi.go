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

// WriteErr sends err as a JSON error; unexpected errors are logged and hidden.
func WriteErr(w http.ResponseWriter, r *http.Request, err error) {
	var ae *Error
	if !errors.As(err, &ae) {
		slog.Error("internal error", "method", r.Method, "path", r.URL.Path, "err", err)
		ae = Errf(http.StatusInternalServerError, "internal", "internal server error")
	}
	if ae.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(ae.RetryAfter, 10))
	}
	WriteJSON(w, ae.Status, map[string]*Error{"error": ae})
}

// MaxBody is the largest accepted JSON request body.
const MaxBody = 64 << 10

// Decode reads a JSON body into v, rejecting unknown fields.
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, MaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Errf(http.StatusBadRequest, "bad_request", "invalid JSON body: %v", err)
	}
	return nil
}
