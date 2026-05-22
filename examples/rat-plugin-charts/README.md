# rat-plugin-charts

An example RAT **platform + portal plugin**: a charts, **modular dashboards**
and **reports** service. Build a chart from a SQL query, arrange charts into a
grid dashboard, or compose a narrative report — all from the portal.

Charts are **live**: only the SQL is stored, and it is re-run against `ratd`
every time a chart is viewed, so dashboards and reports always reflect current
data.

It is also a **plugin-interconnection** example — the
[`rat-plugin-ai`](../rat-plugin-ai) assistant calls this plugin's REST API to
turn a conversation into saved charts and dashboards.

## What it does

| Capability | Detail |
|---|---|
| **Charts** | bar / line / area / pie — each backed by a live SQL query |
| **Dashboards** | a modular grid of chart widgets; resize, reorder, add, remove |
| **Reports** | narrative documents interleaving markdown text and live charts |
| **Live data** | `GET /charts/{id}/data` re-runs the query against `ratd` |

## How it works

- **Layer 2** — a Go ConnectRPC plugin implementing `PluginService`. It phones
  home to `ratd` and exposes a REST API (proxied at `/api/v1/x/charts/*`):
  charts, dashboards, reports CRUD, plus `/charts/{id}/data` (live query) and
  `/preview` (ad-hoc SQL for the editor). State is kept in memory.
- **Layer 3** — a portal UI bundle adds a **`/x/charts`** page ("Dashboards"
  sidebar item) with Dashboards / Charts / Reports tabs.

This is the **first example plugin with a real front-end build**: the bundle is
produced by [esbuild](https://esbuild.github.io) bundling
[Recharts](https://recharts.org). React itself is not bundled — plugin
components must share the portal's React instance, so `react` / `react-dom`
imports are rewritten to the portal's `window.React` / `window.ReactDOM`
globals (see `ui/build.mjs`).

## Plugin interconnection

The AI assistant uses this plugin as a service. When `rat-plugin-charts` is
installed, the AI's `render_chart` tool saves each chart here, and a
`save_dashboard` tool assembles them into a dashboard — so *"build me a sales
dashboard"* in the chat produces a real, saved dashboard with a link to it.
If this plugin is not installed the AI still renders charts inline in the chat;
the interconnection is best-effort.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL (phone-home + running chart SQL) |
| `GRPC_PORT` | `50092` | port to serve on |
| `PLUGIN_NAME` | `charts` | registered plugin name |
| `PLUGIN_ADDR` | `charts:50092` | address `ratd` dials back |

## Build & run

```bash
# Build the image (the Dockerfile builds the UI bundle internally).
make build

# Run it on ratd's Docker network.
make run
```

Then open the **Dashboards** page in the portal. Verify the API:

```bash
curl -s localhost:8080/api/v1/plugins | jq '.[] | select(.name=="charts")'

curl -s -X POST localhost:8080/api/v1/x/charts/charts \
  -H 'Content-Type: application/json' \
  -d '{"title":"Orders by customer","type":"bar",
       "sql":"SELECT name, sum(amount) AS total FROM default.bronze.sd_orders GROUP BY name",
       "x_column":"name","y_columns":["total"]}' | jq
```

### Working on the UI

`make build` rebuilds the bundle every time. To regenerate the committed
`bundle.js` after editing anything under `ui/` (so `go test` and your IDE see
the current bundle):

```bash
make ui
```

## Run tests

```bash
make test
```

## Roadmap

In-memory storage (state is lost on restart) and a fixed grid layout. Natural
next steps: Postgres-backed persistence, drag-and-drop dashboard layout, and
report export (PDF).
