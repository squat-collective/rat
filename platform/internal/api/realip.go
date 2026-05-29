package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// realIPMiddleware resolves the real client IP into r.RemoteAddr.
//
// It replaces chi's middleware.RealIP, which was deprecated in chi 5.3 because
// it trusts X-Forwarded-For / X-Real-IP / True-Client-IP unconditionally and is
// therefore spoofable (GHSA-3fxj-6jh8-hvhx et al.). This version only honors the
// forwarded headers when the *immediate peer* (the real RemoteAddr) is in the
// operator-configured trusted-proxy set; otherwise the direct peer address is
// kept verbatim. With an empty trusted set (the default) forwarded headers are
// never honored, so a client can't forge its rate-limit / audit identity.
//
// Downstream consumers should read the resolved IP via clientIP(r) — i.e. from
// r.RemoteAddr — never from the raw X-Forwarded-For / X-Real-IP headers, which
// remain attacker-controlled.
func realIPMiddleware(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip := realClientIP(r, trusted); ip != "" {
				// Preserve the original peer port so RemoteAddr stays a valid
				// host:port; consumers strip the port anyway.
				_, port, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					port = "0"
				}
				r.RemoteAddr = net.JoinHostPort(ip, port)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// realClientIP returns the client IP for r, honoring forwarded headers only when
// the immediate peer is a trusted proxy. It returns the bare IP (no port), or ""
// if the peer address can't be parsed and there's nothing trustworthy to use.
func realClientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	peer, err := netip.ParseAddr(host)
	if err != nil {
		// Unparseable peer (e.g. a unix socket) — nothing to validate against,
		// so don't trust any headers; hand back whatever host we have.
		return host
	}

	// Only a trusted immediate peer may speak for someone else.
	if !ipInPrefixes(peer, trusted) {
		return peer.String()
	}

	// X-Forwarded-For is "client, proxy1, proxy2" (left = original client).
	// Walk right→left and return the first address that is NOT itself a trusted
	// proxy — that's the closest hop our trusted chain actually received from.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			addr, perr := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if perr != nil {
				continue
			}
			if !ipInPrefixes(addr, trusted) {
				return addr.String()
			}
		}
		// Every hop was trusted → the leftmost entry is the original client.
		if addr, perr := netip.ParseAddr(strings.TrimSpace(parts[0])); perr == nil {
			return addr.String()
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if addr, perr := netip.ParseAddr(xri); perr == nil {
			return addr.String()
		}
	}

	// Trusted peer but no usable forwarded header — use the peer itself.
	return peer.String()
}

// ipInPrefixes reports whether addr falls inside any of the prefixes. IPv4
// addresses tunneled as IPv4-in-IPv6 (::ffff:a.b.c.d) are unmapped first so they
// match plain-IPv4 prefixes.
func ipInPrefixes(addr netip.Addr, prefixes []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// ParseTrustedProxies parses a comma-separated list of trusted-proxy CIDRs and
// bare IPs (e.g. "10.0.0.0/8, 192.168.1.5, ::1") into prefixes. A bare IP becomes
// a /32 (IPv4) or /128 (IPv6). Blank entries are ignored; an invalid entry is a
// hard error so a typo in config can't silently widen or void the trust set.
func ParseTrustedProxies(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, raw := range strings.Split(s, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			p, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, err
			}
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}
