package sdk

import "os"

// PluginEnv is the standard set of env vars every example plugin
// reads at startup. Kept in one struct so the boilerplate stays out
// of every main.go.
type PluginEnv struct {
	// Name is the registered plugin name (PLUGIN_NAME). Defaults to
	// whatever the caller passes to LoadPluginEnv — usually the
	// plugin's canonical short name ("secrets", "diff", "pg-sync").
	Name string

	// Addr is the address ratd dials back over the docker network
	// (PLUGIN_ADDR), formatted "host:port". This is what ratd stores
	// in the registry and uses as the upstream for its reverse proxy.
	Addr string

	// Port is the HTTP/2 port the plugin binds (GRPC_PORT). Named
	// GRPC_PORT for historical reasons — it serves both ConnectRPC
	// and the REST mux multiplexed on the same h2c listener.
	Port string

	// RatdURL is ratd's public API base URL (RATD_URL). Used for the
	// /api/v1/* surface — sharing, capability registration, etc.
	RatdURL string

	// RatdInternalURL is ratd's internal listener base URL
	// (RATD_INTERNAL_URL), exposed on a separate port so the
	// phone-home handshake never crosses the public API. Falls back
	// to RatdURL when unset (single-port dev setups).
	RatdInternalURL string
}

// LoadPluginEnv reads the standard env vars and returns a populated
// PluginEnv. The default* arguments fill any unset variables — pass
// the plugin's canonical short name + its assigned port (50099, 50100,
// etc) + the corresponding "name:port" addr.
//
// RATD_URL defaults to http://ratd:8080 (the compose service name),
// and RATD_INTERNAL_URL falls back to RATD_URL when unset.
func LoadPluginEnv(defaultName, defaultPort, defaultAddr string) PluginEnv {
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	return PluginEnv{
		Name:            envOr("PLUGIN_NAME", defaultName),
		Addr:            envOr("PLUGIN_ADDR", defaultAddr),
		Port:            envOr("GRPC_PORT", defaultPort),
		RatdURL:         ratdURL,
		RatdInternalURL: envOr("RATD_INTERNAL_URL", ratdURL),
	}
}

// envOr returns os.Getenv(key) if set and non-empty, else fallback.
// Internal helper — exported callers should reach for LoadPluginEnv.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
