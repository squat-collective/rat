package main

// Encryption key management. AES-256-GCM needs a 32-byte key. Resolved
// in order:
//
//   1. RAT_SECRETS_KEY env var — hex-encoded 32 bytes. Same key across
//      restarts → secrets stay decryptable. Recommended for any non-toy
//      deployment.
//   2. /data/secrets.key — random 32 bytes generated on first run and
//      persisted to the plugin's mounted volume. Survives restarts but
//      loss of the volume = loss of all secrets (same as forgetting any
//      key in a secret store).
//
// If both are absent or the env value is malformed, the plugin refuses
// to start — silently rolling a new key would orphan existing secrets.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const keyBytes = 32 // AES-256

// loadOrCreateKey returns the 32-byte secret key, generating + persisting
// to /data/secrets.key on first run if RAT_SECRETS_KEY isn't set.
func loadOrCreateKey(envValue, fallbackPath string) ([]byte, error) {
	if envValue != "" {
		k, err := hex.DecodeString(envValue)
		if err != nil {
			return nil, fmt.Errorf("RAT_SECRETS_KEY must be hex-encoded: %w", err)
		}
		if len(k) != keyBytes {
			return nil, fmt.Errorf("RAT_SECRETS_KEY must decode to %d bytes (got %d)", keyBytes, len(k))
		}
		return k, nil
	}

	if b, err := os.ReadFile(fallbackPath); err == nil {
		k, err := hex.DecodeString(string(b))
		if err == nil && len(k) == keyBytes {
			return k, nil
		}
		return nil, fmt.Errorf("%s is corrupted (not %d-byte hex)", fallbackPath, keyBytes)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", fallbackPath, err)
	}

	// First-run: generate, persist, return.
	k := make([]byte, keyBytes)
	if _, err := rand.Read(k); err != nil {
		return nil, fmt.Errorf("rand: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(fallbackPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(fallbackPath, []byte(hex.EncodeToString(k)), 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	slog.Warn("generated a fresh secrets encryption key", "path", fallbackPath,
		"hint", "set RAT_SECRETS_KEY to the value in this file for portability across container rebuilds")
	return k, nil
}
