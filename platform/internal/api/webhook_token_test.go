package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/stretchr/testify/assert"
)

func TestHashWebhookToken_Deterministic(t *testing.T) {
	token := "abc123deadbeef"
	h1 := api.HashWebhookToken(token)
	h2 := api.HashWebhookToken(token)
	assert.Equal(t, h1, h2, "hashing the same token twice should produce identical output")
}

func TestHashWebhookToken_MatchesRawSHA256(t *testing.T) {
	token := "test-token-value"
	sum := sha256.Sum256([]byte(token))
	expected := hex.EncodeToString(sum[:])
	assert.Equal(t, expected, api.HashWebhookToken(token))
}

func TestHashWebhookToken_DifferentTokens_DifferentHashes(t *testing.T) {
	h1 := api.HashWebhookToken("token-a")
	h2 := api.HashWebhookToken("token-b")
	assert.NotEqual(t, h1, h2)
}

func TestHashWebhookToken_EmptyString(t *testing.T) {
	h := api.HashWebhookToken("")
	sum := sha256.Sum256([]byte(""))
	expected := hex.EncodeToString(sum[:])
	assert.Equal(t, expected, h, "empty string should still produce a valid SHA-256 hash")
}

func TestHashWebhookToken_OutputLength(t *testing.T) {
	h := api.HashWebhookToken("any-token")
	assert.Len(t, h, 64, "SHA-256 hex digest should be 64 characters")
}
