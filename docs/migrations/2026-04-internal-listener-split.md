# Internal listener moved to a separate port

**Affects:** Operators with custom docker-compose / k8s manifests that
don't yet bind port 8090. Plugins built before the SDK refresh still
work — the internal-URL env var defaults to the public URL when unset.

## What changed

`ratd` now binds two listeners:

| Listener | Address (default) | Purpose |
|---|---|---|
| Public | `127.0.0.1:8080` | All end-user APIs (`/api/v1/*`), portal access |
| Internal | `127.0.0.1:8090` | Service-to-service callbacks: `POST /api/v1/internal/runs/{runID}/status`, `POST /api/v1/internal/plugins/register` |

The internal listener carries no authentication — its security model is
"the network is private" (only other containers on the docker network /
k8s pod network can reach it). Exposing port 8090 publicly is
catastrophic and ratd refuses to start if `INTERNAL_LISTEN_ADDR ==
RAT_LISTEN_ADDR`.

## Why

Pre-split, the runner's "I'm done with run X" callback, the plugin
phone-home, and end-user API calls all hit the same listener. That
meant the runner's callback either had to bypass auth (security hole)
or had to authenticate (operational burden — managing service-account
tokens). The split lets internal callbacks land on a network-isolated
listener with no auth, while end-user calls go through whatever auth
plugin is wired up.

See [ADR-019](../adr/019-internal-listener-split.md) for the full
rationale.

## Upgrade steps

### Docker Compose

```yaml
services:
  ratd:
    ports:
      - "8080:8080"     # public (existing)
      - "127.0.0.1:8090:8090"  # internal — bind to localhost on host
    environment:
      RAT_LISTEN_ADDR: "0.0.0.0:8080"
      INTERNAL_LISTEN_ADDR: "0.0.0.0:8090"
```

Note the host-side bind for 8090 — `127.0.0.1:8090` on the host means
other containers on `infra_default` can reach it (good), but external
network access can't (also good).

### Kubernetes

Two Service objects:

```yaml
apiVersion: v1
kind: Service
metadata: { name: ratd }
spec:
  selector: { app: ratd }
  ports: [{ port: 8080, targetPort: 8080 }]
  type: LoadBalancer
---
apiVersion: v1
kind: Service
metadata: { name: ratd-internal }
spec:
  selector: { app: ratd }
  ports: [{ port: 8090, targetPort: 8090 }]
  type: ClusterIP   # never expose outside the cluster
```

### Plugins

Set `RATD_INTERNAL_URL=http://ratd:8090` on each plugin container.
If left unset, the SDK falls back to `RATD_URL` — fine for dev,
wasteful in production because the phone-home traffic then crosses
the (potentially auth'd) public listener.

## Reverting

If you really need to revert (you don't), unset `INTERNAL_LISTEN_ADDR`
on ratd and the runner / plugins will fall back to the public listener.
This is dev-mode only and should never run in production.

## Source

- Implementation: ADR-019, [`platform/cmd/ratd/main.go`](../../platform/cmd/ratd/main.go)
- Consolidator: commit `b6dfdd3` ("feat(ratd): single
  MountAllInternalRoutes builder + InternalRouterConfig")
