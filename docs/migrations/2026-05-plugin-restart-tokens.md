# Plugin restart re-Describes for fresh tokens

**Affects:** Operators relying on the (broken) previous behaviour where
a plugin restart silently produced a 30-second window during which
ratd injected a stale `X-RAT-Plugin-Token` header. No one should have
been relying on that, but it's worth flagging.

## What changed

Pre-Wave-8, the health loop only called `HealthCheck` on each tick. A
plugin that restarted (k8s reschedule, OOM, crash) generated a fresh
`platform_token` in its new `Describe` response, but ratd's stored
token in the registry was never refreshed — only re-Describing the
plugin would do that, and re-Describe only happened on initial
registration or explicit reconnect.

The result: after a plugin restart, every proxied call injected the
stale token; the plugin's `TokenAuth` middleware (constant-time-
compared since Wave 7) rejected it; the only recovery was a `ratd`
restart.

Wave 8's fix: when the health loop transitions a plugin from `error`
back to `enabled`, it now calls `Describe` and refreshes `Token`,
`Version`, `Capabilities`, and `EventTypes` from the response.

## What you'll observe

- Plugin restarts now self-heal within one health-loop tick (30s).
- The `Token` field in the catalog (`GET /api/v1/plugins/<name>`) may
  change after a restart even when the plugin name and address stay
  the same. Don't pin clients to a specific token value across
  plugin restarts.
- Legacy plugins that return `Unimplemented` for `Describe` still
  recover correctly — the fix is best-effort, and the legacy fallback
  leaves the previous token in place rather than blanking it.

## Upgrade steps

None. The fix is transparent — health-loop semantics haven't changed
from the operator's perspective; only the silent failure mode is gone.

## Source

- Fix commit: `2dc7ec4` ("fix(ratd): re-Describe plugins on health
  recovery to refresh stale tokens")
- Regression tests: `platform/internal/plugins/healthloop_test.go` —
  `TestHealthLoop_ReDescribesOnRecovery` and
  `TestHealthLoop_RecoveryWithUnimplementedDescribe`
