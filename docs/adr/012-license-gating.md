# ADR-012: License Gating for Pro Plugins

## Status: Accepted

## Context

RAT Pro plugins (auth-keycloak, executor-container, acl, cloud-aws) are closed-source Go containers distributed via ghcr.io. Without license validation, anyone with access to the container images can run them freely. We need a mechanism to:

1. Gate plugin execution behind a valid license
2. Support per-plugin entitlements (one license key, multiple plugins)
3. Work offline (no phone-home to a license server)
4. Keep the community repo free of cryptographic dependencies

## Decision

### Signed JWT license keys with RSA-256

- **Format**: Standard JWT signed with RSA-256 (RS256)
- **Claims**: `tier`, `plugins[]`, `org_id`, `seat_limit`, standard JWT `exp`/`iat`/`iss`
- **Public key**: Embedded in `pkg/license/keys.go` — shipped with every plugin binary
- **Private key**: Stored securely, used only by internal `rat-license` CLI tool

### Enforcement architecture

- **Plugins validate**: Each plugin reads `RAT_LICENSE_KEY` env var at startup, validates JWT signature, checks `plugins[]` claim
- **ratd decodes only**: Community ratd decodes JWT payload (base64, no crypto) for display. No signature validation. No crypto deps in community code.
- **Health check gating**: Unlicensed plugins return `STATUS_NOT_SERVING` with a descriptive message. ratd's existing health check mechanism gracefully disables them.
- **Single env var**: `RAT_LICENSE_KEY` is shared across all plugins via compose environment

### Key design choices

1. **Validation at startup only** — no per-request overhead. Plugin continues serving if license expires mid-run (until restart).
2. **No proto changes** — `HealthCheckResponse.Message` already carries NOT_SERVING reasons.
3. **`pkg/license/` as a separate Go module** — imported by all plugins via `replace` directive, matching existing `platform` module pattern.
4. **RSA-256 over HMAC** — public key can be safely embedded in distributed binaries; only the private key can sign new licenses.

## Consequences

### Positive

- Zero network dependency — fully offline validation
- Zero runtime overhead — validated once at startup
- Graceful degradation — community edition works perfectly without any license
- Existing health check protocol handles all signaling (no new RPCs or proto changes)
- Single `RAT_LICENSE_KEY` env var simplifies customer setup

### Negative

- Key rotation requires rebuilding all plugin images (embedded public key)
- No revocation mechanism (would require phone-home, conflicts with offline-first design)
- License checked only at startup — expired licenses continue serving until plugin restart

### Neutral

- Determined attackers can patch the binary — acceptable trade-off for the target market (honest self-hosters, not enterprise lock-in)
