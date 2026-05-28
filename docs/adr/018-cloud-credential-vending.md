# ADR-018: Cloud Credential Vending via Plugin

> **Update (2026-05-28):** RAT is now **100% free and open-source** — there are no Community/Pro editions or license keys. The Community/Pro framing in this record is **historical**; the capabilities it describes now ship as free, optional plugins.

## Status: Accepted (2026-05-27)

## Context

`proto/cloud/v1/cloud.proto` has declared `CloudService.GetCredentials` since
ADR-011 (the cloud-aws plugin), but the platform side never had an HTTP entry
point or wiring code to reach it. A Pro `cloud-aws` (or future `cloud-gcp` /
`cloud-azure`) plugin could phone home and register the capability, but ratd
had no route to invoke it — `runs.go` already attempted to fetch credentials
via `s.Cloud.GetCredentials(...)` but `s.Cloud` was perpetually `nil` because
no code path set it. The plugin contract was therefore incomplete: registration
worked, dispatch did not.

Two alternatives were considered:

1. **Bake credential vending into ratd config.** The operator would supply
   static AWS access keys in `rat.yaml` or environment variables, and ratd
   would assume roles itself. This couples STS/IAM logic to core platform
   code (which must stay open-source and cloud-agnostic), and ties ratd's
   release cadence to AWS SDK changes.
2. **Vend credentials per pipeline run via a plugin RPC.** A Pro plugin owns
   the cloud-specific logic (STS AssumeRole inline policies for AWS, federated
   tokens for GCP, SAS for Azure) and exposes a single `GetCredentials(userID,
   namespace)` call. ratd just asks for credentials when it needs them.

Option 2 keeps the platform clean, lets multiple cloud plugins coexist (one
per provider), and matches how `auth`, `sharing`, `enforcement`, and `executor`
already work — every cloud concern lives behind a plugin capability.

## Decision

A Pro cloud plugin implements `CloudService.GetCredentials(userID, namespace)`
and registers with the `cloud` capability. ratd's plugin manager fires
`OnCloudChanged` when such a plugin appears or disappears; the callback wires
`srv.Cloud` to the registry, which satisfies `api.CloudProvider` by translating
the wire proto to `domain.CloudCredentials`.

The platform exposes a single read endpoint:

```
GET /api/v1/cloud/credentials?namespace=<slug>
→ 200 { access_key, secret_key, session_token, region, expiry }
→ 401 if unauthenticated
→ 400 if namespace is missing or not a valid slug
→ 501 if no cloud plugin is registered (or registered but disabled)
→ 502 if the plugin call fails (unhealthy / upstream error)
```

`domain.CloudCredentials` has `AccessKey`, `SecretKey`, `SessionToken`
(omitted when empty), `Region`, and `Expiry` (`time.Time`). The wire-side
`expires_at` (Unix seconds) is normalised on the boundary. ratd does **not
cache** the response — every fetch goes to the plugin. The plugin is the
right place to add caching (STS responses are typically valid for an hour;
the plugin owns the assume-role call and knows when to refresh).

Credentials are scoped per `(user, namespace)`. The plugin is expected to
issue an inline policy restricting S3 access to the namespace's prefix; ratd
does not enforce scope itself, by design — the trust boundary is the cloud
plugin, not the platform.

## Consequences

**Positive**

- Cleanly separates STS / IAM / federation logic from core platform code.
  Adding GCP or Azure support is "ship another plugin", not "fork ratd".
- Different cloud plugins can coexist behind the same capability slot —
  the registry rejects duplicate capability claims, so the operator chooses
  exactly one provider per deployment.
- The endpoint shape (`domain.CloudCredentials`, JSON, simple slug-validated
  namespace param) is intentionally narrow. Future-proofing (multiple
  buckets, dual-region failover, …) belongs in the plugin's internal model,
  not the public HTTP shape.
- The hot path (`runs.go` injecting `S3Overrides`) already used the
  `s.Cloud` field — this ADR just closes the gap so that field is no
  longer perpetually `nil`.

**Negative**

- One extra RPC per pipeline run when cloud creds are required. Mitigated
  by plugin-side caching; not visible to end users.
- A plugin failure makes runs unrunnable for namespaces that depend on
  cloud creds. We surface that as a 502 from the endpoint and log a warning
  from `runs.go`; the platform falls back to default S3 creds when the
  plugin returns an error so that non-cloud namespaces continue to work.
- The plugin sees raw user IDs and namespace names. That is unavoidable —
  the whole point is that the plugin makes per-tenant authorization
  decisions — but operators should treat the cloud plugin as a sensitive
  trust boundary (network isolation, logging review, etc.).

## Out of scope for this commit

This commit ships:

- the HTTP endpoint (`GET /api/v1/cloud/credentials`),
- the platform-side `api.CloudProvider` interface and its `domain.CloudCredentials`
  type,
- the `plugins.Registry.GetCredentials` translation between the wire proto
  and the domain type,
- the `OnCloudChanged` callback in `plugins.Manager` and the `srv.Cloud`
  wiring in `cmd/ratd/main.go`,
- unit tests covering the four expected HTTP status codes (401, 400, 501,
  200) plus 502 on upstream error and the `omitempty` shape of
  `session_token`.

**It does NOT ship** runner integration. The existing `runs.go` code that
sets `run.S3Overrides` from the cloud plugin is left in place, but those
overrides are not yet propagated through the executor RPC to the runner
container's environment. Wiring `S3Overrides` into the
`runner.SubmitPipeline` request payload (and teaching the runner to honour
it for DuckDB + PyIceberg + boto3) is a separate, follow-up commit. The
endpoint is callable today; the credentials it returns are not yet used by
pipeline execution. This is deliberate: keeping the executor change out of
scope keeps this commit reviewable as "HTTP contract closed".
