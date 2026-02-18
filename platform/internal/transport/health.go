package transport

import (
	"context"
	"fmt"
	"net"
	"net/url"
)

// TCPHealthChecker implements api.HealthChecker by dialing a TCP address.
// Used for gRPC services (runner, ratq) where a successful TCP connection
// confirms the process is listening. The context deadline controls the timeout.
type TCPHealthChecker struct {
	addr string // host:port to dial
	name string // human-readable service name for error messages
}

// NewTCPHealthChecker creates a health checker that dials the given address.
// The addr can be a URL (http://host:port) or a raw host:port.
func NewTCPHealthChecker(addr, name string) *TCPHealthChecker {
	// Strip scheme if addr is a URL (e.g. "http://runner:50051" → "runner:50051").
	if u, err := url.Parse(addr); err == nil && u.Host != "" {
		addr = u.Host
	}
	return &TCPHealthChecker{addr: addr, name: name}
}

// HealthCheck attempts a TCP connection to the service. Returns nil if reachable.
func (h *TCPHealthChecker) HealthCheck(ctx context.Context) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", h.addr)
	if err != nil {
		return fmt.Errorf("%s unreachable: %w", h.name, err)
	}
	conn.Close()
	return nil
}
