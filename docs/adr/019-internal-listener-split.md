# ADR-019: Internal listener split

## Status: Accepted (2026-05-27)

## Context

Before Wave 1, `ratd` bound a single HTTP listener (default `:8080`)
serving both end-user APIs and service-to-service callbacks. Two of
those callbacks have weak auth by design — the runner posts run-status
updates from inside the docker network, and plugins self-register via
phone-home. The auth middleware can't gate either: the runner has no
bearer token, and a plugin doesn't yet exist in the registry when it
registers itself.

That left both routes reachable from the public listener. A
network-adjacent attacker (SSRF in another container, misconfigured
ingress) could forge run completions or register a hostile plugin
address that ratd would then proxy traffic to.

## Decision

Bind two HTTP listeners in `cmd/ratd/main.go`:

- `LISTEN_ADDR` (default `127.0.0.1:8080`) — end-user APIs only. Auth
  middleware, CORS, the full router.
- `INTERNAL_LISTEN_ADDR` (default `127.0.0.1:8090`) — service-to-service
  callbacks only. Minimal middleware. Mirrored `/health` so a container
  probe pointed at the internal port still works. Never serves end-user
  APIs.

`NewRouter` mounts only public routes; the new `NewInternalRouter` hosts
`/internal/*`. Tests (`internal_listener_test.go`) pin the boundary in
both directions. Implemented in commit `347cbcf`.

## Consequences

**Positive.** The trust boundary is now operationally explicit: the
internal listener MUST be on a private network only the docker compose /
k8s pod network can reach. The two listeners boot and shut under one
`errgroup`, so split-brain is impossible.

**Negative — and important.** The whole guarantee evaporates if an
operator exposes port 8090 publicly. There is no second-line defence on
the internal listener; ratd trusts whatever calls it. The compose file
binds 8090 to `127.0.0.1` on the host as a belt-and-braces measure, but
in k8s it's the operator's job. This ADR exists partly so future
contributors don't "helpfully" expose the internal listener when they
hit a port-forwarding issue.

## Related

- ADR-020 — platform_token (defence-in-depth on the *plugin* side of the
  same docker network).
- [`infra/docker-compose.yml`](../../infra/docker-compose.yml) — the
  reference binding (`127.0.0.1:8090:8090`).
