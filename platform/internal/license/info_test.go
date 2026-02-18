package license

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// buildTestJWT creates a JWT string from raw claims (no signature validation needed).
func buildTestJWT(claims map[string]interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	sig := base64.RawURLEncoding.EncodeToString([]byte("fakesig"))
	return fmt.Sprintf("%s.%s.%s", header, payloadB64, sig)
}

// buildSignedJWT creates a JWT string with a valid HMAC-SHA256 signature.
func buildSignedJWT(claims map[string]interface{}, hmacKey string) string {
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	return SignToken(headerB64, payloadB64, hmacKey)
}

func TestDecode_ValidToken(t *testing.T) {
	exp := time.Now().Add(365 * 24 * time.Hour).Unix()
	token := buildTestJWT(map[string]interface{}{
		"tier":       "pro",
		"org_id":     "acme-corp",
		"plugins":    []string{"auth-keycloak", "acl"},
		"seat_limit": 25,
		"exp":        exp,
	})

	info, err := Decode(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !info.Valid {
		t.Error("expected valid")
	}
	if info.Tier != "pro" {
		t.Errorf("expected tier pro, got %s", info.Tier)
	}
	if info.OrgID != "acme-corp" {
		t.Errorf("expected org_id acme-corp, got %s", info.OrgID)
	}
	if len(info.Plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(info.Plugins))
	}
	if info.SeatLimit != 25 {
		t.Errorf("expected seat_limit 25, got %d", info.SeatLimit)
	}
	if info.ExpiresAt == nil {
		t.Error("expected ExpiresAt to be set")
	}
}

func TestDecode_ExpiredToken(t *testing.T) {
	exp := time.Now().Add(-1 * time.Hour).Unix()
	token := buildTestJWT(map[string]interface{}{
		"tier":    "pro",
		"org_id":  "expired-org",
		"plugins": []string{"auth-keycloak"},
		"exp":     exp,
	})

	info, err := Decode(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Valid {
		t.Error("expected invalid for expired token")
	}
	if info.Error != "license expired" {
		t.Errorf("expected error 'license expired', got %q", info.Error)
	}
}

func TestDecode_NoExpiry(t *testing.T) {
	token := buildTestJWT(map[string]interface{}{
		"tier":    "pro",
		"org_id":  "forever-org",
		"plugins": []string{"acl"},
	})

	info, err := Decode(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !info.Valid {
		t.Error("expected valid for token without expiry")
	}
	if info.ExpiresAt != nil {
		t.Error("expected ExpiresAt to be nil")
	}
}

func TestDecode_MalformedToken(t *testing.T) {
	_, err := Decode("not.a.jwt")
	if err == nil {
		t.Error("expected error for malformed token")
	}
}

func TestDecode_InvalidFormat(t *testing.T) {
	_, err := Decode("onlyonepart")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

// --- Signature verification tests ---

func TestVerifySignature_ValidSignature(t *testing.T) {
	token := buildSignedJWT(map[string]interface{}{
		"tier":    "pro",
		"org_id":  "acme-corp",
		"plugins": []string{"auth-keycloak"},
	}, DefaultHMACKey)

	err := VerifySignature(token, DefaultHMACKey)
	if err != nil {
		t.Fatalf("expected valid signature, got error: %v", err)
	}
}

func TestVerifySignature_ValidWithCustomKey(t *testing.T) {
	customKey := "my-secret-pro-key-2026"
	token := buildSignedJWT(map[string]interface{}{
		"tier":    "enterprise",
		"org_id":  "big-corp",
		"plugins": []string{"auth-keycloak", "acl", "cloud-aws"},
	}, customKey)

	err := VerifySignature(token, customKey)
	if err != nil {
		t.Fatalf("expected valid signature with custom key, got error: %v", err)
	}
}

func TestVerifySignature_InvalidSignature(t *testing.T) {
	// Build a token signed with one key, verify with another
	token := buildSignedJWT(map[string]interface{}{
		"tier":   "pro",
		"org_id": "acme-corp",
	}, "correct-key")

	err := VerifySignature(token, "wrong-key")
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestVerifySignature_FakeSignature(t *testing.T) {
	// A JWT with a hand-crafted fake signature (like buildTestJWT produces)
	token := buildTestJWT(map[string]interface{}{
		"tier":   "pro",
		"org_id": "forged-corp",
	})

	err := VerifySignature(token, DefaultHMACKey)
	if err == nil {
		t.Fatal("expected error for forged signature")
	}
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestVerifySignature_MissingSignature(t *testing.T) {
	// Build header.payload with an empty signature part
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]interface{}{"tier": "pro"})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	token := headerB64 + "." + payloadB64 + "."

	err := VerifySignature(token, DefaultHMACKey)
	if err == nil {
		t.Fatal("expected error for missing signature")
	}
	if !errors.Is(err, ErrMissingSignature) {
		t.Errorf("expected ErrMissingSignature, got: %v", err)
	}
}

func TestVerifySignature_InvalidFormat(t *testing.T) {
	err := VerifySignature("onlyonepart", DefaultHMACKey)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestVerifySignature_TamperedPayload(t *testing.T) {
	// Sign a valid token, then tamper with the payload
	token := buildSignedJWT(map[string]interface{}{
		"tier":       "community",
		"org_id":     "honest-org",
		"seat_limit": 1,
	}, DefaultHMACKey)

	parts := strings.SplitN(token, ".", 3)

	// Tamper: change the payload to claim "enterprise" tier
	tampered, _ := json.Marshal(map[string]interface{}{
		"tier":       "enterprise",
		"org_id":     "honest-org",
		"seat_limit": 9999,
	})
	parts[1] = base64.RawURLEncoding.EncodeToString(tampered)
	tamperedToken := parts[0] + "." + parts[1] + "." + parts[2]

	err := VerifySignature(tamperedToken, DefaultHMACKey)
	if err == nil {
		t.Fatal("expected error for tampered payload")
	}
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestVerifySignature_InvalidBase64Signature(t *testing.T) {
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]interface{}{"tier": "pro"})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	// Use characters that are invalid in base64url
	token := headerB64 + "." + payloadB64 + ".!!!invalid-base64!!!"

	err := VerifySignature(token, DefaultHMACKey)
	if err == nil {
		t.Fatal("expected error for invalid base64 signature")
	}
}

// --- VerifyAndDecode tests ---

func TestVerifyAndDecode_ValidToken(t *testing.T) {
	exp := time.Now().Add(365 * 24 * time.Hour).Unix()
	token := buildSignedJWT(map[string]interface{}{
		"tier":       "pro",
		"org_id":     "acme-corp",
		"plugins":    []string{"auth-keycloak", "acl"},
		"seat_limit": 25,
		"exp":        exp,
	}, DefaultHMACKey)

	info, err := VerifyAndDecode(token, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !info.Valid {
		t.Error("expected valid")
	}
	if info.Tier != "pro" {
		t.Errorf("expected tier pro, got %s", info.Tier)
	}
	if info.OrgID != "acme-corp" {
		t.Errorf("expected org_id acme-corp, got %s", info.OrgID)
	}
	if len(info.Plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(info.Plugins))
	}
	if info.SeatLimit != 25 {
		t.Errorf("expected seat_limit 25, got %d", info.SeatLimit)
	}
}

func TestVerifyAndDecode_DefaultKeyUsedWhenEmpty(t *testing.T) {
	token := buildSignedJWT(map[string]interface{}{
		"tier":   "pro",
		"org_id": "acme-corp",
	}, DefaultHMACKey)

	// Empty key should use DefaultHMACKey
	info, err := VerifyAndDecode(token, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Tier != "pro" {
		t.Errorf("expected tier pro, got %s", info.Tier)
	}
}

func TestVerifyAndDecode_RejectsInvalidSignature(t *testing.T) {
	token := buildTestJWT(map[string]interface{}{
		"tier":   "pro",
		"org_id": "forged-corp",
	})

	_, err := VerifyAndDecode(token, DefaultHMACKey)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature in chain, got: %v", err)
	}
}

func TestVerifyAndDecode_ExpiredButValidSignature(t *testing.T) {
	exp := time.Now().Add(-1 * time.Hour).Unix()
	token := buildSignedJWT(map[string]interface{}{
		"tier":   "pro",
		"org_id": "expired-org",
		"exp":    exp,
	}, DefaultHMACKey)

	info, err := VerifyAndDecode(token, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Signature is valid but license is expired
	if info.Valid {
		t.Error("expected invalid for expired token")
	}
	if info.Error != "license expired" {
		t.Errorf("expected 'license expired', got %q", info.Error)
	}
}

func TestVerifyAndDecode_CustomKey(t *testing.T) {
	customKey := "enterprise-secret-key"
	token := buildSignedJWT(map[string]interface{}{
		"tier":   "enterprise",
		"org_id": "big-corp",
	}, customKey)

	info, err := VerifyAndDecode(token, customKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Tier != "enterprise" {
		t.Errorf("expected tier enterprise, got %s", info.Tier)
	}
}

// --- SignToken tests ---

func TestSignToken_ProducesVerifiableToken(t *testing.T) {
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]interface{}{"tier": "pro", "org_id": "test"})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)

	token := SignToken(headerB64, payloadB64, DefaultHMACKey)

	// Must have 3 parts
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}

	// Must verify
	err := VerifySignature(token, DefaultHMACKey)
	if err != nil {
		t.Fatalf("SignToken produced unverifiable token: %v", err)
	}
}

func TestSignToken_DifferentKeysProduceDifferentSignatures(t *testing.T) {
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]interface{}{"tier": "pro"})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)

	token1 := SignToken(headerB64, payloadB64, "key-one")
	token2 := SignToken(headerB64, payloadB64, "key-two")

	parts1 := strings.SplitN(token1, ".", 3)
	parts2 := strings.SplitN(token2, ".", 3)

	if parts1[2] == parts2[2] {
		t.Error("expected different signatures for different keys")
	}
}

// --- Backward compatibility tests ---

func TestDecode_StillWorksWithoutSignatureVerification(t *testing.T) {
	// Decode (without verify) should still accept tokens with fake signatures.
	// This ensures backward compatibility for display-only use cases.
	token := buildTestJWT(map[string]interface{}{
		"tier":   "pro",
		"org_id": "legacy-org",
	})

	info, err := Decode(token)
	if err != nil {
		t.Fatalf("Decode should still work without verification: %v", err)
	}
	if info.Tier != "pro" {
		t.Errorf("expected tier pro, got %s", info.Tier)
	}
}

