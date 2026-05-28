# `RAT_LISTEN_ADDR` replaces `PORT` (legacy still supported)

**Affects:** Operators setting `PORT=8080` (or similar). The legacy
form still works as a fallback, so this is "should migrate," not "must
migrate."

## What changed

ratd now reads `RAT_LISTEN_ADDR` (the full `host:port` form) instead of
`PORT` (the port number). The legacy `PORT` env var still works when
`RAT_LISTEN_ADDR` is unset, but it can only bind to all interfaces
(`:${PORT}`).

The change happened because the default bind is now `127.0.0.1:8080`
(localhost only) rather than `:8080` (all interfaces). Binding to
all interfaces by default is a footgun — single-host installations
shouldn't accidentally expose ratd to the public internet, and
multi-host ones should be explicit about the network boundary.

## Upgrade steps

### Standard compose

If you're using the bundled `infra/docker-compose.yml`, no action
required — it already sets `RAT_LISTEN_ADDR=0.0.0.0:8080`.

### Custom deployments

Replace:

```yaml
environment:
  PORT: "8080"
```

with:

```yaml
environment:
  RAT_LISTEN_ADDR: "0.0.0.0:8080"   # for network access
  # or
  RAT_LISTEN_ADDR: "127.0.0.1:8080" # for loopback only (default)
```

`RAT_LISTEN_ADDR` is validated with `net.SplitHostPort` at startup —
bad values produce a fast, clear error.

### Public exposure check

If you bind `RAT_LISTEN_ADDR` to a non-loopback address (anything
other than `127.0.0.1` / `localhost`) AND haven't set `RAT_API_KEY` or
wired an auth plugin, ratd logs a security warning on startup. That's
intentional — public ratd with no auth is an open data plane.

## Source

- Implementation in `platform/cmd/ratd/main.go`
- Related: [`docs/migrations/2026-04-internal-listener-split.md`](2026-04-internal-listener-split.md)
  for the matching `INTERNAL_LISTEN_ADDR` rename
