package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for backward-compatible constants in loader.go.
// Comprehensive Registry tests are in registry_test.go.

func TestPluginConstants_MatchCapabilities(t *testing.T) {
	assert.Equal(t, CapAuth, PluginAuth)
	assert.Equal(t, CapExecutor, PluginExecutor)
	assert.Equal(t, CapSharing, PluginSharing)
	assert.Equal(t, CapEnforcement, PluginEnforcement)
	assert.Equal(t, CapCloud, PluginCloud)
}
