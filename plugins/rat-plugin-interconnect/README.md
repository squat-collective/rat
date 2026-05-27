# rat-plugin-interconnect

An example RAT **platform + portal plugin** — and a *meta-plugin*: it makes
**plugin-to-plugin interconnection** a first-class mechanism.

Plugins normally call each other by hardcoding the other plugin's name and
routes (`/api/v1/x/charts/...`). This plugin replaces that with a **capability
broker**: a plugin registers a named *capability* it offers, and any other
plugin invokes that capability **by name** — the broker routes the call to a
healthy provider. The portal UI draws the live **plugin mesh**.

## Concepts

| Term | Meaning |
|---|---|
| **Capability** | A named service a plugin offers — `{name, provider, method, path}`, e.g. `data.analyze`. |
| **Broker** | `POST /invoke {capability}` finds the providing plugin and forwards the call. The caller never names the provider. |
| **Mesh** | Every plugin ratd knows + every registered capability + the wiring between them. |

## How it works

- **Layer 2** — a Go ConnectRPC plugin. It reads ratd's `/api/v1/plugins` for
  the live plugin list, keeps an in-memory capability registry, and brokers
  `invoke` calls through ratd's route proxy. It only routes to healthy
  providers.
- **Layer 3** — a build-free portal UI bundle (`/x/interconnect`, "Plugin Mesh"
  sidebar item): an SVG graph of plugins and capability wiring, plus a panel to
  register, inspect and invoke capabilities live.

The plugin self-registers one capability (`mesh.describe` → its own `/mesh`),
so the broker is testable the moment it starts.

## API (proxied at `/api/v1/x/interconnect/*`)

| Route | Purpose |
|---|---|
| `GET /mesh` | Plugins + capabilities — the whole picture |
| `GET /capabilities` | List registered capabilities |
| `POST /register` | Register a capability `{name, provider, method, path, description, consumers}` |
| `DELETE /capabilities/{name}` | Remove a capability |
| `POST /invoke` | `{capability, payload}` — the broker routes it to the provider |

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL (plugin list + brokered calls) |
| `GRPC_PORT` | `50093` | port to serve on |
| `PLUGIN_NAME` | `interconnect` | registered plugin name |
| `PLUGIN_ADDR` | `interconnect:50093` | address `ratd` dials back |

## Build & run

```bash
make build
make run
```

Then open the **Plugin Mesh** page in the portal. Verify the broker:

```bash
B=localhost:8080/api/v1/x/interconnect

# register a capability another plugin offers
curl -s -X POST $B/register -H 'Content-Type: application/json' \
  -d '{"name":"notifier.events","provider":"event-notifier","method":"GET","path":"/events"}' | jq

# invoke it by name — the broker routes to event-notifier
curl -s -X POST $B/invoke -H 'Content-Type: application/json' \
  -d '{"capability":"notifier.events"}' | jq
```

## Run tests

```bash
make test
```

## Roadmap

In-memory registry (lost on restart). Natural next steps: capabilities
auto-discovered from each plugin's `Describe`, an inter-plugin event-bus
capability, and Postgres-backed persistence.
