package plugins

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// ProtocolVersion is the version of the plugin protocol that this ratd build supports.
// Plugins report their protocol version in the HealthCheckResponse.message field
// using the format "protocol=N" (e.g., "protocol=1").
//
// Version negotiation rules:
//   - If the plugin doesn't report a version, assume v1 (backward compatible).
//   - If the plugin's major version matches ratd's, proceed normally.
//   - If the plugin's major version is higher, log a warning (plugin is newer).
//   - If the plugin's major version is lower, log a warning (plugin is older).
//
// TODO: When the proto HealthCheckResponse gains a dedicated protocol_version field
// (P10-06 follow-up), parse it directly instead of from the message string.
const ProtocolVersion = 1

// parsePluginVersion extracts a protocol version number from a health check message.
// Expected format: "protocol=N" anywhere in the message string.
// Returns 0 if no version is found (treat as v1 for backward compatibility).
func parsePluginVersion(message string) int {
	for _, part := range strings.Fields(message) {
		if strings.HasPrefix(part, "protocol=") {
			v, err := strconv.Atoi(strings.TrimPrefix(part, "protocol="))
			if err == nil {
				return v
			}
		}
	}
	return 0
}

// negotiateVersion checks the plugin's protocol version against ratd's and logs
// any compatibility issues. Returns an error only if versions are fundamentally
// incompatible (currently: never, since we maintain backward compatibility).
func negotiateVersion(pluginName string, pluginVersion int) error {
	if pluginVersion == 0 {
		// Plugin didn't report a version — assume v1 (legacy plugin).
		slog.Debug("plugin did not report protocol version, assuming v1",
			"plugin", pluginName)
		return nil
	}

	if pluginVersion == ProtocolVersion {
		slog.Debug("plugin protocol version matches",
			"plugin", pluginName, "version", pluginVersion)
		return nil
	}

	if pluginVersion > ProtocolVersion {
		slog.Warn("plugin reports newer protocol version than ratd supports — some features may not work",
			"plugin", pluginName,
			"plugin_version", pluginVersion,
			"ratd_version", ProtocolVersion,
		)
		return nil
	}

	// pluginVersion < ProtocolVersion
	slog.Warn("plugin reports older protocol version — consider upgrading the plugin",
		"plugin", pluginName,
		"plugin_version", pluginVersion,
		"ratd_version", ProtocolVersion,
	)
	return nil
}

// CheckVersionFromHealthMessage extracts the protocol version from a health
// check response message and performs version negotiation.
// This should be called after a successful health check during plugin loading.
func CheckVersionFromHealthMessage(pluginName, healthMessage string) error {
	version := parsePluginVersion(healthMessage)
	if err := negotiateVersion(pluginName, version); err != nil {
		return fmt.Errorf("version negotiation failed for %s: %w", pluginName, err)
	}
	return nil
}
