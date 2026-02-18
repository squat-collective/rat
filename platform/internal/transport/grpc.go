// Package transport provides HTTP client factories for gRPC communication.
// Supports both h2c (cleartext) and TLS transports for ConnectRPC clients.
package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"

	"golang.org/x/net/http2"
)

// TLSConfig holds paths to TLS certificates for gRPC clients.
// If CACertFile is empty, h2c (cleartext HTTP/2) is used instead.
type TLSConfig struct {
	CACertFile string // Path to CA certificate (enables TLS when set)
	CertFile   string // Path to client certificate (for mTLS, optional)
	KeyFile    string // Path to client key (for mTLS, optional)
}

// TLSConfigFromEnv reads TLS config from environment variables.
// Returns a config with empty fields if no env vars are set.
func TLSConfigFromEnv() TLSConfig {
	return TLSConfig{
		CACertFile: os.Getenv("GRPC_TLS_CA"),
		CertFile:   os.Getenv("GRPC_TLS_CERT"),
		KeyFile:    os.Getenv("GRPC_TLS_KEY"),
	}
}

// NewGRPCClient creates an HTTP client for ConnectRPC/gRPC communication.
// If tlsCfg has a CACertFile, uses TLS. Otherwise uses h2c (cleartext HTTP/2).
func NewGRPCClient(tlsCfg TLSConfig) (*http.Client, error) {
	if tlsCfg.CACertFile == "" {
		return newH2CClient(), nil
	}
	return newTLSClient(tlsCfg)
}

// newH2CClient creates an HTTP/2 cleartext client (no TLS).
func newH2CClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}
}

// newTLSClient creates an HTTP/2 client with TLS (and optionally mTLS).
func newTLSClient(cfg TLSConfig) (*http.Client, error) {
	caCert, err := os.ReadFile(cfg.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert %s: %w", cfg.CACertFile, err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA cert %s", cfg.CACertFile)
	}

	tlsConfig := &tls.Config{
		RootCAs:    caPool,
		MinVersion: tls.VersionTLS12,
	}

	// Optional client certificate (mTLS)
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}
