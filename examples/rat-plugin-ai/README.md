# rat-plugin-ai

An example RAT **platform + portal plugin**: an **AI Data Navigator**. Chat with
your data — the assistant inspects table schemas and runs read-only queries to
answer, and the conversation is continuable.

It works with **any OpenAI-compatible API** ([Ollama](https://ollama.com),
OpenAI, vLLM, …) — point `OPENAI_BASE_URL` wherever you like.

## What it does

The plugin gives an LLM a set of **tools** and runs an agentic loop: the model
decides which tools to call, the plugin executes them against `ratd`, feeds the
results back, and repeats until the model has an answer.

| Tool | The model can… |
|---|---|
| `list_tables` | discover every table (`namespace.layer.name`) |
| `describe_table` | inspect a table's column schema |
| `run_query` | run a read-only DuckDB SQL query |
| `render_chart` | draw a bar or line chart from a query |

So "navigate and analyse my data" becomes real — ask a question, the assistant
explores schemas, runs queries, draws charts, and answers from the actual
results.

## How it works

- **Layer 2** — a Go ConnectRPC plugin implementing `PluginService`. It phones
  home to `ratd`, and exposes `POST /chat` (proxied at `/api/v1/x/ai/chat`).
  Chat sessions are kept in memory and are continuable.
- **Layer 3** — a portal UI bundle adds an **`/x/ai`** chat page and an
  "AI Assistant" sidebar item. It renders the assistant's markdown (code blocks,
  lists), draws the charts it produces (build-free SVG/bar rendering), and shows
  the tool calls each turn made — so the conversation is transparent.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `OPENAI_BASE_URL` | `http://localhost:11434/v1` | OpenAI-compatible API base |
| `OPENAI_API_KEY` | `ollama` | API key (Ollama ignores it) |
| `AI_MODEL` | `gpt-oss:20b` | model name — must support tool calling |
| `RATD_URL` | `http://ratd:8080` | ratd base URL (phone-home + tools) |
| `GRPC_PORT` | `50091` | port to serve on |
| `PLUGIN_ADDR` | `ai:50091` | address `ratd` dials back |

<sub>Use a **tool-calling capable** model. A reasoning model is strongly
recommended — the multi-agent flow leans on planning. Verified with
`gpt-oss:20b` on Ollama (it handles the hard multi-step requests; small
non-reasoning models like a 7B struggle with them).</sub>

## Build & run

```bash
# Build the image.
make build

# Run it on ratd's network, pointed at your LLM API.
make run OPENAI_BASE_URL=http://10.2.1.1:11434/v1 AI_MODEL=gpt-oss:20b
```

The plugin must be able to reach both `ratd` and the LLM API. For an Ollama
server on another host, ensure Ollama listens on `0.0.0.0` (`OLLAMA_HOST=0.0.0.0`)
so it is reachable off-box.

Verify, then open the **AI Assistant** page in the portal:

```bash
curl -s localhost:8080/api/v1/plugins | jq '.[] | select(.name=="ai")'
curl -s -X POST localhost:8080/api/v1/x/ai/chat \
  -H 'Content-Type: application/json' \
  -d '{"message":"what tables do I have?"}' | jq
```

## Run tests

```bash
docker run --rm \
  -v "$(pwd)":/work -v "$(pwd)/../../platform":/platform \
  -w /work golang:1.24-alpine \
  sh -c "go mod tidy && go test ./..."
```

## Roadmap

This is the **Data Navigator** slice. Planned next: pipeline-builder tools
(draft / validate / create pipelines from a conversation) and editor
autocomplete.
