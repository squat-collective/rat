// Package sdk is the shared helper library for RAT example plugins.
//
// It exists because every example plugin under examples/rat-plugin-*
// grew the same ~150 LOC of boilerplate: a fresh per-startup token, an
// SRI hash over the embedded bundle, a token-auth middleware, a
// phone-home retry loop, the standard env-var fan-out, and a Describe
// builder. That duplication is real tax — any change to the
// X-RAT-Plugin-Token contract or the phone-home payload had to touch
// fourteen identical copies.
//
// Plugins import this package via a local replace directive
// (see go.mod):
//
//	replace github.com/rat-data/rat/sdk-go => ../../sdk-go
package sdk

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
)

// RandomToken returns a freshly generated 32-byte hex token. Used as
// the per-startup platform_token a plugin advertises in Describe(); see
// proto/plugin/v1/plugin.proto for the contract.
//
// Panics if crypto/rand is broken — at that point the host has no
// trustworthy entropy and we cannot safely continue.
func RandomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// SRIHash returns the SHA-256 of b in browser-SRI form
// ("sha256-<base64>"). Used by plugins to compute the bundle_hash of
// their go:embed'd bundle.js at startup so the portal can set
// <script integrity="sha256-…"> and the browser rejects any tampered
// bundle delivered through the ratd reverse proxy.
func SRIHash(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

// TokenAuth wraps next to require X-RAT-Plugin-Token == expected on
// every request. When expected is empty, returns next unwrapped
// (backward-compat: opt-in protection).
//
// Auto-allowlisted paths (no token required):
//   - /health    — liveness probes
//   - /bundle.js — portal SRI bundle fetches (browsers can't add custom
//     headers to <script> tags)
//
// The ConnectRPC plugin-service path is NOT auto-allowlisted; callers
// should attach that handler OUTSIDE the TokenAuth wrap because that
// channel is precisely how ratd LEARNS the token (via Describe).
//
// ratd's reverse proxy injects the token on every forwarded call, so
// browser/CLI/portal traffic via /api/v1/x/{plugin}/* keeps working,
// but a direct peer hit like `curl http://secrets:50099/secrets` from
// another container on the docker network gets 401.
func TokenAuth(expected string, next http.Handler) http.Handler {
	if expected == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bundle.js" || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("X-RAT-Plugin-Token") != expected {
			http.Error(w, "missing or invalid platform token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
