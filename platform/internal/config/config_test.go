package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig_CommunityEdition(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "community", cfg.Edition)
	assert.Empty(t, cfg.Plugins)
}

func TestLoad_NoFile_ReturnsDefaults(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, "community", cfg.Edition)
	assert.Empty(t, cfg.Plugins)
}

func TestLoad_ValidConfig_ParsesPlugins(t *testing.T) {
	content := `
edition: pro
plugins:
  auth:
    addr: "auth:50060"
    config:
      issuer: "https://keycloak.example.com"
  sharing:
    addr: "sharing:50061"
`
	path := writeTemp(t, content)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "pro", cfg.Edition)
	assert.Len(t, cfg.Plugins, 2)

	auth := cfg.Plugins["auth"]
	assert.Equal(t, "auth:50060", auth.Addr)
	assert.Equal(t, "https://keycloak.example.com", auth.Config["issuer"])

	sharing := cfg.Plugins["sharing"]
	assert.Equal(t, "sharing:50061", sharing.Addr)
}

func TestLoad_MissingAddr_ReturnsError(t *testing.T) {
	content := `
edition: pro
plugins:
  auth:
    config:
      issuer: "https://keycloak.example.com"
`
	path := writeTemp(t, content)

	_, err := Load(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "auth")
	assert.Contains(t, err.Error(), "addr")
}

func TestLoad_InvalidYAML_ReturnsError(t *testing.T) {
	path := writeTemp(t, "{{not yaml")

	_, err := Load(path)
	assert.Error(t, err)
}

func TestLoad_EmptyPluginConfig_Allowed(t *testing.T) {
	content := `
edition: pro
plugins:
  auth:
    addr: "auth:50060"
`
	path := writeTemp(t, content)

	cfg, err := Load(path)
	require.NoError(t, err)

	auth := cfg.Plugins["auth"]
	assert.Equal(t, "auth:50060", auth.Addr)
	assert.Nil(t, auth.Config)
}

func TestLoad_NoEdition_DefaultsToCommunity(t *testing.T) {
	content := `
plugins:
  auth:
    addr: "auth:50060"
`
	path := writeTemp(t, content)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "community", cfg.Edition)
}

func TestResolvePath_EnvVar_TakesPriority(t *testing.T) {
	tmp := writeTemp(t, "edition: community")
	t.Setenv("RAT_CONFIG", tmp)

	path := ResolvePath()
	assert.Equal(t, tmp, path)
}

func TestResolvePath_NoEnvVar_FallsBackToDefault(t *testing.T) {
	t.Setenv("RAT_CONFIG", "")

	// Create rat.yaml in a temp dir and chdir there
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "rat.yaml")
	os.WriteFile(yamlPath, []byte("edition: community"), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	path := ResolvePath()
	assert.Equal(t, "rat.yaml", path)
}

func TestResolvePath_NoEnvVar_NoFile_ReturnsEmpty(t *testing.T) {
	t.Setenv("RAT_CONFIG", "")

	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	path := ResolvePath()
	assert.Equal(t, "", path)
}

// writeTemp creates a temporary YAML file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()
	return f.Name()
}
