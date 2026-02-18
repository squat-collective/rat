package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// HashWebhookToken returns the hex-encoded SHA-256 hash of a webhook token.
// Tokens are stored as hashes so that a database compromise does not leak
// the raw secrets. During webhook authentication the incoming token is
// hashed and compared to the stored hash.
func HashWebhookToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// webhookTokenHashesEqual performs constant-time comparison of two hex-encoded
// token hashes to prevent timing side-channel attacks.
func webhookTokenHashesEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
