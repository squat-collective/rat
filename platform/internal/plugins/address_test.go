package plugins

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// stubResolver builds a ResolveFunc that returns a fixed answer for one host.
// Any other host triggers a test failure — keeps surprises out of the suite.
func stubResolver(t *testing.T, wantHost string, ips []net.IP, err error) ResolveFunc {
	t.Helper()
	return func(host string) ([]net.IP, error) {
		if host != wantHost {
			t.Fatalf("unexpected resolve(%q); want %q", host, wantHost)
		}
		return ips, err
	}
}

// noResolverShouldBeCalled fails the test if the resolver runs — useful for
// literal-IP cases that must never touch DNS.
func noResolverShouldBeCalled(t *testing.T) ResolveFunc {
	t.Helper()
	return func(host string) ([]net.IP, error) {
		t.Fatalf("resolver called for %q but should not have been", host)
		return nil, nil
	}
}

// ── Literal IP — allowed cases ─────────────────────────────────────────────

func TestValidateRegistrationAddress_PrivateIPv4_Allowed(t *testing.T) {
	cases := []string{
		"http://10.0.0.5:50100",
		"http://172.18.0.5:50100", // Docker default network range
		"http://172.31.255.255:50100",
		"http://192.168.1.42:50100",
		"http://8.8.8.8:443", // public — still allowed (deny-list approach)
	}
	for _, addr := range cases {
		t.Run(addr, func(t *testing.T) {
			err := validateRegistrationAddress(addr, false, noResolverShouldBeCalled(t))
			if err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
		})
	}
}

// ── Literal IP — loopback ──────────────────────────────────────────────────

func TestValidateRegistrationAddress_LoopbackIPv4_Rejected(t *testing.T) {
	err := validateRegistrationAddress("http://127.0.0.1:50100", false, noResolverShouldBeCalled(t))
	if err == nil {
		t.Fatal("expected loopback rejection, got nil")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("error should mention loopback; got %v", err)
	}
}

func TestValidateRegistrationAddress_LoopbackIPv6_Rejected(t *testing.T) {
	err := validateRegistrationAddress("http://[::1]:50100", false, noResolverShouldBeCalled(t))
	if err == nil {
		t.Fatal("expected ::1 rejection, got nil")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("error should mention loopback; got %v", err)
	}
}

func TestValidateRegistrationAddress_LoopbackHostname_ResolvesToLoopback_Rejected(t *testing.T) {
	resolve := stubResolver(t, "localhost", []net.IP{net.ParseIP("127.0.0.1")}, nil)
	err := validateRegistrationAddress("http://localhost:50100", false, resolve)
	if err == nil {
		t.Fatal("expected localhost rejection, got nil")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("error should mention loopback; got %v", err)
	}
}

func TestValidateRegistrationAddress_LoopbackOverride_Allowed(t *testing.T) {
	// Override allows literal loopback and hostnames that resolve to it.
	if err := validateRegistrationAddress("http://127.0.0.1:50100", true, noResolverShouldBeCalled(t)); err != nil {
		t.Fatalf("loopback override: %v", err)
	}
	resolve := stubResolver(t, "localhost", []net.IP{net.ParseIP("127.0.0.1")}, nil)
	if err := validateRegistrationAddress("http://localhost:50100", true, resolve); err != nil {
		t.Fatalf("localhost with override: %v", err)
	}
}

// ── Link-local — cloud metadata services ───────────────────────────────────

func TestValidateRegistrationAddress_AWSMetadata_Rejected(t *testing.T) {
	err := validateRegistrationAddress("http://169.254.169.254/latest/meta-data/", false, noResolverShouldBeCalled(t))
	if err == nil {
		t.Fatal("expected AWS metadata rejection, got nil")
	}
	if !strings.Contains(err.Error(), "link-local") {
		t.Errorf("error should mention link-local; got %v", err)
	}
}

func TestValidateRegistrationAddress_LinkLocalIPv6_Rejected(t *testing.T) {
	err := validateRegistrationAddress("http://[fe80::1]:50100", false, noResolverShouldBeCalled(t))
	if err == nil {
		t.Fatal("expected fe80:: rejection, got nil")
	}
	if !strings.Contains(err.Error(), "link-local") {
		t.Errorf("error should mention link-local; got %v", err)
	}
}

// ── IPv4-mapped IPv6 — dual-stack SSRF surface ─────────────────────────────

// TestValidateRegistrationAddress_IPv4MappedIPv6 covers the ::ffff:1.2.3.4
// form. Go's net.IP.IsLoopback / IsLinkLocalUnicast already recognise the
// mapped variants (the methods inspect To4() internally), so the validator
// should reject mapped loopback / link-local without any extra code — but
// without an explicit test we have no assurance, and dual-stack SSRF tricks
// are an increasingly common attack vector worth pinning down.
func TestValidateRegistrationAddress_IPv4MappedIPv6(t *testing.T) {
	cases := []struct {
		name      string
		addr      string
		wantErr   bool
		wantMatch string // substring required in the rejection message
	}{
		{
			name:      "mapped loopback rejected",
			addr:      "http://[::ffff:127.0.0.1]:50100",
			wantErr:   true,
			wantMatch: "loopback",
		},
		{
			name:      "mapped AWS metadata rejected",
			addr:      "http://[::ffff:169.254.169.254]/latest/meta-data/",
			wantErr:   true,
			wantMatch: "link-local",
		},
		{
			name:    "mapped private (Docker-style) allowed",
			addr:    "http://[::ffff:10.0.0.5]:50100",
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRegistrationAddress(tc.addr, false, noResolverShouldBeCalled(t))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected rejection for %s, got nil", tc.addr)
				}
				if tc.wantMatch != "" && !strings.Contains(err.Error(), tc.wantMatch) {
					t.Errorf("error for %s should mention %q; got %v", tc.addr, tc.wantMatch, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected ok for %s, got %v", tc.addr, err)
			}
		})
	}
}

// ── Multicast / unspecified ────────────────────────────────────────────────

func TestValidateRegistrationAddress_MulticastIPv4_Rejected(t *testing.T) {
	err := validateRegistrationAddress("http://224.0.0.1:50100", false, noResolverShouldBeCalled(t))
	if err == nil || !strings.Contains(err.Error(), "multicast") {
		t.Fatalf("expected multicast rejection; got %v", err)
	}
}

func TestValidateRegistrationAddress_UnspecifiedIPv4_Rejected(t *testing.T) {
	err := validateRegistrationAddress("http://0.0.0.0:50100", false, noResolverShouldBeCalled(t))
	if err == nil || !strings.Contains(err.Error(), "unspecified") {
		t.Fatalf("expected unspecified rejection; got %v", err)
	}
}

func TestValidateRegistrationAddress_UnspecifiedIPv6_Rejected(t *testing.T) {
	err := validateRegistrationAddress("http://[::]:50100", false, noResolverShouldBeCalled(t))
	if err == nil || !strings.Contains(err.Error(), "unspecified") {
		t.Fatalf("expected unspecified rejection; got %v", err)
	}
}

// ── Malformed inputs ───────────────────────────────────────────────────────

func TestValidateRegistrationAddress_EmptyString_Rejected(t *testing.T) {
	if err := validateRegistrationAddress("", false, noResolverShouldBeCalled(t)); err == nil {
		t.Fatal("expected empty-string rejection, got nil")
	}
}

func TestValidateRegistrationAddress_OnlyScheme_Rejected(t *testing.T) {
	if err := validateRegistrationAddress("http://", false, noResolverShouldBeCalled(t)); err == nil {
		t.Fatal("expected scheme-only rejection, got nil")
	}
}

func TestValidateRegistrationAddress_DisallowedScheme_Rejected(t *testing.T) {
	// EnsureScheme prepends http:// only when neither http:// nor https://
	// is present, so ftp://... reaches the parser intact and we can reject
	// it explicitly.
	err := validateRegistrationAddress("ftp://blah:21/", false, noResolverShouldBeCalled(t))
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("expected scheme rejection; got %v", err)
	}
}

func TestValidateRegistrationAddress_BareHostname_NoScheme_ResolvedSafely(t *testing.T) {
	// "plugin-name:50100" with no scheme — same shape as the rest of the
	// system accepts (EnsureScheme adds http://). It should resolve and pass.
	resolve := stubResolver(t, "plugin-name", []net.IP{net.ParseIP("172.18.0.7")}, nil)
	if err := validateRegistrationAddress("plugin-name:50100", false, resolve); err != nil {
		t.Fatalf("bare host: %v", err)
	}
}

// ── Hostname resolution ───────────────────────────────────────────────────

func TestValidateRegistrationAddress_DockerServiceName_Allowed(t *testing.T) {
	resolve := stubResolver(t, "my-plugin", []net.IP{net.ParseIP("172.18.0.5")}, nil)
	if err := validateRegistrationAddress("http://my-plugin:50100", false, resolve); err != nil {
		t.Fatalf("docker service name should be ok: %v", err)
	}
}

func TestValidateRegistrationAddress_HostnameWithMixedIPs_RejectsIfAnyIsBad(t *testing.T) {
	// Hostile DNS returns a normal IP AND the AWS metadata IP. We must reject.
	resolve := stubResolver(t, "metadata.google.internal", []net.IP{
		net.ParseIP("10.0.0.7"),
		net.ParseIP("169.254.169.254"),
	}, nil)
	err := validateRegistrationAddress("http://metadata.google.internal/", false, resolve)
	if err == nil {
		t.Fatal("expected rejection when hostname resolves to a mix containing a bad IP")
	}
	if !strings.Contains(err.Error(), "link-local") {
		t.Errorf("error should call out link-local; got %v", err)
	}
}

func TestValidateRegistrationAddress_HostnameResolveError_Rejected(t *testing.T) {
	resolve := stubResolver(t, "no-such-host", nil, fmt.Errorf("nxdomain"))
	err := validateRegistrationAddress("http://no-such-host:50100", false, resolve)
	if err == nil {
		t.Fatal("expected error when DNS lookup fails")
	}
	if !strings.Contains(err.Error(), "resolve") {
		t.Errorf("error should mention resolve; got %v", err)
	}
}

func TestValidateRegistrationAddress_HostnameNoAddresses_Rejected(t *testing.T) {
	resolve := stubResolver(t, "empty-host", nil, nil)
	err := validateRegistrationAddress("http://empty-host:50100", false, resolve)
	if err == nil {
		t.Fatal("expected rejection when DNS returns zero addresses")
	}
}

// ── Public ValidateRegistrationAddress wraps with ErrAddressRejected ───────

func TestValidateRegistrationAddress_PublicAPI_WrapsSentinel(t *testing.T) {
	// The exported function must wrap with ErrAddressRejected so handlers can
	// distinguish "client gave us a bad address" from other Register errors.
	err := ValidateRegistrationAddress("http://127.0.0.1:50100", false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAddressRejected) {
		t.Errorf("expected errors.Is(err, ErrAddressRejected); got %v", err)
	}
}

func TestValidateRegistrationAddress_PublicAPI_OkPassesThrough(t *testing.T) {
	// Public API uses the real resolver — feed it a literal IP so DNS isn't
	// touched.
	if err := ValidateRegistrationAddress("http://10.0.0.5:50100", false); err != nil {
		t.Fatalf("public API rejected a private IP: %v", err)
	}
}

// ── loopbackOverrideFromEnv ────────────────────────────────────────────────

func TestLoopbackOverrideFromEnv(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"false": false,
		"no":    false,
		"0":     false,
		"true":  true,
		"TRUE":  true,
		"1":     true,
		"yes":   true,
		"YES":   true,
		" true ": true, //nolint:gocritic // intentional: verifies surrounding whitespace is trimmed
	}
	for v, want := range cases {
		t.Run("val="+v, func(t *testing.T) {
			t.Setenv("PLUGIN_ALLOW_LOOPBACK", v)
			if got := loopbackOverrideFromEnv(); got != want {
				t.Errorf("PLUGIN_ALLOW_LOOPBACK=%q → %v, want %v", v, got, want)
			}
		})
	}
}
