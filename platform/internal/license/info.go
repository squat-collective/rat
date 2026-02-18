// Package license provides JWT claim decoding and signature verification.
// Community edition uses HMAC-SHA256 for integrity validation (the key is
// well-known since the code is open source — this protects against accidental
// corruption, not determined attackers). Pro edition can use asymmetric keys
// via plugin enforcement.
package license

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefaultHMACKey is the well-known HMAC key for community edition license
// validation. Since the community edition is open source, this provides
// integrity checking rather than cryptographic secrecy. Pro edition should
// use a secret key or asymmetric signatures via plugins.
const DefaultHMACKey = "rat-community-edition-v2"

// ErrInvalidSignature is returned when the JWT signature does not match.
var ErrInvalidSignature = errors.New("invalid license signature")

// ErrMissingSignature is returned when the JWT has no signature part.
var ErrMissingSignature = errors.New("missing license signature")

// Info represents decoded license claims for display.
type Info struct {
	Valid     bool
	Tier      string
	OrgID     string
	Plugins   []string
	SeatLimit int
	ExpiresAt *time.Time
	Error     string
}

// jwtPayload matches the license JWT claims structure.
type jwtPayload struct {
	Tier      string   `json:"tier"`
	OrgID     string   `json:"org_id"`
	Plugins   []string `json:"plugins"`
	SeatLimit int      `json:"seat_limit"`
	Exp       *int64   `json:"exp"`
}

// Decode extracts claims from a JWT without signature validation.
// For display only — enforcement happens in plugins.
func Decode(tokenStr string) (*Info, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Base64url decode the payload (part 1)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims jwtPayload
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	info := &Info{
		Valid:     true,
		Tier:      claims.Tier,
		OrgID:     claims.OrgID,
		Plugins:   claims.Plugins,
		SeatLimit: claims.SeatLimit,
	}

	if claims.Exp != nil {
		t := time.Unix(*claims.Exp, 0)
		info.ExpiresAt = &t
		if time.Now().After(t) {
			info.Valid = false
			info.Error = "license expired"
		}
	}

	return info, nil
}

// VerifySignature checks the HMAC-SHA256 signature of a JWT token string.
// The signing input is "header.payload" (the first two dot-separated parts),
// and the signature is the third part, base64url-encoded.
// Returns nil if the signature is valid, ErrMissingSignature if the signature
// part is empty, or ErrInvalidSignature if it does not match.
func VerifySignature(tokenStr, hmacKey string) error {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	sigPart := parts[2]
	if sigPart == "" {
		return ErrMissingSignature
	}

	// Decode the provided signature
	providedSig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	// Compute the expected HMAC-SHA256 over "header.payload"
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(hmacKey))
	mac.Write([]byte(signingInput))
	expectedSig := mac.Sum(nil)

	if !hmac.Equal(providedSig, expectedSig) {
		return ErrInvalidSignature
	}

	return nil
}

// VerifyAndDecode validates the HMAC-SHA256 signature and then decodes the
// claims. This is the recommended entry point for license validation — it
// ensures integrity before trusting the payload contents.
// Use hmacKey="" to use DefaultHMACKey.
func VerifyAndDecode(tokenStr, hmacKey string) (*Info, error) {
	if hmacKey == "" {
		hmacKey = DefaultHMACKey
	}

	if err := VerifySignature(tokenStr, hmacKey); err != nil {
		return nil, fmt.Errorf("license verification failed: %w", err)
	}

	return Decode(tokenStr)
}

// SignToken creates an HMAC-SHA256 signature for a JWT token's header and
// payload, returning the complete signed token. This is primarily useful
// for testing and for the license generation tooling.
func SignToken(headerB64, payloadB64, hmacKey string) string {
	signingInput := headerB64 + "." + payloadB64
	mac := hmac.New(sha256.New, []byte(hmacKey))
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + sig
}
