# rat-plugin-charts

An example RAT **platform + portal plugin**: **living dashboards**. A dashboard
is a drag-and-drop grid of components — charts, headings, markdown, metrics and
AI-written insights — that scrolls vertically, so it doubles as a report.

It is also a **plugin-interconnection** example, in both directions:

- the [`rat-plugin-ai`](../rat-plugin-ai) chat draws its graphs with **this
  plugin's chart renderer**, and chat graphs can be **pinned onto a dashboard**;
- the **AI-analysis component** calls the AI plugin to write an insight about a
  chart on the dashboard.

## Component types

| Component | What it is |
|---|---|
| **Chart** | a graph — bar / line / area / pie / radar, a live SQL query, full styling |
| **Metric** | a single big KPI number from a query |
| **AI analysis** | an AI-written insight about another component; refreshable |
| **Heading** | a section title |
| **Markdown** | a rich text block |

Chart and metric components are **live** — their SQL re-runs against `ratd`
every time the dashboard is viewed (or you hit Refresh). There is no global
chart catalogue: a chart's full config lives inside its component.

## How it works

- **Layer 2** — a Go ConnectRPC plugin implementing `PluginService`. It phones
  home to `ratd` and exposes a REST API (proxied at `/api/v1/x/charts/*`):
  dashboards CRUD, `POST /dashboards/{id}/components` (used by the chat's
  "pin to dashboard"), and `POST /query` (live data + editor previews). State
  is kept in memory.
- **Layer 3** — a portal UI bundle adds the **`/x/charts`** page. The dashboard
  grid is [react-grid-layout](https://github.com/react-grid-layout/react-grid-layout)
  (drag, drop, resize); charts are drawn with [Recharts](https://recharts.org).
  The bundle is built by [esbuild](https://esbuild.github.io) — `react` /
  `react-dom` are rewritten to the portal's `window` globals (see
  `ui/build.mjs`).

The bundle publishes its chart renderer on `window.__RAT_CHARTS` so other
plugins (the AI chat) can draw charts with the very same engine.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL (phone-home + running component SQL) |
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

DASH=$(curl -s -X POST localhost:8080/api/v1/x/charts/dashboards \
  -H 'Content-Type: application/json' -d '{"title":"Sales"}' | jq -r .id)
curl -s -X POST localhost:8080/api/v1/x/charts/dashboards/$DASH/components \
  -H 'Content-Type: application/json' \
  -d '{"type":"heading","props":{"text":"Overview","level":1}}' | jq
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

In-memory storage (state is lost on restart) — a production build would back it
with Postgres. Possible next steps: per-dashboard sharing, scheduled snapshots,
and PDF export of a dashboard.
