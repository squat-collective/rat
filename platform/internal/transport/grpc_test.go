package transport

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGRPCClient_NoCACert_ReturnsH2CClient(t *testing.T) {
	client, err := NewGRPCClient(TLSConfig{})
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewGRPCClient_InvalidCACert_ReturnsError(t *testing.T) {
	_, err := NewGRPCClient(TLSConfig{CACertFile: "/nonexistent/ca.pem"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read CA cert")
}

func TestTLSConfigFromEnv_ReadsEnvVars(t *testing.T) {
	t.Setenv("GRPC_TLS_CA", "/path/to/ca.pem")
	t.Setenv("GRPC_TLS_CERT", "/path/to/cert.pem")
	t.Setenv("GRPC_TLS_KEY", "/path/to/key.pem")

	cfg := TLSConfigFromEnv()
	assert.Equal(t, "/path/to/ca.pem", cfg.CACertFile)
	assert.Equal(t, "/path/to/cert.pem", cfg.CertFile)
	assert.Equal(t, "/path/to/key.pem", cfg.KeyFile)
}

func TestTLSConfigFromEnv_EmptyWhenNoEnvVars(t *testing.T) {
	os.Unsetenv("GRPC_TLS_CA")
	os.Unsetenv("GRPC_TLS_CERT")
	os.Unsetenv("GRPC_TLS_KEY")

	cfg := TLSConfigFromEnv()
	assert.Empty(t, cfg.CACertFile)
	assert.Empty(t, cfg.CertFile)
	assert.Empty(t, cfg.KeyFile)
}
