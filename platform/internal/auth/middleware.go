// Package auth provides authentication middleware for the ratd API.
// Community edition uses Noop (pass-through) or APIKey (static key).
// Pro edition plugs in real auth middleware via the auth plugin slot.
package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Noop returns a middleware that passes every request through unchanged.
// This is the default for the Community edition (single user, no auth).
func Noop() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

// APIKey returns a middleware that validates requests against a static API key.
// The key is read from the "Authorization: Bearer <key>" header.
// If the provided key is empty, the middleware behaves like Noop (no auth).
// GET /health is always exempt from authentication to allow health checks.
// Key comparison uses crypto/subtle.ConstantTimeCompare to prevent timing attacks.
func APIKey(key string) func(http.Handler) http.Handler {
	// Empty key means no auth required — behave like Noop.
	if key == "" {
		return Noop()
	}

	keyBytes := []byte(key)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health endpoint is exempt from auth.
			if r.Method == http.MethodGet && r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
				return
			}

			if subtle.ConstantTimeCompare([]byte(token), keyBytes) != 1 {
				http.Error(w, "invalid API key", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractBearerToken extracts the token from "Authorization: Bearer <token>".
func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
