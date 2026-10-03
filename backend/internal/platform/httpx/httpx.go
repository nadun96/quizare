// Package httpx holds small HTTP helpers shared by all modules.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Error is an error with an HTTP status and a stable machine-readable code.
type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func NewError(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

var (
	ErrNotFound     = NewError(http.StatusNotFound, "not_found", "resource not found")
	ErrUnauthorized = NewError(http.StatusUnauthorized, "unauthorized", "login required")
	ErrForbidden    = NewError(http.StatusForbidden, "forbidden", "not allowed")
)

func BadRequest(msg string) *Error { return NewError(http.StatusBadRequest, "bad_request", msg) }

func Invalid(fields map[string]string) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "validation failed", Fields: fields}
}

func Conflict(msg string) *Error { return NewError(http.StatusConflict, "conflict", msg) }

// JSON writes v as JSON with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// WriteError renders err; unknown errors become a logged 500 without details.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var he *Error
	if errors.As(err, &he) {
		JSON(w, he.Status, he)
		return
	}
	slog.ErrorContext(r.Context(), "internal error", "err", err, "path", r.URL.Path)
	JSON(w, http.StatusInternalServerError, NewError(500, "internal", "internal server error"))
}

const maxBody = 1 << 20 // 1 MiB; CSV import has its own larger limit

// Decode reads a JSON body into dst, rejecting unknown fields and oversized bodies.
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return BadRequest("invalid JSON body: " + err.Error())
	}
	return nil
}

// Handler adapts an error-returning handler.
type Handler func(w http.ResponseWriter, r *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}

// Recover turns panics into 500s so one bad request cannot crash the process (ADR-01).
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.Error("panic", "err", rec, "path", r.URL.Path, "stack", string(debug.Stack()))
				JSON(w, http.StatusInternalServerError, NewError(500, "internal", "internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets baseline hardening headers (ADR-16).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
