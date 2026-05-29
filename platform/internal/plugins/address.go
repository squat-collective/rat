// Package plugins — address.go
//
// SSRF guard for plugin registration. When a plugin POSTs its address to
// /internal/plugins/register, ratd will immediately make outbound HTTP calls
// to that address (health check, Describe RPC). Without validation, a hostile
// or compromised registrant could point ratd at internal services and use
// the resulting responses / errors to map the internal network or — worse —
// reach cloud metadata endpoints (AWS IMDS, GCP metadata) for credential
// theft.
//
// The validator rejects:
//   - non-http/https schemes (ftp://, file://, ...)
//   - loopback addresses (127.0.0.0/8, ::1)
//   - link-local addresses (169.254.0.0/16 incl. AWS metadata 169.254.169.254
//     and the fe80::/10 IPv6 range)
//   - multicast (224.0.0.0/4, ff00::/8)
//   - unspecified addresses (0.0.0.0, ::)
//
// It explicitly ALLOWS standard private ranges (10/8, 172.16-31, 192.168) —
// Docker networks live there and plugins MUST be reachable via Docker service
// names or container IPs.
//
// Hostnames are resolved via net.LookupIP and rejected if ANY returned IP
// falls in the deny ranges. This defends against hostile DNS that resolves
// metadata.google.internal → 169.254.169.254.
//
// Loopback override: PLUGIN_ALLOW_LOOPBACK=true lets developers register
// http://localhost:50100 when running a plugin on the host outside the
// Docker network. Default is false (production).
package plugins

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrAddressRejected wraps every failure from ValidateRegistrationAddress so
// callers can use errors.Is to distinguish "bad input from the caller" (400)
// from "ratd failed internally" (500). The wrapping is transparent — the
// chained error from fmt.Errorf still carries the original details.
var ErrAddressRejected = errors.New("plugin address rejected")

// ResolveFunc resolves a hostname to its IP addresses. The default uses
// net.LookupIP; tests inject a mock to avoid real DNS.
type ResolveFunc func(host string) ([]net.IP, error)

// defaultResolve is the production resolver — uses the OS resolver.
func defaultResolve(host string) ([]net.IP, error) {
	return net.LookupIP(host)
}

// ValidateRegistrationAddress checks that addr is a safe plugin address before
// ratd makes outbound calls to it. Returns nil if the address is acceptable,
// or a descriptive error if it points at loopback, link-local, multicast, or
// an unspecified address (any of which would expose ratd to SSRF).
//
// When allowLoopback is true, 127/8, ::1, and hostnames that resolve to those
// addresses are permitted — this is for local-dev workflows only and should
// NEVER be enabled in production.
//
// Hostnames are resolved with net.LookupIP and rejected if ANY of their IPs
// fall in the deny ranges. The resolver can be swapped via the package-level
// hook for testing.
func ValidateRegistrationAddress(addr string, allowLoopback bool) error {
	if err := validateRegistrationAddress(addr, allowLoopback, defaultResolve); err != nil {
		// Wrap so handlers can errors.Is(err, ErrAddressRejected) and pick
		// the right HTTP status (400, not 500). The original reason stays in
		// the chain so error messages remain informative.
		return fmt.Errorf("%w: %v", ErrAddressRejected, err)
	}
	return nil
}

// validateRegistrationAddress is the testable core. Callers in production go
// through ValidateRegistrationAddress; tests pass a mock resolver.
func validateRegistrationAddress(addr string, allowLoopback bool, resolve ResolveFunc) error {
	if strings.TrimSpace(addr) == "" {
		return fmt.Errorf("address is empty")
	}

	// Mirror what EnsureScheme does so the validator is robust to the same
	// inputs the rest of the system accepts: a bare "plugin:50100" gets an
	// http:// prefix. We only prepend when no scheme is present — never when
	// the caller specified something we plan to reject (ftp://, file://) —
	// otherwise we'd accidentally rewrite "ftp://x" into "http://ftp://x".
	probe := addr
	if !hasURLScheme(probe) {
		probe = "http://" + probe
	}

	u, err := url.Parse(probe)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (only http/https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host in address %q", addr)
	}

	// Try to parse the host as a literal IP first (covers v4, v6, and bare-bracket v6).
	if ip := net.ParseIP(host); ip != nil {
		return checkIP(ip, allowLoopback)
	}

	// Hostname path — resolve and reject if ANY IP is unsafe. Hostile DNS
	// (metadata.google.internal → 169.254.169.254) must not slip through.
	ips, err := resolve(host)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve %q: no addresses returned", host)
	}
	for _, ip := range ips {
		if err := checkIP(ip, allowLoopback); err != nil {
			return fmt.Errorf("hostname %q resolves to disallowed address %s: %w", host, ip, err)
		}
	}
	return nil
}

// hasURLScheme reports whether s begins with a "<scheme>://" prefix per the
// URI spec (letter followed by letters/digits/+-.). We use this — not a
// simple "starts with http://" check — so that strings like "ftp://x" are
// recognised as having a scheme (and rejected later by the scheme guard)
// rather than being mangled into "http://ftp://x" by a naive prepend.
func hasURLScheme(s string) bool {
	i := strings.Index(s, "://")
	if i <= 0 {
		return false
	}
	for j := 0; j < i; j++ {
		c := s[j]
		switch j {
		case 0:
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
				return false
			}
		default:
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
				(c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
				return false
			}
		}
	}
	return true
}

// checkIP returns nil if ip is in an allowed range, or an error describing
// why it was rejected. Standard private ranges (RFC1918 + ULA) are allowed
// since Docker networks live in 10/8, 172.16-31, 192.168, and fc00::/7.
func checkIP(ip net.IP, allowLoopback bool) error {
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified address %s not allowed", ip)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("multicast address %s not allowed", ip)
	}
	if ip.IsLoopback() {
		if allowLoopback {
			return nil
		}
		return fmt.Errorf("loopback address %s not allowed (set PLUGIN_ALLOW_LOOPBACK=true for local dev)", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		// Catches 169.254.0.0/16 (AWS IMDS lives at 169.254.169.254) and fe80::/10.
		return fmt.Errorf("link-local address %s not allowed (cloud metadata services live here)", ip)
	}
	return nil
}
