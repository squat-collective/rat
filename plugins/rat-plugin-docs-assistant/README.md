# rat-plugin-docs-assistant

An example RAT **platform + portal plugin** — an **AI documentation writer for
datasets**. Adds a *🤖 Suggest docs* button to a table-detail page; clicking it
opens a modal of AI-generated suggestions for the table description and each
column's description, that you can edit and save.

It is a *thin consumer plugin*: it has **no LLM code or API keys of its own**.
The plugin brokers `ai.chat` to [`rat-plugin-ai-provider`](../rat-plugin-ai-provider)
through the [`rat-plugin-interconnect`](../rat-plugin-interconnect) broker
(falling back to a direct ai-provider call if the broker is absent) — the same
architecture pattern as the dev assistant.

## What it does

- Loads the table's current schema + descriptions from `GET /api/v1/tables/{ns}/{layer}/{name}`.
- Pulls a small data sample from the table preview.
- Asks the AI for `{description, column_descriptions}` — grounded in the columns
  and the sample, returned as strict JSON.
- Renders an editable modal: table description on top, a textarea per column
  prefilled with the suggestion. Edit and **Save** writes through the core
  table-metadata API (`PUT .../metadata`).
- Click **↻ Regenerate** to ask again.

## How it integrates

It registers a button into the core **`table-actions`** slot of
`app/explorer/[ns]/[layer]/[name]/page.tsx` — zero core portal changes were
needed.

## API

| Route | Purpose |
|---|---|
| `POST /suggest` | `{table, columns, current_description?, current_column_descriptions?, data_sample?}` → `{description, column_descriptions, model}`. Strict JSON. |

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL (broker + ai-provider calls) |
| `GRPC_PORT` | `50096` | port to serve on |
| `PLUGIN_NAME` | `docs-assistant` | registered plugin name |
| `PLUGIN_ADDR` | `docs-assistant:50096` | address `ratd` dials back |

## Requires

- `rat-plugin-ai-provider` — the AI backend (configure its model/API in the
  portal's plugin settings).
- `rat-plugin-interconnect` — optional; used as the broker. Without it, the
  assistant calls ai-provider directly.

## Build & run

```bash
make build
make run
```

Then open the portal, go to a table in the Explorer, and click **🤖 Suggest
docs**.

## Run tests

```bash
make test
```
