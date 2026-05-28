# rat-plugin-event-notifier

An example RAT **platform plugin** demonstrating **Layer 2** (platform / gRPC)
and **Layer 3** (portal / UI bundle) of the plugin system in one container.

The runner example plugins (`../rat-plugin-*`) cover Layer 1. This one covers
the other two layers — and is the first plugin built to the full v3 contract
(phone-home + `Describe`), so it doubles as the reference for migrating the Pro
plugins.

## What it does

Subscribes to platform events (`run_completed`, `quality_failed`), records them
in a capped in-memory buffer, and — if `WEBHOOK_URL` is set — forwards each to a
webhook. It also injects a widget and a nav item into the portal.

## How it works

**Layer 2 — platform plugin.** A Go [ConnectRPC](https://connectrpc.com) server
implementing `PluginService`:

| RPC | Role |
|---|---|
| `HealthCheck` | ratd polls this; reports `STATUS_SERVING` |
| `Describe` | advertises events, the `/events` route, and the UI descriptor |
| `HandleEvent` | ratd delivers subscribed events here |

`Authenticate` / `Authorize` are left to the embedded `Unimplemented` handler —
a plugin implements only what it needs.

On startup the plugin **phones home**: `POST {RATD_INTERNAL_URL}/internal/plugins/register`
(defaults to `RATD_URL`; ratd's internal listener defaults to `http://ratd:8090`)
with `{name, addr}`. ratd then calls back `HealthCheck` + `Describe` and adds it
to the open registry. It also serves a plain-HTTP `/events` route, which ratd
proxies at `/api/v1/x/event-notifier/events`.

**Layer 3 — portal UI.** `Describe` returns a `ui.bundle_url`. The portal injects
`<script src="/api/v1/plugins/event-notifier/ui/bundle.js">` (ratd reverse-proxies
it from this container). [`bundle.js`](./bundle.js) is hand-written, build-free
JS that self-registers via `window.__RAT_REGISTER_PLUGIN` — a dashboard widget,
a sidebar nav item, and the page it links to.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `GRPC_PORT` | `50090` | port to serve on |
| `PLUGIN_NAME` | `event-notifier` | registered plugin name |
| `PLUGIN_ADDR` | `event-notifier:50090` | address ratd dials back |
| `RATD_URL` | `http://ratd:8080` | ratd base URL for phone-home |
| `WEBHOOK_URL` | _(none)_ | optional webhook; events are logged either way |

## Build & run

```bash
# From this directory — builds the image and runs it on ratd's network.
make build
make run
```

`make build` passes the platform module as a named Docker build context (so the
repo-root `.dockerignore` doesn't exclude it); `make run` starts the container
on `infra_default` so phone-home and ratd's callbacks work.

Within ~2s the plugin registers itself. Verify:

```bash
curl -s localhost:8080/api/v1/plugins | jq '.[] | select(.name=="event-notifier")'
curl -s localhost:8080/api/v1/x/event-notifier/events        # recent events
```

Trigger any pipeline run, then re-check `/events` — the `run_completed` event
appears. Open the portal dashboard to see the widget and the **Events** nav item.

## Run tests

```bash
docker run --rm \
  -v "$(pwd)/plugins/rat-plugin-event-notifier":/work \
  -v "$(pwd)/platform":/platform \
  -w /work golang:1.24-alpine \
  sh -c "go mod tidy && go test ./..."
```
