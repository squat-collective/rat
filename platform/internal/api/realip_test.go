package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustPrefixes(t *testing.T, csv string) []netip.Prefix {
	t.Helper()
	p, err := ParseTrustedProxies(csv)
	require.NoError(t, err)
	return p
}

func TestRealClientIP_UntrustedPeer_IgnoresForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "203.0.113.7:4444" // not in the trusted set
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	r.Header.Set("X-Real-IP", "2.2.2.2")

	got := realClientIP(r, mustPrefixes(t, "10.0.0.0/8"))
	assert.Equal(t, "203.0.113.7", got, "a spoofed XFF from an untrusted peer must be ignored")
}

func TestRealClientIP_EmptyTrustSet_AlwaysUsesPeer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1")

	assert.Equal(t, "10.0.0.5", realClientIP(r, nil),
		"with no trusted proxies configured, headers are never honored")
}

func TestRealClientIP_TrustedPeer_HonorsXRealIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Real-IP", "198.51.100.9")

	assert.Equal(t, "198.51.100.9", realClientIP(r, mustPrefixes(t, "10.0.0.0/8")))
}

func TestRealClientIP_TrustedPeer_XFFReturnsRightmostUntrusted(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:1234"
	// client, then two trusted internal proxies appended on the way in.
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 10.1.2.3, 10.0.0.5")

	assert.Equal(t, "198.51.100.9", realClientIP(r, mustPrefixes(t, "10.0.0.0/8")),
		"should skip trusted hops right-to-left and return the real client")
}

func TestRealClientIP_TrustedPeer_AllHopsTrusted_ReturnsLeftmost(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "10.9.9.9, 10.1.2.3")

	assert.Equal(t, "10.9.9.9", realClientIP(r, mustPrefixes(t, "10.0.0.0/8")))
}

func TestRealClientIP_TrustedPeer_NoForwardedHeader_UsesPeer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:1234"

	assert.Equal(t, "10.0.0.5", realClientIP(r, mustPrefixes(t, "10.0.0.0/8")))
}

func TestRealClientIP_IPv4MappedPeer_MatchesIPv4Prefix(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "[::ffff:10.0.0.5]:1234" // IPv4-in-IPv6
	r.Header.Set("X-Real-IP", "198.51.100.9")

	assert.Equal(t, "198.51.100.9", realClientIP(r, mustPrefixes(t, "10.0.0.0/8")),
		"an IPv4-mapped peer should still match a plain IPv4 trusted prefix")
}

func TestRealClientIP_BareIPTrustEntry(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "192.168.1.5:9999"
	r.Header.Set("X-Real-IP", "198.51.100.9")

	assert.Equal(t, "198.51.100.9", realClientIP(r, mustPrefixes(t, "192.168.1.5")),
		"a bare-IP trust entry (/32) should match exactly")
}

func TestRealIPMiddleware_RewritesRemoteAddr_PreservingPort(t *testing.T) {
	var seen string
	h := realIPMiddleware(mustPrefixes(t, "10.0.0.0/8"))(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seen = r.RemoteAddr
		}),
	)

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:55555"
	r.Header.Set("X-Real-IP", "198.51.100.9")
	h.ServeHTTP(httptest.NewRecorder(), r)

	assert.Equal(t, "198.51.100.9:55555", seen,
		"middleware should rewrite the host but keep the original peer port")
}

func TestParseTrustedProxies(t *testing.T) {
	t.Run("mixed CIDRs, bare IPs, and blanks", func(t *testing.T) {
		got, err := ParseTrustedProxies(" 10.0.0.0/8 , 192.168.1.5 , , ::1 ")
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, "10.0.0.0/8", got[0].String())
		assert.Equal(t, "192.168.1.5/32", got[1].String())
		assert.Equal(t, "::1/128", got[2].String())
	})

	t.Run("empty string yields nothing", func(t *testing.T) {
		got, err := ParseTrustedProxies("")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("invalid entry is a hard error", func(t *testing.T) {
		_, err := ParseTrustedProxies("10.0.0.0/8, not-an-ip")
		assert.Error(t, err)
	})
}
