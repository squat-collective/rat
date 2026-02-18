// Package config handles loading and validating the rat.yaml configuration.
// Community edition runs with zero config (sensible defaults).
// Pro edition uses rat.yaml to declare plugin containers.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level rat.yaml configuration.
type Config struct {
	Edition string                  `yaml:"edition"`
	Plugins map[string]PluginConfig `yaml:"plugins"`
}

// PluginConfig describes how to connect to a plugin container.
type PluginConfig struct {
	Addr   string            `yaml:"addr"`   // gRPC address, e.g., "auth:50060"
	Config map[string]string `yaml:"config"` // plugin-specific key-value config
}

// DefaultConfig returns the community edition defaults (no plugins).
func DefaultConfig() *Config {
	return &Config{
		Edition: "community",
		Plugins: nil,
	}
}

// Load parses a rat.yaml file and validates it.
// If path is empty, returns community defaults.
func Load(path string) (*Config, error) {
	if path == "" {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	// Default edition to community if not specified.
	if cfg.Edition == "" {
		cfg.Edition = "community"
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ResolvePath finds the config file path.
// Priority: RAT_CONFIG env var > ./rat.yaml > "" (no config).
func ResolvePath() string {
	if p := os.Getenv("RAT_CONFIG"); p != "" {
		return p
	}
	if _, err := os.Stat("rat.yaml"); err == nil {
		return "rat.yaml"
	}
	return ""
}

// validate checks that all plugin configs have required fields.
func (c *Config) validate() error {
	for name, plugin := range c.Plugins {
		if plugin.Addr == "" {
			return fmt.Errorf("plugin %q: addr is required", name)
		}
	}
	return nil
}
