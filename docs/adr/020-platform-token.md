# ADR-020: Platform token (X-RAT-Plugin-Token)

## Status: Accepted (2026-05-27)

## Context

Plugins run as separate containers on the docker network. ratd reaches
them via a reverse proxy at `/api/v1/x/{plugin}/*`. Before Wave 2 a
plugin's HTTP surface accepted any inbound request — nothing
distinguished "an end-user call ratd forwarded after authenticating the
user" from "a peer container on the docker network calling the plugin
directly." A compromised neighbour could call `secrets/resolve`
straight from the docker network and bypass every auth check ratd
would otherwise enforce.

The classical answer — mTLS between ratd and plugins — would force every
plugin author to manage certs, rotation, and the ConnectRPC client
configuration. Too much friction for a 50-line plugin.

## Decision

Per-startup random shared secret. Each plugin generates a fresh
32-byte hex token in `main()` (via `sdk.RandomToken()`) and advertises it
in `DescribeResponse.platform_token`. ratd reads it from `Describe()`,
stores it on `Plugin.Token`, and the reverse-proxy `Director` strips any
client-supplied `X-RAT-Plugin-Token` before re-injecting ratd's copy.
The plugin's middleware (`sdk.TokenAuth`) rejects mismatching requests.

`/health`, `/bundle.js`, and the ConnectRPC plugin-service paths stay
unauthenticated — for liveness, for the portal's `<script>` tag
(browsers can't add custom headers), and because that's how ratd LEARNS
the token (chicken-and-egg).

Implemented in `d23d52d` (proto + proxy) and `76a8c66` (3-plugin
rollout). Wave 7 fix `1d0dd04` swapped plain `!=` for
`crypto/subtle.ConstantTimeCompare`: a plain comparison is timing-
discriminable, an attacker could reconstruct the 32-byte token in ~2^20
requests, and because all 14 plugins now share `sdk-go`, regressing
that compare would re-open the vulnerability fleet-wide.

## Consequences

**Positive.** Peer-to-peer compromise on the docker network is mitigated
without any cert plumbing. Opt-in: plugins that don't advertise a token
keep working unchanged.

**Negative — stale-token window.** A plugin restart generates a new
token. Until ratd re-fetches `Describe()` (driven by phone-home), the
cached copy is stale and proxied calls 401. The window is small
(phone-home retries on a 2-second tick) but it exists, and that's why
`PhoneHomeLoop` exits the plugin on terminal failure — a plugin ratd
can't see is worse than a plugin that crashes loudly.

## Related

- ADR-019 — internal listener split (closes the platform-side hole that
  motivated this defence-in-depth on the plugin side).
- ADR-021 — `sdk-go` extraction (consolidates `TokenAuth` so the
  constant-time fix lands in one place).
