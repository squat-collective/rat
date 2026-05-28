# rat-plugin-chat

Conversational chat UI in the portal. A data-aware assistant that can
answer questions about your tables, run SQL on your behalf, and (via
[`rat-plugin-agents`](../rat-plugin-agents/)) execute multi-step tasks.

Requires [`rat-plugin-ai-provider`](../rat-plugin-ai-provider/),
[`rat-plugin-interconnect`](../rat-plugin-interconnect/), and (for
multi-step tasks) [`rat-plugin-agents`](../rat-plugin-agents/).

## Install

```bash
docker run -d --name chat --network infra_default --restart unless-stopped \
  -e RATD_URL=http://ratd:8080 \
  -e RATD_INTERNAL_URL=http://ratd:8090 \
  ghcr.io/squat-collective/rat-plugin-chat:latest
```

Or uncomment the `chat:` block in
[`infra/docker-compose.plugins.yml`](../../infra/docker-compose.plugins.yml).

Open **Chat** in the portal sidebar (`/x/chat`).

## How it works

- Conversations are persisted in ratd's plugin-config (chat history
  survives restarts).
- Each user message is sent to `ai.complete` with a system prompt that
  describes RAT's available capabilities (read via interconnect).
- The model picks a capability + payload; the chat plugin executes it
  through interconnect's broker and feeds the result back into the
  conversation.
- For multi-step plans, the chat plugin delegates to
  `rat-plugin-agents`, which streams progress back via SSE.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL |
| `RATD_INTERNAL_URL` | `http://ratd:8090` | ratd internal listener |
| `GRPC_PORT` | `50116` | Port to serve on |

## Build from source

```bash
git clone https://github.com/squat-collective/rat
cd rat/plugins/rat-plugin-chat
make build && make run
```
