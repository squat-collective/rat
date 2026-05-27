# rat-plugin-ai-provider

An example RAT **platform + portal plugin** — a reusable, **configurable AI
provider**.

It is a *backend AI service*, not a chat app. It wraps any OpenAI-compatible
LLM and exposes `/complete` and `/chat` — the primitives other AI extensions
reuse, so they need no LLM code or API keys of their own.

It is also **the first RAT plugin to be configurable**: it declares a
`config_schema_json`, the portal renders a settings form from it, and the
plugin pulls its own config from ratd.

## How configuration works

RAT already had the pieces — this plugin is the first to use them end to end:

1. **Declare** — the plugin returns a `config_schema_json` (a JSON Schema) in
   its `Describe` response.
2. **Edit** — the portal's plugin-config editor renders a form from that
   schema. Saving it calls `PUT /api/v1/plugins/ai-provider/config`, which
   stores the config in `plugin_catalog.config`.
3. **Pull** — ratd stores config but does **not** push it to plugins. So the
   plugin **polls** `GET /api/v1/plugins/ai-provider` every 15s, reads
   `.config`, and merges it over its defaults — changes apply live, no restart.

This poll-for-config pattern is reusable by any plugin that needs settings.

### Config fields

| Field | Purpose |
|---|---|
| `base_url` | OpenAI-compatible `/v1` endpoint (Ollama, OpenAI, vLLM, ...) |
| `api_key` | Bearer token (Ollama ignores it) |
| `model` | Default model name, e.g. `gpt-oss:20b` |
| `system_prompt` | Default system prompt for `/complete` |

## API (proxied at `/api/v1/x/ai-provider/*`)

| Route | Purpose |
|---|---|
| `POST /complete` | `{prompt, system?}` → `{text, model}` — one-shot completion |
| `POST /chat` | `{messages:[{role,content}]}` → `{message, model}` — raw chat |
| `GET /config` | The current effective config (API key masked) |

## Reused by other plugins

On startup it registers two capabilities with the **interconnect** plugin, so
other plugins call it by capability — no hardcoded names:

- `ai.complete` → `POST /complete`
- `ai.chat` → `POST /chat`

```bash
# another plugin invoking the AI through the broker:
curl -s -X POST localhost:8080/api/v1/x/interconnect/invoke -H 'Content-Type: application/json' \
  -d '{"capability":"ai.complete","payload":{"prompt":"Summarise this table."}}'
```

## Environment (initial defaults — override live in the portal)

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL (config pull + capability registration) |
| `OPENAI_BASE_URL` | *(empty)* | initial API base URL |
| `OPENAI_API_KEY` | `ollama` | initial API key |
| `AI_MODEL` | *(empty)* | initial model |
| `GRPC_PORT` | `50094` | port to serve on |
| `PLUGIN_NAME` | `ai-provider` | registered plugin name |
| `PLUGIN_ADDR` | `ai-provider:50094` | address `ratd` dials back |

## Build & run

```bash
make build
make run   # defaults OPENAI_BASE_URL + AI_MODEL — change them in the portal
```

Then open **AI Provider** in the portal: see the effective config, the
exposed capabilities, and a prompt tester. Edit the config in the portal's
**Plugins** page (expand `ai-provider`).

## Run tests

```bash
make test
```
