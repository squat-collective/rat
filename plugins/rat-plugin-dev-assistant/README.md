# rat-plugin-dev-assistant

An example RAT **platform + portal plugin** — an **AI dev assistant** docked
into the pipeline editor. It chats, explains, fixes, and **writes pipeline
code from a described goal** — then applies it straight into the editor.

It is a *thin consumer plugin*: it has **no LLM code or API keys of its own**.
It assembles a RAT pipeline-development prompt plus your editing context, then
brokers the request to [`rat-plugin-ai-provider`](../rat-plugin-ai-provider)
through the [`rat-plugin-interconnect`](../rat-plugin-interconnect) capability
broker (`ai.chat`) — falling back to a direct ai-provider call if the broker
is absent. This is the payoff of the provider + broker architecture.

> **Phase 1.** This ships the editor *panel*. AI autocomplete in the code
> editor is a planned Phase 2.

## What it does

- **Chat** about your pipeline — it knows RAT (layers, `ref()`, merge
  strategies, DuckDB SQL, Jinja, quality tests).
- **Explain / fix** the current file (one-click "Explain file").
- **Generate pipeline code** from a goal — the reply's code block has an
  **"Apply to editor"** button that writes it straight into CodeMirror.
- **Data sample** — optionally previews the current pipeline and feeds the
  columns + sample rows to the model, so generated SQL matches real data.

## How it integrates

It registers a panel component into the core **`pipeline-editor-sidebar`**
slot. The slot hands the panel the live editor content, the pipeline context,
and an `onApply` callback — so the assistant can both read your code and write
back to it. Open any pipeline's **Code** tab; the panel is on the right.

## API

| Route | Purpose |
|---|---|
| `POST /chat` | `{messages, context}` → `{reply, model}`. `context` carries the file content, language, pipeline ref and an optional data sample. |

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL (broker + ai-provider calls) |
| `GRPC_PORT` | `50095` | port to serve on |
| `PLUGIN_NAME` | `dev-assistant` | registered plugin name |
| `PLUGIN_ADDR` | `dev-assistant:50095` | address `ratd` dials back |

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

Then open a pipeline's **Code** tab in the portal.

## Run tests

```bash
make test
```
